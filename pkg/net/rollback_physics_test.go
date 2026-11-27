package net_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	gamenet "github.com/nsgo-dev/game/pkg/net"
	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/game/pkg/physics3d"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	inputLeft  uint64 = 1 << 0
	inputRight uint64 = 1 << 1
	inputJump  uint64 = 1 << 2
)

// physics2DSim wraps a 2D physics.RigidWorld as a deterministic net.SimState.
type physics2DSim struct {
	world   *physics.RigidWorld
	players map[gamenet.PeerID]*physics.RigidBody
}

func newPhysics2DSim() *physics2DSim {
	rw := &physics.RigidWorld{
		GravityY:         500,
		SolverIterations: 10,
	}
	floor := physics.NewStaticRigidBox(200, 300, 420, 20)
	floor.ID = 1
	leftWall := physics.NewStaticRigidBox(0, 150, 20, 320)
	leftWall.ID = 2
	rightWall := physics.NewStaticRigidBox(400, 150, 20, 320)
	rightWall.ID = 3

	p1 := physics.NewRigidBox(120, 240, 24, 24, 1.5)
	p1.ID = 101
	p1.Friction = 0.4
	p1.Restitution = 0.2

	p2 := physics.NewRigidBox(280, 240, 24, 24, 1.5)
	p2.ID = 102
	p2.Friction = 0.4
	p2.Restitution = 0.2

	crate := physics.NewRigidBox(200, 220, 28, 28, 1.0)
	crate.ID = 103
	crate.Restitution = 0.3

	pivot := physics.NewStaticRigidBox(200, 90, 8, 8)
	pivot.ID = 4
	ball := physics.NewRigidCircle(235, 130, 12, 1.0)
	ball.ID = 104

	rw.AddBody(floor, leftWall, rightWall, pivot, p1, p2, crate, ball)
	rw.AddJoint(physics.NewDistanceJoint(pivot, ball, core.Point{}, core.Point{}, 65))
	rw.AddTrigger(&physics.Trigger{
		ID:    1,
		Shape: physics.BoxShape(physics.Box{X: 150, Y: 220, W: 100, H: 70}),
		Area: physics.AreaSettings{
			FluidLinearDrag: 1.5,
		},
	})

	return &physics2DSim{
		world: rw,
		players: map[gamenet.PeerID]*physics.RigidBody{
			1: p1,
			2: p2,
		},
	}
}

func (s *physics2DSim) Step(inputs []gamenet.InputFrame) {
	const dt = time.Second / 60
	for _, in := range inputs {
		b := s.players[in.Peer]
		if b == nil {
			continue
		}
		if in.Bits&inputLeft != 0 {
			b.ApplyForce(-700, 0)
		}
		if in.Bits&inputRight != 0 {
			b.ApplyForce(700, 0)
		}
		if in.Bits&inputJump != 0 && b.Y > 250 {
			b.ApplyImpulse(0, -260)
		}
	}
	s.world.Step(dt)
}

func (s *physics2DSim) Save() []byte {
	data, err := json.Marshal(s.world.Save())
	if err != nil {
		panic(err)
	}
	return data
}

func (s *physics2DSim) Load(snapshot []byte) error {
	var snap physics.RigidWorldSnapshot
	if err := json.Unmarshal(snapshot, &snap); err != nil {
		return err
	}
	return s.world.Restore(snap)
}

func (s *physics2DSim) Checksum() uint64 {
	return s.world.Checksum()
}

// A RollbackSession can keep a deterministic 2D physics.RigidWorld inside SimState:
// Save/Restore captures all rigid-body and joint solver state as JSON, and Checksum
// verifies peers remain bit-identical across rollbacks and re-simulations.
func ExampleRollbackSession_physics() {
	hub := gamenet.NewLoopbackHub(gamenet.LoopbackConfig{Latency: 32 * time.Millisecond, Seed: 7})
	simA, simB := newPhysics2DSim(), newPhysics2DSim()
	peers := []gamenet.PeerID{1, 2}

	sessA := gamenet.NewRollbackSession(gamenet.RollbackConfig{
		Transport:        hub.AddPeer(1),
		Sim:              simA,
		Peers:            peers,
		MaxRollback:      16,
		ChecksumInterval: 10,
	})
	sessB := gamenet.NewRollbackSession(gamenet.RollbackConfig{
		Transport:        hub.AddPeer(2),
		Sim:              simB,
		Peers:            peers,
		MaxRollback:      16,
		ChecksumInterval: 10,
	})

	for tick := uint32(0); tick < 60; tick++ {
		var bitsA, bitsB uint64
		if tick%20 < 10 {
			bitsA = inputRight
		} else {
			bitsA = inputLeft | inputJump
		}
		if tick%15 < 8 {
			bitsB = inputLeft
		} else {
			bitsB = inputRight | inputJump
		}
		_ = sessA.Step(gamenet.InputFrame{Bits: bitsA})
		_ = sessB.Step(gamenet.InputFrame{Bits: bitsB})
		hub.Advance(time.Second / 60)
	}
	hub.Advance(200 * time.Millisecond)
	_ = sessA.Poll()
	_ = sessB.Poll()

	fmt.Println(
		sessA.Tick(),
		simA.Checksum() == simB.Checksum(),
		sessA.Stats().Rollbacks > 0,
		len(sessA.Desyncs()),
		len(sessB.Desyncs()),
	)
	// Output:
	// 60 true true 0 0
}

func TestRollbackPhysics2DSoak(t *testing.T) {
	// Cross-architecture golden checksum after 600 rollback ticks (must match on amd64 and arm64).
	const wantGolden uint64 = 0x62830a39310934bd

	hub := gamenet.NewLoopbackHub(gamenet.LoopbackConfig{
		Latency:  40 * time.Millisecond,
		Jitter:   15 * time.Millisecond,
		LossRate: 0.05,
		Seed:     42,
	})

	simA, simB, simRef := newPhysics2DSim(), newPhysics2DSim(), newPhysics2DSim()
	peers := []gamenet.PeerID{1, 2}
	sessA := gamenet.NewRollbackSession(gamenet.RollbackConfig{
		Transport:        hub.AddPeer(1),
		Sim:              simA,
		Peers:            peers,
		MaxRollback:      24,
		ChecksumInterval: 15,
	})
	sessB := gamenet.NewRollbackSession(gamenet.RollbackConfig{
		Transport:        hub.AddPeer(2),
		Sim:              simB,
		Peers:            peers,
		MaxRollback:      24,
		ChecksumInterval: 15,
	})

	const totalTicks = 600
	for tick := uint32(0); tick < totalTicks; tick++ {
		bitsA := uint64(0)
		switch (tick / 17) % 3 {
		case 0:
			bitsA = inputRight
		case 1:
			bitsA = inputLeft
		case 2:
			bitsA = inputRight | inputJump
		}
		bitsB := uint64(0)
		switch (tick / 13) % 3 {
		case 0:
			bitsB = inputLeft
		case 1:
			bitsB = inputRight | inputJump
		case 2:
			bitsB = inputLeft | inputJump
		}

		if err := sessA.Step(gamenet.InputFrame{Bits: bitsA}); err != nil {
			t.Fatalf("tick %d sessA.Step: %v", tick, err)
		}
		if err := sessB.Step(gamenet.InputFrame{Bits: bitsB}); err != nil {
			t.Fatalf("tick %d sessB.Step: %v", tick, err)
		}
		simRef.Step([]gamenet.InputFrame{
			{Tick: tick, Peer: 1, Bits: bitsA},
			{Tick: tick, Peer: 2, Bits: bitsB},
		})
		hub.Advance(time.Second / 60)
	}

	// Drain in-flight packets so both peers confirm all ticks and exchange final checksums.
	for range 20 {
		hub.Advance(25 * time.Millisecond)
		if err := sessA.Poll(); err != nil {
			t.Fatalf("sessA.Poll: %v", err)
		}
		if err := sessB.Poll(); err != nil {
			t.Fatalf("sessB.Poll: %v", err)
		}
	}

	if len(sessA.Desyncs()) > 0 || len(sessB.Desyncs()) > 0 {
		t.Fatalf("unexpected desyncs: A=%v B=%v", sessA.Desyncs(), sessB.Desyncs())
	}
	if sessA.Stats().Rollbacks == 0 || sessB.Stats().Rollbacks == 0 {
		t.Fatalf("expected rollbacks on both peers, got A=%+v B=%+v", sessA.Stats(), sessB.Stats())
	}
	sumA, sumB, sumRef := simA.Checksum(), simB.Checksum(), simRef.Checksum()
	if sumA != sumRef || sumB != sumRef {
		t.Fatalf("checksum mismatch: A=%#016x B=%#016x ref=%#016x", sumA, sumB, sumRef)
	}
	if sumRef != wantGolden {
		t.Fatalf("2D rollback golden checksum = %#016x, want %#016x", sumRef, wantGolden)
	}
}

// physics3DSim wraps a 3D physics3d.World as a deterministic net.SimState.
type physics3DSim struct {
	world   *physics3d.World
	players map[gamenet.PeerID]*physics3d.Body
}

func newPhysics3DSim() *physics3DSim {
	w := physics3d.NewWorld()
	w.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 20, Y: 0.5, Z: 20}}),
		physics3d.Vec3{Y: -0.5},
	))

	p1 := w.AddBody(physics3d.NewDynamicBody(
		physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}),
		physics3d.Vec3{X: -2.0, Y: 0.6, Z: 0},
		2.0,
	))
	p1.Restitution = 0.3

	p2 := w.AddBody(physics3d.NewDynamicBody(
		physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}),
		physics3d.Vec3{X: 2.0, Y: 0.6, Z: 0},
		2.0,
	))
	p2.Restitution = 0.3

	// Center stack of boxes (warm-start + sleeping + contacts)
	w.AddBody(physics3d.NewDynamicBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.45, Y: 0.45, Z: 0.45}}),
		physics3d.Vec3{X: 0, Y: 0.46, Z: 0},
		1.5,
	))
	w.AddBody(physics3d.NewDynamicBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.4, Y: 0.4, Z: 0.4}}),
		physics3d.Vec3{X: 0.05, Y: 1.4, Z: 0},
		1.0,
	))

	// Motorized hinge obstacle
	anchor := w.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.2, Y: 0.2, Z: 0.2}}),
		physics3d.Vec3{X: 0, Y: 2.5, Z: -2.5},
	))
	blade := w.AddBody(physics3d.NewDynamicBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.8, Y: 0.15, Z: 0.15}}),
		physics3d.Vec3{X: 0.8, Y: 2.5, Z: -2.5},
		1.0,
	))
	hinge := physics3d.NewHingeJoint(anchor, blade, physics3d.Vec3{}, physics3d.Vec3{X: -0.8}, physics3d.Vec3{Z: 1})
	hinge.MotorTargetVel = 2.5
	hinge.MotorMaxTorque = 10
	w.AddJoint(hinge)

	return &physics3DSim{
		world: w,
		players: map[gamenet.PeerID]*physics3d.Body{
			1: p1,
			2: p2,
		},
	}
}

func (s *physics3DSim) Step(inputs []gamenet.InputFrame) {
	const dt = time.Second / 60
	for _, in := range inputs {
		b := s.players[in.Peer]
		if b == nil {
			continue
		}
		if in.Bits&inputLeft != 0 {
			b.ApplyImpulse(physics3d.Vec3{X: -0.25})
		}
		if in.Bits&inputRight != 0 {
			b.ApplyImpulse(physics3d.Vec3{X: 0.25})
		}
		if in.Bits&inputJump != 0 && b.Pos.Y < 0.7 {
			b.ApplyImpulse(physics3d.Vec3{Y: 6.0})
		}
	}
	s.world.Step(dt)
}

func (s *physics3DSim) Save() []byte {
	data, err := json.Marshal(s.world.Save())
	if err != nil {
		panic(err)
	}
	return data
}

func (s *physics3DSim) Load(snapshot []byte) error {
	var snap physics3d.WorldSnapshot
	if err := json.Unmarshal(snapshot, &snap); err != nil {
		return err
	}
	return s.world.Restore(snap)
}

func (s *physics3DSim) Checksum() uint64 {
	return s.world.Checksum()
}

func TestRollbackPhysics3DSoak(t *testing.T) {
	// Cross-architecture golden checksum after 400 rollback ticks (must match on amd64 and arm64).
	const wantGolden uint64 = 0xb2a75b907dc8fc28

	hub := gamenet.NewLoopbackHub(gamenet.LoopbackConfig{
		Latency: 40 * time.Millisecond,
		Jitter:  12 * time.Millisecond,
		Seed:    99,
	})

	simA, simB, simRef := newPhysics3DSim(), newPhysics3DSim(), newPhysics3DSim()
	peers := []gamenet.PeerID{1, 2}
	sessA := gamenet.NewRollbackSession(gamenet.RollbackConfig{
		Transport:        hub.AddPeer(1),
		Sim:              simA,
		Peers:            peers,
		MaxRollback:      24,
		ChecksumInterval: 10,
	})
	sessB := gamenet.NewRollbackSession(gamenet.RollbackConfig{
		Transport:        hub.AddPeer(2),
		Sim:              simB,
		Peers:            peers,
		MaxRollback:      24,
		ChecksumInterval: 10,
	})

	const totalTicks = 400
	for tick := uint32(0); tick < totalTicks; tick++ {
		bitsA := uint64(0)
		switch (tick / 19) % 3 {
		case 0:
			bitsA = inputRight
		case 1:
			bitsA = inputLeft
		case 2:
			bitsA = inputRight | inputJump
		}
		bitsB := uint64(0)
		switch (tick / 11) % 3 {
		case 0:
			bitsB = inputLeft
		case 1:
			bitsB = inputRight | inputJump
		case 2:
			bitsB = inputLeft | inputJump
		}

		if err := sessA.Step(gamenet.InputFrame{Bits: bitsA}); err != nil {
			t.Fatalf("tick %d sessA.Step: %v", tick, err)
		}
		if err := sessB.Step(gamenet.InputFrame{Bits: bitsB}); err != nil {
			t.Fatalf("tick %d sessB.Step: %v", tick, err)
		}
		simRef.Step([]gamenet.InputFrame{
			{Tick: tick, Peer: 1, Bits: bitsA},
			{Tick: tick, Peer: 2, Bits: bitsB},
		})
		hub.Advance(time.Second / 60)
	}

	for range 20 {
		hub.Advance(25 * time.Millisecond)
		if err := sessA.Poll(); err != nil {
			t.Fatalf("sessA.Poll: %v", err)
		}
		if err := sessB.Poll(); err != nil {
			t.Fatalf("sessB.Poll: %v", err)
		}
	}

	if len(sessA.Desyncs()) > 0 || len(sessB.Desyncs()) > 0 {
		t.Fatalf("unexpected desyncs: A=%v B=%v", sessA.Desyncs(), sessB.Desyncs())
	}
	if sessA.Stats().Rollbacks == 0 || sessB.Stats().Rollbacks == 0 {
		t.Fatalf("expected rollbacks on both peers, got A=%+v B=%+v", sessA.Stats(), sessB.Stats())
	}
	sumA, sumB, sumRef := simA.Checksum(), simB.Checksum(), simRef.Checksum()
	if sumA != sumRef || sumB != sumRef {
		t.Fatalf("checksum mismatch: A=%#016x B=%#016x ref=%#016x", sumA, sumB, sumRef)
	}
	if sumRef != wantGolden {
		t.Fatalf("3D rollback golden checksum = %#016x, want %#016x", sumRef, wantGolden)
	}
}
