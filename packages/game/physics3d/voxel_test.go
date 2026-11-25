package physics3d

import (
	"math"
	"testing"
	"time"
)

type testVoxelKey struct{ x, y, z int }

type testVoxelGrid struct {
	voxelSize float32
	solid     map[testVoxelKey]bool
}

func newTestVoxelGrid(vs float32) *testVoxelGrid {
	return &testVoxelGrid{
		voxelSize: vs,
		solid:     make(map[testVoxelKey]bool),
	}
}

func (g *testVoxelGrid) VoxelSize() float32 { return g.voxelSize }

func (g *testVoxelGrid) IsSolid(x, y, z int) bool {
	return g.solid[testVoxelKey{x, y, z}]
}

func (g *testVoxelGrid) set(x, y, z int, solid bool) {
	k := testVoxelKey{x, y, z}
	if solid {
		g.solid[k] = true
	} else {
		delete(g.solid, k)
	}
}

func (g *testVoxelGrid) fillBox(x0, y0, z0, x1, y1, z1 int, solid bool) {
	for y := y0; y <= y1; y++ {
		for z := z0; z <= z1; z++ {
			for x := x0; x <= x1; x++ {
				g.set(x, y, z, solid)
			}
		}
	}
}

func (g *testVoxelGrid) VoxelBounds() (Vec3, Vec3, bool) {
	if len(g.solid) == 0 {
		return Vec3{}, Vec3{}, false
	}
	first := true
	var minX, minY, minZ, maxX, maxY, maxZ int
	for k := range g.solid {
		if first {
			minX, maxX = k.x, k.x
			minY, maxY = k.y, k.y
			minZ, maxZ = k.z, k.z
			first = false
			continue
		}
		minX, maxX = min(minX, k.x), max(maxX, k.x)
		minY, maxY = min(minY, k.y), max(maxY, k.y)
		minZ, maxZ = min(minZ, k.z), max(maxZ, k.z)
	}
	vs := g.voxelSize
	return Vec3{X: float32(minX) * vs, Y: float32(minY) * vs, Z: float32(minZ) * vs},
		Vec3{X: float32(maxX+1) * vs, Y: float32(maxY+1) * vs, Z: float32(maxZ+1) * vs},
		true
}

func TestVoxelColliderOverlapAndSeamFiltering(t *testing.T) {
	grid := newTestVoxelGrid(1.0)
	grid.fillBox(-4, 0, -4, 12, 0, 4, true)
	vc := NewVoxelCollider(grid)

	// Box straddling the seam x = 1.0 slightly sunk into the floor by 0.002:
	// every emitted contact must have a pure +Y normal, never a phantom -X/+X seam normal.
	box := BoxShape(Box{
		Center:      Vec3{X: 0.601, Y: 1.498, Z: 0.5},
		HalfExtents: Vec3{X: 0.4, Y: 0.5, Z: 0.4},
	})
	contacts := vc.CollectContacts(box, nil)
	if len(contacts) < 2 {
		t.Fatalf("expected >= 2 floor contacts across seam, got %d", len(contacts))
	}
	for i, c := range contacts {
		if abs32(c.Normal.X) > 1e-4 || abs32(c.Normal.Z) > 1e-4 || c.Normal.Y < 0.999 {
			t.Fatalf("box contact %d had phantom seam normal %+v (want +Y)", i, c.Normal)
		}
	}

	// Sphere straddling the seam x = 1.0 slightly sunk into the floor:
	sph := SphereShape(Sphere{Center: Vec3{X: 0.99, Y: 1.298, Z: 0.5}, Radius: 0.3})
	for i, c := range vc.CollectContacts(sph, nil) {
		if abs32(c.Normal.X) > 1e-4 || abs32(c.Normal.Z) > 1e-4 || c.Normal.Y < 0.999 {
			t.Fatalf("sphere contact %d had phantom seam normal %+v (want +Y)", i, c.Normal)
		}
	}

	// Capsule straddling the seam x = 1.0:
	cap := CapsuleShape(VerticalCapsule(Vec3{X: 0.99, Y: 1.898, Z: 0.5}, 0.3, 1.8))
	for i, c := range vc.CollectContacts(cap, nil) {
		if abs32(c.Normal.X) > 1e-4 || abs32(c.Normal.Z) > 1e-4 || c.Normal.Y < 0.999 {
			t.Fatalf("capsule contact %d had phantom seam normal %+v (want +Y)", i, c.Normal)
		}
	}

	// Dynamic crate sliding horizontally across 8 block seams must not snag.
	w := NewWorld()
	vc.Friction = 0
	w.AddVoxelCollider(vc)
	crate := w.AddBody(Body{
		Shape:        BoxShape(Box{HalfExtents: Vec3{X: 0.4, Y: 0.4, Z: 0.4}}),
		Pos:          Vec3{X: 0.5, Y: 1.4, Z: 0.5},
		Vel:          Vec3{X: 6.0},
		Mass:         1.0,
		Friction:     0,
		LockRotation: true,
	})
	dt := time.Second / 60
	for range 90 {
		w.Step(dt)
	}
	if crate.Pos.X < 8.0 {
		t.Fatalf("sliding crate snagged on flat voxel seams: final X = %.3f (want >= 8.0)", crate.Pos.X)
	}
	if abs32(crate.Pos.Y-1.4) > 0.02 {
		t.Fatalf("sliding crate bounced or sank on flat floor: final Y = %.4f (want ~1.4)", crate.Pos.Y)
	}
}

func TestVoxelColliderRaycastAndSweep(t *testing.T) {
	grid := newTestVoxelGrid(1.0)
	grid.fillBox(-4, 0, -4, 6, 0, 4, true)
	grid.set(3, 1, 0, true)
	grid.set(-2, 1, -1, true)
	vc := NewVoxelCollider(grid)

	// Raycast downward onto floor.
	hit, ok := vc.Raycast(Ray{Origin: Vec3{X: 1.5, Y: 5.0, Z: 0.5}, Dir: Vec3{Y: -1}, MaxDist: 10})
	if !ok {
		t.Fatal("expected downward raycast hit on floor")
	}
	if abs32(hit.Distance-4.0) > 1e-4 || abs32(hit.Point.Y-1.0) > 1e-4 || hit.Normal != (Vec3{Y: 1}) {
		t.Fatalf("unexpected downward hit: %+v", hit)
	}

	// Long ray clipped by VoxelBoundsGrid hitting negative-coordinate block.
	hit, ok = vc.Raycast(Ray{Origin: Vec3{X: -1000, Y: 1.5, Z: -0.5}, Dir: Vec3{X: 1}, MaxDist: 5000})
	if !ok {
		t.Fatal("expected long raycast hit on negative-coordinate block")
	}
	if abs32(hit.Point.X-(-2.0)) > 1e-3 || hit.Normal != (Vec3{X: -1}) {
		t.Fatalf("unexpected negative-coord hit: %+v", hit)
	}

	// Ray starting inside solid block reports T == 0 and Normal == -Dir.
	hit, ok = vc.Raycast(Ray{Origin: Vec3{X: 3.5, Y: 1.5, Z: 0.5}, Dir: Vec3{X: 1}, MaxDist: 5})
	if !ok || hit.T != 0 || hit.Distance != 0 || hit.Normal != (Vec3{X: -1}) {
		t.Fatalf("unexpected inside-block raycast hit: ok=%v hit=%+v", ok, hit)
	}

	// Analytical AABB sweep into block (3, 1, 0) while resting flush on floor y=1.
	box := BoxShape(Box{Center: Vec3{X: 0.5, Y: 1.4, Z: 0.5}, HalfExtents: Vec3{X: 0.4, Y: 0.4, Z: 0.4}})
	sh, ok := vc.Sweep(box, Vec3{X: 5.0})
	if !ok {
		t.Fatal("expected AABB sweep hit on block (3,1,0)")
	}
	// Front face starts at x=0.9, hits x=3.0 => distance = 2.1, T = 2.1/5.0 = 0.42.
	if abs32(sh.Distance-2.1) > 1e-3 || sh.Normal != (Vec3{X: -1}) || sh.BlockX != 3 || sh.BlockY != 1 || sh.BlockZ != 0 {
		t.Fatalf("unexpected AABB sweep hit: %+v", sh)
	}

	// Grazing AABB sweep along clear lane z=2.5 over flat floor must NOT hit internal seams.
	clearBox := BoxShape(Box{Center: Vec3{X: 0.5, Y: 1.4, Z: 2.5}, HalfExtents: Vec3{X: 0.4, Y: 0.4, Z: 0.4}})
	if shClear, hitClear := vc.Sweep(clearBox, Vec3{X: 5.0}); hitClear {
		t.Fatalf("grazing floor sweep falsely hit seam: %+v", shClear)
	}

	// Sphere sweep into block (3, 1, 0).
	sph := SphereShape(Sphere{Center: Vec3{X: 1.0, Y: 1.5, Z: 0.5}, Radius: 0.3})
	shS, ok := vc.Sweep(sph, Vec3{X: 4.0})
	if !ok {
		t.Fatal("expected sphere sweep hit")
	}
	// Distance should be 3.0 - 0.3 - 1.0 = 1.7.
	if abs32(shS.Distance-1.7) > 0.01 || shS.Normal.X > -0.95 {
		t.Fatalf("unexpected sphere sweep hit: %+v", shS)
	}

	// World-level RaycastFirst, QueryOverlap, and layer filtering.
	w := NewWorld()
	vc.Layer = LayerBit(2)
	w.AddVoxelCollider(vc)
	if _, hitWrong := w.RaycastFirstLayer(Vec3{X: 1.5, Y: 5, Z: 0.5}, Vec3{X: 1.5, Y: -2, Z: 0.5}, LayerBit(1)); hitWrong {
		t.Fatal("layer mask did not filter out voxel collider")
	}
	wh, ok := w.RaycastFirstLayer(Vec3{X: 1.5, Y: 5, Z: 0.5}, Vec3{X: 1.5, Y: -2, Z: 0.5}, LayerBit(2))
	if !ok || wh.Voxel != vc || abs32(wh.Point.Y-1.0) > 1e-4 {
		t.Fatalf("World.RaycastFirstLayer unexpected result: ok=%v hit=%+v", ok, wh)
	}
	ov := w.OverlapLayer(BoxShape(Box{Center: Vec3{X: 3.5, Y: 1.5, Z: 0.5}, HalfExtents: Vec3{X: 0.2, Y: 0.2, Z: 0.2}}), LayerBit(2))
	if len(ov) != 1 || ov[0].Voxel != vc {
		t.Fatalf("World.OverlapLayer expected 1 voxel hit, got %+v", ov)
	}
}

func TestVoxelColliderBodyRestWakeAndFallCustomGrid(t *testing.T) {
	grid := newTestVoxelGrid(1.0)
	grid.fillBox(-2, 0, -2, 4, 0, 4, true)
	vc := NewVoxelCollider(grid)

	w := NewWorld()
	w.AddVoxelCollider(vc)

	crate := w.AddBody(NewDynamicBody(
		BoxShape(Box{HalfExtents: Vec3{X: 0.4, Y: 0.4, Z: 0.4}}),
		Vec3{X: 1.5, Y: 3.0, Z: 1.5},
		1.0,
	))

	dt := time.Second / 60
	for range 180 {
		w.Step(dt)
	}
	if !crate.Sleeping {
		t.Fatalf("crate did not settle to sleep on voxel floor: pos=%+v vel=%+v", crate.Pos, crate.Vel)
	}
	if abs32(crate.Pos.Y-1.4) > 0.02 {
		t.Fatalf("crate settled at wrong Y: %.4f (want ~1.4)", crate.Pos.Y)
	}

	// Remove the block under the crate WITHOUT calling NotifyBlockChanged
	// (tests the wakeForVoxels fallback for plain VoxelGrid implementations).
	grid.set(1, 0, 1, false)

	for range 90 {
		w.Step(dt)
	}
	if crate.Sleeping {
		t.Fatal("crate stayed asleep after supporting voxel was removed")
	}
	if crate.Pos.Y >= 0.0 {
		t.Fatalf("crate did not fall through removed voxel: Y=%.3f (want < 0)", crate.Pos.Y)
	}
}

func TestVoxelColliderCCDPreventsTunneling(t *testing.T) {
	grid := newTestVoxelGrid(1.0)
	// Single-block-thick vertical wall at x = 5.
	grid.fillBox(5, 0, -2, 5, 5, 2, true)
	vc := NewVoxelCollider(grid)

	w := NewWorld()
	w.Gravity = Vec3{}
	w.AddVoxelCollider(vc)

	ball := w.AddBody(Body{
		Shape:       SphereShape(Sphere{Radius: 0.25}),
		Pos:         Vec3{X: 2.0, Y: 2.5, Z: 0.5},
		Vel:         Vec3{X: 180.0}, // 3 units per 1/60s step (> 1-block wall thickness)
		Mass:        1.0,
		Restitution: 0.2,
	})
	dt := time.Second / 60
	for range 10 {
		w.Step(dt)
	}
	if ball.Pos.X > 5.0 {
		t.Fatalf("fast ball tunneled through 1-block voxel wall: X=%.3f", ball.Pos.X)
	}
}

func TestVoxelColliderCharacterControllerStepUpAndJump(t *testing.T) {
	grid := newTestVoxelGrid(1.0)
	// Flat floor y=0, single-block ledge at x in [4..7], y=1, and 2-block wall at x=8, y in [1..3].
	grid.fillBox(-2, 0, -2, 10, 0, 4, true)
	grid.fillBox(4, 1, -2, 7, 1, 4, true)
	grid.fillBox(8, 1, -2, 8, 3, 4, true)
	vc := NewVoxelCollider(grid)

	w := NewWorld()
	w.AddVoxelCollider(vc)

	cc := NewCharacterController(Vec3{X: 0.5, Y: 1.0, Z: 0.5}, 0.3, 1.8)
	cc.StepHeight = 1.05
	dt := time.Second / 60

	// Settle on floor.
	for range 5 {
		cc.Move(w, Vec3{}, dt)
	}
	if !cc.Grounded {
		t.Fatalf("character not grounded on flat voxel floor: pos=%+v", cc.Pos)
	}

	// Walk forward across seams and step up onto the 1-block ledge at x=4..7.
	stepped := false
	for range 120 {
		res := cc.Move(w, Vec3{X: 4.0}, dt)
		if res.SteppedUp {
			stepped = true
		}
	}
	if !stepped {
		t.Fatalf("character failed to step up 1-block ledge: pos=%+v", cc.Pos)
	}
	if abs32(cc.Pos.Y-2.0) > 0.03 {
		t.Fatalf("character should stand on y=2.0 ledge, got Y=%.4f (X=%.3f)", cc.Pos.Y, cc.Pos.X)
	}
	if cc.Pos.X < 4.5 || cc.Pos.X > 7.75 {
		t.Fatalf("character X out of expected ledge range [4.5, 7.75]: X=%.3f", cc.Pos.X)
	}

	// Jump from the ledge and land cleanly back on it.
	res := cc.Move(w, Vec3{Y: 6.0}, dt)
	if res.Grounded {
		t.Fatal("character should leave ground on jump")
	}
	peakY := cc.Pos.Y
	for range 120 {
		cc.Move(w, Vec3{}, dt)
		if cc.Pos.Y > peakY {
			peakY = cc.Pos.Y
		}
	}
	if peakY < 3.0 {
		t.Fatalf("jump apex too low: peakY=%.3f (want >= 3.0)", peakY)
	}
	if !cc.Grounded || abs32(cc.Pos.Y-2.0) > 0.03 {
		t.Fatalf("character did not land cleanly on ledge: grounded=%v pos=%+v", cc.Grounded, cc.Pos)
	}
}

func TestVoxelColliderDeterminism(t *testing.T) {
	run := func() (Vec3, Vec3, Quat, Vec3) {
		grid := newTestVoxelGrid(1.0)
		grid.fillBox(-2, 0, -2, 8, 0, 4, true)
		grid.fillBox(4, 1, -2, 6, 1, 4, true)
		vc := NewVoxelCollider(grid)

		w := NewWorld()
		w.AddVoxelCollider(vc)
		crate := w.AddBody(NewDynamicBody(
			BoxShape(Box{HalfExtents: Vec3{X: 0.4, Y: 0.4, Z: 0.4}}),
			Vec3{X: 1.3, Y: 3.5, Z: 0.7},
			1.5,
		))
		crate.Vel = Vec3{X: 1.2, Z: -0.4}
		cc := NewCharacterController(Vec3{X: 0.5, Y: 1.0, Z: 0.5}, 0.3, 1.8)
		cc.StepHeight = 1.05

		dt := time.Second / 60
		for tick := range 120 {
			if tick == 80 {
				grid.set(1, 0, 0, false)
				vc.NotifyBlockChanged(1, 0, 0)
			}
			w.Step(dt)
			cc.Move(w, Vec3{X: 3.5}, dt)
		}
		return crate.Pos, crate.Vel, crate.Rot, cc.Pos
	}

	p1, v1, r1, c1 := run()
	p2, v2, r2, c2 := run()

	eqVec := func(a, b Vec3) bool {
		return math.Float32bits(a.X) == math.Float32bits(b.X) &&
			math.Float32bits(a.Y) == math.Float32bits(b.Y) &&
			math.Float32bits(a.Z) == math.Float32bits(b.Z)
	}
	eqQuat := func(a, b Quat) bool {
		return math.Float32bits(a.X) == math.Float32bits(b.X) &&
			math.Float32bits(a.Y) == math.Float32bits(b.Y) &&
			math.Float32bits(a.Z) == math.Float32bits(b.Z) &&
			math.Float32bits(a.W) == math.Float32bits(b.W)
	}
	if !eqVec(p1, p2) || !eqVec(v1, v2) || !eqQuat(r1, r2) || !eqVec(c1, c2) {
		t.Fatalf("non-deterministic voxel simulation:\nrun1: p=%+v v=%+v r=%+v cc=%+v\nrun2: p=%+v v=%+v r=%+v cc=%+v",
			p1, v1, r1, c1, p2, v2, r2, c2)
	}
}

func TestVoxelColliderOriginCapsuleSweepAndRemoval(t *testing.T) {
	grid := newTestVoxelGrid(1.0)
	grid.fillBox(0, 0, 0, 4, 0, 4, true)
	grid.set(3, 1, 1, true)

	vc := NewVoxelCollider(grid)
	vc.Origin = Vec3{X: 10, Y: 20, Z: 30}

	// Raycast honors Origin offset.
	rh, ok := vc.Raycast(Ray{Origin: Vec3{X: 11.5, Y: 25, Z: 31.5}, Dir: Vec3{Y: -1}, MaxDist: 10})
	if !ok || abs32(rh.Point.Y-21.0) > 1e-4 || rh.Normal != (Vec3{Y: 1}) {
		t.Fatalf("Origin-offset raycast failed: ok=%v hit=%+v", ok, rh)
	}

	// Capsule sweep into offset block (3, 1, 1) covering world X in [13, 14].
	cap := CapsuleShape(VerticalCapsule(Vec3{X: 11.0, Y: 21.9, Z: 31.5}, 0.3, 1.8))
	sh, ok := vc.Sweep(cap, Vec3{X: 4.0})
	if !ok || sh.BlockX != 3 || sh.BlockY != 1 || sh.BlockZ != 1 || abs32(sh.Distance-1.7) > 0.02 || sh.Normal.X > -0.9 {
		t.Fatalf("unexpected capsule sweep hit with Origin: ok=%v hit=%+v", ok, sh)
	}

	// Crate rests on offset floor, then wakes and falls when collider is removed.
	w := NewWorld()
	w.AddVoxelCollider(vc)
	crate := w.AddBody(NewDynamicBody(
		BoxShape(Box{HalfExtents: Vec3{X: 0.4, Y: 0.4, Z: 0.4}}),
		Vec3{X: 11.5, Y: 23.0, Z: 31.5},
		1.0,
	))
	dt := time.Second / 60
	for range 180 {
		w.Step(dt)
	}
	if !crate.Sleeping || abs32(crate.Pos.Y-21.4) > 0.02 {
		t.Fatalf("crate did not sleep on offset voxel floor: sleeping=%v pos=%+v", crate.Sleeping, crate.Pos)
	}
	if !w.RemoveVoxelCollider(vc.ID) {
		t.Fatal("RemoveVoxelCollider returned false")
	}
	if crate.Sleeping {
		t.Fatal("RemoveVoxelCollider did not wake sleeping crate")
	}
	for range 60 {
		w.Step(dt)
	}
	if crate.Pos.Y >= 20.5 {
		t.Fatalf("crate did not fall after RemoveVoxelCollider: Y=%.3f", crate.Pos.Y)
	}
}
