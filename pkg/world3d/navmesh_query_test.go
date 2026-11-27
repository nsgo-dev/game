package world3d

import (
	"math"
	"sync"
	"testing"

	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/random"
)

func buildGridNavMesh3D(cols, rows int, cellSize float32) *NavMesh {
	polys := make([][]math3d.Vec3, 0, cols*rows)
	for r := range rows {
		for c := range cols {
			x0 := float32(c) * cellSize
			z0 := float32(r) * cellSize
			x1 := float32(c+1) * cellSize
			z1 := float32(r+1) * cellSize
			polys = append(polys, []math3d.Vec3{
				{X: x0, Y: 0, Z: z0},
				{X: x0, Y: 0, Z: z1},
				{X: x1, Y: 0, Z: z1},
				{X: x1, Y: 0, Z: z0},
			})
		}
	}
	return BuildNavMesh(polys, math3d.DegToRad(45))
}

// buildStackedFloorsMesh3D creates:
//   - Poly 0: ground floor [0,10] x [0,10] at Y=0 (walk)
//   - Poly 1: ground floor extension [10,20] x [0,10] at Y=0 (walk)
//   - Poly 2: sloped ramp from X=10..20, Z=10..14 rising from Y=0 to Y=5 (attached to poly 1 along Z=10? Wait: let's connect cleanly along X=20!)
//
// Actually let's make it super clean:
//   - Poly 0: ground floor [0,10] x [0,10] at Y=0
//   - Poly 1: ground floor [10,20] x [0,10] at Y=0 (shares X=10 edge with poly 0)
//   - Poly 2: upper deck [0,10] x [0,10] at Y=5 (directly stacked above poly 0, connected ONLY via poly 3 ramp or disconnected depending on includeRamp)
//   - Poly 3 (optional): sloped ramp connecting X=10..20, Z=0..10 rising linearly from Y=5 at X=10 to Y=0 at X=20 (shares edge X=10, Y=5 with poly 2 and edge X=20, Y=0 with poly 1? No, poly 1 is X=10..20 so ramp at X=20..30 or Z=10..20!)
//
// Let's place ramp along Z=0..10? No, keep non-overlapping ramp:
//   - Poly 0: ground [0,10] x [0,10], Y=0
//   - Poly 1: ground [10,20] x [0,10], Y=0
//   - Poly 2: ramp [10,20] x [10,20] rising from Y=0 at Z=10 (shared with poly 1) to Y=5 at Z=20 (slope atan(0.5) ≈ 26.6 deg)
//   - Poly 3: upper landing [0,10] x [10,20], Y=5 (shared with poly 2 along X=10? Wait: poly 2 has Y varying along Z, so shared edge with upper landing is at Z=20, Y=5!)
//
// Let's make an exact U-shaped 2-story bridge:
//   - Poly 0: lower floor [0,10] x [0,10], Y=0
//   - Poly 1: lower east [10,20] x [0,10], Y=0 (shares X=10 with poly 0)
//   - Poly 2: ramp [10,20] x [10,20], Y=0 at Z=10 -> Y=4 at Z=20 (shares Z=10, Y=0 with poly 1; slope atan(0.4)=21.8 deg)
//   - Poly 3: upper north-east [10,20] x [20,30], Y=4 (shares Z=20, Y=4 with poly 2)
//   - Poly 4: upper bridge [0,10] x [20,30], Y=4 (shares X=10, Y=4 with poly 3)
//   - Poly 5: upper overpass [0,10] x [0,20], Y=4 (directly above poly 0 at Y=4, shares Z=20, Y=4 with poly 4 when connected!)
func buildStackedFloorsMesh3D(connectOverpass bool) *NavMesh {
	inputs := []NavPolygonInput{
		// 0: lower floor [0,10]x[0,10] at Y=0
		{Vertices: []math3d.Vec3{{X: 0, Y: 0, Z: 0}, {X: 0, Y: 0, Z: 10}, {X: 10, Y: 0, Z: 10}, {X: 10, Y: 0, Z: 0}}, Area: NavAreaGrass, Flags: NavFlagWalk},
		// 1: lower east [10,20]x[0,10] at Y=0
		{Vertices: []math3d.Vec3{{X: 10, Y: 0, Z: 0}, {X: 10, Y: 0, Z: 10}, {X: 20, Y: 0, Z: 10}, {X: 20, Y: 0, Z: 0}}, Area: NavAreaRoad, Flags: NavFlagWalk},
		// 2: ramp [10,20]x[10,20], Y=0 at Z=10 -> Y=4 at Z=20
		{Vertices: []math3d.Vec3{{X: 10, Y: 0, Z: 10}, {X: 10, Y: 4, Z: 20}, {X: 20, Y: 4, Z: 20}, {X: 20, Y: 0, Z: 10}}, Area: NavAreaRoad, Flags: NavFlagWalk},
		// 3: upper landing [10,20]x[20,30] at Y=4
		{Vertices: []math3d.Vec3{{X: 10, Y: 4, Z: 20}, {X: 10, Y: 4, Z: 30}, {X: 20, Y: 4, Z: 30}, {X: 20, Y: 4, Z: 20}}, Area: NavAreaRoad, Flags: NavFlagWalk},
	}
	if connectOverpass {
		// 4: upper bridge [0,10]x[20,30] at Y=4
		inputs = append(inputs, NavPolygonInput{
			Vertices: []math3d.Vec3{{X: 0, Y: 4, Z: 20}, {X: 0, Y: 4, Z: 30}, {X: 10, Y: 4, Z: 30}, {X: 10, Y: 4, Z: 20}},
			Area:     NavAreaRoad,
			Flags:    NavFlagWalk,
		})
		// 5: upper overpass [0,10]x[0,20] at Y=4 directly stacked above poly 0!
		inputs = append(inputs, NavPolygonInput{
			Vertices: []math3d.Vec3{{X: 0, Y: 4, Z: 0}, {X: 0, Y: 4, Z: 20}, {X: 10, Y: 4, Z: 20}, {X: 10, Y: 4, Z: 0}},
			Area:     NavAreaRoad,
			Flags:    NavFlagWalk,
		})
	} else {
		// 4: disconnected upper deck directly stacked above poly 0 at Y=4
		inputs = append(inputs, NavPolygonInput{
			Vertices: []math3d.Vec3{{X: 0, Y: 4, Z: 0}, {X: 0, Y: 4, Z: 10}, {X: 10, Y: 4, Z: 10}, {X: 10, Y: 4, Z: 0}},
			Area:     NavAreaRoad,
			Flags:    NavFlagWalk,
		})
	}
	return BuildNavMeshWithAreas(inputs, math3d.DegToRad(45))
}

func TestNavMesh3DNearestPointStackedFloorsAndFilter(t *testing.T) {
	nm := buildStackedFloorsMesh3D(false)

	// Near lower floor: selects poly 0 at Y=0.
	pt, poly, ok := nm.NearestPoint(math3d.Vec3{X: 5, Y: 0.6, Z: 5}, math3d.Vec3{X: 2, Y: 1.0, Z: 2})
	if !ok || poly != 0 || math.Abs(float64(pt.Y)) > 1e-4 {
		t.Fatalf("lower floor NearestPoint = (%v, %d, %v), want poly 0 at Y=0", pt, poly, ok)
	}

	// Near upper stacked floor: selects poly 4 at Y=4.
	pt, poly, ok = nm.NearestPoint(math3d.Vec3{X: 5, Y: 3.4, Z: 5}, math3d.Vec3{X: 2, Y: 1.0, Z: 2})
	if !ok || poly != 4 || math.Abs(float64(pt.Y-4)) > 1e-4 {
		t.Fatalf("upper floor NearestPoint = (%v, %d, %v), want poly 4 at Y=4", pt, poly, ok)
	}

	// Mid-air between stacked floors with tight vertical extent fails cleanly.
	if _, poly, ok := nm.NearestPoint(math3d.Vec3{X: 5, Y: 2.0, Z: 5}, math3d.Vec3{X: 2, Y: 1.0, Z: 2}); ok || poly != -1 {
		t.Fatalf("mid-air NearestPoint should fail, got poly=%d ok=%v", poly, ok)
	}

	// Outside horizontally within extent snaps to upper deck edge at X=0, Y=4.
	pt, poly, ok = nm.NearestPoint(math3d.Vec3{X: -1.5, Y: 4.1, Z: 5}, math3d.Vec3{X: 2.0, Y: 0.5, Z: 2.0})
	if !ok || poly != 4 || math.Abs(float64(pt.X)) > 1e-4 || math.Abs(float64(pt.Y-4)) > 1e-4 {
		t.Fatalf("upper edge NearestPoint = (%v, %d, %v), want (0,4,5) poly 4", pt, poly, ok)
	}

	// Narrow vertical extent on sloped ramp (poly 2 rises Y=0 at Z=10 -> Y=4 at Z=20):
	// query at (15, 0.2, 14) has ramp height 1.6 directly below; unconstrained 3D closest point is near Y=1.4 (outside ext.Y=0.5),
	// yet the ramp passes through Y=0..0.7 near Z=10..11.75 inside extent (5, 0.5, 5). Clipping must find a point strictly inside extent!
	qPos := math3d.Vec3{X: 15, Y: 0.2, Z: 14}
	qExt := math3d.Vec3{X: 5, Y: 0.5, Z: 5}
	rampOnly := DefaultNavFilter()
	rampOnly.ExcludeArea(NavAreaGrass)
	nm.SetPolygonArea(1, NavAreaGrass) // exclude lower east so only ramp/upper remain
	pt, poly, ok = nm.NearestPointWithFilter(qPos, qExt, rampOnly)
	nm.SetPolygonArea(1, NavAreaRoad)
	if !ok || poly != 2 {
		t.Fatalf("clipped ramp NearestPoint = (%v, %d, %v), want poly 2", pt, poly, ok)
	}
	if math.Abs(float64(pt.X-qPos.X)) > float64(qExt.X)+1e-3 ||
		math.Abs(float64(pt.Y-qPos.Y)) > float64(qExt.Y)+1e-3 ||
		math.Abs(float64(pt.Z-qPos.Z)) > float64(qExt.Z)+1e-3 {
		t.Fatalf("clipped ramp NearestPoint %v escaped extent %v around %v", pt, qExt, qPos)
	}

	// Filter excludes disabled upper floor; larger vertical extent falls through to lower floor.
	nm.AddPolygonFlags(4, NavFlagDisabled)
	pt, poly, ok = nm.NearestPoint(math3d.Vec3{X: 5, Y: 3.5, Z: 5}, math3d.Vec3{X: 2, Y: 4.0, Z: 2})
	if !ok || poly != 0 || math.Abs(float64(pt.Y)) > 1e-4 {
		t.Fatalf("disabled upper deck fallback = (%v, %d, %v), want poly 0 at Y=0", pt, poly, ok)
	}
	nm.ClearPolygonFlags(4, NavFlagDisabled)

	// Invalid inputs.
	var nilNM *NavMesh
	if _, _, ok := nilNM.NearestPoint(math3d.Vec3{}, math3d.Vec3{X: 1, Y: 1, Z: 1}); ok {
		t.Fatalf("nil mesh should fail")
	}
	if _, _, ok := nm.NearestPoint(math3d.Vec3{X: 5, Y: 0, Z: 5}, math3d.Vec3{X: 1, Y: -1, Z: 1}); ok {
		t.Fatalf("negative extent should fail")
	}
}

func TestNavMesh3DRaycastStackedFloorsRampAndFilter(t *testing.T) {
	nm := buildStackedFloorsMesh3D(true)

	// Ray on lower floor from poly 0 -> poly 1 passes directly beneath upper overpass (poly 5) without changing level.
	hit, obstructed := nm.Raycast(math3d.Vec3{X: 2, Y: 0, Z: 5}, math3d.Vec3{X: 18, Y: 0, Z: 5})
	if obstructed || hit.Hit || hit.Poly != 1 || math.Abs(float64(hit.Point.Y)) > 1e-4 {
		t.Fatalf("lower floor raycast = (%+v, %v), want clear hit on poly 1 at Y=0", hit, obstructed)
	}

	// Ray on upper overpass (poly 5 at Y=4) heading east (+X) stops at the bridge railing X=10 (does NOT drop onto poly 1 below!).
	hit, obstructed = nm.Raycast(math3d.Vec3{X: 5, Y: 4, Z: 5}, math3d.Vec3{X: 18, Y: 4, Z: 5})
	if !obstructed || !hit.Hit || hit.Poly != 5 {
		t.Fatalf("upper overpass railing raycast = (%+v, %v), want hit on poly 5", hit, obstructed)
	}
	if math.Abs(float64(hit.Point.X-10)) > 1e-3 || math.Abs(float64(hit.Point.Y-4)) > 1e-3 {
		t.Fatalf("upper overpass hit point = %v, want (10, 4, 5)", hit.Point)
	}
	if math.Abs(float64(hit.Normal.X-(-1))) > 1e-3 || math.Abs(float64(hit.Normal.Y)) > 1e-3 || math.Abs(float64(hit.Normal.Z)) > 1e-3 {
		t.Fatalf("upper overpass hit normal = %v, want (-1, 0, 0)", hit.Normal)
	}

	// Ray climbing the ramp from poly 1 (Y=0) across poly 2 (ramp) onto poly 3 (Y=4).
	hit, obstructed = nm.Raycast(math3d.Vec3{X: 15, Y: 0, Z: 5}, math3d.Vec3{X: 15, Y: 4, Z: 25})
	if obstructed || hit.Hit || hit.Poly != 3 || math.Abs(float64(hit.Point.Y-4)) > 1e-3 {
		t.Fatalf("ramp climb raycast = (%+v, %v), want clear on poly 3 at Y=4", hit, obstructed)
	}

	// Ray hitting side wall of sloped ramp returns a 3D surface-tangent inward normal orthogonal to ramp normal.
	hit, obstructed = nm.Raycast(math3d.Vec3{X: 15, Y: 2, Z: 15}, math3d.Vec3{X: 25, Y: 2, Z: 15})
	if !obstructed || !hit.Hit || hit.Poly != 2 {
		t.Fatalf("ramp side raycast = (%+v, %v), want hit on ramp poly 2", hit, obstructed)
	}
	if math.Abs(float64(hit.Point.X-20)) > 1e-3 || math.Abs(float64(hit.Point.Y-2)) > 1e-3 {
		t.Fatalf("ramp side hit point = %v, want (20, 2, 15)", hit.Point)
	}
	if math.Abs(float64(hit.Normal.Dot(nm.Polys[2].Normal))) > 1e-3 {
		t.Fatalf("ramp side hit normal %v not tangent to ramp plane (normal %v)", hit.Normal, nm.Polys[2].Normal)
	}

	// Disabling the ramp blocks the ray at Z=10.
	nm.AddPolygonFlags(2, NavFlagDisabled)
	hit, obstructed = nm.Raycast(math3d.Vec3{X: 15, Y: 0, Z: 5}, math3d.Vec3{X: 15, Y: 4, Z: 25})
	if !obstructed || !hit.Hit || hit.Poly != 1 || math.Abs(float64(hit.Point.Z-10)) > 1e-3 {
		t.Fatalf("disabled ramp raycast = (%+v, %v), want hit at Z=10 on poly 1", hit, obstructed)
	}
	nm.ClearPolygonFlags(2, NavFlagDisabled)

	// Corner grazing across a 2x2 3D grid passes cleanly.
	grid := buildGridNavMesh3D(2, 2, 10)
	hit, obstructed = grid.Raycast(math3d.Vec3{X: 2, Y: 0, Z: 2}, math3d.Vec3{X: 18, Y: 0, Z: 18})
	if obstructed || hit.Hit || math.Abs(float64(hit.T-1)) > 1e-4 {
		t.Fatalf("3D diagonal corner-grazing raycast failed: (%+v, %v)", hit, obstructed)
	}
}

func TestNavMesh3DMoveAlongSurfaceStackedFloorsRampAndDiagonal(t *testing.T) {
	nm := buildStackedFloorsMesh3D(true)

	// Slide diagonally along upper overpass railing (poly 5 -> poly 4 at Y=4) without falling to lower floor.
	res, ok := nm.MoveAlongSurface(math3d.Vec3{X: 5, Y: 4, Z: 10}, math3d.Vec3{X: 15, Y: 4, Z: 25})
	if !ok || !res.HitBoundary {
		t.Fatalf("upper overpass slide = (%+v, %v), want HitBoundary=true", res, ok)
	}
	if math.Abs(float64(res.Point.Y-4)) > 1e-3 {
		t.Fatalf("upper overpass slide fell off upper floor: Y=%v, want 4", res.Point.Y)
	}
	if !nm.Polys[res.Poly].ContainsXZ(res.Point.X, res.Point.Z) {
		t.Fatalf("final point %v outside returned poly %d", res.Point, res.Poly)
	}

	// Climb ramp smoothly tracking Y height.
	res, ok = nm.MoveAlongSurface(math3d.Vec3{X: 15, Y: 0, Z: 5}, math3d.Vec3{X: 15, Y: 4, Z: 15})
	if !ok || res.HitBoundary || res.Poly != 2 || math.Abs(float64(res.Point.Y-2)) > 1e-3 {
		t.Fatalf("ramp MoveAlongSurface = (%+v, %v), want poly 2 at Y=2", res, ok)
	}

	// 45-degree diagonal wall in 3D: exact float64 sliding prevents premature float32 stall.
	diag3D := BuildNavMesh([][]math3d.Vec3{
		{{X: 0, Y: 0, Z: 0}, {X: 0, Y: 0, Z: 10}, {X: 10, Y: 0, Z: 0}},
	}, math3d.DegToRad(45))
	res, ok = diag3D.MoveAlongSurface(math3d.Vec3{X: 2, Y: 0, Z: 2}, math3d.Vec3{X: 10, Y: 0, Z: 6})
	if !ok || !res.HitBoundary {
		t.Fatalf("3D diagonal slide = (%+v, %v), want HitBoundary=true", res, ok)
	}
	if math.Abs(float64(res.Point.X-7)) > 0.05 || math.Abs(float64(res.Point.Z-3)) > 0.05 {
		t.Fatalf("3D diagonal slide Point = %v, want ~(7, 0, 3)", res.Point)
	}
}

func TestNavMesh3DRandomPointDeterminismSurfaceAreaAndStackedReachability(t *testing.T) {
	// 1. Disconnected upper deck directly above poly 0 at Y=4 (3D distance 4 < radius 8).
	disconnected := buildStackedFloorsMesh3D(false)
	center := math3d.Vec3{X: 5, Y: 0, Z: 5}
	const radius float32 = 8.0

	rngA := random.New(20261125)
	rngB := random.New(20261125)
	q := NewNavMeshQuery(disconnected)
	for i := range 300 {
		pA, polyA, okA := disconnected.RandomPointAround(center, radius, rngA)
		pB, polyB, okB := q.RandomPointAround(center, radius, rngB)
		if !okA || !okB {
			t.Fatalf("RandomPointAround failed at %d", i)
		}
		if polyA != polyB ||
			math.Float32bits(pA.X) != math.Float32bits(pB.X) ||
			math.Float32bits(pA.Y) != math.Float32bits(pB.Y) ||
			math.Float32bits(pA.Z) != math.Float32bits(pB.Z) {
			t.Fatalf("3D RandomPointAround determinism mismatch at %d", i)
		}
		if polyA == 4 {
			t.Fatalf("disconnected upper deck poly 4 must never be returned by RandomPointAround from lower floor")
		}
		if pA.Dist(center) > radius+1e-3 {
			t.Fatalf("3D RandomPointAround point %v exceeded 3D radius %v from %v", pA, radius, center)
		}
		if !disconnected.Polys[polyA].ContainsXZ(pA.X, pA.Z) ||
			math.Abs(float64(pA.Y-disconnected.Polys[polyA].HeightAt(pA.X, pA.Z))) > 1e-3 {
			t.Fatalf("3D RandomPointAround point %v not on poly %d surface", pA, polyA)
		}
	}

	// 2. Surface-area weighting on flat vs steep 3D slope (same XZ footprint 10x10, but sloped ramp has 3D area 10*sqrt(10^2+8^2) ≈ 128.06 vs flat 100).
	areaMesh := BuildNavMeshWithAreas([]NavPolygonInput{
		{Vertices: []math3d.Vec3{{X: 0, Y: 0, Z: 0}, {X: 0, Y: 0, Z: 10}, {X: 10, Y: 0, Z: 10}, {X: 10, Y: 0, Z: 0}}, Area: NavAreaGrass, Flags: NavFlagWalk},
		{Vertices: []math3d.Vec3{{X: 10, Y: 0, Z: 0}, {X: 10, Y: 0, Z: 10}, {X: 20, Y: 8, Z: 10}, {X: 20, Y: 8, Z: 0}}, Area: NavAreaRoad, Flags: NavFlagWalk},
		{Vertices: []math3d.Vec3{{X: 20, Y: 8, Z: 0}, {X: 20, Y: 8, Z: 10}, {X: 30, Y: 8, Z: 10}, {X: 30, Y: 8, Z: 0}}, Area: NavAreaWater, Flags: NavFlagSwim},
	}, math3d.DegToRad(45))

	landFilter := DefaultNavFilter()
	landFilter.ExcludeFlags |= NavFlagSwim
	rng := random.New(99)
	const samples = 4000
	rampHits := 0
	for range samples {
		pt, poly, ok := areaMesh.RandomPointWithFilter(rng, landFilter)
		if !ok || poly == 2 {
			t.Fatalf("unexpected RandomPointWithFilter result: (%v, %d, %v)", pt, poly, ok)
		}
		if poly == 1 {
			rampHits++
		}
		if math.Abs(float64(pt.Y-areaMesh.Polys[poly].HeightAt(pt.X, pt.Z))) > 1e-3 {
			t.Fatalf("sampled 3D point %v not on surface of poly %d", pt, poly)
		}
	}
	slopeArea := 10.0 * math.Hypot(10, 8)
	expected := slopeArea / (100.0 + slopeArea)
	got := float64(rampHits) / float64(samples)
	if math.Abs(got-expected) > 0.03 {
		t.Fatalf("3D surface-area weighting ratio = %.4f, want %.4f ± 0.03", got, expected)
	}

	// 3. Same seed produces bit-identical RandomPoint sequence across separate streams.
	r1 := random.New(777)
	r2 := random.New(777)
	for i := range 50 {
		p1, idx1, ok1 := areaMesh.RandomPoint(r1)
		p2, idx2, ok2 := areaMesh.RandomPoint(r2)
		if !ok1 || !ok2 || idx1 != idx2 ||
			math.Float32bits(p1.X) != math.Float32bits(p2.X) ||
			math.Float32bits(p1.Y) != math.Float32bits(p2.Y) ||
			math.Float32bits(p1.Z) != math.Float32bits(p2.Z) {
			t.Fatalf("3D RandomPoint determinism mismatch at %d", i)
		}
	}
}

func TestNavMesh3DFindPathIntoCorridorIntoAndZeroAllocs(t *testing.T) {
	nm := buildGridNavMesh3D(20, 20, 10)
	for r := range 20 {
		if r == 10 {
			continue
		}
		nm.AddPolygonFlags(r*20+10, NavFlagDisabled)
	}

	start := math3d.Vec3{X: 15, Y: 0, Z: 15}
	goal := math3d.Vec3{X: 185, Y: 0, Z: 185}

	refPath, ok := nm.FindPath(start, goal)
	if !ok || len(refPath) < 2 {
		t.Fatalf("3D reference FindPath failed")
	}
	refCorridor, ok := nm.FindCorridor(start, goal)
	if !ok || len(refCorridor) < 2 {
		t.Fatalf("3D reference FindCorridor failed")
	}

	q := NewNavMeshQuery(nm)
	pathBuf := make([]math3d.Vec3, 0, 64)
	corridorBuf := make([]int, 0, 128)
	visitedBuf := make([]int, 0, 64)

	gotPath, ok := q.FindPathInto(pathBuf, start, goal)
	if !ok || len(gotPath) != len(refPath) {
		t.Fatalf("3D FindPathInto len=%d ok=%v, want len=%d", len(gotPath), ok, len(refPath))
	}
	for i := range refPath {
		if !gotPath[i].ApproxEqual(refPath[i], 1e-4) {
			t.Fatalf("3D FindPathInto[%d] = %v, want %v", i, gotPath[i], refPath[i])
		}
	}

	gotCorridor, ok := q.FindCorridorInto(corridorBuf, start, goal)
	if !ok || len(gotCorridor) != len(refCorridor) {
		t.Fatalf("3D FindCorridorInto len=%d ok=%v, want len=%d", len(gotCorridor), ok, len(refCorridor))
	}
	for i := range refCorridor {
		if gotCorridor[i] != refCorridor[i] {
			t.Fatalf("3D FindCorridorInto[%d] = %d, want %d", i, gotCorridor[i], refCorridor[i])
		}
	}

	// Also verify parity on stacked floors + ramp mesh.
	stacked := buildStackedFloorsMesh3D(true)
	sq := NewNavMeshQuery(stacked)
	sStart := math3d.Vec3{X: 5, Y: 0, Z: 5}
	sGoal := math3d.Vec3{X: 5, Y: 4, Z: 5}
	sRef, ok := stacked.FindPath(sStart, sGoal)
	if !ok {
		t.Fatalf("stacked FindPath failed")
	}
	sGot, ok := sq.FindPathInto(nil, sStart, sGoal)
	if !ok || len(sGot) != len(sRef) {
		t.Fatalf("stacked FindPathInto len=%d ok=%v, want len=%d", len(sGot), ok, len(sRef))
	}
	for i := range sRef {
		if !sGot[i].ApproxEqual(sRef[i], 1e-4) {
			t.Fatalf("stacked FindPathInto[%d] = %v, want %v", i, sGot[i], sRef[i])
		}
	}

	// Also verify FindPathWithFilterInto (area costs) and FindPathWithFilterAndOptionsInto (partial + off-mesh).
	areaNM := BuildNavMeshWithAreas([]NavPolygonInput{
		{Vertices: []math3d.Vec3{{X: 0, Y: 0, Z: 0}, {X: 0, Y: 0, Z: 10}, {X: 10, Y: 0, Z: 10}, {X: 10, Y: 0, Z: 0}}, Area: NavAreaGrass},
		{Vertices: []math3d.Vec3{{X: 10, Y: 0, Z: 0}, {X: 10, Y: 0, Z: 10}, {X: 20, Y: 0, Z: 10}, {X: 20, Y: 0, Z: 0}}, Area: NavAreaMud},
		{Vertices: []math3d.Vec3{{X: 20, Y: 0, Z: 0}, {X: 20, Y: 0, Z: 10}, {X: 30, Y: 0, Z: 10}, {X: 30, Y: 0, Z: 0}}, Area: NavAreaGrass},
		{Vertices: []math3d.Vec3{{X: 0, Y: 0, Z: 10}, {X: 0, Y: 0, Z: 18}, {X: 30, Y: 0, Z: 18}, {X: 30, Y: 0, Z: 10}}, Area: NavAreaRoad},
		{Vertices: []math3d.Vec3{{X: 40, Y: 2, Z: 0}, {X: 40, Y: 2, Z: 10}, {X: 50, Y: 2, Z: 10}, {X: 50, Y: 2, Z: 0}}, Area: NavAreaGrass},
	}, math3d.DegToRad(45))
	filter := DefaultNavFilter()
	filter.SetAreaCost(NavAreaRoad, 0.4)
	filter.SetAreaCost(NavAreaMud, 6.0)
	aq := NewNavMeshQuery(areaNM)
	var areaDst []math3d.Vec3
	areaDst, ok = aq.FindPathWithFilterInto(areaDst, math3d.Vec3{X: 2, Y: 0, Z: 5}, math3d.Vec3{X: 28, Y: 0, Z: 5}, filter)
	if !ok || len(areaDst) < 3 {
		t.Fatalf("3D area-weighted FindPathWithFilterInto failed: %v %v", areaDst, ok)
	}
	usesRoad := false
	for _, wp := range areaDst {
		if wp.Z >= 9.9 {
			usesRoad = true
			break
		}
	}
	if !usesRoad {
		t.Fatalf("3D area-weighted FindPathWithFilterInto should detour via road, got %v", areaDst)
	}

	partRes := aq.FindPathWithFilterAndOptionsInto(areaDst, math3d.Vec3{X: 2, Y: 0, Z: 5}, math3d.Vec3{X: 45, Y: 2, Z: 5}, filter, ai.PathOptions{AllowPartial: true})
	if !partRes.Found || !partRes.Partial || len(partRes.Path) < 2 {
		t.Fatalf("3D partial FindPathWithFilterAndOptionsInto = %+v, want Found=true Partial=true", partRes)
	}
	if !areaNM.AddOffMeshLink(NavOffMeshLink{Start: math3d.Vec3{X: 29, Y: 0, Z: 8}, End: math3d.Vec3{X: 41, Y: 2, Z: 2}, Cost: 5}) {
		t.Fatalf("3D AddOffMeshLink failed")
	}
	linkRef, okRef := areaNM.FindPath(math3d.Vec3{X: 2, Y: 0, Z: 5}, math3d.Vec3{X: 45, Y: 2, Z: 5})
	linkPath, ok := aq.FindPathInto(areaDst, math3d.Vec3{X: 2, Y: 0, Z: 5}, math3d.Vec3{X: 45, Y: 2, Z: 5})
	if !ok || !okRef || len(linkPath) != len(linkRef) || len(linkPath) < 3 {
		t.Fatalf("3D off-mesh FindPathInto = (%v, %v), want parity with FindPath (%v, %v)", linkPath, ok, linkRef, okRef)
	}

	// Warm up and verify 0 allocations.
	pathBuf, _ = nm.FindPathInto(pathBuf, start, goal)
	corridorBuf, _ = nm.FindCorridorInto(corridorBuf, start, goal)
	_, _ = q.MoveAlongSurfaceInto(visitedBuf, start, math3d.Vec3{X: 115, Y: 0, Z: 55})
	rng := random.New(11)
	_, _, _ = q.RandomPointAround(start, 25, rng)

	if allocs := testing.AllocsPerRun(50, func() {
		pathBuf, ok = nm.FindPathInto(pathBuf, start, goal)
		if !ok {
			t.Fatal("3D NavMesh.FindPathInto failed")
		}
	}); allocs != 0 {
		t.Fatalf("3D NavMesh.FindPathInto allocated %.2f times/run, want 0", allocs)
	}

	if allocs := testing.AllocsPerRun(50, func() {
		pathBuf, ok = q.FindPathInto(pathBuf, start, goal)
		if !ok {
			t.Fatal("3D NavMeshQuery.FindPathInto failed")
		}
	}); allocs != 0 {
		t.Fatalf("3D NavMeshQuery.FindPathInto allocated %.2f times/run, want 0", allocs)
	}

	if allocs := testing.AllocsPerRun(50, func() {
		corridorBuf, ok = q.FindCorridorInto(corridorBuf, start, goal)
		if !ok {
			t.Fatal("3D NavMeshQuery.FindCorridorInto failed")
		}
	}); allocs != 0 {
		t.Fatalf("3D NavMeshQuery.FindCorridorInto allocated %.2f times/run, want 0", allocs)
	}

	if allocs := testing.AllocsPerRun(50, func() {
		if _, ok := nm.FindPolygon(start); !ok {
			t.Fatal("3D FindPolygon failed")
		}
		if _, _, ok := nm.NearestPoint(math3d.Vec3{X: -2, Y: 0, Z: 15}, math3d.Vec3{X: 5, Y: 2, Z: 5}); !ok {
			t.Fatal("3D NearestPoint failed")
		}
		if _, hit := nm.Raycast(start, math3d.Vec3{X: 85, Y: 0, Z: 15}); hit {
			t.Fatal("3D Raycast failed")
		}
		res, ok := q.MoveAlongSurfaceInto(visitedBuf, start, math3d.Vec3{X: 115, Y: 0, Z: 55})
		if !ok {
			t.Fatal("3D MoveAlongSurfaceInto failed")
		}
		visitedBuf = res.Visited
		if _, _, ok := q.RandomPointAround(start, 25, rng); !ok {
			t.Fatal("3D RandomPointAround failed")
		}
	}); allocs != 0 {
		t.Fatalf("3D spatial queries allocated %.2f times/run, want 0", allocs)
	}

	// Concurrent read-only queries with separate NavMeshQuery per goroutine.
	var wg sync.WaitGroup
	for g := range 4 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			localQ := NewNavMeshQuery(nm)
			var localDst []math3d.Vec3
			localRng := random.New(uint64(200 + id))
			for range 25 {
				var ok bool
				localDst, ok = localQ.FindPathInto(localDst, start, goal)
				if !ok || len(localDst) != len(refPath) {
					t.Errorf("goroutine %d 3D FindPathInto mismatch", id)
					return
				}
				if _, _, ok := localQ.NearestPoint(start, math3d.Vec3{X: 5, Y: 2, Z: 5}); !ok {
					t.Errorf("goroutine %d 3D NearestPoint failed", id)
					return
				}
				if _, _, ok := localQ.RandomPointAround(start, 20, localRng); !ok {
					t.Errorf("goroutine %d 3D RandomPointAround failed", id)
					return
				}
			}
		}(g)
	}
	wg.Wait()
}

func BenchmarkNavMesh3D10kFindPolygon(b *testing.B) {
	nm := buildGridNavMesh3D(100, 100, 10) // 10,000 polygons
	pts := []math3d.Vec3{
		{X: 15, Y: 0, Z: 15},
		{X: 505, Y: 0, Z: 505},
		{X: 985, Y: 0, Z: 985},
		{X: 255, Y: 0, Z: 745},
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, ok := nm.FindPolygon(pts[i&3]); !ok {
			b.Fatal("expected polygon hit")
		}
	}
}

func BenchmarkNavMesh3D10kFindPolygonLinear(b *testing.B) {
	nm := buildGridNavMesh3D(100, 100, 10)
	nm.bvhNodes = nil // force linear scan baseline for comparison
	pts := []math3d.Vec3{
		{X: 15, Y: 0, Z: 15},
		{X: 505, Y: 0, Z: 505},
		{X: 985, Y: 0, Z: 985},
		{X: 255, Y: 0, Z: 745},
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, ok := nm.FindPolygon(pts[i&3]); !ok {
			b.Fatal("expected polygon hit")
		}
	}
}

func BenchmarkNavMesh3D10kNearestPoint(b *testing.B) {
	nm := buildGridNavMesh3D(100, 100, 10)
	pts := []math3d.Vec3{
		{X: -3, Y: 0.5, Z: 505},
		{X: 1003, Y: 0.5, Z: 505},
		{X: 505, Y: 0.5, Z: -3},
		{X: 505, Y: 0.5, Z: 1003},
	}
	ext := math3d.Vec3{X: 10, Y: 2, Z: 10}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, _, ok := nm.NearestPoint(pts[i&3], ext); !ok {
			b.Fatal("expected nearest point")
		}
	}
}

func BenchmarkNavMesh3DFindPathInto(b *testing.B) {
	nm := buildGridNavMesh3D(40, 40, 10)
	for r := range 40 {
		if r == 20 {
			continue
		}
		nm.AddPolygonFlags(r*40+20, NavFlagDisabled)
	}
	start := math3d.Vec3{X: 15, Y: 0, Z: 15}
	goal := math3d.Vec3{X: 385, Y: 0, Z: 385}
	dst := make([]math3d.Vec3, 0, 64)
	dst, _ = nm.FindPathInto(dst, start, goal)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var ok bool
		dst, ok = nm.FindPathInto(dst, start, goal)
		if !ok {
			b.Fatal("expected 3D path")
		}
	}
}
