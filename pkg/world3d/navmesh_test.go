package world3d_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/pathfind"
	"github.com/nsgo-dev/game/pkg/world3d"
)

func assertPath3D(t *testing.T, got, want []math3d.Vec3, tol float32) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("path len = %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if !got[i].ApproxEqual(want[i], tol) {
			t.Fatalf("path[%d] = %v, want %v (full got=%v)", i, got[i], want[i], got)
		}
	}
}

func TestNavMesh3DAreaCostsPreferRoadOverGrass(t *testing.T) {
	// Layout on Y=0 plane (X in [0, 20], Z in [0, 10]):
	// - Poly 0 (west grass):  [0, 2] x [0, 10]
	// - Poly 1 (grass strip): [2, 18] x [0, 5]   (direct geometric line for (1,0,2) -> (19,0,2))
	// - Poly 2 (road strip):  [2, 18] x [5, 10]  (cheaper area cost: 0.5 vs grass 1.0)
	// - Poly 3 (east grass):  [18, 20] x [0, 10]
	nm := world3d.BuildNavMeshWithAreas([]world3d.NavPolygonInput{
		{Vertices: quadXZ(0, 0, 2, 10, 0), Area: world3d.NavAreaGrass},
		{Vertices: quadXZ(2, 0, 18, 5, 0), Area: world3d.NavAreaGrass},
		{Vertices: quadXZ(2, 5, 18, 10, 0), Area: world3d.NavAreaRoad},
		{Vertices: quadXZ(18, 0, 20, 10, 0), Area: world3d.NavAreaGrass},
	}, math3d.DegToRad(45))

	if nm.PolygonCount() != 4 {
		t.Fatalf("PolygonCount = %d, want 4", nm.PolygonCount())
	}
	if nm.PolygonArea(1) != world3d.NavAreaGrass || nm.PolygonArea(2) != world3d.NavAreaRoad {
		t.Fatalf("unexpected areas: grass=%d road=%d", nm.PolygonArea(1), nm.PolygonArea(2))
	}
	if nm.PolygonFlags(2) != world3d.NavFlagWalk {
		t.Fatalf("zero input flags should default to NavFlagWalk, got %v", nm.PolygonFlags(2))
	}

	start := math3d.Vec3{X: 1, Y: 0, Z: 2}
	goal := math3d.Vec3{X: 19, Y: 0, Z: 2}

	// Default filter takes direct line across grass.
	defaultPath, ok := nm.FindPath(start, goal)
	if !ok {
		t.Fatal("expected default 3D path")
	}
	assertPath3D(t, defaultPath, []math3d.Vec3{start, goal}, 1e-3)

	// Shared pathfind.NavFilter works directly with world3d.NavMesh (type alias).
	filter := pathfind.DefaultNavFilter()
	filter.SetAreaCost(pathfind.NavAreaRoad, 0.5)
	filter.SetAreaCost(pathfind.NavAreaGrass, 1.0)

	roadPath, ok := nm.FindPathWithFilter(start, goal, filter)
	if !ok {
		t.Fatal("expected road-preferring 3D path")
	}
	wantRoad := []math3d.Vec3{
		start,
		{X: 2, Y: 0, Z: 5},
		{X: 18, Y: 0, Z: 5},
		goal,
	}
	assertPath3D(t, roadPath, wantRoad, 1e-3)

	// Determinism across repeated runs.
	for i := range 50 {
		again, ok := nm.FindPathWithFilter(start, goal, filter)
		if !ok {
			t.Fatalf("run %d failed", i)
		}
		assertPath3D(t, again, wantRoad, 1e-3)
	}

	// Mutating road polygon to mud at runtime restores the straight grass route without rebuilding.
	nm.SetPolygonArea(2, world3d.NavAreaMud)
	filter.SetAreaCost(world3d.NavAreaMud, 2.5)
	afterMud, ok := nm.FindPathWithFilter(start, goal, filter)
	if !ok {
		t.Fatal("expected path after runtime area change")
	}
	assertPath3D(t, afterMud, []math3d.Vec3{start, goal}, 1e-3)
}

func TestNavMesh3DExcludeWaterAndSwimmingAgent(t *testing.T) {
	// Ring around a central water basin [4, 16] x [2, 8] at Y=0:
	nm := world3d.BuildNavMeshWithAreas([]world3d.NavPolygonInput{
		{Vertices: quadXZ(0, 0, 4, 10, 0), Area: world3d.NavAreaGrass, Flags: world3d.NavFlagWalk},
		{Vertices: quadXZ(16, 0, 20, 10, 0), Area: world3d.NavAreaGrass, Flags: world3d.NavFlagWalk},
		{Vertices: quadXZ(4, 2, 16, 8, 0), Area: world3d.NavAreaWater, Flags: world3d.NavFlagSwim},
		{Vertices: quadXZ(4, 8, 16, 10, 0), Area: world3d.NavAreaGrass, Flags: world3d.NavFlagWalk},
		{Vertices: quadXZ(4, 0, 16, 2, 0), Area: world3d.NavAreaGrass, Flags: world3d.NavFlagWalk},
	}, math3d.DegToRad(45))

	start := math3d.Vec3{X: 2, Y: 0, Z: 3}
	goal := math3d.Vec3{X: 18, Y: 0, Z: 3}

	// Land filter excludes water flag: detours around south edge (4,0,2) -> (16,0,2).
	landFilter := world3d.DefaultNavFilter()
	landFilter.ExcludeFlags |= world3d.NavFlagSwim
	landPath, ok := nm.FindPathWithFilter(start, goal, landFilter)
	if !ok {
		t.Fatal("expected 3D land path around water")
	}
	assertPath3D(t, landPath, []math3d.Vec3{
		start,
		{X: 4, Y: 0, Z: 2},
		{X: 16, Y: 0, Z: 2},
		goal,
	}, 1e-3)

	// Swimming agent crosses water directly.
	swimFilter := world3d.NavFilter{
		IncludeFlags: world3d.NavFlagWalk | world3d.NavFlagSwim,
		ExcludeFlags: world3d.NavFlagDisabled,
	}
	swimPath, ok := nm.FindPathWithFilter(start, goal, swimFilter)
	if !ok {
		t.Fatal("expected 3D swimming path across water")
	}
	assertPath3D(t, swimPath, []math3d.Vec3{start, goal}, 1e-3)

	// Filtered spatial queries respect exclusion.
	waterPt := math3d.Vec3{X: 10, Y: 0, Z: 3}
	if !nm.Contains(waterPt) {
		t.Fatal("unfiltered Contains should report water")
	}
	if nm.ContainsWithFilter(waterPt, landFilter) {
		t.Fatal("landFilter should exclude water point")
	}
	nearIdx, nearPt, ok := nm.NearestPolygonWithFilter(waterPt, landFilter)
	if !ok || nearIdx != 4 {
		t.Fatalf("NearestPolygonWithFilter = (%d, %v, %v), want poly 4 (south)", nearIdx, nearPt, ok)
	}
	if !nearPt.ApproxEqual(math3d.Vec3{X: 10, Y: 0, Z: 2}, 1e-3) {
		t.Fatalf("nearest dry point = %v, want (10,0,2)", nearPt)
	}
}

func TestNavMesh3DRuntimeDoorToggleWithoutRebuild(t *testing.T) {
	// West hall [0,4]x[0,10], East hall [8,12]x[0,10],
	// Direct doorway [4,8]x[4,6] (Poly 2, NavAreaDoor, NavFlagWalk|NavFlagDoor),
	// Northern bypass [4,8]x[8,10] (Poly 3, NavFlagWalk).
	nm := world3d.BuildNavMeshWithAreas([]world3d.NavPolygonInput{
		{Vertices: quadXZ(0, 0, 4, 10, 0), Area: world3d.NavAreaDefault, Flags: world3d.NavFlagWalk},
		{Vertices: quadXZ(8, 0, 12, 10, 0), Area: world3d.NavAreaDefault, Flags: world3d.NavFlagWalk},
		{Vertices: quadXZ(4, 4, 8, 6, 0), Area: world3d.NavAreaDoor, Flags: world3d.NavFlagWalk | world3d.NavFlagDoor},
		{Vertices: quadXZ(4, 8, 8, 10, 0), Area: world3d.NavAreaDefault, Flags: world3d.NavFlagWalk},
	}, math3d.DegToRad(45))

	start := math3d.Vec3{X: 2, Y: 0, Z: 5}
	goal := math3d.Vec3{X: 10, Y: 0, Z: 5}
	const doorPoly = 2

	// Open door -> straight through.
	openPath, ok := nm.FindPath(start, goal)
	if !ok {
		t.Fatal("expected straight 3D path through open door")
	}
	assertPath3D(t, openPath, []math3d.Vec3{start, goal}, 1e-3)

	// Close door at runtime -> detours via northern bypass (4,0,8)->(8,0,8).
	nm.AddPolygonFlags(doorPoly, world3d.NavFlagDisabled)
	closedPath, ok := nm.FindPath(start, goal)
	if !ok {
		t.Fatal("expected 3D detour when door is closed")
	}
	assertPath3D(t, closedPath, []math3d.Vec3{
		start,
		{X: 4, Y: 0, Z: 8},
		{X: 8, Y: 0, Z: 8},
		goal,
	}, 1e-3)

	// Reopen door -> restores straight shortcut immediately without rebuilding.
	nm.ClearPolygonFlags(doorPoly, world3d.NavFlagDisabled)
	reopenedPath, ok := nm.FindPath(start, goal)
	if !ok {
		t.Fatal("expected straight 3D path after reopening door")
	}
	assertPath3D(t, reopenedPath, []math3d.Vec3{start, goal}, 1e-3)

	// Setting door flags to 0 also blocks default filter; restoring flags unblocks it.
	nm.SetPolygonFlags(doorPoly, 0)
	zeroPath, ok := nm.FindPath(start, goal)
	if !ok {
		t.Fatal("expected detour when door flags are cleared to 0")
	}
	assertPath3D(t, zeroPath, []math3d.Vec3{
		start,
		{X: 4, Y: 0, Z: 8},
		{X: 8, Y: 0, Z: 8},
		goal,
	}, 1e-3)
}

func TestNavMesh3DMultiLevelRampWithAreasAndFilter(t *testing.T) {
	// Ground level Y=0 vs Bridge level Y=3 connected by a sloped ramp:
	// - Poly 0 (ground):   [0, 12] x [-2, 2] at Y=0
	// - Poly 1 (landing):  [12, 16] x [-2, 2] at Y=0
	// - Poly 2 (approach): [12, 16] x [2, 6] at Y=0
	// - Poly 3 (ramp):     X 12->6, Z [2, 6], Y 0->3 (AreaDoor so it acts as a drawbridge/gate)
	// - Poly 4 (upper):    [2, 6] x [2, 6] at Y=3
	// - Poly 5 (bridge):   [2, 10] x [-2, 2] at Y=3
	ramp := []math3d.Vec3{
		{X: 6, Y: 3, Z: 2},
		{X: 6, Y: 3, Z: 6},
		{X: 12, Y: 0, Z: 6},
		{X: 12, Y: 0, Z: 2},
	}
	nm := world3d.BuildNavMeshWithAreas([]world3d.NavPolygonInput{
		{Vertices: quadXZ(0, -2, 12, 2, 0), Area: world3d.NavAreaGrass},
		{Vertices: quadXZ(12, -2, 16, 2, 0), Area: world3d.NavAreaGrass},
		{Vertices: quadXZ(12, 2, 16, 6, 0), Area: world3d.NavAreaGrass},
		{Vertices: ramp, Area: world3d.NavAreaDoor, Flags: world3d.NavFlagWalk | world3d.NavFlagDoor},
		{Vertices: quadXZ(2, 2, 6, 6, 3), Area: world3d.NavAreaRoad},
		{Vertices: quadXZ(2, -2, 10, 2, 3), Area: world3d.NavAreaRoad},
	}, math3d.DegToRad(45))
	nm.VerticalTolerance = 1.2

	start := math3d.Vec3{X: 6, Y: 0, Z: 0}
	goal := math3d.Vec3{X: 6, Y: 3, Z: 0}

	// Ramp open -> climb succeeds.
	if _, ok := nm.FindPath(start, goal); !ok {
		t.Fatal("expected climb path with open ramp")
	}

	// Disable ramp at runtime -> bridge becomes unreachable from ground floor.
	nm.AddPolygonFlags(3, world3d.NavFlagDisabled)
	if _, ok := nm.FindPath(start, goal); ok {
		t.Fatal("expected bridge to be unreachable while ramp is disabled")
	}

	// Re-enable ramp -> climb succeeds again.
	nm.ClearPolygonFlags(3, world3d.NavFlagDisabled)
	if _, ok := nm.FindPath(start, goal); !ok {
		t.Fatal("expected climb path after re-enabling ramp")
	}

	// Nil/out-of-range safety and invalid cost filtering.
	var nilMesh *world3d.NavMesh
	if nilMesh.PolygonCount() != 0 || nilMesh.PolygonArea(0) != world3d.NavAreaDefault || nilMesh.PolygonFlags(0) != 0 {
		t.Fatal("nil NavMesh getters returned unexpected values")
	}
	nilMesh.SetPolygonArea(0, world3d.NavAreaRoad)
	nilMesh.SetPolygonFlags(0, world3d.NavFlagDisabled)
	badFilter := world3d.DefaultNavFilter()
	badFilter.SetAreaCost(world3d.NavAreaRoad, math.NaN())
	if _, ok := nm.FindPathWithFilter(start, goal, badFilter); ok {
		t.Fatal("NaN area cost on destination polygon should forbid path")
	}
}
