package ai

import (
	"math"
	"sync"
	"testing"

	"github.com/nsgo-dev/game/packages/game/random"
	"github.com/nsgo-dev/nsgo/core"
)

func buildGridNavMesh2D(cols, rows int, cellSize float32) *NavMesh {
	polys := make([][]core.Point, 0, cols*rows)
	for r := range rows {
		for c := range cols {
			x0 := core.Points(float32(c) * cellSize)
			y0 := core.Points(float32(r) * cellSize)
			x1 := core.Points(float32(c+1) * cellSize)
			y1 := core.Points(float32(r+1) * cellSize)
			polys = append(polys, []core.Point{
				{X: x0, Y: y0},
				{X: x1, Y: y0},
				{X: x1, Y: y1},
				{X: x0, Y: y1},
			})
		}
	}
	return BuildNavMesh(polys)
}

func TestNavMesh2DNearestPointExtentAndFilter(t *testing.T) {
	mesh := BuildNavMeshWithAreas([]NavPolygonInput{
		{
			Vertices: []core.Point{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}},
			Area:     NavAreaGrass,
			Flags:    NavFlagWalk,
		},
		{
			Vertices: []core.Point{{X: 14, Y: 0}, {X: 24, Y: 0}, {X: 24, Y: 10}, {X: 14, Y: 10}},
			Area:     NavAreaWater,
			Flags:    NavFlagSwim,
		},
		{
			// Diagonal strip whose unconstrained closest point to (0, 20) is near (4, 16)
			// while still intersecting a narrow X extent box [-2, 2] x [10, 30].
			Vertices: []core.Point{{X: 1, Y: 25}, {X: 5, Y: 15}, {X: 6, Y: 16}, {X: 2, Y: 26}},
			Area:     NavAreaRoad,
			Flags:    NavFlagWalk,
		},
	})

	// Inside polygon returns the point itself.
	pt, poly, ok := mesh.NearestPoint(core.Point{X: 3, Y: 4}, core.Point{X: 1, Y: 1})
	if !ok || poly != 0 || math.Abs(float64(pt.X-3)) > 1e-4 || math.Abs(float64(pt.Y-4)) > 1e-4 {
		t.Fatalf("inside NearestPoint = (%v, %d, %v), want ((3,4), 0, true)", pt, poly, ok)
	}

	// Outside within extent snaps to closest boundary edge.
	pt, poly, ok = mesh.NearestPoint(core.Point{X: 11.5, Y: 5}, core.Point{X: 2, Y: 2})
	if !ok || poly != 0 || math.Abs(float64(pt.X-10)) > 1e-4 || math.Abs(float64(pt.Y-5)) > 1e-4 {
		t.Fatalf("nearby NearestPoint = (%v, %d, %v), want ((10,5), 0, true)", pt, poly, ok)
	}

	// Outside beyond extent fails cleanly.
	if _, poly, ok := mesh.NearestPoint(core.Point{X: 12, Y: 5}, core.Point{X: 1, Y: 1}); ok || poly != -1 {
		t.Fatalf("out-of-extent NearestPoint should fail, got poly=%d ok=%v", poly, ok)
	}

	// Filter excludes water polygon and falls back to grass polygon within extent.
	landFilter := DefaultNavFilter()
	landFilter.ExcludeFlags |= NavFlagSwim
	pt, poly, ok = mesh.NearestPointWithFilter(core.Point{X: 15, Y: 5}, core.Point{X: 6, Y: 2}, landFilter)
	if !ok || poly != 0 || math.Abs(float64(pt.X-10)) > 1e-4 {
		t.Fatalf("filtered NearestPoint = (%v, %d, %v), want grass poly 0 at x=10", pt, poly, ok)
	}

	// Disabled polygon is excluded by DefaultNavFilter.
	mesh.AddPolygonFlags(0, NavFlagDisabled)
	if _, _, ok := mesh.NearestPoint(core.Point{X: 5, Y: 5}, core.Point{X: 2, Y: 2}); ok {
		t.Fatalf("disabled polygon should be excluded by DefaultNavFilter")
	}
	mesh.ClearPolygonFlags(0, NavFlagDisabled)

	// Narrow extent clipping: polygon 2 overlaps [-2, 2] x [14, 26] even though its unconstrained
	// closest point to (0, 20) lies at x > 2; NearestPoint must clip and return a point inside extent.
	queryCenter := core.Point{X: 0, Y: 20}
	queryExtent := core.Point{X: 2, Y: 6}
	pt, poly, ok = mesh.NearestPoint(queryCenter, queryExtent)
	if !ok || poly != 2 {
		t.Fatalf("clipped extent NearestPoint = (%v, %d, %v), want poly 2", pt, poly, ok)
	}
	if math.Abs(float64(pt.X-queryCenter.X)) > float64(queryExtent.X)+1e-3 ||
		math.Abs(float64(pt.Y-queryCenter.Y)) > float64(queryExtent.Y)+1e-3 {
		t.Fatalf("clipped NearestPoint %v escaped search extent %v around %v", pt, queryExtent, queryCenter)
	}
	if !mesh.Contains(pt) {
		t.Fatalf("clipped NearestPoint %v is not on mesh", pt)
	}

	// Invalid inputs.
	var nilMesh *NavMesh
	if _, p, ok := nilMesh.NearestPoint(core.Point{}, core.Point{X: 1, Y: 1}); ok || p != -1 {
		t.Fatalf("nil mesh should fail")
	}
	if _, p, ok := mesh.NearestPoint(core.Point{X: 5, Y: 5}, core.Point{X: -1, Y: 1}); ok || p != -1 {
		t.Fatalf("negative extent should fail")
	}
	if _, p, ok := mesh.NearestPoint(core.Point{X: core.Points(math.NaN()), Y: 5}, core.Point{X: 1, Y: 1}); ok || p != -1 {
		t.Fatalf("NaN point should fail")
	}
}

func TestNavMesh2DRaycastObstaclesCornersAndFilter(t *testing.T) {
	// 3x3 grid of 10x10 cells with center cell (1,1) removed (forming an obstacle hole),
	// and cell (2,1) tagged as a toggleable door.
	var inputs []NavPolygonInput
	doorPoly := -1
	for r := range 3 {
		for c := range 3 {
			if c == 1 && r == 1 {
				continue // central hole
			}
			x0, y0 := core.Points(c*10), core.Points(r*10)
			x1, y1 := core.Points((c+1)*10), core.Points((r+1)*10)
			area := NavAreaDefault
			flags := NavFlagWalk
			if c == 2 && r == 1 {
				area = NavAreaDoor
				flags = NavFlagWalk | NavFlagDoor
				doorPoly = len(inputs)
			}
			inputs = append(inputs, NavPolygonInput{
				Vertices: []core.Point{{X: x0, Y: y0}, {X: x1, Y: y0}, {X: x1, Y: y1}, {X: x0, Y: y1}},
				Area:     area,
				Flags:    flags,
			})
		}
	}
	mesh := BuildNavMeshWithAreas(inputs)

	// Unobstructed ray across bottom row (3 polygons).
	hit, obstructed := mesh.Raycast(core.Point{X: 2, Y: 5}, core.Point{X: 28, Y: 5})
	if obstructed || hit.Hit || math.Abs(float64(hit.T-1)) > 1e-4 || hit.Poly < 0 {
		t.Fatalf("clear raycast = (%+v, %v), want unobstructed T=1", hit, obstructed)
	}

	// Ray hits the central hole edge at x=10 with inward normal (-1, 0).
	hit, obstructed = mesh.Raycast(core.Point{X: 5, Y: 15}, core.Point{X: 25, Y: 15})
	if !obstructed || !hit.Hit {
		t.Fatalf("ray across central hole should hit, got %+v", hit)
	}
	if math.Abs(float64(hit.Point.X-10)) > 1e-3 || math.Abs(float64(hit.Point.Y-15)) > 1e-3 {
		t.Fatalf("hole hit point = %v, want (10, 15)", hit.Point)
	}
	if math.Abs(float64(hit.T-0.25)) > 1e-3 {
		t.Fatalf("hole hit T = %v, want 0.25", hit.T)
	}
	if math.Abs(float64(hit.Normal.X-(-1))) > 1e-3 || math.Abs(float64(hit.Normal.Y)) > 1e-3 {
		t.Fatalf("hole hit normal = %v, want (-1, 0)", hit.Normal)
	}

	// Closing the door blocks a vertical ray along the right column; reopening restores line of sight.
	mesh.AddPolygonFlags(doorPoly, NavFlagDisabled)
	hit, obstructed = mesh.Raycast(core.Point{X: 25, Y: 5}, core.Point{X: 25, Y: 25})
	if !obstructed || !hit.Hit || math.Abs(float64(hit.Point.Y-10)) > 1e-3 {
		t.Fatalf("closed door raycast = (%+v, %v), want hit at y=10", hit, obstructed)
	}
	mesh.ClearPolygonFlags(doorPoly, NavFlagDisabled)
	hit, obstructed = mesh.Raycast(core.Point{X: 25, Y: 5}, core.Point{X: 25, Y: 25})
	if obstructed || hit.Hit {
		t.Fatalf("reopened door raycast should be clear, got %+v", hit)
	}

	// Ray grazing an interior shared corner of a 2x2 grid must pass cleanly without false hits.
	grid2x2 := buildGridNavMesh2D(2, 2, 10)
	hit, obstructed = grid2x2.Raycast(core.Point{X: 2, Y: 2}, core.Point{X: 18, Y: 18})
	if obstructed || hit.Hit || math.Abs(float64(hit.T-1)) > 1e-4 {
		t.Fatalf("diagonal corner-grazing raycast failed: (%+v, %v)", hit, obstructed)
	}

	// Ray hitting a shared corner where a portal edge meets a boundary wall must return the wall normal
	// regardless of polygon vertex cyclic order.
	vertStrip := BuildNavMesh([][]core.Point{
		// Order vertices so the portal edge (top edge y=10) comes BEFORE the right wall edge (x=10).
		{{X: 10, Y: 10}, {X: 0, Y: 10}, {X: 0, Y: 0}, {X: 10, Y: 0}},
		{{X: 0, Y: 10}, {X: 10, Y: 10}, {X: 10, Y: 20}, {X: 0, Y: 20}},
	})
	hit, obstructed = vertStrip.Raycast(core.Point{X: 5, Y: 5}, core.Point{X: 15, Y: 15})
	if !obstructed || !hit.Hit {
		t.Fatalf("corner wall raycast should hit")
	}
	if math.Abs(float64(hit.Normal.X-(-1))) > 1e-3 || math.Abs(float64(hit.Normal.Y)) > 1e-3 {
		t.Fatalf("corner wall hit normal = %v, want wall normal (-1, 0), not portal normal", hit.Normal)
	}

	// Start outside mesh reports Hit=true, Poly=-1, T=0.
	hit, obstructed = mesh.Raycast(core.Point{X: 15, Y: 15}, core.Point{X: 25, Y: 25})
	if !obstructed || !hit.Hit || hit.Poly != -1 || hit.T != 0 {
		t.Fatalf("off-mesh start raycast = (%+v, %v), want Poly=-1 T=0 Hit=true", hit, obstructed)
	}
}

func TestNavMesh2DMoveAlongSurfaceSlidingAndDiagonalWall(t *testing.T) {
	// Vertical strip of two quads (top edge portal comes first in poly 0 to exercise corner normal selection).
	strip := BuildNavMesh([][]core.Point{
		{{X: 10, Y: 10}, {X: 0, Y: 10}, {X: 0, Y: 0}, {X: 10, Y: 0}},
		{{X: 0, Y: 10}, {X: 10, Y: 10}, {X: 10, Y: 20}, {X: 0, Y: 20}},
	})

	// Unobstructed move across both polygons.
	res, ok := strip.MoveAlongSurface(core.Point{X: 5, Y: 2}, core.Point{X: 5, Y: 18})
	if !ok || res.HitBoundary || res.Poly != 1 || len(res.Visited) != 2 {
		t.Fatalf("clear MoveAlongSurface = (%+v, %v), want Poly=1 HitBoundary=false Visited=[0 1]", res, ok)
	}

	// Slide into right wall and glide across portal into poly 1.
	res, ok = strip.MoveAlongSurface(core.Point{X: 5, Y: 5}, core.Point{X: 15, Y: 15})
	if !ok || !res.HitBoundary || res.Poly != 1 {
		t.Fatalf("wall slide MoveAlongSurface = (%+v, %v), want Poly=1 HitBoundary=true", res, ok)
	}
	if math.Abs(float64(res.Point.X-10)) > 1e-3 || math.Abs(float64(res.Point.Y-15)) > 1e-2 {
		t.Fatalf("wall slide Point = %v, want ~(10, 15)", res.Point)
	}
	if !strip.Contains(res.Point) {
		t.Fatalf("wall slide Point %v left the mesh", res.Point)
	}

	// 45-degree diagonal wall: sliding must glide cleanly along the sloped boundary without stalling.
	diagMesh := BuildNavMesh([][]core.Point{
		{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 0, Y: 10}},
	})
	res, ok = diagMesh.MoveAlongSurface(core.Point{X: 2, Y: 2}, core.Point{X: 10, Y: 6})
	if !ok || !res.HitBoundary || !diagMesh.Contains(res.Point) {
		t.Fatalf("diagonal wall slide = (%+v, %v), want HitBoundary=true on mesh", res, ok)
	}
	// Expected projection of (10,6) onto hypotenuse x+y=10 is (7, 3).
	if math.Abs(float64(res.Point.X-7)) > 0.05 || math.Abs(float64(res.Point.Y-3)) > 0.05 {
		t.Fatalf("diagonal wall slide Point = %v, want ~(7, 3)", res.Point)
	}

	// Filter blocks disabled polygon so agent slides along the door edge instead of entering.
	doorMesh := BuildNavMeshWithAreas([]NavPolygonInput{
		{Vertices: []core.Point{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}}, Flags: NavFlagWalk},
		{Vertices: []core.Point{{X: 10, Y: 0}, {X: 20, Y: 0}, {X: 20, Y: 10}, {X: 10, Y: 10}}, Area: NavAreaDoor, Flags: NavFlagWalk | NavFlagDisabled},
	})
	res, ok = doorMesh.MoveAlongSurface(core.Point{X: 5, Y: 2}, core.Point{X: 15, Y: 8})
	if !ok || !res.HitBoundary || res.Poly != 0 || math.Abs(float64(res.Point.X-10)) > 1e-3 || math.Abs(float64(res.Point.Y-8)) > 1e-2 {
		t.Fatalf("door-blocked MoveAlongSurface = (%+v, %v), want ~(10, 8) on poly 0", res, ok)
	}
}

func TestNavMesh2DRandomPointDeterminismAreaWeightAndReachability(t *testing.T) {
	// Poly 0: 10x10 (area 100), Poly 1: 30x30 (area 900), connected along x=10.
	// Poly 2: 10x10 disconnected island at x=[12, 22], y=[100, 110] and another nearby island at x=[-12, -2], y=[0, 10].
	// Poly 3: 10x10 water polygon connected to Poly 0 at y=[-10, 0].
	mesh := BuildNavMeshWithAreas([]NavPolygonInput{
		{Vertices: []core.Point{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}}, Area: NavAreaGrass, Flags: NavFlagWalk},
		{Vertices: []core.Point{{X: 10, Y: 0}, {X: 40, Y: 0}, {X: 40, Y: 30}, {X: 10, Y: 30}}, Area: NavAreaRoad, Flags: NavFlagWalk},
		{Vertices: []core.Point{{X: -12, Y: 0}, {X: -2, Y: 0}, {X: -2, Y: 10}, {X: -12, Y: 10}}, Area: NavAreaGrass, Flags: NavFlagWalk},
		{Vertices: []core.Point{{X: 0, Y: -10}, {X: 10, Y: -10}, {X: 10, Y: 0}, {X: 0, Y: 0}}, Area: NavAreaWater, Flags: NavFlagSwim},
	})

	// 1. Cross-run determinism (exact bit identity for same seed).
	rngA := random.New(123456789)
	rngB := random.New(123456789)
	for i := range 50 {
		pA, polyA, okA := mesh.RandomPoint(rngA)
		pB, polyB, okB := mesh.RandomPoint(rngB)
		if !okA || !okB || polyA != polyB ||
			math.Float32bits(float32(pA.X)) != math.Float32bits(float32(pB.X)) ||
			math.Float32bits(float32(pA.Y)) != math.Float32bits(float32(pB.Y)) {
			t.Fatalf("RandomPoint mismatch at %d: (%v,%d,%v) vs (%v,%d,%v)", i, pA, polyA, okA, pB, polyB, okB)
		}
	}

	// 2. Area-weighted uniformity on land-only polygons (areas 100, 900, 100 -> poly 1 should get ~9/11 ≈ 81.8%).
	landFilter := DefaultNavFilter()
	landFilter.ExcludeFlags |= NavFlagSwim
	rng := random.New(42)
	const samples = 4000
	counts := [4]int{}
	for range samples {
		pt, poly, ok := mesh.RandomPointWithFilter(rng, landFilter)
		if !ok || poly < 0 || poly >= 4 {
			t.Fatalf("RandomPointWithFilter failed: (%v, %d, %v)", pt, poly, ok)
		}
		if poly == 3 {
			t.Fatalf("excluded water poly 3 was returned")
		}
		if !polyContains(&mesh.polys[poly], toVec2(pt)) {
			t.Fatalf("sampled point %v not inside returned poly %d", pt, poly)
		}
		counts[poly]++
	}
	ratioPoly1 := float64(counts[1]) / float64(samples)
	expectedRatio := 900.0 / 1100.0
	if math.Abs(ratioPoly1-expectedRatio) > 0.03 {
		t.Fatalf("area-weighted ratio for poly 1 = %.4f, want %.4f ± 0.03 (counts=%v)", ratioPoly1, expectedRatio, counts)
	}

	// 3. RandomPointAround: reachable only, within radius, respects filter, never visits disconnected island poly 2.
	center := core.Point{X: 2, Y: 5}
	const radius float32 = 10.0 // Euclidean circle easily overlaps disconnected island at x=-2 (dist=4) and water at y=0 (dist=5)
	rngAroundA := random.New(98765)
	rngAroundB := random.New(98765)
	q := NewNavMeshQuery(mesh)
	hitPoly1 := false
	for i := range 500 {
		pA, polyA, okA := mesh.RandomPointAroundWithFilter(center, radius, rngAroundA, landFilter)
		pB, polyB, okB := q.RandomPointAroundWithFilter(center, radius, rngAroundB, landFilter)
		if !okA || !okB {
			t.Fatalf("RandomPointAround failed at %d", i)
		}
		if polyA != polyB ||
			math.Float32bits(float32(pA.X)) != math.Float32bits(float32(pB.X)) ||
			math.Float32bits(float32(pA.Y)) != math.Float32bits(float32(pB.Y)) {
			t.Fatalf("NavMesh vs NavMeshQuery RandomPointAround mismatch at %d", i)
		}
		if polyA == 2 {
			t.Fatalf("disconnected island poly 2 should never be returned by RandomPointAround")
		}
		if polyA == 3 {
			t.Fatalf("filtered-out water poly 3 should never be returned by RandomPointAround")
		}
		if polyA == 1 {
			hitPoly1 = true
		}
		dx := float64(pA.X - center.X)
		dy := float64(pA.Y - center.Y)
		if math.Hypot(dx, dy) > float64(radius)+1e-4 {
			t.Fatalf("RandomPointAround point %v exceeded radius %v from %v", pA, radius, center)
		}
		if !polyContains(&mesh.polys[polyA], toVec2(pA)) {
			t.Fatalf("RandomPointAround point %v outside returned poly %d", pA, polyA)
		}
	}
	if !hitPoly1 {
		t.Fatalf("expected reachable connected poly 1 within radius to be sampled")
	}

	// Nil rng / invalid radius / off-mesh center fail cleanly.
	if _, _, ok := mesh.RandomPoint(nil); ok {
		t.Fatalf("nil rng should fail")
	}
	if _, _, ok := mesh.RandomPointAround(center, -1, rng); ok {
		t.Fatalf("negative radius should fail")
	}
	if _, _, ok := mesh.RandomPointAround(core.Point{X: 100, Y: 100}, 5, rng); ok {
		t.Fatalf("off-mesh center should fail")
	}
}

func TestNavMesh2DFindPathIntoCorridorIntoAndZeroAllocs(t *testing.T) {
	mesh := buildGridNavMesh2D(20, 20, 10)
	// Block a vertical wall in the middle with a doorway at row 10.
	for r := range 20 {
		if r == 10 {
			continue
		}
		mesh.AddPolygonFlags(r*20+10, NavFlagDisabled)
	}

	start := core.Point{X: 15, Y: 15}
	goal := core.Point{X: 185, Y: 185}

	refPath, ok := mesh.FindPath(start, goal)
	if !ok || len(refPath) < 2 {
		t.Fatalf("reference FindPath failed")
	}
	refCorridor, ok := mesh.FindCorridor(start, goal)
	if !ok || len(refCorridor) < 2 {
		t.Fatalf("reference FindCorridor failed")
	}

	q := NewNavMeshQuery(mesh)
	pathBuf := make([]core.Point, 0, 64)
	corridorBuf := make([]int, 0, 128)
	visitedBuf := make([]int, 0, 64)

	// Parity check.
	gotPath, ok := q.FindPathInto(pathBuf, start, goal)
	if !ok || len(gotPath) != len(refPath) {
		t.Fatalf("FindPathInto len=%d ok=%v, want len=%d", len(gotPath), ok, len(refPath))
	}
	for i := range refPath {
		if math.Abs(float64(gotPath[i].X-refPath[i].X)) > 1e-4 || math.Abs(float64(gotPath[i].Y-refPath[i].Y)) > 1e-4 {
			t.Fatalf("FindPathInto[%d] = %v, want %v", i, gotPath[i], refPath[i])
		}
	}

	gotCorridor, ok := q.FindCorridorInto(corridorBuf, start, goal)
	if !ok || len(gotCorridor) != len(refCorridor) {
		t.Fatalf("FindCorridorInto len=%d ok=%v, want len=%d", len(gotCorridor), ok, len(refCorridor))
	}
	for i := range refCorridor {
		if gotCorridor[i] != refCorridor[i] {
			t.Fatalf("FindCorridorInto[%d] = %d, want %d", i, gotCorridor[i], refCorridor[i])
		}
	}

	// Also verify FindPathWithFilterInto (area costs) and FindPathWithFilterAndOptionsInto (partial + off-mesh).
	areaMesh := BuildNavMeshWithAreas([]NavPolygonInput{
		{Vertices: []core.Point{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}}, Area: NavAreaGrass},
		{Vertices: []core.Point{{X: 10, Y: 0}, {X: 20, Y: 0}, {X: 20, Y: 10}, {X: 10, Y: 10}}, Area: NavAreaMud},
		{Vertices: []core.Point{{X: 20, Y: 0}, {X: 30, Y: 0}, {X: 30, Y: 10}, {X: 20, Y: 10}}, Area: NavAreaGrass},
		{Vertices: []core.Point{{X: 0, Y: 10}, {X: 30, Y: 10}, {X: 30, Y: 18}, {X: 0, Y: 18}}, Area: NavAreaRoad},
		{Vertices: []core.Point{{X: 40, Y: 0}, {X: 50, Y: 0}, {X: 50, Y: 10}, {X: 40, Y: 10}}, Area: NavAreaGrass},
	})
	filter := DefaultNavFilter()
	filter.SetAreaCost(NavAreaRoad, 0.4)
	filter.SetAreaCost(NavAreaMud, 6.0)
	aq := NewNavMeshQuery(areaMesh)
	var areaDst []core.Point
	areaDst, ok = aq.FindPathWithFilterInto(areaDst, core.Point{X: 2, Y: 5}, core.Point{X: 28, Y: 5}, filter)
	if !ok || len(areaDst) < 3 {
		t.Fatalf("area-weighted FindPathWithFilterInto failed: %v %v", areaDst, ok)
	}
	// Route must detour onto the road (y >= 10) rather than crossing mud.
	usesRoad := false
	for _, wp := range areaDst {
		if wp.Y >= 9.9 {
			usesRoad = true
			break
		}
	}
	if !usesRoad {
		t.Fatalf("area-weighted FindPathWithFilterInto should detour via road, got %v", areaDst)
	}

	// Partial path toward disconnected island poly 4.
	partRes := aq.FindPathWithFilterAndOptionsInto(areaDst, core.Point{X: 2, Y: 5}, core.Point{X: 45, Y: 5}, filter, PathOptions{AllowPartial: true})
	if !partRes.Found || !partRes.Partial || len(partRes.Path) < 2 {
		t.Fatalf("partial FindPathWithFilterAndOptionsInto = %+v, want Found=true Partial=true", partRes)
	}
	if math.Abs(float64(partRes.Path[len(partRes.Path)-1].X-30)) > 1e-3 {
		t.Fatalf("partial path endpoint = %v, want x=30", partRes.Path[len(partRes.Path)-1])
	}

	// Adding an off-mesh link connects the island cleanly and matches FindPath.
	if !areaMesh.AddOffMeshLink(NavOffMeshLink{Start: core.Point{X: 29, Y: 8}, End: core.Point{X: 41, Y: 2}, Cost: 5}) {
		t.Fatalf("AddOffMeshLink failed")
	}
	linkRef, okRef := areaMesh.FindPath(core.Point{X: 2, Y: 5}, core.Point{X: 45, Y: 5})
	linkPath, ok := aq.FindPathInto(areaDst, core.Point{X: 2, Y: 5}, core.Point{X: 45, Y: 5})
	if !ok || !okRef || len(linkPath) != len(linkRef) || len(linkPath) < 3 {
		t.Fatalf("off-mesh FindPathInto = (%v, %v), want parity with FindPath (%v, %v)", linkPath, ok, linkRef, okRef)
	}

	// Warm up mesh defaultQuery and query q, then verify 0 heap allocations.
	pathBuf, _ = mesh.FindPathInto(pathBuf, start, goal)
	corridorBuf, _ = mesh.FindCorridorInto(corridorBuf, start, goal)
	_, _ = q.MoveAlongSurfaceInto(visitedBuf, core.Point{X: 15, Y: 15}, core.Point{X: 95, Y: 55})
	rng := random.New(7)
	_, _, _ = q.RandomPointAround(start, 25, rng)

	if allocs := testing.AllocsPerRun(50, func() {
		pathBuf, ok = mesh.FindPathInto(pathBuf, start, goal)
		if !ok {
			t.Fatal("FindPathInto failed")
		}
	}); allocs != 0 {
		t.Fatalf("NavMesh.FindPathInto allocated %.2f times/run, want 0", allocs)
	}

	if allocs := testing.AllocsPerRun(50, func() {
		pathBuf, ok = q.FindPathInto(pathBuf, start, goal)
		if !ok {
			t.Fatal("NavMeshQuery.FindPathInto failed")
		}
	}); allocs != 0 {
		t.Fatalf("NavMeshQuery.FindPathInto allocated %.2f times/run, want 0", allocs)
	}

	if allocs := testing.AllocsPerRun(50, func() {
		corridorBuf, ok = q.FindCorridorInto(corridorBuf, start, goal)
		if !ok {
			t.Fatal("NavMeshQuery.FindCorridorInto failed")
		}
	}); allocs != 0 {
		t.Fatalf("NavMeshQuery.FindCorridorInto allocated %.2f times/run, want 0", allocs)
	}

	if allocs := testing.AllocsPerRun(50, func() {
		if idx := mesh.FindPolygon(start); idx < 0 {
			t.Fatal("FindPolygon failed")
		}
		if _, _, ok := mesh.NearestPoint(core.Point{X: -2, Y: 15}, core.Point{X: 5, Y: 5}); !ok {
			t.Fatal("NearestPoint failed")
		}
		if _, hit := mesh.Raycast(start, core.Point{X: 85, Y: 15}); hit {
			t.Fatal("Raycast failed")
		}
		res, ok := q.MoveAlongSurfaceInto(visitedBuf, start, core.Point{X: 115, Y: 55})
		if !ok {
			t.Fatal("MoveAlongSurfaceInto failed")
		}
		visitedBuf = res.Visited
		if _, _, ok := q.RandomPointAround(start, 25, rng); !ok {
			t.Fatal("RandomPointAround failed")
		}
	}); allocs != 0 {
		t.Fatalf("2D spatial queries allocated %.2f times/run, want 0", allocs)
	}

	// Concurrent read-only queries with separate NavMeshQuery per goroutine.
	var wg sync.WaitGroup
	for g := range 4 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			localQ := NewNavMeshQuery(mesh)
			var localDst []core.Point
			localRng := random.New(uint64(100 + id))
			for range 25 {
				var ok bool
				localDst, ok = localQ.FindPathInto(localDst, start, goal)
				if !ok || len(localDst) != len(refPath) {
					t.Errorf("goroutine %d FindPathInto mismatch", id)
					return
				}
				if _, _, ok := localQ.NearestPoint(start, core.Point{X: 5, Y: 5}); !ok {
					t.Errorf("goroutine %d NearestPoint failed", id)
					return
				}
				if _, _, ok := localQ.RandomPointAround(start, 20, localRng); !ok {
					t.Errorf("goroutine %d RandomPointAround failed", id)
					return
				}
			}
		}(g)
	}
	wg.Wait()
}

func BenchmarkNavMesh2D10kFindPolygon(b *testing.B) {
	mesh := buildGridNavMesh2D(100, 100, 10) // 10,000 polygons
	pts := []core.Point{
		{X: 15, Y: 15},
		{X: 505, Y: 505},
		{X: 985, Y: 985},
		{X: 255, Y: 745},
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if idx := mesh.FindPolygon(pts[i&3]); idx < 0 {
			b.Fatal("expected polygon hit")
		}
	}
}

func BenchmarkNavMesh2D10kFindPolygonLinear(b *testing.B) {
	mesh := buildGridNavMesh2D(100, 100, 10)
	mesh.bvhNodes = nil // force linear scan baseline for comparison
	pts := []core.Point{
		{X: 15, Y: 15},
		{X: 505, Y: 505},
		{X: 985, Y: 985},
		{X: 255, Y: 745},
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if idx := mesh.FindPolygon(pts[i&3]); idx < 0 {
			b.Fatal("expected polygon hit")
		}
	}
}

func BenchmarkNavMesh2D10kNearestPoint(b *testing.B) {
	mesh := buildGridNavMesh2D(100, 100, 10)
	pts := []core.Point{
		{X: -3, Y: 505},
		{X: 1003, Y: 505},
		{X: 505, Y: -3},
		{X: 505, Y: 1003},
	}
	ext := core.Point{X: 10, Y: 10}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, _, ok := mesh.NearestPoint(pts[i&3], ext); !ok {
			b.Fatal("expected nearest point")
		}
	}
}

func BenchmarkNavMesh2DFindPathInto(b *testing.B) {
	mesh := buildGridNavMesh2D(40, 40, 10)
	for r := range 40 {
		if r == 20 {
			continue
		}
		mesh.AddPolygonFlags(r*40+20, NavFlagDisabled)
	}
	start := core.Point{X: 15, Y: 15}
	goal := core.Point{X: 385, Y: 385}
	dst := make([]core.Point, 0, 64)
	dst, _ = mesh.FindPathInto(dst, start, goal)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var ok bool
		dst, ok = mesh.FindPathInto(dst, start, goal)
		if !ok {
			b.Fatal("expected path")
		}
	}
}
