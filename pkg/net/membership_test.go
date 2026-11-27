package net_test

import (
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	gamenet "github.com/nsgo-dev/game/pkg/net"
)

// crowdSim is a deterministic simulation for any number of players whose state depends on every input, its peer and its order, and which remembers its checksum at every tick it reached.
type crowdSim struct {
	tick    uint32
	acc     uint64
	players uint32
	sums    map[uint32]uint64
}

func (s *crowdSim) Step(inputs []gamenet.InputFrame) {
	for _, in := range inputs {
		s.acc = (s.acc ^ uint64(in.Peer)<<40 ^ uint64(in.Tick)<<20 ^ in.Bits) * 1099511628211
	}
	s.players = uint32(len(inputs))
	s.tick++
	s.remember()
}

func (s *crowdSim) remember() {
	if s.sums == nil {
		s.sums = make(map[uint32]uint64)
	}
	s.sums[s.tick] = s.Checksum()
}

// newCrowdSim returns a simulation at tick 0 that remembers that tick's checksum.
func newCrowdSim() *crowdSim {
	s := &crowdSim{}
	s.remember()
	return s
}

func (s *crowdSim) Save() []byte {
	b := make([]byte, 16)
	binary.BigEndian.PutUint32(b[0:4], s.tick)
	binary.BigEndian.PutUint64(b[4:12], s.acc)
	binary.BigEndian.PutUint32(b[12:16], s.players)
	return b
}

func (s *crowdSim) Load(b []byte) error {
	if len(b) != 16 {
		return fmt.Errorf("crowdSim: snapshot of %d bytes", len(b))
	}
	s.tick = binary.BigEndian.Uint32(b[0:4])
	s.acc = binary.BigEndian.Uint64(b[4:12])
	s.players = binary.BigEndian.Uint32(b[12:16])
	s.remember()
	return nil
}

func (s *crowdSim) Checksum() uint64 { return gamenet.ChecksumBytes(s.Save()) }

// crowdInput is peer's scripted input for tick, never zero so a declared zero input shows.
func crowdInput(peer gamenet.PeerID, tick uint32) gamenet.InputFrame {
	return gamenet.InputFrame{Tick: tick, Peer: peer, Bits: uint64(tick*7+uint32(peer)*13)%23 + 1}
}

// span is the ticks a peer takes part in, and zero, the ticks it plays a zero input.
type span struct {
	from, until uint32
	zero        func(tick uint32) bool
}

// replay runs the reference simulation of a match with the given peers to tick end and returns its checksum at every tick.
func replay(peers map[gamenet.PeerID]span, end uint32) map[uint32]uint64 {
	ids := make([]gamenet.PeerID, 0, len(peers))
	for id := range peers {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	ref := newCrowdSim()
	for t := range end {
		var inputs []gamenet.InputFrame
		for _, id := range ids {
			sp := peers[id]
			if t < sp.from || t >= sp.until {
				continue
			}
			in := crowdInput(id, t)
			if sp.zero != nil && sp.zero(t) {
				in = gamenet.InputFrame{Tick: t, Peer: id}
			}
			inputs = append(inputs, in)
		}
		ref.Step(inputs)
	}
	return ref.sums
}

// checkAgainst fails unless sim reached every tick from first to its current one with the reference checksum.
func checkAgainst(t *testing.T, name string, sim *crowdSim, first uint32, ref map[uint32]uint64) {
	t.Helper()
	for tick := first; tick <= sim.tick; tick++ {
		got, ok := sim.sums[tick]
		if !ok || got != ref[tick] {
			t.Fatalf("%s at tick %d: checksum %x (reached %v), want %x", name, tick, got, ok, ref[tick])
		}
	}
}

const never = ^uint32(0)

// TestLockstepPeerJoinsAndLeavesMidMatch runs peers 1 and 2 for 10 ticks, admits peer 3, which loads the host's state at its join tick and plays on, then removes peer 2 while the host has submitted inputs ahead, so the ticks the host holds no input of peer 2 for are played with a zero input on every peer.
func TestLockstepPeerJoinsAndLeavesMidMatch(t *testing.T) {
	const latency = 20 * time.Millisecond
	hub := gamenet.NewLoopbackHub(gamenet.LoopbackConfig{Latency: latency})
	sims := map[gamenet.PeerID]*crowdSim{1: newCrowdSim(), 2: newCrowdSim(), 3: {}}
	sessions := map[gamenet.PeerID]*gamenet.LockstepSession{}
	for _, id := range []gamenet.PeerID{1, 2} {
		sessions[id] = gamenet.NewLockstepSession(gamenet.LockstepConfig{Transport: hub.AddPeer(id), Sim: sims[id], Peers: []gamenet.PeerID{1, 2}, ChecksumInterval: 5})
	}
	submitted := map[gamenet.PeerID]uint32{}
	// round submits each active session's input for its tick once, lets the network deliver, and steps every session.
	round := func(skip ...gamenet.PeerID) {
		for id, s := range sessions {
			if s.Active() && !slices.Contains(skip, id) && submitted[id] <= s.Tick() {
				if err := s.SubmitInput(crowdInput(id, s.Tick())); err != nil {
					t.Fatalf("peer %d submit: %v", id, err)
				}
				submitted[id] = s.Tick() + 1
			}
		}
		hub.Advance(latency)
		for _, s := range sessions {
			s.Step()
		}
	}
	for range 10 {
		round()
	}

	sessions[3] = gamenet.NewLockstepSession(gamenet.LockstepConfig{Transport: hub.AddPeer(3), Sim: sims[3], Host: 1, Join: true, ChecksumInterval: 5})
	if _, err := sessions[2].AddPeer(3); !errors.Is(err, gamenet.ErrNotHost) {
		t.Fatalf("AddPeer on a peer that is not the host = %v, want ErrNotHost", err)
	}
	join, err := sessions[1].AddPeer(3)
	if err != nil || join != 10 {
		t.Fatalf("AddPeer = %d, %v; want join tick 10", join, err)
	}
	if again, err := sessions[1].AddPeer(3); err != nil || again != join {
		t.Fatalf("AddPeer again = %d, %v; want %d", again, err, join)
	}
	if sessions[3].Active() || sessions[3].Step() {
		t.Fatal("the joining peer took part before its join state arrived")
	}
	// The host is at the join tick already, so the state is on its way; one latency later the newcomer holds it.
	hub.Advance(latency)
	sessions[3].Step()
	if !sessions[3].Active() || sessions[3].Tick() != join || sims[3].Checksum() != sims[1].sums[join] {
		t.Fatalf("after the state transfer the newcomer is active=%v at tick %d with %x, want tick %d with the host's %x", sessions[3].Active(), sessions[3].Tick(), sims[3].Checksum(), join, sims[1].sums[join])
	}
	if got := sessions[3].Members(); !slices.Equal(got, []gamenet.PeerID{1, 2, 3}) {
		t.Fatalf("newcomer's members = %v", got)
	}
	// Peer 2 sent its input for the join tick before it learned of peer 3 and sends it again on learning, one round late.
	for range 40 {
		if sessions[1].Tick() == 30 {
			break
		}
		round()
	}
	for id, s := range sessions {
		if s.Tick() != 30 {
			t.Fatalf("peer %d at tick %d, want 30", id, s.Tick())
		}
	}

	// The host and peer 3 submit three ticks ahead; peer 2 stops sending and is removed.
	for tick := uint32(30); tick < 33; tick++ {
		for _, id := range []gamenet.PeerID{1, 3} {
			if err := sessions[id].SubmitInput(crowdInput(id, tick)); err != nil {
				t.Fatalf("submit ahead: %v", err)
			}
			submitted[id] = tick + 1
		}
	}
	leave, err := sessions[1].RemovePeer(2)
	if err != nil || leave != 33 {
		t.Fatalf("RemovePeer = %d, %v; want leave tick 33", leave, err)
	}
	if _, err := sessions[1].RemovePeer(1); !errors.Is(err, gamenet.ErrHostLeave) {
		t.Fatalf("RemovePeer(host) = %v, want ErrHostLeave", err)
	}
	if _, err := sessions[1].AddPeer(2); !errors.Is(err, gamenet.ErrPeerRejoin) {
		t.Fatalf("AddPeer of a leaving peer = %v, want ErrPeerRejoin", err)
	}
	for range 20 {
		round(2)
	}
	if sessions[2].Active() || sessions[2].Tick() != leave {
		t.Fatalf("the removed peer is active=%v at tick %d, want stopped at %d", sessions[2].Active(), sessions[2].Tick(), leave)
	}
	if got := sessions[1].Members(); !slices.Equal(got, []gamenet.PeerID{1, 3}) {
		t.Fatalf("host's members after the leave = %v", got)
	}

	ref := replay(map[gamenet.PeerID]span{
		1: {until: never},
		2: {until: leave, zero: func(tick uint32) bool { return tick >= 30 }},
		3: {from: join, until: never},
	}, 50)
	checkAgainst(t, "peer 1", sims[1], 0, ref)
	checkAgainst(t, "peer 2", sims[2], 0, ref)
	checkAgainst(t, "peer 3", sims[3], join, ref)
	if sims[1].tick != 50 || sims[3].tick != 50 {
		t.Fatalf("the match stopped: peer 1 at %d, peer 3 at %d", sims[1].tick, sims[3].tick)
	}
	for id, s := range sessions {
		if d := s.Desyncs(); len(d) != 0 {
			t.Fatalf("peer %d desyncs: %+v", id, d)
		}
	}
}

// TestRollbackPeerJoinsAndLeavesMidMatch admits peer 3 to a rollback match under latency, which the others have already predicted past by the time the change arrives, and later removes peer 2 once every input it sent before its leave tick is confirmed. Every peer must end in the state of a zero-latency replay.
func TestRollbackPeerJoinsAndLeavesMidMatch(t *testing.T) {
	for _, delay := range []uint32{0, 2} {
		t.Run(fmt.Sprintf("delay=%d", delay), func(t *testing.T) {
			const frame = 16 * time.Millisecond
			hub := gamenet.NewLoopbackHub(gamenet.LoopbackConfig{Latency: 50 * time.Millisecond})
			sims := map[gamenet.PeerID]*crowdSim{1: newCrowdSim(), 2: newCrowdSim(), 3: {}}
			sessions := map[gamenet.PeerID]*gamenet.RollbackSession{}
			cfg := func(id gamenet.PeerID) gamenet.RollbackConfig {
				return gamenet.RollbackConfig{Transport: hub.AddPeer(id), Sim: sims[id], Peers: []gamenet.PeerID{1, 2}, MaxRollback: 32, InputDelay: int(delay), ChecksumInterval: 5}
			}
			for _, id := range []gamenet.PeerID{1, 2} {
				sessions[id] = gamenet.NewRollbackSession(cfg(id))
			}
			step := func(id gamenet.PeerID) {
				s := sessions[id]
				if err := s.Step(crowdInput(id, s.Tick()+delay)); err != nil {
					t.Fatalf("peer %d step at %d: %v", id, s.Tick(), err)
				}
			}
			// frameOf steps every session once and lets a frame pass; a newcomer catches up with the host as a game fast-forwards.
			frameOf := func() {
				for _, id := range []gamenet.PeerID{1, 2, 3} {
					if sessions[id] == nil {
						continue
					}
					step(id)
					for sessions[id].Active() && sessions[id].Tick() < sessions[1].Tick() {
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
			for range 20 {
				frameOf()
			}

			c := cfg(3)
			c.Peers, c.Join, c.Host = nil, true, 1
			sessions[3] = gamenet.NewRollbackSession(c)
			join, err := sessions[1].AddPeer(3)
			if err != nil || join != 20+delay {
				t.Fatalf("AddPeer = %d, %v; want %d", join, err, 20+delay)
			}
			for range 40 {
				frameOf()
			}
			if !sessions[3].Active() {
				t.Fatal("the newcomer never received its join state")
			}

			// Everyone stops and confirms everything, then the host removes peer 2, whose later inputs fall on or after its leave tick and are ignored.
			settle()
			if a, b := sessions[1].Tick(), sessions[2].Tick(); a != b || sessions[1].ConfirmedTick() != int64(a)-1 {
				t.Fatalf("not settled: peer 1 at %d (confirmed %d), peer 2 at %d", a, sessions[1].ConfirmedTick(), b)
			}
			leave, err := sessions[1].RemovePeer(2)
			if err != nil || leave != sessions[1].Tick()+delay {
				t.Fatalf("RemovePeer = %d, %v; want %d", leave, err, sessions[1].Tick()+delay)
			}
			for range 30 {
				frameOf()
			}
			settle()

			end := sessions[1].Tick()
			if sessions[3].Tick() != end || sims[1].tick != end {
				t.Fatalf("peer 3 at %d, peer 1 at %d", sessions[3].Tick(), end)
			}
			ref := replay(map[gamenet.PeerID]span{
				1: {until: never, zero: func(tick uint32) bool { return tick < delay }},
				2: {until: leave, zero: func(tick uint32) bool { return tick < delay }},
				3: {from: join, until: never, zero: func(tick uint32) bool { return tick < join+delay }},
			}, end)
			if sims[3].sums[join] != ref[join] {
				t.Fatalf("the newcomer's state at its join tick is %x, want %x", sims[3].sums[join], ref[join])
			}
			if sims[1].Checksum() != ref[end] || sims[3].Checksum() != ref[end] {
				t.Fatalf("final states differ from the replay: peer 1 %x, peer 3 %x, want %x", sims[1].Checksum(), sims[3].Checksum(), ref[end])
			}
			if sessions[2].Active() {
				t.Fatal("the removed peer is still active")
			}
			if st := sessions[2].Stats(); sessions[1].Stats().Rollbacks == 0 && st.Rollbacks == 0 {
				t.Fatal("no peer rolled back; the join should have arrived after they predicted past it")
			}
			for id, s := range sessions {
				if d := s.Desyncs(); len(d) != 0 {
					t.Fatalf("peer %d desyncs: %+v", id, d)
				}
			}
		})
	}
}

// TestLockstepPeerLeavesBeforeANewcomerHasItsState removes peer 2 after the host admits peer 3 but before peer 3 has its join state, with the leave tick after the join tick. The join state carries the inputs the host declared for peer 2 from the join tick to its leave, so peer 3 plays those ticks instead of waiting for inputs no one sends it.
func TestLockstepPeerLeavesBeforeANewcomerHasItsState(t *testing.T) {
	const latency = 20 * time.Millisecond
	hub := gamenet.NewLoopbackHub(gamenet.LoopbackConfig{Latency: latency})
	sims := map[gamenet.PeerID]*crowdSim{1: newCrowdSim(), 2: newCrowdSim(), 3: {}}
	sessions := map[gamenet.PeerID]*gamenet.LockstepSession{}
	for _, id := range []gamenet.PeerID{1, 2} {
		sessions[id] = gamenet.NewLockstepSession(gamenet.LockstepConfig{Transport: hub.AddPeer(id), Sim: sims[id], Peers: []gamenet.PeerID{1, 2}, ChecksumInterval: 5})
	}
	submitted := map[gamenet.PeerID]uint32{}
	submit := func(id gamenet.PeerID, tick uint32) {
		if err := sessions[id].SubmitInput(crowdInput(id, tick)); err != nil {
			t.Fatalf("peer %d submit %d: %v", id, tick, err)
		}
		submitted[id] = tick + 1
	}
	round := func(players ...gamenet.PeerID) {
		for _, id := range players {
			if s := sessions[id]; s.Active() && submitted[id] <= s.Tick() {
				submit(id, s.Tick())
			}
		}
		hub.Advance(latency)
		for _, s := range sessions {
			s.Step()
		}
	}
	for range 10 {
		round(1, 2)
	}
	if sessions[1].Tick() != 10 || sessions[2].Tick() != 10 {
		t.Fatalf("peers at %d and %d, want 10", sessions[1].Tick(), sessions[2].Tick())
	}

	// Peer 2 stops sending. The host submits ahead, admits peer 3, submits once more and removes peer 2, all before its simulation reaches the join tick.
	for tick := uint32(10); tick < 13; tick++ {
		submit(1, tick)
	}
	sessions[3] = gamenet.NewLockstepSession(gamenet.LockstepConfig{Transport: hub.AddPeer(3), Sim: sims[3], Host: 1, Join: true, ChecksumInterval: 5})
	join, err := sessions[1].AddPeer(3)
	if err != nil || join != 13 {
		t.Fatalf("AddPeer = %d, %v; want 13", join, err)
	}
	submit(1, 13)
	leave, err := sessions[1].RemovePeer(2)
	if err != nil || leave != 14 {
		t.Fatalf("RemovePeer = %d, %v; want 14", leave, err)
	}
	if sessions[3].Active() {
		t.Fatal("the newcomer had its state before the leave")
	}
	for range 60 {
		if sessions[1].Tick() == 40 && sessions[3].Tick() == 40 {
			break
		}
		round(1, 3)
	}
	if !sessions[3].Active() || sims[1].tick != 40 || sims[3].tick != 40 {
		t.Fatalf("the match stalled: newcomer active=%v, peer 1 at %d, peer 3 at %d", sessions[3].Active(), sims[1].tick, sims[3].tick)
	}
	if sessions[2].Tick() != leave || sessions[2].Active() {
		t.Fatalf("the removed peer is at %d, active=%v; want stopped at %d", sessions[2].Tick(), sessions[2].Active(), leave)
	}
	ref := replay(map[gamenet.PeerID]span{
		1: {until: never},
		2: {until: leave, zero: func(tick uint32) bool { return tick >= 10 }},
		3: {from: join, until: never},
	}, 40)
	checkAgainst(t, "peer 1", sims[1], 0, ref)
	checkAgainst(t, "peer 2", sims[2], 0, ref)
	checkAgainst(t, "peer 3", sims[3], join, ref)
	for id, s := range sessions {
		if d := s.Desyncs(); len(d) != 0 {
			t.Fatalf("peer %d desyncs: %+v", id, d)
		}
	}
}

// TestRollbackPeerLeavesBeforeANewcomerHasItsState is the rollback form of TestLockstepPeerLeavesBeforeANewcomerHasItsState: the host removes peer 2 while the ticks before peer 3's join tick are not all confirmed yet, so peer 3's join state goes out after the leave, and peer 3 must still confirm every tick it plays.
func TestRollbackPeerLeavesBeforeANewcomerHasItsState(t *testing.T) {
	for _, delay := range []uint32{2, 3} {
		t.Run(fmt.Sprintf("delay=%d", delay), func(t *testing.T) {
			const frame = 16 * time.Millisecond
			hub := gamenet.NewLoopbackHub(gamenet.LoopbackConfig{Latency: 50 * time.Millisecond})
			sims := map[gamenet.PeerID]*crowdSim{1: newCrowdSim(), 2: newCrowdSim(), 3: {}}
			sessions := map[gamenet.PeerID]*gamenet.RollbackSession{}
			cfg := func(id gamenet.PeerID) gamenet.RollbackConfig {
				return gamenet.RollbackConfig{Transport: hub.AddPeer(id), Sim: sims[id], Peers: []gamenet.PeerID{1, 2}, MaxRollback: 32, InputDelay: int(delay), ChecksumInterval: 5}
			}
			for _, id := range []gamenet.PeerID{1, 2} {
				sessions[id] = gamenet.NewRollbackSession(cfg(id))
			}
			step := func(id gamenet.PeerID) {
				s := sessions[id]
				if err := s.Step(crowdInput(id, s.Tick()+delay)); err != nil {
					t.Fatalf("peer %d step at %d: %v", id, s.Tick(), err)
				}
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
			for range 20 {
				step(1)
				step(2)
				hub.Advance(frame)
			}
			settle()
			t0 := sessions[1].Tick()
			if sessions[2].Tick() != t0 || sessions[1].ConfirmedTick() != int64(t0)-1 {
				t.Fatalf("not settled: peer 1 at %d (confirmed %d), peer 2 at %d", t0, sessions[1].ConfirmedTick(), sessions[2].Tick())
			}

			// Peer 2 stops sending; the host admits peer 3, runs one tick and removes peer 2.
			c := cfg(3)
			c.Peers, c.Join, c.Host = nil, true, 1
			sessions[3] = gamenet.NewRollbackSession(c)
			join, err := sessions[1].AddPeer(3)
			if err != nil || join != t0+delay {
				t.Fatalf("AddPeer = %d, %v; want %d", join, err, t0+delay)
			}
			step(1)
			leave, err := sessions[1].RemovePeer(2)
			if err != nil || leave != join+1 {
				t.Fatalf("RemovePeer = %d, %v; want %d", leave, err, join+1)
			}
			if sessions[1].ConfirmedTick()+1 >= int64(join) {
				t.Fatalf("the host confirmed tick %d, so the join state left before the leave", sessions[1].ConfirmedTick())
			}
			for range 40 {
				for _, id := range []gamenet.PeerID{1, 3} {
					step(id)
					for sessions[id].Active() && sessions[id].Tick() < sessions[1].Tick() {
						step(id)
					}
				}
				if err := sessions[2].Poll(); err != nil {
					t.Fatal(err)
				}
				hub.Advance(frame)
			}
			settle()

			end := sessions[1].Tick()
			for _, id := range []gamenet.PeerID{1, 3} {
				if s := sessions[id]; s.Tick() != end || s.ConfirmedTick() != int64(end)-1 {
					t.Fatalf("peer %d at %d confirmed %d, want %d confirmed %d", id, s.Tick(), s.ConfirmedTick(), end, end-1)
				}
			}
			ref := replay(map[gamenet.PeerID]span{
				1: {until: never, zero: func(tick uint32) bool { return tick < delay }},
				2: {until: leave, zero: func(tick uint32) bool { return tick < delay || tick >= join }},
				3: {from: join, until: never, zero: func(tick uint32) bool { return tick < join+delay }},
			}, end)
			if sims[3].sums[join] != ref[join] {
				t.Fatalf("the newcomer's state at its join tick is %x, want %x", sims[3].sums[join], ref[join])
			}
			if sims[1].Checksum() != ref[end] || sims[3].Checksum() != ref[end] {
				t.Fatalf("final states differ from the replay: peer 1 %x, peer 3 %x, want %x", sims[1].Checksum(), sims[3].Checksum(), ref[end])
			}
			for id, s := range sessions {
				if d := s.Desyncs(); len(d) != 0 {
					t.Fatalf("peer %d desyncs: %+v", id, d)
				}
			}
		})
	}
}
