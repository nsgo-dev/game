package net

import (
	"bytes"
	"encoding/binary"
	"errors"
	"slices"
	"testing"
	"time"
)

func TestMemberChangeRoundTrip(t *testing.T) {
	for _, c := range []memberChange{
		{kind: memberJoin, peer: 7, tick: 40},
		{kind: memberLeave, peer: 3, tick: 12, inputs: []InputFrame{{Tick: 10, Peer: 3, Bits: 5, Axes: [2]int16{-1, 2}}, {Tick: 11, Peer: 3}}},
		{kind: memberLeave, peer: 3, tick: 0},
	} {
		got, ok := decodeMemberChange(encodeMemberChange(c))
		if !ok || got.kind != c.kind || got.peer != c.peer || got.tick != c.tick || !slices.Equal(got.inputs, c.inputs) {
			t.Fatalf("round trip of %+v = %+v, %v", c, got, ok)
		}
	}
}

func TestMemberChangeRejectsMalformedMessages(t *testing.T) {
	leave := encodeMemberChange(memberChange{kind: memberLeave, peer: 3, tick: 12, inputs: []InputFrame{{Tick: 10, Peer: 3}}})
	for name, b := range map[string][]byte{
		"empty":            nil,
		"truncated":        leave[:len(leave)-1],
		"trailing":         append(slices.Clone(leave), 0),
		"unknown kind":     encodeMemberChange(memberChange{kind: 9, peer: 3, tick: 1}),
		"zero peer":        encodeMemberChange(memberChange{kind: memberJoin, tick: 1}),
		"join with inputs": encodeMemberChange(memberChange{kind: memberJoin, peer: 3, tick: 12, inputs: []InputFrame{{Tick: 10, Peer: 3}}}),
		"another's input":  encodeMemberChange(memberChange{kind: memberLeave, peer: 3, tick: 12, inputs: []InputFrame{{Tick: 10, Peer: 4}}}),
		"input at leave":   encodeMemberChange(memberChange{kind: memberLeave, peer: 3, tick: 12, inputs: []InputFrame{{Tick: 12, Peer: 3}}}),
	} {
		if _, ok := decodeMemberChange(b); ok {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestJoinStateRoundTripAcrossParts(t *testing.T) {
	state := make([]byte, 2*joinPartSize+123)
	for i := range state {
		state[i] = byte(i * 31)
	}
	js := joinState{tick: 90, members: []matchMember{{id: 1, until: noLeave}, {id: 4, from: 30, until: 120}, {id: 9, from: 90, until: noLeave}}, inputs: []InputFrame{{Tick: 90, Peer: 4, Bits: 3}, {Tick: 119, Peer: 4, Axes: [2]int16{-5, 6}}}, state: state}
	parts := encodeJoinParts(encodeJoinState(js))
	if len(parts) != 3 {
		t.Fatalf("%d parts, want 3", len(parts))
	}
	var a joinAssembly
	// A partial transfer that starts over is replaced by the new one.
	if _, done := a.add(parts[0]); done {
		t.Fatal("done after one part")
	}
	var blob []byte
	for i, p := range parts {
		var done bool
		blob, done = a.add(p)
		if done != (i == len(parts)-1) {
			t.Fatalf("part %d: done=%v", i, done)
		}
	}
	got, ok := decodeJoinState(blob)
	if !ok || got.tick != js.tick || !slices.Equal(got.members, js.members) || !slices.Equal(got.inputs, js.inputs) || !bytes.Equal(got.state, js.state) {
		t.Fatalf("round trip differs: ok=%v tick=%d members=%v", ok, got.tick, got.members)
	}
	// A part out of order drops the partial state.
	if _, done := a.add(parts[0]); done {
		t.Fatal("done after one part")
	}
	if _, done := a.add(parts[2]); done || a.buf != nil {
		t.Fatal("an out-of-order part was taken")
	}
}

func TestJoinStateRejectsBadMembers(t *testing.T) {
	for name, members := range map[string][]matchMember{
		"none":             nil,
		"unsorted":         {{id: 4, until: noLeave}, {id: 1, until: noLeave}},
		"duplicate":        {{id: 4, until: noLeave}, {id: 4, until: noLeave}},
		"zero ID":          {{id: 0, until: noLeave}},
		"empty span":       {{id: 4, from: 50, until: 50}},
		"left before it":   {{id: 4, until: 10}},
		"left at the tick": {{id: 4, until: 20}},
	} {
		if _, ok := decodeJoinState(encodeJoinState(joinState{tick: 20, members: members})); ok {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestJoinStateRejectsBadInputs(t *testing.T) {
	members := []matchMember{{id: 1, until: noLeave}, {id: 4, from: 5, until: 30}, {id: 6, from: 25, until: 40}}
	for name, inputs := range map[string][]InputFrame{
		"staying member":  {{Tick: 20, Peer: 1}},
		"unknown peer":    {{Tick: 20, Peer: 5}},
		"before the join": {{Tick: 19, Peer: 4}},
		"at the leave":    {{Tick: 30, Peer: 4}},
		"before its from": {{Tick: 24, Peer: 6}},
		"duplicate":       {{Tick: 20, Peer: 4}, {Tick: 20, Peer: 4}},
		"ticks unsorted":  {{Tick: 21, Peer: 4}, {Tick: 20, Peer: 4}},
		"peers unsorted":  {{Tick: 25, Peer: 6}, {Tick: 20, Peer: 4}},
	} {
		if _, ok := decodeJoinState(encodeJoinState(joinState{tick: 20, members: members, inputs: inputs})); ok {
			t.Errorf("%s: accepted", name)
		}
	}
	good := encodeJoinState(joinState{tick: 20, members: members, inputs: []InputFrame{{Tick: 20, Peer: 4}, {Tick: 29, Peer: 4}, {Tick: 25, Peer: 6}}})
	if _, ok := decodeJoinState(good); !ok {
		t.Fatal("valid inputs refused")
	}
	// A count larger than the inputs present, or than maxJoinInputs, is refused.
	at := joinBlobHeader + joinMemberSize*len(members)
	for _, count := range []uint32{4, maxJoinInputs + 1} {
		bad := slices.Clone(good)
		binary.BigEndian.PutUint32(bad[at:], count)
		if _, ok := decodeJoinState(bad); ok {
			t.Errorf("count %d: accepted", count)
		}
	}
}

func FuzzDecodeMemberChange(f *testing.F) {
	f.Add(encodeMemberChange(memberChange{kind: memberJoin, peer: 7, tick: 40}))
	f.Add(encodeMemberChange(memberChange{kind: memberLeave, peer: 3, tick: 12, inputs: []InputFrame{{Tick: 10, Peer: 3, Bits: 5}}}))
	f.Add([]byte{syncMsgMembership})
	f.Fuzz(func(t *testing.T, b []byte) {
		c, ok := decodeMemberChange(b)
		if !ok {
			return
		}
		if again := encodeMemberChange(c); !bytes.Equal(again, b) {
			t.Fatalf("re-encoded change differs:\n%x\n%x", again, b)
		}
	})
}

func FuzzJoinAssembly(f *testing.F) {
	small := encodeJoinParts(encodeJoinState(joinState{tick: 5, members: []matchMember{{id: 1, until: noLeave}, {id: 2, from: 5, until: noLeave}, {id: 3, until: 7}}, inputs: []InputFrame{{Tick: 5, Peer: 3, Bits: 1}, {Tick: 6, Peer: 3}}, state: []byte("state")}))
	f.Add(small[0], []byte(nil))
	big := encodeJoinParts(encodeJoinState(joinState{tick: 5, members: []matchMember{{id: 1, until: noLeave}}, state: make([]byte, joinPartSize)}))
	f.Add(big[0], big[1])
	f.Add([]byte{syncMsgJoinState, 0, 0, 0, 1}, []byte{syncMsgJoinState, 0, 1, 0, 2})
	f.Fuzz(func(t *testing.T, first, second []byte) {
		var a joinAssembly
		for _, part := range [][]byte{first, second} {
			blob, done := a.add(part)
			if !done {
				continue
			}
			if len(blob) > maxJoinParts*joinPartSize {
				t.Fatalf("assembled %d bytes", len(blob))
			}
			js, ok := decodeJoinState(blob)
			if !ok {
				continue
			}
			if again := encodeJoinState(js); !bytes.Equal(again, blob) {
				t.Fatalf("re-encoded join state differs:\n%x\n%x", again, blob)
			}
		}
	})
}

// memberSim is a deterministic simulation whose state depends on every input and on their order.
type memberSim struct {
	tick uint32
	acc  uint64
}

func (s *memberSim) Step(inputs []InputFrame) {
	for _, in := range inputs {
		s.acc = (s.acc ^ uint64(in.Peer)<<40 ^ in.Bits) * 1099511628211
	}
	s.tick++
}

func (s *memberSim) Save() []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint32(b[0:4], s.tick)
	binary.BigEndian.PutUint64(b[4:12], s.acc)
	return b
}

func (s *memberSim) Load(b []byte) error {
	if len(b) != 12 {
		return ErrInvalidPacket
	}
	s.tick = binary.BigEndian.Uint32(b[0:4])
	s.acc = binary.BigEndian.Uint64(b[4:12])
	return nil
}

func (s *memberSim) Checksum() uint64 { return ChecksumBytes(s.Save()) }

// memberTransport hands a session scripted packets, records what it sends and reports peers as its Peers.
type memberTransport struct {
	id    PeerID
	inbox []Packet
	sent  []Packet
	peers []PeerID
}

func (s *memberTransport) Send(to PeerID, ch Channel, payload []byte) error {
	s.sent = append(s.sent, Packet{From: to, Channel: ch, Payload: slices.Clone(payload)})
	return nil
}

func (s *memberTransport) Recv() (Packet, bool) {
	if len(s.inbox) == 0 {
		return Packet{}, false
	}
	p := s.inbox[0]
	s.inbox = s.inbox[1:]
	return p, true
}

func (s *memberTransport) LocalID() PeerID { return s.id }
func (s *memberTransport) Peers() []PeerID { return s.peers }
func (s *memberTransport) Close() error    { return nil }

// FuzzSessionsTakeHostMessages feeds arbitrary messages from the host to a running and a joining session of both kinds, which must neither panic nor take part in a tick their members do not cover.
func FuzzSessionsTakeHostMessages(f *testing.F) {
	f.Add(encodeMemberChange(memberChange{kind: memberLeave, peer: 2, tick: 3, inputs: []InputFrame{{Tick: 1, Peer: 2, Bits: 1}}}), encodeJoinParts(encodeJoinState(joinState{tick: 4, members: []matchMember{{id: 1, until: noLeave}, {id: 2, until: noLeave}, {id: 3, from: 4, until: noLeave}}, state: make([]byte, 8)}))[0])
	f.Add(encodeMemberChange(memberChange{kind: memberJoin, peer: 9, tick: 0}), []byte{syncMsgJoinState, 0, 0, 0, 1, 1})
	f.Fuzz(func(t *testing.T, a, b []byte) {
		for _, join := range []bool{false, true} {
			local := PeerID(2)
			if join {
				local = 3
			}
			pkts := []Packet{{From: 1, Payload: a}, {From: 1, Payload: b}, {From: 2, Payload: encodeSingleInput(InputFrame{Peer: 2})}}
			lt := &memberTransport{id: local, inbox: slices.Clone(pkts)}
			ls := NewLockstepSession(LockstepConfig{Transport: lt, Sim: &memberSim{}, Peers: []PeerID{1, 2}, Join: join, Host: 1})
			ls.Step()
			if ls.Active() && !slices.Contains(ls.Members(), local) {
				t.Fatalf("lockstep active outside its members %v", ls.Members())
			}
			rt := &memberTransport{id: local, inbox: slices.Clone(pkts)}
			rs := NewRollbackSession(RollbackConfig{Transport: rt, Sim: &memberSim{}, Peers: []PeerID{1, 2}, InputDelay: 1, Join: join, Host: 1})
			_ = rs.Step(InputFrame{Bits: 1})
			_ = rs.Step(InputFrame{Bits: 2})
			if rs.Active() && !slices.Contains(rs.Members(), local) {
				t.Fatalf("rollback active outside its members %v", rs.Members())
			}
		}
	})
}

// TestLockstepKeepsInputsThatOvertakeTheJoin delivers a newcomer's input to a peer before the host's announcement of the join: the peer keeps it aside and uses it once the announcement arrives.
func TestLockstepKeepsInputsThatOvertakeTheJoin(t *testing.T) {
	hub := NewLoopbackHub(LoopbackConfig{Latency: 10 * time.Millisecond})
	s1 := NewLockstepSession(LockstepConfig{Transport: hub.AddPeer(1), Sim: &memberSim{}, Peers: []PeerID{1, 2}})
	s2 := NewLockstepSession(LockstepConfig{Transport: hub.AddPeer(2), Sim: &memberSim{}, Peers: []PeerID{1, 2}})
	t3 := hub.AddPeer(3)
	if err := t3.Send(2, ChannelReliableOrdered, encodeSingleInput(InputFrame{Tick: 0, Peer: 3, Bits: 5})); err != nil {
		t.Fatal(err)
	}
	hub.Advance(5 * time.Millisecond)
	if tick, err := s1.AddPeer(3); err != nil || tick != 0 {
		t.Fatalf("AddPeer = %d, %v", tick, err)
	}
	hub.Advance(5 * time.Millisecond)
	s2.Poll()
	if s2.member(3) != nil || len(s2.strays[3]) != 1 {
		t.Fatalf("before the announcement: member %v, strays %v", s2.member(3), s2.strays)
	}
	hub.Advance(5 * time.Millisecond)
	s2.Poll()
	if in, ok := s2.inputs[0][3]; !ok || in.Bits != 5 || len(s2.strays) != 0 {
		t.Fatalf("after the announcement: input %+v (%v), strays %v", in, ok, s2.strays)
	}
}

// TestHostRetriesAJoinStateTheTransportCannotDeliverYet admits a peer the host's transport does not know yet, as when a relay's membership update is still on its way: the join state waits in the outbox and goes out, with what was sent after it, once the transport knows the peer.
func TestHostRetriesAJoinStateTheTransportCannotDeliverYet(t *testing.T) {
	hub := NewLoopbackHub(LoopbackConfig{Latency: 10 * time.Millisecond})
	simA := &memberSim{}
	host := NewLockstepSession(LockstepConfig{Transport: hub.AddPeer(1), Sim: simA, Peers: []PeerID{1}})
	if _, err := host.AddPeer(3); err != nil {
		t.Fatal(err)
	}
	if err := host.SubmitInput(InputFrame{Tick: 0, Bits: 1}); err != nil {
		t.Fatalf("submit while the peer is unreachable: %v", err)
	}
	if n := len(host.outbox[3]); n != 2 {
		t.Fatalf("outbox holds %d messages, want the state and the input", n)
	}
	simC := &memberSim{}
	joiner := NewLockstepSession(LockstepConfig{Transport: hub.AddPeer(3), Sim: simC, Host: 1, Join: true})
	host.Poll()
	if len(host.outbox) != 0 {
		t.Fatalf("outbox not flushed: %v", host.outbox)
	}
	hub.Advance(10 * time.Millisecond)
	if joiner.Step() || !joiner.Active() || joiner.Tick() != 0 {
		t.Fatalf("joiner: active %v at %d, want active at 0 waiting for its own input", joiner.Active(), joiner.Tick())
	}
	if err := joiner.SubmitInput(InputFrame{Tick: 0, Bits: 2}); err != nil {
		t.Fatal(err)
	}
	hub.Advance(10 * time.Millisecond)
	if !host.Step() || !joiner.Step() || simA.Checksum() != simC.Checksum() {
		t.Fatalf("tick 0 not run alike: host %+v joiner %+v", simA, simC)
	}
}

// sizedSim is a simulation whose saved state is as large as size says.
type sizedSim struct {
	memberSim
	size int
}

func (s *sizedSim) Save() []byte {
	b := make([]byte, s.size)
	copy(b, s.memberSim.Save())
	return b
}

func (s *sizedSim) Load(b []byte) error {
	if len(b) < 12 {
		return ErrInvalidPacket
	}
	s.size = len(b)
	return s.memberSim.Load(b[:12])
}

// TestJoinStateLimit refuses a join while the state is over MaxJoinStateSize, and turns a join whose state has grown past it by the join tick into a leave, so the other peers stop waiting for the newcomer.
func TestJoinStateLimit(t *testing.T) {
	hub := NewLoopbackHub(LoopbackConfig{Latency: 10 * time.Millisecond})
	sim := &sizedSim{size: MaxJoinStateSize + 1}
	host := NewLockstepSession(LockstepConfig{Transport: hub.AddPeer(1), Sim: sim, Peers: []PeerID{1, 2}})
	other := NewLockstepSession(LockstepConfig{Transport: hub.AddPeer(2), Sim: &memberSim{}, Peers: []PeerID{1, 2}})
	if _, err := host.AddPeer(3); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("AddPeer with an oversized state = %v, want ErrPayloadTooLarge", err)
	}
	sim.size = 12
	if err := host.SubmitInput(InputFrame{Tick: 0}); err != nil {
		t.Fatal(err)
	}
	join, err := host.AddPeer(3)
	if err != nil || join != 1 {
		t.Fatalf("AddPeer = %d, %v", join, err)
	}
	sim.size = MaxJoinStateSize + 1
	_ = other.SubmitInput(InputFrame{Tick: 0})
	hub.Advance(10 * time.Millisecond)
	if !host.Step() || !other.Step() {
		t.Fatal("tick 0 did not run")
	}
	if mm := host.member(3); mm == nil || mm.until != join {
		t.Fatalf("the oversized join was not turned into a leave at %d: %+v", join, mm)
	}
	for tick := uint32(1); tick < 4; tick++ {
		_ = host.SubmitInput(InputFrame{Tick: tick})
		_ = other.SubmitInput(InputFrame{Tick: tick})
		hub.Advance(10 * time.Millisecond)
		other.Step()
		host.Step()
	}
	if host.Tick() != 4 || other.Tick() != 4 || !slices.Equal(other.Members(), []PeerID{1, 2}) {
		t.Fatalf("the match stalled on the cancelled join: host %d, other %d, members %v", host.Tick(), other.Tick(), other.Members())
	}
}

// TestMembersAreForgottenAfterTheirRetention drops a departed member's record, and the messages kept for it, memberRetention ticks after it left, but still refuses its ID and reports its leave tick.
func TestMembersAreForgottenAfterTheirRetention(t *testing.T) {
	m := newMatch(&memberTransport{id: 1}, []PeerID{1, 2}, 0, false)
	m.member(2).until = 10
	_ = m.enqueue(2, []byte{1})
	m.pruneMembers(10 + memberRetention)
	if m.member(2) == nil {
		t.Fatal("forgotten too early")
	}
	m.pruneMembers(11 + memberRetention)
	if m.member(2) != nil || m.outbox[2] != nil {
		t.Fatal("not forgotten")
	}
	if _, _, err := m.checkAdd(2); !errors.Is(err, ErrPeerRejoin) {
		t.Fatalf("adding a forgotten peer = %v, want ErrPeerRejoin", err)
	}
	if _, tick, done, err := m.checkRemove(2); err != nil || !done || tick != 10 {
		t.Fatalf("removing a forgotten peer = %d, %v, %v; want its leave tick 10", tick, done, err)
	}
	m.stray(InputFrame{Tick: 600, Peer: 2})
	if len(m.strays) != 0 {
		t.Fatal("kept an input from a forgotten peer")
	}
}

// TestSessionsRefuseADepartedIDAfterTheRetention runs a lockstep host past memberRetention after a peer left: AddPeer of that ID still returns ErrPeerRejoin and RemovePeer its leave tick.
func TestSessionsRefuseADepartedIDAfterTheRetention(t *testing.T) {
	host := NewLockstepSession(LockstepConfig{Transport: &memberTransport{id: 1}, Sim: &memberSim{}, Peers: []PeerID{1, 2}})
	leave, err := host.RemovePeer(2)
	if err != nil || leave != 0 {
		t.Fatalf("RemovePeer = %d, %v", leave, err)
	}
	for tick := range uint32(memberRetention + 2) {
		if err := host.SubmitInput(InputFrame{Tick: tick}); err != nil {
			t.Fatal(err)
		}
		if !host.Step() {
			t.Fatalf("tick %d did not run", tick)
		}
	}
	if host.member(2) != nil {
		t.Fatal("the departed member is still kept")
	}
	if _, err := host.AddPeer(2); !errors.Is(err, ErrPeerRejoin) {
		t.Fatalf("AddPeer of a departed ID = %v, want ErrPeerRejoin", err)
	}
	if tick, err := host.RemovePeer(2); err != nil || tick != leave {
		t.Fatalf("RemovePeer of a departed ID = %d, %v; want %d", tick, err, leave)
	}
}

// downTransport is a memberTransport that cannot reach peer 2 at all.
type downTransport struct{ memberTransport }

func (d *downTransport) Send(to PeerID, ch Channel, payload []byte) error {
	if to == 2 {
		return ErrDisconnected
	}
	return d.memberTransport.Send(to, ch, payload)
}

// TestOutboxOverflowIsReported fills the messages kept for an unreachable peer: the next SubmitInput (lockstep) or Step (rollback) reports ErrSessionBacklog once, and later messages to that peer are dropped instead of leaving a gap in the middle of its stream.
func TestOutboxOverflowIsReported(t *testing.T) {
	ls := NewLockstepSession(LockstepConfig{Transport: &downTransport{memberTransport{id: 1}}, Sim: &memberSim{}, Peers: []PeerID{1, 2}})
	for range maxOutbox - 1 {
		if err := ls.send(2, []byte{1}); err != nil {
			t.Fatal(err)
		}
	}
	if err := ls.SubmitInput(InputFrame{Tick: 0}); err != nil {
		t.Fatalf("submit that fills the outbox: %v", err)
	}
	if err := ls.SubmitInput(InputFrame{Tick: 1}); !errors.Is(err, ErrSessionBacklog) {
		t.Fatalf("submit past the outbox = %v, want ErrSessionBacklog", err)
	}
	if err := ls.SubmitInput(InputFrame{Tick: 2}); err != nil {
		t.Fatalf("the overflow was reported twice: %v", err)
	}
	if n := len(ls.outbox[2]); n != maxOutbox || !ls.lost[2] {
		t.Fatalf("outbox holds %d, lost %v", n, ls.lost[2])
	}

	rs := NewRollbackSession(RollbackConfig{Transport: &downTransport{memberTransport{id: 1}}, Sim: &memberSim{}, Peers: []PeerID{1, 2}, MaxRollback: 8})
	for range maxOutbox {
		_ = rs.send(2, []byte{1})
	}
	if err := rs.Step(InputFrame{}); !errors.Is(err, ErrSessionBacklog) || rs.Tick() != 1 {
		t.Fatalf("step past the outbox = %v at tick %d, want ErrSessionBacklog after running the tick", err, rs.Tick())
	}
	if err := rs.Step(InputFrame{}); err != nil {
		t.Fatalf("the overflow was reported twice: %v", err)
	}
}

// TestJoiningSessionFindsItsHost resolves the host of a joining session created with no host and no peers, or naming itself, from the transport's peers once they are known, as over a relay.
func TestJoiningSessionFindsItsHost(t *testing.T) {
	for _, host := range []PeerID{0, 3} {
		tr := &memberTransport{id: 3}
		s := NewRollbackSession(RollbackConfig{Transport: tr, Sim: &memberSim{}, Host: host, Join: true})
		if s.host != 0 {
			t.Fatalf("host %d before any peer is known", s.host)
		}
		_ = s.Poll()
		tr.peers = []PeerID{5, 3, 1}
		js := encodeJoinParts(encodeJoinState(joinState{tick: 4, members: []matchMember{{id: 1, until: noLeave}, {id: 3, from: 4, until: noLeave}}, state: make([]byte, 12)}))
		tr.inbox = append(tr.inbox, Packet{From: 1, Payload: js[0]})
		if err := s.Poll(); err != nil || s.host != 1 || !s.Active() || s.Tick() != 4 {
			t.Fatalf("configured host %d: resolved %d, active %v at %d (%v)", host, s.host, s.Active(), s.Tick(), err)
		}
	}
}

// TestRollbackJoinThatCannotBeSentBecomesALeave drops a join whose state cannot be sent while the host runs far ahead of its confirmed ticks, so far that the usual leave would carry more inputs than fit one message: the join still becomes a leave, right after the newcomer's preconfirmed ticks, instead of being lost.
func TestRollbackJoinThatCannotBeSentBecomesALeave(t *testing.T) {
	tr := &memberTransport{id: 1}
	sim := &growingSim{bigAt: 6}
	host := NewRollbackSession(RollbackConfig{Transport: tr, Sim: sim, Peers: []PeerID{1, 2}, InputDelay: 1, MaxRollback: 1 << 14})
	// Peer 2 sends nothing, so nothing past tick 0 is confirmed.
	for range 5 {
		if err := host.Step(InputFrame{}); err != nil {
			t.Fatal(err)
		}
	}
	join, err := host.AddPeer(3)
	if err != nil || join != 6 {
		t.Fatalf("AddPeer = %d, %v; want 6", join, err)
	}
	for range maxLeaveInputs + 10 {
		if err := host.Step(InputFrame{}); err != nil {
			t.Fatal(err)
		}
	}
	if host.ConfirmedTick() != 0 || len(host.pending) != 1 {
		t.Fatalf("confirmed %d with %d joins pending, want 0 and 1", host.ConfirmedTick(), len(host.pending))
	}
	// Peer 2's inputs up to the join tick arrive; the state saved at the join tick is too large to send.
	var inputs []InputFrame
	for tick := uint32(1); tick < join; tick++ {
		inputs = append(inputs, InputFrame{Tick: tick, Peer: 2})
	}
	tr.inbox = append(tr.inbox, Packet{From: 2, Payload: encodeInputBundle(inputs)})
	tr.sent = nil
	if err := host.Poll(); err != nil {
		t.Fatal(err)
	}
	// The host is so far past the leave tick that it has already forgotten the member.
	if tick, gone := host.departed[3]; !gone || tick != join+1 || len(host.pending) != 0 {
		t.Fatalf("the join was not turned into a leave at %d: left at %d (%v), %d pending", join+1, tick, gone, len(host.pending))
	}
	announced := false
	for _, pkt := range tr.sent {
		if c, ok := decodeMemberChange(pkt.Payload); ok && pkt.From == 2 && c.kind == memberLeave && c.peer == 3 && c.tick == join+1 {
			announced = true
		}
	}
	if !announced {
		t.Fatal("the leave was not announced to peer 2")
	}
}

// growingSim is a memberSim whose state saved at tick bigAt is larger than MaxJoinStateSize.
type growingSim struct {
	memberSim
	bigAt uint32
}

func (s *growingSim) Save() []byte {
	b := s.memberSim.Save()
	if s.tick == s.bigAt {
		b = append(b, make([]byte, MaxJoinStateSize)...)
	}
	return b
}

func (s *growingSim) Load(b []byte) error {
	if len(b) < 12 {
		return ErrInvalidPacket
	}
	return s.memberSim.Load(b[:12])
}

// TestStraysAreBounded keeps at most maxStrayTicks ticks per unknown sender, dropping the oldest, and at most maxStraySenders senders.
func TestStraysAreBounded(t *testing.T) {
	m := newMatch(&memberTransport{id: 1}, []PeerID{1}, 0, false)
	for tick := range uint32(maxStrayTicks + 10) {
		m.stray(InputFrame{Tick: tick, Peer: 2})
	}
	got := m.takeStrays(2)
	if len(got) != maxStrayTicks || got[0].Tick != 10 {
		t.Fatalf("kept %d ticks from %d", len(got), got[0].Tick)
	}
	for id := range PeerID(maxStraySenders + 5) {
		m.stray(InputFrame{Peer: id + 10})
	}
	if len(m.strays) != maxStraySenders {
		t.Fatalf("%d senders kept", len(m.strays))
	}
}

// BenchmarkJoinState measures encoding a 1 MiB join state into parts and assembling it again.
func BenchmarkJoinState(b *testing.B) {
	state := make([]byte, 1<<20)
	binary.BigEndian.PutUint64(state, 42)
	js := joinState{tick: 600, members: []matchMember{{id: 1, until: noLeave}, {id: 2, until: noLeave}, {id: 3, from: 600, until: noLeave}}, state: state}
	b.SetBytes(int64(len(state)))
	b.ReportAllocs()
	for b.Loop() {
		var a joinAssembly
		for _, p := range encodeJoinParts(encodeJoinState(js)) {
			if blob, done := a.add(p); done {
				if _, ok := decodeJoinState(blob); !ok {
					b.Fatal("decode failed")
				}
			}
		}
	}
}
