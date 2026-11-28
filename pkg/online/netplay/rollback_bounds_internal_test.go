package netplay

import (
	"errors"
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
