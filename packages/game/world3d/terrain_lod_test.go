package world3d_test

import (
	"context"
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/quality"
	"github.com/nsgo-dev/game/packages/game/world3d"
)

func makeRollingHeightmap(cols, rows int, cellSize, heightScale float32) *world3d.Heightmap {
	heights := make([]float32, cols*rows)
	for r := range rows {
		fr := float64(r)
		for c := range cols {
			fc := float64(c)
			// Multi-frequency ridges & hills so every grid vertex has non-coplanar elevation.
			h := 0.5 +
				0.25*math.Sin(fc*0.23)*math.Cos(fr*0.19) +
				0.15*math.Sin(fc*0.71+fr*0.53) +
				0.10*math.Cos(fc*1.31-fr*0.97)
			heights[r*cols+c] = float32(h)
		}
	}
	return &world3d.Heightmap{
		Cols:        cols,
		Rows:        rows,
		CellSize:    cellSize,
		HeightScale: heightScale,
		Heights:     heights,
	}
}

type quantVec3 struct {
	x, y, z int64
}

func quantizeVec3(v math3d.Vec3) quantVec3 {
	const scale = 1e4
	return quantVec3{
		x: int64(math.Round(float64(v.X) * scale)),
		y: int64(math.Round(float64(v.Y) * scale)),
		z: int64(math.Round(float64(v.Z) * scale)),
	}
}

type directedEdge struct {
	a, b quantVec3
}

// verifyChunkMeshInvariants validates winding, exact XZ footprint area, zero
// unreferenced vertices, and watertight interior 2-manifoldness.
func verifyChunkMeshInvariants(t *testing.T, label string, hm *world3d.Heightmap, mesh world3d.TerrainMesh, c0, r0, nx, nz int) {
	t.Helper()
	if len(mesh.Positions) == 0 || len(mesh.Indices) == 0 || len(mesh.Indices)%3 != 0 {
		t.Fatalf("%s: invalid mesh counts verts=%d indices=%d", label, len(mesh.Positions), len(mesh.Indices))
	}
	if len(mesh.Normals) != len(mesh.Positions) || len(mesh.UVs) != len(mesh.Positions) {
		t.Fatalf("%s: attribute length mismatch pos=%d norm=%d uv=%d", label, len(mesh.Positions), len(mesh.Normals), len(mesh.UVs))
	}

	minX := hm.Origin.X + float32(c0)*hm.CellSize
	maxX := hm.Origin.X + float32(c0+nx)*hm.CellSize
	minZ := hm.Origin.Z + float32(r0)*hm.CellSize
	maxZ := hm.Origin.Z + float32(r0+nz)*hm.CellSize

	used := make([]bool, len(mesh.Positions))
	directedCounts := make(map[directedEdge]int, len(mesh.Indices))
	var totalAreaXZ float64

	for i := 0; i < len(mesh.Indices); i += 3 {
		i0, i1, i2 := mesh.Indices[i], mesh.Indices[i+1], mesh.Indices[i+2]
		if int(i0) >= len(mesh.Positions) || int(i1) >= len(mesh.Positions) || int(i2) >= len(mesh.Positions) {
			t.Fatalf("%s: triangle %d index out of bounds", label, i/3)
		}
		used[i0], used[i1], used[i2] = true, true, true

		v0, v1, v2 := mesh.Positions[i0], mesh.Positions[i1], mesh.Positions[i2]
		cross := v1.Sub(v0).Cross(v2.Sub(v0))
		if cross.Y <= 0 {
			t.Fatalf("%s: triangle %d (%v, %v, %v) has non-upward winding cross=%v", label, i/3, v0, v1, v2, cross)
		}
		totalAreaXZ += 0.5 * float64(cross.Y)

		q0, q1, q2 := quantizeVec3(v0), quantizeVec3(v1), quantizeVec3(v2)
		directedCounts[directedEdge{q0, q1}]++
		directedCounts[directedEdge{q1, q2}]++
		directedCounts[directedEdge{q2, q0}]++
	}

	for i, ok := range used {
		if !ok {
			t.Fatalf("%s: unreferenced vertex %d at %v", label, i, mesh.Positions[i])
		}
	}

	wantAreaXZ := float64(nx) * float64(nz) * float64(hm.CellSize) * float64(hm.CellSize)
	if math.Abs(totalAreaXZ-wantAreaXZ) > 1e-2 {
		t.Fatalf("%s: projected XZ area = %v, want %v (holes or overlaps detected)", label, totalAreaXZ, wantAreaXZ)
	}

	onBorder := func(q quantVec3) bool {
		x := float32(q.x) / 1e4
		z := float32(q.z) / 1e4
		return math.Abs(float64(x-minX)) < 1e-3 ||
			math.Abs(float64(x-maxX)) < 1e-3 ||
			math.Abs(float64(z-minZ)) < 1e-3 ||
			math.Abs(float64(z-maxZ)) < 1e-3
	}

	for e, cnt := range directedCounts {
		if cnt != 1 {
			t.Fatalf("%s: duplicate directed edge %+v count=%d", label, e, cnt)
		}
		rev := directedEdge{a: e.b, b: e.a}
		revCnt := directedCounts[rev]
		// Every edge not lying wholly on one of the 4 chunk borders must have an
		// opposite-orientation twin (interior watertight 2-manifold).
		if revCnt == 0 {
			if !(onBorder(e.a) && onBorder(e.b)) {
				t.Fatalf("%s: unmatched interior edge %+v (crack or T-junction inside chunk)", label, e)
			}
		}
	}
}

// collectSharedBorderEdges extracts all directed exterior edges lying on the
// plane coord == planeVal (isX=true for X=planeVal, false for Z=planeVal).
func collectSharedBorderEdges(mesh world3d.TerrainMesh, isX bool, planeVal float32) ([]directedEdge, map[quantVec3]math3d.Vec3) {
	onPlane := func(v math3d.Vec3) bool {
		if isX {
			return math.Abs(float64(v.X-planeVal)) < 1e-3
		}
		return math.Abs(float64(v.Z-planeVal)) < 1e-3
	}

	var edges []directedEdge
	normals := make(map[quantVec3]math3d.Vec3)
	for i := 0; i < len(mesh.Indices); i += 3 {
		tri := [3]uint32{mesh.Indices[i], mesh.Indices[i+1], mesh.Indices[i+2]}
		for k := range 3 {
			i0, i1 := tri[k], tri[(k+1)%3]
			p0, p1 := mesh.Positions[i0], mesh.Positions[i1]
			if onPlane(p0) && onPlane(p1) {
				q0, q1 := quantizeVec3(p0), quantizeVec3(p1)
				edges = append(edges, directedEdge{a: q0, b: q1})
				normals[q0] = mesh.Normals[i0]
				normals[q1] = mesh.Normals[i1]
			}
		}
	}
	return edges, normals
}

func assertSeamWatertight(t *testing.T, label string, meshA, meshB world3d.TerrainMesh, isX bool, planeVal float32) {
	t.Helper()
	edgesA, normsA := collectSharedBorderEdges(meshA, isX, planeVal)
	edgesB, normsB := collectSharedBorderEdges(meshB, isX, planeVal)
	if len(edgesA) == 0 || len(edgesA) != len(edgesB) {
		t.Fatalf("%s: border edge count mismatch A=%d B=%d (T-junction or gap)", label, len(edgesA), len(edgesB))
	}

	// Because both chunks have +Y CCW winding, every directed border edge a->b in
	// A must appear with exact opposite orientation b->a in B.
	setB := make(map[directedEdge]int, len(edgesB))
	for _, e := range edgesB {
		setB[e]++
	}
	for _, e := range edgesA {
		twin := directedEdge{a: e.b, b: e.a}
		if setB[twin] != 1 {
			t.Fatalf("%s: border edge %+v in chunk A has no opposite-orientation twin in chunk B (setB=%d)", label, e, setB[twin])
		}
		delete(setB, twin)
	}
	if len(setB) != 0 {
		t.Fatalf("%s: %d unmatched border edges remain in chunk B", label, len(setB))
	}

	// Verify normals match identically at every shared boundary vertex.
	for q, nA := range normsA {
		nB, ok := normsB[q]
		if !ok {
			t.Fatalf("%s: shared vertex %+v missing in chunk B", label, q)
		}
		if !nA.ApproxEqual(nB, 1e-4) {
			t.Fatalf("%s: normal seam mismatch at %+v: A=%v B=%v", label, q, nA, nB)
		}
	}
}

func TestBuildMeshLODSeamPairsAllEdges(t *testing.T) {
	// Test both full power-of-two chunks (16x16, LOD 0..4 => strides 1..16)
	// and non-power-of-two partial edge chunks (13x13, plus thin 2-cell edge strip).
	cases := []struct {
		name      string
		cols      int
		rows      int
		chunkSize int
		maxLOD    int
	}{
		{name: "pow2_16x16", cols: 33, rows: 33, chunkSize: 16, maxLOD: 4},
		{name: "nonpow2_partial_13", cols: 30, rows: 30, chunkSize: 16, maxLOD: 4}, // chunks of 16 and 13 cells
		{name: "thin_edge_strip", cols: 19, rows: 19, chunkSize: 16, maxLOD: 4},    // chunks of 16 and 2 cells
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hm := makeRollingHeightmap(tc.cols, tc.rows, 2.0, 25.0)
			cs := tc.chunkSize
			remX := (tc.cols - 1) - cs
			remZ := (tc.rows - 1) - cs

			for lodA := 0; lodA <= tc.maxLOD; lodA++ {
				for lodB := 0; lodB <= tc.maxLOD; lodB++ {
					// 1. East / West seam: A=(0,0,cs,cs) east-adjacent to B=(cs,0,remX,cs) along X = cs*CellSize.
					meshA := hm.BuildMeshLOD(0, 0, cs, cs, lodA, world3d.TerrainEdgeLODs{East: lodB})
					meshB := hm.BuildMeshLOD(cs, 0, remX, cs, lodB, world3d.TerrainEdgeLODs{West: lodA})
					lblEW := fmt.Sprintf("EW(%d,%d)", lodA, lodB)
					verifyChunkMeshInvariants(t, lblEW+":A", hm, meshA, 0, 0, cs, cs)
					verifyChunkMeshInvariants(t, lblEW+":B", hm, meshB, cs, 0, remX, cs)
					assertSeamWatertight(t, lblEW, meshA, meshB, true, float32(cs)*hm.CellSize)

					// 2. South / North seam: A=(0,0,cs,cs) south-adjacent to B=(0,cs,cs,remZ) along Z = cs*CellSize.
					meshN := hm.BuildMeshLOD(0, 0, cs, cs, lodA, world3d.TerrainEdgeLODs{South: lodB})
					meshS := hm.BuildMeshLOD(0, cs, cs, remZ, lodB, world3d.TerrainEdgeLODs{North: lodA})
					lblNS := fmt.Sprintf("NS(%d,%d)", lodA, lodB)
					verifyChunkMeshInvariants(t, lblNS+":N", hm, meshN, 0, 0, cs, cs)
					verifyChunkMeshInvariants(t, lblNS+":S", hm, meshS, 0, cs, cs, remZ)
					assertSeamWatertight(t, lblNS, meshN, meshS, false, float32(cs)*hm.CellSize)

					// 3. Partial-to-partial seam on the bottom-right strip:
					// East/West between (0,cs,cs,remZ) and (cs,cs,remX,remZ).
					meshSW := hm.BuildMeshLOD(0, cs, cs, remZ, lodA, world3d.TerrainEdgeLODs{East: lodB})
					meshSE := hm.BuildMeshLOD(cs, cs, remX, remZ, lodB, world3d.TerrainEdgeLODs{West: lodA})
					lblPEW := fmt.Sprintf("PartialEW(%d,%d)", lodA, lodB)
					verifyChunkMeshInvariants(t, lblPEW+":SW", hm, meshSW, 0, cs, cs, remZ)
					verifyChunkMeshInvariants(t, lblPEW+":SE", hm, meshSE, cs, cs, remX, remZ)
					assertSeamWatertight(t, lblPEW, meshSW, meshSE, true, float32(cs)*hm.CellSize)

					// North/South between (cs,0,remX,cs) and (cs,cs,remX,remZ).
					meshNE := hm.BuildMeshLOD(cs, 0, remX, cs, lodA, world3d.TerrainEdgeLODs{South: lodB})
					meshSE2 := hm.BuildMeshLOD(cs, cs, remX, remZ, lodB, world3d.TerrainEdgeLODs{North: lodA})
					lblPNS := fmt.Sprintf("PartialNS(%d,%d)", lodA, lodB)
					verifyChunkMeshInvariants(t, lblPNS+":NE", hm, meshNE, cs, 0, remX, cs)
					verifyChunkMeshInvariants(t, lblPNS+":SE", hm, meshSE2, cs, cs, remX, remZ)
					assertSeamWatertight(t, lblPNS, meshNE, meshSE2, false, float32(cs)*hm.CellSize)
				}
			}
		})
	}
}

func TestBuildMeshLODMultiCornerMixedNeighbors(t *testing.T) {
	// 3x3 chunk grid (each 16x16 cells = 49x49 samples): center chunk at LOD 0
	// surrounded simultaneously by 4 DIFFERENT coarser LODs (N=1, E=2, S=3, W=4).
	const cs = 16
	hm := makeRollingHeightmap(3*cs+1, 3*cs+1, 1.5, 30.0)

	center := hm.BuildMeshLOD(cs, cs, cs, cs, 0, world3d.TerrainEdgeLODs{
		North: 1,
		East:  2,
		South: 3,
		West:  4,
	})
	north := hm.BuildMeshLOD(cs, 0, cs, cs, 1, world3d.TerrainEdgeLODs{South: 0})
	east := hm.BuildMeshLOD(2*cs, cs, cs, cs, 2, world3d.TerrainEdgeLODs{West: 0})
	south := hm.BuildMeshLOD(cs, 2*cs, cs, cs, 3, world3d.TerrainEdgeLODs{North: 0})
	west := hm.BuildMeshLOD(0, cs, cs, cs, 4, world3d.TerrainEdgeLODs{East: 0})

	verifyChunkMeshInvariants(t, "center", hm, center, cs, cs, cs, cs)
	verifyChunkMeshInvariants(t, "north", hm, north, cs, 0, cs, cs)
	verifyChunkMeshInvariants(t, "east", hm, east, 2*cs, cs, cs, cs)
	verifyChunkMeshInvariants(t, "south", hm, south, cs, 2*cs, cs, cs)
	verifyChunkMeshInvariants(t, "west", hm, west, 0, cs, cs, cs)

	assertSeamWatertight(t, "center-north", center, north, false, float32(cs)*hm.CellSize)
	assertSeamWatertight(t, "center-south", center, south, false, float32(2*cs)*hm.CellSize)
	assertSeamWatertight(t, "center-west", center, west, true, float32(cs)*hm.CellSize)
	assertSeamWatertight(t, "center-east", center, east, true, float32(2*cs)*hm.CellSize)
}

func TestBuildMeshLODZeroMatchesBuildMesh(t *testing.T) {
	hm := makeRollingHeightmap(33, 33, 2.0, 15.0)
	base := hm.BuildMesh(4, 6, 16, 16)
	lod0 := hm.BuildMeshLOD(4, 6, 16, 16, 0, world3d.TerrainEdgeLODs{})

	if !slices.Equal(base.Positions, lod0.Positions) ||
		!slices.Equal(base.Normals, lod0.Normals) ||
		!slices.Equal(base.UVs, lod0.UVs) ||
		!slices.Equal(base.Indices, lod0.Indices) {
		t.Fatalf("BuildMeshLOD(lod=0, no coarser neighbors) differed from BuildMesh")
	}
}

func TestTerrainLODHysteresisPreventsThrashing(t *testing.T) {
	// Single 16x16 chunk at X=[0..16], Z=[0..16], Y=0.
	hm := &world3d.Heightmap{
		Cols:        17,
		Rows:        17,
		CellSize:    1.0,
		HeightScale: 1.0,
		Heights:     make([]float32, 17*17),
	}
	tlod := world3d.NewTerrainLOD(hm, world3d.TerrainLODConfig{
		ChunkCells: 16,
		Distances:  []float32{100, 200, 400},
		Hysteresis: 0.10, // outward switch at 110, inward switch at 90
	})
	coord := world3d.ChunkCoord{X: 0, Z: 0}

	// Start well inside LOD 0 (nearest distance to chunk max X=16 is 50 => camX = 66).
	vis := tlod.Update(math3d.Vec3{X: 66, Y: 0, Z: 8}, nil)
	if len(vis) != 1 || vis[0].LOD != 0 {
		t.Fatalf("initial LOD = %v, want 0", vis)
	}

	// Oscillate around threshold 100 within [92, 108]: must stay locked at LOD 0 with zero rebuilds.
	flips := 0
	for _, d := range []float32{95, 105, 92, 108, 99, 101, 109, 91, 104} {
		vis = tlod.Update(math3d.Vec3{X: 16 + d, Y: 0, Z: 8}, nil)
		if vis[0].LOD != 0 {
			flips++
		}
		if vis[0].Changed {
			t.Fatalf("chunk rebuilt unexpectedly during hysteresis deadband at dist=%v", d)
		}
	}
	if flips != 0 {
		t.Fatalf("hysteresis failed: %d unexpected LOD flips while oscillating inside [91..109]", flips)
	}

	// Cross outward threshold (> 110): must degrade to LOD 1.
	vis = tlod.Update(math3d.Vec3{X: 16 + 111, Y: 0, Z: 8}, nil)
	if vis[0].LOD != 1 || !vis[0].Changed {
		t.Fatalf("after crossing 110: LOD=%d changed=%v, want 1, true", vis[0].LOD, vis[0].Changed)
	}

	// Oscillate back inside [91, 109]: must stay locked at LOD 1.
	for _, d := range []float32{99, 95, 91, 105, 93, 108} {
		vis = tlod.Update(math3d.Vec3{X: 16 + d, Y: 0, Z: 8}, nil)
		if vis[0].LOD != 1 {
			t.Fatalf("upgraded prematurely at dist=%v (LOD=%d), want 1 until <= 90", d, vis[0].LOD)
		}
	}

	// Cross inward threshold (<= 90): must upgrade back to LOD 0.
	vis = tlod.Update(math3d.Vec3{X: 16 + 89, Y: 0, Z: 8}, nil)
	if vis[0].LOD != 0 || !vis[0].Changed {
		t.Fatalf("after crossing 90 inward: LOD=%d changed=%v, want 0, true", vis[0].LOD, vis[0].Changed)
	}
	if got, ok := tlod.ChunkLOD(coord); !ok || got != 0 {
		t.Fatalf("ChunkLOD = (%d, %v), want (0, true)", got, ok)
	}
}

func TestTerrainLODQualityPresetBias(t *testing.T) {
	// Verify TerrainLODBiasFor mapping matches quality presets.
	if world3d.TerrainLODBiasFor(quality.Low) <= world3d.TerrainLODBiasFor(quality.Medium) ||
		world3d.TerrainLODBiasFor(quality.Medium) <= world3d.TerrainLODBiasFor(quality.High) {
		t.Fatalf("expected Low bias > Medium bias > High bias, got Low=%v Med=%v High=%v",
			world3d.TerrainLODBiasFor(quality.Low),
			world3d.TerrainLODBiasFor(quality.Medium),
			world3d.TerrainLODBiasFor(quality.High))
	}
	if world3d.TerrainLODBiasFor(quality.Level(99)) != world3d.TerrainLODBiasFor(quality.Medium) {
		t.Fatalf("invalid quality level should map to Medium bias")
	}

	// 8x8 chunks of 32x32 cells (257x257 samples).
	hm := makeRollingHeightmap(257, 257, 2.0, 20.0)
	tlod := world3d.NewTerrainLOD(hm, world3d.TerrainLODConfig{
		ChunkCells: 32,
	})
	cam := math3d.Vec3{X: 32, Y: 10, Z: 32}

	tlod.SetQualityLevel(quality.High)
	tlod.Update(cam, nil)
	highTris := tlod.Stats().TotalTriangles

	tlod.SetQualityLevel(quality.Medium)
	tlod.Update(cam, nil)
	medTris := tlod.Stats().TotalTriangles

	tlod.SetQualityLevel(quality.Low)
	tlod.Update(cam, nil)
	lowTris := tlod.Stats().TotalTriangles

	if !(lowTris < medTris && medTris < highTris) {
		t.Fatalf("expected Low < Medium < High triangles, got Low=%d Med=%d High=%d", lowTris, medTris, highTris)
	}

	// Verify zero cracks across all adjacent visible chunks under every preset.
	for _, lvl := range quality.Levels() {
		tlod.SetQualityLevel(lvl)
		vis := tlod.Update(cam, nil)
		byCoord := make(map[world3d.ChunkCoord]world3d.TerrainLODChunk, len(vis))
		for _, ch := range vis {
			byCoord[ch.Coord] = ch
		}
		for _, ch := range vis {
			if east, ok := byCoord[world3d.ChunkCoord{X: ch.Coord.X + 1, Z: ch.Coord.Z}]; ok {
				assertSeamWatertight(t, fmt.Sprintf("%s:E(%v)", lvl, ch.Coord), *ch.Mesh, *east.Mesh, true, ch.Bounds.Max.X)
			}
			if south, ok := byCoord[world3d.ChunkCoord{X: ch.Coord.X, Z: ch.Coord.Z + 1}]; ok {
				assertSeamWatertight(t, fmt.Sprintf("%s:S(%v)", lvl, ch.Coord), *ch.Mesh, *south.Mesh, false, ch.Bounds.Max.Z)
			}
		}
	}
}

func TestTerrainLODFrustumFloatingOriginAndStreaming(t *testing.T) {
	// 8x8 chunks of 32x32 cells (cellSize=2m => 64m per chunk => 512m x 512m world),
	// placed far from the world origin at X=100_000, Z=-50_000 to exercise
	// float64 WorldOrigin + FloatingOrigin rebasing + ChunkStreamer lifecycle.
	const chunkCells = 32
	const cellSize = 2.0
	const chunkWorld = chunkCells * cellSize // 64m
	hm := makeRollingHeightmap(8*chunkCells+1, 8*chunkCells+1, cellSize, 30.0)
	worldOrigin := world3d.WorldPos64{X: 100_000, Y: 0, Z: -50_000}

	tlod := world3d.NewTerrainLOD(hm, world3d.TerrainLODConfig{
		ChunkCells:     chunkCells,
		WorldOrigin:    worldOrigin,
		HasWorldOrigin: true,
		StreamManaged:  true,
	})
	if tlod.ChunkWorldSize() != chunkWorld {
		t.Fatalf("ChunkWorldSize = %v, want %v", tlod.ChunkWorldSize(), chunkWorld)
	}

	// Hook ChunkStreamer directly to TerrainLOD.SetChunkActive.
	streamer := world3d.NewChunkStreamer(world3d.ChunkStreamerConfig[struct{}]{
		ChunkSize:    tlod.ChunkWorldSize(),
		LoadRadius:   2,
		UnloadRadius: 3,
		Workers:      0,
		Loader: func(_ context.Context, _ world3d.ChunkCoord) (struct{}, error) {
			return struct{}{}, nil
		},
		OnLoad: func(coord world3d.ChunkCoord, _ struct{}) {
			tlod.SetChunkActive(coord, true)
		},
		OnUnload: func(coord world3d.ChunkCoord, _ struct{}) {
			tlod.SetChunkActive(coord, false)
		},
	})
	defer streamer.Close()

	// FloatingOrigin centered near the terrain's center.
	fo := world3d.NewFloatingOrigin(128)
	fo.Origin = world3d.WorldPos64{X: 100_256, Y: 0, Z: -49_744}
	camLocal := math3d.Vec3{X: 0, Y: 40, Z: 0}
	camWorld := fo.LocalToWorld(camLocal)

	streamer.Update(camWorld)
	if len(streamer.LoadedChunks()) == 0 {
		t.Fatalf("expected streamer to load chunks around %v", camWorld)
	}

	// Camera looking toward -Z (North) with narrow FOV so chunks behind (+Z) get culled.
	makeFrustum := func(pos math3d.Vec3) math3d.Frustum {
		view := math3d.LookAt(pos, pos.Add(math3d.Vec3{X: 0, Y: -5, Z: -100}), math3d.Vec3Up())
		proj := math3d.Perspective(math3d.DegToRad(60), 16.0/9.0, 0.5, 500)
		return math3d.FrustumFromMat4(proj.Mul(view))
	}

	frustumBefore := makeFrustum(camLocal)
	visBefore := tlod.UpdateWithOrigin(camLocal, &frustumBefore, fo)
	statsBefore := tlod.Stats()
	if statsBefore.VisibleChunks == 0 || statsBefore.CulledChunks == 0 {
		t.Fatalf("expected both visible and culled chunks, got stats=%+v", statsBefore)
	}

	// Snapshot LODs and transformed vertex containment before rebase.
	type snap struct {
		lod      int
		revision uint64
	}
	beforeMap := make(map[world3d.ChunkCoord]snap, len(visBefore))
	for _, ch := range visBefore {
		beforeMap[ch.Coord] = snap{lod: ch.LOD, revision: ch.Revision}
		// Every transformed vertex must lie inside the chunk's local-space Bounds.
		for _, p := range ch.Mesh.Positions {
			lp := ch.Transform.MulVec4(p.Vec4(1)).XYZ()
			if lp.X < ch.Bounds.Min.X-1e-2 || lp.X > ch.Bounds.Max.X+1e-2 ||
				lp.Y < ch.Bounds.Min.Y-1e-2 || lp.Y > ch.Bounds.Max.Y+1e-2 ||
				lp.Z < ch.Bounds.Min.Z-1e-2 || lp.Z > ch.Bounds.Max.Z+1e-2 {
				t.Fatalf("chunk %v transformed vertex %v outside local bounds %v", ch.Coord, lp, ch.Bounds)
			}
		}
	}

	// Trigger a FloatingOrigin rebase without moving the camera in world space:
	// move origin by (+200, 0, -150) and shift camLocal by (-200, 0, +150).
	fo.Origin = fo.Origin.Add(world3d.WorldPos64{X: 200, Y: 0, Z: -150})
	camLocalAfter := camLocal.Add(math3d.Vec3{X: -200, Y: 0, Z: 150})
	frustumAfter := makeFrustum(camLocalAfter)

	visAfter := tlod.UpdateWithOrigin(camLocalAfter, &frustumAfter, fo)
	if len(visAfter) != len(visBefore) {
		t.Fatalf("visible chunk count changed across floating-origin rebase: before=%d after=%d", len(visBefore), len(visAfter))
	}
	if tlod.Stats().RebuiltChunks != 0 {
		t.Fatalf("floating-origin rebase caused %d unexpected mesh rebuilds, want 0", tlod.Stats().RebuiltChunks)
	}
	for _, ch := range visAfter {
		prev, ok := beforeMap[ch.Coord]
		if !ok {
			t.Fatalf("chunk %v appeared only after rebase", ch.Coord)
		}
		if ch.LOD != prev.lod || ch.Revision != prev.revision || ch.Changed {
			t.Fatalf("chunk %v changed across rebase: got (lod=%d rev=%d changed=%v), want (%d, %d, false)",
				ch.Coord, ch.LOD, ch.Revision, ch.Changed, prev.lod, prev.revision)
		}
		for _, p := range ch.Mesh.Positions {
			lp := ch.Transform.MulVec4(p.Vec4(1)).XYZ()
			if lp.X < ch.Bounds.Min.X-1e-2 || lp.X > ch.Bounds.Max.X+1e-2 ||
				lp.Y < ch.Bounds.Min.Y-1e-2 || lp.Y > ch.Bounds.Max.Y+1e-2 ||
				lp.Z < ch.Bounds.Min.Z-1e-2 || lp.Z > ch.Bounds.Max.Z+1e-2 {
				t.Fatalf("after rebase: chunk %v transformed vertex %v outside local bounds %v", ch.Coord, lp, ch.Bounds)
			}
		}
	}

	// Steady-state UpdateWithOrigin must incur zero heap allocations.
	allocs := testing.AllocsPerRun(50, func() {
		_ = tlod.UpdateWithOrigin(camLocalAfter, &frustumAfter, fo)
	})
	if allocs != 0 {
		t.Fatalf("steady-state UpdateWithOrigin allocs = %v, want 0", allocs)
	}
}

func TestTerrainLOD4096TriangleBudget(t *testing.T) {
	// 4096x4096 heightmap (4095x4095 cells = 33,538,050 full-res triangles).
	// Verify LOD reduces visible triangles to well within mobile/web budgets and
	// handles partial 63-cell edge chunks seamlessly.
	const size = 4096
	heights := make([]float32, size*size)
	// Fast deterministic terrain profile (avoid 16M sin/cos calls in unit test).
	for r := range size {
		rz := float32((r & 63) - 32)
		rowOff := r * size
		for c := range size {
			cx := float32((c & 63) - 32)
			heights[rowOff+c] = (cx*cx + rz*rz) * (1.0 / 2048.0)
		}
	}
	hm := &world3d.Heightmap{
		Cols:        size,
		Rows:        size,
		CellSize:    1.0,
		HeightScale: 40.0,
		Heights:     heights,
	}

	tlod := world3d.NewTerrainLOD(hm, world3d.TerrainLODConfig{
		ChunkCells: 64,
		LODBias:    world3d.TerrainLODBiasFor(quality.Medium),
	})
	if tlod.ChunkCount() != 64*64 {
		t.Fatalf("ChunkCount = %d, want 4096", tlod.ChunkCount())
	}

	// Mid-range mobile camera looking diagonally across the entire 4km terrain.
	camPos := math3d.Vec3{X: 128, Y: 60, Z: 128}
	view := math3d.LookAt(camPos, math3d.Vec3{X: 2500, Y: 0, Z: 2500}, math3d.Vec3Up())
	proj := math3d.Perspective(math3d.DegToRad(60), 16.0/9.0, 0.5, 5000)
	frustum := math3d.FrustumFromMat4(proj.Mul(view))

	vis := tlod.Update(camPos, &frustum)
	stats := tlod.Stats()
	if len(vis) == 0 {
		t.Fatalf("expected visible chunks across 4096x4096 terrain")
	}
	const fullResTris = (size - 1) * (size - 1) * 2
	// With LOD + frustum culling on Medium quality, total triangles must be under 100k
	// (a >300x reduction from 33.5M full-res triangles).
	if stats.TotalTriangles >= 100_000 {
		t.Fatalf("4096x4096 Medium visible triangles = %d, want < 100,000 (full-res=%d)", stats.TotalTriangles, fullResTris)
	}

	// Verify Low preset reduces triangle count even further.
	tlod.SetQualityLevel(quality.Low)
	tlod.Update(camPos, &frustum)
	lowStats := tlod.Stats()
	if lowStats.TotalTriangles >= stats.TotalTriangles {
		t.Fatalf("Low triangles (%d) not smaller than Medium (%d)", lowStats.TotalTriangles, stats.TotalTriangles)
	}
}

func TestTerrainLODEdgeCasesAndInvalidation(t *testing.T) {
	// Nil safety on TerrainLOD and invalid Heightmap.
	var nilLOD *world3d.TerrainLOD
	if nilLOD.ChunkCount() != 0 || nilLOD.Update(math3d.Vec3Zero(), nil) != nil {
		t.Fatalf("nil TerrainLOD returned unexpected non-zero values")
	}
	emptyLOD := world3d.NewTerrainLOD(&world3d.Heightmap{}, world3d.TerrainLODConfig{})
	if emptyLOD.ChunkCount() != 0 || len(emptyLOD.Update(math3d.Vec3Zero(), nil)) != 0 {
		t.Fatalf("invalid heightmap TerrainLOD should have 0 chunks")
	}

	// 2x1 chunks of 16x16 cells.
	hm := makeRollingHeightmap(33, 17, 1.0, 10.0)
	tlod := world3d.NewTerrainLOD(hm, world3d.TerrainLODConfig{
		ChunkCells: 16,
		Distances:  []float32{5, 20},
		Hysteresis: -1, // disable hysteresis
	})
	if cols, rows := tlod.GridSize(); cols != 2 || rows != 1 {
		t.Fatalf("GridSize = (%d, %d), want (2, 1)", cols, rows)
	}
	// ChunkBounds matches Heightmap.ChunkBounds.
	c0 := world3d.ChunkCoord{X: 0, Z: 0}
	c1 := world3d.ChunkCoord{X: 1, Z: 0}
	b0, ok := tlod.ChunkBounds(c0)
	wantB0 := hm.ChunkBounds(0, 0, 16, 16)
	if !ok || !b0.Min.ApproxEqual(wantB0.Min, 1e-4) || !b0.Max.ApproxEqual(wantB0.Max, 1e-4) {
		t.Fatalf("ChunkBounds(0,0) = (%v, %v), want %v", b0, ok, wantB0)
	}

	// Place camera inside chunk 0 so chunk 0 is LOD 0 and chunk 1 (dist > 5) is LOD 1.
	vis := tlod.Update(math3d.Vec3{X: 8, Y: 0, Z: 8}, nil)
	if len(vis) != 2 || vis[0].LOD != 0 || vis[1].LOD != 1 {
		t.Fatalf("initial LODs = (%d, %d), want (0, 1)", vis[0].LOD, vis[1].LOD)
	}
	if vis[0].Edges.East != 1 {
		t.Fatalf("chunk 0 East edge = %d, want 1", vis[0].Edges.East)
	}
	rev0 := vis[0].Revision

	// Deactivate chunk 1: chunk 0's East seam must drop back to unstitched (-1) and rebuild.
	tlod.SetChunkActive(c1, false)
	if tlod.IsChunkActive(c1) {
		t.Fatalf("chunk 1 still active after SetChunkActive(false)")
	}
	vis = tlod.Update(math3d.Vec3{X: 8, Y: 0, Z: 8}, nil)
	if len(vis) != 1 || vis[0].Edges.East != -1 || !vis[0].Changed || vis[0].Revision <= rev0 {
		t.Fatalf("after deactivating neighbor: len=%d east=%d changed=%v rev=%d", len(vis), vis[0].Edges.East, vis[0].Changed, vis[0].Revision)
	}

	// Mutate heightmap in place and call InvalidateChunk: AABB & mesh must update.
	hm.Heights[8*hm.Cols+8] = 50.0 // world Y = 500
	tlod.InvalidateChunk(c0)
	bUpdated, _ := tlod.ChunkBounds(c0)
	if bUpdated.Max.Y < 499 {
		t.Fatalf("InvalidateChunk did not update AABB Max.Y: got %v, want ~500", bUpdated.Max.Y)
	}
	vis = tlod.Update(math3d.Vec3{X: 8, Y: 0, Z: 8}, nil)
	if !vis[0].Changed {
		t.Fatalf("InvalidateChunk did not trigger mesh rebuild on Update")
	}

	// EvictHiddenMeshes frees meshes of culled chunks without affecting visible ones.
	viewAway := math3d.LookAt(math3d.Vec3{X: -10, Y: 0, Z: 8}, math3d.Vec3{X: -100, Y: 0, Z: 8}, math3d.Vec3Up())
	proj := math3d.Perspective(math3d.DegToRad(60), 1, 0.5, 200)
	frustumAway := math3d.FrustumFromMat4(proj.Mul(viewAway))
	vis = tlod.Update(math3d.Vec3{X: -10, Y: 0, Z: 8}, &frustumAway)
	if len(vis) != 0 {
		t.Fatalf("expected chunk 0 to be culled when looking away, got %d visible", len(vis))
	}
	tlod.EvictHiddenMeshes()
	vis = tlod.Update(math3d.Vec3{X: 8, Y: 0, Z: 8}, nil)
	if len(vis) != 1 || !vis[0].Changed {
		t.Fatalf("expected evicted chunk to rebuild when visible again")
	}
}

func BenchmarkTerrainLOD4096Update(b *testing.B) {
	const size = 4096
	heights := make([]float32, size*size)
	for r := range size {
		rz := float32((r & 63) - 32)
		rowOff := r * size
		for c := range size {
			cx := float32((c & 63) - 32)
			heights[rowOff+c] = (cx*cx + rz*rz) * (1.0 / 2048.0)
		}
	}
	hm := &world3d.Heightmap{
		Cols:        size,
		Rows:        size,
		CellSize:    1.0,
		HeightScale: 40.0,
		Heights:     heights,
	}

	tlod := world3d.NewTerrainLOD(hm, world3d.TerrainLODConfig{
		ChunkCells: 64,
		LODBias:    world3d.TerrainLODBiasFor(quality.Medium),
	})

	camPos := math3d.Vec3{X: 128, Y: 60, Z: 128}
	view := math3d.LookAt(camPos, math3d.Vec3{X: 2500, Y: 0, Z: 2500}, math3d.Vec3Up())
	proj := math3d.Perspective(math3d.DegToRad(60), 16.0/9.0, 0.5, 5000)
	frustum := math3d.FrustumFromMat4(proj.Mul(view))

	// Warm up initial visible meshes.
	tlod.Update(camPos, &frustum)
	stats := tlod.Stats()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		tlod.Update(camPos, &frustum)
	}
	b.ReportMetric(float64(stats.VisibleChunks), "vis_chunks")
	b.ReportMetric(float64(stats.TotalTriangles), "vis_tris")
}

func BenchmarkTerrainLOD4096ColdBuild(b *testing.B) {
	const size = 4096
	heights := make([]float32, size*size)
	for r := range size {
		rz := float32((r & 63) - 32)
		rowOff := r * size
		for c := range size {
			cx := float32((c & 63) - 32)
			heights[rowOff+c] = (cx*cx + rz*rz) * (1.0 / 2048.0)
		}
	}
	hm := &world3d.Heightmap{
		Cols:        size,
		Rows:        size,
		CellSize:    1.0,
		HeightScale: 40.0,
		Heights:     heights,
	}

	tlod := world3d.NewTerrainLOD(hm, world3d.TerrainLODConfig{
		ChunkCells: 64,
		LODBias:    world3d.TerrainLODBiasFor(quality.Medium),
	})

	camPos := math3d.Vec3{X: 128, Y: 60, Z: 128}
	view := math3d.LookAt(camPos, math3d.Vec3{X: 2500, Y: 0, Z: 2500}, math3d.Vec3Up())
	proj := math3d.Perspective(math3d.DegToRad(60), 16.0/9.0, 0.5, 5000)
	frustum := math3d.FrustumFromMat4(proj.Mul(view))

	// Opposite frustum looking away from terrain to evict cached meshes in O(chunks)
	// between cold-build iterations without rescanning all 16.7M heightmap samples.
	viewAway := math3d.LookAt(math3d.Vec3{X: -100, Y: 60, Z: -100}, math3d.Vec3{X: -1000, Y: 0, Z: -1000}, math3d.Vec3Up())
	frustumAway := math3d.FrustumFromMat4(proj.Mul(viewAway))

	b.ReportAllocs()
	b.ResetTimer()
	var stats world3d.TerrainLODStats
	for range b.N {
		b.StopTimer()
		tlod.Update(math3d.Vec3{X: -100, Y: 60, Z: -100}, &frustumAway)
		tlod.EvictHiddenMeshes()
		b.StartTimer()
		tlod.Update(camPos, &frustum)
		stats = tlod.Stats()
	}
	b.ReportMetric(float64(stats.RebuiltChunks), "rebuilt_chunks")
	b.ReportMetric(float64(stats.TotalTriangles), "vis_tris")
}
