package netplay

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// TestLockstepRemoteChecksumsStayBounded has a member send checksums for ticks far ahead of and far behind the current tick on every step: the session keeps only the ticks inside its checksum window, so the map cannot be grown at will.
func TestLockstepRemoteChecksumsStayBounded(t *testing.T) {
	tr := &memberTransport{id: 1}
	s := NewLockstepSession(LockstepConfig{Transport: tr, Sim: &memberSim{}, Peers: []PeerID{1, 2}, ChecksumInterval: 4})
	const steps = 600
	for i := range uint32(steps) {
		tick := s.Tick()
		tr.inbox = append(tr.inbox, Packet{From: 2, Payload: encodeSingleInput(InputFrame{Tick: tick, Peer: 2})})
		for k := range uint32(50) {
			tr.inbox = append(tr.inbox,
				Packet{From: 2, Payload: encodeChecksumMsg(tick+1000+7*(i*50+k), 2, 1)},
				Packet{From: 2, Payload: encodeChecksumMsg(tick-lockstepChecksumHistory-2-k*3, 2, 1)},
			)
		}
		if err := s.SubmitInput(InputFrame{Tick: tick}); err != nil {
			t.Fatal(err)
		}
		if !s.Step() {
			t.Fatalf("step %d did not run", i)
		}
	}
	if n := len(s.remoteChecksums); n > lockstepChecksumHistory+maxLockstepLeadTicks+2 {
		t.Fatalf("remote checksums hold %d ticks after %d steps, want at most %d", n, steps, lockstepChecksumHistory+maxLockstepLeadTicks+2)
	}
	for tick := range s.remoteChecksums {
		if d := tickDelta(tick, s.Tick()); d < -lockstepChecksumHistory-1 || d > maxLockstepLeadTicks {
			t.Fatalf("kept a remote checksum for tick %d at tick %d", tick, s.Tick())
		}
	}
}

// TestRollbackRemoteChecksumsStayBounded has a member send checksums for ticks long past on every step, ticks this peer never computed a checksum for: the session refuses them and prunes the map by its own ticks, so it stays within the checksum window.
func TestRollbackRemoteChecksumsStayBounded(t *testing.T) {
	tr := &memberTransport{id: 1}
	s := NewRollbackSession(RollbackConfig{Transport: tr, Sim: &memberSim{}, Peers: []PeerID{1, 2}, MaxRollback: 8, ChecksumInterval: 4})
	const steps = 1000
	for i := range uint32(steps) {
		tick := s.Tick()
		tr.inbox = append(tr.inbox, Packet{From: 2, Payload: encodeSingleInput(InputFrame{Tick: tick, Peer: 2})})
		for k := range uint32(50) {
			tr.inbox = append(tr.inbox, Packet{From: 2, Payload: encodeChecksumMsg(0-3*(i*50+k)-1, 2, 1)})
		}
		if err := s.Step(InputFrame{}); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	limit := rollbackChecksumHistory + maxRollbackLeadTicks + 2
	if n := len(s.remoteChecksums); n > limit {
		t.Fatalf("remote checksums hold %d ticks after %d steps, want at most %d", n, steps, limit)
	}
	if n := len(s.computedChecksums); n > rollbackChecksumHistory+2 {
		t.Fatalf("computed checksums hold %d ticks", n)
	}
}

// TestRollbackSilentPeerStalls runs a two-peer session in which peer 2 sends nothing: once the local peer is MaxRollback ticks past the oldest unconfirmed tick, Step stops advancing and returns ErrRollbackStalled, which is also ErrRollbackWindowExceeded, so history stays bounded; when peer 2's inputs arrive the session rolls back, confirms and advances again.
func TestRollbackSilentPeerStalls(t *testing.T) {
	const maxRB = 8
	tr := &memberTransport{id: 1}
	sim := &memberSim{}
	s := NewRollbackSession(RollbackConfig{Transport: tr, Sim: sim, Peers: []PeerID{1, 2}, MaxRollback: maxRB, ChecksumInterval: 4})
	for i := range maxRB {
		if err := s.Step(InputFrame{Bits: 1}); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	for i := range 2000 {
		err := s.Step(InputFrame{Bits: 1})
		if !errors.Is(err, ErrRollbackStalled) || !errors.Is(err, ErrRollbackWindowExceeded) {
			t.Fatalf("stalled step %d = %v, want ErrRollbackStalled", i, err)
		}
	}
	if s.Tick() != maxRB {
		t.Fatalf("tick = %d while peer 2 is silent, want %d", s.Tick(), maxRB)
	}
	for name, n := range map[string]int{"snapshots": len(s.snapshots), "usedInputs": len(s.usedInputs), "computedChecksums": len(s.computedChecksums), "confirmedInputs": len(s.confirmedInputs)} {
		if n > maxRB+2 {
			t.Fatalf("%s holds %d ticks while stalled, want at most %d", name, n, maxRB+2)
		}
	}

	// Peer 2's inputs arrive and differ from the prediction at tick 0, the oldest the window allows rolling back to.
	var late []InputFrame
	for tick := range uint32(maxRB + 1) {
		late = append(late, InputFrame{Tick: tick, Peer: 2, Bits: 2})
	}
	tr.inbox = append(tr.inbox, Packet{From: 2, Payload: encodeInputBundle(late)})
	if err := s.Step(InputFrame{Bits: 1}); err != nil {
		t.Fatalf("step after peer 2's inputs: %v", err)
	}
	if s.Tick() != maxRB+1 || s.ConfirmedTick() != maxRB {
		t.Fatalf("tick %d, confirmed %d after peer 2's inputs, want %d and %d", s.Tick(), s.ConfirmedTick(), maxRB+1, maxRB)
	}
	ref := &memberSim{}
	for tick := range uint32(maxRB + 1) {
		ref.Step([]InputFrame{{Tick: tick, Peer: 1, Bits: 1}, {Tick: tick, Peer: 2, Bits: 2}})
	}
	if sim.Checksum() != ref.Checksum() {
		t.Fatalf("state after resuming = %x, want %x", sim.Checksum(), ref.Checksum())
	}
}

// TestRollbackDesyncsStayBounded has a member send a mismatching checksum for every tick, not only the checksum ticks: the session takes only the checksum ticks, reports each once through OnDesync and Stats, and keeps only the latest maxDesyncHistory of them for Desyncs.
func TestRollbackDesyncsStayBounded(t *testing.T) {
	const interval = 4
	tr := &memberTransport{id: 1}
	reported := 0
	s := NewRollbackSession(RollbackConfig{Transport: tr, Sim: &memberSim{}, Peers: []PeerID{1, 2}, MaxRollback: 8, ChecksumInterval: interval, OnDesync: func(ev DesyncEvent) {
		reported++
		if (ev.Tick+1)%interval != 0 {
			t.Fatalf("desync reported for tick %d, which is not a checksum tick", ev.Tick)
		}
	}})
	const steps = 4000
	for i := range uint32(steps) {
		tick := s.Tick()
		tr.inbox = append(tr.inbox, Packet{From: 2, Payload: encodeSingleInput(InputFrame{Tick: tick, Peer: 2})})
		if tick > 0 {
			tr.inbox = append(tr.inbox, Packet{From: 2, Payload: encodeChecksumMsg(tick-1, 2, 0xdead)})
		}
		if err := s.Step(InputFrame{}); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	if want := steps / interval; reported < want-2 || reported > want {
		t.Fatalf("OnDesync ran %d times, want about %d", reported, want)
	}
	if got := s.Stats().Desyncs; got != reported {
		t.Fatalf("Stats().Desyncs = %d, OnDesync ran %d times", got, reported)
	}
	if n := len(s.desyncs); n != maxDesyncHistory {
		t.Fatalf("desyncs holds %d events, want %d", n, maxDesyncHistory)
	}
	if d := s.Desyncs(); d[len(d)-1].Tick+interval < s.Tick()-2 {
		t.Fatalf("latest kept desync is at tick %d at tick %d, want the most recent", d[len(d)-1].Tick, s.Tick())
	}
}

// TestLockstepDesyncsStayBounded has a member send a mismatching checksum for every checksum tick: OnDesync sees every one while Desyncs keeps the latest maxDesyncHistory.
func TestLockstepDesyncsStayBounded(t *testing.T) {
	tr := &memberTransport{id: 1}
	reported := 0
	s := NewLockstepSession(LockstepConfig{Transport: tr, Sim: &memberSim{}, Peers: []PeerID{1, 2}, ChecksumInterval: 1, OnDesync: func(DesyncEvent) { reported++ }})
	const steps = 1000
	for i := range uint32(steps) {
		tick := s.Tick()
		tr.inbox = append(tr.inbox, Packet{From: 2, Payload: encodeSingleInput(InputFrame{Tick: tick, Peer: 2})})
		if tick > 0 {
			tr.inbox = append(tr.inbox, Packet{From: 2, Payload: encodeChecksumMsg(tick-1, 2, 0xdead)})
		}
		if err := s.SubmitInput(InputFrame{Tick: tick}); err != nil {
			t.Fatal(err)
		}
		if !s.Step() {
			t.Fatalf("step %d did not run", i)
		}
	}
	if reported < steps-2 {
		t.Fatalf("OnDesync ran %d times, want about %d", reported, steps)
	}
	if n := len(s.Desyncs()); n != maxDesyncHistory {
		t.Fatalf("Desyncs holds %d events, want %d", n, maxDesyncHistory)
	}
}

// unreachableTransport delivers nothing and fails every send to the peers in down with ErrDisconnected, as a transport does for a peer that has dropped.
type unreachableTransport struct {
	memberTransport
	down map[PeerID]bool
}

func (u *unreachableTransport) Send(to PeerID, ch Channel, payload []byte) error {
	if u.down[to] {
		return ErrDisconnected
	}
	return u.memberTransport.Send(to, ch, payload)
}

// TestRollbackStallNamesUnreachablePeer has peer 2 drop: its transport fails every send and it sends nothing. A stalled session queues nothing more for it, so ErrSessionBacklog never comes; the stall error and Waiting name peer 2 instead, and once the host removes it the match resumes.
func TestRollbackStallNamesUnreachablePeer(t *testing.T) {
	const maxRB = 8
	tr := &unreachableTransport{memberTransport: memberTransport{id: 1}, down: map[PeerID]bool{2: true}}
	s := NewRollbackSession(RollbackConfig{Transport: tr, Sim: &memberSim{}, Peers: []PeerID{1, 2}, MaxRollback: maxRB})
	var err error
	for range 6000 {
		if err = s.Step(InputFrame{Bits: 1}); err != nil && !errors.Is(err, ErrRollbackStalled) {
			t.Fatalf("Step = %v, want only ErrRollbackStalled", err)
		}
	}
	if !errors.Is(err, ErrRollbackStalled) || !strings.Contains(err.Error(), "peer 2") {
		t.Fatalf("Step = %v, want ErrRollbackStalled naming peer 2", err)
	}
	if got := s.Waiting(); !slices.Equal(got, []PeerID{2}) {
		t.Fatalf("Waiting = %v, want [2]", got)
	}
	if _, err := s.RemovePeer(2); err != nil {
		t.Fatalf("RemovePeer: %v", err)
	}
	before := s.Tick()
	for i := range 20 {
		if err := s.Step(InputFrame{Bits: 1}); err != nil {
			t.Fatalf("step %d after removing peer 2: %v", i, err)
		}
	}
	if s.Tick() != before+20 || len(s.Waiting()) != 0 {
		t.Fatalf("tick %d, waiting %v after removing peer 2, want tick %d and none", s.Tick(), s.Waiting(), before+20)
	}
}

// TestRollbackStalledStepReportsBacklog overflows the outbox of an unreachable peer 3 while the session stalls on a silent peer 2: the stalled Step returns ErrSessionBacklog on its own, which is reported only once, so a caller that skips ErrRollbackStalled still sees it.
func TestRollbackStalledStepReportsBacklog(t *testing.T) {
	const maxRB = 8
	tr := &unreachableTransport{memberTransport: memberTransport{id: 1}, down: map[PeerID]bool{3: true}}
	s := NewRollbackSession(RollbackConfig{Transport: tr, Sim: &memberSim{}, Peers: []PeerID{1, 2, 3}, MaxRollback: maxRB})
	for range maxRB + 1 {
		if err := s.Step(InputFrame{Bits: 1}); err != nil && !errors.Is(err, ErrRollbackStalled) {
			t.Fatal(err)
		}
	}
	for len(s.outbox[3]) < maxOutbox {
		s.outbox[3] = append(s.outbox[3], []byte{0})
	}
	if _, err := s.AddPeer(4); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}
	err := s.Step(InputFrame{Bits: 1})
	if !errors.Is(err, ErrSessionBacklog) || errors.Is(err, ErrRollbackStalled) || !strings.Contains(err.Error(), "peer 3") {
		t.Fatalf("stalled Step = %v, want ErrSessionBacklog for peer 3 alone", err)
	}
	if err := s.Step(InputFrame{Bits: 1}); !errors.Is(err, ErrRollbackStalled) {
		t.Fatalf("next Step = %v, want ErrRollbackStalled", err)
	}
}
