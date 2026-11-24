//go:build !js

package net

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"testing"
	"time"
)

func TestBoundedQueuesEvictOldestAndStayFIFO(t *testing.T) {
	var pq packetQueue
	drops := 0
	for i := range 10 {
		if pq.pushBounded(Packet{Seq: uint32(i)}, 4) {
			drops++
		}
	}
	if drops != 6 || pq.len() != 4 {
		t.Fatalf("packetQueue pushBounded drops=%d len=%d, want 6 and 4", drops, pq.len())
	}
	for want := uint32(6); want < 10; want++ {
		p, ok := pq.pop()
		if !ok || p.Seq != want {
			t.Fatalf("packetQueue pop = (%d, %v), want %d", p.Seq, ok, want)
		}
	}

	var eq eventQueue
	evDrops := 0
	for i := range 10 {
		if eq.pushBounded(PeerEvent{Kind: PeerJoined, Peer: PeerID(i + 1)}, 4) {
			evDrops++
		}
	}
	if evDrops != 6 || eq.len() != 4 {
		t.Fatalf("eventQueue pushBounded drops=%d len=%d, want 6 and 4", evDrops, eq.len())
	}
	for want := PeerID(7); want <= 10; want++ {
		ev, ok := eq.pop()
		if !ok || ev.Peer != want {
			t.Fatalf("eventQueue pop = (%+v, %v), want peer %d", ev, ok, want)
		}
	}

	// Serial-number tick comparisons across uint32 wraparound.
	if !tickAfter(0, math.MaxUint32) || !tickBefore(math.MaxUint32, 0) {
		t.Fatal("tickAfter/tickBefore failed across uint32 wrap")
	}
	if d := tickDelta(1, math.MaxUint32); d != 2 {
		t.Fatalf("tickDelta(1, MaxUint32) = %d, want 2", d)
	}
}

type wrapSim struct {
	val uint64
}

func (s *wrapSim) Step(inputs []InputFrame) {
	for _, in := range inputs {
		s.val = s.val*131 + uint64(in.Peer)*17 + in.Bits + uint64(in.Tick&0xff) + 1
	}
}

func (s *wrapSim) Save() []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], s.val)
	return b[:]
}

func (s *wrapSim) Load(b []byte) error {
	s.val = binary.BigEndian.Uint64(b[:8])
	return nil
}

func (s *wrapSim) Checksum() uint64 { return s.val }

func TestLockstepAndRollbackTickWraparound(t *testing.T) {
	// #546: stepping, predicting, rolling back, and pruning across uint32 wraparound.
	t.Run("lockstep", func(t *testing.T) {
		hub := NewLoopbackHub(LoopbackConfig{})
		t1, t2 := hub.AddPeer(1), hub.AddPeer(2)
		sim1, sim2 := &wrapSim{}, &wrapSim{}
		s1 := NewLockstepSession(LockstepConfig{Transport: t1, Sim: sim1, Peers: []PeerID{1, 2}, ChecksumInterval: 2})
		s2 := NewLockstepSession(LockstepConfig{Transport: t2, Sim: sim2, Peers: []PeerID{1, 2}, ChecksumInterval: 2})
		start := uint32(math.MaxUint32 - 2)
		s1.tick = start
		s2.tick = start

		for i := range 6 {
			tick := start + uint32(i)
			_ = s1.SubmitInput(InputFrame{Tick: tick, Bits: uint64(i + 1)})
			_ = s2.SubmitInput(InputFrame{Tick: tick, Bits: uint64(i + 10)})
			s1.Poll()
			s2.Poll()
			if !s1.Step() || !s2.Step() {
				t.Fatalf("lockstep stalled at wrap step %d (tick=%d)", i, tick)
			}
		}
		if s1.Tick() != 3 || s2.Tick() != 3 || sim1.val != sim2.val {
			t.Fatalf("lockstep wrap result: ticks=(%d,%d) vals=(%d,%d)", s1.Tick(), s2.Tick(), sim1.val, sim2.val)
		}
		if len(s1.Desyncs()) != 0 || len(s2.Desyncs()) != 0 {
			t.Fatalf("unexpected desyncs across wraparound: %v / %v", s1.Desyncs(), s2.Desyncs())
		}
	})

	t.Run("rollback", func(t *testing.T) {
		hub := NewLoopbackHub(LoopbackConfig{Latency: 32 * time.Millisecond, Seed: 5})
		t1, t2 := hub.AddPeer(1), hub.AddPeer(2)
		sim1, sim2 := &wrapSim{}, &wrapSim{}
		r1 := NewRollbackSession(RollbackConfig{Transport: t1, Sim: sim1, Peers: []PeerID{1, 2}, MaxRollback: 8, ChecksumInterval: 2})
		r2 := NewRollbackSession(RollbackConfig{Transport: t2, Sim: sim2, Peers: []PeerID{1, 2}, MaxRollback: 8, ChecksumInterval: 2})
		start := uint32(math.MaxUint32 - 2)
		r1.currentTick = start
		r2.currentTick = start

		for i := range 8 {
			if err := r1.Step(InputFrame{Bits: uint64(i * 3)}); err != nil {
				t.Fatalf("r1.Step(%d): %v", i, err)
			}
			if err := r2.Step(InputFrame{Bits: uint64(i * 5)}); err != nil {
				t.Fatalf("r2.Step(%d): %v", i, err)
			}
			hub.Advance(16 * time.Millisecond)
		}
		hub.Advance(100 * time.Millisecond)
		if err := r1.Poll(); err != nil {
			t.Fatalf("r1.Poll: %v", err)
		}
		if err := r2.Poll(); err != nil {
			t.Fatalf("r2.Poll: %v", err)
		}
		if sim1.val != sim2.val {
			t.Fatalf("rollback diverged across wraparound: %d vs %d", sim1.val, sim2.val)
		}
		if r1.Stats().Rollbacks == 0 {
			t.Fatal("expected at least one rollback across wraparound")
		}
		if len(r1.Desyncs()) != 0 || len(r2.Desyncs()) != 0 {
			t.Fatalf("unexpected rollback desyncs: %v / %v", r1.Desyncs(), r2.Desyncs())
		}
	})
}

func TestWebSocketControlFrameValidation(t *testing.T) {
	// RFC 6455 §5.5: control frames must not be fragmented and must have payload <= 125 bytes.
	fragPing := []byte{wsOpPing, 0x00}
	if _, _, _, _, err := readWSFrameLimit(bufio.NewReader(bytes.NewReader(fragPing)), maxWSMessageSize); !errors.Is(err, ErrInvalidPacket) {
		t.Fatalf("fragmented ping err = %v, want ErrInvalidPacket", err)
	}
	bigPing := []byte{0x80 | wsOpPing, 126, 0, 126}
	if _, _, _, _, err := readWSFrameLimit(bufio.NewReader(bytes.NewReader(bigPing)), maxWSMessageSize); !errors.Is(err, ErrInvalidPacket) {
		t.Fatalf("oversized ping err = %v, want ErrInvalidPacket", err)
	}
}

func TestWebSocketInboxBackpressure(t *testing.T) {
	// #546: native WebSocket inboxes apply TCP backpressure rather than growing
	// unboundedly or dropping reliable packets.
	wsInboxLimitMu.Lock()
	origLimit := wsMaxInboxPackets
	wsMaxInboxPackets = 4
	wsInboxLimitMu.Unlock()
	defer func() {
		wsInboxLimitMu.Lock()
		wsMaxInboxPackets = origLimit
		wsInboxLimitMu.Unlock()
	}()

	srv, err := ListenWebSocket("127.0.0.1:0", 1, Auth{})
	if err != nil {
		t.Fatalf("ListenWebSocket: %v", err)
	}
	defer srv.Close()

	cli, err := DialWebSocket("ws://"+srv.Addr().String(), nil)
	if err != nil {
		t.Fatalf("DialWebSocket: %v", err)
	}
	defer cli.Close()

	const total = 12
	for i := range total {
		if err := cli.Send(1, ChannelReliableOrdered, []byte{byte(i)}); err != nil {
			t.Fatalf("Send %d: %v", i, err)
		}
	}

	// Give the server read loop a moment to hit the 4-packet cap.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		srv.mu.Lock()
		n := srv.inbox.len()
		srv.mu.Unlock()
		if n >= 4 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	srv.mu.Lock()
	buffered := srv.inbox.len()
	srv.mu.Unlock()
	if buffered > 4 {
		t.Fatalf("server inbox grew past cap 4: got %d", buffered)
	}

	// Draining Recv must deliver all 12 reliable packets in exact order.
	got := make([]byte, 0, total)
	deadline = time.Now().Add(2 * time.Second)
	for len(got) < total && time.Now().Before(deadline) {
		if pkt, ok := srv.Recv(); ok {
			got = append(got, pkt.Payload[0])
		} else {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if len(got) != total {
		t.Fatalf("received %d/%d reliable packets under backpressure", len(got), total)
	}
	for i, b := range got {
		if int(b) != i {
			t.Fatalf("packet %d = %d, want %d", i, b, i)
		}
	}
}
