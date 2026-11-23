package world3d_test

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/quality"
	"github.com/nsgo-dev/game/packages/game/world3d"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

const eps = 1e-4

func TestHeightmapLoadGrayAndInterpolation(t *testing.T) {
	// 5x5 grayscale ramp image where height = (col + 2*row) / 12 scaled by 12.
	img := image.NewGray16(image.Rect(0, 0, 5, 5))
	for r := range 5 {
		for c := range 5 {
			v := uint16(math.Round((float64(c+2*r) / 12.0) * 65535.0))
			img.SetGray16(c, r, color.Gray16{Y: v})
		}
	}

	hm := world3d.LoadHeightmapGray(img, 2.0, 12.0)
	hm.Origin = math3d.Vec3{X: -4, Y: 1, Z: -4}

	// Exact analytic plane: y = Origin.Y + ((x - Origin.X)/2) + 2*((z - Origin.Z)/2)
	//                       = 1 + 0.5*(x+4) + 1.0*(z+4)
	wantNormal := math3d.Vec3{X: -0.5, Y: 1, Z: -1.0}.Norm()

	samples := [][2]float32{
		{-4, -4},
		{4, 4},
		{-3.3, -3.7}, // inside Triangle 0 (fx+fz <= 1)
		{-2.2, -2.1}, // inside Triangle 1 (fx+fz > 1)
		{0.5, 1.8},
		{3.9, 3.9},
	}
	for _, s := range samples {
		x, z := s[0], s[1]
		gotY, ok := hm.HeightAt(x, z)
		if !ok {
			t.Fatalf("HeightAt(%v, %v) returned ok=false", x, z)
		}
		wantY := float32(1.0) + 0.5*(x+4) + 1.0*(z+4)
		if !math3d.ApproxEqual(gotY, wantY, 1e-3) {
			t.Fatalf("HeightAt(%v, %v) = %v, want %v", x, z, gotY, wantY)
		}
		gotN := hm.NormalAt(x, z)
		if !gotN.ApproxEqual(wantNormal, 1e-3) {
			t.Fatalf("NormalAt(%v, %v) = %v, want %v", x, z, gotN, wantNormal)
		}
	}

	// Out of bounds returns false for HeightAt and Vec3Up for NormalAt.
	if _, ok := hm.HeightAt(-4.1, 0); ok {
		t.Fatalf("expected out-of-bounds HeightAt to return false")
	}
	if _, ok := hm.HeightAt(0, 4.1); ok {
		t.Fatalf("expected out-of-bounds HeightAt to return false")
	}
	if n := hm.NormalAt(100, 100); !n.ApproxEqual(math3d.Vec3Up(), eps) {
		t.Fatalf("out-of-bounds NormalAt = %v, want Vec3Up", n)
	}
}

func TestHeightmapBarycentricDiagonalContinuity(t *testing.T) {
	// Single cell with saddle heights: v00=0, v10=2, v01=4, v11=0.
	hm := &world3d.Heightmap{
		Cols:        2,
		Rows:        2,
		CellSize:    1,
		HeightScale: 1,
		Heights: []float32{
			0, 2,
			4, 0,
		},
	}

	// Along diagonal fx + fz == 1, both triangles must agree continuously.
	yLeft, ok1 := hm.HeightAt(0.499, 0.5)
	yRight, ok2 := hm.HeightAt(0.501, 0.5)
	if !ok1 || !ok2 {
		t.Fatalf("expected both diagonal sides in bounds")
	}
	// Midpoint of v10 (2) and v01 (4) is 3.
	if !math3d.ApproxEqual(yLeft, 3, 1e-2) || !math3d.ApproxEqual(yRight, 3, 1e-2) {
		t.Fatalf("diagonal discontinuity: left=%v right=%v, want ~3", yLeft, yRight)
	}
	// Triangle 0 interior (0.25, 0.25): 0.5*0 + 0.25*2 + 0.25*4 = 1.5
	if y, _ := hm.HeightAt(0.25, 0.25); !math3d.ApproxEqual(y, 1.5, eps) {
		t.Fatalf("tri0 height = %v, want 1.5", y)
	}
	// Triangle 1 interior (0.75, 0.75): 0.25*2 + 0.25*4 + 0.5*0 = 1.5
	if y, _ := hm.HeightAt(0.75, 0.75); !math3d.ApproxEqual(y, 1.5, eps) {
		t.Fatalf("tri1 height = %v, want 1.5", y)
	}
}

func TestHeightmapRaycastHillsAndValleys(t *testing.T) {
	// 9x9 terrain with two hills separated by a valley:
	// - Near hill peak at col=2, row=4 (x=2, z=4), height=4
	// - Valley floor at col=4, row=4 (x=4, z=4), height=0
	// - Far hill peak at col=6, row=4 (x=6, z=4), height=6
	const cols, rows = 9, 9
	heights := make([]float32, cols*rows)
	for r := range rows {
		for c := range cols {
			d1 := float64((c-2)*(c-2) + (r-4)*(r-4))
			d2 := float64((c-6)*(c-6) + (r-4)*(r-4))
			h := 4.0*math.Exp(-d1/1.5) + 6.0*math.Exp(-d2/1.5)
			heights[r*cols+c] = float32(h)
		}
	}
	hm := &world3d.Heightmap{
		Cols:        cols,
		Rows:        rows,
		CellSize:    1.0,
		HeightScale: 1.0,
		Heights:     heights,
	}

	// Vertical ray down onto valley & hill slope matches HeightAt and NormalAt exactly.
	for _, pt := range [][2]float32{{2.3, 4.2}, {4.1, 3.8}, {5.7, 4.4}} {
		ray := math3d.Ray{
			Origin: math3d.Vec3{X: pt[0], Y: 20, Z: pt[1]},
			Dir:    math3d.Vec3{X: 0, Y: -2, Z: 0}, // intentionally non-unit to test Euclidean distance
		}
		hit, ok := hm.Raycast(ray, 50)
		if !ok {
			t.Fatalf("vertical ray at %v missed terrain", pt)
		}
		wantY, _ := hm.HeightAt(pt[0], pt[1])
		wantN := hm.NormalAt(pt[0], pt[1])
		if !math3d.ApproxEqual(hit.Point.Y, wantY, eps) {
			t.Fatalf("vertical hit Y = %v, want %v", hit.Point.Y, wantY)
		}
		if !math3d.ApproxEqual(hit.Distance, 20-wantY, eps) {
			t.Fatalf("vertical hit Distance = %v, want %v", hit.Distance, 20-wantY)
		}
		if !hit.Normal.ApproxEqual(wantN, eps) {
			t.Fatalf("vertical hit Normal = %v, want %v", hit.Normal, wantN)
		}
	}

	// Horizontal ray at Y=2 from X=-1 toward +X along Z=4 must hit the NEAR hill
	// (around X < 2) and NOT tunnel through it to the far hill!
	horiz := math3d.Ray{
		Origin: math3d.Vec3{X: -1, Y: 2, Z: 4},
		Dir:    math3d.Vec3{X: 1, Y: 0, Z: 0},
	}
	hit, ok := hm.Raycast(horiz, 20)
	if !ok {
		t.Fatalf("horizontal ray missed near hill")
	}
	if hit.Point.X >= 2.1 || hit.Point.X <= 0 {
		t.Fatalf("horizontal ray hit X = %v, expected near side of hill 1 in (0, 2.1)", hit.Point.X)
	}
	surfaceY, inBounds := hm.HeightAt(hit.Point.X, hit.Point.Z)
	if !inBounds || !math3d.ApproxEqual(surfaceY, hit.Point.Y, 1e-3) {
		t.Fatalf("hit point %v does not lie on terrain surface (HeightAt=%v)", hit.Point, surfaceY)
	}

	// Same ray with short maxDist must miss.
	if _, ok := hm.Raycast(horiz, 0.5); ok {
		t.Fatalf("expected ray with short maxDist to miss")
	}

	// Ray passing above all hills at Y=10 must miss.
	high := math3d.Ray{
		Origin: math3d.Vec3{X: -1, Y: 10, Z: 4},
		Dir:    math3d.Vec3{X: 1, Y: 0, Z: 0},
	}
	if _, ok := hm.Raycast(high, 30); ok {
		t.Fatalf("expected ray above terrain to miss")
	}
}

func TestHeightmapBuildMeshChunks(t *testing.T) {
	hm := &world3d.Heightmap{
		Cols:        5,
		Rows:        5,
		CellSize:    2,
		HeightScale: 1,
		Origin:      math3d.Vec3{X: 10, Y: 0, Z: 20},
		Heights:     make([]float32, 25),
	}
	for r := range 5 {
		for c := range 5 {
			hm.Heights[r*5+c] = float32(c) + 0.5*float32(r)
		}
	}

	// Build a 2x2 cell sub-chunk starting at (1, 2).
	mesh := hm.BuildMesh(1, 2, 2, 2)
	if len(mesh.Positions) != 9 || len(mesh.Normals) != 9 || len(mesh.UVs) != 9 {
		t.Fatalf("chunk vertex counts = (%d, %d, %d), want (9, 9, 9)",
			len(mesh.Positions), len(mesh.Normals), len(mesh.UVs))
	}
	if len(mesh.Indices) != 2*2*6 {
		t.Fatalf("chunk index count = %d, want 24", len(mesh.Indices))
	}

	// Verify every vertex matches HeightAt and UVs are normalized in [0, 1].
	for i, p := range mesh.Positions {
		y, ok := hm.HeightAt(p.X, p.Z)
		if !ok || !math3d.ApproxEqual(p.Y, y, eps) {
			t.Fatalf("vertex %d pos %v disagrees with HeightAt=%v (ok=%v)", i, p, y, ok)
		}
		if !math3d.ApproxEqual(mesh.Normals[i].Len(), 1, eps) || mesh.Normals[i].Y <= 0 {
			t.Fatalf("vertex %d invalid normal %v", i, mesh.Normals[i])
		}
		uv := mesh.UVs[i]
		if uv.X < 0 || uv.X > 1 || uv.Y < 0 || uv.Y > 1 {
			t.Fatalf("vertex %d UV out of [0,1]: %v", i, uv)
		}
	}

	// Verify every triangle has valid indices and CCW upward-facing (+Y) winding.
	for i := 0; i < len(mesh.Indices); i += 3 {
		i0, i1, i2 := mesh.Indices[i], mesh.Indices[i+1], mesh.Indices[i+2]
		if int(i0) >= len(mesh.Positions) || int(i1) >= len(mesh.Positions) || int(i2) >= len(mesh.Positions) {
			t.Fatalf("triangle %d index out of bounds", i/3)
		}
		v0, v1, v2 := mesh.Positions[i0], mesh.Positions[i1], mesh.Positions[i2]
		faceNormal := v1.Sub(v0).Cross(v2.Sub(v0))
		if faceNormal.Y <= 0 {
			t.Fatalf("triangle %d has downward winding normal %v", i/3, faceNormal)
		}
	}

	// Full terrain via BuildMesh(0, 0, 0, 0).
	full := hm.BuildMesh(0, 0, 0, 0)
	if len(full.Positions) != 25 || len(full.Indices) != 4*4*6 {
		t.Fatalf("full mesh = (%d verts, %d indices), want (25, 96)", len(full.Positions), len(full.Indices))
	}
}

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

	nm := world3d.BuildNavMesh(polys, math3d.DegToRad(45))
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

	nm := world3d.BuildNavMesh([][]math3d.Vec3{
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

func TestQualityLevelForFollowsTheGamePreset(t *testing.T) {
	cases := map[quality.Level]world3d.QualityLevel{
		quality.Low:       world3d.QualityLow,
		quality.Medium:    world3d.QualityMedium,
		quality.High:      world3d.QualityHigh,
		quality.Level(42): world3d.QualityMedium,
	}
	for in, want := range cases {
		if got := world3d.QualityLevelFor(in); got != want {
			t.Errorf("QualityLevelFor(%v) = %v, want %v", in, got, want)
		}
	}
}
