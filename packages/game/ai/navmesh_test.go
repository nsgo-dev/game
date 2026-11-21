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
