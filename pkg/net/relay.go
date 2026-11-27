package net

import (
	"encoding/binary"
	"errors"
	"slices"
	"sync"
	"time"
)

// Relay wire format, version 1. Every relay frame travels as one ChannelUnreliable packet of the inner client or server transport, because reliability is end to end between the two RelayTransports, not per hop:
//
//	client to relay: version(1) to(4) body
//	relay to client: version(1) from(4) body
//
// A frame from the relay itself has from 0 and carries a control message:
//
//	members: version(1) 0(4) relayCtlMembers(1) epoch(4) count(2) peer(4)...
//
// The epoch counts the session's membership changes. The relay sends the membership again every second with the same epoch, and a client compares epochs as serial numbers, so the counter may wrap.
//
// A body between two peers is one of:
//
//	unreliable: relayBodyUnreliable(1) seq(4) payload
//	reliable:   relayBodyReliable(1) seq(4) payload
//	ack:        relayBodyAck(1) next(4) mask(8)
//
// An ack says every reliable seq below next arrived, and bit i of mask says next+1+i arrived too, so the sender retransmits only what is missing.
const (
	relayVersion     = 1
	relayFrameHeader = 5

	relayCtlMembers = 0x01

	relayBodyUnreliable = 0x01
	relayBodyReliable   = 0x02
	relayBodyAck        = 0x03

	relayDataHeader = 5
	relayAckSize    = 13

	// relayWindow is how many reliable messages may be in flight to one peer, and how far ahead of the next expected one a receiver buffers.
	relayWindow = 1024

	relayInitialRTO = 200 * time.Millisecond
	relayMinRTO     = 20 * time.Millisecond
	relayMaxRTO     = 2 * time.Second

	// relayMaxBuffered is how many payload bytes a receiver holds for one peer's reliable messages that arrived ahead of a missing one. A frame past it is dropped and the sender resends it after a timeout, so a peer that never sends the missing message cannot make the receiver hold relayWindow full payloads.
	relayMaxBuffered = 4 << 20

	// relayMaxRetransmits bounds the retransmissions one Recv call sends to one peer, so a long outage does not turn into a burst.
	relayMaxRetransmits = 64

	// MaxRelayPayload is the largest payload RelayTransport.Send accepts: what fits in one UDP datagram after the relay's and the transport's headers.
	MaxRelayPayload = 64000
)

// ErrRelayBacklog is returned by RelayTransport.Send when a peer has not acknowledged the last 1024 reliable messages sent to it, so the sender stops instead of buffering without bound.
var ErrRelayBacklog = errors.New("net: relay backlog full")

// encodeRelayFrame builds a frame addressed to or from peer.
func encodeRelayFrame(peer PeerID, body []byte) []byte {
	b := make([]byte, relayFrameHeader+len(body))
	b[0] = relayVersion
	binary.BigEndian.PutUint32(b[1:5], uint32(peer))
	copy(b[relayFrameHeader:], body)
	return b
}

// decodeRelayFrame returns the frame's peer and a view of its body.
func decodeRelayFrame(b []byte) (PeerID, []byte, bool) {
	if len(b) < relayFrameHeader || b[0] != relayVersion {
		return 0, nil, false
	}
	return PeerID(binary.BigEndian.Uint32(b[1:5])), b[relayFrameHeader:], true
}

// encodeRelayMembers builds the relay's membership message for a session.
func encodeRelayMembers(epoch uint32, members []PeerID) []byte {
	body := make([]byte, 7+4*len(members))
	body[0] = relayCtlMembers
	binary.BigEndian.PutUint32(body[1:5], epoch)
	binary.BigEndian.PutUint16(body[5:7], uint16(len(members)))
	for i, id := range members {
		binary.BigEndian.PutUint32(body[7+4*i:], uint32(id))
	}
	return encodeRelayFrame(0, body)
}

// decodeRelayMembers parses a membership body. The peers must be sorted, distinct and non-zero.
func decodeRelayMembers(body []byte) (uint32, []PeerID, bool) {
	if len(body) < 7 || body[0] != relayCtlMembers {
		return 0, nil, false
	}
	epoch := binary.BigEndian.Uint32(body[1:5])
	n := int(binary.BigEndian.Uint16(body[5:7]))
	if len(body) != 7+4*n {
		return 0, nil, false
	}
	members := make([]PeerID, n)
	for i := range members {
		members[i] = PeerID(binary.BigEndian.Uint32(body[7+4*i:]))
		if members[i] == 0 || (i > 0 && members[i] <= members[i-1]) {
			return 0, nil, false
		}
	}
	return epoch, members, true
}

func encodeRelayData(kind byte, seq uint32, payload []byte) []byte {
	b := make([]byte, relayDataHeader+len(payload))
	b[0] = kind
	binary.BigEndian.PutUint32(b[1:5], seq)
	copy(b[relayDataHeader:], payload)
	return b
}

func encodeRelayAck(next uint32, mask uint64) []byte {
	b := make([]byte, relayAckSize)
	b[0] = relayBodyAck
	binary.BigEndian.PutUint32(b[1:5], next)
	binary.BigEndian.PutUint64(b[5:13], mask)
	return b
}

// RelayTransportConfig configures a RelayTransport.
type RelayTransportConfig struct {
	// Now is the clock for retransmission timers; nil uses time.Now.
	Now func() time.Time
	// Inbox bounds the packets the RelayTransport keeps until Recv takes them (see InboxLimits); the zero value uses the defaults. The inner transport's inbox has its own limits, set on it directly; the relay carries reliable messages as unreliable frames, so a frame the inner inbox drops is sent again.
	Inbox InboxLimits
}

// RelayTransport is a peer-to-peer Transport whose packets travel through a Relay, so peers that cannot reach each other, behind NAT or in a browser, still exchange packets. It wraps a client transport connected to the relay: a WebSocketTransport (native or browser) or a UDPClient.
//
// Peers reports the other members of the relay session the client joined, and Send addresses them by the IDs the relay assigned. ChannelReliableOrdered is reliable and ordered end to end: the transport numbers each peer's messages, the receiver acknowledges them, and lost ones are sent again, so it works over UDP, which drops datagrams, and survives the relay dropping a packet for a peer that is reconnecting. ChannelUnreliable packets are sent once and may be lost.
//
// Acknowledgements and retransmissions happen inside Recv, so a game polls Recv every tick, as LockstepSession and RollbackSession do.
type RelayTransport struct {
	inner Transport
	now   func() time.Time

	mu         sync.Mutex
	localID    PeerID
	admissions uint64
	epoch      uint32
	members    []PeerID
	peers      map[PeerID]*relayPeer
	inbox      packetQueue
}

// relayPeer is the end-to-end state for one remote peer.
type relayPeer struct {
	unrelSeq uint32

	// Sending reliable messages.
	sendSeq uint32
	unacked []*relaySegment // in seq order
	sampled bool
	srtt    time.Duration
	rttvar  time.Duration
	rto     time.Duration

	// Receiving reliable messages.
	recvNext uint32
	ooo      map[uint32][]byte
	oooBytes int
	ackDue   bool
}

type relaySegment struct {
	seq     uint32
	body    []byte
	sentAt  time.Time
	due     time.Time
	retries int
	sacked  bool
}

// NewRelayTransport wraps inner, a client transport whose handshake with a Relay has succeeded. Closing the RelayTransport closes inner. To reconnect after a drop, call the inner transport's Reconnect: within the relay's grace period the client keeps its ID and its messages, and unacknowledged reliable ones are sent again.
func NewRelayTransport(inner Transport, cfg RelayTransportConfig) *RelayTransport {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	rt := &RelayTransport{inner: inner, now: now, localID: inner.LocalID(), admissions: relayAdmissionsOf(inner), peers: make(map[PeerID]*relayPeer)}
	rt.inbox.setLimits(cfg.Inbox)
	return rt
}

// relayAdmitted is implemented by the client transports that count the handshakes admitting them as a new peer: WebSocketTransport and UDPClient.
type relayAdmitted interface {
	relayAdmissions() uint64
}

func relayAdmissionsOf(inner Transport) uint64 {
	if a, ok := inner.(relayAdmitted); ok {
		return a.relayAdmissions()
	}
	return 0
}

// LocalID returns the ID the relay assigned this client.
func (rt *RelayTransport) LocalID() PeerID {
	return rt.inner.LocalID()
}

// Peers returns the sorted IDs of the other members of this client's relay session, including members inside the relay's grace period, as of the last membership update Recv processed.
func (rt *RelayTransport) Peers() []PeerID {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.pumpLocked()
	return rt.othersLocked()
}

func (rt *RelayTransport) othersLocked() []PeerID {
	out := make([]PeerID, 0, len(rt.members))
	for _, id := range rt.members {
		if id != rt.localID {
			out = append(out, id)
		}
	}
	return out
}

// Send sends payload to a member of the session, or to every other member when to is PeerBroadcast. It returns ErrUnknownPeer for an ID outside the session, ErrPayloadTooLarge above MaxRelayPayload and ErrRelayBacklog when a peer has too many unacknowledged reliable messages. A reliable message the inner transport cannot send right now, because it is reconnecting, is kept and sent again; an unreliable one returns the inner transport's error.
func (rt *RelayTransport) Send(to PeerID, ch Channel, payload []byte) error {
	if ch != ChannelReliableOrdered && ch != ChannelUnreliable {
		return ErrInvalidPacket
	}
	if len(payload) > MaxRelayPayload {
		return ErrPayloadTooLarge
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.syncLocalLocked()
	var targets []PeerID
	if to == PeerBroadcast {
		targets = rt.othersLocked()
	} else {
		if to == rt.localID || !slices.Contains(rt.members, to) {
			return ErrUnknownPeer
		}
		targets = []PeerID{to}
	}
	if ch == ChannelReliableOrdered {
		for _, id := range targets {
			if len(rt.peerLocked(id).unacked) >= relayWindow {
				return ErrRelayBacklog
			}
		}
	}
	now := rt.now()
	var firstErr error
	for _, id := range targets {
		p := rt.peerLocked(id)
		if ch == ChannelUnreliable {
			p.unrelSeq++
			err := rt.inner.Send(PeerBroadcast, ChannelUnreliable, encodeRelayFrame(id, encodeRelayData(relayBodyUnreliable, p.unrelSeq, payload)))
			if err != nil && firstErr == nil {
				firstErr = err
			}
			continue
		}
		p.sendSeq++
		seg := &relaySegment{seq: p.sendSeq, body: encodeRelayData(relayBodyReliable, p.sendSeq, payload), sentAt: now, due: now.Add(p.rto)}
		p.unacked = append(p.unacked, seg)
		if err := rt.inner.Send(PeerBroadcast, ChannelUnreliable, encodeRelayFrame(id, seg.body)); errors.Is(err, ErrClosed) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Recv pops the next packet from a session member without blocking. Each call first drains the inner transport, acknowledges reliable messages and resends the ones whose acknowledgement is overdue.
func (rt *RelayTransport) Recv() (Packet, bool) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.pumpLocked()
	return rt.inbox.pop()
}

// SetInboxLimits bounds the packets the transport keeps until Recv takes them (see InboxLimits). A peer whose queued reliable messages reach InboxLimits.Reliable is not acknowledged further, so it resends them once Recv has made room.
func (rt *RelayTransport) SetInboxLimits(l InboxLimits) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.inbox.setLimits(l)
}

// InboxStats reports what the transport's inbox holds and what its limits dropped or warned about.
func (rt *RelayTransport) InboxStats() InboxStats {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.inbox.stats()
}

// Close closes the inner transport.
func (rt *RelayTransport) Close() error {
	return rt.inner.Close()
}

// syncLocalLocked forgets the session when the inner transport was admitted as a new peer instead of resuming: after its grace period ran out, which also changes its ID, or after the relay restarted, which may hand out the same ID again. The new relay session's epochs, members and message numbers have nothing to do with the old ones.
func (rt *RelayTransport) syncLocalLocked() {
	id, admissions := rt.inner.LocalID(), relayAdmissionsOf(rt.inner)
	if id != rt.localID || admissions != rt.admissions {
		rt.localID = id
		rt.admissions = admissions
		rt.epoch = 0
		rt.members = nil
		clear(rt.peers)
	}
}

func (rt *RelayTransport) peerLocked(id PeerID) *relayPeer {
	p := rt.peers[id]
	if p == nil {
		p = &relayPeer{rto: relayInitialRTO, recvNext: 1, ooo: make(map[uint32][]byte)}
		rt.peers[id] = p
	}
	return p
}

// pumpLocked drains the inner transport, then sends due acknowledgements and retransmissions.
func (rt *RelayTransport) pumpLocked() {
	rt.syncLocalLocked()
	now := rt.now()
	for {
		pkt, ok := rt.inner.Recv()
		if !ok {
			break
		}
		rt.ingestLocked(pkt.Payload, now)
	}
	ids := make([]PeerID, 0, len(rt.peers))
	for id := range rt.peers {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		p := rt.peers[id]
		rt.deliverBufferedLocked(id, p)
		if p.ackDue {
			p.ackDue = false
			_ = rt.inner.Send(PeerBroadcast, ChannelUnreliable, encodeRelayFrame(id, encodeRelayAck(p.recvNext, p.sackMask())))
		}
		sent := 0
		for _, seg := range p.unacked {
			if sent == relayMaxRetransmits {
				break
			}
			if seg.sacked || now.Before(seg.due) {
				continue
			}
			seg.retries++
			seg.sentAt = now
			seg.due = now.Add(min(p.rto<<min(seg.retries, 6), relayMaxRTO))
			sent++
			_ = rt.inner.Send(PeerBroadcast, ChannelUnreliable, encodeRelayFrame(id, seg.body))
		}
	}
}

// ingestLocked handles one frame from the relay.
func (rt *RelayTransport) ingestLocked(frame []byte, now time.Time) {
	from, body, ok := decodeRelayFrame(frame)
	if !ok || len(body) == 0 {
		return
	}
	if from == 0 {
		rt.membersLocked(body)
		return
	}
	if from == rt.localID || !slices.Contains(rt.members, from) {
		return
	}
	p := rt.peerLocked(from)
	switch body[0] {
	case relayBodyUnreliable:
		if len(body) < relayDataHeader {
			return
		}
		rt.inbox.push(Packet{From: from, Channel: ChannelUnreliable, Payload: slices.Clone(body[relayDataHeader:]), Seq: binary.BigEndian.Uint32(body[1:5])})
	case relayBodyReliable:
		if len(body) < relayDataHeader {
			return
		}
		rt.reliableLocked(from, p, binary.BigEndian.Uint32(body[1:5]), body[relayDataHeader:])
	case relayBodyAck:
		if len(body) != relayAckSize {
			return
		}
		p.ack(binary.BigEndian.Uint32(body[1:5]), binary.BigEndian.Uint64(body[5:13]), now)
	}
}

// membersLocked applies a membership update newer than the last one, forgetting peers that left. Epochs compare as serial numbers, so an update after the relay's counter wraps still counts as newer.
func (rt *RelayTransport) membersLocked(body []byte) {
	epoch, members, ok := decodeRelayMembers(body)
	if !ok || (rt.members != nil && int32(epoch-rt.epoch) <= 0) || !slices.Contains(members, rt.localID) {
		return
	}
	rt.epoch = epoch
	rt.members = members
	for id := range rt.peers {
		if !slices.Contains(members, id) {
			delete(rt.peers, id)
		}
	}
}

// reliableLocked accepts a reliable message, delivering it and any buffered successors in order.
func (rt *RelayTransport) reliableLocked(from PeerID, p *relayPeer, seq uint32, payload []byte) {
	p.ackDue = true
	switch {
	case seq < p.recvNext || seq-p.recvNext >= relayWindow:
		return // a duplicate, or too far ahead; the ack tells the sender where we are
	case seq > p.recvNext:
		if _, dup := p.ooo[seq]; !dup && p.oooBytes+len(payload) <= relayMaxBuffered {
			p.ooo[seq] = slices.Clone(payload)
			p.oooBytes += len(payload)
		}
		return
	}
	// A peer past its reliable inbox cap is not acknowledged further, so it resends once Recv has made room instead of growing the inbox without bound (#546).
	if !rt.inbox.admit(Packet{From: from, Channel: ChannelReliableOrdered, Payload: slices.Clone(payload), Seq: seq}) {
		return
	}
	p.recvNext++
	rt.deliverBufferedLocked(from, p)
}

// deliverBufferedLocked delivers a peer's buffered messages that follow the last one delivered, while its reliable inbox has room. A sender never resends a message the receiver reported as buffered, so pumpLocked calls it again after Recv has made room, and a message held back by the cap is still delivered.
func (rt *RelayTransport) deliverBufferedLocked(from PeerID, p *relayPeer) {
	for {
		next, ok := p.ooo[p.recvNext]
		if !ok || !rt.inbox.hasReliableRoom(from) {
			return
		}
		rt.inbox.push(Packet{From: from, Channel: ChannelReliableOrdered, Payload: next, Seq: p.recvNext})
		delete(p.ooo, p.recvNext)
		p.oooBytes -= len(next)
		p.recvNext++
		p.ackDue = true
	}
}

// sackMask reports which of the 64 messages after recvNext have been buffered.
func (p *relayPeer) sackMask() uint64 {
	var mask uint64
	for seq := range p.ooo {
		if d := seq - p.recvNext - 1; d < 64 {
			mask |= 1 << d
		}
	}
	return mask
}

// ack drops acknowledged segments, marks selectively acknowledged ones and updates the retransmission timeout from segments sent once (Karn's rule). A segment is timed when it is first acknowledged, cumulatively or selectively, so a segment held back behind a lost one does not count the wait for that one's retransmission.
func (p *relayPeer) ack(next uint32, mask uint64, now time.Time) {
	if next > p.sendSeq+1 {
		return // acknowledges messages never sent
	}
	keep := p.unacked[:0]
	for _, seg := range p.unacked {
		if seg.seq < next {
			if seg.retries == 0 && !seg.sacked {
				p.sample(now.Sub(seg.sentAt))
			}
			continue
		}
		if d := seg.seq - next - 1; !seg.sacked && seg.seq > next && d < 64 && mask&(1<<d) != 0 {
			seg.sacked = true
			if seg.retries == 0 {
				p.sample(now.Sub(seg.sentAt))
			}
		}
		keep = append(keep, seg)
	}
	clear(p.unacked[len(keep):])
	p.unacked = keep
}

// sample folds a round-trip time into the retransmission timeout as RFC 6298 does.
func (p *relayPeer) sample(rtt time.Duration) {
	if rtt < 0 {
		return
	}
	if !p.sampled {
		p.sampled = true
		p.srtt, p.rttvar = rtt, rtt/2
	} else {
		diff := p.srtt - rtt
		if diff < 0 {
			diff = -diff
		}
		p.rttvar = (3*p.rttvar + diff) / 4
		p.srtt = (7*p.srtt + rtt) / 8
	}
	p.rto = min(max(p.srtt+4*p.rttvar, relayMinRTO), relayMaxRTO)
}
