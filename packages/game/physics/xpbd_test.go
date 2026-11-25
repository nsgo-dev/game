package physics_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/physics"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestSoftWorld2DRopeAttachedToCrateSwingsWithoutStretch(t *testing.T) {
	const (
		restLen   = float32(120)
		segments  = 12
		gravityY  = float32(600)
		dt        = time.Second / 60
		steps     = 240
		maxFactor = 1.01 // <= 1% stretch acceptance criterion
	)

	rw := &physics.RigidWorld{GravityY: gravityY}
	// Heavy crate (mass 10) relative to light rope (total mass 0.5) = 20:1 mass ratio.
	crateW, crateH := float32(24), float32(24)
	anchorLocal := core.Point{X: 0, Y: -core.Points(crateH * 0.5)}
	crate := physics.NewRigidBox(restLen, crateH*0.5, crateW, crateH, 10)
	rw.AddBody(crate)

	sw := physics.NewSoftWorld()
	sw.RigidWorld = rw
	sw.GravityY = gravityY
	sw.Damping = 0.01

	rope := sw.AddRope(physics.RopeConfig{
		StartX:         0,
		StartY:         0,
		EndX:           restLen,
		EndY:           0,
		Segments:       segments,
		TotalMass:      0.5,
		ParticleRadius: 2,
		Compliance:     0,
		PinStart:       true,
	})
	sw.AttachRigidBody(rope[len(rope)-1], crate, anchorLocal, 0)

	minCrateX := crate.X
	maxCrateY := crate.Y
	maxAbsAngle := float32(0)
	maxStretchRatio := float32(0)

	for step := 0; step < steps; step++ {
		rw.Step(dt)
		sw.Step(dt)

		if crate.X < minCrateX {
			minCrateX = crate.X
		}
		if crate.Y > maxCrateY {
			maxCrateY = crate.Y
		}
		if a := float32(math.Abs(float64(crate.Angle))); a > maxAbsAngle {
			maxAbsAngle = a
		}

		// Verify finite state on every particle and rigid body.
		for i, p := range sw.Particles {
			if math.IsNaN(float64(p.X)) || math.IsNaN(float64(p.Y)) || math.IsInf(float64(p.X), 0) || math.IsInf(float64(p.Y), 0) {
				t.Fatalf("step %d particle %d non-finite: (%v, %v)", step, i, p.X, p.Y)
			}
		}

		// Measure total polyline length and pin-to-anchor Euclidean distance.
		polyLen := sw.RopeLength(rope)
		ratio := polyLen / restLen
		if ratio > maxStretchRatio {
			maxStretchRatio = ratio
		}
		if ratio > maxFactor {
			t.Fatalf("step %d: rope stretched to %.4f (%.2f%% of rest length %.1f), exceeds 1%% limit",
				step, polyLen, ratio*100, restLen)
		}

		anchorWorld := crate.LocalToWorld(anchorLocal)
		directDist := float32(math.Hypot(float64(anchorWorld.X), float64(anchorWorld.Y)))
		if directDist > restLen*maxFactor {
			t.Fatalf("step %d: pin-to-anchor distance %.4f exceeds 1%% of rest length %.1f",
				step, directDist, restLen)
		}
	}

	// Verify two-way coupling caused a genuine pendulum swing and crate torque.
	if minCrateX > -50 {
		t.Fatalf("crate did not swing across center: minCrateX=%.2f, want < -50", minCrateX)
	}
	if maxCrateY < restLen*0.8 {
		t.Fatalf("crate did not drop to bottom of swing arc: maxCrateY=%.2f", maxCrateY)
	}
	if maxAbsAngle < 0.05 {
		t.Fatalf("offset attachment did not torque crate: maxAbsAngle=%.4f", maxAbsAngle)
	}
	t.Logf("2D rope+crate swing maxStretchRatio=%.5f minX=%.1f maxY=%.1f maxAngle=%.2f",
		maxStretchRatio, minCrateX, maxCrateY, maxAbsAngle)
}

func TestSoftWorld2DCompliantAttachmentCouplesTwoWay(t *testing.T) {
	const dt = time.Second / 60

	rw := &physics.RigidWorld{GravityY: 400}
	crate := physics.NewRigidBox(0, 60, 20, 20, 2)
	rw.AddBody(crate)

	sw := physics.NewSoftWorld()
	sw.RigidWorld = rw
	sw.GravityY = 400

	p0 := sw.AddParticle(physics.Particle{X: 0, Y: 0, Pinned: true})
	p1 := sw.AddParticle(physics.Particle{X: 0, Y: 60, InvMass: 1})
	sw.AddDistance(p0, p1, 60, 0)
	sw.AttachRigidBody(p1, crate, core.Point{}, 0.0005)

	for range 90 {
		rw.Step(dt)
		sw.Step(dt)
	}

	// Free fall for 1.5s at g=400 would drop 0.5*400*1.5^2 = 450 units; elastic rope must hold it near Y=60.
	if crate.Y > 120 {
		t.Fatalf("compliant attachment failed to support crate: Y=%.2f", crate.Y)
	}
	if crate.Y < 55 {
		t.Fatalf("crate launched upward unexpectedly: Y=%.2f", crate.Y)
	}
}

func TestSoftWorld2DSoftBodyPressureAndShapeMatching(t *testing.T) {
	const dt = time.Second / 60

	t.Run("PressureArea", func(t *testing.T) {
		rw := &physics.RigidWorld{}
		floor := physics.NewStaticRigidBox(0, 120, 400, 20) // top edge at Y=110
		rw.AddBody(floor)

		sw := physics.NewSoftWorld()
		sw.RigidWorld = rw
		sw.GravityY = 500
		sw.Damping = 0.08

		body := sw.AddSoftBody(physics.SoftBodyConfig{
			CenterX:           0,
			CenterY:           40,
			RadiusX:           22,
			Segments:          14,
			TotalMass:         1,
			ParticleRadius:    2,
			EdgeCompliance:    0.00001,
			AreaCompliance:    0.000001,
			Pressure:          1.0,
			EnableBending:     true,
			BendingCompliance: 0.01,
			Restitution:       0.3,
		})
		restArea := math.Abs(float64(sw.PolygonArea(body)))

		for range 180 {
			sw.Step(dt)
		}

		// Every particle must stay above the floor top edge (Y <= 110).
		for i, idx := range body {
			p := sw.Particles[idx]
			if p.Y > 110.5 {
				t.Fatalf("soft body particle %d penetrated floor: Y=%.2f > 110", i, p.Y)
			}
		}

		// Area must be recovered closely.
		finalArea := math.Abs(float64(sw.PolygonArea(body)))
		ratio := finalArea / restArea
		if ratio < 0.85 || ratio > 1.15 {
			t.Fatalf("soft body failed to preserve area: rest=%.1f final=%.1f (ratio=%.3f)", restArea, finalArea, ratio)
		}
	})

	t.Run("ShapeMatching", func(t *testing.T) {
		rw := &physics.RigidWorld{}
		floor := physics.NewStaticRigidBox(0, 120, 400, 20)
		rw.AddBody(floor)

		sw := physics.NewSoftWorld()
		sw.RigidWorld = rw
		sw.GravityY = 500
		sw.Damping = 0.08

		body := sw.AddSoftBody(physics.SoftBodyConfig{
			CenterX:             0,
			CenterY:             40,
			RadiusX:             20,
			Segments:            10,
			TotalMass:           1,
			ParticleRadius:      2,
			EdgeCompliance:      0.0001,
			AreaCompliance:      0.0001,
			UseShapeMatching:    true,
			ShapeMatchStiffness: 0.4,
		})
		restArea := math.Abs(float64(sw.PolygonArea(body)))

		for range 150 {
			sw.Step(dt)
		}

		for i, idx := range body {
			if sw.Particles[idx].Y > 110.5 {
				t.Fatalf("shape-matched particle %d penetrated floor: Y=%.2f", i, sw.Particles[idx].Y)
			}
		}
		finalArea := math.Abs(float64(sw.PolygonArea(body)))
		if ratio := finalArea / restArea; ratio < 0.85 || ratio > 1.15 {
			t.Fatalf("shape-matched body lost shape: rest=%.1f final=%.1f (ratio=%.3f)", restArea, finalArea, ratio)
		}
	})
}

func TestSoftWorld2DBendingConstraintRestoresAngle(t *testing.T) {
	sw := physics.NewSoftWorld()
	sw.Damping = 6.0

	a := sw.AddParticle(physics.Particle{X: 0, Y: 0, Pinned: true})
	b := sw.AddParticle(physics.Particle{X: 10, Y: 0, Pinned: true})
	c := sw.AddParticle(physics.Particle{X: 20, Y: 0, InvMass: 1})
	sw.AddDistance(a, b, 10, 0)
	sw.AddDistance(b, c, 10, 0)
	sw.AddBending(a, b, c, 0.001)

	// Bend particle c downward by 90 degrees.
	sw.Particles[c].X = 10
	sw.Particles[c].Y = 10
	sw.Particles[c].PrevX = 10
	sw.Particles[c].PrevY = 10

	for range 90 {
		sw.Step(time.Second / 60)
	}

	// Bending constraint must restore c back close to (20, 0).
	pc := sw.Particles[c]
	if math.Abs(float64(pc.Y)) > 1.0 || pc.X < 18.5 {
		t.Fatalf("bending constraint did not straighten rod: c=(%.2f, %.2f), want near (20, 0)", pc.X, pc.Y)
	}
}

func TestSoftWorld2DTilesAndLayerFiltering(t *testing.T) {
	const dt = time.Second / 60

	grid := physics.NewGrid(10, 10, 16, 16)
	grid.FillRect(0, 5, 10, 1, physics.TileSolid)  // top edge at Y=80
	grid.FillRect(0, 3, 10, 1, physics.TileOneWay) // top edge at Y=48

	layerTerrain := physics.LayerBit(0)
	layerPlatform := physics.LayerBit(1)
	layerGhost := physics.LayerBit(2)

	blockA := physics.NewBoxBody(16, 32, 32, 16) // top at Y=32
	blockA.Layer = layerPlatform
	blockA.Mask = physics.LayerAll

	world := &physics.World{Tiles: grid}
	world.Add(blockA)

	sw := physics.NewSoftWorld()
	sw.World = world
	sw.TileLayer = layerTerrain
	sw.GravityY = 500

	// Particle 0 collides with blockA (layerPlatform).
	pHitBox := sw.AddParticle(physics.Particle{
		X: 32, Y: 10, InvMass: 1, Radius: 3,
		Layer: layerPlatform, Mask: layerPlatform,
	})
	// Particle 1 ignores blockA and lands on the one-way tile at Y=48.
	pHitOneWay := sw.AddParticle(physics.Particle{
		X: 32, Y: 10, InvMass: 1, Radius: 3,
		Layer: layerGhost, Mask: layerTerrain,
	})
	// Particle 2 starts below the one-way tile (Y=56) and lands on the solid tile at Y=80.
	pHitSolid := sw.AddParticle(physics.Particle{
		X: 32, Y: 56, InvMass: 1, Radius: 3,
		Layer: layerGhost, Mask: layerTerrain,
	})
	// Particle 3 ignores both tiles and blockA, falling straight past Y=100.
	pPassThrough := sw.AddParticle(physics.Particle{
		X: 32, Y: 10, InvMass: 1, Radius: 3,
		Layer: layerGhost, Mask: layerGhost,
	})

	for range 90 {
		sw.Step(dt)
	}

	if y := sw.Particles[pHitBox].Y; y < 27 || y > 30 {
		t.Fatalf("pHitBox should rest on blockA top (Y~29): got Y=%.2f", y)
	}
	if y := sw.Particles[pHitOneWay].Y; y < 43 || y > 46 {
		t.Fatalf("pHitOneWay should rest on one-way tile top (Y~45): got Y=%.2f", y)
	}
	if y := sw.Particles[pHitSolid].Y; y < 75 || y > 78 {
		t.Fatalf("pHitSolid should rest on solid tile top (Y~77): got Y=%.2f", y)
	}
	if y := sw.Particles[pPassThrough].Y; y < 150 {
		t.Fatalf("pPassThrough should pass through excluded layers: got Y=%.2f", y)
	}
}

func TestSoftWorld2DDeterminism(t *testing.T) {
	run := func() ([]physics.Particle, physics.RigidBody) {
		rw := &physics.RigidWorld{GravityY: 500}
		floor := physics.NewStaticRigidBox(0, 180, 400, 20)
		crate := physics.NewRigidBox(90, 12, 24, 24, 5)
		rw.AddBody(floor, crate)

		grid := physics.NewGrid(16, 16, 16, 16)
		grid.FillRect(0, 12, 16, 1, physics.TileSolid)

		sw := physics.NewSoftWorld()
		sw.RigidWorld = rw
		sw.Tiles = grid
		sw.GravityY = 500

		rope := sw.AddRope(physics.RopeConfig{
			StartX: 0, StartY: 0, EndX: 90, EndY: 0,
			Segments: 9, TotalMass: 0.6, ParticleRadius: 2,
			PinStart: true, EnableBending: true, BendingCompliance: 0.01,
		})
		sw.AttachRigidBody(rope[len(rope)-1], crate, core.Point{X: 0, Y: -12}, 0)

		sw.AddSoftBody(physics.SoftBodyConfig{
			CenterX: 60, CenterY: 40, RadiusX: 16, Segments: 10,
			TotalMass: 1, ParticleRadius: 2, UseShapeMatching: true,
		})

		const dt = time.Second / 60
		for range 120 {
			rw.Step(dt)
			sw.Step(dt)
		}
		return sw.Particles, *crate
	}

	p1, c1 := run()
	p2, c2 := run()
	if len(p1) != len(p2) {
		t.Fatalf("particle count mismatch: %d vs %d", len(p1), len(p2))
	}
	for i := range p1 {
		if math.Float32bits(p1[i].X) != math.Float32bits(p2[i].X) ||
			math.Float32bits(p1[i].Y) != math.Float32bits(p2[i].Y) ||
			math.Float32bits(p1[i].VX) != math.Float32bits(p2[i].VX) ||
			math.Float32bits(p1[i].VY) != math.Float32bits(p2[i].VY) {
			t.Fatalf("particle %d diverged: %+v vs %+v", i, p1[i], p2[i])
		}
	}
	if math.Float32bits(c1.X) != math.Float32bits(c2.X) ||
		math.Float32bits(c1.Y) != math.Float32bits(c2.Y) ||
		math.Float32bits(c1.Angle) != math.Float32bits(c2.Angle) ||
		math.Float32bits(c1.VX) != math.Float32bits(c2.VX) ||
		math.Float32bits(c1.VY) != math.Float32bits(c2.VY) {
		t.Fatalf("rigid crate diverged: %+v vs %+v", c1, c2)
	}
}

func TestDrawSoftWorld2D(t *testing.T) {
	rw := &physics.RigidWorld{}
	crate := physics.NewRigidBox(60, 20, 16, 16, 1)
	rw.AddBody(crate)

	sw := physics.NewSoftWorld()
	rope := sw.AddRope(physics.RopeConfig{
		StartX: 10, StartY: 10, EndX: 60, EndY: 20,
		Segments: 4, PinStart: true,
	})
	sw.AttachRigidBody(rope[len(rope)-1], crate, core.Point{}, 0)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{
			TestID: "soft2d",
			Style:  core.Style{Width: core.Pt(200), Height: core.Pt(200)},
			Draw: func(d *core.Drawing, _ core.Size) {
				physics.DrawSoftWorld(d, nil, physics.DefaultDebugStyle())
				physics.DrawSoftWorld(d, sw, physics.DefaultDebugStyle())
			},
		}
	})

	if ops := app.Find("soft2d").Ops; len(ops) == 0 {
		t.Fatal("DrawSoftWorld emitted 0 draw ops, want non-empty debug drawing")
	}
}
