package physics3d_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/physics3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestSoftWorld3DRopeAttachedToCrateSwingsWithoutStretch(t *testing.T) {
	const (
		restLen   = float32(2.4)
		segments  = 12
		dt        = time.Second / 60
		steps     = 240
		maxFactor = 1.01 // <= 1% stretch acceptance criterion
	)

	world := physics3d.NewWorld()
	pinPos := physics3d.Vec3{X: 0, Y: 5, Z: 0}
	crateHalf := physics3d.Vec3{X: 0.25, Y: 0.25, Z: 0.25}
	anchorLocal := physics3d.Vec3{Y: crateHalf.Y}
	// Heavy crate (mass 6.0) on light rope (total mass 0.3) = 20:1 mass ratio.
	crate := world.AddBody(physics3d.NewDynamicBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: crateHalf}),
		physics3d.Vec3{X: restLen, Y: pinPos.Y - crateHalf.Y, Z: 0},
		6.0,
	))

	sw := physics3d.NewSoftWorld()
	sw.World = world
	sw.Damping = 0.005

	rope := sw.AddRope(physics3d.RopeConfig{
		Start:          pinPos,
		End:            physics3d.Vec3{X: restLen, Y: pinPos.Y, Z: 0},
		Segments:       segments,
		TotalMass:      0.3,
		ParticleRadius: 0.03,
		Compliance:     0,
		PinStart:       true,
	})
	sw.AttachBody(rope[len(rope)-1], crate, anchorLocal, 0)

	minX := crate.Pos.X
	minY := crate.Pos.Y
	maxStretchRatio := float32(0)
	maxRotAngle := float32(0)

	for step := 0; step < steps; step++ {
		world.Step(dt)
		sw.Step(dt)

		if crate.Pos.X < minX {
			minX = crate.Pos.X
		}
		if crate.Pos.Y < minY {
			minY = crate.Pos.Y
		}
		if ang := crate.Rot.AngleTo(physics3d.QuatIdentity()); ang > maxRotAngle {
			maxRotAngle = ang
		}

		polyLen := sw.RopeLength(rope)
		ratio := polyLen / restLen
		if ratio > maxStretchRatio {
			maxStretchRatio = ratio
		}
		if ratio > maxFactor {
			t.Fatalf("step %d: 3D rope stretched to %.4f (%.2f%% of rest length %.2f), exceeds 1%% limit",
				step, polyLen, ratio*100, restLen)
		}

		anchorWorld := crate.Pos.Add(crate.Rot.RotateVec(anchorLocal))
		directDist := anchorWorld.Dist(pinPos)
		if directDist > restLen*maxFactor {
			t.Fatalf("step %d: 3D pin-to-anchor distance %.4f exceeds 1%% of rest length %.2f",
				step, directDist, restLen)
		}
	}

	if minX > -1.2 {
		t.Fatalf("3D crate did not swing across center: minX=%.2f, want < -1.2", minX)
	}
	if minY > pinPos.Y-restLen*0.8 {
		t.Fatalf("3D crate did not descend along swing arc: minY=%.2f", minY)
	}
	if maxRotAngle < 0.05 {
		t.Fatalf("offset attachment did not torque 3D crate: maxRotAngle=%.4f", maxRotAngle)
	}
	t.Logf("3D rope+crate swing maxStretchRatio=%.5f minX=%.2f minY=%.2f maxRot=%.2f",
		maxStretchRatio, minX, minY, maxRotAngle)
}

func TestSoftWorld3DClothDrapesOverSphereWithoutPassThrough(t *testing.T) {
	const (
		sphereRadius = float32(1.0)
		dt           = time.Second / 60
		steps        = 180
	)
	sphereCenter := physics3d.Vec3{X: 0, Y: 1.0, Z: 0}

	world := physics3d.NewWorld()
	world.AddBody(physics3d.NewStaticBody(
		physics3d.SphereShape(physics3d.Sphere{Radius: sphereRadius}),
		sphereCenter,
	))

	sw := physics3d.NewSoftWorld()
	sw.World = world
	sw.Damping = 0.05

	// Cloth flag pinned on two top corners, suspended just above the sphere so it drapes over it.
	cloth := sw.AddCloth(physics3d.ClothConfig{
		Origin:            physics3d.Vec3{X: -1.1, Y: 2.25, Z: -1.1},
		AxisU:             physics3d.Vec3{X: 1},
		AxisV:             physics3d.Vec3{Z: 1},
		Width:             2.2,
		Height:            2.2,
		Cols:              10,
		Rows:              10,
		TotalMass:         1.0,
		ParticleRadius:    0.05,
		StretchCompliance: 0.00001,
		BendCompliance:    5.0,
		PinTopLeft:        true,
		PinTopRight:       true,
		Friction:          0.4,
	})

	for step := 0; step < steps; step++ {
		sw.Step(dt)

		// No particle may penetrate inside the sphere.
		for idx, pIdx := range cloth.Particles {
			p := sw.Particles[pIdx]
			dist := p.Pos.Dist(sphereCenter)
			if dist < sphereRadius-1e-3 {
				t.Fatalf("step %d: cloth particle %d penetrated sphere: dist=%.4f < radius=%.2f (pos=%+v)",
					step, idx, dist, sphereRadius, p.Pos)
			}
		}

		// Edge midpoints must also stay clear of the sphere surface (no edge pass-through).
		for r := 0; r <= cloth.Rows; r++ {
			for c := 0; c < cloth.Cols; c++ {
				a := sw.Particles[cloth.Index(c, r)].Pos
				b := sw.Particles[cloth.Index(c+1, r)].Pos
				mid := a.Add(b).Scale(0.5)
				if d := mid.Dist(sphereCenter); d < sphereRadius-0.02 {
					t.Fatalf("step %d: cloth horizontal edge (%d,%d) passed through sphere: midDist=%.4f", step, c, r, d)
				}
			}
		}
		for r := 0; r < cloth.Rows; r++ {
			for c := 0; c <= cloth.Cols; c++ {
				a := sw.Particles[cloth.Index(c, r)].Pos
				b := sw.Particles[cloth.Index(c, r+1)].Pos
				mid := a.Add(b).Scale(0.5)
				if d := mid.Dist(sphereCenter); d < sphereRadius-0.02 {
					t.Fatalf("step %d: cloth vertical edge (%d,%d) passed through sphere: midDist=%.4f", step, c, r, d)
				}
			}
		}
	}

	// Verify the unpinned front corners draped down below the sphere's crown (Y < 1.9),
	// while the center of the cloth rests on top of the sphere (Y >= sphereCenter.Y + sphereRadius).
	bl := sw.Particles[cloth.Index(0, cloth.Rows)].Pos
	br := sw.Particles[cloth.Index(cloth.Cols, cloth.Rows)].Pos
	center := sw.Particles[cloth.Index(cloth.Cols/2, cloth.Rows/2)].Pos

	if bl.Y > 1.8 || br.Y > 1.8 {
		t.Fatalf("unpinned cloth corners did not drape down: bl=%+v br=%+v", bl, br)
	}
	if center.Y < sphereCenter.Y+sphereRadius-0.05 {
		t.Fatalf("cloth center fell through sphere crown: center=%+v", center)
	}
	// Verify pinned corners stayed fixed.
	tl := sw.Particles[cloth.Index(0, 0)].Pos
	tr := sw.Particles[cloth.Index(cloth.Cols, 0)].Pos
	if tl.Dist(physics3d.Vec3{X: -1.1, Y: 2.25, Z: -1.1}) > 1e-5 ||
		tr.Dist(physics3d.Vec3{X: 1.1, Y: 2.25, Z: -1.1}) > 1e-5 {
		t.Fatalf("pinned cloth corners drifted: tl=%+v tr=%+v", tl, tr)
	}
}

func TestSoftWorld3DSoftBoxPreservesVolumeOnFloor(t *testing.T) {
	const dt = time.Second / 60

	world := physics3d.NewWorld()
	world.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 10, Y: 0.5, Z: 10}}),
		physics3d.Vec3{Y: -0.5},
	))

	sw := physics3d.NewSoftWorld()
	sw.World = world
	sw.Damping = 0.08

	boxParticles := sw.AddSoftBox(physics3d.SoftBoxConfig{
		Center:           physics3d.Vec3{Y: 2.0},
		HalfExtents:      physics3d.Vec3{X: 0.4, Y: 0.4, Z: 0.4},
		TotalMass:        1.0,
		ParticleRadius:   0.04,
		EdgeCompliance:   0.0001,
		VolumeCompliance: 0.00001,
		Restitution:      0.3,
	})

	for range 180 {
		sw.Step(dt)
	}

	minY := float32(math.MaxFloat32)
	maxY := float32(-math.MaxFloat32)
	for i, idx := range boxParticles {
		y := sw.Particles[idx].Pos.Y
		if y < -0.01 {
			t.Fatalf("soft box particle %d penetrated ground: Y=%.4f", i, y)
		}
		if y < minY {
			minY = y
		}
		if y > maxY {
			maxY = y
		}
	}
	height := maxY - minY
	if height < 0.65 || height > 0.95 {
		t.Fatalf("soft box collapsed or exploded: height=%.3f, want ~0.8", height)
	}
}

func TestSoftWorld3DTriMeshHeightfieldAndLayerFiltering(t *testing.T) {
	const dt = time.Second / 60

	world := physics3d.NewWorld()

	layerMesh := physics3d.LayerBit(0)
	layerTerrain := physics3d.LayerBit(1)
	layerOther := physics3d.LayerBit(2)

	// Flat triangle mesh deck at Y = 2.0 over [-2, 2] x [-2, 2].
	meshVerts := []physics3d.Vec3{
		{X: -2, Y: 2, Z: -2},
		{X: 2, Y: 2, Z: -2},
		{X: 2, Y: 2, Z: 2},
		{X: -2, Y: 2, Z: 2},
	}
	meshIdx := []uint32{0, 2, 1, 0, 3, 2}
	tm := physics3d.NewTriMeshCollider(meshVerts, meshIdx)
	tm.Layer = layerMesh
	world.AddTriMesh(tm)

	// Heightfield at Y = 0.5 over [-4, 4] x [-4, 4].
	heights := make([]float32, 9*9)
	for i := range heights {
		heights[i] = 0.5
	}
	hf := physics3d.NewHeightfieldCollider(9, 9, 1.0, physics3d.Vec3{X: -4, Y: 0, Z: -4}, heights)
	hf.Layer = layerTerrain
	world.AddHeightfield(hf)

	sw := physics3d.NewSoftWorld()
	sw.World = world

	pMesh := sw.AddParticle(physics3d.Particle{
		Pos: physics3d.Vec3{X: 0, Y: 3.0, Z: 0}, InvMass: 1, Radius: 0.1,
		Layer: layerMesh, Mask: layerMesh,
	})
	pTerrain := sw.AddParticle(physics3d.Particle{
		Pos: physics3d.Vec3{X: 0, Y: 3.0, Z: 0}, InvMass: 1, Radius: 0.1,
		Layer: layerTerrain, Mask: layerTerrain,
	})
	pGhost := sw.AddParticle(physics3d.Particle{
		Pos: physics3d.Vec3{X: 0, Y: 3.0, Z: 0}, InvMass: 1, Radius: 0.1,
		Layer: layerOther, Mask: layerOther,
	})

	for range 120 {
		sw.Step(dt)
	}

	if y := sw.Particles[pMesh].Pos.Y; y < 2.05 || y > 2.2 {
		t.Fatalf("pMesh should rest on TriMesh at Y~2.1: got %.3f", y)
	}
	if y := sw.Particles[pTerrain].Pos.Y; y < 0.55 || y > 0.7 {
		t.Fatalf("pTerrain should rest on Heightfield at Y~0.6: got %.3f", y)
	}
	if y := sw.Particles[pGhost].Pos.Y; y > -5 {
		t.Fatalf("pGhost should pass through excluded layers: got Y=%.3f", y)
	}
}

func TestSoftWorld3DDeterminism(t *testing.T) {
	run := func() ([]physics3d.Particle, physics3d.Body) {
		world := physics3d.NewWorld()
		world.AddBody(physics3d.NewStaticBody(
			physics3d.SphereShape(physics3d.Sphere{Radius: 0.8}),
			physics3d.Vec3{X: 3, Y: 1, Z: 0},
		))
		crate := world.AddBody(physics3d.NewDynamicBody(
			physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.2, Y: 0.2, Z: 0.2}}),
			physics3d.Vec3{X: 1.6, Y: 4.8, Z: 0},
			4.0,
		))

		sw := physics3d.NewSoftWorld()
		sw.World = world

		rope := sw.AddRope(physics3d.RopeConfig{
			Start:    physics3d.Vec3{X: 0, Y: 5, Z: 0},
			End:      physics3d.Vec3{X: 1.6, Y: 5, Z: 0},
			Segments: 8, TotalMass: 0.4, ParticleRadius: 0.03, PinStart: true,
		})
		sw.AttachBody(rope[len(rope)-1], crate, physics3d.Vec3{Y: 0.2}, 0)

		sw.AddCloth(physics3d.ClothConfig{
			Origin: physics3d.Vec3{X: 2.2, Y: 2.1, Z: -0.8},
			Width:  1.6, Height: 1.6, Cols: 4, Rows: 4,
			PinTopLeft: true, PinTopRight: true,
		})

		const dt = time.Second / 60
		for range 120 {
			world.Step(dt)
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
		if math.Float32bits(p1[i].Pos.X) != math.Float32bits(p2[i].Pos.X) ||
			math.Float32bits(p1[i].Pos.Y) != math.Float32bits(p2[i].Pos.Y) ||
			math.Float32bits(p1[i].Pos.Z) != math.Float32bits(p2[i].Pos.Z) ||
			math.Float32bits(p1[i].Vel.X) != math.Float32bits(p2[i].Vel.X) ||
			math.Float32bits(p1[i].Vel.Y) != math.Float32bits(p2[i].Vel.Y) ||
			math.Float32bits(p1[i].Vel.Z) != math.Float32bits(p2[i].Vel.Z) {
			t.Fatalf("3D particle %d diverged: %+v vs %+v", i, p1[i], p2[i])
		}
	}
	if math.Float32bits(c1.Pos.X) != math.Float32bits(c2.Pos.X) ||
		math.Float32bits(c1.Pos.Y) != math.Float32bits(c2.Pos.Y) ||
		math.Float32bits(c1.Pos.Z) != math.Float32bits(c2.Pos.Z) ||
		math.Float32bits(c1.Rot.X) != math.Float32bits(c2.Rot.X) ||
		math.Float32bits(c1.Rot.Y) != math.Float32bits(c2.Rot.Y) ||
		math.Float32bits(c1.Rot.Z) != math.Float32bits(c2.Rot.Z) ||
		math.Float32bits(c1.Rot.W) != math.Float32bits(c2.Rot.W) {
		t.Fatalf("3D rigid crate diverged: %+v vs %+v", c1, c2)
	}
}

func TestDrawSoftWorld3D(t *testing.T) {
	vp := core.Bounds{Size: core.Size{Width: 640, Height: 480}}
	view := physics3d.Mat4LookAt(
		physics3d.Vec3{X: 0, Y: 2, Z: 8},
		physics3d.Vec3{X: 0, Y: 1, Z: 0},
		physics3d.Vec3{X: 0, Y: 1, Z: 0},
	)
	viewProj := physics3d.Mat4Mul(
		physics3d.Mat4Perspective(float32(60*math.Pi/180), 640.0/480.0, 0.1, 100),
		view,
	)

	world := physics3d.NewWorld()
	crate := world.AddBody(physics3d.NewDynamicBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.2, Y: 0.2, Z: 0.2}}),
		physics3d.Vec3{X: 1, Y: 1, Z: 0},
		1.0,
	))

	sw := physics3d.NewSoftWorld()
	rope := sw.AddRope(physics3d.RopeConfig{
		Start:    physics3d.Vec3{X: 0, Y: 2, Z: 0},
		End:      physics3d.Vec3{X: 1, Y: 1, Z: 0},
		Segments: 4, PinStart: true,
	})
	sw.AttachBody(rope[len(rope)-1], crate, physics3d.Vec3{}, 0)

	if edges := sw.Edges(); len(edges) != 4 {
		t.Fatalf("Edges() returned %d structural edges, want 4", len(edges))
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{
			TestID: "soft3d",
			Style:  core.Style{Width: core.Pt(640), Height: core.Pt(480)},
			Draw: func(d *core.Drawing, _ core.Size) {
				physics3d.DrawSoftWorld(d, viewProj, vp, nil, physics3d.DefaultDebugStyle())
				physics3d.DrawSoftWorld(d, viewProj, vp, sw, physics3d.DefaultDebugStyle())
			},
		}
	})

	if ops := app.Find("soft3d").Ops; len(ops) == 0 {
		t.Fatal("DrawSoftWorld emitted 0 draw ops, want non-empty wireframe ops")
	}
}
