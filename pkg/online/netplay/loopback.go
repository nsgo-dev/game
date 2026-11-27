package netplay

import (
	"math/rand/v2"
	"slices"
	"sync"
	"time"
)

// LoopbackConfig configures deterministic network simulation on a LoopbackHub.
type LoopbackConfig struct {
	// Latency is the base one-way packet transit delay.
	Latency time.Duration
	// Jitter adds a uniform random offset in [-Jitter, +Jitter] to Latency (clamped to >= 0).
	Jitter time.Duration
	// LossRate is the drop probability in [0, 1] applied to ChannelUnreliable packets only.
	// ChannelReliableOrdered packets are never dropped and always arrive in send order.
	LossRate float64
	// Seed initializes the deterministic PRNG used for jitter and packet loss.
	Seed int64
	// Inbox bounds the inbox of each peer AddPeer adds from then on (see InboxLimits); the zero value uses the defaults. SetConfig leaves it unchanged; LoopbackTransport.SetInboxLimits changes one peer's.
	Inbox InboxLimits
}

type linkKey struct {
	from PeerID
	to   PeerID
}

type inFlightPacket struct {
	to        PeerID
	pkt       Packet
	deliverAt time.Duration
	order     uint64
}

// LoopbackHub simulates an in-memory packet network between LoopbackTransport peers
// driven by a manual virtual clock via Advance.
type LoopbackHub struct {
	mu           sync.Mutex
	cfg          LoopbackConfig
	rng          *rand.Rand
	now          time.Duration
	nextOrder    uint64
	peers        map[PeerID]*LoopbackTransport
	inFlight     []inFlightPacket
	lastRelDeliv map[linkKey]time.Duration
}

// NewLoopbackHub creates a new deterministic in-memory network hub.
func NewLoopbackHub(cfg LoopbackConfig) *LoopbackHub {
	seed := uint64(cfg.Seed)
	return &LoopbackHub{
		cfg:          cfg,
		rng:          rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)),
		peers:        make(map[PeerID]*LoopbackTransport),
		lastRelDeliv: make(map[linkKey]time.Duration),
	}
}

// Config returns the current network simulation configuration.
func (h *LoopbackHub) Config() LoopbackConfig {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cfg
}

// SetConfig updates Latency, Jitter, and LossRate at runtime without resetting the PRNG or clock.
func (h *LoopbackHub) SetConfig(cfg LoopbackConfig) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cfg.Latency = cfg.Latency
	h.cfg.Jitter = cfg.Jitter
	h.cfg.LossRate = cfg.LossRate
}

// Now returns the hub's current virtual clock time.
func (h *LoopbackHub) Now() time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.now
}

// InFlight returns the number of packets currently in transit.
func (h *LoopbackHub) InFlight() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.inFlight)
}

// AddPeer registers a peer with the given ID and returns its Transport.
// If a peer with the same ID already exists, it is replaced.
func (h *LoopbackHub) AddPeer(id PeerID) *LoopbackTransport {
	h.mu.Lock()
	defer h.mu.Unlock()
	t := &LoopbackTransport{
		hub:     h,
		localID: id,
		seq:     make(map[seqKey]uint32),
	}
	t.inbox.setLimits(h.cfg.Inbox)
	h.peers[id] = t
	return t
}

// RemovePeer disconnects a peer from the hub.
func (h *LoopbackHub) RemovePeer(id PeerID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if t, ok := h.peers[id]; ok {
		t.closed = true
		delete(h.peers, id)
	}
}

// Advance moves the virtual clock forward by dt and delivers all in-flight packets
// whose scheduled arrival time is <= Now().
func (h *LoopbackHub) Advance(dt time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if dt > 0 {
		h.now += dt
	}
	h.flushLocked()
}

func (h *LoopbackHub) flushLocked() {
	if len(h.inFlight) == 0 {
		return
	}
	var ready []inFlightPacket
	remaining := h.inFlight[:0]
	for _, item := range h.inFlight {
		if item.deliverAt <= h.now {
			ready = append(ready, item)
		} else {
			remaining = append(remaining, item)
		}
	}
	h.inFlight = remaining
	if len(ready) == 0 {
		return
	}
	slices.SortFunc(ready, func(a, b inFlightPacket) int {
		if a.deliverAt < b.deliverAt {
			return -1
		}
		if a.deliverAt > b.deliverAt {
			return 1
		}
		if a.order < b.order {
			return -1
		}
		if a.order > b.order {
			return 1
		}
		return 0
	})
	for _, item := range ready {
		if dst, ok := h.peers[item.to]; ok && !dst.closed {
			dst.inbox.push(item.pkt)
		}
	}
}

func (h *LoopbackHub) sampleDelayLocked() time.Duration {
	delay := h.cfg.Latency
	if h.cfg.Jitter > 0 {
		span := int64(h.cfg.Jitter)*2 + 1
		offset := time.Duration(h.rng.Int64N(span)) - h.cfg.Jitter
		delay += offset
	}
	if delay < 0 {
		delay = 0
	}
	return delay
}

func (h *LoopbackHub) sendLocked(from *LoopbackTransport, to PeerID, ch Channel, payload []byte) error {
	if from.closed {
		return ErrClosed
	}
	var targets []PeerID
	if to == PeerBroadcast {
		targets = make([]PeerID, 0, len(h.peers))
		for id := range h.peers {
			if id != from.localID {
				targets = append(targets, id)
			}
		}
		slices.Sort(targets)
	} else {
		if _, ok := h.peers[to]; !ok {
			return ErrUnknownPeer
		}
		targets = []PeerID{to}
	}

	for _, dstID := range targets {
		sk := seqKey{to: dstID, ch: ch}
		from.seq[sk]++
		seq := from.seq[sk]

		if ch == ChannelUnreliable && h.cfg.LossRate > 0 {
			if h.cfg.LossRate >= 1 || h.rng.Float64() < h.cfg.LossRate {
				continue
			}
		}

		delay := h.sampleDelayLocked()
		deliverAt := h.now + delay
		lk := linkKey{from: from.localID, to: dstID}
		if ch == ChannelReliableOrdered {
			if prev, ok := h.lastRelDeliv[lk]; ok && deliverAt < prev {
				deliverAt = prev
			}
			h.lastRelDeliv[lk] = deliverAt
		}

		cp := make([]byte, len(payload))
		copy(cp, payload)
		pkt := Packet{
			From:    from.localID,
			Channel: ch,
			Payload: cp,
			Seq:     seq,
		}
		h.nextOrder++
		if deliverAt <= h.now {
			if dst, ok := h.peers[dstID]; ok && !dst.closed {
				dst.inbox.push(pkt)
			}
			continue
		}
		h.inFlight = append(h.inFlight, inFlightPacket{
			to:        dstID,
			pkt:       pkt,
			deliverAt: deliverAt,
			order:     h.nextOrder,
		})
	}
	return nil
}

type seqKey struct {
	to PeerID
	ch Channel
}

// LoopbackTransport is an endpoint connected to a LoopbackHub.
type LoopbackTransport struct {
	hub     *LoopbackHub
	localID PeerID
	closed  bool
	seq     map[seqKey]uint32
	inbox   packetQueue
}

// Send transmits payload to the target peer (or all other peers if to == PeerBroadcast).
func (t *LoopbackTransport) Send(to PeerID, ch Channel, payload []byte) error {
	t.hub.mu.Lock()
	defer t.hub.mu.Unlock()
	return t.hub.sendLocked(t, to, ch, payload)
}

// Recv pops the next delivered packet without blocking.
func (t *LoopbackTransport) Recv() (Packet, bool) {
	t.hub.mu.Lock()
	defer t.hub.mu.Unlock()
	return t.inbox.pop()
}

// SetInboxLimits bounds the packets the transport keeps until Recv takes them (see InboxLimits).
func (t *LoopbackTransport) SetInboxLimits(l InboxLimits) {
	t.hub.mu.Lock()
	defer t.hub.mu.Unlock()
	t.inbox.setLimits(l)
}

// InboxStats reports what the transport's inbox holds and what its limits dropped or warned about.
func (t *LoopbackTransport) InboxStats() InboxStats {
	t.hub.mu.Lock()
	defer t.hub.mu.Unlock()
	return t.inbox.stats()
}

// LocalID returns this endpoint's PeerID.
func (t *LoopbackTransport) LocalID() PeerID {
	return t.localID
}

// Peers returns the sorted list of other connected peers on the hub.
func (t *LoopbackTransport) Peers() []PeerID {
	t.hub.mu.Lock()
	defer t.hub.mu.Unlock()
	if t.closed {
		return nil
	}
	out := make([]PeerID, 0, len(t.hub.peers))
	for id := range t.hub.peers {
		if id != t.localID {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

// Close disconnects this endpoint from its LoopbackHub.
func (t *LoopbackTransport) Close() error {
	t.hub.RemovePeer(t.localID)
	return nil
}
