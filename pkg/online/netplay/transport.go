// Package netplay provides cross-target multiplayer primitives for NSGo games:
//   - Transport abstraction with deterministic in-memory loopback, pure-Go RFC 6455
//     WebSocket (native + browser WebAssembly), HTTP/3 QUIC WebTransport (native +
//     browser WebAssembly), and native UDP transports.
//   - Server-authoritative state replication with bitmask delta compression,
//     snapshot interpolation, and client-side prediction/reconciliation.
//   - Deterministic lockstep and GGPO-style rollback netcode with periodic
//     checksum verification and desync detection.
//   - A Relay that forwards peer-to-peer sessions between RelayTransports over WebSocket, WebTransport, or UDP for players who cannot reach each other directly.
package netplay

import (
	"encoding/binary"
	"errors"

	"github.com/nsgo-dev/game/pkg/online/internal/wsproto"
)

// Channel selects delivery semantics for a packet.
type Channel uint8

const (
	// ChannelReliableOrdered guarantees delivery in send order. A transport that cannot provide that guarantee refuses it with ErrUnsupportedChannel rather than silently dropping packets; UDP has no retransmission, so lockstep and rollback need WebSocket, loopback or a RelayTransport, which retransmits end to end over either protocol.
	ChannelReliableOrdered Channel = 0
	// ChannelUnreliable delivers best-effort packets that may be dropped or reordered.
	ChannelUnreliable Channel = 1
)

// String returns a human-readable channel name.
func (c Channel) String() string {
	switch c {
	case ChannelReliableOrdered:
		return "reliable-ordered"
	case ChannelUnreliable:
		return "unreliable"
	default:
		return "unknown"
	}
}

// PeerID uniquely identifies a participant in a multiplayer session.
type PeerID uint32

// PeerBroadcast targets all connected remote peers when passed to Transport.Send.
const PeerBroadcast PeerID = 0

// Packet is a single datagram received from a remote peer.
type Packet struct {
	From    PeerID
	Channel Channel
	Payload []byte
	Seq     uint32
}

// Transport is the cross-target messaging interface consumed by replication and lockstep/rollback sessions.
//
// Recv is strictly non-blocking so games can drain incoming packets directly inside a fixed-step game.Loop Update callback without stalling a frame.
//
// A transport has a single consumer. Recv hands each packet to exactly one caller, and the consumers in this package (ServerWorld, ClientWorld, LockstepSession, RollbackSession, Relay and RelayTransport) drain every packet waiting on the transport and discard the ones that are not theirs. Two of them reading the same transport therefore steal each other's packets. To run replication and a lockstep or rollback session side by side, give each its own transport, or put a demultiplexer in front that implements Transport for each consumer.
//
// Every transport in this package bounds its inbox and reports on it through InboxReporter: a peer's unreliable packets past a cap are dropped oldest first, a WebSocketServer ends the session of a client whose reliable packets pass a cap, and a reliable backlog past a threshold is counted as a warning (see InboxLimits).
type Transport interface {
	Send(to PeerID, ch Channel, payload []byte) error
	Recv() (Packet, bool)
	LocalID() PeerID
	Peers() []PeerID
	Close() error
}

var (
	// ErrClosed is returned when operating on a closed transport.
	ErrClosed = errors.New("net: transport closed")
	// ErrUnknownPeer is returned when sending to a peer that is not connected.
	ErrUnknownPeer = errors.New("net: unknown peer")
	// ErrInvalidPacket is returned when a wire frame is malformed or truncated.
	ErrInvalidPacket = wsproto.ErrInvalidPacket
	// ErrPayloadTooLarge is returned when a payload exceeds transport limits.
	ErrPayloadTooLarge = wsproto.ErrPayloadTooLarge
	// ErrUnsupportedChannel is returned when a transport cannot provide the delivery guarantees the requested channel asks for.
	ErrUnsupportedChannel = errors.New("net: channel not supported by transport")
	// ErrReceiveFailed is returned, wrapping the socket's error, by a transport whose socket failed so that it can no longer receive.
	ErrReceiveFailed = errors.New("net: receive failed")
)

const wireHeaderSize = 5

// encodeWireFrame prepends the compact 5-byte transport header (channel + seq)
// to payload, allocating a fresh slice safe for asynchronous transmission.
func encodeWireFrame(ch Channel, seq uint32, payload []byte) []byte {
	frame := make([]byte, wireHeaderSize+len(payload))
	frame[0] = byte(ch)
	binary.BigEndian.PutUint32(frame[1:5], seq)
	copy(frame[wireHeaderSize:], payload)
	return frame
}

// decodeWireFrame parses the 5-byte transport header and returns a copy-free
// slice view of the payload.
func decodeWireFrame(frame []byte) (ch Channel, seq uint32, payload []byte, err error) {
	if len(frame) < wireHeaderSize {
		return 0, 0, nil, ErrInvalidPacket
	}
	ch = Channel(frame[0])
	if ch != ChannelReliableOrdered && ch != ChannelUnreliable {
		return 0, 0, nil, ErrInvalidPacket
	}
	seq = binary.BigEndian.Uint32(frame[1:5])
	payload = frame[wireHeaderSize:]
	return ch, seq, payload, nil
}

const (
	// DefaultUnreliableInbox is the most ChannelUnreliable packets a transport's inbox holds from one peer when InboxLimits.Unreliable is zero.
	DefaultUnreliableInbox = 4096
	// DefaultReliableInbox is the most ChannelReliableOrdered packets a WebSocketServer's, WebTransportServer's, or RelayTransport's inbox holds from one peer when InboxLimits.Reliable is zero.
	DefaultReliableInbox = 16384
	// DefaultReliableInboxWarning is the ChannelReliableOrdered backlog that raises a warning when InboxLimits.ReliableWarning is zero.
	DefaultReliableInboxWarning = 4096
)

// InboxLimits bounds the packets a transport keeps until Recv takes them. Every transport in this package accepts it through SetInboxLimits, and LoopbackConfig and RelayTransportConfig carry it too.
//
// The unreliable channel is capped per peer: when a ChannelUnreliable packet arrives from a peer that already has Unreliable packets queued, that peer's oldest queued unreliable packet is dropped to make room, so a consumer that falls behind keeps the newest state rather than the stalest, a peer that floods the inbox evicts only its own packets, and InboxStats.UnreliableDropped counts the loss.
//
// The reliable channel is never trimmed, because dropping a packet would break ChannelReliableOrdered. A transport that receives reliable packets from peers it cannot trust caps them per peer at Reliable instead and refuses the packet that goes past it, counting it in InboxStats.ReliableRefused: WebSocketServer then ends that client's session, as if it had left, so the packets it did deliver stay in order, and RelayTransport leaves the message unacknowledged, so the sending peer resends it once Recv has made room. The other transports receive reliable packets only from their server or from the same process and keep them all. Every transport counts a warning in InboxStats.ReliableWarnings when its whole reliable backlog passes ReliableWarning.
type InboxLimits struct {
	// Unreliable is the most ChannelUnreliable packets the inbox holds from one peer. Zero uses DefaultUnreliableInbox; a negative value removes the cap.
	Unreliable int
	// Reliable is the most ChannelReliableOrdered packets a WebSocketServer's or RelayTransport's inbox holds from one peer. Zero uses DefaultReliableInbox; a negative value removes the cap. The other transports ignore it.
	Reliable int
	// ReliableWarning is the ChannelReliableOrdered backlog, across all peers, that raises a warning: the warning counts once when the backlog grows past it, and counts again only after the backlog has fallen to half of it, so a queue hovering at the threshold does not raise one warning per packet. Zero uses DefaultReliableInboxWarning; a negative value turns the warning off.
	ReliableWarning int
}

// resolveLimit returns v, def when v is zero, or 0 for none when v is negative.
func resolveLimit(v, def int) int {
	switch {
	case v < 0:
		return 0
	case v == 0:
		return def
	}
	return v
}

// unreliableCap returns the resolved per-peer unreliable cap, or 0 for none.
func (l InboxLimits) unreliableCap() int { return resolveLimit(l.Unreliable, DefaultUnreliableInbox) }

// reliableCap returns the resolved per-peer reliable cap, or 0 for none.
func (l InboxLimits) reliableCap() int { return resolveLimit(l.Reliable, DefaultReliableInbox) }

// reliableWarning returns the resolved reliable warning threshold, or 0 for none.
func (l InboxLimits) reliableWarning() int {
	return resolveLimit(l.ReliableWarning, DefaultReliableInboxWarning)
}

// InboxStats reports a transport's inbox: what is queued and what the limits did. The counters only grow, so a game or a debug overlay that polls InboxStats every frame reports a warning or a loss when a counter differs from the previous frame's.
type InboxStats struct {
	// Unreliable is the number of ChannelUnreliable packets waiting for Recv.
	Unreliable int
	// Reliable is the number of ChannelReliableOrdered packets waiting for Recv.
	Reliable int
	// UnreliableDropped counts the unreliable packets dropped because their peer already had InboxLimits.Unreliable packets queued when a newer one arrived.
	UnreliableDropped uint64
	// ReliableRefused counts the reliable packets a WebSocketServer or RelayTransport refused because their peer already had InboxLimits.Reliable packets queued.
	ReliableRefused uint64
	// ReliableWarnings counts the times the reliable backlog grew past InboxLimits.ReliableWarning: once per crossing, not once per packet.
	ReliableWarnings uint64
	// ReliableBacklog is true from the moment the reliable backlog passes InboxLimits.ReliableWarning until it falls to half of it.
	ReliableBacklog bool
}

// InboxReporter is implemented by every transport in this package: LoopbackTransport, WebSocketTransport, WebSocketServer, WebTransportTransport, WebTransportServer, UDPTransport, UDPServer, UDPClient and RelayTransport. SetInboxLimits changes the inbox's limits at any time; lowering the unreliable cap below what a peer has queued drops that peer's oldest unreliable packets at once and counts them, and lowering the reliable cap refuses only the packets that arrive afterwards. InboxStats reads the inbox's counters.
type InboxReporter interface {
	SetInboxLimits(InboxLimits)
	InboxStats() InboxStats
}

// packetFIFOKeep is the largest buffer, in entries, a fifo keeps once it is empty.
const packetFIFOKeep = 256

// queuedPacket is a packet with the order it arrived in, so a packetQueue can merge its FIFOs back into arrival order.
type queuedPacket struct {
	pkt Packet
	n   uint64
}

// fifo is a FIFO. Pop advances a head index instead of shifting the slice, and the FIFO compacts only once the consumed prefix is at least as long as what remains, so draining n entries moves at most n entries in total rather than n²/2 (#546).
type fifo[T any] struct {
	buf  []T
	head int
	// moved counts entries copied by compaction, so tests can check that a drain stays linear.
	moved int
}

// packetFIFO is a FIFO of received packets.
type packetFIFO = fifo[queuedPacket]

// push appends v to the back of the FIFO.
func (f *fifo[T]) push(v T) {
	f.buf = append(f.buf, v)
}

// front returns the entry at the front of the FIFO without removing it.
func (f *fifo[T]) front() (T, bool) {
	if f.head >= len(f.buf) {
		var zero T
		return zero, false
	}
	return f.buf[f.head], true
}

// pop removes and returns the entry at the front of the FIFO.
func (f *fifo[T]) pop() (T, bool) {
	var zero T
	if f.head >= len(f.buf) {
		return zero, false
	}
	v := f.buf[f.head]
	f.buf[f.head] = zero // release a packet's payload for the garbage collector
	f.head++
	switch rest := len(f.buf) - f.head; {
	case rest == 0 && cap(f.buf) > packetFIFOKeep:
		f.buf, f.head = nil, 0 // a burst is over: give its buffer back rather than hold it for good
	case rest == 0:
		f.buf, f.head = f.buf[:0], 0
	case f.head >= 64 && f.head >= rest:
		n := copy(f.buf, f.buf[f.head:])
		clear(f.buf[n:])
		f.buf, f.head = f.buf[:n], 0
		f.moved += n
	}
	return v, true
}

// len returns the number of queued entries.
func (f *fifo[T]) len() int {
	return len(f.buf) - f.head
}

// unreliableRef records which peer's unreliable FIFO holds the packet that arrived n-th.
type unreliableRef struct {
	from PeerID
	n    uint64
}

// packetQueue is a transport's inbox (#546). Reliable packets share one FIFO; unreliable packets have a FIFO per peer, so the oldest packet of a peer past its cap is dropped in constant time without touching another peer's. Every packet is stamped with its arrival order, and order lists the unreliable arrivals across peers, so pop still returns packets in the order they arrived. A drop leaves its entry in order behind as stale; pop skips stale entries, and compactOrder removes them once there are at least 64 and they outnumber the live ones, so order never holds more than 64 entries beyond twice the most unreliable packets the inbox has held. Its owner serializes access with the transport's lock.
type packetQueue struct {
	rel     packetFIFO
	relFrom map[PeerID]int
	unrel   map[PeerID]*packetFIFO
	order   fifo[unreliableRef]
	unrelN  int
	stale   int
	next    uint64
	limits  InboxLimits
	dropped uint64
	refused uint64
	warns   uint64
	backlog bool
}

// push queues p and never refuses it: a peer past its unreliable cap loses its oldest unreliable packet instead, and the reliable channel is not capped. A channel other than ChannelUnreliable is kept as reliable, so a packet is never dropped on a channel that may promise delivery.
func (q *packetQueue) push(p Packet) {
	if p.Channel == ChannelUnreliable {
		q.pushUnreliable(p)
		return
	}
	q.pushReliable(p)
}

// admit queues p like push, except that it refuses a reliable packet, and returns false, when p's peer already has InboxLimits.Reliable reliable packets queued. The transports whose peers cannot be trusted (WebSocketServer and RelayTransport) use it.
func (q *packetQueue) admit(p Packet) bool {
	if p.Channel == ChannelUnreliable {
		q.pushUnreliable(p)
		return true
	}
	if !q.hasReliableRoom(p.From) {
		q.refused++
		return false
	}
	q.pushReliable(p)
	return true
}

// hasReliableRoom reports whether admit would accept a reliable packet from peer.
func (q *packetQueue) hasReliableRoom(peer PeerID) bool {
	limit := q.limits.reliableCap()
	return limit == 0 || q.relFrom[peer] < limit
}

// pushReliable queues a reliable packet and counts a warning when it takes the backlog past its threshold.
func (q *packetQueue) pushReliable(p Packet) {
	q.next++
	q.rel.push(queuedPacket{pkt: p, n: q.next})
	if q.relFrom == nil {
		q.relFrom = make(map[PeerID]int)
	}
	q.relFrom[p.From]++
	q.checkBacklog()
}

// pushUnreliable queues an unreliable packet, dropping its peer's oldest unreliable packet when that peer is past its cap.
func (q *packetQueue) pushUnreliable(p Packet) {
	q.next++
	f := q.unrel[p.From]
	if f == nil {
		if q.unrel == nil {
			q.unrel = make(map[PeerID]*packetFIFO)
		}
		f = new(packetFIFO)
		q.unrel[p.From] = f
	}
	f.push(queuedPacket{pkt: p, n: q.next})
	q.order.push(unreliableRef{from: p.From, n: q.next})
	q.unrelN++
	q.trimPeer(f)
}

// oldestUnreliable returns the unreliable packet that arrived first across peers, discarding the stale entries at the front of order.
func (q *packetQueue) oldestUnreliable() (queuedPacket, bool) {
	for {
		ref, ok := q.order.front()
		if !ok {
			return queuedPacket{}, false
		}
		if f := q.unrel[ref.from]; f != nil {
			if p, _ := f.front(); p.n == ref.n {
				return p, true
			}
		}
		q.order.pop()
		q.stale--
	}
}

// pop removes and returns the packet that arrived first across both channels and every peer.
func (q *packetQueue) pop() (Packet, bool) {
	r, rok := q.rel.front()
	u, uok := q.oldestUnreliable()
	switch {
	case rok && (!uok || r.n < u.n):
		q.rel.pop()
		if q.relFrom[r.pkt.From]--; q.relFrom[r.pkt.From] <= 0 {
			delete(q.relFrom, r.pkt.From)
		}
		q.checkBacklog()
		return r.pkt, true
	case uok:
		q.order.pop()
		f := q.unrel[u.pkt.From]
		f.pop()
		if f.len() == 0 {
			delete(q.unrel, u.pkt.From)
		}
		q.unrelN--
		return u.pkt, true
	}
	return Packet{}, false
}

// len returns the number of queued packets on both channels.
func (q *packetQueue) len() int {
	return q.rel.len() + q.unrelN
}

// setLimits replaces the limits, dropping each peer's oldest unreliable packets past a lowered cap and reevaluating the reliable warning against the new threshold: a backlog that is not past the new threshold clears the flag, so the first crossing of a raised threshold is counted.
func (q *packetQueue) setLimits(l InboxLimits) {
	q.limits = l
	for _, f := range q.unrel {
		q.trimPeer(f)
	}
	if warn := l.reliableWarning(); warn == 0 || q.rel.len() <= warn {
		q.backlog = false
	}
	q.checkBacklog()
}

// stats returns the inbox's counters.
func (q *packetQueue) stats() InboxStats {
	return InboxStats{
		Unreliable:        q.unrelN,
		Reliable:          q.rel.len(),
		UnreliableDropped: q.dropped,
		ReliableRefused:   q.refused,
		ReliableWarnings:  q.warns,
		ReliableBacklog:   q.backlog,
	}
}

// trimPeer drops a peer's oldest unreliable packets until its FIFO fits the cap. The cap is at least one, so the FIFO is never emptied here.
func (q *packetQueue) trimPeer(f *packetFIFO) {
	limit := q.limits.unreliableCap()
	if limit == 0 {
		return
	}
	for f.len() > limit {
		f.pop()
		q.unrelN--
		q.dropped++
		q.stale++
	}
	if q.stale >= 64 && q.stale > q.unrelN {
		q.compactOrder()
	}
}

// compactOrder removes the stale entries from order. An entry is live while its peer's FIFO still holds a packet at least as new as it, since a peer's packets leave only from the front.
func (q *packetQueue) compactOrder() {
	k := 0
	for i := q.order.head; i < len(q.order.buf); i++ {
		ref := q.order.buf[i]
		if f := q.unrel[ref.from]; f != nil {
			if p, _ := f.front(); ref.n >= p.n {
				q.order.buf[k] = ref
				k++
			}
		}
	}
	clear(q.order.buf[k:])
	q.order.buf, q.order.head = q.order.buf[:k], 0
	q.order.moved += k
	q.stale = 0
}

// checkBacklog raises the reliable warning when the backlog is past the threshold and the warning is armed, and rearms it once the backlog has fallen to half the threshold.
func (q *packetQueue) checkBacklog() {
	warn := q.limits.reliableWarning()
	switch n := q.rel.len(); {
	case warn == 0:
		q.backlog = false
	case n > warn && !q.backlog:
		q.backlog = true
		q.warns++
	case n <= warn/2:
		q.backlog = false
	}
}
