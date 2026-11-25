package physics3d

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

func verifyTreeInvariants(t *testing.T, tr *aabbTree, expectedLeaves int) {
	t.Helper()
	if expectedLeaves == 0 {
		if tr.root != aabbNullNode {
			t.Fatalf("expected empty tree, got root=%d", tr.root)
		}
		return
	}
	if tr.root == aabbNullNode {
		t.Fatalf("expected %d leaves, got empty tree", expectedLeaves)
	}
	if tr.nodes[tr.root].parent != aabbNullNode {
		t.Fatalf("root %d has non-null parent %d", tr.root, tr.nodes[tr.root].parent)
	}
	leaves := 0
	var walk func(id, expectedParent int32) int32
	walk = func(id, expectedParent int32) int32 {
		n := &tr.nodes[id]
		if n.parent != expectedParent {
			t.Fatalf("node %d parent=%d, want %d", id, n.parent, expectedParent)
		}
		if n.child1 == aabbNullNode {
			if n.child2 != aabbNullNode || n.height != 0 || n.payload < 0 {
				t.Fatalf("invalid leaf %d: %+v", id, *n)
			}
			leaves++
			return 0
		}
		if n.child2 == aabbNullNode {
			t.Fatalf("internal node %d missing child2", id)
		}
		h1 := walk(n.child1, id)
		h2 := walk(n.child2, id)
		wantH := 1 + max(h1, h2)
		if n.height != wantH {
			t.Fatalf("node %d height=%d, want %d", id, n.height, wantH)
		}
		if diff := h1 - h2; diff < -1 || diff > 1 {
			t.Fatalf("node %d imbalanced: h1=%d h2=%d", id, h1, h2)
		}
		if !n.bounds.contains(tr.nodes[n.child1].bounds) || !n.bounds.contains(tr.nodes[n.child2].bounds) {
			t.Fatalf("node %d bounds do not contain children", id)
		}
		return wantH
	}
	walk(tr.root, aabbNullNode)
	if leaves != expectedLeaves {
		t.Fatalf("counted %d leaves, want %d", leaves, expectedLeaves)
	}
}

func TestBroadphaseAABBTreeInvariants(t *testing.T) {
	var tr aabbTree
	ids := make([]int32, 0, 128)
	for i := 0; i < 128; i++ {
		x := float32((i*37)%40) - 20
		y := float32((i*17)%20) - 10
		z := float32((i*53)%40) - 20
		b := aabbBoundsFromBox(Box{Center: Vec3{X: x, Y: y, Z: z}, HalfExtents: Vec3{X: 0.5, Y: 0.5, Z: 0.5}}, broadphaseDynamicMargin)
		ids = append(ids, tr.createProxy(b, int32(i)))
	}
	verifyTreeInvariants(t, &tr, 128)

	// Move half the leaves and verify balance & containment.
	for i := 0; i < 64; i++ {
		x := float32((i*19)%30) - 15
		tight := aabbBoundsFromBox(Box{Center: Vec3{X: x, Y: 2, Z: float32(i) * 0.5}, HalfExtents: Vec3{X: 0.5, Y: 0.5, Z: 0.5}}, 0)
		tr.moveProxy(ids[i], tight, broadphaseDynamicMargin, Vec3{X: 1, Y: 0, Z: 0.25})
	}
	verifyTreeInvariants(t, &tr, 128)

	// Remove alternating leaves, then re-insert to exercise the free list.
	for i := 0; i < 128; i += 2 {
		tr.destroyProxy(ids[i])
	}
	verifyTreeInvariants(t, &tr, 64)

	for i := 0; i < 128; i += 2 {
		b := aabbBoundsFromBox(Box{Center: Vec3{X: float32(i), Y: 0, Z: 0}, HalfExtents: Vec3{X: 0.4, Y: 0.4, Z: 0.4}}, broadphaseDynamicMargin)
		ids[i] = tr.createProxy(b, int32(i))
	}
	verifyTreeInvariants(t, &tr, 128)

	for _, id := range ids {
		tr.destroyProxy(id)
	}
	verifyTreeInvariants(t, &tr, 0)
}

func TestBroadphaseMeshAndHeightfieldSingleEntry(t *testing.T) {
	w := NewWorld()
	// Build a 10x10 grid triangle mesh (200 triangles) and a 16x16 heightfield.
	const grid = 10
	verts := make([]Vec3, 0, (grid+1)*(grid+1))
	for z := 0; z <= grid; z++ {
		for x := 0; x <= grid; x++ {
			verts = append(verts, Vec3{X: float32(x), Y: 0, Z: float32(z)})
		}
	}
	indices := make([]uint32, 0, grid*grid*6)
	for z := 0; z < grid; z++ {
		for x := 0; x < grid; x++ {
			i0 := uint32(z*(grid+1) + x)
			i1 := i0 + 1
			i2 := i0 + uint32(grid+1)
			i3 := i2 + 1
			indices = append(indices, i0, i2, i1, i1, i2, i3)
		}
	}
	tm := NewTriMeshCollider(verts, indices)
	w.AddTriMesh(tm)

	heights := make([]float32, 16*16)
	hf := NewHeightfieldCollider(16, 16, 1.0, Vec3{X: 20, Y: 0, Z: 0}, heights)
	w.AddHeightfield(hf)

	// Static tree must contain exactly 2 leaves: one for the whole TriMesh and one for the whole Heightfield.
	w.bp.ensureStaticTree()
	verifyTreeInvariants(t, &w.bp.staticTree, 2)
	verifyTreeInvariants(t, &w.bp.dynamicTree, 0)
}

type bpTestVoxelGrid struct{}

func (bpTestVoxelGrid) VoxelSize() float32 { return 1.0 }
func (bpTestVoxelGrid) IsSolid(x, y, z int) bool {
	return y == -1 && x >= 10 && x <= 14 && z >= -2 && z <= 2
}

func makeTestHull(t *testing.T, scale float32) ConvexHull {
	t.Helper()
	pts := []Vec3{
		{X: -0.5 * scale, Y: -0.4 * scale, Z: -0.4 * scale},
		{X: 0.6 * scale, Y: -0.4 * scale, Z: -0.4 * scale},
		{X: -0.4 * scale, Y: -0.4 * scale, Z: 0.5 * scale},
		{X: 0.5 * scale, Y: -0.4 * scale, Z: 0.5 * scale},
		{X: -0.2 * scale, Y: 0.5 * scale, Z: -0.2 * scale},
		{X: 0.3 * scale, Y: 0.5 * scale, Z: 0.3 * scale},
		{X: 0.0, Y: 0.65 * scale, Z: -0.1 * scale},
	}
	h, err := NewConvexHull(pts, 16)
	if err != nil {
		t.Fatalf("NewConvexHull: %v", err)
	}
	return h
}

func buildParityWorld(t *testing.T, bruteForce bool) (*World, *Body, *Body, *TriMeshCollider, *HeightfieldCollider) {
	t.Helper()
	w := NewWorld()
	w.bp.bruteForce = bruteForce

	// Static floor + walls (including oriented box, cylinder pillar, convex hull rock).
	w.AddBody(NewStaticBody(BoxShape(Box{HalfExtents: Vec3{X: 30, Y: 0.5, Z: 30}}), Vec3{Y: -0.5}))
	wall := NewStaticBody(BoxShape(Box{HalfExtents: Vec3{X: 0.2, Y: 3, Z: 6}}), Vec3{X: 6, Y: 3, Z: 0})
	wall.Rot = QuatFromAxisAngle(Vec3{Y: 1}, 0.25)
	w.AddBody(wall)
	w.AddBody(NewStaticBody(CylinderShape(VerticalCylinder(Vec3{}, 0.7, 2.4)), Vec3{X: -4, Y: 1.2, Z: 3}))
	w.AddBody(NewStaticBody(ConvexHullShape(makeTestHull(t, 1.5)), Vec3{X: 3, Y: 0.6, Z: -4}))

	// Static TriMesh ramp and Heightfield patch.
	tm := NewTriMeshCollider(
		[]Vec3{{X: -8, Y: 0, Z: -2}, {X: -4, Y: 0, Z: -2}, {X: -4, Y: 0, Z: 2}, {X: -8, Y: 1.5, Z: 2}},
		[]uint32{0, 2, 1, 0, 3, 2},
	)
	w.AddTriMesh(tm)

	hf := NewHeightfieldCollider(4, 4, 2.0, Vec3{X: -16, Y: 0, Z: -3}, []float32{
		0.0, 0.2, 0.3, 0.1,
		0.2, 0.5, 0.4, 0.2,
		0.1, 0.4, 0.6, 0.2,
		0.0, 0.1, 0.2, 0.0,
	})
	w.AddHeightfield(hf)
	w.AddVoxelCollider(NewVoxelCollider(bpTestVoxelGrid{}))

	// Static trigger volume with buoyancy & drag (overlapping both static floor and dynamic bodies).
	trig := NewStaticBody(BoxShape(Box{HalfExtents: Vec3{X: 2.5, Y: 1.5, Z: 2.5}}), Vec3{X: -12, Y: 1.0, Z: 0})
	trig.IsTrigger = true
	trig.Area = AreaSettings{
		FluidDensity:     4.0,
		FluidLinearDrag:  2.0,
		FluidAngularDrag: 1.0,
		FluidVelocity:    Vec3{X: 0.8},
	}
	w.AddBody(trig)

	// Dynamic bodies of all 5 shapes: boxes, spheres, capsules, convex hulls, cylinders.
	b1 := w.AddBody(NewDynamicBody(BoxShape(Box{HalfExtents: Vec3{X: 0.5, Y: 0.5, Z: 0.5}}), Vec3{X: 0, Y: 0.6, Z: 0}, 2.0))
	b2 := NewDynamicBody(BoxShape(Box{HalfExtents: Vec3{X: 0.45, Y: 0.45, Z: 0.45}}), Vec3{X: 0.05, Y: 1.65, Z: 0}, 1.0)
	b2.Rot = QuatFromAxisAngle(Vec3{Z: 1}, 0.15)
	b2Ptr := w.AddBody(b2)
	w.AddJoint(NewDistanceJoint(b1, b2Ptr, Vec3{Y: 0.5}, Vec3{Y: -0.45}, 0.0, 0.25))

	hull := makeTestHull(t, 1.0)
	hBody := NewDynamicBody(ConvexHullShape(hull), Vec3{X: -12, Y: 2.5, Z: 0.2}, 1.5)
	hBody.Rot = QuatFromAxisAngle(Vec3{X: 1, Y: 0.5, Z: 0.2}.Norm(), 0.4)
	w.AddBody(hBody)

	cylBody := NewDynamicBody(CylinderShape(VerticalCylinder(Vec3{}, 0.35, 1.0)), Vec3{X: -6, Y: 2.2, Z: 0}, 1.2)
	cylBody.Rot = QuatFromAxisAngle(Vec3{Z: 1}, 0.2)
	w.AddBody(cylBody)

	capBody := NewDynamicBody(CapsuleShape(VerticalCapsule(Vec3{}, 0.3, 1.2)), Vec3{X: 12, Y: 1.8, Z: 0}, 1.0)
	w.AddBody(capBody)

	// Fast CCD sphere heading directly at the thin static wall at X=6.
	bullet := NewDynamicBody(SphereShape(Sphere{Radius: 0.15}), Vec3{X: 2.0, Y: 1.5, Z: 0}, 0.5)
	bullet.Vel = Vec3{X: 45, Y: 0, Z: 0}
	bullet.LinearDamping = 0
	w.AddBody(bullet)

	// Kinematic pusher + sleeping dynamic target.
	kin := NewStaticBody(BoxShape(Box{HalfExtents: Vec3{X: 0.5, Y: 0.5, Z: 0.5}}), Vec3{X: -2, Y: 0.5, Z: 5})
	kin.Static = false
	kin.Kinematic = true
	kinPtr := w.AddBody(kin)

	sleeper := w.AddBody(NewDynamicBody(BoxShape(Box{HalfExtents: Vec3{X: 0.4, Y: 0.4, Z: 0.4}}), Vec3{X: -0.5, Y: 0.41, Z: 5}, 1.0))

	// Layer-filtered bodies and ignored pair.
	l1 := NewDynamicBody(SphereShape(Sphere{Radius: 0.4}), Vec3{X: 2, Y: 0.5, Z: 4}, 1.0)
	l1.Layer = LayerBit(1)
	l1.Mask = LayerBit(0) | LayerBit(1)
	l1Ptr := w.AddBody(l1)
	l2 := NewDynamicBody(SphereShape(Sphere{Radius: 0.4}), Vec3{X: 2.3, Y: 0.5, Z: 4}, 1.0)
	l2.Layer = LayerBit(2)
	l2.Mask = LayerBit(0) | LayerBit(2)
	l2Ptr := w.AddBody(l2)
	w.IgnoreCollision(b1, l1Ptr)
	_ = l2Ptr

	return w, kinPtr, sleeper, tm, hf
}

func equalContactEvents(a, b []ContactEvent) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		ea, eb := a[i], b[i]
		if ea.Kind != eb.Kind || ea.IsTrigger != eb.IsTrigger {
			return false
		}
		if bodyIDOrZero(ea.BodyA) != bodyIDOrZero(eb.BodyA) || bodyIDOrZero(ea.BodyB) != bodyIDOrZero(eb.BodyB) {
			return false
		}
		if math.Float32bits(ea.Depth) != math.Float32bits(eb.Depth) ||
			math.Float32bits(ea.Point.X) != math.Float32bits(eb.Point.X) ||
			math.Float32bits(ea.Point.Y) != math.Float32bits(eb.Point.Y) ||
			math.Float32bits(ea.Point.Z) != math.Float32bits(eb.Point.Z) ||
			math.Float32bits(ea.Normal.X) != math.Float32bits(eb.Normal.X) ||
			math.Float32bits(ea.Normal.Y) != math.Float32bits(eb.Normal.Y) ||
			math.Float32bits(ea.Normal.Z) != math.Float32bits(eb.Normal.Z) {
			return false
		}
	}
	return true
}

func TestBroadphaseBitIdenticalToBruteForce(t *testing.T) {
	wBP, kinBP, sleeperBP, tmBP, hfBP := buildParityWorld(t, false)
	wBF, kinBF, sleeperBF, tmBF, hfBF := buildParityWorld(t, true)

	const dt = time.Second / 60
	for step := 0; step < 120; step++ {
		if step == 40 {
			// Move kinematic body directly into sleeper and mutate a body's position.
			kinBP.Pos.X += 0.6
			kinBF.Pos.X += 0.6
		}
		if step == 60 {
			// Add a new dynamic convex hull mid-simulation.
			extra := NewDynamicBody(ConvexHullShape(makeTestHull(t, 0.8)), Vec3{X: 0, Y: 4, Z: 0}, 1.0)
			wBP.AddBody(extra)
			wBF.AddBody(extra)
		}
		if step == 80 {
			// Remove a body and a static collider mid-simulation.
			wBP.RemoveBody(sleeperBP.ID)
			wBF.RemoveBody(sleeperBF.ID)
			wBP.RemoveTriMesh(tmBP.ID)
			wBF.RemoveTriMesh(tmBF.ID)
		}
		if step == 100 {
			wBP.RemoveHeightfield(hfBP.ID)
			wBF.RemoveHeightfield(hfBF.ID)
		}

		wBP.Step(dt)
		wBF.Step(dt)

		if wBP.Checksum() != wBF.Checksum() {
			t.Fatalf("step %d: Checksum mismatch broadphase=%#x bruteForce=%#x", step, wBP.Checksum(), wBF.Checksum())
		}
		if !equalContactEvents(wBP.ContactEvents(), wBF.ContactEvents()) {
			t.Fatalf("step %d: ContactEvents differ:\nBP=%+v\nBF=%+v", step, wBP.ContactEvents(), wBF.ContactEvents())
		}
	}

	verifyTreeInvariants(t, &wBP.bp.staticTree, countStaticLeaves(&wBP.bp))
	verifyTreeInvariants(t, &wBP.bp.dynamicTree, len(wBP.bp.dynamicList))
}

func countStaticLeaves(bp *broadphase3D) int {
	n := 0
	for _, pid := range bp.bodyProxies {
		if !bp.proxies[pid].inDynamic {
			n++
		}
	}
	for _, pid := range bp.triMeshProxies {
		if bp.proxies[pid].nodeID != aabbNullNode {
			n++
		}
	}
	for _, pid := range bp.hfProxies {
		if bp.proxies[pid].nodeID != aabbNullNode {
			n++
		}
	}
	return n
}

func equalRaycastHit(a, b RaycastHit) bool {
	ak, aid := hitTieOrder(a)
	bk, bid := hitTieOrder(b)
	return ak == bk && aid == bid &&
		math.Float32bits(a.T) == math.Float32bits(b.T) &&
		math.Float32bits(a.Distance) == math.Float32bits(b.Distance) &&
		math.Float32bits(a.Point.X) == math.Float32bits(b.Point.X) &&
		math.Float32bits(a.Point.Y) == math.Float32bits(b.Point.Y) &&
		math.Float32bits(a.Point.Z) == math.Float32bits(b.Point.Z) &&
		math.Float32bits(a.Normal.X) == math.Float32bits(b.Normal.X) &&
		math.Float32bits(a.Normal.Y) == math.Float32bits(b.Normal.Y) &&
		math.Float32bits(a.Normal.Z) == math.Float32bits(b.Normal.Z)
}

func equalOverlapHits(a, b []OverlapHit) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		ha, hb := a[i], b[i]
		if bodyIDOrZero(ha.Body) != bodyIDOrZero(hb.Body) {
			return false
		}
		tmA, tmB := 0, 0
		if ha.TriMesh != nil {
			tmA = ha.TriMesh.ID
		}
		if hb.TriMesh != nil {
			tmB = hb.TriMesh.ID
		}
		hfA, hfB := 0, 0
		if ha.Heightfield != nil {
			hfA = ha.Heightfield.ID
		}
		if hb.Heightfield != nil {
			hfB = hb.Heightfield.ID
		}
		vcA, vcB := 0, 0
		if ha.Voxel != nil {
			vcA = ha.Voxel.ID
		}
		if hb.Voxel != nil {
			vcB = hb.Voxel.ID
		}
		if tmA != tmB || hfA != hfB || vcA != vcB {
			return false
		}
		ca, cb := ha.Contact, hb.Contact
		if ca.Hit != cb.Hit ||
			math.Float32bits(ca.Depth) != math.Float32bits(cb.Depth) ||
			math.Float32bits(ca.Normal.X) != math.Float32bits(cb.Normal.X) ||
			math.Float32bits(ca.Normal.Y) != math.Float32bits(cb.Normal.Y) ||
			math.Float32bits(ca.Normal.Z) != math.Float32bits(cb.Normal.Z) ||
			math.Float32bits(ca.Point.X) != math.Float32bits(cb.Point.X) ||
			math.Float32bits(ca.Point.Y) != math.Float32bits(cb.Point.Y) ||
			math.Float32bits(ca.Point.Z) != math.Float32bits(cb.Point.Z) {
			return false
		}
	}
	return true
}

func TestBroadphaseQueriesBitIdenticalToBruteForce(t *testing.T) {
	wBP, _, _, _, _ := buildParityWorld(t, false)
	wBF, _, _, _, _ := buildParityWorld(t, true)

	// Add two coplanar overlapping static boxes to exercise exact distance/depth tie-breaking.
	tieBox := BoxShape(Box{HalfExtents: Vec3{X: 1, Y: 1, Z: 1}})
	wBP.AddBody(NewStaticBody(tieBox, Vec3{X: 0, Y: 2, Z: -8}))
	wBP.AddBody(NewStaticBody(tieBox, Vec3{X: 0, Y: 2, Z: -8}))
	wBF.AddBody(NewStaticBody(tieBox, Vec3{X: 0, Y: 2, Z: -8}))
	wBF.AddBody(NewStaticBody(tieBox, Vec3{X: 0, Y: 2, Z: -8}))

	const dt = time.Second / 60
	for range 15 {
		wBP.Step(dt)
		wBF.Step(dt)
	}

	// Test 128 diverse rays (including inside-origin ties, grazing rays, diagonal rays, layer masks).
	rays := [][2]Vec3{
		{{X: 0, Y: 2, Z: -8}, {X: 0, Y: 2, Z: -4}},  // inside overlapping tie boxes (T == 0)
		{{X: -5, Y: 2, Z: -8}, {X: 5, Y: 2, Z: -8}}, // hitting overlapping tie boxes at exact same T
		{{X: -20, Y: 5, Z: 0}, {X: 20, Y: -2, Z: 0}},
		{{X: -12, Y: 6, Z: 0}, {X: -12, Y: -3, Z: 0}},
		{{X: 12, Y: 5, Z: 0}, {X: 12, Y: -3, Z: 0}},
		{{X: 0, Y: 10, Z: 0}, {X: 0, Y: -5, Z: 0}},
	}
	for i := 0; i < 120; i++ {
		ang := float32(i) * 0.27
		ox := float32(math.Cos(float64(ang))) * 18
		oz := float32(math.Sin(float64(ang))) * 18
		oy := float32((i%7)-1) * 1.1
		tx := float32(math.Cos(float64(ang+2.3))) * 18
		tz := float32(math.Sin(float64(ang+2.3))) * 18
		ty := float32(((i+3)%5)-2) * 0.8
		rays = append(rays, [2]Vec3{{X: ox, Y: oy, Z: oz}, {X: tx, Y: ty, Z: tz}})
	}

	masks := []LayerMask{LayerAll, LayerBit(0) | LayerBit(1), LayerBit(2)}
	for _, mask := range masks {
		for idx, seg := range rays {
			hBP, okBP := wBP.RaycastFirstLayer(seg[0], seg[1], mask)
			hBF, okBF := wBF.RaycastFirstLayer(seg[0], seg[1], mask)
			if okBP != okBF || (okBP && !equalRaycastHit(hBP, hBF)) {
				t.Fatalf("ray %d mask %#x RaycastFirstLayer mismatch:\nBP=(%v,%+v)\nBF=(%v,%+v)", idx, mask, okBP, hBP, okBF, hBF)
			}

			allBP := wBP.RaycastLayer(seg[0], seg[1], mask)
			allBF := wBF.RaycastLayer(seg[0], seg[1], mask)
			if len(allBP) != len(allBF) {
				t.Fatalf("ray %d mask %#x RaycastLayer len BP=%d BF=%d", idx, mask, len(allBP), len(allBF))
			}
			for k := range allBP {
				if !equalRaycastHit(allBP[k], allBF[k]) {
					t.Fatalf("ray %d mask %#x RaycastLayer[%d] mismatch:\nBP=%+v\nBF=%+v", idx, mask, k, allBP[k], allBF[k])
				}
			}
		}
	}

	// Test diverse overlap shapes across layers.
	probes := []Shape{
		BoxShape(Box{Center: Vec3{X: 0, Y: 1, Z: 0}, HalfExtents: Vec3{X: 5, Y: 3, Z: 5}}),
		SphereShape(Sphere{Center: Vec3{X: -6, Y: 0.5, Z: 0}, Radius: 4}),
		CapsuleShape(VerticalCapsule(Vec3{X: 12, Y: 0, Z: 0}, 1.0, 4.0)),
		BoxShape(Box{Center: Vec3{X: 0, Y: 2, Z: -8}, HalfExtents: Vec3{X: 2, Y: 2, Z: 2}}),
		ConvexHullShape(makeTestHull(t, 3.0).Translated(Vec3{X: -12, Y: 1, Z: 0})),
		CylinderShape(VerticalCylinder(Vec3{X: 3, Y: 1, Z: -4}, 2.0, 3.0)),
	}
	for _, mask := range masks {
		for idx, probe := range probes {
			ovBP := wBP.OverlapLayer(probe, mask)
			ovBF := wBF.OverlapLayer(probe, mask)
			if !equalOverlapHits(ovBP, ovBF) {
				t.Fatalf("probe %d mask %#x OverlapLayer mismatch:\nBP=%+v\nBF=%+v", idx, mask, ovBP, ovBF)
			}
		}
	}

	// Test CharacterController.Move trajectory parity across flat, ramp, wall, cylinder, convex hull, and voxels.
	ccBP := NewCharacterController(Vec3{X: -10, Y: 2.0, Z: 0}, 0.3, 1.8)
	ccBF := NewCharacterController(Vec3{X: -10, Y: 2.0, Z: 0}, 0.3, 1.8)
	ccBP.PushForce = 25
	ccBF.PushForce = 25
	for i := 0; i < 80; i++ {
		wish := Vec3{X: 3.5, Z: float32(math.Sin(float64(i)*0.1)) * 1.5}
		if i == 25 {
			wish.Y = 5.0
		}
		rBP := ccBP.Move(wBP, wish, dt)
		rBF := ccBF.Move(wBF, wish, dt)
		if bodyIDOrZero(rBP.GroundBody) != bodyIDOrZero(rBF.GroundBody) {
			t.Fatalf("char move %d GroundBody ID mismatch: BP=%d BF=%d", i, bodyIDOrZero(rBP.GroundBody), bodyIDOrZero(rBF.GroundBody))
		}
		rBP.GroundBody = nil
		rBF.GroundBody = nil
		if rBP != rBF {
			t.Fatalf("char move %d mismatch:\nBP=%+v\nBF=%+v", i, rBP, rBF)
		}
	}
}

func TestBroadphaseSaveRestoreBitIdentical(t *testing.T) {
	w1, _, _, _, _ := buildParityWorld(t, false)
	const dt = time.Second / 60
	for range 30 {
		w1.Step(dt)
	}

	snap := w1.Save()
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	var decoded WorldSnapshot
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}

	w2 := NewWorld()
	vc := NewVoxelCollider(bpTestVoxelGrid{})
	vc.ID = w1.VoxelColliders()[0].ID
	w2.AddVoxelCollider(vc)
	if err := w2.Restore(decoded); err != nil {
		t.Fatalf("restore into fresh world: %v", err)
	}
	if w1.Checksum() != w2.Checksum() {
		t.Fatalf("post-restore checksum mismatch: %#x vs %#x", w1.Checksum(), w2.Checksum())
	}

	for step := 0; step < 60; step++ {
		w1.Step(dt)
		w2.Step(dt)
		if w1.Checksum() != w2.Checksum() {
			t.Fatalf("step %d post-restore checksum mismatch: %#x vs %#x", step, w1.Checksum(), w2.Checksum())
		}
	}

	// Verify raycast and overlap match on the restored world too.
	h1, ok1 := w1.RaycastFirst(Vec3{X: -15, Y: 2, Z: 0}, Vec3{X: 15, Y: 0, Z: 0})
	h2, ok2 := w2.RaycastFirst(Vec3{X: -15, Y: 2, Z: 0}, Vec3{X: 15, Y: 0, Z: 0})
	if ok1 != ok2 || (ok1 && !equalRaycastHit(h1, h2)) {
		t.Fatalf("post-restore RaycastFirst mismatch: (%v,%+v) vs (%v,%+v)", ok1, h1, ok2, h2)
	}

	// Also test in-place restore on w1 and re-step to the same final checksum.
	wantFinal := w1.Checksum()
	if err := w1.Restore(decoded); err != nil {
		t.Fatalf("in-place restore: %v", err)
	}
	for range 60 {
		w1.Step(dt)
	}
	if w1.Checksum() != wantFinal {
		t.Fatalf("in-place restore re-step mismatch: %#x vs %#x", w1.Checksum(), wantFinal)
	}
}

func TestBroadphaseExternalMutationsAndSync(t *testing.T) {
	w := NewWorld()
	b := w.AddBody(NewStaticBody(BoxShape(Box{HalfExtents: Vec3{X: 0.5, Y: 0.5, Z: 0.5}}), Vec3{X: 0, Y: 0, Z: 0}))

	// Move static body externally and sync explicitly before raycasting.
	b.Pos = Vec3{X: 10, Y: 0, Z: 0}
	w.SyncBroadphase()
	if hit, ok := w.RaycastFirst(Vec3{X: 10, Y: 5, Z: 0}, Vec3{X: 10, Y: -5, Z: 0}); !ok || hit.Body != b {
		t.Fatalf("expected hit at moved static position, got ok=%v hit=%+v", ok, hit)
	}
	if _, ok := w.RaycastFirst(Vec3{X: 0, Y: 5, Z: 0}, Vec3{X: 0, Y: -5, Z: 0}); ok {
		t.Fatalf("unexpected hit at old static position")
	}

	// Swap shape to ConvexHull and transition Static -> Dynamic + IsTrigger, then let Step reconcile automatically.
	b.Shape = ConvexHullShape(makeTestHull(t, 2.0))
	b.Static = false
	b.Mass = 1
	b.InvMass = 1
	b.IsTrigger = true
	b.Pos = Vec3{X: -8, Y: 2, Z: 0}
	w.Gravity = Vec3{}
	w.Step(time.Second / 60)

	if hit, ok := w.RaycastFirst(Vec3{X: -8, Y: 6, Z: 0}, Vec3{X: -8, Y: -2, Z: 0}); !ok || hit.Body != b {
		t.Fatalf("expected hit after Step reconciled shape+pos+flags, got ok=%v hit=%+v", ok, hit)
	}
	verifyTreeInvariants(t, &w.bp.staticTree, 0)
	verifyTreeInvariants(t, &w.bp.dynamicTree, 1)
}

func buildPerfScenarioWorld(numStatic, numDynamic int, bruteForce bool) *World {
	w := NewWorld()
	w.bp.bruteForce = bruteForce

	// 2000-body level grid of static boxes (floor tiles + pillars/obstacles).
	cols := 50
	for i := 0; i < numStatic; i++ {
		gx := float32(i%cols) * 4.0
		gz := float32(i/cols) * 4.0
		var b Body
		if i%5 == 0 {
			b = NewStaticBody(BoxShape(Box{HalfExtents: Vec3{X: 0.8, Y: 2.0, Z: 0.8}}), Vec3{X: gx, Y: 2.0, Z: gz})
		} else {
			b = NewStaticBody(BoxShape(Box{HalfExtents: Vec3{X: 1.9, Y: 0.5, Z: 1.9}}), Vec3{X: gx, Y: -0.5, Z: gz})
		}
		w.AddBody(b)
	}

	// Dynamic props spread across the level so collisions are local.
	for i := 0; i < numDynamic; i++ {
		gx := float32((i*7)%cols)*4.0 + 0.3
		gz := float32((i*11)%(max(1, numStatic/cols)))*4.0 + 0.3
		var shape Shape
		switch i % 3 {
		case 0:
			shape = BoxShape(Box{HalfExtents: Vec3{X: 0.4, Y: 0.4, Z: 0.4}})
		case 1:
			shape = SphereShape(Sphere{Radius: 0.4})
		default:
			shape = CapsuleShape(VerticalCapsule(Vec3{}, 0.3, 1.0))
		}
		d := NewDynamicBody(shape, Vec3{X: gx, Y: 1.2 + float32(i%4)*0.6, Z: gz}, 1.0)
		d.Vel = Vec3{X: float32((i%5)-2) * 0.5, Y: 0, Z: float32(((i*3)%5)-2) * 0.5}
		w.AddBody(d)
	}
	return w
}

func BenchmarkBroadphaseStep2000Static200Dynamic(b *testing.B) {
	w := buildPerfScenarioWorld(2000, 200, false)
	const dt = time.Second / 60
	// Warm up caches and settle initial overlaps for 30 frames.
	for range 30 {
		w.Step(dt)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Keep a subset of dynamic bodies actively moving every frame so we
		// benchmark active broadphase updates + narrowphase, not 100% sleeping.
		if i%30 == 0 {
			for k := 2000; k < 2040; k++ {
				w.bodies[k].ApplyImpulse(Vec3{X: 0.3, Y: 1.5, Z: -0.2})
			}
		}
		w.Step(dt)
	}
}

func BenchmarkBroadphase1000Raycasts(b *testing.B) {
	w := buildPerfScenarioWorld(2000, 200, false)
	const dt = time.Second / 60
	for range 10 {
		w.Step(dt)
	}

	type raySeg struct{ from, to Vec3 }
	rays := make([]raySeg, 1000)
	for i := range rays {
		x := float32((i*13)%190) + 2.0
		z := float32((i*29)%150) + 2.0
		ang := float32(i) * 0.17
		dx := float32(math.Cos(float64(ang))) * 35
		dz := float32(math.Sin(float64(ang))) * 35
		rays[i] = raySeg{
			from: Vec3{X: x, Y: 1.5, Z: z},
			to:   Vec3{X: x + dx, Y: 0.2, Z: z + dz},
		}
	}

	var hitCount int
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, r := range rays {
			if _, ok := w.RaycastFirst(r.from, r.to); ok {
				hitCount++
			}
		}
	}
	if hitCount == 0 {
		b.Fatal("expected raycast hits in perf scenario")
	}
}

func BenchmarkBroadphaseStep1000Bodies(b *testing.B) {
	b.Run("broadphase", func(b *testing.B) {
		w := buildPerfScenarioWorld(900, 100, false)
		const dt = time.Second / 60
		for range 10 {
			w.Step(dt)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if i%30 == 0 {
				for k := 900; k < 920; k++ {
					w.bodies[k].ApplyImpulse(Vec3{X: 0.2, Y: 1.0, Z: 0.2})
				}
			}
			w.Step(dt)
		}
	})
	b.Run("bruteforce", func(b *testing.B) {
		w := buildPerfScenarioWorld(900, 100, true)
		const dt = time.Second / 60
		for range 10 {
			w.Step(dt)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if i%30 == 0 {
				for k := 900; k < 920; k++ {
					w.bodies[k].ApplyImpulse(Vec3{X: 0.2, Y: 1.0, Z: 0.2})
				}
			}
			w.Step(dt)
		}
	})
}

func BenchmarkBroadphaseCharacterMove2000Static(b *testing.B) {
	w := buildPerfScenarioWorld(2000, 200, false)
	const dt = time.Second / 60
	w.Step(dt)
	cc := NewCharacterController(Vec3{X: 2.0, Y: 0.2, Z: 2.0}, 0.3, 1.8)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%120 == 0 {
			cc.Pos = Vec3{X: 2.0, Y: 0.2, Z: 2.0}
		}
		cc.Move(w, Vec3{X: 4.0, Z: 1.5}, dt)
	}
}

func TestBroadphaseCompoundAndStaticTriggerChildBitIdentical(t *testing.T) {
	build := func(bruteForce bool) (*World, *int) {
		w := NewWorld()
		w.bp.bruteForce = bruteForce
		w.Gravity = Vec3{Y: -9.81}

		// Static compound body with a solid base AND a trigger child (must be placed in
		// dynamic broadphase tree so static-vs-static trigger overlaps are still detected).
		staticSensor := NewStaticBody(CompoundShape(
			CompoundChild{
				Shape:  BoxShape(Box{HalfExtents: Vec3{X: 8, Y: 0.5, Z: 8}}),
				Offset: Vec3{Y: -0.5},
			},
			CompoundChild{
				Shape:   BoxShape(Box{HalfExtents: Vec3{X: 1.5, Y: 1.0, Z: 1.5}}),
				Offset:  Vec3{Y: 1.0},
				Trigger: true,
			},
		), Vec3{})
		w.AddBody(staticSensor)

		// Another static body overlapping staticSensor's trigger child.
		w.AddBody(NewStaticBody(
			SphereShape(Sphere{Radius: 0.4}),
			Vec3{X: 0.5, Y: 1.0, Z: 0},
		))

		// Dynamic compound body falling onto staticSensor and entering its trigger child.
		dynShape := CompoundShape(
			CompoundChild{
				Shape:  BoxShape(Box{HalfExtents: Vec3{X: 0.6, Y: 0.2, Z: 0.4}}),
				Offset: Vec3{Y: 0.3},
				Mass:   2,
			},
			CompoundChild{
				Shape:  SphereShape(Sphere{Radius: 0.3}),
				Offset: Vec3{X: -0.4, Y: -0.2},
				Mass:   1,
			},
			CompoundChild{
				Shape:   SphereShape(Sphere{Radius: 0.8}),
				Offset:  Vec3{X: 0.4, Y: 0.2},
				Trigger: true,
			},
		)
		dyn := w.AddBody(NewDynamicBody(dynShape, Vec3{X: -0.3, Y: 2.2, Z: 0.1}, 3))
		dyn.AngVel = Vec3{X: 0.5, Y: 0.2, Z: -0.4}

		triggers := 0
		w.OnTrigger = func(ev ContactEvent) {
			if ev.Kind == ContactEnter {
				triggers++
			}
		}
		return w, &triggers
	}

	wBP, trigBP := build(false)
	wBF, trigBF := build(true)

	const dt = time.Second / 60
	for step := range 90 {
		wBP.Step(dt)
		wBF.Step(dt)
		if wBP.Checksum() != wBF.Checksum() {
			t.Fatalf("step %d: Checksum mismatch BP=%#x BF=%#x", step, wBP.Checksum(), wBF.Checksum())
		}
	}
	if *trigBP == 0 || *trigBP != *trigBF {
		t.Fatalf("trigger count BP=%d BF=%d, want equal and > 0", *trigBP, *trigBF)
	}
}
