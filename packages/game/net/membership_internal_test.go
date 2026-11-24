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
	js := joinState{tick: 90, members: []matchMember{{id: 1, until: noLeave}, {id: 4, from: 30, until: 120}, {id: 9, from: 90, until: noLeave}}, state: state}
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
	if !ok || got.tick != js.tick || !slices.Equal(got.members, js.members) || !bytes.Equal(got.state, js.state) {
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
	small := encodeJoinParts(encodeJoinState(joinState{tick: 5, members: []matchMember{{id: 1, until: noLeave}, {id: 2, from: 5, until: noLeave}}, state: []byte("state")}))
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

// scriptTransport hands a session scripted packets and records what it sends.
type scriptTransport struct {
	id    PeerID
	inbox []Packet
	sent  []Packet
}

func (s *scriptTransport) Send(to PeerID, ch Channel, payload []byte) error {
	s.sent = append(s.sent, Packet{From: to, Channel: ch, Payload: slices.Clone(payload)})
	return nil
}

func (s *scriptTransport) Recv() (Packet, bool) {
	if len(s.inbox) == 0 {
		return Packet{}, false
	}
	p := s.inbox[0]
	s.inbox = s.inbox[1:]
	return p, true
}

func (s *scriptTransport) LocalID() PeerID { return s.id }
func (s *scriptTransport) Peers() []PeerID { return nil }
func (s *scriptTransport) Close() error    { return nil }

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
			lt := &scriptTransport{id: local, inbox: slices.Clone(pkts)}
			ls := NewLockstepSession(LockstepConfig{Transport: lt, Sim: &memberSim{}, Peers: []PeerID{1, 2}, Join: join, Host: 1})
			ls.Step()
			if ls.Active() && !slices.Contains(ls.Members(), local) {
				t.Fatalf("lockstep active outside its members %v", ls.Members())
			}
			rt := &scriptTransport{id: local, inbox: slices.Clone(pkts)}
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

// TestMembersAreForgottenAfterTheirRetention drops a departed member's record, and the messages kept for it, memberRetention ticks after it left.
func TestMembersAreForgottenAfterTheirRetention(t *testing.T) {
	m := newMatch(&scriptTransport{id: 1}, []PeerID{1, 2}, 0, false)
	m.member(2).until = 10
	m.enqueue(2, []byte{1})
	m.pruneMembers(10 + memberRetention)
	if m.member(2) == nil {
		t.Fatal("forgotten too early")
	}
	m.pruneMembers(11 + memberRetention)
	if m.member(2) != nil || m.outbox[2] != nil {
		t.Fatal("not forgotten")
	}
}

// TestStraysAreBounded keeps at most maxStrayTicks ticks per unknown sender, dropping the oldest, and at most maxStraySenders senders.
func TestStraysAreBounded(t *testing.T) {
	m := newMatch(&scriptTransport{id: 1}, []PeerID{1}, 0, false)
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
