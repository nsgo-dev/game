package physics3d_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/packages/game/physics3d"
)

func scNear32(a, b, tol float32) bool {
	return float32(math.Abs(float64(a-b))) <= tol
}

func scNearVec3(a, b physics3d.Vec3, tol float32) bool {
	return scNear32(a.X, b.X, tol) && scNear32(a.Y, b.Y, tol) && scNear32(a.Z, b.Z, tol)
}

func TestShapeCastSphereStopsAtWall(t *testing.T) {
	w := physics3d.NewWorld()
	// Wall face at x = 5 (box centered at x = 6 with half-extent X = 1).
	wall := w.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 1, Y: 5, Z: 5}}),
		physics3d.Vec3{X: 6, Y: 0, Z: 0},
	))
	wall.Tag = "wall"

	sphere := physics3d.SphereShape(physics3d.Sphere{Radius: 0.5})
	origin := physics3d.Vec3{X: 0, Y: 1, Z: 0}
	delta := physics3d.Vec3{X: 10, Y: 0, Z: 0}

	hit, ok := w.ShapeCast(sphere, origin, physics3d.QuatIdentity(), delta)
	if !ok {
		t.Fatal("expected sphere cast to hit wall")
	}
	if hit.Body != wall {
		t.Fatalf("hit.Body = %v, want wall", hit.Body)
	}
	// Sphere center must stop at x = 5 - 0.5 = 4.5 (within 1e-3).
	stopCenter := origin.Add(delta.Scale(hit.T))
	if !scNear32(stopCenter.X, 4.5, 1e-3) {
		t.Fatalf("sphere stopped at x=%v, want 4.5 (within 1e-3)", stopCenter.X)
	}
	if !scNear32(hit.Distance, 4.5, 1e-3) || !scNear32(hit.T, 0.45, 1e-4) {
		t.Fatalf("hit.Distance=%v hit.T=%v, want 4.5 / 0.45", hit.Distance, hit.T)
	}
	if !scNearVec3(hit.Normal, physics3d.Vec3{X: -1}, 1e-4) {
		t.Fatalf("hit.Normal = %v, want (-1, 0, 0)", hit.Normal)
	}
	if !scNearVec3(hit.Point, physics3d.Vec3{X: 5, Y: 1, Z: 0}, 1e-3) {
		t.Fatalf("hit.Point = %v, want (5, 1, 0)", hit.Point)
	}

	// Sphere grazing along the top of the wall (y = 5.5, dy = 0) must NOT hit.
	if gh, ok := w.ShapeCast(sphere, physics3d.Vec3{X: 0, Y: 5.5, Z: 0}, physics3d.QuatIdentity(), delta); ok {
		t.Fatalf("grazing sphere unexpectedly hit: %+v", gh)
	}
}

func TestShapeCastAllShapePairs(t *testing.T) {
	// 1. Sphere vs Sphere.
	h, ok := physics3d.ShapeCastShape(
		physics3d.SphereShape(physics3d.Sphere{Center: physics3d.Vec3{X: 0}, Radius: 0.5}),
		physics3d.Vec3{X: 10},
		physics3d.SphereShape(physics3d.Sphere{Center: physics3d.Vec3{X: 6}, Radius: 1.0}),
	)
	if !ok || !scNear32(h.Distance, 4.5, 1e-4) || !scNearVec3(h.Normal, physics3d.Vec3{X: -1}, 1e-4) || !scNearVec3(h.Point, physics3d.Vec3{X: 5}, 1e-4) {
		t.Fatalf("sphere-sphere = (%+v, %v), want dist=4.5 normal=(-1,0,0) pt=(5,0,0)", h, ok)
	}

	// 2. Sphere vs Rotated Box (45 deg around Y).
	rot45 := physics3d.QuatFromAxisAngle(physics3d.Vec3{Y: 1}, float32(math.Pi/4))
	rotBox := physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 1, Y: 1, Z: 1}}).Transformed(physics3d.Vec3{X: 6}, rot45)
	// Closest corner of rotated 2x2x2 cube to -X is at x = 6 - sqrt(2). Sphere r=0.5 stops at x = 6 - sqrt(2) - 0.5.
	wantDist := float32(6.0 - math.Sqrt2 - 0.5)
	h, ok = physics3d.ShapeCastShape(
		physics3d.SphereShape(physics3d.Sphere{Center: physics3d.Vec3{}, Radius: 0.5}),
		physics3d.Vec3{X: 10},
		rotBox,
	)
	if !ok || !scNear32(h.Distance, wantDist, 1e-3) || !scNearVec3(h.Normal, physics3d.Vec3{X: -1}, 1e-3) {
		t.Fatalf("sphere-obb = (%+v, %v), want dist=%v normal=(-1,0,0)", h, ok, wantDist)
	}

	// 3. AABB vs AABB.
	h, ok = physics3d.ShapeCastShape(
		physics3d.BoxShape(physics3d.Box{Center: physics3d.Vec3{X: 0}, HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}),
		physics3d.Vec3{X: 10},
		physics3d.BoxShape(physics3d.Box{Center: physics3d.Vec3{X: 5}, HalfExtents: physics3d.Vec3{X: 1, Y: 1, Z: 1}}),
	)
	if !ok || !scNear32(h.Distance, 3.5, 1e-4) || !scNearVec3(h.Normal, physics3d.Vec3{X: -1}, 1e-4) || !scNearVec3(h.Point, physics3d.Vec3{X: 4}, 1e-4) {
		t.Fatalf("aabb-aabb = (%+v, %v), want dist=3.5 normal=(-1,0,0) pt=(4,0,0)", h, ok)
	}

	// 4. Rotated Box (OBB) vs Static Wall Box via World.ShapeCast rot parameter.
	w := physics3d.NewWorld()
	w.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 1, Y: 5, Z: 5}}),
		physics3d.Vec3{X: 6},
	))
	unitCube := physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}})
	// Rotated 45 deg around Y: X half-projection is 0.5*sqrt(2). Wall face at x=5 -> center stops at 5 - 0.5*sqrt(2).
	wantOBBDist := float32(5.0 - 0.5*math.Sqrt2)
	h, ok = w.ShapeCast(unitCube, physics3d.Vec3{}, rot45, physics3d.Vec3{X: 10})
	if !ok || !scNear32(h.Distance, wantOBBDist, 1e-3) || !scNearVec3(h.Normal, physics3d.Vec3{X: -1}, 1e-3) {
		t.Fatalf("obb-wall = (%+v, %v), want dist=%v normal=(-1,0,0)", h, ok, wantOBBDist)
	}

	// 5. Capsule vs Box wall, Capsule vs Sphere, Capsule vs Capsule, Sphere vs Capsule, Box vs Capsule.
	capShape := physics3d.CapsuleShape(physics3d.VerticalCapsule(physics3d.Vec3{}, 0.4, 1.8))
	h, ok = w.ShapeCast(capShape, physics3d.Vec3{}, physics3d.QuatIdentity(), physics3d.Vec3{X: 10})
	if !ok || !scNear32(h.Distance, 4.6, 1e-3) || !scNearVec3(h.Normal, physics3d.Vec3{X: -1}, 1e-3) {
		t.Fatalf("capsule-wall = (%+v, %v), want dist=4.6 normal=(-1,0,0)", h, ok)
	}

	// Capsule vs Capsule (vertical capsules r=0.4 and r=0.6 separated by 5 on X -> dist = 4.0).
	targetCap := physics3d.CapsuleShape(physics3d.VerticalCapsule(physics3d.Vec3{X: 5}, 0.6, 2.0))
	h, ok = physics3d.ShapeCastShape(capShape, physics3d.Vec3{X: 10}, targetCap)
	if !ok || !scNear32(h.Distance, 4.0, 1e-3) || !scNearVec3(h.Normal, physics3d.Vec3{X: -1}, 1e-3) {
		t.Fatalf("capsule-capsule = (%+v, %v), want dist=4.0 normal=(-1,0,0)", h, ok)
	}

	// Sphere vs Capsule.
	h, ok = physics3d.ShapeCastShape(
		physics3d.SphereShape(physics3d.Sphere{Radius: 0.4}),
		physics3d.Vec3{X: 10},
		targetCap,
	)
	if !ok || !scNear32(h.Distance, 4.0, 1e-3) || !scNearVec3(h.Normal, physics3d.Vec3{X: -1}, 1e-3) {
		t.Fatalf("sphere-capsule = (%+v, %v), want dist=4.0 normal=(-1,0,0)", h, ok)
	}

	// Box vs Capsule.
	h, ok = physics3d.ShapeCastShape(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.4, Y: 0.5, Z: 0.5}}),
		physics3d.Vec3{X: 10},
		targetCap,
	)
	if !ok || !scNear32(h.Distance, 4.0, 1e-3) || !scNearVec3(h.Normal, physics3d.Vec3{X: -1}, 1e-3) {
		t.Fatalf("box-capsule = (%+v, %v), want dist=4.0 normal=(-1,0,0)", h, ok)
	}

	// Initial overlap reports T == 0 and Distance == 0 even with zero displacement.
	h, ok = w.ShapeCast(
		physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}),
		physics3d.Vec3{X: 4.8},
		physics3d.QuatIdentity(),
		physics3d.Vec3{},
	)
	if !ok || h.T != 0 || h.Distance != 0 || !scNearVec3(h.Normal, physics3d.Vec3{X: -1}, 1e-3) {
		t.Fatalf("initial overlap = (%+v, %v), want T=0 dist=0 normal=(-1,0,0)", h, ok)
	}
}

func TestShapeCastTriMeshHeightfieldAndVoxel(t *testing.T) {
	// 1. TriMeshCollider: thin vertical wall at z = -5 (no tunneling even on 100-unit sweep).
	w := physics3d.NewWorld()
	verts := []physics3d.Vec3{
		{X: -5, Y: -5, Z: -5},
		{X: 5, Y: -5, Z: -5},
		{X: 5, Y: 5, Z: -5},
		{X: -5, Y: 5, Z: -5},
	}
	indices := []uint32{0, 1, 2, 0, 2, 3}
	tm := w.AddTriMesh(physics3d.NewTriMeshCollider(verts, indices))

	for _, tc := range []struct {
		name     string
		shape    physics3d.Shape
		wantDist float32
	}{
		{"sphere", physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}), 4.5},
		{"box", physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}), 4.5},
		{"capsule", physics3d.CapsuleShape(physics3d.VerticalCapsule(physics3d.Vec3{}, 0.3, 1.4)), 4.7},
	} {
		h, ok := w.ShapeCast(tc.shape, physics3d.Vec3{}, physics3d.QuatIdentity(), physics3d.Vec3{Z: -100})
		if !ok || h.TriMesh != tm {
			t.Fatalf("%s vs trimesh = (%+v, %v), want tm hit", tc.name, h, ok)
		}
		if !scNear32(h.Distance, tc.wantDist, 1e-3) || !scNearVec3(h.Normal, physics3d.Vec3{Z: 1}, 1e-3) {
			t.Fatalf("%s vs trimesh dist=%v normal=%v, want dist=%v normal=(0,0,1)", tc.name, h.Distance, h.Normal, tc.wantDist)
		}
	}

	// 2. HeightfieldCollider: flat terrain at y = 2.0 over [0, 10] x [0, 10].
	wHF := physics3d.NewWorld()
	heights := make([]float32, 6*6)
	for i := range heights {
		heights[i] = 2.0
	}
	hf := wHF.AddHeightfield(physics3d.NewHeightfieldCollider(6, 6, 2.0, physics3d.Vec3{}, heights))

	for _, tc := range []struct {
		name     string
		shape    physics3d.Shape
		wantDist float32
	}{
		{"sphere", physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}), 7.5},
		{"box", physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}), 7.5},
		{"capsule", physics3d.CapsuleShape(physics3d.VerticalCapsule(physics3d.Vec3{}, 0.4, 1.6)), 7.2},
	} {
		h, ok := wHF.ShapeCast(tc.shape, physics3d.Vec3{X: 5, Y: 10, Z: 5}, physics3d.QuatIdentity(), physics3d.Vec3{Y: -15})
		if !ok || h.Heightfield != hf {
			t.Fatalf("%s vs heightfield = (%+v, %v), want hf hit", tc.name, h, ok)
		}
		if !scNear32(h.Distance, tc.wantDist, 1e-3) || !scNearVec3(h.Normal, physics3d.Vec3{Y: 1}, 1e-3) {
			t.Fatalf("%s vs heightfield dist=%v normal=%v, want dist=%v normal=(0,1,0)", tc.name, h.Distance, h.Normal, tc.wantDist)
		}
	}

	// 3. VoxelCollider: solid wall at x = 5 (voxels [5, 6) in X, [0, 4) in Y, [-2, 2) in Z).
	wVox := physics3d.NewWorld()
	vg := &testVoxelGrid{size: 1.0, blocks: map[[3]int]bool{}}
	for y := 0; y < 4; y++ {
		for z := -2; z < 2; z++ {
			vg.blocks[[3]int{5, y, z}] = true
		}
	}
	vc := wVox.AddVoxelCollider(physics3d.NewVoxelCollider(vg))
	h, ok := wVox.ShapeCast(
		physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}),
		physics3d.Vec3{X: 0, Y: 2, Z: 0},
		physics3d.QuatIdentity(),
		physics3d.Vec3{X: 10},
	)
	if !ok || h.Voxel != vc || !scNear32(h.Distance, 4.5, 1e-3) || !scNearVec3(h.Normal, physics3d.Vec3{X: -1}, 1e-3) {
		t.Fatalf("sphere vs voxel = (%+v, %v), want voxel dist=4.5 normal=(-1,0,0)", h, ok)
	}
}

type testVoxelGrid struct {
	size   float32
	blocks map[[3]int]bool
}

func (g *testVoxelGrid) VoxelSize() float32       { return g.size }
func (g *testVoxelGrid) IsSolid(x, y, z int) bool { return g.blocks[[3]int{x, y, z}] }

func TestShapeCastLayerIgnoreAndTriggers(t *testing.T) {
	w := physics3d.NewWorld()
	layerPlayer := physics3d.LayerBit(0)
	layerEnemy := physics3d.LayerBit(1)
	layerWall := physics3d.LayerBit(2)

	player := w.AddBody(physics3d.NewStaticBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}), physics3d.Vec3{X: 0}))
	player.Layer = layerPlayer

	trigger := w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 2, Z: 2}}), physics3d.Vec3{X: 3}))
	trigger.Layer = layerEnemy
	trigger.IsTrigger = true

	wall := w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 2, Z: 2}}), physics3d.Vec3{X: 7}))
	wall.Layer = layerWall

	sphere := physics3d.SphereShape(physics3d.Sphere{Radius: 0.5})

	// Explicit mask 0 returns no hits (consistent with RaycastLayer).
	if _, ok := w.ShapeCastLayer(sphere, physics3d.Vec3{}, physics3d.QuatIdentity(), physics3d.Vec3{X: 10}, 0, nil); ok {
		t.Fatal("ShapeCastLayer with mask 0 should not hit")
	}

	// Ignoring player skips overlap at origin and hits trigger first.
	h, ok := w.ShapeCastLayer(sphere, physics3d.Vec3{}, physics3d.QuatIdentity(), physics3d.Vec3{X: 10}, physics3d.LayerAll, player)
	if !ok || h.Body != trigger {
		t.Fatalf("ignore player = (%+v, %v), want trigger", h, ok)
	}

	// ShapeCastWith IgnoreTriggers + IgnoreBodyID skips both player and trigger, hitting wall.
	h, ok = w.ShapeCastWith(sphere, physics3d.Vec3{}, physics3d.QuatIdentity(), physics3d.Vec3{X: 10}, physics3d.ShapeCastOptions{
		IgnoreBodyID:   player.ID,
		IgnoreTriggers: true,
	})
	if !ok || h.Body != wall || !scNear32(h.Distance, 6.0, 1e-3) {
		t.Fatalf("IgnoreTriggers + IgnoreBodyID = (%+v, %v), want wall dist=6.0", h, ok)
	}

	// Layer filter for wall only.
	h, ok = w.ShapeCastLayer(sphere, physics3d.Vec3{}, physics3d.QuatIdentity(), physics3d.Vec3{X: 10}, layerWall, nil)
	if !ok || h.Body != wall {
		t.Fatalf("layerWall = (%+v, %v), want wall", h, ok)
	}

	// ShapeCastAllLayer returns trigger and wall when ignoring player.
	all := w.ShapeCastAllLayer(sphere, physics3d.Vec3{}, physics3d.QuatIdentity(), physics3d.Vec3{X: 10}, physics3d.LayerAll, player)
	if len(all) != 2 || all[0].Body != trigger || all[1].Body != wall {
		t.Fatalf("ShapeCastAllLayer = %+v, want [trigger, wall]", all)
	}
}

func TestCameraSpringArmNeverClips(t *testing.T) {
	// Build a room with 4 walls, a floor, an angled pillar, and a TriMesh ceiling.
	w := physics3d.NewWorld()
	w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 10, Y: 0.5, Z: 10}}), physics3d.Vec3{Y: -0.5}))
	w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 4, Z: 10}}), physics3d.Vec3{X: 5.5, Y: 4}))
	w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 4, Z: 10}}), physics3d.Vec3{X: -5.5, Y: 4}))
	w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 10, Y: 4, Z: 0.5}}), physics3d.Vec3{Y: 4, Z: 5.5}))
	w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 10, Y: 4, Z: 0.5}}), physics3d.Vec3{Y: 4, Z: -5.5}))
	pillar := physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.6, Y: 4, Z: 0.6}}), physics3d.Vec3{X: 2.2, Y: 4, Z: -2.0})
	pillar.Rot = physics3d.QuatFromAxisAngle(physics3d.Vec3{Y: 1}, float32(math.Pi/5))
	w.AddBody(pillar)

	// Ceiling triangle mesh at y = 3.2.
	w.AddTriMesh(physics3d.NewTriMeshCollider(
		[]physics3d.Vec3{{X: -10, Y: 3.2, Z: -10}, {X: 10, Y: 3.2, Z: -10}, {X: 10, Y: 3.2, Z: 10}, {X: -10, Y: 3.2, Z: 10}},
		[]uint32{0, 1, 2, 0, 2, 3},
	))

	// Player capsule near a corner of the room; camera spring arm ignores player body.
	player := w.AddBody(physics3d.NewStaticBody(
		physics3d.CapsuleShape(physics3d.VerticalCapsule(physics3d.Vec3{}, 0.35, 1.8)),
		physics3d.Vec3{X: 3.0, Y: 0.9, Z: -2.5},
	))

	pivot := physics3d.Vec3{X: 3.0, Y: 1.6, Z: -2.5}
	const (
		armLength    = float32(5.0)
		cameraRadius = float32(0.25)
		skin         = float32(0.01)
	)
	camProbe := physics3d.SphereShape(physics3d.Sphere{Radius: cameraRadius})

	pulledInCount := 0
	for yawDeg := 0; yawDeg < 360; yawDeg += 10 {
		for pitchDeg := -30; pitchDeg <= 50; pitchDeg += 20 {
			yaw := float64(yawDeg) * math.Pi / 180
			pitch := float64(pitchDeg) * math.Pi / 180
			dir := physics3d.Vec3{
				X: float32(math.Cos(pitch) * math.Cos(yaw)),
				Y: float32(math.Sin(pitch)),
				Z: float32(math.Cos(pitch) * math.Sin(yaw)),
			}
			delta := dir.Scale(armLength)

			camPos := pivot.Add(delta)
			if hit, ok := w.ShapeCastLayer(camProbe, pivot, physics3d.QuatIdentity(), delta, physics3d.LayerAll, player); ok {
				pulledInCount++
				safeDist := max(float32(0), hit.Distance-skin)
				camPos = pivot.Add(dir.Scale(safeDist))
			}

			// Verify camera sphere at camPos NEVER clips into any wall, floor, pillar, or ceiling.
			overlaps := w.QueryOverlap(physics3d.SphereShape(physics3d.Sphere{Center: camPos, Radius: cameraRadius - 2*skin}))
			for _, ov := range overlaps {
				if ov.Body == player {
					continue
				}
				t.Fatalf("camera clipped at yaw=%d pitch=%d pos=%v: overlap=%+v", yawDeg, pitchDeg, camPos, ov)
			}
		}
	}
	if pulledInCount == 0 {
		t.Fatal("expected spring arm to pull in against walls/ceiling in at least some orientations")
	}
}

func TestShapeCastDeterminism3D(t *testing.T) {
	w := physics3d.NewWorld()
	b1 := w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 1, Y: 1, Z: 1}}), physics3d.Vec3{X: 5, Y: 0.5}))
	b2 := w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 1, Y: 1, Z: 1}}), physics3d.Vec3{X: 5, Y: -0.5}))
	tm := w.AddTriMesh(physics3d.NewTriMeshCollider(
		[]physics3d.Vec3{{X: 4, Y: -2, Z: -2}, {X: 4, Y: 2, Z: -2}, {X: 4, Y: 2, Z: 2}, {X: 4, Y: -2, Z: 2}},
		[]uint32{0, 1, 2, 0, 2, 3},
	))

	sphere := physics3d.SphereShape(physics3d.Sphere{Radius: 0.5})
	origin := physics3d.Vec3{X: 0, Y: 0, Z: 0}
	delta := physics3d.Vec3{X: 10, Y: 0, Z: 0}

	baseline := w.ShapeCastAll(sphere, origin, physics3d.QuatIdentity(), delta)
	if len(baseline) != 3 {
		t.Fatalf("expected 3 tied hits at x=4, got %d: %+v", len(baseline), baseline)
	}
	// Tie-break order: Body(ID=1), Body(ID=2), TriMesh.
	if baseline[0].Body != b1 || baseline[1].Body != b2 || baseline[2].TriMesh != tm {
		t.Fatalf("unexpected tie order: %+v", baseline)
	}
	first, ok := w.ShapeCast(sphere, origin, physics3d.QuatIdentity(), delta)
	if !ok || first != baseline[0] {
		t.Fatalf("ShapeCast (%+v) != ShapeCastAll[0] (%+v)", first, baseline[0])
	}

	for range 20 {
		got := w.ShapeCastAll(sphere, origin, physics3d.QuatIdentity(), delta)
		if len(got) != len(baseline) {
			t.Fatal("non-deterministic hit count")
		}
		for i := range baseline {
			if got[i] != baseline[i] {
				t.Fatalf("run diverged at %d: %+v != %+v", i, got[i], baseline[i])
			}
		}
	}
}
