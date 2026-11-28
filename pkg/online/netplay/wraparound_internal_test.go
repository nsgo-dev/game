package netplay

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"slices"
	"testing"
	"time"
)

// wrapSim is a deterministic simulation whose checksum depends on every input,
// its peer, its tick and its order, and which tracks the checksum at each tick.
type wrapSim struct {
	tick uint32
	acc  uint64
	sums map[uint32]uint64
}

func newWrapSim(start uint32) *wrapSim {
	s := &wrapSim{tick: start, sums: make(map[uint32]uint64)}
	s.sums[start] = s.Checksum()
	return s
}

func (s *wrapSim) Step(inputs []InputFrame) {
	for _, in := range inputs {
		s.acc = (s.acc ^ uint64(in.Peer)<<40 ^ uint64(in.Tick)<<20 ^ in.Bits) * 1099511628211
	}
	s.tick++
	if s.sums == nil {
		s.sums = make(map[uint32]uint64)
	}
	s.sums[s.tick] = s.Checksum()
}

func (s *wrapSim) Save() []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint32(b[0:4], s.tick)
	binary.BigEndian.PutUint64(b[4:12], s.acc)
	return b
}

func (s *wrapSim) Load(b []byte) error {
	if len(b) != 12 {
		return ErrInvalidPacket
	}
	s.tick = binary.BigEndian.Uint32(b[0:4])
	s.acc = binary.BigEndian.Uint64(b[4:12])
	if s.sums == nil {
		s.sums = make(map[uint32]uint64)
	}
	s.sums[s.tick] = s.Checksum()
	return nil
}

func (s *wrapSim) Checksum() uint64 { return ChecksumBytes(s.Save()) }

func wrapInput(peer PeerID, tick uint32) InputFrame {
	return InputFrame{Tick: tick, Peer: peer, Bits: uint64(tick*7+uint32(peer)*13)%23 + 1}
}

// setLockstepStart positions a newly created LockstepSession at start so tests
// can exercise uint32 wraparound without running 2^32 ticks.
func setLockstepStart(s *LockstepSession, start uint32) {
	s.start = start
	s.tick = start
	s.nextInput = start
	for i := range s.members {
		s.members[i].from = start
	}
}

// setRollbackStart positions a newly created RollbackSession at start so tests
// can exercise uint32 wraparound without running 2^32 ticks.
func setRollbackStart(s *RollbackSession, start uint32) {
	s.start = start
	s.currentTick = start
	s.hasConfirmed = false
	s.confirmedTick = 0
	clear(s.confirmedInputs)
	for i := range s.members {
		s.members[i].from = start
		s.preconfirm(s.members[i])
	}
}

type wrapSpan struct {
	from, until uint32
	hasLeave    bool
	zero        func(tick uint32) bool
}

func replayWrap(start, end uint32, peers map[PeerID]wrapSpan) map[uint32]uint64 {
	ids := make([]PeerID, 0, len(peers))
	for id := range peers {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	ref := newWrapSim(start)
	for t := start; tickBefore(t, end); t++ {
		var inputs []InputFrame
		for _, id := range ids {
			sp := peers[id]
			if tickBefore(t, sp.from) || (sp.hasLeave && !tickBefore(t, sp.until)) {
				continue
			}
			in := wrapInput(id, t)
			if sp.zero != nil && sp.zero(t) {
				in = InputFrame{Tick: t, Peer: id}
			}
			inputs = append(inputs, in)
		}
		ref.Step(inputs)
	}
	return ref.sums
}

// TestLockstepWraparoundWithJoinAndLeave starts a lockstep match a few ticks
// before math.MaxUint32 and runs across the 2^32 wrap with a mid-match join
// and leave whose agreed ticks cross the wrap, plus a leave right on 0xFFFFFFFF.
func TestLockstepWraparoundWithJoinAndLeave(t *testing.T) {
	const start uint32 = math.MaxUint32 - 3 // 0xFFFFFFFC
	const latency = 20 * time.Millisecond

	hub := NewLoopbackHub(LoopbackConfig{Latency: latency})
	sims := map[PeerID]*wrapSim{1: newWrapSim(start), 2: newWrapSim(start), 3: {}}
	sessions := map[PeerID]*LockstepSession{}
	for _, id := range []PeerID{1, 2} {
		s := NewLockstepSession(LockstepConfig{
			Transport:        hub.AddPeer(id),
			Sim:              sims[id],
			Peers:            []PeerID{1, 2},
			ChecksumInterval: 4,
		})
		setLockstepStart(s, start)
		sessions[id] = s
	}

	submitted := map[PeerID]uint32{1: start, 2: start}
	round := func(skip ...PeerID) {
		for _, id := range []PeerID{1, 2, 3} {
			s := sessions[id]
			if s == nil || !s.Active() || slices.Contains(skip, id) {
				continue
			}
			if tickAtOrBefore(submitted[id], s.Tick()) {
				if err := s.SubmitInput(wrapInput(id, s.Tick())); err != nil {
					t.Fatalf("peer %d submit at %#x: %v", id, s.Tick(), err)
				}
				submitted[id] = s.Tick() + 1
			}
		}
		hub.Advance(latency)
		for _, id := range []PeerID{1, 2, 3} {
			if s := sessions[id]; s != nil {
				s.Step()
			}
		}
	}

	// Advance 2 ticks: 0xFFFFFFFC, 0xFFFFFFFD -> now at 0xFFFFFFFE.
	for range 2 {
		round()
	}
	if sessions[1].Tick() != 0xFFFFFFFE || sessions[2].Tick() != 0xFFFFFFFE {
		t.Fatalf("pre-wrap ticks = %#x, %#x; want 0xfffffffe", sessions[1].Tick(), sessions[2].Tick())
	}

	// Host submits 3 ticks ahead across the wrap (0xFFFFFFFE, 0xFFFFFFFF, 0),
	// then admits peer 3 so its agreed join tick is 1 (crossing the wrap).
	for _, tk := range []uint32{0xFFFFFFFE, 0xFFFFFFFF, 0} {
		if err := sessions[1].SubmitInput(wrapInput(1, tk)); err != nil {
			t.Fatalf("host submit ahead %#x: %v", tk, err)
		}
		submitted[1] = tk + 1
	}
	sessions[3] = NewLockstepSession(LockstepConfig{
		Transport:        hub.AddPeer(3),
		Sim:              sims[3],
		Host:             1,
		Join:             true,
		ChecksumInterval: 4,
	})
	joinTick, err := sessions[1].AddPeer(3)
	if err != nil || joinTick != 1 {
		t.Fatalf("AddPeer = %#x, %v; want join tick 1 across wrap", joinTick, err)
	}
	submitted[3] = joinTick

	// Step across 0xFFFFFFFE -> 0xFFFFFFFF -> 0 -> 1 -> 15.
	for range 40 {
		if sessions[1].Tick() == 15 && sessions[3].Tick() == 15 {
			break
		}
		round()
	}
	if !sessions[3].Active() || sessions[1].Tick() != 15 || sessions[2].Tick() != 15 || sessions[3].Tick() != 15 {
		t.Fatalf("post-join ticks: p1=%#x p2=%#x p3=%#x active=%v",
			sessions[1].Tick(), sessions[2].Tick(), sessions[3].Tick(), sessions[3].Active())
	}

	// Remove peer 2 with host submitting ahead to tick 18 -> leave tick 18.
	for _, tk := range []uint32{15, 16, 17} {
		for _, id := range []PeerID{1, 3} {
			if err := sessions[id].SubmitInput(wrapInput(id, tk)); err != nil {
				t.Fatalf("peer %d submit ahead %d: %v", id, tk, err)
			}
			submitted[id] = tk + 1
		}
	}
	leaveTick, err := sessions[1].RemovePeer(2)
	if err != nil || leaveTick != 18 {
		t.Fatalf("RemovePeer = %d, %v; want 18", leaveTick, err)
	}

	// Run past both lockstepChecksumHistory (256) and memberRetention (512)
	// so checksum and member pruning both run across the wrap boundary.
	const end uint32 = 540
	for tickBefore(sessions[1].Tick(), end) || tickBefore(sessions[3].Tick(), end) {
		round(2)
	}
	hub.Advance(2 * latency)
	sessions[1].Poll()
	sessions[3].Poll()

	if sessions[2].Active() || sessions[2].Tick() != leaveTick {
		t.Fatalf("removed peer 2: active=%v tick=%d, want inactive at %d", sessions[2].Active(), sessions[2].Tick(), leaveTick)
	}
	if sessions[1].member(2) != nil {
		t.Fatal("departed peer 2 was not pruned after memberRetention across wrap")
	}

	ref := replayWrap(start, end, map[PeerID]wrapSpan{
		1: {from: start},
		2: {from: start, until: leaveTick, hasLeave: true, zero: func(tk uint32) bool { return tickAtOrAfter(tk, 15) }},
		3: {from: joinTick},
	})
	if sims[1].Checksum() != ref[end] || sims[3].Checksum() != ref[end] {
		t.Fatalf("final checksums diverged across wrap: p1=%x p3=%x ref=%x", sims[1].Checksum(), sims[3].Checksum(), ref[end])
	}
	if sims[2].Checksum() != ref[leaveTick] {
		t.Fatalf("peer 2 checksum at leave = %x, want %x", sims[2].Checksum(), ref[leaveTick])
	}
	for id, s := range sessions {
		if d := s.Desyncs(); len(d) != 0 {
			t.Fatalf("peer %d reported desyncs across wrap: %+v", id, d)
		}
	}
}

// TestLockstepLeaveAtMaxUint32 verifies that a peer leaving right on tick
// 0xFFFFFFFF (which used to collide with the noLeave sentinel) leaves cleanly
// and can be encoded inside a joinState for a newcomer joining earlier.
func TestLockstepLeaveAtMaxUint32(t *testing.T) {
	const start uint32 = math.MaxUint32 - 3 // 0xFFFFFFFC
	const latency = 10 * time.Millisecond

	hub := NewLoopbackHub(LoopbackConfig{Latency: latency})
	sims := map[PeerID]*wrapSim{1: newWrapSim(start), 2: newWrapSim(start), 3: {}}
	sessions := map[PeerID]*LockstepSession{}
	for _, id := range []PeerID{1, 2} {
		s := NewLockstepSession(LockstepConfig{
			Transport: hub.AddPeer(id),
			Sim:       sims[id],
			Peers:     []PeerID{1, 2},
		})
		setLockstepStart(s, start)
		sessions[id] = s
	}

	// Host submits 0xFFFFFFFC, 0xFFFFFFFD, 0xFFFFFFFE; peer 3 joins at 0xFFFFFFFE,
	// then peer 2 leaves at 0xFFFFFFFF before peer 3 receives its join state.
	_ = sessions[1].SubmitInput(wrapInput(1, 0xFFFFFFFC))
	_ = sessions[1].SubmitInput(wrapInput(1, 0xFFFFFFFD))
	sessions[3] = NewLockstepSession(LockstepConfig{Transport: hub.AddPeer(3), Sim: sims[3], Host: 1, Join: true})
	joinTick, err := sessions[1].AddPeer(3)
	if err != nil || joinTick != 0xFFFFFFFE {
		t.Fatalf("AddPeer = %#x, %v; want 0xfffffffe", joinTick, err)
	}
	_ = sessions[1].SubmitInput(wrapInput(1, 0xFFFFFFFE))
	leaveTick, err := sessions[1].RemovePeer(2)
	if err != nil || leaveTick != math.MaxUint32 {
		t.Fatalf("RemovePeer = %#x, %v; want 0xffffffff", leaveTick, err)
	}
	if mm := sessions[1].member(2); mm == nil || !mm.hasLeave || mm.until != math.MaxUint32 {
		t.Fatalf("member 2 after leave at MaxUint32 = %+v", mm)
	}

	submitted := map[PeerID]uint32{1: 0xFFFFFFFF, 3: joinTick}
	const end uint32 = 10
	for range 30 {
		if sessions[1].Tick() == end && sessions[3].Tick() == end {
			break
		}
		for _, id := range []PeerID{1, 3} {
			s := sessions[id]
			if s.Active() && tickAtOrBefore(submitted[id], s.Tick()) {
				_ = s.SubmitInput(wrapInput(id, s.Tick()))
				submitted[id] = s.Tick() + 1
			}
		}
		hub.Advance(latency)
		for _, s := range sessions {
			s.Step()
		}
	}
	if !sessions[3].Active() || sessions[1].Tick() != end || sessions[3].Tick() != end {
		t.Fatalf("stalled after leave at MaxUint32: p1=%#x p3=%#x active=%v",
			sessions[1].Tick(), sessions[3].Tick(), sessions[3].Active())
	}
	if sessions[2].Active() || sessions[2].Tick() != math.MaxUint32 {
		t.Fatalf("peer 2: active=%v tick=%#x, want inactive at 0xffffffff", sessions[2].Active(), sessions[2].Tick())
	}
	ref := replayWrap(start, end, map[PeerID]wrapSpan{
		1: {from: start},
		2: {from: start, until: math.MaxUint32, hasLeave: true, zero: func(uint32) bool { return true }},
		3: {from: joinTick},
	})
	if sims[1].Checksum() != ref[end] || sims[3].Checksum() != ref[end] {
		t.Fatalf("checksums diverged: p1=%x p3=%x ref=%x", sims[1].Checksum(), sims[3].Checksum(), ref[end])
	}
}

// TestRollbackWraparoundWithJoinAndLeave starts a rollback match a few ticks
// before math.MaxUint32 under latency and runs across the 2^32 wrap with a
// mid-match join and leave whose ticks cross the wrap boundary.
func TestRollbackWraparoundWithJoinAndLeave(t *testing.T) {
	for _, delay := range []uint32{0, 2} {
		t.Run(fmt.Sprintf("delay=%d", delay), func(t *testing.T) {
			const start uint32 = math.MaxUint32 - 3 // 0xFFFFFFFC
			const frame = 16 * time.Millisecond

			hub := NewLoopbackHub(LoopbackConfig{Latency: 50 * time.Millisecond})
			sims := map[PeerID]*wrapSim{1: newWrapSim(start), 2: newWrapSim(start), 3: {}}
			sessions := map[PeerID]*RollbackSession{}
			cfg := func(id PeerID) RollbackConfig {
				return RollbackConfig{
					Transport:        hub.AddPeer(id),
					Sim:              sims[id],
					Peers:            []PeerID{1, 2},
					MaxRollback:      32,
					InputDelay:       int(delay),
					ChecksumInterval: 5,
				}
			}
			for _, id := range []PeerID{1, 2} {
				s := NewRollbackSession(cfg(id))
				setRollbackStart(s, start)
				sessions[id] = s
			}
			step := func(id PeerID) {
				s := sessions[id]
				if err := s.Step(wrapInput(id, s.Tick()+delay)); err != nil {
					t.Fatalf("peer %d step at %#x: %v", id, s.Tick(), err)
				}
			}
			frameOf := func() {
				for _, id := range []PeerID{1, 2, 3} {
					if sessions[id] == nil {
						continue
					}
					step(id)
					for sessions[id].Active() && tickBefore(sessions[id].Tick(), sessions[1].Tick()) {
						step(id)
					}
				}
				hub.Advance(frame)
			}
			settle := func() {
				for range 20 {
					hub.Advance(frame)
					for id, s := range sessions {
						if err := s.Poll(); err != nil {
							t.Fatalf("peer %d poll: %v", id, err)
						}
					}
				}
			}

			// Step 2 frames: currentTick is 0xFFFFFFFE.
			// With delay=2, AddPeer(3) selects join tick 0x00000000 (right across the wrap!).
			// With delay=0, AddPeer(3) selects join tick 0xFFFFFFFE (just before the wrap!).
			for range 2 {
				frameOf()
			}
			c := cfg(3)
			c.Peers, c.Join, c.Host = nil, true, 1
			sessions[3] = NewRollbackSession(c)
			joinTick, err := sessions[1].AddPeer(3)
			wantJoin := uint32(0xFFFFFFFE) + delay
			if err != nil || joinTick != wantJoin {
				t.Fatalf("AddPeer = %#x, %v; want %#x", joinTick, err, wantJoin)
			}

			// Run 15 frames across the wrap so peer 3 joins and rollbacks occur across 0xFFFFFFFF -> 0.
			for range 15 {
				frameOf()
			}
			settle()
			if !sessions[3].Active() {
				t.Fatal("newcomer never became active across wrap")
			}
			if sessions[1].ConfirmedTick() != int64(sessions[1].Tick()-1) {
				t.Fatalf("host confirmed %d at tick %#x", sessions[1].ConfirmedTick(), sessions[1].Tick())
			}

			// Remove peer 2 after settling, then step another 25 frames and settle.
			leaveTick, err := sessions[1].RemovePeer(2)
			if err != nil || leaveTick != sessions[1].Tick()+delay {
				t.Fatalf("RemovePeer = %#x, %v; want %#x", leaveTick, err, sessions[1].Tick()+delay)
			}
			for range 25 {
				frameOf()
			}
			settle()

			end := sessions[1].Tick()
			if sessions[3].Tick() != end || sims[1].tick != end || sims[3].tick != end {
				t.Fatalf("final ticks differ: p1=%#x p3=%#x sim1=%#x sim3=%#x",
					end, sessions[3].Tick(), sims[1].tick, sims[3].tick)
			}
			for _, id := range []PeerID{1, 3} {
				if got := sessions[id].ConfirmedTick(); got != int64(end-1) {
					t.Fatalf("peer %d confirmed = %d, want %d", id, got, end-1)
				}
			}
			if sessions[2].Active() {
				t.Fatal("removed peer 2 is still active")
			}
			if sessions[1].Stats().Rollbacks == 0 && sessions[2].Stats().Rollbacks == 0 {
				t.Fatal("expected rollbacks under 50ms latency across wrap")
			}

			ref := replayWrap(start, end, map[PeerID]wrapSpan{
				1: {from: start, zero: func(tk uint32) bool { return tickBefore(tk, start+delay) }},
				2: {from: start, until: leaveTick, hasLeave: true, zero: func(tk uint32) bool { return tickBefore(tk, start+delay) }},
				3: {from: joinTick, zero: func(tk uint32) bool { return tickBefore(tk, joinTick+delay) }},
			})
			if sims[3].sums[joinTick] != ref[joinTick] {
				t.Fatalf("newcomer state at join tick %#x = %x, want %x", joinTick, sims[3].sums[joinTick], ref[joinTick])
			}
			if sims[1].Checksum() != ref[end] || sims[3].Checksum() != ref[end] {
				t.Fatalf("final checksums differ from replay: p1=%x p3=%x ref=%x", sims[1].Checksum(), sims[3].Checksum(), ref[end])
			}
			for id, s := range sessions {
				if d := s.Desyncs(); len(d) != 0 {
					t.Fatalf("peer %d desyncs across wrap: %+v", id, d)
				}
			}
		})
	}
}

// TestRollbackLeaveAcrossWrap verifies a rollback peer removed right before
// math.MaxUint32 whose leave tick falls after the wrap (delay=2: tick 0xFFFFFFFF
// -> leave tick 1).
func TestRollbackLeaveAcrossWrap(t *testing.T) {
	const start uint32 = math.MaxUint32 - 2 // 0xFFFFFFFD
	const delay uint32 = 2
	const frame = 16 * time.Millisecond

	hub := NewLoopbackHub(LoopbackConfig{Latency: 32 * time.Millisecond})
	sims := map[PeerID]*wrapSim{1: newWrapSim(start), 2: newWrapSim(start)}
	sessions := map[PeerID]*RollbackSession{}
	for _, id := range []PeerID{1, 2} {
		s := NewRollbackSession(RollbackConfig{
			Transport:        hub.AddPeer(id),
			Sim:              sims[id],
			Peers:            []PeerID{1, 2},
			MaxRollback:      16,
			InputDelay:       int(delay),
			ChecksumInterval: 4,
		})
		setRollbackStart(s, start)
		sessions[id] = s
	}

	// Step 2 ticks to 0xFFFFFFFF, settle, then remove peer 2 so leaveTick = 1 (crossing wrap).
	for range 2 {
		for _, id := range []PeerID{1, 2} {
			_ = sessions[id].Step(wrapInput(id, sessions[id].Tick()+delay))
		}
		hub.Advance(frame)
	}
	for range 10 {
		hub.Advance(frame)
		for _, s := range sessions {
			if err := s.Poll(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if sessions[1].Tick() != math.MaxUint32 {
		t.Fatalf("host tick = %#x, want 0xffffffff", sessions[1].Tick())
	}

	leaveTick, err := sessions[1].RemovePeer(2)
	if err != nil || leaveTick != 1 {
		t.Fatalf("RemovePeer = %#x, %v; want leave tick 1 across wrap", leaveTick, err)
	}
	for range 20 {
		if err := sessions[1].Step(wrapInput(1, sessions[1].Tick()+delay)); err != nil {
			t.Fatalf("host step at %#x: %v", sessions[1].Tick(), err)
		}
		if err := sessions[2].Poll(); err != nil {
			t.Fatalf("peer 2 poll: %v", err)
		}
		hub.Advance(frame)
	}
	for range 10 {
		hub.Advance(frame)
		_ = sessions[1].Poll()
		_ = sessions[2].Poll()
	}

	end := sessions[1].Tick()
	if sessions[1].ConfirmedTick() != int64(end-1) {
		t.Fatalf("host confirmed = %d at %#x, want %d", sessions[1].ConfirmedTick(), end, end-1)
	}
	ref := replayWrap(start, end, map[PeerID]wrapSpan{
		1: {from: start, zero: func(tk uint32) bool { return tickBefore(tk, start+delay) }},
		2: {from: start, until: leaveTick, hasLeave: true, zero: func(tk uint32) bool { return tickBefore(tk, start+delay) }},
	})
	if sims[1].Checksum() != ref[end] {
		t.Fatalf("host checksum = %x, want %x", sims[1].Checksum(), ref[end])
	}
	if d := sessions[1].Desyncs(); len(d) != 0 {
		t.Fatalf("host desyncs: %+v", d)
	}
}

// TestRollbackLateFirstInputBeyondMaxRollbackConfirms verifies that pruneHistory never discards the first unconfirmed tick while nothing is confirmed, and that a session whose peer has sent nothing stalls with ErrRollbackStalled MaxRollback ticks in, so a peer whose first input arrives later than that still allows confirmation to advance cleanly.
func TestRollbackLateFirstInputBeyondMaxRollbackConfirms(t *testing.T) {
	for _, start := range []uint32{0, math.MaxUint32 - 3} {
		t.Run(fmt.Sprintf("start=%#x", start), func(t *testing.T) {
			const maxRB = 4
			const lateSteps = maxRB + 6 // strictly greater than MaxRollback + 2

			tr := &memberTransport{id: 1}
			sim := newWrapSim(start)
			r := NewRollbackSession(RollbackConfig{
				Transport:        tr,
				Sim:              sim,
				Peers:            []PeerID{1, 2},
				MaxRollback:      maxRB,
				InputDelay:       0,
				ChecksumInterval: 3,
			})
			setRollbackStart(r, start)

			// Step for more than MaxRollback+2 ticks while peer 2 has sent nothing at all: the session runs MaxRollback ticks, then stalls.
			for i := range lateSteps {
				err := r.Step(wrapInput(1, r.Tick()))
				if i < maxRB && err != nil {
					t.Fatalf("step at %#x: %v", r.Tick(), err)
				}
				if i >= maxRB && !errors.Is(err, ErrRollbackStalled) {
					t.Fatalf("step at %#x = %v, want ErrRollbackStalled", r.Tick(), err)
				}
			}
			if r.ConfirmedTick() != -1 || r.Tick() != start+maxRB {
				t.Fatalf("ConfirmedTick = %d at tick %#x while peer 2 sent nothing, want -1 at %#x", r.ConfirmedTick(), r.Tick(), start+maxRB)
			}

			// Peer 2's first inputs arrive late, matching the predicted zero input.
			var p2Inputs []InputFrame
			for i := range uint32(lateSteps) {
				p2Inputs = append(p2Inputs, InputFrame{Tick: start + i, Peer: 2})
			}
			tr.inbox = append(tr.inbox, Packet{From: 2, Payload: encodeInputBundle(p2Inputs)})
			if err := r.Poll(); err != nil {
				t.Fatalf("Poll after late first inputs: %v", err)
			}

			wantConfirmed := int64(start + maxRB - 1)
			if r.ConfirmedTick() != wantConfirmed {
				t.Fatalf("ConfirmedTick = %d, want %d (start=%#x)", r.ConfirmedTick(), wantConfirmed, start)
			}

			// Continuing to step with both peers' inputs prunes old snapshots normally.
			for range 10 {
				tk := r.Tick()
				tr.inbox = append(tr.inbox, Packet{From: 2, Payload: encodeSingleInput(InputFrame{Tick: tk, Peer: 2})})
				if err := r.Step(wrapInput(1, tk)); err != nil {
					t.Fatalf("step after confirm at %#x: %v", tk, err)
				}
			}
			if r.ConfirmedTick() != int64(r.Tick()-1) {
				t.Fatalf("ConfirmedTick = %d at tick %#x, want %d", r.ConfirmedTick(), r.Tick(), r.Tick()-1)
			}
			if len(r.snapshots) > maxRB+4 {
				t.Fatalf("snapshots not pruned after confirmation: %d entries kept", len(r.snapshots))
			}
		})
	}
}
