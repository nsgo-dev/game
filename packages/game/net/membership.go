package net

import (
	"cmp"
	"encoding/binary"
	"errors"
	"slices"
)

// Players joining and leaving a running lockstep or rollback match (#744).
//
// One peer of a match, the host, decides every change to its peers. To admit a peer it picks a join tick T that no peer can have simulated yet, tells the other peers that the newcomer takes part from T, and once its own simulation stands at the start of T it sends the newcomer the state at T, the tick and the peers. To remove a peer it picks a leave tick L the same way and sends, with the change, the inputs it holds from the leaving peer for the ticks before L, taking a zero input for any tick it never received, so every peer runs the same ticks with the same inputs.
//
// A tick no peer can have simulated yet is one at or after the host's next own input: a lockstep peer runs a tick only with every peer's input for it and a rollback peer confirms one only that way, and the change travels on the same reliable, ordered channel as the host's inputs, ahead of the host's input for that tick.

var (
	// ErrNotHost is returned by AddPeer and RemovePeer on a peer that is not the match's host: only the host changes who takes part.
	ErrNotHost = errors.New("net: only the match's host changes its peers")
	// ErrHostLeave is returned by RemovePeer for the host itself. The host cannot leave a running match; end the match instead.
	ErrHostLeave = errors.New("net: the host cannot leave the match")
	// ErrPeerRejoin is returned by AddPeer for a peer that has left the match: a peer that comes back joins under a new ID, as a relay or server assigns one.
	ErrPeerRejoin = errors.New("net: a peer that left cannot rejoin under the same ID")
)

// MaxJoinStateSize is the largest snapshot, as returned by SimState.Save, that a host sends a joining peer. AddPeer refuses a peer with ErrPayloadTooLarge while the state is larger, and a join whose state has grown past it by the join tick is turned into a leave at that tick.
const MaxJoinStateSize = 16 << 20

// Membership wire messages, sent on ChannelReliableOrdered next to the input and checksum messages.
//
//	change:     syncMsgMembership(1) kind(1) peer(4) tick(4) count(2) input(20)...
//	join state: syncMsgJoinState(1) part(2) parts(2) data
//
// A change of kind memberJoin carries no inputs; one of kind memberLeave carries the leaving peer's inputs for ticks before tick. The parts of a join state concatenate to: tick(4) count(2) {peer(4) from(4) until(4)}... state, the members sorted by ID, until noLeave for a member with no leave scheduled.
const (
	syncMsgMembership byte = 13
	syncMsgJoinState  byte = 14

	memberJoin  byte = 1
	memberLeave byte = 2

	noLeave = ^uint32(0)

	membershipHeader = 1 + 1 + 4 + 4 + 2
	joinPartHeader   = 1 + 2 + 2
	joinBlobHeader   = 4 + 2
	joinMemberSize   = 12

	// joinPartSize is the most state bytes in one join-state part, so a part fits a relay, WebSocket or loopback payload.
	joinPartSize = 48000
	// maxJoinParts bounds the parts a joining peer assembles: the state limit plus room for a full member list.
	maxJoinParts = (MaxJoinStateSize+joinBlobHeader+joinMemberSize*0xFFFF)/joinPartSize + 1
	// maxLeaveInputs is the most inputs a leave change carries, so the message fits MaxRelayPayload.
	maxLeaveInputs = (MaxRelayPayload - membershipHeader) / inputFrameWireSize

	// maxStrayTicks is how many ticks of inputs a peer keeps from one sender it does not yet know as a member, and maxStraySenders how many such senders.
	maxStrayTicks   = 512
	maxStraySenders = 64
	// maxOutbox is how many messages a peer holds for one remote peer the transport cannot reach right now.
	maxOutbox = 4096
	// memberRetention is how many ticks a departed member's record outlives its leave tick.
	memberRetention = 512
)

// matchMember is one peer of a match and the ticks it takes part in: from (inclusive) to until (exclusive).
type matchMember struct {
	id          PeerID
	from, until uint32
}

func (m matchMember) activeAt(tick uint32) bool {
	return tick >= m.from && tick < m.until
}

// memberChange is a change to the match's peers, as the host announces it.
type memberChange struct {
	kind   byte
	peer   PeerID
	tick   uint32
	inputs []InputFrame
}

func encodeMemberChange(c memberChange) []byte {
	buf := make([]byte, membershipHeader, membershipHeader+len(c.inputs)*inputFrameWireSize)
	buf[0] = syncMsgMembership
	buf[1] = c.kind
	binary.BigEndian.PutUint32(buf[2:6], uint32(c.peer))
	binary.BigEndian.PutUint32(buf[6:10], c.tick)
	binary.BigEndian.PutUint16(buf[10:12], uint16(len(c.inputs)))
	for _, in := range c.inputs {
		buf = appendInputFrame(buf, in)
	}
	return buf
}

// decodeMemberChange parses a whole change message. It refuses unknown kinds, a zero peer, a join with inputs, and a leave input of another peer or for a tick at or after the leave.
func decodeMemberChange(b []byte) (memberChange, bool) {
	if len(b) < membershipHeader || b[0] != syncMsgMembership {
		return memberChange{}, false
	}
	c := memberChange{kind: b[1], peer: PeerID(binary.BigEndian.Uint32(b[2:6])), tick: binary.BigEndian.Uint32(b[6:10])}
	n := int(binary.BigEndian.Uint16(b[10:12]))
	if (c.kind != memberJoin && c.kind != memberLeave) || c.peer == 0 || len(b) != membershipHeader+n*inputFrameWireSize || (c.kind == memberJoin && n != 0) {
		return memberChange{}, false
	}
	if n > 0 {
		c.inputs = make([]InputFrame, n)
	}
	src := b[membershipHeader:]
	for i := range c.inputs {
		in, _ := decodeInputFrame(src[i*inputFrameWireSize:])
		if in.Peer != c.peer || in.Tick >= c.tick {
			return memberChange{}, false
		}
		c.inputs[i] = in
	}
	return c, true
}

// joinState is what a host sends a joining peer: the tick it joins at, the members from that tick on, and the simulation's state at the start of the tick.
type joinState struct {
	tick    uint32
	members []matchMember
	state   []byte
}

func encodeJoinState(js joinState) []byte {
	b := make([]byte, joinBlobHeader, joinBlobHeader+joinMemberSize*len(js.members)+len(js.state))
	binary.BigEndian.PutUint32(b[0:4], js.tick)
	binary.BigEndian.PutUint16(b[4:6], uint16(len(js.members)))
	for _, m := range js.members {
		b = binary.BigEndian.AppendUint32(b, uint32(m.id))
		b = binary.BigEndian.AppendUint32(b, m.from)
		b = binary.BigEndian.AppendUint32(b, m.until)
	}
	return append(b, js.state...)
}

// decodeJoinState parses an assembled join state. The members must be sorted, distinct and non-zero, each must take part for at least one tick, and none may have left by the join tick.
func decodeJoinState(b []byte) (joinState, bool) {
	if len(b) < joinBlobHeader {
		return joinState{}, false
	}
	js := joinState{tick: binary.BigEndian.Uint32(b[0:4])}
	n := int(binary.BigEndian.Uint16(b[4:6]))
	if n == 0 || len(b) < joinBlobHeader+n*joinMemberSize {
		return joinState{}, false
	}
	js.members = make([]matchMember, n)
	for i := range js.members {
		o := joinBlobHeader + i*joinMemberSize
		m := matchMember{
			id:    PeerID(binary.BigEndian.Uint32(b[o : o+4])),
			from:  binary.BigEndian.Uint32(b[o+4 : o+8]),
			until: binary.BigEndian.Uint32(b[o+8 : o+12]),
		}
		if m.id == 0 || (i > 0 && m.id <= js.members[i-1].id) || m.from >= m.until || m.until <= js.tick {
			return joinState{}, false
		}
		js.members[i] = m
	}
	js.state = slices.Clone(b[joinBlobHeader+n*joinMemberSize:])
	return js, true
}

// encodeJoinParts splits an encoded join state into join-state messages.
func encodeJoinParts(blob []byte) [][]byte {
	parts := max(1, (len(blob)+joinPartSize-1)/joinPartSize)
	out := make([][]byte, parts)
	for i := range out {
		chunk := blob[min(i*joinPartSize, len(blob)):min((i+1)*joinPartSize, len(blob))]
		msg := make([]byte, joinPartHeader+len(chunk))
		msg[0] = syncMsgJoinState
		binary.BigEndian.PutUint16(msg[1:3], uint16(i))
		binary.BigEndian.PutUint16(msg[3:5], uint16(parts))
		copy(msg[joinPartHeader:], chunk)
		out[i] = msg
	}
	return out
}

// decodeJoinPart parses one join-state message.
func decodeJoinPart(b []byte) (part, parts int, data []byte, ok bool) {
	if len(b) < joinPartHeader || b[0] != syncMsgJoinState {
		return 0, 0, nil, false
	}
	part = int(binary.BigEndian.Uint16(b[1:3]))
	parts = int(binary.BigEndian.Uint16(b[3:5]))
	data = b[joinPartHeader:]
	if parts == 0 || parts > maxJoinParts || part >= parts || len(data) > joinPartSize || (part < parts-1 && len(data) != joinPartSize) {
		return 0, 0, nil, false
	}
	return part, parts, data, true
}

// joinAssembly collects the parts of a join state, which arrive in order on the reliable channel. A part 0 starts over, so a host that sends the state again replaces a partial one.
type joinAssembly struct {
	parts int
	next  int
	buf   []byte
}

// add takes one part and returns the whole state once its last part has arrived.
func (a *joinAssembly) add(b []byte) ([]byte, bool) {
	part, parts, data, ok := decodeJoinPart(b)
	switch {
	case !ok:
		return nil, false
	case part == 0:
		a.parts, a.next, a.buf = parts, 0, make([]byte, 0, min(parts*joinPartSize, 1<<20))
	case a.parts != parts || part != a.next:
		a.parts, a.next, a.buf = 0, 0, nil
		return nil, false
	}
	a.buf = append(a.buf, data...)
	a.next++
	if a.next < a.parts {
		return nil, false
	}
	blob := a.buf
	a.parts, a.next, a.buf = 0, 0, nil
	return blob, true
}

// pendingJoin is a join whose state the host has not sent yet.
type pendingJoin struct {
	peer PeerID
	tick uint32
}

// match is the membership state LockstepSession and RollbackSession share: who takes part in which ticks, the host's pending joins, inputs from peers not known yet, and messages waiting for a peer the transport cannot reach right now.
type match struct {
	transport Transport
	localID   PeerID
	host      PeerID
	joined    bool
	start     uint32        // the first tick the local peer took part in: its join tick, or 0
	members   []matchMember // sorted by ID

	pending  []pendingJoin
	strays   map[PeerID]map[uint32]InputFrame
	assembly joinAssembly
	outbox   map[PeerID][][]byte
}

// newMatch sets up the members: the given peers from tick 0, or none when join is set, in which case the session waits for the host's join state. A zero host is the lowest of peers, leaving out the local peer when joining.
func newMatch(transport Transport, peers []PeerID, host PeerID, join bool) match {
	m := match{transport: transport, joined: !join}
	if transport != nil {
		m.localID = transport.LocalID()
	}
	if !join {
		for _, id := range peers {
			m.members = append(m.members, matchMember{id: id, until: noLeave})
		}
	}
	if host == 0 {
		for _, id := range peers {
			if !join || id != m.localID {
				host = id
				break
			}
		}
	}
	m.host = host
	return m
}

func (m *match) member(id PeerID) *matchMember {
	i, ok := slices.BinarySearchFunc(m.members, id, func(mm matchMember, id PeerID) int {
		return cmp.Compare(mm.id, id)
	})
	if !ok {
		return nil
	}
	return &m.members[i]
}

func (m *match) addMember(id PeerID, from uint32) {
	i, _ := slices.BinarySearchFunc(m.members, id, func(mm matchMember, id PeerID) int {
		return cmp.Compare(mm.id, id)
	})
	m.members = slices.Insert(m.members, i, matchMember{id: id, from: from, until: noLeave})
}

func (m *match) activeAt(id PeerID, tick uint32) bool {
	mm := m.member(id)
	return mm != nil && mm.activeAt(tick)
}

// activeIDs returns the sorted IDs of the members taking part in tick.
func (m *match) activeIDs(tick uint32) []PeerID {
	out := make([]PeerID, 0, len(m.members))
	for _, mm := range m.members {
		if mm.activeAt(tick) {
			out = append(out, mm.id)
		}
	}
	return out
}

func (m *match) isHost() bool {
	return m.joined && m.localID == m.host
}

// accepts reports whether an input sent by its own peer is one the session takes directly: from a member with no leave scheduled, for a tick it takes part in and the local peer does too. A leaving member's inputs before its leave tick come from the host's change instead, so a late one cannot disagree with it.
func (m *match) accepts(in InputFrame) bool {
	mm := m.member(in.Peer)
	return mm != nil && mm.until == noLeave && mm.activeAt(in.Tick) && in.Tick >= m.start
}

// stray keeps an input from a sender that is not a member yet, such as a joining peer whose inputs overtake the host's announcement, or every sender while the local peer waits for its join state.
func (m *match) stray(in InputFrame) {
	if in.Peer == m.localID {
		return
	}
	if m.strays == nil {
		m.strays = make(map[PeerID]map[uint32]InputFrame)
	}
	byTick := m.strays[in.Peer]
	if byTick == nil {
		if len(m.strays) >= maxStraySenders {
			return
		}
		byTick = make(map[uint32]InputFrame)
		m.strays[in.Peer] = byTick
	}
	byTick[in.Tick] = in
	if len(byTick) > maxStrayTicks {
		delete(byTick, slices.Min(mapKeys(byTick)))
	}
}

// takeStrays removes and returns the inputs kept from id, in tick order.
func (m *match) takeStrays(id PeerID) []InputFrame {
	byTick := m.strays[id]
	if byTick == nil {
		return nil
	}
	delete(m.strays, id)
	ticks := mapKeys(byTick)
	slices.Sort(ticks)
	out := make([]InputFrame, len(ticks))
	for i, t := range ticks {
		out[i] = byTick[t]
	}
	return out
}

func mapKeys[V any](m map[uint32]V) []uint32 {
	out := make([]uint32, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// retryable reports whether a failed reliable send may succeed later: the transport does not know the peer yet (a relay's membership update is still on its way), the peer is reconnecting, or the relay's window to it is full.
func retryable(err error) bool {
	return errors.Is(err, ErrUnknownPeer) || errors.Is(err, ErrDisconnected) || errors.Is(err, ErrRelayBacklog)
}

// send sends a reliable message to a remote peer. A message the transport cannot take right now is kept and sent again by flush, and every later message to that peer queues behind it, so the peer still receives them in order: a membership change must reach a peer before the host's input for the change's tick.
func (m *match) send(to PeerID, payload []byte) error {
	if q := m.outbox[to]; len(q) > 0 {
		m.enqueue(to, payload)
		return nil
	}
	err := m.transport.Send(to, ChannelReliableOrdered, payload)
	if err != nil && retryable(err) {
		m.enqueue(to, payload)
		return nil
	}
	return err
}

func (m *match) enqueue(to PeerID, payload []byte) {
	if m.outbox == nil {
		m.outbox = make(map[PeerID][][]byte)
	}
	if len(m.outbox[to]) < maxOutbox {
		m.outbox[to] = append(m.outbox[to], payload)
	}
}

// flush sends the kept messages, in ID order, stopping at a peer's first one that still fails.
func (m *match) flush() {
	if len(m.outbox) == 0 {
		return
	}
	ids := make([]PeerID, 0, len(m.outbox))
	for id := range m.outbox {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		q := m.outbox[id]
		sent := 0
		for _, msg := range q {
			if err := m.transport.Send(id, ChannelReliableOrdered, msg); err != nil && retryable(err) {
				break
			}
			sent++
		}
		if sent == len(q) {
			delete(m.outbox, id)
		} else {
			m.outbox[id] = slices.Delete(q, 0, sent)
		}
	}
}

// sendInputs sends inputs to one peer as bundles of at most 255.
func (m *match) sendInputs(to PeerID, inputs []InputFrame) {
	for len(inputs) > 0 {
		n := min(len(inputs), 255)
		_ = m.send(to, encodeInputBundle(inputs[:n]))
		inputs = inputs[n:]
	}
}

// announce sends a change to every remote member still taking part at tick now or later, and to the subject of a leave, which stops at the leave tick. The subject of a join learns of it from its join state instead.
func (m *match) announce(c memberChange, now uint32) {
	msg := encodeMemberChange(c)
	for _, mm := range m.members {
		if mm.id == m.localID || (mm.id == c.peer && c.kind == memberJoin) {
			continue
		}
		if mm.until > now || mm.id == c.peer {
			_ = m.send(mm.id, msg)
		}
	}
}

// sendJoinState sends a joining peer the state at the start of its join tick and the members from then on.
func (m *match) sendJoinState(p pendingJoin, state []byte) {
	js := joinState{tick: p.tick, state: state}
	for _, mm := range m.members {
		if mm.until > p.tick && mm.from < mm.until {
			js.members = append(js.members, mm)
		}
	}
	for _, part := range encodeJoinParts(encodeJoinState(js)) {
		_ = m.send(p.peer, part)
	}
}

// acceptJoinState checks that a join state admits the local peer at its tick and names the host.
func (m *match) acceptJoinState(js joinState) bool {
	self, host := false, false
	for _, mm := range js.members {
		self = self || (mm.id == m.localID && mm.from == js.tick)
		host = host || (mm.id == m.host && mm.activeAt(js.tick))
	}
	return self && host
}

// pruneMembers forgets members that left more than memberRetention ticks before tick, with the messages kept for them.
func (m *match) pruneMembers(tick uint32) {
	m.members = slices.DeleteFunc(m.members, func(mm matchMember) bool {
		gone := mm.until != noLeave && tick > mm.until && tick-mm.until > memberRetention
		if gone {
			delete(m.outbox, mm.id)
		}
		return gone
	})
}

// takePending removes and returns the pending joins ready by ready, in tick order.
func (m *match) takePending(ready func(pendingJoin) bool) []pendingJoin {
	var out []pendingJoin
	m.pending = slices.DeleteFunc(m.pending, func(p pendingJoin) bool {
		if ready(p) {
			out = append(out, p)
			return true
		}
		return false
	})
	return out
}

// checkAdd validates an AddPeer call. It returns the existing join tick and true when the peer already takes part.
func (m *match) checkAdd(id PeerID) (uint32, bool, error) {
	if !m.isHost() {
		return 0, false, ErrNotHost
	}
	if id == 0 {
		return 0, false, ErrUnknownPeer
	}
	if mm := m.member(id); mm != nil {
		if mm.until != noLeave {
			return 0, false, ErrPeerRejoin
		}
		return mm.from, true, nil
	}
	return 0, false, nil
}

// checkRemove validates a RemovePeer call. It returns the existing leave tick and true when the peer is already leaving.
func (m *match) checkRemove(id PeerID) (*matchMember, uint32, bool, error) {
	if !m.isHost() {
		return nil, 0, false, ErrNotHost
	}
	if id == m.host {
		return nil, 0, false, ErrHostLeave
	}
	mm := m.member(id)
	if mm == nil {
		return nil, 0, false, ErrUnknownPeer
	}
	if mm.until != noLeave {
		return mm, mm.until, true, nil
	}
	return mm, 0, false, nil
}

// leaveInputs builds the inputs a leave change carries for ticks first to leave-1: the ones held, and from declareFrom on a zero input for any tick the host never received. Before declareFrom every peer has already run or confirmed the tick, so only held inputs go along, for peers still behind.
func leaveInputs(peer PeerID, first, declareFrom, leave uint32, held func(uint32) (InputFrame, bool)) []InputFrame {
	var out []InputFrame
	for t := first; t < leave; t++ {
		in, ok := held(t)
		if !ok {
			if t < declareFrom {
				continue
			}
			in = InputFrame{Tick: t, Peer: peer}
		}
		out = append(out, in)
	}
	return out
}
