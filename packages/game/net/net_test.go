package net_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"testing"
	"time"

	gamenet "github.com/nsgo-dev/game/packages/game/net"
)

func TestLoopbackReliableAndUnreliableChannels(t *testing.T) {
	hub := gamenet.NewLoopbackHub(gamenet.LoopbackConfig{
		Latency:  50 * time.Millisecond,
		Jitter:   20 * time.Millisecond,
		LossRate: 0.4,
		Seed:     42,
	})
	p1 := hub.AddPeer(1)
	p2 := hub.AddPeer(2)

	const total = 100
	for i := range total {
		payload := []byte{byte(i)}
		if err := p1.Send(2, gamenet.ChannelReliableOrdered, payload); err != nil {
			t.Fatalf("reliable send %d failed: %v", i, err)
		}
		if err := p1.Send(2, gamenet.ChannelUnreliable, payload); err != nil {
			t.Fatalf("unreliable send %d failed: %v", i, err)
		}
	}

	// Before latency elapses, nothing should have arrived.
	hub.Advance(10 * time.Millisecond)
	if _, ok := p2.Recv(); ok {
		t.Fatal("expected no packets before minimum latency elapsed")
	}

	// Advance past max latency+jitter in steps.
	var relGot []int
	var unrelGot []int
	for range 20 {
		hub.Advance(10 * time.Millisecond)
		for {
			pkt, ok := p2.Recv()
			if !ok {
				break
			}
			if pkt.From != 1 {
				t.Fatalf("got packet from %d, want 1", pkt.From)
			}
			val := int(pkt.Payload[0])
			switch pkt.Channel {
			case gamenet.ChannelReliableOrdered:
				relGot = append(relGot, val)
			case gamenet.ChannelUnreliable:
				unrelGot = append(unrelGot, val)
			}
		}
	}

	// Reliable ordered: 100% delivery in exact send order despite 40% loss and 20ms jitter.
	if len(relGot) != total {
		t.Fatalf("reliable received %d packets, want %d", len(relGot), total)
	}
	for i, v := range relGot {
		if v != i {
			t.Fatalf("reliable out of order at index %d: got %d, want %d", i, v, i)
		}
	}

	// Unreliable: some packets must have been dropped and some delivered.
	if len(unrelGot) == 0 || len(unrelGot) == total {
		t.Fatalf("expected partial unreliable delivery with 40%% loss, got %d/%d", len(unrelGot), total)
	}

	// Determinism check: re-running with the same seed produces identical unreliable sequence.
	hub2 := gamenet.NewLoopbackHub(gamenet.LoopbackConfig{
		Latency:  50 * time.Millisecond,
		Jitter:   20 * time.Millisecond,
		LossRate: 0.4,
		Seed:     42,
	})
	q1 := hub2.AddPeer(1)
	q2 := hub2.AddPeer(2)
	for i := range total {
		_ = q1.Send(2, gamenet.ChannelReliableOrdered, []byte{byte(i)})
		_ = q1.Send(2, gamenet.ChannelUnreliable, []byte{byte(i)})
	}
	hub2.Advance(10 * time.Millisecond)
	var unrelGot2 []int
	for range 20 {
		hub2.Advance(10 * time.Millisecond)
		for {
			pkt, ok := q2.Recv()
			if !ok {
				break
			}
			if pkt.Channel == gamenet.ChannelUnreliable {
				unrelGot2 = append(unrelGot2, int(pkt.Payload[0]))
			}
		}
	}
	if len(unrelGot) != len(unrelGot2) {
		t.Fatalf("non-deterministic delivery count: %d vs %d", len(unrelGot), len(unrelGot2))
	}
	for i := range unrelGot {
		if unrelGot[i] != unrelGot2[i] {
			t.Fatalf("non-deterministic delivery at %d: %d vs %d", i, unrelGot[i], unrelGot2[i])
		}
	}
}

func TestReplicationSpawnUpdateDespawnAndInterpolation(t *testing.T) {
	hub := gamenet.NewLoopbackHub(gamenet.LoopbackConfig{
		Latency: 16 * time.Millisecond,
		Seed:    7,
	})
	srvT := hub.AddPeer(1)
	cliT := hub.AddPeer(2)

	srv := gamenet.NewServerWorld(srvT)
	cli := gamenet.NewClientWorld(cliT, 1)

	var spawned []gamenet.EntityID
	var despawned []gamenet.EntityID
	cli.OnSpawn = func(e gamenet.EntityState) { spawned = append(spawned, e.ID) }
	cli.OnDespawn = func(id gamenet.EntityID) { despawned = append(despawned, id) }

	e1 := srv.Spawn(gamenet.EntityState{
		ComponentState: gamenet.ComponentState{
			Kind:     1,
			X:        0,
			Y:        10,
			VX:       10,
			VY:       0,
			Rotation: 0,
			Flags:    0x3,
			Props:    []byte("hero"),
		},
	})
	e2 := srv.Spawn(gamenet.EntityState{
		ComponentState: gamenet.ComponentState{
			Kind:     2,
			X:        100,
			Y:        200,
			Rotation: 3.0, // near +pi for wrap-around check
		},
	})

	// Step ticks 1..5, moving e1 linearly and rotating e2 across +pi/-pi.
	for tick := uint32(1); tick <= 5; tick++ {
		srv.Update(e1, gamenet.EntityState{
			ComponentState: gamenet.ComponentState{
				Kind:     1,
				X:        float32(tick * 10),
				Y:        10 + float32(tick*2),
				VX:       10,
				VY:       2,
				Rotation: float32(tick) * 0.25,
				Flags:    0x3,
				Props:    []byte("hero"),
			},
		})
		if tick == 4 {
			// Cross from +3.0 rad to -3.0 rad (shortest arc is +0.2831853 rad).
			srv.Update(e2, gamenet.EntityState{
				ComponentState: gamenet.ComponentState{
					Kind:     2,
					X:        100,
					Y:        200,
					Rotation: -3.0,
				},
			})
		}
		if err := srv.Step(tick); err != nil {
			t.Fatalf("server step %d: %v", tick, err)
		}
		hub.Advance(16 * time.Millisecond)
		cli.Poll()
		hub.Advance(16 * time.Millisecond) // allow ack to reach server
	}

	if len(spawned) != 2 || spawned[0] != e1 || spawned[1] != e2 {
		t.Fatalf("OnSpawn = %v, want [%d %d]", spawned, e1, e2)
	}
	stats := srv.Stats()
	if stats.FullSnapshotsSent < 1 || stats.DeltaSnapshotsSent < 3 {
		t.Fatalf("expected full then delta snapshots, got stats=%+v", stats)
	}

	// Verify properties synced accurately at tick 5.
	gotE1, ok := cli.Entity(e1)
	if !ok {
		t.Fatal("e1 missing on client")
	}
	if gotE1.X != 50 || gotE1.Y != 20 || !bytes.Equal(gotE1.Props, []byte("hero")) {
		t.Fatalf("unexpected e1 state: %+v", gotE1)
	}

	// Verify snapshot interpolation at tick 2.5 (halfway between tick 2 and tick 3).
	interp := cli.Interpolate(2.5)
	if len(interp) != 2 {
		t.Fatalf("interpolated count = %d, want 2", len(interp))
	}
	if math.Abs(float64(interp[0].X-25.0)) > 1e-4 || math.Abs(float64(interp[0].Y-15.0)) > 1e-4 {
		t.Fatalf("interpolated e1 pos at 2.5 = (%f, %f), want (25, 15)", interp[0].X, interp[0].Y)
	}
	if math.Abs(float64(interp[0].Rotation-0.625)) > 1e-4 {
		t.Fatalf("interpolated e1 rot at 2.5 = %f, want 0.625", interp[0].Rotation)
	}

	// Verify shortest-arc rotation interpolation across +pi/-pi at tick 3.5.
	interpWrap := cli.Interpolate(3.5)
	// Halfway between +3.0 and -3.0 via +pi is ~3.14159265.
	if math.Abs(float64(interpWrap[1].Rotation)-math.Pi) > 1e-3 {
		t.Fatalf("expected shortest-arc wrap near pi, got %f", interpWrap[1].Rotation)
	}

	// Despawn e2 and verify OnDespawn fires.
	srv.Despawn(e2)
	if err := srv.Step(6); err != nil {
		t.Fatalf("server step 6: %v", err)
	}
	hub.Advance(16 * time.Millisecond)
	cli.Poll()
	if len(despawned) != 1 || despawned[0] != e2 {
		t.Fatalf("OnDespawn = %v, want [%d]", despawned, e2)
	}
	if _, exists := cli.Entity(e2); exists {
		t.Fatal("e2 still present on client after despawn")
	}
}

func TestReplicationPredictionAndReconciliation(t *testing.T) {
	hub := gamenet.NewLoopbackHub(gamenet.LoopbackConfig{Latency: 0, Seed: 1})
	srvT := hub.AddPeer(1)
	cliT := hub.AddPeer(2)

	srv := gamenet.NewServerWorld(srvT)
	cli := gamenet.NewClientWorld(cliT, 1)

	playerID := srv.Spawn(gamenet.EntityState{
		ComponentState: gamenet.ComponentState{Kind: 1, X: 0, Y: 0},
	})
	_ = srv.Step(1)
	cli.Poll()

	applyMove := func(st *gamenet.EntityState, in any) {
		dx := in.(float32)
		st.X += dx
	}

	// Client predicts 3 ticks ahead (ticks 2, 3, 4) moving +5 per tick -> X = 15.
	cli.Predict(playerID, float32(5), applyMove)
	cli.Predict(playerID, float32(5), applyMove)
	cli.Predict(playerID, float32(5), applyMove)

	pred, ok := cli.PredictedState(playerID)
	if !ok || pred.X != 15 {
		t.Fatalf("predicted X = %v (ok=%v), want 15", pred.X, ok)
	}
	if cli.PendingPredictions(playerID) != 3 {
		t.Fatalf("pending = %d, want 3", cli.PendingPredictions(playerID))
	}

	// Server confirms tick 2 matching prediction (X=5): reconciliation keeps X=15 with 0 error.
	srv.Update(playerID, gamenet.EntityState{ComponentState: gamenet.ComponentState{Kind: 1, X: 5}})
	_ = srv.Step(2)
	cli.Poll()
	pred, _ = cli.PredictedState(playerID)
	if pred.X != 15 || cli.LastReconcileError() != 0 || cli.PendingPredictions(playerID) != 2 {
		t.Fatalf("exact reconcile: X=%f err=%f pending=%d", pred.X, cli.LastReconcileError(), cli.PendingPredictions(playerID))
	}

	// Server applies authoritative knockback at tick 3 (X=7 instead of 10).
	// Client should snap to 7 and replay remaining tick 4 (+5) -> X=12, with error=3.
	srv.Update(playerID, gamenet.EntityState{ComponentState: gamenet.ComponentState{Kind: 1, X: 7}})
	_ = srv.Step(3)
	cli.Poll()
	pred, _ = cli.PredictedState(playerID)
	if math.Abs(float64(pred.X-12)) > 1e-4 {
		t.Fatalf("reconciled X = %f, want 12", pred.X)
	}
	if math.Abs(float64(cli.LastReconcileError()-3)) > 1e-4 {
		t.Fatalf("reconcile error = %f, want 3", cli.LastReconcileError())
	}
	if cli.PendingPredictions(playerID) != 1 {
		t.Fatalf("pending = %d, want 1", cli.PendingPredictions(playerID))
	}
}

func TestReplication16EntityBandwidth(t *testing.T) {
	hub := gamenet.NewLoopbackHub(gamenet.LoopbackConfig{
		Latency: 16 * time.Millisecond,
		Seed:    99,
	})
	srvT := hub.AddPeer(1)
	cliT := hub.AddPeer(2)

	srv := gamenet.NewServerWorld(srvT)
	cli := gamenet.NewClientWorld(cliT, 1)

	const entityCount = 16
	ids := make([]gamenet.EntityID, entityCount)
	for i := range entityCount {
		ids[i] = srv.Spawn(gamenet.EntityState{
			ComponentState: gamenet.ComponentState{
				Kind:     uint16(i%4 + 1),
				X:        float32(i * 32),
				Y:        float32(i * 16),
				VX:       4.0,
				VY:       -2.0,
				Rotation: float32(i) * 0.1,
				Flags:    1,
				Props:    []byte(fmt.Sprintf("e%02d", i)),
			},
		})
	}

	const ticks = 120
	for tick := uint32(1); tick <= ticks; tick++ {
		for i, id := range ids {
			st, _ := srv.Entity(id)
			// All 16 entities actively move every tick; half also rotate each tick.
			st.X += st.VX * 0.05
			st.Y += st.VY * 0.05
			if i%2 == 0 {
				st.Rotation += 0.02
			}
			srv.Update(id, st)
		}
		if err := srv.Step(tick); err != nil {
			t.Fatalf("step %d: %v", tick, err)
		}
		hub.Advance(16 * time.Millisecond)
		cli.Poll()
		hub.Advance(16 * time.Millisecond)
	}

	stats := srv.Stats()
	bytesPerTick := stats.AvgBytesPerTick
	kbPerSec20Hz := bytesPerTick * 20.0 / 1024.0
	kbPerSec60Hz := bytesPerTick * 60.0 / 1024.0

	t.Logf("16-entity active scene replication: %d ticks, total=%d B, full=%d, delta=%d, avg=%.1f B/tick (%.2f KB/s @20Hz, %.2f KB/s @60Hz)",
		ticks, stats.BytesSent, stats.FullSnapshotsSent, stats.DeltaSnapshotsSent,
		bytesPerTick, kbPerSec20Hz, kbPerSec60Hz)

	if stats.DeltaSnapshotsSent < ticks-5 {
		t.Fatalf("expected predominantly delta snapshots, got full=%d delta=%d", stats.FullSnapshotsSent, stats.DeltaSnapshotsSent)
	}
	if bytesPerTick > 512 {
		t.Fatalf("16-entity delta bandwidth too high: %.1f B/tick (want <= 512 B/tick)", bytesPerTick)
	}
}

func BenchmarkReplication16Entities(b *testing.B) {
	hub := gamenet.NewLoopbackHub(gamenet.LoopbackConfig{Latency: 0, Seed: 1})
	srvT := hub.AddPeer(1)
	cliT := hub.AddPeer(2)
	srv := gamenet.NewServerWorld(srvT)
	cli := gamenet.NewClientWorld(cliT, 1)

	ids := make([]gamenet.EntityID, 16)
	for i := range 16 {
		ids[i] = srv.Spawn(gamenet.EntityState{
			ComponentState: gamenet.ComponentState{
				Kind:  uint16(i%4 + 1),
				X:     float32(i * 10),
				Y:     float32(i * 10),
				VX:    3,
				VY:    -1,
				Flags: 1,
				Props: []byte("ent"),
			},
		})
	}
	// Warm up baseline ack.
	_ = srv.Step(1)
	cli.Poll()

	startBytes := srv.Stats().BytesSent
	b.ResetTimer()
	for i := range b.N {
		tick := uint32(i + 2)
		for j, id := range ids {
			st, _ := srv.Entity(id)
			st.X += 1.5
			st.Y -= 0.5
			if j%2 == 0 {
				st.Rotation += 0.01
			}
			srv.Update(id, st)
		}
		_ = srv.Step(tick)
		cli.Poll()
	}
	b.StopTimer()
	deltaBytes := srv.Stats().BytesSent - startBytes
	bpt := float64(deltaBytes) / float64(max(1, b.N))
	b.ReportMetric(bpt, "B/tick")
	b.ReportMetric(bpt*20.0/1024.0, "KB/s@20Hz")
}

// deterministicArenaSim is a fixed-point deterministic 2-player arena simulation
// implementing SimState for testing LockstepSession and RollbackSession.
type deterministicArenaSim struct {
	tick    uint32
	players [2]struct {
		x, y   int32
		vx, vy int32
		score  uint32
	}
}

func (s *deterministicArenaSim) Step(inputs []gamenet.InputFrame) {
	for _, in := range inputs {
		idx := int(in.Peer - 1)
		if idx < 0 || idx >= len(s.players) {
			continue
		}
		p := &s.players[idx]
		p.vx = int32(in.Axes[0])
		p.vy = int32(in.Axes[1])
		if in.Bits&1 != 0 {
			p.vx *= 2 // dash multiplier
		}
		p.x += p.vx
		p.y += p.vy
		if in.Bits&2 != 0 {
			p.score += 10
		}
	}
	// Player-player interaction so remote mispredictions nonlinearly affect both players.
	dx := s.players[0].x - s.players[1].x
	dy := s.players[0].y - s.players[1].y
	if dx*dx+dy*dy < 16 {
		s.players[0].x -= 3
		s.players[1].x += 3
		s.players[0].score++
		s.players[1].score++
	}
	s.tick++
}

func (s *deterministicArenaSim) Save() []byte {
	buf := make([]byte, 4+2*5*4)
	binary.BigEndian.PutUint32(buf[0:4], s.tick)
	off := 4
	for i := range 2 {
		binary.BigEndian.PutUint32(buf[off:off+4], uint32(s.players[i].x))
		binary.BigEndian.PutUint32(buf[off+4:off+8], uint32(s.players[i].y))
		binary.BigEndian.PutUint32(buf[off+8:off+12], uint32(s.players[i].vx))
		binary.BigEndian.PutUint32(buf[off+12:off+16], uint32(s.players[i].vy))
		binary.BigEndian.PutUint32(buf[off+16:off+20], s.players[i].score)
		off += 20
	}
	return buf
}

func (s *deterministicArenaSim) Load(snapshot []byte) error {
	if len(snapshot) != 4+2*5*4 {
		return fmt.Errorf("invalid snapshot size %d", len(snapshot))
	}
	s.tick = binary.BigEndian.Uint32(snapshot[0:4])
	off := 4
	for i := range 2 {
		s.players[i].x = int32(binary.BigEndian.Uint32(snapshot[off : off+4]))
		s.players[i].y = int32(binary.BigEndian.Uint32(snapshot[off+4 : off+8]))
		s.players[i].vx = int32(binary.BigEndian.Uint32(snapshot[off+8 : off+12]))
		s.players[i].vy = int32(binary.BigEndian.Uint32(snapshot[off+12 : off+16]))
		s.players[i].score = binary.BigEndian.Uint32(snapshot[off+16 : off+20])
		off += 20
	}
	return nil
}

func (s *deterministicArenaSim) Checksum() uint64 {
	return gamenet.ChecksumBytes(s.Save())
}

func scriptedInput(peer gamenet.PeerID, tick uint32) gamenet.InputFrame {
	var bits uint64
	if (tick+uint32(peer)*3)%7 == 0 {
		bits |= 1
	}
	if (tick+uint32(peer)*5)%11 == 0 {
		bits |= 2
	}
	ax := int16(((int(tick)*3 + int(peer)*7) % 9) - 4)
	ay := int16(((int(tick)*5 + int(peer)*11) % 7) - 3)
	return gamenet.InputFrame{
		Tick: tick,
		Peer: peer,
		Bits: bits,
		Axes: [2]int16{ax, ay},
	}
}

func TestLockstepSessionAndDesyncDetection(t *testing.T) {
	hub := gamenet.NewLoopbackHub(gamenet.LoopbackConfig{
		Latency: 32 * time.Millisecond,
		Seed:    12,
	})
	t1 := hub.AddPeer(1)
	t2 := hub.AddPeer(2)

	sim1 := &deterministicArenaSim{}
	sim2 := &deterministicArenaSim{}
	ref := &deterministicArenaSim{}

	var desync1, desync2 []gamenet.DesyncEvent
	s1 := gamenet.NewLockstepSession(gamenet.LockstepConfig{
		Transport:        t1,
		Sim:              sim1,
		ChecksumInterval: 10,
		OnDesync:         func(ev gamenet.DesyncEvent) { desync1 = append(desync1, ev) },
	})
	s2 := gamenet.NewLockstepSession(gamenet.LockstepConfig{
		Transport:        t2,
		Sim:              sim2,
		ChecksumInterval: 10,
		OnDesync:         func(ev gamenet.DesyncEvent) { desync2 = append(desync2, ev) },
	})

	const ticks = 40
	for tick := uint32(0); tick < ticks; tick++ {
		in1 := scriptedInput(1, tick)
		in2 := scriptedInput(2, tick)
		ref.Step([]gamenet.InputFrame{in1, in2})

		_ = s1.SubmitInput(in1)
		_ = s2.SubmitInput(in2)

		// Before latency elapses, neither peer can step.
		if s1.Step() || s2.Step() {
			t.Fatalf("lockstep stepped prematurely at tick %d before remote input arrived", tick)
		}

		hub.Advance(32 * time.Millisecond)
		if !s1.Step() || !s2.Step() {
			t.Fatalf("lockstep failed to step at tick %d after remote input arrived", tick)
		}
	}
	hub.Advance(64 * time.Millisecond)
	s1.Poll()
	s2.Poll()

	if sim1.Checksum() != ref.Checksum() || sim2.Checksum() != ref.Checksum() {
		t.Fatalf("lockstep checksums diverged: p1=%x p2=%x ref=%x", sim1.Checksum(), sim2.Checksum(), ref.Checksum())
	}
	if len(desync1) != 0 || len(desync2) != 0 {
		t.Fatalf("unexpected desyncs: p1=%v p2=%v", desync1, desync2)
	}

	// Tamper with peer 2's state and run past the next checksum interval (tick 49).
	sim2.players[1].x += 999
	for tick := uint32(ticks); tick < ticks+10; tick++ {
		_ = s1.SubmitInput(scriptedInput(1, tick))
		_ = s2.SubmitInput(scriptedInput(2, tick))
		hub.Advance(32 * time.Millisecond)
		s1.Step()
		s2.Step()
	}
	hub.Advance(64 * time.Millisecond)
	s1.Poll()
	s2.Poll()

	if len(desync1) == 0 || len(desync2) == 0 {
		t.Fatalf("expected desync detection after state tampering, got p1=%d p2=%d", len(desync1), len(desync2))
	}
	if desync1[0].LocalChecksum == desync1[0].RemoteChecksum {
		t.Fatalf("invalid desync event: %+v", desync1[0])
	}
}

func TestRollbackConvergenceAndDesyncDetection(t *testing.T) {
	for _, inputDelay := range []int{0, 2} {
		t.Run(fmt.Sprintf("delay=%d", inputDelay), func(t *testing.T) {
			const stepDT = 16 * time.Millisecond
			// 50ms one-way latency (~3.1 frames one-way, ~6 frames RTT).
			hub := gamenet.NewLoopbackHub(gamenet.LoopbackConfig{
				Latency: 50 * time.Millisecond,
				Seed:    123,
			})
			t1 := hub.AddPeer(1)
			t2 := hub.AddPeer(2)

			sim1 := &deterministicArenaSim{}
			sim2 := &deterministicArenaSim{}
			ref := &deterministicArenaSim{}

			var desync1, desync2 []gamenet.DesyncEvent
			r1 := gamenet.NewRollbackSession(gamenet.RollbackConfig{
				Transport:        t1,
				Sim:              sim1,
				MaxRollback:      12,
				InputDelay:       inputDelay,
				ChecksumInterval: 10,
				OnDesync:         func(ev gamenet.DesyncEvent) { desync1 = append(desync1, ev) },
			})
			r2 := gamenet.NewRollbackSession(gamenet.RollbackConfig{
				Transport:        t2,
				Sim:              sim2,
				MaxRollback:      12,
				InputDelay:       inputDelay,
				ChecksumInterval: 10,
				OnDesync:         func(ev gamenet.DesyncEvent) { desync2 = append(desync2, ev) },
			})

			const totalTicks = 120
			// Build exact reference timeline accounting for inputDelay.
			refInputs := make([][2]gamenet.InputFrame, totalTicks)
			for tick := range totalTicks {
				refInputs[tick][0] = gamenet.InputFrame{Tick: uint32(tick), Peer: 1}
				refInputs[tick][1] = gamenet.InputFrame{Tick: uint32(tick), Peer: 2}
			}
			for tick := range totalTicks {
				in1 := scriptedInput(1, uint32(tick))
				in2 := scriptedInput(2, uint32(tick))
				target := tick + inputDelay
				if target < totalTicks {
					in1.Tick = uint32(target)
					in2.Tick = uint32(target)
					refInputs[target][0] = in1
					refInputs[target][1] = in2
				}
				if err := r1.Step(in1); err != nil {
					t.Fatalf("r1 step %d: %v", tick, err)
				}
				if err := r2.Step(in2); err != nil {
					t.Fatalf("r2 step %d: %v", tick, err)
				}
				hub.Advance(stepDT)
			}

			// Flush remaining in-flight inputs and checksums.
			for range 10 {
				hub.Advance(stepDT)
				if err := r1.Poll(); err != nil {
					t.Fatalf("r1 poll: %v", err)
				}
				if err := r2.Poll(); err != nil {
					t.Fatalf("r2 poll: %v", err)
				}
			}

			// Run zero-latency reference simulation.
			for tick := range totalTicks {
				ref.Step(refInputs[tick][:])
			}

			if !bytes.Equal(sim1.Save(), ref.Save()) {
				t.Fatalf("peer 1 state diverged from zero-latency reference:\np1=%+v\nref=%+v", sim1, ref)
			}
			if !bytes.Equal(sim2.Save(), ref.Save()) {
				t.Fatalf("peer 2 state diverged from zero-latency reference:\np2=%+v\nref=%+v", sim2, ref)
			}
			if sim1.Checksum() != ref.Checksum() || sim2.Checksum() != ref.Checksum() {
				t.Fatalf("checksums diverged: p1=%x p2=%x ref=%x", sim1.Checksum(), sim2.Checksum(), ref.Checksum())
			}

			st1 := r1.Stats()
			st2 := r2.Stats()
			if st1.Rollbacks == 0 || st2.Rollbacks == 0 {
				t.Fatalf("expected rollbacks under 50ms latency, got p1=%+v p2=%+v", st1, st2)
			}
			if st1.ResimulatedFrames == 0 || st2.ResimulatedFrames == 0 {
				t.Fatalf("expected resimulated frames, got p1=%+v p2=%+v", st1, st2)
			}
			if len(desync1) != 0 || len(desync2) != 0 {
				t.Fatalf("unexpected desyncs during clean run: p1=%v p2=%v", desync1, desync2)
			}
			t.Logf("delay=%d rollback stats: p1=%+v p2=%+v", inputDelay, st1, st2)

			// Now tamper with peer 2's state and advance past next checksum interval.
			sim2.players[0].score += 777
			for tick := totalTicks; tick < totalTicks+20; tick++ {
				if err := r1.Step(scriptedInput(1, uint32(tick))); err != nil {
					t.Fatalf("r1 tamper step %d: %v", tick, err)
				}
				if err := r2.Step(scriptedInput(2, uint32(tick))); err != nil {
					t.Fatalf("r2 tamper step %d: %v", tick, err)
				}
				hub.Advance(stepDT)
			}
			for range 10 {
				hub.Advance(stepDT)
				_ = r1.Poll()
				_ = r2.Poll()
			}

			if len(desync1) == 0 || len(desync2) == 0 {
				t.Fatalf("expected OnDesync after state tampering, got p1=%d p2=%d", len(desync1), len(desync2))
			}
			if desync1[0].LocalChecksum == desync1[0].RemoteChecksum {
				t.Fatalf("invalid desync checksums: %+v", desync1[0])
			}
		})
	}
}
