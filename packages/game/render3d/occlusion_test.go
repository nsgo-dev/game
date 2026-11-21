package render3d_test

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
)

func TestOccluderConstructors(t *testing.T) {
	vp, _ := testCamera() // cam at (0,0,10) looking down -Z

	// Default dimensions when <= 0.
	occ := render3d.NewOcclusionBuffer(0, 0)
	if occ.Width() != render3d.DefaultOcclusionWidth || occ.Height() != render3d.DefaultOcclusionHeight {
		t.Fatalf("default dims = %dx%d, want %dx%d", occ.Width(), occ.Height(), render3d.DefaultOcclusionWidth, render3d.DefaultOcclusionHeight)
	}
	if occ.Levels() < 2 {
		t.Fatalf("expected multi-level Hi-Z pyramid, got %d", occ.Levels())
	}

	// Empty box produces empty bounds and does not occlude anything.
	emptyBox := render3d.NewBoxOccluder(math3d.AABBEmpty(), math3d.Mat4Identity())
	if !emptyBox.Bounds.Empty() || len(emptyBox.Indices) != 0 {
		t.Fatalf("expected empty box occluder, got bounds=%+v indices=%d", emptyBox.Bounds, len(emptyBox.Indices))
	}

	// Box occluder at z=0 should occlude a box behind it at z=-10 and keep a box in front at z=5 visible.
	wallBounds := math3d.AABB{Min: math3d.Vec3{X: -6, Y: -4, Z: -0.5}, Max: math3d.Vec3{X: 6, Y: 4, Z: 0.5}}
	wall := render3d.NewBoxOccluder(wallBounds, math3d.Mat4Identity())
	if len(wall.Vertices) != 8 || len(wall.Indices) != 36 {
		t.Fatalf("box occluder verts/indices = %d/%d, want 8/36", len(wall.Vertices), len(wall.Indices))
	}

	occ.Clear()
	occ.Rasterize(vp, wall)
	occ.BuildPyramid()

	inFront := unitBoxAt(math3d.Vec3{Z: 5}, 0.5)
	behind := unitBoxAt(math3d.Vec3{Z: -10}, 0.5)
	if !occ.TestAABB(inFront, vp) {
		t.Fatal("object in front of box wall should be visible")
	}
	if occ.TestAABB(behind, vp) {
		t.Fatal("object strictly behind box wall should be occluded")
	}

	// Single-sided vs double-sided quad occluder.
	// Quad facing +Z (toward camera at z=10): CCW from +Z is (-6,-4,0)->(6,-4,0)->(6,4,0)->(-6,4,0).
	a := math3d.Vec3{X: -6, Y: -4, Z: 0}
	b := math3d.Vec3{X: 6, Y: -4, Z: 0}
	c := math3d.Vec3{X: 6, Y: 4, Z: 0}
	d := math3d.Vec3{X: -6, Y: 4, Z: 0}

	quadFront := render3d.NewQuadOccluder(a, b, c, d, false)
	quadBack := render3d.NewQuadOccluder(a, d, c, b, false) // CW from +Z (back-facing)
	quadDouble := render3d.NewQuadOccluder(a, d, c, b, true)

	occ.Clear()
	occ.Rasterize(vp, quadFront)
	occ.BuildPyramid()
	if occ.TestAABB(behind, vp) {
		t.Fatal("front-facing quad should occlude object behind it")
	}

	occ.Clear()
	occ.Rasterize(vp, quadBack)
	occ.BuildPyramid()
	if !occ.TestAABB(behind, vp) {
		t.Fatal("single-sided back-facing quad should NOT occlude")
	}

	occ.Clear()
	occ.Rasterize(vp, quadDouble)
	occ.BuildPyramid()
	if occ.TestAABB(behind, vp) {
		t.Fatal("double-sided quad should occlude from back side too")
	}

	// OccluderFromMesh: tessellated 2x2 grid wall (8 coplanar triangles, zero diagonal cracks)
	// plus degenerate and out-of-range triangles that must be skipped cleanly.
	gridVerts := []math3d.Vec3{
		{X: -6, Y: -4, Z: 0}, {X: 0, Y: -4, Z: 0}, {X: 6, Y: -4, Z: 0},
		{X: -6, Y: 0, Z: 0}, {X: 0, Y: 0, Z: 0}, {X: 6, Y: 0, Z: 0},
		{X: -6, Y: 4, Z: 0}, {X: 0, Y: 4, Z: 0}, {X: 6, Y: 4, Z: 0},
	}
	gridIdx := []uint32{
		0, 1, 4, 0, 4, 3,
		1, 2, 5, 1, 5, 4,
		3, 4, 7, 3, 7, 6,
		4, 5, 8, 4, 8, 7,
		// Degenerate (collinear) and out-of-range triangles:
		0, 1, 2,
		0, 1, 999,
	}
	meshOcc := render3d.OccluderFromMesh(gridVerts, gridIdx, math3d.Translate(math3d.Vec3{Z: -1}))
	if len(meshOcc.Indices) != 24 {
		t.Fatalf("OccluderFromMesh clean indices = %d, want 24", len(meshOcc.Indices))
	}
	occ.Clear()
	occ.Rasterize(vp, meshOcc)
	occ.BuildPyramid()
	// Object directly behind the center seam/diagonal intersection (0, 0, -10) must be occluded (zero cracks!).
	if occ.TestAABB(unitBoxAt(math3d.Vec3{X: 0, Y: 0, Z: -10}, 0.5), vp) {
		t.Fatal("tessellated OccluderFromMesh leaked at internal coplanar seam")
	}

	// Non-indexed triangle soup (indices == nil) with mirrored transform (negative determinant).
	soup := []math3d.Vec3{a, b, c, a, c, d}
	mirrored := render3d.OccluderFromMesh(soup, nil, math3d.Scale(math3d.Vec3{X: -1, Y: 1, Z: 1}))
	occ.Clear()
	occ.Rasterize(vp, mirrored)
	occ.BuildPyramid()
	if occ.TestAABB(behind, vp) {
		t.Fatal("mirrored OccluderFromMesh should flip winding and still occlude")
	}

	// Near-plane clipping of an occluder wall straddling the near plane (z from +15 behind cam to -5 in front).
	sideWall := render3d.NewBoxOccluder(
		math3d.AABB{Min: math3d.Vec3{X: -4, Y: -4, Z: -5}, Max: math3d.Vec3{X: 4, Y: 4, Z: 15}},
		math3d.Mat4Identity(),
	)
	occ.Clear()
	occ.Rasterize(vp, sideWall)
	occ.BuildPyramid()
	// Camera at (0,0,10) is INSIDE sideWall; front face (+Z at z=15) is behind camera, back faces are culled,
	// so nothing inside/in front should have corrupted depth (NaN/Inf).
	for y := range occ.Height() {
		for x := range occ.Width() {
			d := occ.DepthAt(x, y)
			if math.IsNaN(float64(d)) || math.IsInf(float64(d), 0) {
				t.Fatalf("non-finite depth %v at (%d,%d)", d, x, y)
			}
		}
	}

	// OpenGL [-1, 1] DepthNO mode with PerspectiveNO.
	camPos := math3d.Vec3{X: 0, Y: 0, Z: 10}
	view := math3d.LookAt(camPos, math3d.Vec3Zero(), math3d.Vec3Up())
	vpNO := math3d.PerspectiveNO(math3d.DegToRad(60), 16.0/9.0, 0.1, 100).Mul(view)
	occNO := render3d.NewOcclusionBuffer(128, 72)
	occNO.DepthNO = true
	occNO.Clear()
	occNO.Rasterize(vpNO, wall)
	occNO.BuildPyramid()
	if !occNO.TestAABB(inFront, vpNO) {
		t.Fatal("DepthNO: object in front should be visible")
	}
	if occNO.TestAABB(behind, vpNO) {
		t.Fatal("DepthNO: object behind wall should be occluded")
	}
}

func TestOcclusionIndoorScene(t *testing.T) {
	// Camera stands in Room A (corridor/lobby) looking down -Z toward a dividing wall
	// with an open doorway on the right (x in [2, 5], y in [0, 3]) leading into Room B,
	// and a sealed Room C behind Room B plus sealed left room behind the solid left wall.
	camPos := math3d.Vec3{X: 0, Y: 1.7, Z: 10}
	view := math3d.LookAt(camPos, math3d.Vec3{X: 0, Y: 1.5, Z: -10}, math3d.Vec3Up())
	proj := math3d.Perspective(math3d.DegToRad(65), 16.0/9.0, 0.1, 120)
	vp := proj.Mul(view)

	wallBoxes := []math3d.AABB{
		// Dividing wall 1 at z in [-0.5, 0.0], with open doorway at x in [2, 5], y in [0, 3].
		{Min: math3d.Vec3{X: -20, Y: -1, Z: -0.5}, Max: math3d.Vec3{X: 2.0, Y: 8, Z: 0}},  // left solid wall
		{Min: math3d.Vec3{X: 5.0, Y: -1, Z: -0.5}, Max: math3d.Vec3{X: 20, Y: 8, Z: 0}},   // right solid wall
		{Min: math3d.Vec3{X: 2.0, Y: 3.0, Z: -0.5}, Max: math3d.Vec3{X: 5.0, Y: 8, Z: 0}}, // doorway lintel
		// Dividing wall 2 at z in [-14.5, -14.0] completely sealing Room C behind Room B.
		{Min: math3d.Vec3{X: -25, Y: -1, Z: -14.5}, Max: math3d.Vec3{X: 25, Y: 10, Z: -14.0}},
	}

	occluders := make([]render3d.Occluder, len(wallBoxes))
	for i, wb := range wallBoxes {
		occluders[i] = render3d.NewBoxOccluder(wb, math3d.Mat4Identity())
	}

	var items []render3d.DrawItem
	mustBeVisible := make(map[uint64]string)
	var nextID uint64 = 1

	addItem := func(pos math3d.Vec3, half float32, matID uint32, visibleLabel string) {
		id := nextID
		nextID++
		items = append(items, render3d.DrawItem{
			ID:          id,
			PipelineID:  1,
			MaterialID:  matID,
			MeshID:      uint32(id % 8),
			Blend:       render3d.BlendOpaque,
			Transform:   math3d.Translate(pos),
			WorldBounds: unitBoxAt(pos, half),
			Triangles:   240,
			Vertices:    160,
		})
		if visibleLabel != "" {
			mustBeVisible[id] = visibleLabel
		}
	}

	// 12 visible meshes in current Room A (z in [2, 7]).
	for i := range 12 {
		x := float32(i%4)*1.8 - 2.7
		z := 2.0 + float32(i/4)*2.0
		addItem(math3d.Vec3{X: x, Y: 1.0, Z: z}, 0.4, uint32(1+i%3), fmt.Sprintf("roomA-%d", i))
	}

	// 6 visible meshes in Room B seen directly through the open doorway (x in [2.6, 4.4], y in [0.6, 2.2], z in [-11, -3]).
	for i := range 6 {
		x := 2.8 + float32(i%2)*1.2
		z := -3.0 - float32(i/2)*3.0
		addItem(math3d.Vec3{X: x, Y: 1.2, Z: z}, 0.35, uint32(10+i), fmt.Sprintf("doorway-%d", i))
	}

	// 20 hidden meshes in left room behind solid wall 1 (x in [-8, -1.5], y in [0.5, 4.0], z in [-12, -2]).
	for i := range 20 {
		x := -7.5 + float32(i%5)*1.4
		z := -3.0 - float32(i/5)*2.5
		addItem(math3d.Vec3{X: x, Y: 1.5, Z: z}, 0.45, uint32(20+i%5), "")
	}

	// 24 hidden meshes in sealed Room C behind wall 2 (x in [-9, 9], y in [0.5, 4.0], z in [-35, -17]).
	for i := range 24 {
		x := -8.0 + float32(i%6)*3.2
		z := -18.0 - float32(i/6)*4.5
		addItem(math3d.Vec3{X: x, Y: 1.5, Z: z}, 0.5, uint32(30+i%6), "")
	}

	occ := render3d.NewOcclusionBuffer(128, 72)
	occ.Clear()
	occ.Rasterize(vp, occluders...)
	occ.BuildPyramid()

	var q render3d.Queue
	planFrustum := q.Build(vp, camPos, items)
	planOccluded := q.BuildWithOcclusion(vp, camPos, items, occ)

	// Frustum-only should see almost everything in front of the camera.
	if planFrustum.Stats.VisibleItems < 50 {
		t.Fatalf("expected frustum-only to see >= 50 items, got %d", planFrustum.Stats.VisibleItems)
	}
	if planFrustum.Stats.Occluded != 0 {
		t.Fatalf("frustum-only Occluded = %d, want 0", planFrustum.Stats.Occluded)
	}

	// Occlusion culling must cut visible items & draw calls dramatically (all 44 hidden meshes culled!).
	if planOccluded.Stats.Occluded < 38 {
		t.Fatalf("expected >= 38 items occluded behind walls, got Occluded=%d (frustumVisible=%d occludedVisible=%d)",
			planOccluded.Stats.Occluded, planFrustum.Stats.VisibleItems, planOccluded.Stats.VisibleItems)
	}
	if planOccluded.Stats.TotalDrawCalls >= planFrustum.Stats.TotalDrawCalls/2 {
		t.Fatalf("expected draw calls to drop by >50%%: frustum=%d occluded=%d",
			planFrustum.Stats.TotalDrawCalls, planOccluded.Stats.TotalDrawCalls)
	}
	if planOccluded.Stats.VisibleItems+planOccluded.Stats.CulledItems != planOccluded.Stats.TotalItems {
		t.Fatalf("invariant broken: Visible(%d) + Culled(%d) != Total(%d)",
			planOccluded.Stats.VisibleItems, planOccluded.Stats.CulledItems, planOccluded.Stats.TotalItems)
	}
	if !strings.Contains(planOccluded.Stats.Summary(), fmt.Sprintf("occluded=%d", planOccluded.Stats.Occluded)) {
		t.Fatalf("Summary() missing occluded count: %s", planOccluded.Stats.Summary())
	}

	// Verify EVERY truly visible mesh (Room A + doorway Room B) survived occlusion culling.
	seen := make(map[uint64]bool, len(planOccluded.Opaque))
	for _, d := range planOccluded.Opaque {
		seen[d.ID] = true
	}
	for id, label := range mustBeVisible {
		if !seen[id] {
			t.Fatalf("truly visible item %q (ID=%d) was falsely culled!", label, id)
		}
	}

	// Verify Queue.Occluder field produces identical results to BuildWithOcclusion.
	qField := render3d.Queue{Occluder: occ}
	planField := qField.Build(vp, camPos, items)
	if planField.Stats != planOccluded.Stats {
		t.Fatalf("Queue.Occluder stats mismatch: %+v vs %+v", planField.Stats, planOccluded.Stats)
	}

	// Verify 0 steady-state allocations for Clear + Rasterize + BuildPyramid + BuildWithOcclusion.
	_ = q.BuildWithOcclusion(vp, camPos, items, occ)
	allocs := testing.AllocsPerRun(50, func() {
		occ.Clear()
		occ.Rasterize(vp, occluders...)
		occ.BuildPyramid()
		_ = q.BuildWithOcclusion(vp, camPos, items, occ)
	})
	if allocs != 0 {
		t.Fatalf("steady-state occlusion pipeline allocs = %v, want 0", allocs)
	}
}

// analyticallyVisible returns true if any sampled point of targetBounds lies
// inside the camera frustum (or straddles the near plane) and has an
// unobstructed analytic ray from camPos that does not intersect any wallBox
// before reaching the target point.
func analyticallyVisible(camPos math3d.Vec3, vp math3d.Mat4, target math3d.AABB, wallBoxes []math3d.AABB) bool {
	// If any corner of target crosses behind the near plane, treat as visible.
	xs := [2]float32{target.Min.X, target.Max.X}
	ys := [2]float32{target.Min.Y, target.Max.Y}
	zs := [2]float32{target.Min.Z, target.Max.Z}
	behindCount := 0
	for _, x := range xs {
		for _, y := range ys {
			for _, z := range zs {
				c := vp.MulVec4(math3d.Vec4{X: x, Y: y, Z: z, W: 1})
				if c.W <= 1e-5 || c.Z < 0 {
					behindCount++
				}
			}
		}
	}
	if behindCount == 8 {
		return false
	}
	if behindCount > 0 {
		return true
	}

	// Sample a 5x5x5 grid across targetBounds (covers all 8 corners, edges,
	// faces, and interior).
	const steps = 5
	for ix := range steps {
		tx := float32(ix) / float32(steps-1)
		px := target.Min.X + (target.Max.X-target.Min.X)*tx
		for iy := range steps {
			ty := float32(iy) / float32(steps-1)
			py := target.Min.Y + (target.Max.Y-target.Min.Y)*ty
			for iz := range steps {
				tz := float32(iz) / float32(steps-1)
				pz := target.Min.Z + (target.Max.Z-target.Min.Z)*tz

				p := math3d.Vec3{X: px, Y: py, Z: pz}
				c := vp.MulVec4(p.Vec4(1))
				if c.W <= 1e-5 || c.Z < 0 || c.Z > c.W {
					continue
				}
				invW := 1.0 / c.W
				nx, ny := c.X*invW, c.Y*invW
				if nx < -1 || nx > 1 || ny < -1 || ny > 1 {
					continue
				}

				delta := p.Sub(camPos)
				dist := delta.Len()
				if dist < 1e-5 {
					return true
				}
				ray := math3d.Ray{Origin: camPos, Dir: delta.Scale(1 / dist)}
				blocked := false
				for _, wb := range wallBoxes {
					if tMin, _, hit := ray.IntersectAABB(wb); hit && tMin < dist-1e-4 {
						blocked = true
						break
					}
				}
				if !blocked {
					return true
				}
			}
		}
	}
	return false
}

func TestOcclusionNoPoppingSweep(t *testing.T) {
	// Thick dividing wall at z in [-0.4, 0.0] with an open doorway at x in [-1.2, 1.2], y in [0, 3.0].
	wallBoxes := []math3d.AABB{
		{Min: math3d.Vec3{X: -14, Y: -1, Z: -0.4}, Max: math3d.Vec3{X: -1.2, Y: 7, Z: 0.0}},
		{Min: math3d.Vec3{X: 1.2, Y: -1, Z: -0.4}, Max: math3d.Vec3{X: 14, Y: 7, Z: 0.0}},
		{Min: math3d.Vec3{X: -1.2, Y: 3.0, Z: -0.4}, Max: math3d.Vec3{X: 1.2, Y: 7, Z: 0.0}},
	}
	occluders := make([]render3d.Occluder, len(wallBoxes))
	for i, wb := range wallBoxes {
		occluders[i] = render3d.NewBoxOccluder(wb, math3d.Mat4Identity())
	}

	type testObj struct {
		name   string
		bounds math3d.AABB
	}
	objects := []testObj{
		// Objects right at the left/right doorway edges (emerging & disappearing around corners).
		{name: "door-left-edge", bounds: unitBoxAt(math3d.Vec3{X: -1.3, Y: 1.4, Z: -2.0}, 0.45)},
		{name: "door-right-edge", bounds: unitBoxAt(math3d.Vec3{X: 1.3, Y: 1.4, Z: -2.0}, 0.45)},
		{name: "door-center-deep", bounds: unitBoxAt(math3d.Vec3{X: 0.0, Y: 1.3, Z: -6.0}, 0.5)},
		{name: "door-lintel-edge", bounds: unitBoxAt(math3d.Vec3{X: 0.0, Y: 2.8, Z: -2.5}, 0.35)},
		// Objects behind left & right walls that emerge as camera sweeps past the doorway.
		{name: "behind-left-near", bounds: unitBoxAt(math3d.Vec3{X: -3.2, Y: 1.5, Z: -3.5}, 0.5)},
		{name: "behind-right-near", bounds: unitBoxAt(math3d.Vec3{X: 3.2, Y: 1.5, Z: -3.5}, 0.5)},
		{name: "behind-left-deep", bounds: unitBoxAt(math3d.Vec3{X: -6.5, Y: 1.5, Z: -8.0}, 0.6)},
		{name: "behind-right-deep", bounds: unitBoxAt(math3d.Vec3{X: 6.5, Y: 1.5, Z: -8.0}, 0.6)},
		// Objects directly on the camera sweep path that straddle the near plane across frames!
		{name: "near-straddle-1", bounds: unitBoxAt(math3d.Vec3{X: -2.0, Y: 1.5, Z: 4.5}, 0.6)},
		{name: "near-straddle-2", bounds: unitBoxAt(math3d.Vec3{X: 0.5, Y: 1.5, Z: 3.8}, 0.6)},
		{name: "near-straddle-3", bounds: unitBoxAt(math3d.Vec3{X: 2.5, Y: 1.5, Z: 4.2}, 0.6)},
	}

	occ := render3d.NewOcclusionBuffer(128, 72)
	proj := math3d.Perspective(math3d.DegToRad(60), 16.0/9.0, 0.1, 80)

	const frames = 120
	falseNegatives := 0
	occludedCount := 0

	for f := range frames {
		tFrac := float64(f) / float64(frames-1)
		// Sweep smoothly from x=-5.5 to x=+5.5 while moving slightly forward/back
		// past the doorway and through the near-plane straddling objects.
		camX := float32(-5.5 + 11.0*tFrac)
		camZ := float32(4.2 + 1.2*math.Sin(tFrac*2*math.Pi))
		camPos := math3d.Vec3{X: camX, Y: 1.6, Z: camZ}
		lookTarget := math3d.Vec3{X: camX * 0.25, Y: 1.4, Z: -5.0}
		view := math3d.LookAt(camPos, lookTarget, math3d.Vec3Up())
		vp := proj.Mul(view)

		occ.Clear()
		occ.Rasterize(vp, occluders...)
		occ.BuildPyramid()

		for _, obj := range objects {
			analyticVis := analyticallyVisible(camPos, vp, obj.bounds, wallBoxes)
			hiZVis := occ.TestAABB(obj.bounds, vp)

			if analyticVis && !hiZVis {
				falseNegatives++
				t.Errorf("frame %d (cam=%v): FALSE NEGATIVE (popping!) on %q", f, camPos, obj.name)
			}
			if !hiZVis {
				occludedCount++
			}
		}
	}

	if falseNegatives > 0 {
		t.Fatalf("detected %d false-negative visibility drops across %d frames", falseNegatives, frames)
	}
	if occludedCount < 100 {
		t.Fatalf("expected substantial occlusion during 120-frame sweep, only occluded %d frame-object pairs", occludedCount)
	}
}

func BenchmarkOcclusionIndoor500(b *testing.B) {
	camPos := math3d.Vec3{X: 0, Y: 1.7, Z: 12}
	view := math3d.LookAt(camPos, math3d.Vec3{X: 0, Y: 1.5, Z: -12}, math3d.Vec3Up())
	proj := math3d.Perspective(math3d.DegToRad(65), 16.0/9.0, 0.1, 120)
	vp := proj.Mul(view)

	// Realistic indoor multi-room dividing walls & pillars.
	wallBoxes := []math3d.AABB{
		{Min: math3d.Vec3{X: -20, Y: -1, Z: 2.0}, Max: math3d.Vec3{X: -3.0, Y: 8, Z: 2.5}},
		{Min: math3d.Vec3{X: 3.0, Y: -1, Z: 2.0}, Max: math3d.Vec3{X: 20, Y: 8, Z: 2.5}},
		{Min: math3d.Vec3{X: -3.0, Y: 3.5, Z: 2.0}, Max: math3d.Vec3{X: 3.0, Y: 8, Z: 2.5}},
		{Min: math3d.Vec3{X: -20, Y: -1, Z: -10.5}, Max: math3d.Vec3{X: 20, Y: 8, Z: -10.0}},
		{Min: math3d.Vec3{X: -1.5, Y: -1, Z: -4.5}, Max: math3d.Vec3{X: 1.5, Y: 6, Z: -3.5}},
		{Min: math3d.Vec3{X: -10, Y: -1, Z: -4.5}, Max: math3d.Vec3{X: -7, Y: 6, Z: -3.5}},
		{Min: math3d.Vec3{X: 7, Y: -1, Z: -4.5}, Max: math3d.Vec3{X: 10, Y: 6, Z: -3.5}},
		{Min: math3d.Vec3{X: -22, Y: -1, Z: -30}, Max: math3d.Vec3{X: -19, Y: 8, Z: 15}},
		{Min: math3d.Vec3{X: 19, Y: -1, Z: -30}, Max: math3d.Vec3{X: 22, Y: 8, Z: 15}},
	}
	occluders := make([]render3d.Occluder, len(wallBoxes))
	for i, wb := range wallBoxes {
		occluders[i] = render3d.NewBoxOccluder(wb, math3d.Mat4Identity())
	}

	const count = 500
	items := make([]render3d.DrawItem, count)
	for i := range count {
		x := float32((i%20)-10) * 1.6
		y := float32((i/20)%3)*1.4 + 0.6
		z := 8.0 - float32(i/60)*4.5
		pos := math3d.Vec3{X: x, Y: y, Z: z}
		items[i] = render3d.DrawItem{
			ID:          uint64(i + 1),
			PipelineID:  uint32(1 + i%2),
			MaterialID:  uint32(1 + i%8),
			MeshID:      uint32(1 + i%16),
			Blend:       render3d.BlendOpaque,
			Transform:   math3d.Translate(pos),
			WorldBounds: unitBoxAt(pos, 0.45),
			Triangles:   180,
			Vertices:    120,
		}
	}

	occ := render3d.NewOcclusionBuffer(128, 72)
	var q render3d.Queue

	// Warm up Queue internal buffers so steady-state frame allocations are 0.
	occ.Clear()
	occ.Rasterize(vp, occluders...)
	occ.BuildPyramid()
	plan := q.BuildWithOcclusion(vp, camPos, items, occ)
	if plan.Stats.Occluded == 0 {
		b.Fatalf("expected benchmark scene to occlude items, got Occluded=0")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		occ.Clear()
		occ.Rasterize(vp, occluders...)
		occ.BuildPyramid()
		_ = q.BuildWithOcclusion(vp, camPos, items, occ)
	}
}
