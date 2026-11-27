package pathfind3d_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/pathfind3d"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

const eps = 1e-4

func quadXZ(x0, z0, x1, z1, y float32) []math3d.Vec3 {
	return []math3d.Vec3{
		{X: x0, Y: y, Z: z0},
		{X: x0, Y: y, Z: z1},
		{X: x1, Y: y, Z: z1},
		{X: x1, Y: y, Z: z0},
	}
}

func TestNavMeshSlopeFilteringAndObstacleAvoidance(t *testing.T) {
	// Ring of 4 walkable floor quads surrounding a 4x4 obstacle hole [2..6, 2..6],
	// plus a vertical wall (90 deg) and a steep 60-deg cliff that must be filtered out.
	polys := [][]math3d.Vec3{
		quadXZ(0, 0, 8, 2, 0), // north strip
		quadXZ(0, 6, 8, 8, 0), // south strip
		quadXZ(0, 2, 2, 6, 0), // west strip
		// East strip intentionally passed in CW winding to verify winding normalization.
		{
			{X: 6, Y: 0, Z: 2},
			{X: 8, Y: 0, Z: 2},
			{X: 8, Y: 0, Z: 6},
			{X: 6, Y: 0, Z: 6},
		},
		// Vertical wall (90 deg slope) — must be filtered out.
		{
			{X: 2, Y: 0, Z: 2},
			{X: 6, Y: 0, Z: 2},
			{X: 6, Y: 4, Z: 2},
			{X: 2, Y: 4, Z: 2},
		},
		// Steep 60-deg slope (rise = sqrt(3)*2 over run = 2) — exceeds 45-deg maxSlope.
		{
			{X: 2, Y: 0, Z: 2},
			{X: 2, Y: 0, Z: 6},
			{X: 4, Y: float32(2 * math.Sqrt(3)), Z: 6},
			{X: 4, Y: float32(2 * math.Sqrt(3)), Z: 2},
		},
	}

	nm := pathfind3d.BuildNavMesh(polys, math3d.DegToRad(45))
	if len(nm.Polys) != 4 {
		t.Fatalf("walkable poly count = %d, want 4 (wall and 60-deg slope filtered)", len(nm.Polys))
	}

	// Point inside obstacle hole must not be on navmesh.
	if _, ok := nm.FindPolygon(math3d.Vec3{X: 4, Y: 0, Z: 4}); ok {
		t.Fatalf("expected obstacle interior to have no polygon")
	}

	// Path from west side (1, 0, 4) to east side (7, 0, 4) must route cleanly around the hole.
	start := math3d.Vec3{X: 1, Y: 0, Z: 4}
	goal := math3d.Vec3{X: 7, Y: 0, Z: 4}
	path, ok := nm.FindPath(start, goal)
	if !ok {
		t.Fatalf("FindPath around obstacle failed")
	}
	if len(path) < 3 {
		t.Fatalf("expected path around obstacle to bend around corners, got %v", path)
	}
	if !path[0].ApproxEqual(start, eps) || !path[len(path)-1].ApproxEqual(goal, eps) {
		t.Fatalf("path endpoints = (%v, %v), want (%v, %v)", path[0], path[len(path)-1], start, goal)
	}

	// Verify no sampled point along any path segment enters the central obstacle (2..6, 2..6).
	for i := range len(path) - 1 {
		for step := 1; step < 10; step++ {
			p := path[i].Lerp(path[i+1], float32(step)/10.0)
			if p.X > 2.05 && p.X < 5.95 && p.Z > 2.05 && p.Z < 5.95 {
				t.Fatalf("path cut through obstacle at %v (full path: %v)", p, path)
			}
		}
	}
}

func TestNavMeshSlopedRampAndMultiLevelBridge(t *testing.T) {
	// Multi-level 3D scene:
	// - Ground floor: X in [0, 12], Z in [-2, 2], Y = 0
	// - Side landing (ground): X in [12, 16], Z in [-2, 2], Y = 0
	// - North approach (ground): X in [12, 16], Z in [2, 6], Y = 0
	// - Sloped ramp heading west: X in [12, 6] (run=6), Z in [2, 6], Y rises 0 -> 3 (slope ~26.6 deg < 45 deg)
	// - Upper landing: X in [2, 6], Z in [2, 6], Y = 3
	// - Upper bridge deck directly ABOVE ground floor: X in [2, 10], Z in [-2, 2], Y = 3
	// - Disconnected floating island: X in [20, 24], Z in [-2, 2], Y = 3
	ground := quadXZ(0, -2, 12, 2, 0)
	eastLanding := quadXZ(12, -2, 16, 2, 0)
	northApproach := quadXZ(12, 2, 16, 6, 0)
	ramp := []math3d.Vec3{
		{X: 6, Y: 3, Z: 2},
		{X: 6, Y: 3, Z: 6},
		{X: 12, Y: 0, Z: 6},
		{X: 12, Y: 0, Z: 2},
	}
	upperLanding := quadXZ(2, 2, 6, 6, 3)
	bridgeDeck := quadXZ(2, -2, 10, 2, 3)
	island := quadXZ(20, -2, 24, 2, 3)

	nm := pathfind3d.BuildNavMesh([][]math3d.Vec3{
		ground,
		eastLanding,
		northApproach,
		ramp,
		upperLanding,
		bridgeDeck,
		island,
	}, math3d.DegToRad(45))
	nm.VerticalTolerance = 1.2

	if len(nm.Polys) != 7 {
		t.Fatalf("expected all 7 walkable polygons, got %d", len(nm.Polys))
	}

	// Vertical separation check: same XZ (6, 0) at Y=0.1 vs Y=2.9 selects distinct polygons!
	pGround, okG := nm.FindPolygon(math3d.Vec3{X: 6, Y: 0.1, Z: 0})
	pBridge, okB := nm.FindPolygon(math3d.Vec3{X: 6, Y: 2.9, Z: 0})
	if !okG || !okB {
		t.Fatalf("FindPolygon failed: ground=%v bridge=%v", okG, okB)
	}
	if pGround == pBridge {
		t.Fatalf("ground and bridge above it resolved to the same polygon %d", pGround)
	}
	if _, ok := nm.FindPolygon(math3d.Vec3{X: 6, Y: 10, Z: 0}); ok {
		t.Fatalf("point far above bridge should not match any polygon")
	}

	// 1. Straight path UNDER the bridge on the ground floor stays at Y = 0.
	underPath, ok := nm.FindPath(math3d.Vec3{X: 1, Y: 0, Z: 0}, math3d.Vec3{X: 11, Y: 0, Z: 0})
	if !ok {
		t.Fatalf("FindPath under bridge failed")
	}
	for _, wp := range underPath {
		if !math3d.ApproxEqual(wp.Y, 0, eps) {
			t.Fatalf("under-bridge waypoint jumped to Y=%v (path=%v)", wp.Y, underPath)
		}
	}

	// 2. Path from ground floor directly underneath the bridge (6, 0, 0) to the bridge
	// deck directly overhead (6, 3, 0): must route out to the ramp, climb up to Y=3,
	// and arrive on the bridge deck!
	climbPath, ok := nm.FindPath(math3d.Vec3{X: 6, Y: 0, Z: 0}, math3d.Vec3{X: 6, Y: 3, Z: 0})
	if !ok {
		t.Fatalf("FindPath from under bridge up ramp onto bridge failed")
	}
	if !climbPath[0].ApproxEqual(math3d.Vec3{X: 6, Y: 0, Z: 0}, eps) {
		t.Fatalf("climb start = %v, want (6,0,0)", climbPath[0])
	}
	if !climbPath[len(climbPath)-1].ApproxEqual(math3d.Vec3{X: 6, Y: 3, Z: 0}, eps) {
		t.Fatalf("climb goal = %v, want (6,3,0)", climbPath[len(climbPath)-1])
	}
	// Must visit the east ramp side (X >= 11.9) and climb monotonically through Y in [0, 3].
	reachedRampEntrance := false
	for _, wp := range climbPath {
		if wp.X >= 11.9 {
			reachedRampEntrance = true
		}
		// Every waypoint must lie on a walkable polygon at its exact surface height.
		polyIdx, found := nm.FindPolygon(wp)
		if !found {
			t.Fatalf("climb waypoint %v is off the navmesh (path=%v)", wp, climbPath)
		}
		surfY := nm.Polys[polyIdx].HeightAt(wp.X, wp.Z)
		if !math3d.ApproxEqual(wp.Y, surfY, 1e-3) {
			t.Fatalf("climb waypoint %v does not match surface height %v", wp, surfY)
		}
	}
	if !reachedRampEntrance {
		t.Fatalf("climb path did not route through ramp entrance at X>=12: %v", climbPath)
	}

	// 3. Straight climb straight up the ramp: verify exact Y bends at ramp bottom (X=12) and top (X=6).
	rampPath, ok := nm.FindPath(math3d.Vec3{X: 15, Y: 0, Z: 4}, math3d.Vec3{X: 3, Y: 3, Z: 4})
	if !ok {
		t.Fatalf("FindPath straight up ramp failed")
	}
	// Expect waypoints: (15,0,4) -> (12,0,4) -> (6,3,4) -> (3,3,4)
	if len(rampPath) != 4 {
		t.Fatalf("straight ramp path = %v (len %d), want 4 exact ramp-edge waypoints", rampPath, len(rampPath))
	}
	wantRamp := []math3d.Vec3{
		{X: 15, Y: 0, Z: 4},
		{X: 12, Y: 0, Z: 4},
		{X: 6, Y: 3, Z: 4},
		{X: 3, Y: 3, Z: 4},
	}
	for i, want := range wantRamp {
		if !rampPath[i].ApproxEqual(want, 1e-3) {
			t.Fatalf("rampPath[%d] = %v, want %v (full=%v)", i, rampPath[i], want, rampPath)
		}
	}

	// 4. Disconnected island is unreachable.
	if _, ok := nm.FindPath(math3d.Vec3{X: 6, Y: 0, Z: 0}, math3d.Vec3{X: 22, Y: 3, Z: 0}); ok {
		t.Fatalf("expected path to disconnected floating island to return false")
	}
}
