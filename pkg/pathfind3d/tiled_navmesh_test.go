package pathfind3d_test

import (
	"context"
	"math"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/pathfind"
	"github.com/nsgo-dev/game/pkg/pathfind3d"
	"github.com/nsgo-dev/game/pkg/world3d"
)

func v3(x, y, z float32) math3d.Vec3 {
	return math3d.Vec3{X: x, Y: y, Z: z}
}

// tiledQuadXZ builds a horizontal CCW-from-+Y quad on plane y.
func tiledQuadXZ(x0, z0, x1, z1, y float32) []math3d.Vec3 {
	return []math3d.Vec3{v3(x0, y, z0), v3(x0, y, z1), v3(x1, y, z1), v3(x1, y, z0)}
}

func pathLen3D(pts []math3d.Vec3) float64 {
	var total float64
	for i := 1; i < len(pts); i++ {
		total += float64(pts[i].Dist(pts[i-1]))
	}
	return total
}

func segmentIntersectsBox3D(a, b math3d.Vec3, minX, minZ, maxX, maxZ float64) bool {
	const shrink = 1e-2
	x0, z0 := minX+shrink, minZ+shrink
	x1, z1 := maxX-shrink, maxZ-shrink
	for i := 0; i <= 32; i++ {
		t := float64(i) / 32.0
		px := float64(a.X) + (float64(b.X)-float64(a.X))*t
		pz := float64(a.Z) + (float64(b.Z)-float64(a.Z))*t
		if px > x0 && px < x1 && pz > z0 && pz < z1 {
			return true
		}
	}
	return false
}

// Acceptance Criterion 1 (3D): Dropping a box obstacle on a corridor makes the next path detour around it,
// and removing it restores the original path.
func TestTiledNavMesh3DBoxObstacleDetourAndRestore(t *testing.T) {
	corridor := [][]math3d.Vec3{tiledQuadXZ(0, 0, 120, 40, 0)}
	tm := pathfind3d.BuildTiledNavMesh(corridor, math3d.DegToRad(45), 30)

	start := v3(10, 0, 20)
	goal := v3(110, 0, 20)

	origPath, ok := tm.FindPath(start, goal)
	if !ok {
		t.Fatal("expected initial 3D corridor path")
	}
	if len(origPath) != 2 || !origPath[0].ApproxEqual(start, 1e-3) || !origPath[1].ApproxEqual(goal, 1e-3) {
		t.Fatalf("expected direct straight 3D path, got %v", origPath)
	}
	origLen := pathLen3D(origPath)

	v0 := tm.TileVersion(world3d.ChunkCoord{X: 0, Z: 0})
	v3Ver := tm.TileVersion(world3d.ChunkCoord{X: 3, Z: 0})

	obsID, ok := tm.AddObstacle(pathfind3d.NavBoxObstacle(v3(50, -1, 10), v3(70, 3, 30)))
	if !ok || obsID == 0 {
		t.Fatal("AddObstacle 3D failed")
	}
	if tm.TileVersion(world3d.ChunkCoord{X: 0, Z: 0}) != v0 {
		t.Fatal("untouched tile (0,0) was rebuilt")
	}
	if tm.TileVersion(world3d.ChunkCoord{X: 3, Z: 0}) != v3Ver {
		t.Fatal("untouched tile (3,0) was rebuilt")
	}
	if tm.Contains(v3(60, 0, 20)) {
		t.Fatal("expected box interior to be carved unwalkable")
	}

	detour, ok := tm.FindPath(start, goal)
	if !ok {
		t.Fatal("expected 3D detour path around box")
	}
	if len(detour) <= 2 || pathLen3D(detour) <= origLen+1.0 {
		t.Fatalf("expected longer detour around box, got %v", detour)
	}
	for i := 1; i < len(detour); i++ {
		if segmentIntersectsBox3D(detour[i-1], detour[i], 50, 10, 70, 30) {
			t.Fatalf("detour segment %v -> %v entered box obstacle", detour[i-1], detour[i])
		}
	}

	if !tm.RemoveObstacle(obsID) {
		t.Fatal("RemoveObstacle failed")
	}
	restored, ok := tm.FindPath(start, goal)
	if !ok {
		t.Fatal("expected restored 3D path")
	}
	if len(restored) != len(origPath) {
		t.Fatalf("restored path len=%d want %d", len(restored), len(origPath))
	}
	for i := range origPath {
		if !restored[i].ApproxEqual(origPath[i], 1e-3) {
			t.Fatalf("restored[%d]=%v != orig[%d]=%v", i, restored[i], i, origPath[i])
		}
	}
}

// Acceptance Criterion 3 (3D): Paths across tile borders match those of an untiled 3D NavMesh,
// including flat corridors, rooms with pillars, and sloped ramps.
func TestTiledNavMesh3DMatchesUntiledAcrossBordersAndRamps(t *testing.T) {
	// Ground floor [0, 40] x [0, 40] -> sloped ramp [40, 80] rising from y=0 to y=10 ->
	// upper deck [80, 140] x [0, 40] at y=10 with a pillar hole [95, 115] x [12, 28].
	ramp := []math3d.Vec3{
		v3(40, 0, 0),
		v3(40, 0, 40),
		v3(80, 10, 40),
		v3(80, 10, 0),
	}
	polys := [][]math3d.Vec3{
		tiledQuadXZ(0, 0, 40, 40, 0),
		ramp,
		tiledQuadXZ(80, 0, 140, 12, 10),
		tiledQuadXZ(80, 28, 140, 40, 10),
		tiledQuadXZ(80, 12, 95, 28, 10),
		tiledQuadXZ(115, 12, 140, 28, 10),
	}

	untiled := pathfind3d.BuildNavMesh(polys, math3d.DegToRad(45))
	// Tile size 25 splits the floor, the sloped ramp, and the upper deck across borders!
	tiled := pathfind3d.BuildTiledNavMesh(polys, math3d.DegToRad(45), 25)

	cases := []struct {
		name  string
		start math3d.Vec3
		goal  math3d.Vec3
	}{
		{"flat_to_ramp_to_upper", v3(10, 0, 5), v3(130, 10, 5)},
		{"up_ramp_and_around_pillar_south", v3(10, 0, 16), v3(130, 10, 16)},
		{"on_tile_border_across_ramp", v3(25, 0, 35), v3(125, 10, 35)},
		{"down_ramp_reverse", v3(135, 10, 20), v3(5, 0, 20)},
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
			uLen := pathLen3D(up)
			tLen := pathLen3D(tp)
			if math.Abs(uLen-tLen) > 2e-2 {
				t.Fatalf("3D path length mismatch: untiled=%.4f (%v) vs tiled=%.4f (%v)", uLen, up, tLen, tp)
			}
			if len(up) != len(tp) {
				t.Fatalf("3D waypoint count mismatch: untiled=%v tiled=%v", up, tp)
			}
			for i := range up {
				if !up[i].ApproxEqual(tp[i], 2e-2) {
					t.Fatalf("3D waypoint[%d] mismatch: untiled=%v tiled=%v", i, up[i], tp[i])
				}
			}
		})
	}
}

func TestTiledNavMesh3DMultiLevelVerticalSeparation(t *testing.T) {
	// Ground floor at y=0 and overpass bridge at y=8 crossing tile boundary x=32.
	polys := [][]math3d.Vec3{
		tiledQuadXZ(0, 0, 64, 32, 0),  // ground floor
		tiledQuadXZ(0, 10, 64, 22, 8), // upper bridge
	}
	tm := pathfind3d.BuildTiledNavMesh(polys, math3d.DegToRad(45), 32)

	// Place a box obstacle ONLY on the ground floor (y in [-1, 3]).
	tm.AddObstacle(pathfind3d.NavBoxObstacle(v3(26, -1, 0), v3(38, 3, 32)))

	// Ground floor should now be completely blocked.
	if _, ok := tm.FindPath(v3(8, 0, 16), v3(56, 0, 16)); ok {
		t.Fatal("ground floor should be blocked by ground-level obstacle")
	}
	// Upper bridge at y=8 must remain completely clear and walkable!
	bridgePath, ok := tm.FindPath(v3(8, 8, 16), v3(56, 8, 16))
	if !ok {
		t.Fatal("upper bridge should remain unaffected by ground-floor obstacle")
	}
	for _, pt := range bridgePath {
		if math.Abs(float64(pt.Y-8)) > 1e-2 {
			t.Fatalf("expected bridge path at y=8, got %v", pt)
		}
	}
	// Ground floor must NEVER link directly to bridge.
	if _, ok := tm.FindPath(v3(8, 0, 16), v3(8, 8, 16)); ok {
		t.Fatal("ground and disconnected upper bridge should not connect")
	}
}

func TestTiledNavMesh3DCylinderPolygonAndMarkObstacles(t *testing.T) {
	tm := pathfind3d.BuildTiledNavMesh([][]math3d.Vec3{tiledQuadXZ(0, 0, 120, 80, 0)}, math3d.DegToRad(45), 40)

	// Cylinder obstacle.
	cid, ok := tm.AddObstacle(pathfind3d.NavCylinderObstacle(v3(60, 0, 40), 10, 4))
	if !ok {
		t.Fatal("AddObstacle cylinder failed")
	}
	if tm.Contains(v3(60, 0, 40)) {
		t.Fatal("cylinder interior should be unwalkable")
	}
	p, ok := tm.FindPath(v3(10, 0, 40), v3(110, 0, 40))
	if !ok || pathLen3D(p) <= 100.5 {
		t.Fatalf("expected detour around cylinder, got ok=%v len=%.2f", ok, pathLen3D(p))
	}
	tm.RemoveObstacle(cid)

	// Concave polygon obstacle.
	lPoly := []math3d.Vec3{
		v3(50, 0, 20), v3(80, 0, 20), v3(80, 0, 32),
		v3(62, 0, 32), v3(62, 0, 60), v3(50, 0, 60),
	}
	pid, ok := tm.AddObstacle(pathfind3d.NavPolygonObstacle(lPoly, -1, 5))
	if !ok {
		t.Fatal("AddObstacle polygon failed")
	}
	if tm.Contains(v3(56, 0, 26)) || !tm.Contains(v3(72, 0, 50)) {
		t.Fatal("concave polygon carve mismatch")
	}
	tm.RemoveObstacle(pid)

	// Mark mode (disabled).
	mark := pathfind3d.NavBoxObstacle(v3(55, -1, 0), v3(65, 4, 80))
	mark.Mode = pathfind3d.NavObstacleMark
	tm.AddObstacle(mark)
	if _, ok := tm.FindPath(v3(10, 0, 40), v3(110, 0, 40)); ok {
		t.Fatal("expected blocked path across marked disabled strip")
	}
}

func TestNavTileStreamerFollowsChunkStreaming(t *testing.T) {
	const tileSize = 32
	tm := pathfind3d.NewTiledNavMesh(pathfind3d.TiledNavMeshConfig{TileSize: tileSize})

	// Place a dynamic obstacle ahead in chunk (2, 0) BEFORE that chunk is even streamed in!
	tm.AddObstacle(pathfind3d.NavBoxObstacle(v3(2*tileSize+12, -1, 12), v3(2*tileSize+20, 4, 20)))

	var loaded, unloaded []world3d.ChunkCoord
	streamer, err := pathfind3d.NewNavTileStreamer(pathfind3d.NavTileStreamerConfig{
		Mesh:         tm,
		LoadRadius:   1,
		UnloadRadius: 1,
		Workers:      0, // synchronous for deterministic testing
		Bake: func(_ context.Context, coord world3d.ChunkCoord, bounds math3d.AABB) (*pathfind3d.NavTile, error) {
			poly := tiledQuadXZ(bounds.Min.X, bounds.Min.Z, bounds.Max.X, bounds.Max.Z, 0)
			return pathfind3d.BakeNavTile(coord, tileSize, tm.Origin(), []pathfind3d.NavPolygonInput{{Vertices: poly}}, math3d.DegToRad(45)), nil
		},
		OnLoad: func(c world3d.ChunkCoord, _ *pathfind3d.NavTile) {
			loaded = append(loaded, c)
		},
		OnUnload: func(c world3d.ChunkCoord) {
			unloaded = append(unloaded, c)
		},
	})
	if err != nil {
		t.Fatalf("NewNavTileStreamer failed: %v", err)
	}
	defer streamer.Close()

	// Anchor at chunk (0, 0): loads 3x3 grid [-1..1] x [-1..1] = 9 tiles.
	streamer.Update(world3d.WorldPos64{X: 16, Z: 16})
	if tm.TileCount() != 9 {
		t.Fatalf("expected 9 streamed tiles, got %d", tm.TileCount())
	}

	// Path across streamed chunks (-1,0) -> (0,0) -> (1,0) works and is watched.
	res := tm.FindPathWithOptions(v3(-16, 0, 16), v3(48, 0, 16), pathfind.PathOptions{})
	if !res.Found {
		t.Fatal("expected cross-chunk path across streamed tiles")
	}
	invalidated := false
	tm.WatchPath(res.TileCoords(), func(_ pathfind3d.NavTileChange) {
		invalidated = true
	})

	// Move player to chunk (2, 0): unloads x=-1..0 chunks and loads x=1..3 chunks!
	streamer.Update(world3d.WorldPos64{X: 2*tileSize + 16, Z: 16})

	if tm.HasTile(world3d.ChunkCoord{X: -1, Z: 0}) || tm.HasTile(world3d.ChunkCoord{X: 0, Z: 0}) {
		t.Fatal("expected distant chunks (-1,0) and (0,0) to unload")
	}
	if !tm.HasTile(world3d.ChunkCoord{X: 2, Z: 0}) || !tm.HasTile(world3d.ChunkCoord{X: 3, Z: 0}) {
		t.Fatal("expected new chunks (2,0) and (3,0) to load")
	}
	if !invalidated || tm.IsPathValid(res) {
		t.Fatal("expected path crossing unloaded chunk to be invalidated")
	}
	// Pre-existing dynamic obstacle in chunk (2, 0) must be automatically carved on stream-in!
	if tm.Contains(v3(2*tileSize+16, 0, 16)) {
		t.Fatal("expected active obstacle to be carved automatically onto newly streamed tile (2,0)")
	}
	// New path crossing (1,0) -> (2,0) -> (3,0) detours around the obstacle in (2,0).
	newPath, ok := tm.FindPath(v3(1*tileSize+16, 0, 16), v3(3*tileSize+16, 0, 16))
	if !ok || len(newPath) <= 2 {
		t.Fatalf("expected cross-chunk detour path in streamed region, got ok=%v path=%v", ok, newPath)
	}
	if len(loaded) == 0 || len(unloaded) == 0 {
		t.Fatalf("expected both load and unload callbacks, got loaded=%d unloaded=%d", len(loaded), len(unloaded))
	}

	// Close unloads all remaining tiles cleanly.
	streamer.Close()
	if tm.TileCount() != 0 {
		t.Fatalf("expected 0 tiles after Close, got %d", tm.TileCount())
	}
}

func TestNavTileStreamerAsyncWorkers(t *testing.T) {
	const tileSize = 32
	tm := pathfind3d.NewTiledNavMesh(pathfind3d.TiledNavMeshConfig{TileSize: tileSize})

	streamer, err := pathfind3d.NewNavTileStreamer(pathfind3d.NavTileStreamerConfig{
		Mesh:         tm,
		LoadRadius:   1,
		UnloadRadius: 2,
		Workers:      2,
		Bake: func(_ context.Context, coord world3d.ChunkCoord, bounds math3d.AABB) (*pathfind3d.NavTile, error) {
			poly := tiledQuadXZ(bounds.Min.X, bounds.Min.Z, bounds.Max.X, bounds.Max.Z, 0)
			return pathfind3d.BakeNavTile(coord, tileSize, tm.Origin(), []pathfind3d.NavPolygonInput{{Vertices: poly}}, math3d.DegToRad(45)), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer streamer.Close()

	deadline := time.Now().Add(2 * time.Second)
	for tm.TileCount() < 9 && time.Now().Before(deadline) {
		streamer.Update(world3d.WorldPos64{X: 16, Z: 16})
		time.Sleep(2 * time.Millisecond)
	}
	if tm.TileCount() != 9 {
		t.Fatalf("expected 9 asynchronously streamed tiles, got %d", tm.TileCount())
	}
	if _, ok := tm.FindPath(v3(-16, 0, -16), v3(48, 0, 48)); !ok {
		t.Fatal("expected diagonal cross-chunk path on async streamed tiles")
	}
}

func TestTiledNavMesh3DDeterminism(t *testing.T) {
	build := func(rev bool) *pathfind3d.TiledNavMesh {
		tm := pathfind3d.NewTiledNavMesh(pathfind3d.TiledNavMeshConfig{TileSize: 32})
		coords := []world3d.ChunkCoord{{X: 0, Z: 0}, {X: 1, Z: 0}, {X: 2, Z: 0}, {X: 0, Z: 1}, {X: 1, Z: 1}, {X: 2, Z: 1}}
		if rev {
			slices.Reverse(coords)
		}
		tm.Batch(func() {
			for _, c := range coords {
				x0, z0 := float32(c.X*32), float32(c.Z*32)
				tm.BakeTile(c, []pathfind3d.NavPolygonInput{{Vertices: tiledQuadXZ(x0, z0, x0+32, z0+32, 0)}})
			}
			tm.AddObstacle(pathfind3d.NavBoxObstacle(v3(28, -1, 16), v3(44, 4, 40)))
			tm.AddObstacle(pathfind3d.NavCylinderObstacle(v3(64, 0, 32), 8, 4))
		})
		return tm
	}

	m1 := build(false)
	m2 := build(true)
	p1, ok1 := m1.FindPath(v3(4, 0, 4), v3(92, 0, 60))
	p2, ok2 := m2.FindPath(v3(4, 0, 4), v3(92, 0, 60))
	if !ok1 || !ok2 || len(p1) != len(p2) {
		t.Fatalf("determinism mismatch: ok1=%v ok2=%v len1=%d len2=%d", ok1, ok2, len(p1), len(p2))
	}
	for i := range p1 {
		if p1[i] != p2[i] {
			t.Fatalf("non-deterministic 3D waypoint[%d]: %v vs %v", i, p1[i], p2[i])
		}
	}
}

func buildLargeTiledMesh3D(gridN int, tileSize float32) *pathfind3d.TiledNavMesh {
	tm := pathfind3d.NewTiledNavMesh(pathfind3d.TiledNavMeshConfig{TileSize: tileSize})
	tm.Batch(func() {
		for tx := range gridN {
			for tz := range gridN {
				x0 := float32(tx) * tileSize
				z0 := float32(tz) * tileSize
				midX := x0 + tileSize*0.5
				midZ := z0 + tileSize*0.5
				tm.BakeTile(world3d.ChunkCoord{X: int32(tx), Z: int32(tz)}, []pathfind3d.NavPolygonInput{
					{Vertices: tiledQuadXZ(x0, z0, midX, midZ, 0)},
					{Vertices: tiledQuadXZ(midX, z0, x0+tileSize, midZ, 0)},
					{Vertices: tiledQuadXZ(x0, midZ, midX, z0+tileSize, 0)},
					{Vertices: tiledQuadXZ(midX, midZ, x0+tileSize, z0+tileSize, 0)},
				})
			}
		}
	})
	return tm
}

func TestTiledNavMesh3DSingleTileRebuild(t *testing.T) {
	const tileSize = 32
	tm := buildLargeTiledMesh3D(20, tileSize) // 400 tiles, 1600 polygons
	cx := float32(10*tileSize + 16)
	cz := float32(10*tileSize + 16)
	id, ok := tm.AddObstacle(pathfind3d.NavBoxObstacle(v3(cx-4, -1, cz-4), v3(cx+4, 3, cz+4)))
	if !ok {
		t.Fatal("AddObstacle failed")
	}

	const iters = 20
	for i := range iters {
		shift := float32(i%3) * 0.5
		tm.UpdateObstacle(id, pathfind3d.NavBoxObstacle(v3(cx-4+shift, -1, cz-4), v3(cx+4+shift, 3, cz+4)))
	}
}

func BenchmarkTiledNavMesh3DRebuildSingleTile(b *testing.B) {
	const tileSize = 32
	tm := buildLargeTiledMesh3D(20, tileSize)
	cx := float32(10*tileSize + 16)
	cz := float32(10*tileSize + 16)
	id, _ := tm.AddObstacle(pathfind3d.NavBoxObstacle(v3(cx-4, -1, cz-4), v3(cx+4, 3, cz+4)))
	obsA := pathfind3d.NavBoxObstacle(v3(cx-4, -1, cz-4), v3(cx+4, 3, cz+4))
	obsB := pathfind3d.NavBoxObstacle(v3(cx-3, -1, cz-3), v3(cx+5, 3, cz+5))

	b.ResetTimer()
	for i := range b.N {
		if i&1 == 0 {
			tm.UpdateObstacle(id, obsA)
		} else {
			tm.UpdateObstacle(id, obsB)
		}
	}
}

func BenchmarkTiledNavMesh3DFindPolygon(b *testing.B) {
	const tileSize = 32
	tm := buildLargeTiledMesh3D(20, tileSize)
	query := v3(10*tileSize+12, 0, 10*tileSize+12)

	b.ResetTimer()
	for range b.N {
		if _, ok := tm.FindPolygon(query); !ok {
			b.Fatal("FindPolygon missed")
		}
	}
}

func BenchmarkTiledNavMesh3DFindPath(b *testing.B) {
	const tileSize = 32
	tm := buildLargeTiledMesh3D(20, tileSize)
	start := v3(8, 0, 8)
	goal := v3(19*tileSize+24, 0, 19*tileSize+24)

	b.ResetTimer()
	for range b.N {
		if _, ok := tm.FindPath(start, goal); !ok {
			b.Fatal("FindPath missed")
		}
	}
}

func TestTiledNavMesh3DConcurrentNearestPolygonRaceFree(t *testing.T) {
	tm := buildLargeTiledMesh3D(6, 32)
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := range 32 {
				q := v3(float32(-20+id*25+i), 0, float32(-10+i*6))
				if _, _, ok := tm.NearestPolygon(q); !ok {
					t.Errorf("goroutine %d: NearestPolygon(%v) failed", id, q)
					return
				}
			}
		}(g)
	}
	wg.Wait()
}

func TestTiledNavMesh3DRaycastDiagonalCornerBridge(t *testing.T) {
	// 2x2 grid of 16x16 tiles meeting at corner (16, 0, 16).
	tm := pathfind3d.BuildTiledNavMesh([][]math3d.Vec3{tiledQuadXZ(0, 0, 32, 32, 0)}, math.Pi/4, 16)
	hit, blocked := tm.Raycast(v3(2, 0, 2), v3(30, 0, 30))
	if blocked || hit.Hit {
		t.Fatalf("diagonal 3D raycast across 4-tile corner (16,0,16) falsely blocked: %+v", hit)
	}
}

func TestTiledNavMesh3DSlopedRampIgnoresFloatingObstacle(t *testing.T) {
	// Sloped ramp rising linearly from y=0 at x=0 to y=10 at x=20 (slope ~26.6 deg < 45 deg).
	ramp := []math3d.Vec3{
		v3(0, 0, 0),
		v3(0, 0, 8),
		v3(20, 10, 8),
		v3(20, 10, 0),
	}
	tm := pathfind3d.BuildTiledNavMesh([][]math3d.Vec3{ramp}, math.Pi/4, 32)
	// Place a floating box at X in [2,6], Y in [7,10] — well above the ramp surface Y in [1,3] there,
	// even though its Y span overlaps the ramp polygon's overall AABB Y in [0,10].
	if _, ok := tm.AddObstacle(pathfind3d.NavBoxObstacle(v3(2, 7, 1), v3(6, 10, 7))); !ok {
		t.Fatal("AddObstacle failed")
	}
	// Surface at (4, 2, 4) directly beneath the floating box must remain walkable and uncarved.
	if !tm.Contains(v3(4, 2, 4)) {
		t.Fatal("floating obstacle above low end of sloped ramp falsely carved the ramp")
	}
	path, ok := tm.FindPath(v3(1, 0.5, 4), v3(19, 9.5, 4))
	if !ok || len(path) != 2 {
		t.Fatalf("expected direct uncarved path up ramp, got ok=%v path=%v", ok, path)
	}
}

func TestTiledNavMesh3DTightenRespectsCheapRoadAndOversizedObstacleSafe(t *testing.T) {
	polys := []pathfind3d.NavPolygonInput{
		{Vertices: tiledQuadXZ(0, 0, 32, 10, 0), Area: pathfind3d.NavAreaRoad, Flags: pathfind3d.NavFlagWalk},
		{Vertices: tiledQuadXZ(22, 10, 32, 32, 0), Area: pathfind3d.NavAreaRoad, Flags: pathfind3d.NavFlagWalk},
		{Vertices: tiledQuadXZ(0, 22, 22, 32, 0), Area: pathfind3d.NavAreaRoad, Flags: pathfind3d.NavFlagWalk},
		{Vertices: tiledQuadXZ(0, 10, 22, 22, 0), Area: pathfind3d.NavAreaDefault, Flags: pathfind3d.NavFlagWalk},
	}
	tm := pathfind3d.BuildTiledNavMeshWithAreas(polys, math.Pi/4, 16)
	filter := pathfind3d.DefaultNavFilter()
	filter.SetAreaCost(pathfind3d.NavAreaRoad, 0.2)

	res := tm.FindPathWithFilterAndOptions(v3(4, 0, 5), v3(4, 0, 27), filter, pathfind.PathOptions{})
	if !res.Found {
		t.Fatal("expected 3D path along cheap road")
	}
	if len(res.Path) <= 2 {
		t.Fatalf("tightenTiledPath3D erroneously shortcut across higher-cost implicit NavAreaDefault: %v", res.Path)
	}
	if id, ok := tm.AddObstacle(pathfind3d.NavBoxObstacle(v3(-1e9, -10, -1e9), v3(1e9, 10, 1e9))); ok || id != 0 {
		t.Fatalf("AddObstacle accepted oversized 3D box: id=%d ok=%v", id, ok)
	}
	if tm.ObstacleCount() != 0 {
		t.Fatalf("ObstacleCount = %d after rejected obstacle, want 0", tm.ObstacleCount())
	}
}
