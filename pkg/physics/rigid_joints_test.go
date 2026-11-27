package physics_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestRigidWorldBoxStackSettlesDeterministicallyOnLoop(t *testing.T) {
	type snap struct {
		x, y, vx, vy, angle, av float32
	}

	runStack := func(t *testing.T) []snap {
		t.Helper()
		var rw physics.RigidWorld
		rw.GravityY = 600
		rw.SolverIterations = 12

		// Static floor: top edge at y = 200.
		floor := physics.NewStaticRigidBox(160, 210, 320, 20)
		// 3 stacked 20x20 boxes dropped slightly above each other.
		b1 := physics.NewRigidBox(160, 184, 20, 20, 1)
		b2 := physics.NewRigidBox(160, 158, 20, 20, 1)
		b3 := physics.NewRigidBox(160, 132, 20, 20, 1)
		for _, b := range []*physics.RigidBody{b1, b2, b3} {
			b.Friction = 0.6
			b.Restitution = 0.0
			b.LinearDamping = 0.2
			b.AngularDamping = 0.5
		}
		rw.AddBody(floor, b1, b2, b3)

		loop := &game.Loop{
			Rate: 60,
			Update: func(dt time.Duration) {
				rw.Step(dt)
			},
		}
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			loop.Run(cx)
			return core.View{}
		})
		app.Advance(2 * time.Second)

		return []snap{
			{b1.X, b1.Y, b1.VX, b1.VY, b1.Angle, b1.AngularVel},
			{b2.X, b2.Y, b2.VX, b2.VY, b2.Angle, b2.AngularVel},
			{b3.X, b3.Y, b3.VX, b3.VY, b3.Angle, b3.AngularVel},
		}
	}

	s1 := runStack(t)
	s2 := runStack(t)

	// Bit-for-bit identical across runs.
	for i := range s1 {
		if s1[i] != s2[i] {
			t.Fatalf("box %d not deterministic: run1=%+v vs run2=%+v", i, s1[i], s2[i])
		}
	}

	// Expected settled centers: floor top=200 => b1.Y=190, b2.Y=170, b3.Y=150.
	wantY := []float32{190, 170, 150}
	for i, s := range s1 {
		if !near(s.x, 160, 1.0) {
			t.Fatalf("box %d X=%v, want ~160", i, s.x)
		}
		if !near(s.y, wantY[i], 1.5) {
			t.Fatalf("box %d Y=%v, want ~%v", i, s.y, wantY[i])
		}
		if math.Abs(float64(s.vy)) > 15 || math.Abs(float64(s.vx)) > 5 {
			t.Fatalf("box %d did not settle: vel=(%v, %v)", i, s.vx, s.vy)
		}
		if math.Abs(float64(s.angle)) > 0.05 {
			t.Fatalf("box %d tilted unexpectedly: angle=%v", i, s.angle)
		}
	}
}

func TestRigidWorldCircleCollisionsAndLayerFiltering(t *testing.T) {
	dt := time.Second / 60

	var rw physics.RigidWorld
	rw.GravityY = 0

	// Layer-filtered circles pass through each other.
	a := physics.NewRigidCircle(0, 50, 10, 1)
	a.VX = 120
	a.Layer = layerPlayer
	a.Mask = layerEnemy

	b := physics.NewRigidCircle(30, 50, 10, 1)
	b.Layer = layerBullet
	b.Mask = layerEnemy

	rw.AddBody(a, b)
	for range 30 {
		rw.Step(dt)
	}
	if a.X < 45 || b.X != 30 {
		t.Fatalf("non-colliding layers should pass through: a.X=%v b.X=%v", a.X, b.X)
	}

	// Elastic head-on collision between equal-mass circles exchanges velocities.
	var rw2 physics.RigidWorld
	c1 := physics.NewRigidCircle(0, 50, 10, 1)
	c1.VX = 100
	c1.Restitution = 1
	c1.Friction = 0

	c2 := physics.NewRigidCircle(25, 50, 10, 1)
	c2.Restitution = 1
	c2.Friction = 0

	rw2.AddBody(c1, c2)
	for range 30 {
		rw2.Step(dt)
	}
	if c2.VX < 80 || c1.VX > 20 {
		t.Fatalf("expected elastic momentum transfer: c1.VX=%v c2.VX=%v", c1.VX, c2.VX)
	}
}

func TestDistanceJointMaintainsRodLength(t *testing.T) {
	dt := time.Second / 60

	var rw physics.RigidWorld
	rw.GravityY = 600
	rw.SolverIterations = 12

	pivot := physics.NewStaticRigidBox(100, 50, 8, 8)
	bob := physics.NewRigidCircle(160, 50, 10, 1) // starts horizontally 60 px right of pivot
	rw.AddBody(pivot, bob)

	rod := physics.NewDistanceJoint(pivot, bob, core.Point{}, core.Point{}, 60)
	rw.AddJoint(rod)

	swungDown := false
	for range 180 {
		rw.Step(dt)
		dist := float32(math.Hypot(float64(bob.X-pivot.X), float64(bob.Y-pivot.Y)))
		if !near(dist, 60, 0.5) {
			t.Fatalf("distance joint violated rod length: dist=%v, want 60", dist)
		}
		if bob.Y > pivot.Y+50 {
			swungDown = true
		}
	}
	if !swungDown {
		t.Fatalf("pendulum bob never swung downward under gravity: pos=(%v, %v)", bob.X, bob.Y)
	}
}

func TestSpringJointPullsAndDampsToRestLength(t *testing.T) {
	dt := time.Second / 60

	var rw physics.RigidWorld
	rw.GravityY = 0

	anchor := physics.NewStaticRigidBox(100, 100, 10, 10)
	weight := physics.NewRigidCircle(220, 100, 10, 1) // stretched 120 px vs restLength 60 px
	rw.AddBody(anchor, weight)

	spring := physics.NewSpringJoint(anchor, weight, core.Point{}, core.Point{}, 60, 120, 12)
	rw.AddJoint(spring)

	for range 300 {
		rw.Step(dt)
		if math.IsNaN(float64(weight.X)) || math.IsInf(float64(weight.X), 0) {
			t.Fatalf("spring joint diverged")
		}
	}

	dist := float32(math.Hypot(float64(weight.X-anchor.X), float64(weight.Y-anchor.Y)))
	if !near(dist, 60, 1.0) {
		t.Fatalf("damped spring did not settle to rest length 60: dist=%v", dist)
	}
	if math.Abs(float64(weight.VX)) > 2.0 {
		t.Fatalf("damped spring still oscillating fast: VX=%v", weight.VX)
	}
}

func TestRevoluteJointKeepsHingeAnchorsCoincident(t *testing.T) {
	dt := time.Second / 60

	var rw physics.RigidWorld
	rw.GravityY = 600
	rw.SolverIterations = 12

	// Pin left end of a horizontal bar to a static wall bracket at (100, 80).
	bracket := physics.NewStaticRigidBox(100, 80, 8, 8)
	bar := physics.NewRigidBox(140, 80, 80, 12, 2) // center at (140, 80), left end at (100, 80)
	rw.AddBody(bracket, bar)

	hinge := physics.NewRevoluteJoint(bracket, bar, core.Point{X: 100, Y: 80})
	rw.AddJoint(hinge)

	maxAngle := float32(0)
	for range 120 {
		rw.Step(dt)
		pA := bracket.LocalToWorld(hinge.AnchorA)
		pB := bar.LocalToWorld(hinge.AnchorB)
		err := float32(math.Hypot(float64(pB.X-pA.X), float64(pB.Y-pA.Y)))
		if err > 0.5 {
			t.Fatalf("revolute hinge drifted apart: pA=%+v pB=%+v err=%v", pA, pB, err)
		}
		if bar.Angle > maxAngle {
			maxAngle = bar.Angle
		}
	}
	// Bar should rotate significantly clockwise (downward in +Y down screen space) around the hinge.
	if maxAngle < 0.8 {
		t.Fatalf("bar did not swing freely around revolute hinge: maxAngle=%v", maxAngle)
	}
}

func TestRigidWorldCircleRollsDownTiltedRamp(t *testing.T) {
	dt := time.Second / 60

	var rw physics.RigidWorld
	rw.GravityY = 600

	// Ramp tilted downward to the right by ~20 degrees.
	ramp := physics.NewStaticRigidBox(120, 140, 220, 16)
	ramp.Angle = 0.35
	ramp.Friction = 0.8

	ball := physics.NewRigidCircle(60, 80, 12, 1)
	ball.Friction = 0.8
	rw.AddBody(ramp, ball)

	for range 90 {
		rw.Step(dt)
	}
	if ball.X <= 80 {
		t.Fatalf("expected ball to roll right down tilted ramp, got X=%v", ball.X)
	}
	if ball.AngularVel <= 0 || ball.Angle <= 0 {
		t.Fatalf("expected friction torque to spin ball clockwise, got angle=%v av=%v", ball.Angle, ball.AngularVel)
	}
}

func TestDistanceJointComplianceAllowsSoftStretch(t *testing.T) {
	dt := time.Second / 60

	sim := func(compliance float32) float32 {
		var rw physics.RigidWorld
		rw.GravityY = 600
		pivot := physics.NewStaticRigidBox(100, 50, 8, 8)
		bob := physics.NewRigidCircle(100, 110, 10, 2)
		rw.AddBody(pivot, bob)
		j := physics.NewDistanceJoint(pivot, bob, core.Point{}, core.Point{}, 60)
		j.Compliance = compliance
		rw.AddJoint(j)
		for range 30 {
			rw.Step(dt)
		}
		return bob.Y - pivot.Y
	}

	rigidLen := sim(0)
	softLen := sim(0.05)
	if !near(rigidLen, 60, 0.25) {
		t.Fatalf("rigid rod length = %v, want 60", rigidLen)
	}
	if softLen <= rigidLen+0.5 {
		t.Fatalf("compliant rod did not stretch more than rigid rod: soft=%v rigid=%v", softLen, rigidLen)
	}
}
