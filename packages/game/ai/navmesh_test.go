package ai_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/nsgo/core"
)

func pt(x, y float32) core.Point { return core.Point{X: core.Points(x), Y: core.Points(y)} }

func rectPoly(x0, y0, x1, y1 float32) []core.Point {
	return []core.Point{pt(x0, y0), pt(x1, y0), pt(x1, y1), pt(x0, y1)}
}

func assertPathPoints(t *testing.T, got, want []core.Point, eps float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("path len = %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		dx := float64(got[i].X - want[i].X)
		dy := float64(got[i].Y - want[i].Y)
		if math.Hypot(dx, dy) > eps {
			t.Fatalf("path[%d] = %v, want %v (full got=%v)", i, got[i], want[i], got)
		}
	}
}

func TestNavMeshStraightLineAcrossPolygons(t *testing.T) {
	// Four chained rectangles from x=0 to x=40, y in [0,10], with alternating CW and CCW winding.
	mesh := ai.BuildNavMesh([][]core.Point{
		rectPoly(0, 0, 10, 10),
		{pt(10, 0), pt(10, 10), pt(20, 10), pt(20, 0)}, // CW winding
		rectPoly(20, 0, 30, 10),
		{pt(30, 0), pt(30, 10), pt(40, 10), pt(40, 0)}, // CW winding
	})
	if mesh.PolygonCount() != 4 {
		t.Fatalf("PolygonCount = %d, want 4", mesh.PolygonCount())
	}

	start, goal := pt(2, 3), pt(38, 8)
	path, ok := mesh.FindPath(start, goal)
	if !ok {
		t.Fatal("expected direct path")
	}
	// Funnel smoothing collapses all intermediate portals to the exact straight line.
	assertPathPoints(t, path, []core.Point{start, goal}, 1e-4)
}

func TestNavMeshLCorridor(t *testing.T) {
	// L-shaped corridor made of two rectangles sharing edge (2,0)-(2,2):
	// Vertical leg: [0,2] x [0,10]
	// Horizontal leg: [2,10] x [0,2]
	// Note: this also exercises a partial shared-edge T-junction (edge (2,0)-(2,10) vs (2,0)-(2,2)).
	mesh := ai.BuildNavMesh([][]core.Point{
		rectPoly(0, 0, 2, 10),
		rectPoly(2, 0, 10, 2),
	})

	// Around the corner: must pull taut onto exact inner corner (2, 2).
	path, ok := mesh.FindPath(pt(1, 9), pt(9, 1))
	if !ok {
		t.Fatal("expected L-corridor path")
	}
	assertPathPoints(t, path, []core.Point{pt(1, 9), pt(2, 2), pt(9, 1)}, 1e-4)

	// Clear line of sight across the shared edge: exact straight line without bending.
	direct, ok := mesh.FindPath(pt(1, 1.5), pt(9, 0.5))
	if !ok {
		t.Fatal("expected direct path across L-corridor corner")
	}
	assertPathPoints(t, direct, []core.Point{pt(1, 1.5), pt(9, 0.5)}, 1e-4)
}

func TestNavMeshUTurn(t *testing.T) {
	// U-shaped corridor around a wall occupying [2,8] x [0,8]:
	// Left leg:   [0,2] x [0,10]
	// Top bridge: [2,8] x [8,10]
	// Right leg:  [8,10] x [0,10]
	mesh := ai.BuildNavMesh([][]core.Point{
		rectPoly(0, 0, 2, 10),
		rectPoly(2, 8, 8, 10),
		rectPoly(8, 0, 10, 10),
	})

	start, goal := pt(1, 1), pt(9, 1)
	path, ok := mesh.FindPath(start, goal)
	if !ok {
		t.Fatal("expected U-turn path")
	}
	// Exact geometric shortest path hugs both inner corners of the wall: (2,8) and (8,8).
	assertPathPoints(t, path, []core.Point{start, pt(2, 8), pt(8, 8), goal}, 1e-4)
}

func TestNavMeshIslandObstacleShortestSide(t *testing.T) {
	// Room with a central island obstacle [4,6] x [4,6].
	// Make the bottom polygon much deeper ([4,6] x [-40,4]) so centroid-based search would fail,
	// while true geometric shortest path from (1,4.5) to (9,4.5) goes around the bottom corners (4,4)->(6,4).
	mesh := ai.BuildNavMesh([][]core.Point{
		rectPoly(0, -40, 4, 10),  // left
		rectPoly(6, -40, 10, 10), // right
		rectPoly(4, -40, 6, 4),   // bottom (deep)
		rectPoly(4, 6, 6, 10),    // top (narrow)
	})

	// Closer to y=4 (bottom edge of island) -> bottom route via (4,4) and (6,4).
	pathBottom, ok := mesh.FindPath(pt(1, 4.5), pt(9, 4.5))
	if !ok {
		t.Fatal("expected path around bottom of island")
	}
	assertPathPoints(t, pathBottom, []core.Point{pt(1, 4.5), pt(4, 4), pt(6, 4), pt(9, 4.5)}, 1e-4)

	// Closer to y=6 (top edge of island) -> top route via (4,6) and (6,6).
	pathTop, ok := mesh.FindPath(pt(1, 5.5), pt(9, 5.5))
	if !ok {
		t.Fatal("expected path around top of island")
	}
	assertPathPoints(t, pathTop, []core.Point{pt(1, 5.5), pt(4, 6), pt(6, 6), pt(9, 5.5)}, 1e-4)
}

func TestNavMeshTriangleFanAroundSharedVertex(t *testing.T) {
	// Fan of 4 triangles wrapping 180 degrees around reflex corner (0,0) in the upper half-plane.
	mesh := ai.BuildNavMesh([][]core.Point{
		{pt(0, 0), pt(-10, 0), pt(-7, 7)},
		{pt(0, 0), pt(-7, 7), pt(0, 10)},
		{pt(0, 0), pt(0, 10), pt(7, 7)},
		{pt(0, 0), pt(7, 7), pt(10, 0)},
	})

	// Direct straight chord across the upper half-plane.
	path, ok := mesh.FindPath(pt(-6, 2), pt(6, 2))
	if !ok {
		t.Fatal("expected path through triangle fan")
	}
	assertPathPoints(t, path, []core.Point{pt(-6, 2), pt(6, 2)}, 1e-4)

	// Wrapping past the reflex vertex at (0,0) when lower wings dip below y=0.
	fan2 := ai.BuildNavMesh([][]core.Point{
		{pt(0, 0), pt(-6, -4), pt(-10, 0)},
		{pt(0, 0), pt(-10, 0), pt(-7, 7)},
		{pt(0, 0), pt(-7, 7), pt(7, 7)},
		{pt(0, 0), pt(7, 7), pt(10, 0)},
		{pt(0, 0), pt(10, 0), pt(6, -4)},
	})
	wrap, ok := fan2.FindPath(pt(-6, -2), pt(6, -2))
	if !ok {
		t.Fatal("expected path wrapping around fan apex")
	}
	assertPathPoints(t, wrap, []core.Point{pt(-6, -2), pt(0, 0), pt(6, -2)}, 1e-4)
}

func TestNavMeshContainmentNearestAndDisconnected(t *testing.T) {
	mesh := ai.BuildNavMesh([][]core.Point{
		rectPoly(0, 0, 10, 10),  // poly 0
		rectPoly(20, 0, 30, 10), // poly 1 (disconnected)
		{pt(0, 0), pt(1, 0)},    // degenerate (ignored)
	})
	if mesh.PolygonCount() != 2 {
		t.Fatalf("PolygonCount = %d, want 2", mesh.PolygonCount())
	}

	// Interior, edge, and vertex containment.
	for _, inside := range []core.Point{pt(5, 5), pt(0, 5), pt(10, 10), pt(0, 0)} {
		if idx := mesh.FindPolygon(inside); idx != 0 {
			t.Fatalf("FindPolygon(%v) = %d, want 0", inside, idx)
		}
		if !mesh.Contains(inside) {
			t.Fatalf("Contains(%v) = false, want true", inside)
		}
	}

	// Point in the gap between the two islands.
	if mesh.Contains(pt(14, 5)) {
		t.Fatal("gap point should not be contained")
	}
	idx, clamped, ok := mesh.NearestPolygon(pt(14, 5))
	if !ok || idx != 0 {
		t.Fatalf("NearestPolygon(14,5) = (%d, %v, %v), want poly 0", idx, clamped, ok)
	}
	assertPathPoints(t, []core.Point{clamped}, []core.Point{pt(10, 5)}, 1e-4)

	idx2, clamped2, ok2 := mesh.NearestPolygon(pt(17, 15))
	if !ok2 || idx2 != 1 {
		t.Fatalf("NearestPolygon(17,15) = (%d, %v, %v), want poly 1", idx2, clamped2, ok2)
	}
	assertPathPoints(t, []core.Point{clamped2}, []core.Point{pt(20, 10)}, 1e-4)

	// Disconnected regions return no path.
	if _, ok := mesh.FindPath(pt(5, 5), pt(25, 5)); ok {
		t.Fatal("disconnected regions should not find a path")
	}
	// Outside endpoint returns no path.
	if _, ok := mesh.FindPath(pt(5, 5), pt(15, 5)); ok {
		t.Fatal("outside goal should not find a path")
	}
	// Same start and goal inside mesh.
	same, ok := mesh.FindPath(pt(5, 5), pt(5, 5))
	if !ok {
		t.Fatal("start==goal inside mesh should succeed")
	}
	assertPathPoints(t, same, []core.Point{pt(5, 5)}, 1e-4)
}

func TestNavMeshAreaCostsPreferRoadOverGrass(t *testing.T) {
	// Layout (x in [0, 20], y in [0, 10]):
	// - Poly 0 (left grass):  [0, 2] x [0, 10]
	// - Poly 1 (grass strip): [2, 18] x [0, 5]   (direct geometric path from (1,2) to (19,2))
	// - Poly 2 (road strip):  [2, 18] x [5, 10]  (cheaper area cost: 0.5 vs grass 1.0)
	// - Poly 3 (right grass): [18, 20] x [0, 10]
	mesh := ai.BuildNavMeshWithAreas([]ai.NavPolygonInput{
		{Vertices: rectPoly(0, 0, 2, 10), Area: ai.NavAreaGrass},
		{Vertices: rectPoly(2, 0, 18, 5), Area: ai.NavAreaGrass},
		{Vertices: rectPoly(2, 5, 18, 10), Area: ai.NavAreaRoad},
		{Vertices: rectPoly(18, 0, 20, 10), Area: ai.NavAreaGrass},
	})

	if mesh.PolygonArea(1) != ai.NavAreaGrass || mesh.PolygonArea(2) != ai.NavAreaRoad {
		t.Fatalf("unexpected polygon areas: grass=%d road=%d", mesh.PolygonArea(1), mesh.PolygonArea(2))
	}
	if mesh.PolygonFlags(2) != ai.NavFlagWalk {
		t.Fatalf("zero input flags should default to NavFlagWalk, got %v", mesh.PolygonFlags(2))
	}

	start, goal := pt(1, 2), pt(19, 2)

	// Default filter treats all areas equally (cost 1.0): straight line across grass.
	defaultPath, ok := mesh.FindPath(start, goal)
	if !ok {
		t.Fatal("expected default path")
	}
	assertPathPoints(t, defaultPath, []core.Point{start, goal}, 1e-4)

	// With road cheaper than grass (road=0.5, grass=1.0):
	// Direct grass cost = 18.0; road detour cost = sqrt(10) + 16*0.5 + sqrt(10) ≈ 14.324 < 18.0.
	// Funnel pulls taut onto road corners (2,5) and (18,5).
	filter := ai.DefaultNavFilter()
	filter.SetAreaCost(ai.NavAreaRoad, 0.5)
	filter.SetAreaCost(ai.NavAreaGrass, 1.0)

	roadPath, ok := mesh.FindPathWithFilter(start, goal, filter)
	if !ok {
		t.Fatal("expected road-preferring path")
	}
	wantRoad := []core.Point{start, pt(2, 5), pt(18, 5), goal}
	assertPathPoints(t, roadPath, wantRoad, 1e-4)

	// Determinism across repeated runs.
	for i := range 50 {
		again, ok := mesh.FindPathWithFilter(start, goal, filter)
		if !ok {
			t.Fatalf("run %d failed", i)
		}
		assertPathPoints(t, again, wantRoad, 1e-4)
	}

	// Changing road polygon to mud at runtime (without rebuilding) restores straight grass path.
	mesh.SetPolygonArea(2, ai.NavAreaMud)
	filter.SetAreaCost(ai.NavAreaMud, 2.5)
	mudAvoidPath, ok := mesh.FindPathWithFilter(start, goal, filter)
	if !ok {
		t.Fatal("expected path after runtime area change")
	}
	assertPathPoints(t, mudAvoidPath, []core.Point{start, goal}, 1e-4)
}

func TestNavMeshExcludeWaterAndSwimmingAgent(t *testing.T) {
	// Ring around a central water pool [4, 16] x [2, 8]:
	// - Poly 0 (west):  [0, 4] x [0, 10]   (Walk)
	// - Poly 1 (east):  [16, 20] x [0, 10] (Walk)
	// - Poly 2 (water): [4, 16] x [2, 8]   (AreaWater, FlagSwim)
	// - Poly 3 (north): [4, 16] x [8, 10]  (Walk)
	// - Poly 4 (south): [4, 16] x [0, 2]   (Walk)
	mesh := ai.BuildNavMeshWithAreas([]ai.NavPolygonInput{
		{Vertices: rectPoly(0, 0, 4, 10), Area: ai.NavAreaGrass, Flags: ai.NavFlagWalk},
		{Vertices: rectPoly(16, 0, 20, 10), Area: ai.NavAreaGrass, Flags: ai.NavFlagWalk},
		{Vertices: rectPoly(4, 2, 16, 8), Area: ai.NavAreaWater, Flags: ai.NavFlagSwim},
		{Vertices: rectPoly(4, 8, 16, 10), Area: ai.NavAreaGrass, Flags: ai.NavFlagWalk},
		{Vertices: rectPoly(4, 0, 16, 2), Area: ai.NavAreaGrass, Flags: ai.NavFlagWalk},
	})

	start, goal := pt(2, 3), pt(18, 3)

	// Land unit excludes water via ExcludeFlags: routes cleanly around south corners (4,2)->(16,2).
	landFilter := ai.DefaultNavFilter()
	landFilter.ExcludeFlags |= ai.NavFlagSwim
	landPath, ok := mesh.FindPathWithFilter(start, goal, landFilter)
	if !ok {
		t.Fatal("expected land path around water")
	}
	assertPathPoints(t, landPath, []core.Point{start, pt(4, 2), pt(16, 2), goal}, 1e-4)

	// Excluding by area ID produces the same detour.
	areaFilter := ai.DefaultNavFilter()
	areaFilter.ExcludeArea(ai.NavAreaWater)
	areaPath, ok := mesh.FindPathWithFilter(start, goal, areaFilter)
	if !ok {
		t.Fatal("expected area-excluded path around water")
	}
	assertPathPoints(t, areaPath, []core.Point{start, pt(4, 2), pt(16, 2), goal}, 1e-4)

	// Swimming unit includes both Walk and Swim: crosses water in a straight line.
	swimFilter := ai.NavFilter{
		IncludeFlags: ai.NavFlagWalk | ai.NavFlagSwim,
		ExcludeFlags: ai.NavFlagDisabled,
	}
	swimPath, ok := mesh.FindPathWithFilter(start, goal, swimFilter)
	if !ok {
		t.Fatal("expected swimming path across water")
	}
	assertPathPoints(t, swimPath, []core.Point{start, goal}, 1e-4)

	// Filtered spatial queries respect exclusion.
	waterCenter := pt(10, 5)
	if !mesh.Contains(waterCenter) {
		t.Fatal("unfiltered Contains should report water polygon")
	}
	if mesh.ContainsWithFilter(waterCenter, landFilter) {
		t.Fatal("landFilter should exclude water center")
	}
	if idx := mesh.FindPolygonWithFilter(waterCenter, landFilter); idx != -1 {
		t.Fatalf("FindPolygonWithFilter in water = %d, want -1", idx)
	}
	// Shared edge between west grass and water resolves to grass under landFilter.
	if idx := mesh.FindPolygonWithFilter(pt(4, 5), landFilter); idx != 0 {
		t.Fatalf("shared edge FindPolygonWithFilter = %d, want 0 (west grass)", idx)
	}
	// NearestPolygonWithFilter from inside water snaps out to nearest dry edge (south or north).
	nearIdx, nearPt, ok := mesh.NearestPolygonWithFilter(pt(10, 3), landFilter)
	if !ok || nearIdx != 4 {
		t.Fatalf("NearestPolygonWithFilter(10,3) = (%d, %v, %v), want poly 4 (south)", nearIdx, nearPt, ok)
	}
	assertPathPoints(t, []core.Point{nearPt}, []core.Point{pt(10, 2)}, 1e-4)

	// Path starting inside excluded water returns no path.
	if _, ok := mesh.FindPathWithFilter(waterCenter, goal, landFilter); ok {
		t.Fatal("path starting inside excluded water should fail")
	}
}

func TestNavMeshRuntimeDoorToggleWithoutRebuild(t *testing.T) {
	// Two routes between west room [0,4]x[0,10] and east room [8,12]x[0,10]:
	// - Direct doorway shortcut: [4, 8] x [4, 6] (Poly 2, AreaDoor, FlagWalk|FlagDoor)
	// - Long northern detour:    [4, 8] x [8, 10] (Poly 3, FlagWalk)
	// Wall separates them at [4,8]x[6,8] and [4,8]x[0,4].
	mesh := ai.BuildNavMeshWithAreas([]ai.NavPolygonInput{
		{Vertices: rectPoly(0, 0, 4, 10), Area: ai.NavAreaDefault, Flags: ai.NavFlagWalk},
		{Vertices: rectPoly(8, 0, 12, 10), Area: ai.NavAreaDefault, Flags: ai.NavFlagWalk},
		{Vertices: rectPoly(4, 4, 8, 6), Area: ai.NavAreaDoor, Flags: ai.NavFlagWalk | ai.NavFlagDoor},
		{Vertices: rectPoly(4, 8, 8, 10), Area: ai.NavAreaDefault, Flags: ai.NavFlagWalk},
	})

	start, goal := pt(2, 5), pt(10, 5)
	const doorPoly = 2

	// 1. Door open -> straight through doorway.
	openPath, ok := mesh.FindPath(start, goal)
	if !ok {
		t.Fatal("expected straight path through open door")
	}
	assertPathPoints(t, openPath, []core.Point{start, goal}, 1e-4)

	// 2. Close door by adding NavFlagDisabled -> immediate detour around north corridor.
	mesh.AddPolygonFlags(doorPoly, ai.NavFlagDisabled)
	closedPath, ok := mesh.FindPath(start, goal)
	if !ok {
		t.Fatal("expected detour path when door is closed")
	}
	assertPathPoints(t, closedPath, []core.Point{start, pt(4, 8), pt(8, 8), goal}, 1e-4)

	// 3. Reopen door by clearing NavFlagDisabled -> restores direct shortcut immediately.
	mesh.ClearPolygonFlags(doorPoly, ai.NavFlagDisabled)
	reopenedPath, ok := mesh.FindPath(start, goal)
	if !ok {
		t.Fatal("expected direct path after reopening door")
	}
	assertPathPoints(t, reopenedPath, []core.Point{start, goal}, 1e-4)

	// 4. Closing door by clearing all flags to 0 also blocks DefaultNavFilter.
	mesh.SetPolygonFlags(doorPoly, 0)
	zeroFlagPath, ok := mesh.FindPath(start, goal)
	if !ok {
		t.Fatal("expected detour path when door flags are 0")
	}
	assertPathPoints(t, zeroFlagPath, []core.Point{start, pt(4, 8), pt(8, 8), goal}, 1e-4)

	// 5. Restoring NavFlagWalk | NavFlagDoor restores shortcut, while a filter excluding NavFlagDoor still detours.
	mesh.SetPolygonFlags(doorPoly, ai.NavFlagWalk|ai.NavFlagDoor)
	noDoorFilter := ai.DefaultNavFilter()
	noDoorFilter.ExcludeFlags |= ai.NavFlagDoor
	noDoorPath, ok := mesh.FindPathWithFilter(start, goal, noDoorFilter)
	if !ok {
		t.Fatal("expected detour for filter excluding NavFlagDoor")
	}
	assertPathPoints(t, noDoorPath, []core.Point{start, pt(4, 8), pt(8, 8), goal}, 1e-4)
}

func TestNavMeshFilterEdgeCasesAndNilSafety(t *testing.T) {
	// Invalid cost values (0, negative, NaN, +Inf) safely forbid that area.
	mesh := ai.BuildNavMeshWithAreas([]ai.NavPolygonInput{
		{Vertices: rectPoly(0, 0, 10, 10), Area: ai.NavAreaHazard},
	})
	for _, badCost := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		f := ai.DefaultNavFilter()
		f.SetAreaCost(ai.NavAreaHazard, badCost)
		if f.Allows(ai.NavAreaHazard, ai.NavFlagWalk) {
			t.Fatalf("bad cost %v should not be allowed", badCost)
		}
		if !math.IsInf(f.AreaCost(ai.NavAreaHazard), 1) {
			t.Fatalf("AreaCost(%v) = %v, want +Inf", badCost, f.AreaCost(ai.NavAreaHazard))
		}
		if _, ok := mesh.FindPathWithFilter(pt(1, 1), pt(9, 9), f); ok {
			t.Fatalf("path should fail for invalid area cost %v", badCost)
		}
	}

	// Clone produces an independent filter.
	orig := ai.DefaultNavFilter()
	orig.SetAreaCost(ai.NavAreaRoad, 0.5)
	orig.ExcludeArea(ai.NavAreaWater)
	cloned := orig.Clone()
	cloned.SetAreaCost(ai.NavAreaRoad, 2.0)
	cloned.IncludeArea(ai.NavAreaWater)
	if orig.AreaCost(ai.NavAreaRoad) != 0.5 {
		t.Fatalf("Clone mutated original AreaCosts")
	}
	if orig.Allows(ai.NavAreaWater, ai.NavFlagWalk) {
		t.Fatalf("Clone mutated original ExcludedAreas")
	}

	// Nil and out-of-range safety.
	var nilMesh *ai.NavMesh
	if nilMesh.PolygonArea(0) != ai.NavAreaDefault || nilMesh.PolygonFlags(0) != 0 {
		t.Fatal("nil mesh getters returned unexpected values")
	}
	nilMesh.SetPolygonArea(0, ai.NavAreaRoad)
	nilMesh.SetPolygonFlags(0, ai.NavFlagDisabled)
	nilMesh.AddPolygonFlags(0, ai.NavFlagDoor)
	nilMesh.ClearPolygonFlags(0, ai.NavFlagWalk)
	if mesh.PolygonArea(-1) != ai.NavAreaDefault || mesh.PolygonArea(99) != ai.NavAreaDefault {
		t.Fatal("out-of-range PolygonArea should return NavAreaDefault")
	}
	if mesh.PolygonFlags(-1) != 0 || mesh.PolygonFlags(99) != 0 {
		t.Fatal("out-of-range PolygonFlags should return 0")
	}
	mesh.SetPolygonArea(-1, ai.NavAreaRoad)
	mesh.SetPolygonFlags(99, ai.NavFlagDisabled)
}
