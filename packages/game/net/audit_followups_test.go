package net_test

import (
	"encoding/binary"
	"fmt"
	"slices"
	"testing"

	gamenet "github.com/nsgo-dev/game/packages/game/net"
)

type peerSumSim struct {
	acc uint64
}

func (s *peerSumSim) Step(inputs []gamenet.InputFrame) {
	for _, in := range inputs {
		s.acc = s.acc*31 + uint64(in.Peer)*7 + in.Bits + 1
	}
}

func (s *peerSumSim) Save() []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], s.acc)
	return buf[:]
}

func (s *peerSumSim) Load(b []byte) error {
	if len(b) < 8 {
		return fmt.Errorf("short sim state")
	}
	s.acc = binary.BigEndian.Uint64(b[:8])
	return nil
}

func (s *peerSumSim) Checksum() uint64 { return s.acc }

func TestLockstepAndRollbackLateJoinAndRemovePeer(t *testing.T) {
	// #546: sessions must allow adding late-joining peers and removing departed
	// peers instead of freezing the peer list forever at construction.
	t.Run("lockstep", func(t *testing.T) {
		hub := gamenet.NewLoopbackHub(gamenet.LoopbackConfig{})
		t1 := hub.AddPeer(1)
		t2 := hub.AddPeer(2)

		s1 := gamenet.NewLockstepSession(gamenet.LockstepConfig{Transport: t1, Sim: &peerSumSim{}, Peers: []gamenet.PeerID{1, 2}})
		s2 := gamenet.NewLockstepSession(gamenet.LockstepConfig{Transport: t2, Sim: &peerSumSim{}, Peers: []gamenet.PeerID{1, 2}})

		// Late peer 3 joins at tick 0.
		t3 := hub.AddPeer(3)
		s3 := gamenet.NewLockstepSession(gamenet.LockstepConfig{Transport: t3, Sim: &peerSumSim{}, Peers: []gamenet.PeerID{1, 2, 3}})
		s1.AddPeer(3)
		s2.AddPeer(3)
		if !slices.Equal(s1.Peers(), []gamenet.PeerID{1, 2, 3}) {
			t.Fatalf("s1.Peers = %v, want [1 2 3]", s1.Peers())
		}

		// All three peers advance tick 0 together.
		_ = s1.SubmitInput(gamenet.InputFrame{Tick: 0, Bits: 1})
		_ = s2.SubmitInput(gamenet.InputFrame{Tick: 0, Bits: 2})
		_ = s3.SubmitInput(gamenet.InputFrame{Tick: 0, Bits: 4})
		for _, s := range []*gamenet.LockstepSession{s1, s2, s3} {
			s.Poll()
			if !s.Step() {
				t.Fatal("lockstep did not advance after all 3 peers submitted tick 0")
			}
		}

		// Peer 3 drops; removing peer 3 allows peers 1 and 2 to keep stepping.
		s1.RemovePeer(3)
		s2.RemovePeer(3)
		_ = s1.SubmitInput(gamenet.InputFrame{Tick: 1, Bits: 8})
		_ = s2.SubmitInput(gamenet.InputFrame{Tick: 1, Bits: 16})
		s1.Poll()
		s2.Poll()
		if !s1.Step() || !s2.Step() {
			t.Fatal("lockstep stayed stalled after removing departed peer 3")
		}
	})

	t.Run("rollback", func(t *testing.T) {
		hub := gamenet.NewLoopbackHub(gamenet.LoopbackConfig{})
		t1 := hub.AddPeer(1)
		t2 := hub.AddPeer(2)

		sim1 := &peerSumSim{}
		sim2 := &peerSumSim{}
		r1 := gamenet.NewRollbackSession(gamenet.RollbackConfig{Transport: t1, Sim: sim1, Peers: []gamenet.PeerID{1, 2}, InputDelay: 1})
		r2 := gamenet.NewRollbackSession(gamenet.RollbackConfig{Transport: t2, Sim: sim2, Peers: []gamenet.PeerID{1, 2}, InputDelay: 1})

		// Add late peer 3 before stepping.
		t3 := hub.AddPeer(3)
		sim3 := &peerSumSim{}
		r3 := gamenet.NewRollbackSession(gamenet.RollbackConfig{Transport: t3, Sim: sim3, Peers: []gamenet.PeerID{1, 2, 3}, InputDelay: 1})
		r1.AddPeer(3)
		r2.AddPeer(3)

		for tick := range 4 {
			_ = r1.Step(gamenet.InputFrame{Bits: uint64(tick + 1)})
			_ = r2.Step(gamenet.InputFrame{Bits: uint64(tick + 10)})
			_ = r3.Step(gamenet.InputFrame{Bits: uint64(tick + 100)})
		}
		_ = r1.Poll()
		_ = r2.Poll()
		_ = r3.Poll()
		if sim1.acc != sim2.acc || sim2.acc != sim3.acc {
			t.Fatalf("3-peer rollback diverged: %d vs %d vs %d", sim1.acc, sim2.acc, sim3.acc)
		}

		// Peer 3 disconnects; removing peer 3 lets r1/r2 advance confirmedTick.
		r1.RemovePeer(3)
		r2.RemovePeer(3)
		before := r1.ConfirmedTick()
		_ = r1.Step(gamenet.InputFrame{Bits: 99})
		_ = r2.Step(gamenet.InputFrame{Bits: 88})
		_ = r1.Poll()
		_ = r2.Poll()
		if r1.ConfirmedTick() <= before {
			t.Fatalf("ConfirmedTick did not advance after RemovePeer(3): before=%d after=%d", before, r1.ConfirmedTick())
		}
	})
}

func TestLoopbackBoundedUnreliableInboxCapsMemory(t *testing.T) {
	// #546: unread unreliable packets must be capped instead of growing the inbox without bound.
	hub := gamenet.NewLoopbackHub(gamenet.LoopbackConfig{})
	p1 := hub.AddPeer(1)
	p2 := hub.AddPeer(2)

	const total = 5000
	for i := range total {
		var buf [4]byte
		binary.BigEndian.PutUint32(buf[:], uint32(i))
		if err := p1.Send(2, gamenet.ChannelUnreliable, buf[:]); err != nil {
			t.Fatalf("Send %d: %v", i, err)
		}
	}

	var count int
	var first, last uint32
	for {
		pkt, ok := p2.Recv()
		if !ok {
			break
		}
		v := binary.BigEndian.Uint32(pkt.Payload)
		if count == 0 {
			first = v
		}
		last = v
		count++
	}
	if count >= total {
		t.Fatalf("inbox was unbounded: received all %d packets", count)
	}
	if count == 0 || last != total-1 || first == 0 {
		t.Fatalf("expected freshest bounded window ending at %d, got count=%d first=%d last=%d", total-1, count, first, last)
	}
}
