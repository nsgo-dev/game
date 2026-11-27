package ai_test

import (
	"math"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/nsgo/core"
)

func pt2(x, y float32) core.Point {
	return core.Point{X: core.Points(x), Y: core.Points(y)}
}

func rect2D(x0, y0, x1, y1 float32) []core.Point {
	return []core.Point{pt2(x0, y0), pt2(x1, y0), pt2(x1, y1), pt2(x0, y1)}
}

func pathLen2D(pts []core.Point) float64 {
	var total float64
	for i := 1; i < len(pts); i++ {
		dx := float64(pts[i].X - pts[i-1].X)
		dy := float64(pts[i].Y - pts[i-1].Y)
		total += math.Hypot(dx, dy)
	}
	return total
}

func approxPt2D(a, b core.Point, eps float64) bool {
	return math.Hypot(float64(a.X-b.X), float64(a.Y-b.Y)) <= eps
}

func segmentIntersectsBox2D(a, b core.Point, minX, minY, maxX, maxY float64) bool {
	// Sample 20 interior points strictly inside box (shrunk slightly so grazing edges is allowed).
	const shrink = 1e-2
	x0, y0 := minX+shrink, minY+shrink
	x1, y1 := maxX-shrink, maxY-shrink
	for i := 0; i <= 32; i++ {
		t := float64(i) / 32.0
		px := float64(a.X) + (float64(b.X)-float64(a.X))*t
		py := float64(a.Y) + (float64(b.Y)-float64(a.Y))*t
		if px > x0 && px < x1 && py > y0 && py < y1 {
			return true
		}
	}
	return false
}

// Acceptance Criterion 1: Dropping a box obstacle on a corridor makes the next path detour around it,
// and removing it restores the original path.
func TestTiledNavMeshBoxObstacleDetourAndRestore(t *testing.T) {
	corridor := [][]core.Point{rect2D(0, 0, 120, 40)}
	tm := ai.BuildTiledNavMesh(corridor, 30)

	start := pt2(10, 20)
	goal := pt2(110, 20)

	origPath, ok := tm.FindPath(start, goal)
	if !ok {
		t.Fatal("expected initial corridor path")
	}
	if len(origPath) != 2 || !approxPt2D(origPath[0], start, 1e-3) || !approxPt2D(origPath[1], goal, 1e-3) {
		t.Fatalf("expected direct straight path, got %v", origPath)
	}
	origLen := pathLen2D(origPath)

	// Record tile versions before obstacle placement to prove untouched tiles do not rebuild.
	v0 := tm.TileVersion(ai.NavTileCoord{X: 0, Y: 0})
	v3 := tm.TileVersion(ai.NavTileCoord{X: 3, Y: 0})

	// Drop a box obstacle spanning the middle of the corridor (inside tile (1, 0) and (2, 0)).
	obsID, ok := tm.AddObstacle(ai.NavBoxObstacle(pt2(50, 10), pt2(70, 30)))
	if !ok || obsID == 0 {
		t.Fatal("AddObstacle failed")
	}

	// Untouched outer tiles must NOT have been rebuilt.
	if tm.TileVersion(ai.NavTileCoord{X: 0, Y: 0}) != v0 {
		t.Fatalf("untouched tile (0,0) version changed: got %d want %d", tm.TileVersion(ai.NavTileCoord{X: 0, Y: 0}), v0)
	}
	if tm.TileVersion(ai.NavTileCoord{X: 3, Y: 0}) != v3 {
		t.Fatalf("untouched tile (3,0) version changed: got %d want %d", tm.TileVersion(ai.NavTileCoord{X: 3, Y: 0}), v3)
	}
	// Interior of the carved obstacle must no longer be walkable.
	if tm.Contains(pt2(60, 20)) {
		t.Fatal("expected center of box obstacle to be carved unwalkable")
	}

	detour, ok := tm.FindPath(start, goal)
	if !ok {
		t.Fatal("expected detour path around box obstacle")
	}
	if len(detour) <= 2 {
		t.Fatalf("expected detour with corners around box, got %v", detour)
	}
	if pathLen2D(detour) <= origLen+1.0 {
		t.Fatalf("expected detour longer than straight path: detour=%v orig=%v", pathLen2D(detour), origLen)
	}
	for i := 1; i < len(detour); i++ {
		if segmentIntersectsBox2D(detour[i-1], detour[i], 50, 10, 70, 30) {
			t.Fatalf("detour segment %v -> %v passed through obstacle box", detour[i-1], detour[i])
		}
	}

	// Remove the obstacle and verify the original straight path is restored identically.
	if !tm.RemoveObstacle(obsID) {
		t.Fatal("RemoveObstacle returned false")
	}
	if !tm.Contains(pt2(60, 20)) {
		t.Fatal("expected corridor center to be walkable again after obstacle removal")
	}
	restored, ok := tm.FindPath(start, goal)
	if !ok {
		t.Fatal("expected restored path after obstacle removal")
	}
	if len(restored) != len(origPath) {
		t.Fatalf("restored path len=%d want %d (%v vs %v)", len(restored), len(origPath), restored, origPath)
	}
	for i := range origPath {
		if !approxPt2D(restored[i], origPath[i], 1e-3) {
			t.Fatalf("restored[%d]=%v != orig[%d]=%v", i, restored[i], i, origPath[i])
		}
	}
}

// Acceptance Criterion 3: Paths across tile borders match those of an untiled mesh for the same geometry.
func TestTiledNavMeshMatchesUntiledPathsAcrossBorders(t *testing.T) {
	// L-shaped hallway + central room with a hole/pillar formed by 4 surrounding quads.
	polys := [][]core.Point{
		// West wing
		rect2D(0, 0, 60, 40),
		// Central hall around pillar [80, 100] x [15, 45]
		rect2D(60, 0, 120, 15),   // south strip
		rect2D(60, 45, 120, 60),  // north strip
		rect2D(60, 15, 80, 45),   // west of pillar
		rect2D(100, 15, 120, 45), // east of pillar
		// East wing going north
		rect2D(120, 0, 160, 60),
		rect2D(120, 60, 160, 120),
	}

	untiled := ai.BuildNavMesh(polys)
	// Tile size 25 cuts right through rooms, pillar edges, and corridors.
	tiled := ai.BuildTiledNavMesh(polys, 25)

	cases := []struct {
		name  string
		start core.Point
		goal  core.Point
	}{
		{"straight_cross_tiles", pt2(5, 5), pt2(150, 5)},
		{"around_pillar_upper", pt2(20, 35), pt2(140, 40)},
		{"around_pillar_lower", pt2(20, 5), pt2(140, 20)},
		{"L_turn_long", pt2(10, 20), pt2(145, 110)},
		{"start_on_tile_border", pt2(25, 20), pt2(125, 80)},
		{"goal_on_tile_corner", pt2(10, 10), pt2(125, 50)},
		{"reverse_long", pt2(150, 115), pt2(5, 35)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			up, uok := untiled.FindPath(tc.start, tc.goal)
			tp, tok := tiled.FindPath(tc.start, tc.goal)
			if uok != tok {
				t.Fatalf("found mismatch: untiled=%v tiled=%v", uok, tok)
			}
			if !uok {
				return
			}
			uLen := pathLen2D(up)
			tLen := pathLen2D(tp)
			if math.Abs(uLen-tLen) > 1e-2 {
				t.Fatalf("path length mismatch: untiled=%.4f (%v) vs tiled=%.4f (%v)", uLen, up, tLen, tp)
			}
			if len(up) != len(tp) {
				t.Fatalf("waypoint count mismatch: untiled=%v tiled=%v", up, tp)
			}
			for i := range up {
				if !approxPt2D(up[i], tp[i], 1e-2) {
					t.Fatalf("waypoint[%d] mismatch: untiled=%v tiled=%v", i, up[i], tp[i])
				}
			}
		})
	}
}

func TestTiledNavMeshObstacleShapesAndModes(t *testing.T) {
	arena := [][]core.Point{rect2D(0, 0, 120, 80)}
	tm := ai.BuildTiledNavMesh(arena, 40)

	// 1. Circle obstacle.
	cid, ok := tm.AddObstacle(ai.NavCircleObstacle(pt2(60, 40), 12))
	if !ok {
		t.Fatal("AddObstacle circle failed")
	}
	if tm.Contains(pt2(60, 40)) {
		t.Fatal("circle center should be unwalkable")
	}
	path, ok := tm.FindPath(pt2(10, 40), pt2(110, 40))
	if !ok || pathLen2D(path) <= 100.5 {
		t.Fatalf("expected detour around circle, got ok=%v len=%.2f path=%v", ok, pathLen2D(path), path)
	}
	tm.RemoveObstacle(cid)

	// 2. Concave L-shaped polygon obstacle.
	lPoly := []core.Point{
		pt2(50, 20), pt2(80, 20), pt2(80, 32),
		pt2(62, 32), pt2(62, 60), pt2(50, 60),
	}
	pid, ok := tm.AddObstacle(ai.NavPolygonObstacle(lPoly))
	if !ok {
		t.Fatal("AddObstacle concave polygon failed")
	}
	if tm.Contains(pt2(56, 26)) || tm.Contains(pt2(56, 50)) {
		t.Fatal("inside concave L-obstacle should be unwalkable")
	}
	// Pocket inside the L's concave nook must remain walkable!
	if !tm.Contains(pt2(72, 50)) {
		t.Fatal("nook outside concave L-obstacle should remain walkable")
	}
	if _, ok := tm.FindPath(pt2(10, 40), pt2(72, 50)); !ok {
		t.Fatal("expected path into concave nook")
	}
	tm.RemoveObstacle(pid)

	// 3. NavObstacleMark mode: mark a strip as NavFlagDisabled (blocked for DefaultNavFilter,
	// walkable for a filter that clears ExcludeFlags).
	markObs := ai.NavBoxObstacle(pt2(55, 0), pt2(65, 80))
	markObs.Mode = ai.NavObstacleMark
	mid, ok := tm.AddObstacle(markObs)
	if !ok {
		t.Fatal("AddObstacle mark failed")
	}
	if _, ok := tm.FindPath(pt2(10, 40), pt2(110, 40)); ok {
		t.Fatal("expected path blocked by full-width NavFlagDisabled mark obstacle")
	}
	// A filter that allows disabled polygons can still cross.
	allowAll := ai.NavFilter{IncludeFlags: ai.NavFlagAll}
	if _, ok := tm.FindPathWithFilter(pt2(10, 40), pt2(110, 40), allowAll); !ok {
		t.Fatal("expected unfiltered query to pass through marked polygon")
	}

	// UpdateObstacle to turn the strip into high-cost mud (walkable but expensive) with a gap.
	mudObs := ai.NavBoxObstacle(pt2(55, 10), pt2(65, 70))
	mudObs.Mode = ai.NavObstacleMark
	mudObs.Area = ai.NavAreaMud
	if !tm.UpdateObstacle(mid, mudObs) {
		t.Fatal("UpdateObstacle failed")
	}
	filter := ai.DefaultNavFilter()
	filter.SetAreaCost(ai.NavAreaMud, 20.0)
	detourMud, ok := tm.FindPathWithFilter(pt2(10, 40), pt2(110, 40), filter)
	if !ok {
		t.Fatal("expected path around high-cost mud zone")
	}
	// Verify path goes around the mud patch rather than straight through y=40.
	if len(detourMud) <= 2 {
		t.Fatalf("expected path to detour around high-cost mud, got %v", detourMud)
	}
}

func TestTiledNavMeshTileLifecycleAndInvalidation(t *testing.T) {
	tm := ai.NewTiledNavMesh(ai.TiledNavMeshConfig{TileSize: 32})

	var events []ai.NavTileChange
	lid := tm.AddTileListener(func(ev ai.NavTileChange) {
		events = append(events, ev)
	})

	// Add 3 horizontal tiles (0,0), (1,0), (2,0) plus an unrelated tile (0,5).
	t0 := ai.BakeNavTile(ai.NavTileCoord{X: 0, Y: 0}, 32, core.Point{}, []ai.NavPolygonInput{{Vertices: rect2D(0, 0, 32, 32)}})
	t1 := ai.BakeNavTile(ai.NavTileCoord{X: 1, Y: 0}, 32, core.Point{}, []ai.NavPolygonInput{{Vertices: rect2D(32, 0, 64, 32)}})
	t2 := ai.BakeNavTile(ai.NavTileCoord{X: 2, Y: 0}, 32, core.Point{}, []ai.NavPolygonInput{{Vertices: rect2D(64, 0, 96, 32)}})
	tFar := ai.BakeNavTile(ai.NavTileCoord{X: 0, Y: 5}, 32, core.Point{}, []ai.NavPolygonInput{{Vertices: rect2D(0, 160, 32, 192)}})

	if !tm.AddTile(ai.NavTileCoord{X: 0, Y: 0}, t0) {
		t.Fatal("AddTile (0,0) failed")
	}
	if tm.AddTile(ai.NavTileCoord{X: 0, Y: 0}, t0) {
		t.Fatal("AddTile on occupied coord should return false")
	}
	tm.AddTile(ai.NavTileCoord{X: 1, Y: 0}, t1)
	tm.AddTile(ai.NavTileCoord{X: 2, Y: 0}, t2)
	tm.AddTile(ai.NavTileCoord{X: 0, Y: 5}, tFar)

	res := tm.FindPathWithOptions(pt2(8, 16), pt2(88, 16), ai.PathOptions{})
	if !res.Found {
		t.Fatal("expected cross-tile path")
	}
	if !tm.IsPathValid(res) {
		t.Fatal("fresh path must be valid")
	}
	if !res.CrossesTile(ai.NavTileCoord{X: 1, Y: 0}) || res.CrossesTile(ai.NavTileCoord{X: 0, Y: 5}) {
		t.Fatalf("unexpected tile footprint: %v", res.Tiles)
	}

	var invalidated []ai.NavTileChange
	tm.WatchPath(res.TileCoords(), func(ch ai.NavTileChange) {
		invalidated = append(invalidated, ch)
	})

	// Mutating unrelated tile (0, 5) must NOT invalidate res or trigger its watcher.
	tm.AddObstacle(ai.NavBoxObstacle(pt2(4, 164), pt2(12, 172)))
	if !tm.IsPathValid(res) {
		t.Fatal("unrelated tile change invalidated path")
	}
	if len(invalidated) != 0 {
		t.Fatalf("unrelated tile triggered path watcher: %v", invalidated)
	}

	// Mutating tile (1, 0) along the path MUST invalidate res and trigger its watcher once.
	tm.AddObstacle(ai.NavBoxObstacle(pt2(44, 10), pt2(52, 22)))
	if tm.IsPathValid(res) {
		t.Fatal("path should be invalidated after crossed tile changed")
	}
	if len(invalidated) != 1 || invalidated[0].Coord != (ai.NavTileCoord{X: 1, Y: 0}) {
		t.Fatalf("expected single invalidation for (1,0), got %v", invalidated)
	}

	// Removing middle tile disconnects (0,0) from (2,0).
	if !tm.RemoveTile(ai.NavTileCoord{X: 1, Y: 0}) {
		t.Fatal("RemoveTile failed")
	}
	if _, ok := tm.FindPath(pt2(8, 16), pt2(88, 16)); ok {
		t.Fatal("expected disconnected path after removing middle tile")
	}

	// Re-adding tile (1,0) restores connectivity (and automatically applies the active obstacle on (1,0)!).
	if !tm.AddTile(ai.NavTileCoord{X: 1, Y: 0}, t1) {
		t.Fatal("re-AddTile failed")
	}
	if tm.Contains(pt2(48, 16)) {
		t.Fatal("re-added tile should automatically have active obstacle carved")
	}
	if _, ok := tm.FindPath(pt2(8, 16), pt2(88, 16)); !ok {
		t.Fatal("expected path restored after re-adding middle tile")
	}

	if !tm.RemoveTileListener(lid) {
		t.Fatal("RemoveTileListener failed")
	}
	if len(events) == 0 {
		t.Fatal("expected tile listener events")
	}
}

func TestTiledNavMeshBatchAndDeterminism(t *testing.T) {
	buildMesh := func(reverseOrder bool) *ai.TiledNavMesh {
		tm := ai.NewTiledNavMesh(ai.TiledNavMeshConfig{TileSize: 32})
		coords := []ai.NavTileCoord{{0, 0}, {1, 0}, {2, 0}, {0, 1}, {1, 1}, {2, 1}}
		if reverseOrder {
			slices.Reverse(coords)
		}
		tm.Batch(func() {
			for _, c := range coords {
				x0, y0 := float32(c.X*32), float32(c.Y*32)
				tm.BakeTile(c, []ai.NavPolygonInput{{Vertices: rect2D(x0, y0, x0+32, y0+32)}})
			}
			tm.AddObstacle(ai.NavBoxObstacle(pt2(28, 16), pt2(44, 40)))
			tm.AddObstacle(ai.NavCircleObstacle(pt2(64, 32), 8))
		})
		return tm
	}

	m1 := buildMesh(false)
	m2 := buildMesh(true)

	// In Batch mode, each tile is built once (version == 1).
	for _, c := range m1.TileCoords() {
		if v := m1.TileVersion(c); v != 1 {
			t.Fatalf("expected batched tile %v version=1, got %d", c, v)
		}
	}

	p1, ok1 := m1.FindPath(pt2(4, 4), pt2(92, 60))
	p2, ok2 := m2.FindPath(pt2(4, 4), pt2(92, 60))
	if !ok1 || !ok2 {
		t.Fatalf("expected paths on both meshes: ok1=%v ok2=%v", ok1, ok2)
	}
	if len(p1) != len(p2) {
		t.Fatalf("non-deterministic path lengths: %v vs %v", p1, p2)
	}
	for i := range p1 {
		if p1[i] != p2[i] {
			t.Fatalf("non-deterministic waypoint[%d]: %v vs %v", i, p1[i], p2[i])
		}
	}
}

func TestTiledNavMeshSpatialQueriesAndRaycast(t *testing.T) {
	tm := ai.BuildTiledNavMesh([][]core.Point{rect2D(0, 0, 160, 160)}, 32)
	tm.AddObstacle(ai.NavBoxObstacle(pt2(70, 70), pt2(90, 90)))

	// NearestPoint outside obstacle snaps to obstacle edge.
	pt, ref, ok := tm.NearestPoint(pt2(80, 80), pt2(20, 20))
	if !ok || !ref.Valid() {
		t.Fatal("NearestPoint inside obstacle failed")
	}
	if math.Abs(float64(pt.X-70)) > 0.1 && math.Abs(float64(pt.X-90)) > 0.1 &&
		math.Abs(float64(pt.Y-70)) > 0.1 && math.Abs(float64(pt.Y-90)) > 0.1 {
		t.Fatalf("expected snapped point on obstacle boundary, got %v", pt)
	}

	// Unbounded NearestPolygon far away finds corner of mesh via coarse tile BVH.
	farRef, farPt, ok := tm.NearestPolygon(pt2(250, 250))
	if !ok || !farRef.Valid() || !approxPt2D(farPt, pt2(160, 160), 1e-2) {
		t.Fatalf("NearestPolygon far failed: ok=%v ref=%v pt=%v", ok, farRef, farPt)
	}

	// Raycast across multiple tiles unobstructed vs obstructed by carved obstacle.
	hitClear, obsClear := tm.Raycast(pt2(10, 20), pt2(150, 20))
	if obsClear || hitClear.Hit || hitClear.T != 1 {
		t.Fatalf("expected clear raycast across tiles, got %+v", hitClear)
	}
	hitBlocked, obsBlocked := tm.Raycast(pt2(10, 80), pt2(150, 80))
	if !obsBlocked || !hitBlocked.Hit || math.Abs(float64(hitBlocked.Point.X-70)) > 0.2 {
		t.Fatalf("expected raycast to hit box at x=70, got %+v", hitBlocked)
	}
}

func buildLargeTiledMesh2D(gridN int, tileSize float32) *ai.TiledNavMesh {
	tm := ai.NewTiledNavMesh(ai.TiledNavMeshConfig{TileSize: tileSize})
	tm.Batch(func() {
		for tx := range gridN {
			for ty := range gridN {
				x0 := float32(tx) * tileSize
				y0 := float32(ty) * tileSize
				// 4 quads per tile (1600 polygons for 20x20 tiles).
				midX := x0 + tileSize*0.5
				midY := y0 + tileSize*0.5
				tm.BakeTile(ai.NavTileCoord{X: int32(tx), Y: int32(ty)}, []ai.NavPolygonInput{
					{Vertices: rect2D(x0, y0, midX, midY)},
					{Vertices: rect2D(midX, y0, x0+tileSize, midY)},
					{Vertices: rect2D(x0, midY, midX, y0+tileSize)},
					{Vertices: rect2D(midX, midY, x0+tileSize, y0+tileSize)},
				})
			}
		}
	})
	return tm
}

// Acceptance Criterion 2: Rebuilding one tile of a large mesh stays well within a desktop frame budget.
func TestTiledNavMeshSingleTileRebuildBudget(t *testing.T) {
	const tileSize = 32
	tm := buildLargeTiledMesh2D(20, tileSize) // 400 tiles, 1600 polygons

	// Warm up and measure rebuilding one central tile with an obstacle.
	cx := float32(10*tileSize + 16)
	cy := float32(10*tileSize + 16)
	id, ok := tm.AddObstacle(ai.NavBoxObstacle(pt2(cx-4, cy-4), pt2(cx+4, cy+4)))
	if !ok {
		t.Fatal("AddObstacle failed")
	}

	start := time.Now()
	const iters = 20
	for i := range iters {
		shift := float32(i%3) * 0.5
		tm.UpdateObstacle(id, ai.NavBoxObstacle(pt2(cx-4+shift, cy-4), pt2(cx+4+shift, cy+4)))
	}
	avg := time.Since(start) / iters
	// Desktop 60 FPS frame budget is 16.6ms; single tile rebuild should be far below it.
	if avg > 16*time.Millisecond {
		t.Fatalf("single tile rebuild averaged %v, exceeding 16ms frame budget", avg)
	}
}

func BenchmarkTiledNavMeshRebuildSingleTile(b *testing.B) {
	const tileSize = 32
	tm := buildLargeTiledMesh2D(20, tileSize) // 400 tiles, 1600 polygons
	cx := float32(10*tileSize + 16)
	cy := float32(10*tileSize + 16)
	id, _ := tm.AddObstacle(ai.NavBoxObstacle(pt2(cx-4, cy-4), pt2(cx+4, cy+4)))
	obsA := ai.NavBoxObstacle(pt2(cx-4, cy-4), pt2(cx+4, cy+4))
	obsB := ai.NavBoxObstacle(pt2(cx-3, cy-3), pt2(cx+5, cy+5))

	b.ResetTimer()
	for i := range b.N {
		if i&1 == 0 {
			tm.UpdateObstacle(id, obsA)
		} else {
			tm.UpdateObstacle(id, obsB)
		}
	}
}

func BenchmarkTiledNavMeshFindPolygon(b *testing.B) {
	const tileSize = 32
	tm := buildLargeTiledMesh2D(20, tileSize)
	query := pt2(10*tileSize+12, 10*tileSize+12)

	b.ResetTimer()
	for range b.N {
		if _, ok := tm.FindPolygon(query); !ok {
			b.Fatal("FindPolygon missed")
		}
	}
}

func BenchmarkTiledNavMeshFindPath(b *testing.B) {
	const tileSize = 32
	tm := buildLargeTiledMesh2D(20, tileSize)
	start := pt2(8, 8)
	goal := pt2(19*tileSize+24, 19*tileSize+24)

	b.ResetTimer()
	for range b.N {
		if _, ok := tm.FindPath(start, goal); !ok {
			b.Fatal("FindPath missed")
		}
	}
}

func TestTiledNavMeshConcurrentNearestPolygonRaceFree(t *testing.T) {
	tm := buildLargeTiledMesh2D(6, 32)
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := range 32 {
				q := pt2(float32(-20+id*25+i), float32(-10+i*6))
				if _, _, ok := tm.NearestPolygon(q); !ok {
					t.Errorf("goroutine %d: NearestPolygon(%v) failed", id, q)
					return
				}
			}
		}(g)
	}
	wg.Wait()
}

func TestTiledNavMeshTightenRespectsCheapRoadCost(t *testing.T) {
	// L-shaped cheap road around a default-cost square:
	// Road goes (0,0)-(32,10), (22,10)-(32,32), (0,22)-(22,32); default fills (0,10)-(22,22).
	polys := []ai.NavPolygonInput{
		{Vertices: rect2D(0, 0, 32, 10), Area: ai.NavAreaRoad, Flags: ai.NavFlagWalk},
		{Vertices: rect2D(22, 10, 32, 32), Area: ai.NavAreaRoad, Flags: ai.NavFlagWalk},
		{Vertices: rect2D(0, 22, 22, 32), Area: ai.NavAreaRoad, Flags: ai.NavFlagWalk},
		{Vertices: rect2D(0, 10, 22, 22), Area: ai.NavAreaDefault, Flags: ai.NavFlagWalk},
	}
	tm := ai.BuildTiledNavMeshWithAreas(polys, 16)
	filter := ai.DefaultNavFilter()
	// Only NavAreaRoad is set in AreaCosts (< 1.0); NavAreaDefault has implicit cost 1.0.
	filter.SetAreaCost(ai.NavAreaRoad, 0.2)

	res := tm.FindPathWithFilterAndOptions(pt2(4, 5), pt2(4, 27), filter, ai.PathOptions{})
	if !res.Found {
		t.Fatal("expected path along cheap road")
	}
	// If tightening erroneously shortcut across implicit-cost NavAreaDefault (1.0),
	// the path would be a straight 2-point segment (4,5)->(4,27) through x=4, y=16.
	if len(res.Path) <= 2 {
		t.Fatalf("tightenTiledPath2D erroneously shortcut across higher-cost implicit NavAreaDefault: %v", res.Path)
	}
	for _, p := range res.Path {
		if p.X < 21 && p.Y > 11 && p.Y < 21 {
			t.Fatalf("path entered expensive default interior at %v: %v", p, res.Path)
		}
	}
}

func TestTiledNavMeshOversizedObstacleAndExtremeCoordsSafe(t *testing.T) {
	tm := ai.BuildTiledNavMesh([][]core.Point{rect2D(0, 0, 32, 32)}, 16)
	// Huge obstacle spanning far more than maxNavObstacleTiles2D must be rejected cleanly.
	if id, ok := tm.AddObstacle(ai.NavBoxObstacle(pt2(-1e9, -1e9), pt2(1e9, 1e9))); ok || id != 0 {
		t.Fatalf("AddObstacle accepted oversized box: id=%d ok=%v", id, ok)
	}
	if tm.ObstacleCount() != 0 {
		t.Fatalf("ObstacleCount = %d after rejected obstacle, want 0", tm.ObstacleCount())
	}
	// Extreme TileCoordAt and TileBounds must not panic or overflow int32.
	c := tm.TileCoordAt(pt2(float32(math.MaxFloat32/2), float32(-math.MaxFloat32/2)))
	_, _ = tm.TileBounds(c)
	_, _ = tm.TileBounds(ai.NavTileCoord{X: math.MaxInt32, Y: math.MaxInt32})
}
