package world3d

import (
	"math"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/quality"
)

const (
	defaultTerrainChunkCells = 64
	defaultTerrainHysteresis = 0.10
	maxTerrainLODShift       = 20
)

// TerrainEdgeLODs specifies the LOD level of each cardinal neighbor of a
// terrain chunk so BuildMeshLOD can stitch shared borders without cracks or
// T-junctions. A neighbor LOD less than or equal to the chunk's own LOD
// (including negative values for absent neighbors) leaves that edge unstitched.
type TerrainEdgeLODs struct {
	North int // neighbor toward -Z (smaller row)
	South int // neighbor toward +Z (larger row)
	West  int // neighbor toward -X (smaller col)
	East  int // neighbor toward +X (larger col)
}

// TerrainLODBiasFor returns the recommended terrain LOD bias for a game-wide
// quality preset: +1.0 for quality.Low (one LOD coarser), +0.5 for
// quality.Medium, and 0.0 for quality.High (full detail distances). An invalid
// level maps to quality.Medium, matching quality.DefaultPreset and
// QualityLevelFor.
//
// Positive bias shifts chunks toward coarser LODs by scaling distance
// thresholds by 2^(-bias); negative bias keeps finer LODs farther away.
func TerrainLODBiasFor(l quality.Level) float32 {
	switch l {
	case quality.Low:
		return 1.0
	case quality.High:
		return 0.0
	default:
		return 0.5
	}
}

// ChunkBounds returns the world-space axis-aligned bounding box enclosing the
// rectangular chunk of grid cells starting at (chunkCol, chunkRow) and spanning
// (chunkCols, chunkRows) cells. If chunkCols or chunkRows is <= 0, the chunk
// extends to the remaining edge of the heightmap.
func (h *Heightmap) ChunkBounds(chunkCol, chunkRow, chunkCols, chunkRows int) math3d.AABB {
	c0, r0, nx, nz, ok := h.clampChunkRange(chunkCol, chunkRow, chunkCols, chunkRows)
	if !ok {
		return math3d.AABBEmpty()
	}
	minH, maxH := h.chunkRawHeightRange(c0, r0, c0+nx, r0+nz)
	y0 := h.Origin.Y + minH*h.HeightScale
	y1 := h.Origin.Y + maxH*h.HeightScale
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	return math3d.AABB{
		Min: math3d.Vec3{
			X: h.Origin.X + float32(c0)*h.CellSize,
			Y: y0,
			Z: h.Origin.Z + float32(r0)*h.CellSize,
		},
		Max: math3d.Vec3{
			X: h.Origin.X + float32(c0+nx)*h.CellSize,
			Y: y1,
			Z: h.Origin.Z + float32(r0+nz)*h.CellSize,
		},
	}
}

func (h *Heightmap) clampChunkRange(chunkCol, chunkRow, chunkCols, chunkRows int) (c0, r0, nx, nz int, ok bool) {
	if !h.valid() {
		return 0, 0, 0, 0, false
	}
	if chunkCol < 0 {
		chunkCol = 0
	}
	if chunkRow < 0 {
		chunkRow = 0
	}
	if chunkCol >= h.Cols-1 || chunkRow >= h.Rows-1 {
		return 0, 0, 0, 0, false
	}
	if chunkCols <= 0 {
		chunkCols = (h.Cols - 1) - chunkCol
	}
	if chunkRows <= 0 {
		chunkRows = (h.Rows - 1) - chunkRow
	}
	endCol := min(chunkCol+chunkCols, h.Cols-1)
	endRow := min(chunkRow+chunkRows, h.Rows-1)
	nx = endCol - chunkCol
	nz = endRow - chunkRow
	if nx <= 0 || nz <= 0 {
		return 0, 0, 0, 0, false
	}
	return chunkCol, chunkRow, nx, nz, true
}

func (h *Heightmap) chunkRawHeightRange(c0, r0, c1, r1 int) (minH, maxH float32) {
	minH = h.Heights[r0*h.Cols+c0]
	maxH = minH
	for r := r0; r <= r1; r++ {
		row := h.Heights[r*h.Cols+c0 : r*h.Cols+c1+1]
		for _, v := range row {
			if v < minH {
				minH = v
			}
			if v > maxH {
				maxH = v
			}
		}
	}
	return minH, maxH
}

// BuildMeshLOD generates an indexed triangle mesh for the rectangular chunk of
// grid cells starting at (chunkCol, chunkRow) and spanning (chunkCols, chunkRows)
// cells at the given LOD level (vertex stride 1 << lod).
//
// Whenever a neighbor in neighbors has a strictly coarser LOD (> lod), the
// shared border strip is re-triangulated to connect directly to that neighbor's
// coarser edge vertices, eliminating both height cracks and T-junctions across
// any pair of adjacent LOD levels (including partial border chunks). All
// triangles use counter-clockwise upward (+Y) winding and every emitted vertex
// is referenced by at least one triangle.
func (h *Heightmap) BuildMeshLOD(chunkCol, chunkRow, chunkCols, chunkRows int, lod int, neighbors TerrainEdgeLODs) TerrainMesh {
	c0, r0, nx, nz, ok := h.clampChunkRange(chunkCol, chunkRow, chunkCols, chunkRows)
	if !ok {
		return TerrainMesh{}
	}
	if lod < 0 {
		lod = 0
	} else if lod > maxTerrainLODShift {
		lod = maxTerrainLODShift
	}
	stride := 1 << lod

	xs := makeSampleOffsets(nx, stride)
	zs := makeSampleOffsets(nz, stride)
	w := len(xs)
	ht := len(zs)

	rN := edgeStrideRatio(neighbors.North, lod)
	rS := edgeStrideRatio(neighbors.South, lod)
	rW := edgeStrideRatio(neighbors.West, lod)
	rE := edgeStrideRatio(neighbors.East, lod)

	invMaxC := float32(1) / float32(h.Cols-1)
	invMaxR := float32(1) / float32(h.Rows-1)

	// Fast path: no coarser neighbor or single-quad chunk.
	if (rN == 1 && rS == 1 && rW == 1 && rE == 1) || (w == 2 && ht == 2) {
		return h.buildRegularLODMesh(c0, r0, xs, zs, invMaxC, invMaxR)
	}

	// Thin strip chunks (only 2 vertices along X or Z) stitch cleanly with a
	// two-pointer ribbon connecting the two opposite outer borders.
	if w == 2 {
		return h.buildThinXStitchedMesh(c0, r0, xs, zs, rW, rE, invMaxC, invMaxR)
	}
	if ht == 2 {
		return h.buildThinZStitchedMesh(c0, r0, xs, zs, rN, rS, invMaxC, invMaxR)
	}

	return h.buildStitchedLODMesh(c0, r0, xs, zs, rN, rS, rW, rE, invMaxC, invMaxR)
}

func makeSampleOffsets(span, stride int) []int {
	count := (span-1)/stride + 2
	out := make([]int, 0, count)
	for v := 0; v < span; v += stride {
		out = append(out, v)
	}
	out = append(out, span)
	return out
}

func edgeStrideRatio(neighborLOD, lod int) int {
	if neighborLOD <= lod {
		return 1
	}
	diff := min(neighborLOD-lod, maxTerrainLODShift)
	return 1 << diff
}

func makeEdgeAnchors(n, ratio int) []int {
	if ratio <= 1 {
		out := make([]int, n)
		for i := range n {
			out[i] = i
		}
		return out
	}
	last := n - 1
	out := make([]int, 0, (last-1)/ratio+2)
	for i := 0; i < last; i += ratio {
		out = append(out, i)
	}
	out = append(out, last)
	return out
}

func (h *Heightmap) appendVertex(mesh *TerrainMesh, gc, gr int, invMaxC, invMaxR float32) uint32 {
	idx := uint32(len(mesh.Positions))
	mesh.Positions = append(mesh.Positions, math3d.Vec3{
		X: h.Origin.X + float32(gc)*h.CellSize,
		Y: h.SampleHeight(gc, gr),
		Z: h.Origin.Z + float32(gr)*h.CellSize,
	})
	mesh.Normals = append(mesh.Normals, h.vertexNormal(gc, gr))
	mesh.UVs = append(mesh.UVs, math3d.Vec2{
		X: float32(gc) * invMaxC,
		Y: float32(gr) * invMaxR,
	})
	return idx
}

func (h *Heightmap) buildRegularLODMesh(c0, r0 int, xs, zs []int, invMaxC, invMaxR float32) TerrainMesh {
	w := len(xs)
	ht := len(zs)
	vertCount := w * ht
	mesh := TerrainMesh{
		Positions: make([]math3d.Vec3, vertCount),
		Normals:   make([]math3d.Vec3, vertCount),
		UVs:       make([]math3d.Vec2, vertCount),
		Indices:   make([]uint32, 0, (w-1)*(ht-1)*6),
	}

	for lr, oz := range zs {
		gr := r0 + oz
		rowOff := lr * w
		for lc, ox := range xs {
			gc := c0 + ox
			idx := rowOff + lc
			mesh.Positions[idx] = math3d.Vec3{
				X: h.Origin.X + float32(gc)*h.CellSize,
				Y: h.SampleHeight(gc, gr),
				Z: h.Origin.Z + float32(gr)*h.CellSize,
			}
			mesh.Normals[idx] = h.vertexNormal(gc, gr)
			mesh.UVs[idx] = math3d.Vec2{
				X: float32(gc) * invMaxC,
				Y: float32(gr) * invMaxR,
			}
		}
	}

	for lr := range ht - 1 {
		rowOff := uint32(lr * w)
		for lc := range w - 1 {
			i00 := rowOff + uint32(lc)
			i10 := i00 + 1
			i01 := i00 + uint32(w)
			i11 := i01 + 1
			mesh.Indices = append(mesh.Indices,
				i00, i01, i10,
				i10, i01, i11,
			)
		}
	}
	return mesh
}

func (h *Heightmap) buildThinXStitchedMesh(c0, r0 int, xs, zs []int, rW, rE int, invMaxC, invMaxR float32) TerrainMesh {
	ht := len(zs)
	wAnchors := makeEdgeAnchors(ht, rW)
	eAnchors := makeEdgeAnchors(ht, rE)
	vertCap := len(wAnchors) + len(eAnchors)
	triCap := (len(wAnchors) + len(eAnchors) - 2) * 3
	mesh := TerrainMesh{
		Positions: make([]math3d.Vec3, 0, vertCap),
		Normals:   make([]math3d.Vec3, 0, vertCap),
		UVs:       make([]math3d.Vec2, 0, vertCap),
		Indices:   make([]uint32, 0, triCap),
	}
	wVerts := make([]uint32, len(wAnchors))
	for i, a := range wAnchors {
		wVerts[i] = h.appendVertex(&mesh, c0+xs[0], r0+zs[a], invMaxC, invMaxR)
	}
	eVerts := make([]uint32, len(eAnchors))
	for i, a := range eAnchors {
		eVerts[i] = h.appendVertex(&mesh, c0+xs[1], r0+zs[a], invMaxC, invMaxR)
	}

	i, j := 0, 0
	p, q := len(wAnchors)-1, len(eAnchors)-1
	for i < p || j < q {
		if j == q || (i < p && wAnchors[i+1] <= eAnchors[j+1]) {
			mesh.Indices = append(mesh.Indices, wVerts[i], wVerts[i+1], eVerts[j])
			i++
		} else {
			mesh.Indices = append(mesh.Indices, wVerts[i], eVerts[j+1], eVerts[j])
			j++
		}
	}
	return mesh
}

func (h *Heightmap) buildThinZStitchedMesh(c0, r0 int, xs, zs []int, rN, rS int, invMaxC, invMaxR float32) TerrainMesh {
	w := len(xs)
	nAnchors := makeEdgeAnchors(w, rN)
	sAnchors := makeEdgeAnchors(w, rS)
	vertCap := len(nAnchors) + len(sAnchors)
	triCap := (len(nAnchors) + len(sAnchors) - 2) * 3
	mesh := TerrainMesh{
		Positions: make([]math3d.Vec3, 0, vertCap),
		Normals:   make([]math3d.Vec3, 0, vertCap),
		UVs:       make([]math3d.Vec2, 0, vertCap),
		Indices:   make([]uint32, 0, triCap),
	}
	nVerts := make([]uint32, len(nAnchors))
	for i, a := range nAnchors {
		nVerts[i] = h.appendVertex(&mesh, c0+xs[a], r0+zs[0], invMaxC, invMaxR)
	}
	sVerts := make([]uint32, len(sAnchors))
	for i, a := range sAnchors {
		sVerts[i] = h.appendVertex(&mesh, c0+xs[a], r0+zs[1], invMaxC, invMaxR)
	}

	i, j := 0, 0
	p, q := len(nAnchors)-1, len(sAnchors)-1
	for i < p || j < q {
		if j == q || (i < p && nAnchors[i+1] <= sAnchors[j+1]) {
			mesh.Indices = append(mesh.Indices, nVerts[i], sVerts[j], nVerts[i+1])
			i++
		} else {
			mesh.Indices = append(mesh.Indices, nVerts[i], sVerts[j], sVerts[j+1])
			j++
		}
	}
	return mesh
}

// buildStitchedLODMesh partitions a chunk with w >= 3 and ht >= 3 into an
// interior regular grid [1..w-2]x[1..ht-2] and four independent trapezoidal
// border strips meeting along the four corner diagonals. Each border strip uses
// only the coarser neighbor's anchor vertices on its outer edge, so adjacent
// chunks match edge-for-edge with zero cracks and zero T-junctions regardless
// of neighbor LOD combinations.
func (h *Heightmap) buildStitchedLODMesh(c0, r0 int, xs, zs []int, rN, rS, rW, rE int, invMaxC, invMaxR float32) TerrainMesh {
	w := len(xs)
	ht := len(zs)
	inW := w - 2
	inH := ht - 2

	nAnchors := makeEdgeAnchors(w, rN)
	sAnchors := makeEdgeAnchors(w, rS)
	wAnchors := makeEdgeAnchors(ht, rW)
	eAnchors := makeEdgeAnchors(ht, rE)

	vertCount := inW*inH + 4 + (len(nAnchors) - 2) + (len(sAnchors) - 2) + (len(wAnchors) - 2) + (len(eAnchors) - 2)
	estTris := (inW-1)*(inH-1)*2 + (w-1)*4 + (ht-1)*4
	mesh := TerrainMesh{
		Positions: make([]math3d.Vec3, 0, vertCount),
		Normals:   make([]math3d.Vec3, 0, vertCount),
		UVs:       make([]math3d.Vec2, 0, vertCount),
		Indices:   make([]uint32, 0, estTris*3),
	}

	// 1. Interior vertices [1..w-2] x [1..ht-2].
	for lr := 1; lr <= ht-2; lr++ {
		gr := r0 + zs[lr]
		for lc := 1; lc <= w-2; lc++ {
			h.appendVertex(&mesh, c0+xs[lc], gr, invMaxC, invMaxR)
		}
	}
	innerIdx := func(lc, lr int) uint32 {
		return uint32((lr-1)*inW + (lc - 1))
	}

	// 2. Four corner vertices.
	nwIdx := h.appendVertex(&mesh, c0+xs[0], r0+zs[0], invMaxC, invMaxR)
	neIdx := h.appendVertex(&mesh, c0+xs[w-1], r0+zs[0], invMaxC, invMaxR)
	swIdx := h.appendVertex(&mesh, c0+xs[0], r0+zs[ht-1], invMaxC, invMaxR)
	seIdx := h.appendVertex(&mesh, c0+xs[w-1], r0+zs[ht-1], invMaxC, invMaxR)

	// 3. Border anchor vertices (excluding corners, which are shared above).
	nVerts := make([]uint32, len(nAnchors))
	nVerts[0] = nwIdx
	nVerts[len(nVerts)-1] = neIdx
	for i := 1; i < len(nAnchors)-1; i++ {
		nVerts[i] = h.appendVertex(&mesh, c0+xs[nAnchors[i]], r0+zs[0], invMaxC, invMaxR)
	}

	sVerts := make([]uint32, len(sAnchors))
	sVerts[0] = swIdx
	sVerts[len(sVerts)-1] = seIdx
	for i := 1; i < len(sAnchors)-1; i++ {
		sVerts[i] = h.appendVertex(&mesh, c0+xs[sAnchors[i]], r0+zs[ht-1], invMaxC, invMaxR)
	}

	wVerts := make([]uint32, len(wAnchors))
	wVerts[0] = nwIdx
	wVerts[len(wVerts)-1] = swIdx
	for i := 1; i < len(wAnchors)-1; i++ {
		wVerts[i] = h.appendVertex(&mesh, c0+xs[0], r0+zs[wAnchors[i]], invMaxC, invMaxR)
	}

	eVerts := make([]uint32, len(eAnchors))
	eVerts[0] = neIdx
	eVerts[len(eVerts)-1] = seIdx
	for i := 1; i < len(eAnchors)-1; i++ {
		eVerts[i] = h.appendVertex(&mesh, c0+xs[w-1], r0+zs[eAnchors[i]], invMaxC, invMaxR)
	}

	// 4. Interior regular quads.
	for lr := 1; lr < ht-2; lr++ {
		for lc := 1; lc < w-2; lc++ {
			i00 := innerIdx(lc, lr)
			i10 := i00 + 1
			i01 := i00 + uint32(inW)
			i11 := i01 + 1
			mesh.Indices = append(mesh.Indices,
				i00, i01, i10,
				i10, i01, i11,
			)
		}
	}

	// 5. North trapezoid (outer row 0, inner row 1).
	for j := range len(nAnchors) - 1 {
		a, b := nAnchors[j], nAnchors[j+1]
		vA, vB := nVerts[j], nVerts[j+1]
		inStart := max(a, 1)
		inEnd := min(b, w-2)
		mid := (inStart + inEnd) / 2
		for k := inStart; k < mid; k++ {
			mesh.Indices = append(mesh.Indices, vA, innerIdx(k, 1), innerIdx(k+1, 1))
		}
		mesh.Indices = append(mesh.Indices, vA, innerIdx(mid, 1), vB)
		for k := mid; k < inEnd; k++ {
			mesh.Indices = append(mesh.Indices, vB, innerIdx(k, 1), innerIdx(k+1, 1))
		}
	}

	// 6. South trapezoid (outer row ht-1, inner row ht-2).
	for j := range len(sAnchors) - 1 {
		a, b := sAnchors[j], sAnchors[j+1]
		vA, vB := sVerts[j], sVerts[j+1]
		inStart := max(a, 1)
		inEnd := min(b, w-2)
		mid := (inStart + inEnd) / 2
		for k := inStart; k < mid; k++ {
			mesh.Indices = append(mesh.Indices, vA, innerIdx(k+1, ht-2), innerIdx(k, ht-2))
		}
		mesh.Indices = append(mesh.Indices, vA, vB, innerIdx(mid, ht-2))
		for k := mid; k < inEnd; k++ {
			mesh.Indices = append(mesh.Indices, vB, innerIdx(k+1, ht-2), innerIdx(k, ht-2))
		}
	}

	// 7. West trapezoid (outer col 0, inner col 1).
	for j := range len(wAnchors) - 1 {
		a, b := wAnchors[j], wAnchors[j+1]
		vA, vB := wVerts[j], wVerts[j+1]
		inStart := max(a, 1)
		inEnd := min(b, ht-2)
		mid := (inStart + inEnd) / 2
		for k := inStart; k < mid; k++ {
			mesh.Indices = append(mesh.Indices, vA, innerIdx(1, k+1), innerIdx(1, k))
		}
		mesh.Indices = append(mesh.Indices, vA, vB, innerIdx(1, mid))
		for k := mid; k < inEnd; k++ {
			mesh.Indices = append(mesh.Indices, vB, innerIdx(1, k+1), innerIdx(1, k))
		}
	}

	// 8. East trapezoid (outer col w-1, inner col w-2).
	for j := range len(eAnchors) - 1 {
		a, b := eAnchors[j], eAnchors[j+1]
		vA, vB := eVerts[j], eVerts[j+1]
		inStart := max(a, 1)
		inEnd := min(b, ht-2)
		mid := (inStart + inEnd) / 2
		for k := inStart; k < mid; k++ {
			mesh.Indices = append(mesh.Indices, vA, innerIdx(w-2, k), innerIdx(w-2, k+1))
		}
		mesh.Indices = append(mesh.Indices, vA, innerIdx(w-2, mid), vB)
		for k := mid; k < inEnd; k++ {
			mesh.Indices = append(mesh.Indices, vB, innerIdx(w-2, k), innerIdx(w-2, k+1))
		}
	}

	return mesh
}

// TerrainLODConfig configures a TerrainLOD chunk manager.
type TerrainLODConfig struct {
	// ChunkCells is the number of heightmap grid cells along each chunk edge.
	// Defaults to 64 when <= 0.
	ChunkCells int

	// Distances lists the maximum camera-to-chunk-AABB distance for LOD 0, 1,
	// ... in world units. Chunks beyond the last entry use the coarsest level.
	// When empty, geometric distances starting at 1.0 * ChunkWorldSize (doubling
	// per LOD level up to MaxLOD) are generated automatically.
	Distances []float32

	// Hysteresis is the relative deadband in (0, 0.5] around each LOD distance
	// threshold to prevent popping when the camera oscillates near a boundary.
	// Zero uses the default 0.10 (10%); negative values disable hysteresis.
	Hysteresis float32

	// LODBias shifts LOD distance thresholds by 2^(-LODBias). Positive values
	// choose coarser LODs at shorter distances (fewer triangles); negative values
	// keep finer detail farther away. Use TerrainLODBiasFor(level) to derive a
	// bias from a game-wide quality preset.
	LODBias float32

	// MaxLOD optionally caps the coarsest LOD level (vertex stride 1<<MaxLOD).
	// When <= 0, MaxLOD is derived from ChunkCells so stride reaches ChunkCells.
	MaxLOD int

	// WorldOrigin places the heightmap's (0, 0) sample in double-precision world
	// space for FloatingOrigin and ChunkStreamer integration. When HasWorldOrigin
	// is false and WorldOrigin is zero, Heightmap.Origin is used.
	WorldOrigin    WorldPos64
	HasWorldOrigin bool

	// StreamManaged, when true, starts with all chunks inactive so a
	// ChunkStreamer activates and deactivates chunks via SetChunkActive.
	// When false (default), every chunk covering the heightmap is active.
	StreamManaged bool
}

// TerrainLODChunk describes one visible terrain chunk returned by
// TerrainLOD.Update or TerrainLOD.UpdateWithOrigin.
type TerrainLODChunk struct {
	Coord     ChunkCoord
	Col       int
	Row       int
	Cols      int
	Rows      int
	LOD       int
	Edges     TerrainEdgeLODs
	Bounds    math3d.AABB
	Transform math3d.Mat4
	Mesh      *TerrainMesh
	Revision  uint64
	Changed   bool
}

// TerrainLODStats summarizes the most recent TerrainLOD update.
type TerrainLODStats struct {
	TotalChunks    int
	ActiveChunks   int
	VisibleChunks  int
	CulledChunks   int
	RebuiltChunks  int
	TotalTriangles int
	TotalVertices  int
}

type terrainChunkState struct {
	coord    ChunkCoord
	col, row int
	nx, nz   int
	minX     float64
	maxX     float64
	minY     float64
	maxY     float64
	minZ     float64
	maxZ     float64

	active       bool
	visible      bool
	lod          int
	localBounds  math3d.AABB
	mesh         TerrainMesh
	hasMesh      bool
	meshLOD      int
	meshEdges    TerrainEdgeLODs
	meshRevision uint64
}

// TerrainLOD partitions a Heightmap into chunks, selects per-chunk LOD levels
// from camera distance with hysteresis and quality bias, culls chunk AABBs
// against a camera frustum, and caches crack-free stitched TerrainMesh geometry
// per chunk. It works in world space or with FloatingOrigin and ChunkStreamer.
type TerrainLOD struct {
	hm             *Heightmap
	chunkCells     int
	chunkWorldSize float64
	gridCols       int
	gridRows       int
	baseCoord      ChunkCoord
	worldOrigin    WorldPos64

	maxLOD        int
	thresholds    []float64
	effThresholds []float64
	outThresholds []float64
	inThresholds  []float64
	hysteresis    float64
	lodBias       float32
	biasScale     float64

	chunks  []terrainChunkState
	visible []TerrainLODChunk
	stats   TerrainLODStats
}

// NewTerrainLOD constructs a TerrainLOD manager for hm using cfg.
func NewTerrainLOD(hm *Heightmap, cfg TerrainLODConfig) *TerrainLOD {
	chunkCells := cfg.ChunkCells
	if chunkCells <= 0 {
		chunkCells = defaultTerrainChunkCells
	}

	hyst := float64(cfg.Hysteresis)
	if cfg.Hysteresis == 0 {
		hyst = defaultTerrainHysteresis
	} else if hyst < 0 {
		hyst = 0
	} else if hyst > 0.5 {
		hyst = 0.5
	}

	t := &TerrainLOD{
		hm:         hm,
		chunkCells: chunkCells,
		hysteresis: hyst,
		lodBias:    cfg.LODBias,
		biasScale:  math.Pow(2, -float64(cfg.LODBias)),
	}
	if !hm.valid() {
		return t
	}

	cellSize := float64(hm.CellSize)
	t.chunkWorldSize = float64(chunkCells) * cellSize

	if cfg.HasWorldOrigin || cfg.WorldOrigin != (WorldPos64{}) {
		t.worldOrigin = cfg.WorldOrigin
	} else {
		t.worldOrigin = WorldPos64{
			X: float64(hm.Origin.X),
			Y: float64(hm.Origin.Y),
			Z: float64(hm.Origin.Z),
		}
	}
	t.baseCoord = ChunkCoord{
		X: int32(math.Floor(t.worldOrigin.X / t.chunkWorldSize)),
		Z: int32(math.Floor(t.worldOrigin.Z / t.chunkWorldSize)),
	}

	maxLOD := maxLODForCells(chunkCells)
	if cfg.MaxLOD > 0 && cfg.MaxLOD < maxLOD {
		maxLOD = cfg.MaxLOD
	}
	if len(cfg.Distances) > 0 {
		if len(cfg.Distances) < maxLOD {
			maxLOD = len(cfg.Distances)
		}
		t.thresholds = make([]float64, maxLOD)
		for i := range maxLOD {
			d := float64(cfg.Distances[i])
			if d < 0 {
				d = 0
			}
			if i > 0 && d < t.thresholds[i-1] {
				d = t.thresholds[i-1]
			}
			t.thresholds[i] = d
		}
	} else {
		t.thresholds = make([]float64, maxLOD)
		baseDist := t.chunkWorldSize
		for i := range maxLOD {
			t.thresholds[i] = baseDist * float64(uint64(1)<<i)
		}
	}
	t.maxLOD = maxLOD
	t.effThresholds = make([]float64, maxLOD)
	t.outThresholds = make([]float64, maxLOD)
	t.inThresholds = make([]float64, maxLOD)
	t.refreshScaledThresholds()

	totalCols := hm.Cols - 1
	totalRows := hm.Rows - 1
	t.gridCols = (totalCols + chunkCells - 1) / chunkCells
	t.gridRows = (totalRows + chunkCells - 1) / chunkCells
	t.chunks = make([]terrainChunkState, t.gridCols*t.gridRows)
	t.visible = make([]TerrainLODChunk, 0, min(len(t.chunks), 256))

	for cz := range t.gridRows {
		r0 := cz * chunkCells
		nz := min(chunkCells, totalRows-r0)
		for cx := range t.gridCols {
			c0 := cx * chunkCells
			nx := min(chunkCells, totalCols-c0)
			idx := cz*t.gridCols + cx
			st := &t.chunks[idx]
			st.coord = ChunkCoord{
				X: t.baseCoord.X + int32(cx),
				Z: t.baseCoord.Z + int32(cz),
			}
			st.col = c0
			st.row = r0
			st.nx = nx
			st.nz = nz
			st.active = !cfg.StreamManaged
			st.lod = -1
			t.computeChunkBounds(st)
		}
	}

	return t
}

func maxLODForCells(cells int) int {
	lod := 0
	for (1<<lod) < cells && lod < maxTerrainLODShift {
		lod++
	}
	return lod
}

func (t *TerrainLOD) computeChunkBounds(st *terrainChunkState) {
	cellSize := float64(t.hm.CellSize)
	minH, maxH := t.hm.chunkRawHeightRange(st.col, st.row, st.col+st.nx, st.row+st.nz)
	y0 := t.worldOrigin.Y + float64(minH)*float64(t.hm.HeightScale)
	y1 := t.worldOrigin.Y + float64(maxH)*float64(t.hm.HeightScale)
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	st.minX = t.worldOrigin.X + float64(st.col)*cellSize
	st.maxX = t.worldOrigin.X + float64(st.col+st.nx)*cellSize
	st.minY = y0
	st.maxY = y1
	st.minZ = t.worldOrigin.Z + float64(st.row)*cellSize
	st.maxZ = t.worldOrigin.Z + float64(st.row+st.nz)*cellSize
}

func (t *TerrainLOD) chunkIndex(coord ChunkCoord) (int, bool) {
	if t == nil || len(t.chunks) == 0 {
		return -1, false
	}
	cx := int(coord.X) - int(t.baseCoord.X)
	cz := int(coord.Z) - int(t.baseCoord.Z)
	if cx < 0 || cx >= t.gridCols || cz < 0 || cz >= t.gridRows {
		return -1, false
	}
	return cz*t.gridCols + cx, true
}

// ChunkCount returns the total number of chunks partitioning the heightmap.
func (t *TerrainLOD) ChunkCount() int {
	if t == nil {
		return 0
	}
	return len(t.chunks)
}

// GridSize returns the number of terrain chunks along X (cols) and Z (rows).
func (t *TerrainLOD) GridSize() (cols, rows int) {
	if t == nil {
		return 0, 0
	}
	return t.gridCols, t.gridRows
}

// ChunkCells returns the nominal cell count along each chunk edge.
func (t *TerrainLOD) ChunkCells() int {
	if t == nil {
		return 0
	}
	return t.chunkCells
}

// ChunkWorldSize returns the world-space width of a full chunk (ChunkCells *
// Heightmap.CellSize), suitable for configuring ChunkStreamerConfig.ChunkSize.
func (t *TerrainLOD) ChunkWorldSize() float64 {
	if t == nil {
		return 0
	}
	return t.chunkWorldSize
}

// MaxLOD returns the coarsest LOD index managed by t.
func (t *TerrainLOD) MaxLOD() int {
	if t == nil {
		return 0
	}
	return t.maxLOD
}

// LODBias returns the current LOD bias.
func (t *TerrainLOD) LODBias() float32 {
	if t == nil {
		return 0
	}
	return t.lodBias
}

func (t *TerrainLOD) refreshScaledThresholds() {
	h := t.hysteresis
	for i, th := range t.thresholds {
		eff := th * t.biasScale
		t.effThresholds[i] = eff
		t.outThresholds[i] = eff * (1 + h)
		t.inThresholds[i] = eff * (1 - h)
	}
}

// SetLODBias updates the LOD bias (threshold scale = 2^(-bias)) and resets
// per-chunk hysteresis memory so the next Update reflects the new bias
// immediately.
func (t *TerrainLOD) SetLODBias(bias float32) {
	if t == nil || t.lodBias == bias {
		return
	}
	t.lodBias = bias
	t.biasScale = math.Pow(2, -float64(bias))
	t.refreshScaledThresholds()
	for i := range t.chunks {
		t.chunks[i].lod = -1
	}
}

// SetQualityLevel applies TerrainLODBiasFor(level) to t.
func (t *TerrainLOD) SetQualityLevel(level quality.Level) {
	t.SetLODBias(TerrainLODBiasFor(level))
}

// SetChunkActive activates or deactivates the chunk at coord (typically called
// from ChunkStreamer's OnLoad and OnUnload callbacks when StreamManaged is
// true). Deactivating a chunk releases its cached mesh; neighboring seams
// automatically re-stitch on the next Update.
func (t *TerrainLOD) SetChunkActive(coord ChunkCoord, active bool) {
	idx, ok := t.chunkIndex(coord)
	if !ok {
		return
	}
	st := &t.chunks[idx]
	if st.active == active {
		return
	}
	st.active = active
	if !active {
		st.lod = -1
		st.visible = false
		st.mesh = TerrainMesh{}
		st.hasMesh = false
	}
}

// IsChunkActive reports whether coord is a valid chunk coordinate in t and is
// currently active.
func (t *TerrainLOD) IsChunkActive(coord ChunkCoord) bool {
	idx, ok := t.chunkIndex(coord)
	if !ok {
		return false
	}
	return t.chunks[idx].active
}

// ChunkBounds returns the world-space AABB for the chunk at coord.
func (t *TerrainLOD) ChunkBounds(coord ChunkCoord) (math3d.AABB, bool) {
	idx, ok := t.chunkIndex(coord)
	if !ok {
		return math3d.AABBEmpty(), false
	}
	st := &t.chunks[idx]
	return math3d.AABB{
		Min: math3d.Vec3{X: float32(st.minX), Y: float32(st.minY), Z: float32(st.minZ)},
		Max: math3d.Vec3{X: float32(st.maxX), Y: float32(st.maxY), Z: float32(st.maxZ)},
	}, true
}

// ChunkLOD returns the most recently selected LOD level for coord, or (-1, false)
// if coord is out of bounds or has not been evaluated while active.
func (t *TerrainLOD) ChunkLOD(coord ChunkCoord) (int, bool) {
	idx, ok := t.chunkIndex(coord)
	if !ok {
		return -1, false
	}
	st := &t.chunks[idx]
	if !st.active || st.lod < 0 {
		return -1, false
	}
	return st.lod, true
}

// InvalidateChunk recomputes the AABB for coord and forces its mesh to rebuild
// on the next Update (for example after editing Heightmap.Heights in place).
func (t *TerrainLOD) InvalidateChunk(coord ChunkCoord) {
	idx, ok := t.chunkIndex(coord)
	if !ok {
		return
	}
	st := &t.chunks[idx]
	t.computeChunkBounds(st)
	st.hasMesh = false
}

// InvalidateAll recomputes every chunk AABB and forces all cached meshes to
// rebuild as they become visible.
func (t *TerrainLOD) InvalidateAll() {
	if t == nil {
		return
	}
	for i := range t.chunks {
		t.computeChunkBounds(&t.chunks[i])
		t.chunks[i].hasMesh = false
	}
}

// EvictHiddenMeshes frees cached TerrainMesh buffers for chunks that were not
// visible in the most recent Update.
func (t *TerrainLOD) EvictHiddenMeshes() {
	if t == nil {
		return
	}
	for i := range t.chunks {
		if !t.chunks[i].visible && t.chunks[i].hasMesh {
			t.chunks[i].mesh = TerrainMesh{}
			t.chunks[i].hasMesh = false
		}
	}
}

// Stats returns summary statistics from the most recent Update or
// UpdateWithOrigin call.
func (t *TerrainLOD) Stats() TerrainLODStats {
	if t == nil {
		return TerrainLODStats{}
	}
	return t.stats
}

// Update selects per-chunk LOD levels for a world-space camera at camPos, culls
// chunk bounds against frustum (when non-nil), builds or reuses crack-free
// stitched meshes for visible chunks, and returns the visible chunk slice.
//
// The returned slice aliases internal storage reused across calls; copy any
// fields needed past the next Update.
func (t *TerrainLOD) Update(camPos math3d.Vec3, frustum *math3d.Frustum) []TerrainLODChunk {
	return t.UpdateWithOrigin(camPos, frustum, nil)
}

// UpdateWithOrigin advances terrain LOD selection and frustum culling using a
// camera and frustum expressed in FloatingOrigin local space.
//
// Camera-to-chunk distances are evaluated in float64 world space so LOD
// selection is completely invariant to FloatingOrigin rebasing. Each returned
// TerrainLODChunk carries Bounds and a translation Transform in local space,
// allowing cached chunk meshes to render cleanly across rebases without
// rebuilding vertex buffers.
func (t *TerrainLOD) UpdateWithOrigin(camLocal math3d.Vec3, frustumLocal *math3d.Frustum, origin *FloatingOrigin) []TerrainLODChunk {
	if t == nil {
		return nil
	}
	t.visible = t.visible[:0]
	t.stats = TerrainLODStats{TotalChunks: len(t.chunks)}
	if len(t.chunks) == 0 {
		return t.visible
	}

	var camWorld WorldPos64
	var originPos WorldPos64
	if origin != nil {
		camWorld = origin.LocalToWorld(camLocal)
		originPos = origin.Origin
	} else {
		camWorld = WorldPos64{X: float64(camLocal.X), Y: float64(camLocal.Y), Z: float64(camLocal.Z)}
	}

	// Translation mapping Heightmap-space vertices (hm.Origin + offset) into the
	// caller's local rendering space.
	shift := math3d.Vec3{
		X: float32(t.worldOrigin.X-originPos.X) - t.hm.Origin.X,
		Y: float32(t.worldOrigin.Y-originPos.Y) - t.hm.Origin.Y,
		Z: float32(t.worldOrigin.Z-originPos.Z) - t.hm.Origin.Z,
	}
	chunkTransform := math3d.Translate(shift)

	var planes [6]math3d.Plane
	hasFrustum := frustumLocal != nil
	if hasFrustum {
		planes = frustumLocal.Planes
	}

	// Pass 1: select LOD for every active chunk and evaluate frustum visibility.
	activeCount := 0
	culledCount := 0
	for i := range t.chunks {
		st := &t.chunks[i]
		if !st.active {
			st.visible = false
			continue
		}
		activeCount++

		dist := aabbMinDist64(st.minX, st.minY, st.minZ, st.maxX, st.maxY, st.maxZ, camWorld)
		st.lod = t.selectLOD(dist, st.lod)

		localBounds := math3d.AABB{
			Min: math3d.Vec3{
				X: float32(st.minX - originPos.X),
				Y: float32(st.minY - originPos.Y),
				Z: float32(st.minZ - originPos.Z),
			},
			Max: math3d.Vec3{
				X: float32(st.maxX - originPos.X),
				Y: float32(st.maxY - originPos.Y),
				Z: float32(st.maxZ - originPos.Z),
			},
		}
		st.localBounds = localBounds

		if hasFrustum && classifyAABBFrustum(planes, localBounds) == frustumOutside {
			st.visible = false
			culledCount++
			continue
		}
		st.visible = true
	}

	// Pass 2: resolve neighbor seams and build/reuse meshes for visible chunks.
	rebuiltCount := 0
	totalTris := 0
	totalVerts := 0

	for cz := range t.gridRows {
		rowBase := cz * t.gridCols
		for cx := range t.gridCols {
			st := &t.chunks[rowBase+cx]
			if !st.visible {
				continue
			}

			edges := t.effectiveEdges(cx, cz, st.lod)
			changed := false
			if !st.hasMesh || st.meshLOD != st.lod || st.meshEdges != edges {
				st.mesh = t.hm.BuildMeshLOD(st.col, st.row, st.nx, st.nz, st.lod, edges)
				st.hasMesh = true
				st.meshLOD = st.lod
				st.meshEdges = edges
				st.meshRevision++
				changed = true
				rebuiltCount++
			}

			totalTris += len(st.mesh.Indices) / 3
			totalVerts += len(st.mesh.Positions)
			t.visible = append(t.visible, TerrainLODChunk{
				Coord:     st.coord,
				Col:       st.col,
				Row:       st.row,
				Cols:      st.nx,
				Rows:      st.nz,
				LOD:       st.lod,
				Edges:     edges,
				Bounds:    st.localBounds,
				Transform: chunkTransform,
				Mesh:      &st.mesh,
				Revision:  st.meshRevision,
				Changed:   changed,
			})
		}
	}

	t.stats = TerrainLODStats{
		TotalChunks:    len(t.chunks),
		ActiveChunks:   activeCount,
		VisibleChunks:  len(t.visible),
		CulledChunks:   culledCount,
		RebuiltChunks:  rebuiltCount,
		TotalTriangles: totalTris,
		TotalVertices:  totalVerts,
	}
	return t.visible
}

func (t *TerrainLOD) effectiveEdges(cx, cz, lod int) TerrainEdgeLODs {
	edges := TerrainEdgeLODs{North: -1, South: -1, West: -1, East: -1}
	cols := t.gridCols
	rowBase := cz * cols
	chunks := t.chunks

	if cz > 0 {
		nb := &chunks[rowBase-cols+cx]
		if nb.active && nb.lod > lod {
			edges.North = nb.lod
		}
	}
	if cz+1 < t.gridRows {
		nb := &chunks[rowBase+cols+cx]
		if nb.active && nb.lod > lod {
			edges.South = nb.lod
		}
	}
	if cx > 0 {
		nb := &chunks[rowBase+cx-1]
		if nb.active && nb.lod > lod {
			edges.West = nb.lod
		}
	}
	if cx+1 < cols {
		nb := &chunks[rowBase+cx+1]
		if nb.active && nb.lod > lod {
			edges.East = nb.lod
		}
	}
	return edges
}

func (t *TerrainLOD) selectLOD(dist float64, prevLOD int) int {
	eff := t.effThresholds
	n := len(eff)
	if n == 0 {
		return 0
	}

	raw := n
	for i, th := range eff {
		if dist <= th {
			raw = i
			break
		}
	}

	if t.hysteresis == 0 || prevLOD < 0 || prevLOD > n || raw == prevLOD {
		return raw
	}

	if raw > prevLOD {
		// Moving farther away (coarsening): hold prevLOD until dist exceeds its
		// outward hysteresis deadband.
		out := t.outThresholds
		if prevLOD < n && dist <= out[prevLOD] {
			return prevLOD
		}
		for i := prevLOD + 1; i < raw; i++ {
			if dist <= out[i] {
				return i
			}
		}
		return raw
	}

	// Moving closer (refining): only upgrade to a finer LOD once dist crosses
	// inside its inward hysteresis deadband.
	in := t.inThresholds
	for i := raw; i < prevLOD; i++ {
		if dist <= in[i] {
			return i
		}
	}
	return prevLOD
}

func aabbMinDist64(minX, minY, minZ, maxX, maxY, maxZ float64, p WorldPos64) float64 {
	var dx, dy, dz float64
	if p.X < minX {
		dx = minX - p.X
	} else if p.X > maxX {
		dx = p.X - maxX
	}
	if p.Y < minY {
		dy = minY - p.Y
	} else if p.Y > maxY {
		dy = p.Y - maxY
	}
	if p.Z < minZ {
		dz = minZ - p.Z
	} else if p.Z > maxZ {
		dz = p.Z - maxZ
	}
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}
