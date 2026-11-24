package voxel

import (
	"slices"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
)

// UVMode controls how texture coordinates are assigned to merged voxel quads.
type UVMode uint8

const (
	// UVWorld spans UVs from (0, 0) to (width, height) in voxel units so
	// repeating textures tile seamlessly across greedy-merged quads.
	UVWorld UVMode = iota
	// UVUnit assigns [0, 1] x [0, 1] UVs to each quad regardless of size.
	UVUnit
)

// MeshOptions configures greedy chunk meshing.
// The zero value enables greedy meshing, cross-chunk neighbor culling,
// chunk-local vertex positions, and tiling UVWorld coordinates.
type MeshOptions struct {
	// AmbientOcclusion computes per-vertex Minecraft-style ambient occlusion
	// levels (0..3) in ChunkMesh.AO and flips quad diagonals to avoid
	// interpolation artifacts. Faces merge only when all four AO corners match.
	AmbientOcclusion bool
	// WorldSpace offsets emitted vertex positions by ChunkMesh.Origin so the
	// mesh can be drawn with an identity model transform. When false (default),
	// positions are chunk-local in [0, ChunkSize*VoxelSize].
	WorldSpace bool
	// DisableGreedy emits one quad per visible voxel face without merging
	// coplanar neighbors. Useful for comparison benchmarks and debugging.
	DisableGreedy bool
	// IgnoreNeighbors treats voxels outside the chunk as Air instead of
	// sampling neighboring chunks for face culling and ambient occlusion.
	IgnoreNeighbors bool
	// UVMode controls quad UV scaling (default UVWorld).
	UVMode UVMode
}

// SubMesh describes a contiguous index range in [ChunkMesh.Mesh] sharing one
// [BlockDef.MaterialID].
type SubMesh struct {
	MaterialID uint32
	FirstIndex uint32
	IndexCount uint32
}

// ChunkMesh holds the triangulated geometry generated for one chunk, ready to
// upload with [render3d.UploadMesh].
type ChunkMesh struct {
	// Coord is the chunk's grid coordinate.
	Coord ChunkCoord
	// Origin is the world-space minimum corner of the chunk.
	Origin math3d.Vec3
	// Bounds is the tight axis-aligned bounding box of Mesh.Positions, or
	// math3d.AABBEmpty when the chunk emitted no faces.
	Bounds math3d.AABB
	// Mesh holds CCW front-facing indexed triangle geometry compatible with
	// render3d.UploadMesh.
	Mesh render3d.Mesh
	// SubMeshes partitions Mesh.Indices by BlockDef.MaterialID in ascending
	// MaterialID order.
	SubMeshes []SubMesh
	// AO holds per-vertex ambient occlusion levels in [0, 3] (3 = unoccluded,
	// 0 = fully occluded) when MeshOptions.AmbientOcclusion is true; nil otherwise.
	AO []uint8
	// QuadCount is the number of emitted quads (len(Mesh.Indices) / 6).
	QuadCount int
}

// Empty reports whether the chunk produced no visible triangles.
func (cm ChunkMesh) Empty() bool {
	return len(cm.Mesh.Positions) == 0
}

// AOFactor converts a discrete ambient occlusion level (0..3) into a linear
// brightness multiplier in [0.25, 1.0].
func AOFactor(level uint8) float32 {
	switch level {
	case 0:
		return 0.25
	case 1:
		return 0.5
	case 2:
		return 0.75
	default:
		return 1.0
	}
}

// Mesher builds chunk meshes using reusable scratch buffers so incremental
// per-frame remeshing avoids temporary working-set allocations.
// A Mesher is not safe for concurrent use by multiple goroutines; give each
// worker its own Mesher.
type Mesher struct {
	padSize    int
	padded     []BlockID
	opaque     []bool
	mask       []uint32
	matIndices map[uint32][]uint32
	matOrder   []uint32
}

// NewMesher returns a reusable Mesher.
func NewMesher() *Mesher {
	return &Mesher{
		matIndices: make(map[uint32][]uint32),
	}
}

func (m *Mesher) ensureScratch(chunkSize int) {
	p := chunkSize + 2
	if m.padSize != p {
		m.padSize = p
		m.padded = make([]BlockID, p*p*p)
		m.opaque = make([]bool, p*p*p)
		m.mask = make([]uint32, chunkSize*chunkSize)
	}
	if m.matIndices == nil {
		m.matIndices = make(map[uint32][]uint32)
	}
}

// MeshChunk builds a [ChunkMesh] for the chunk at cc in w.
// If cc does not exist or has no visible faces, MeshChunk returns an empty
// ChunkMesh with Coord and Origin populated.
func (m *Mesher) MeshChunk(w *World, cc ChunkCoord, opts MeshOptions) ChunkMesh {
	origin := w.ChunkWorldOrigin(cc)
	out := ChunkMesh{
		Coord:  cc,
		Origin: origin,
		Bounds: math3d.AABBEmpty(),
	}
	if w == nil {
		return out
	}
	ch := w.chunks[cc]
	if ch == nil || ch.nonAir == 0 {
		return out
	}

	n := w.chunkSize
	m.ensureScratch(n)
	m.fillPadded(w, ch, opts)

	vs := w.voxelSize
	var baseOffset math3d.Vec3
	if opts.WorldSpace {
		baseOffset = origin
	}

	// Clear reusable per-material index buckets without freeing backing arrays.
	for k, s := range m.matIndices {
		m.matIndices[k] = s[:0]
	}
	m.matOrder = m.matOrder[:0]

	var positions []math3d.Vec3
	var normals []math3d.Vec3
	var uvs []math3d.Vec2
	var aos []uint8

	// Sweep each of the 6 face directions.
	for face := range Face(FaceCount) {
		m.meshFaceDir(w, face, n, vs, baseOffset, opts, &positions, &normals, &uvs, &aos)
	}

	if len(positions) == 0 {
		return out
	}

	// Build contiguous Mesh.Indices ordered by ascending MaterialID.
	totalIndices := 0
	for _, matID := range m.matOrder {
		totalIndices += len(m.matIndices[matID])
	}
	slices.Sort(m.matOrder)

	indices := make([]uint32, 0, totalIndices)
	subMeshes := make([]SubMesh, 0, len(m.matOrder))
	for _, matID := range m.matOrder {
		bucket := m.matIndices[matID]
		if len(bucket) == 0 {
			continue
		}
		subMeshes = append(subMeshes, SubMesh{
			MaterialID: matID,
			FirstIndex: uint32(len(indices)),
			IndexCount: uint32(len(bucket)),
		})
		indices = append(indices, bucket...)
	}

	mesh := render3d.Mesh{
		Positions: positions,
		Normals:   normals,
		UVs:       uvs,
		Indices:   indices,
	}
	out.Mesh = mesh
	out.Bounds = mesh.Bounds()
	out.SubMeshes = subMeshes
	out.AO = aos
	out.QuadCount = len(indices) / 6
	return out
}

// RemeshDirty meshes every chunk currently marked dirty in w in deterministic
// [CompareChunkCoords] order, clears their dirty flags, and returns the
// rebuilt [ChunkMesh] list. Chunks that became completely empty still appear
// in the result with Empty()==true so callers can discard stale GPU meshes.
func (m *Mesher) RemeshDirty(w *World, opts MeshOptions) []ChunkMesh {
	if w == nil || len(w.dirty) == 0 {
		return nil
	}
	coords := w.DirtyChunks()
	out := make([]ChunkMesh, len(coords))
	for i, cc := range coords {
		out[i] = m.MeshChunk(w, cc, opts)
		w.ClearDirty(cc)
	}
	return out
}

// MeshChunk is a convenience wrapper around [Mesher.MeshChunk] for one-off builds.
// For repeated per-frame remeshing, keep a reusable [Mesher].
func (w *World) MeshChunk(cc ChunkCoord, opts MeshOptions) ChunkMesh {
	return NewMesher().MeshChunk(w, cc, opts)
}

// RemeshDirty is a convenience wrapper around [Mesher.RemeshDirty].
func (w *World) RemeshDirty(opts MeshOptions) []ChunkMesh {
	return NewMesher().RemeshDirty(w, opts)
}

func (m *Mesher) padIdx(px, py, pz int) int {
	p := m.padSize
	return (py*p+pz)*p + px
}

func (m *Mesher) fillPadded(w *World, ch *Chunk, opts MeshOptions) {
	n := w.chunkSize
	p := m.padSize
	reg := w.registry
	cc := ch.coord

	clear(m.padded)
	clear(m.opaque)

	// Copy interior N x N x N block data row by row.
	for ly := range n {
		py := ly + 1
		for lz := range n {
			pz := lz + 1
			srcOff := (ly*n + lz) * n
			dstOff := (py*p+pz)*p + 1
			copy(m.padded[dstOff:dstOff+n], ch.blocks[srcOff:srcOff+n])
			for lx := range n {
				id := ch.blocks[srcOff+lx]
				if id != Air && reg.IsOpaque(id) {
					m.opaque[dstOff+lx] = true
				}
			}
		}
	}

	if opts.IgnoreNeighbors {
		return
	}

	// Copy the 6 face-adjacent slabs (always needed for cross-chunk face culling).
	if nb := w.chunks[cc.Add(ChunkCoord{X: -1})]; nb != nil && nb.nonAir > 0 {
		for ly := range n {
			for lz := range n {
				m.setPad(0, ly+1, lz+1, nb.getUnchecked(n-1, ly, lz), reg)
			}
		}
	}
	if nb := w.chunks[cc.Add(ChunkCoord{X: 1})]; nb != nil && nb.nonAir > 0 {
		for ly := range n {
			for lz := range n {
				m.setPad(n+1, ly+1, lz+1, nb.getUnchecked(0, ly, lz), reg)
			}
		}
	}
	if nb := w.chunks[cc.Add(ChunkCoord{Y: -1})]; nb != nil && nb.nonAir > 0 {
		for lz := range n {
			for lx := range n {
				m.setPad(lx+1, 0, lz+1, nb.getUnchecked(lx, n-1, lz), reg)
			}
		}
	}
	if nb := w.chunks[cc.Add(ChunkCoord{Y: 1})]; nb != nil && nb.nonAir > 0 {
		for lz := range n {
			for lx := range n {
				m.setPad(lx+1, n+1, lz+1, nb.getUnchecked(lx, 0, lz), reg)
			}
		}
	}
	if nb := w.chunks[cc.Add(ChunkCoord{Z: -1})]; nb != nil && nb.nonAir > 0 {
		for ly := range n {
			for lx := range n {
				m.setPad(lx+1, ly+1, 0, nb.getUnchecked(lx, ly, n-1), reg)
			}
		}
	}
	if nb := w.chunks[cc.Add(ChunkCoord{Z: 1})]; nb != nil && nb.nonAir > 0 {
		for ly := range n {
			for lx := range n {
				m.setPad(lx+1, ly+1, n+1, nb.getUnchecked(lx, ly, 0), reg)
			}
		}
	}

	if !opts.AmbientOcclusion {
		return
	}

	// Copy the 12 edge strips and 8 corners for seamless cross-chunk AO.
	for _, dx := range [2]int{-1, 1} {
		px, nlx := padSide(dx, n)
		for _, dy := range [2]int{-1, 1} {
			py, nly := padSide(dy, n)
			if nb := w.chunks[cc.Add(ChunkCoord{X: dx, Y: dy})]; nb != nil && nb.nonAir > 0 {
				for lz := range n {
					m.setPad(px, py, lz+1, nb.getUnchecked(nlx, nly, lz), reg)
				}
			}
		}
		for _, dz := range [2]int{-1, 1} {
			pz, nlz := padSide(dz, n)
			if nb := w.chunks[cc.Add(ChunkCoord{X: dx, Z: dz})]; nb != nil && nb.nonAir > 0 {
				for ly := range n {
					m.setPad(px, ly+1, pz, nb.getUnchecked(nlx, ly, nlz), reg)
				}
			}
		}
	}
	for _, dy := range [2]int{-1, 1} {
		py, nly := padSide(dy, n)
		for _, dz := range [2]int{-1, 1} {
			pz, nlz := padSide(dz, n)
			if nb := w.chunks[cc.Add(ChunkCoord{Y: dy, Z: dz})]; nb != nil && nb.nonAir > 0 {
				for lx := range n {
					m.setPad(lx+1, py, pz, nb.getUnchecked(lx, nly, nlz), reg)
				}
			}
		}
	}
	for _, dx := range [2]int{-1, 1} {
		px, nlx := padSide(dx, n)
		for _, dy := range [2]int{-1, 1} {
			py, nly := padSide(dy, n)
			for _, dz := range [2]int{-1, 1} {
				pz, nlz := padSide(dz, n)
				if nb := w.chunks[cc.Add(ChunkCoord{X: dx, Y: dy, Z: dz})]; nb != nil && nb.nonAir > 0 {
					m.setPad(px, py, pz, nb.getUnchecked(nlx, nly, nlz), reg)
				}
			}
		}
	}
}

func padSide(dir, n int) (padCoord, neighborLocal int) {
	if dir < 0 {
		return 0, n - 1
	}
	return n + 1, 0
}

func (m *Mesher) setPad(px, py, pz int, id BlockID, reg *Registry) {
	if id == Air {
		return
	}
	idx := m.padIdx(px, py, pz)
	m.padded[idx] = id
	if reg.IsOpaque(id) {
		m.opaque[idx] = true
	}
}

type faceAxisSpec struct {
	d           int  // normal axis: 0=X, 1=Y, 2=Z
	u           int  // tangent U axis
	v           int  // tangent V axis
	step        int  // +1 or -1 along d
	planeOffset int  // 1 for positive face, 0 for negative face
	ccwNormal   bool // true when e_u x e_v == outward normal
}

var faceSpecs = [FaceCount]faceAxisSpec{
	// X axis: u=Z, v=Y, e_u x e_v = Z x Y = -X
	FacePosX: {d: 0, u: 2, v: 1, step: 1, planeOffset: 1, ccwNormal: false},
	FaceNegX: {d: 0, u: 2, v: 1, step: -1, planeOffset: 0, ccwNormal: true},
	// Y axis: u=X, v=Z, e_u x e_v = X x Z = -Y
	FacePosY: {d: 1, u: 0, v: 2, step: 1, planeOffset: 1, ccwNormal: false},
	FaceNegY: {d: 1, u: 0, v: 2, step: -1, planeOffset: 0, ccwNormal: true},
	// Z axis: u=X, v=Y, e_u x e_v = X x Y = +Z
	FacePosZ: {d: 2, u: 0, v: 1, step: 1, planeOffset: 1, ccwNormal: true},
	FaceNegZ: {d: 2, u: 0, v: 1, step: -1, planeOffset: 0, ccwNormal: false},
}

func (m *Mesher) meshFaceDir(
	w *World,
	face Face,
	n int,
	vs float32,
	baseOffset math3d.Vec3,
	opts MeshOptions,
	positions *[]math3d.Vec3,
	normals *[]math3d.Vec3,
	uvs *[]math3d.Vec2,
	aos *[]uint8,
) {
	spec := faceSpecs[face]
	normal := faceNormals[face]
	p := m.padSize
	strides := [3]int{1, p * p, p} // index = (py*p + pz)*p + px => dx=1, dy=p*p, dz=p
	dStride := strides[spec.d]
	uStride := strides[spec.u]
	vStride := strides[spec.v]
	nStep := spec.step * dStride

	mask := m.mask[:n*n]
	padded := m.padded
	opaque := m.opaque
	reg := w.registry

	// Base index in padded array for local (0, 0, 0) is (1, 1, 1).
	originPad := dStride + uStride + vStride

	for s := range n {
		sliceBase := originPad + s*dStride
		anyVisible := false
		clear(mask)

		for v := range n {
			rowBase := sliceBase + v*vStride
			maskRow := v * n
			for u := range n {
				curIdx := rowBase + u*uStride
				block := padded[curIdx]
				if block == Air {
					continue
				}
				nbIdx := curIdx + nStep
				nb := padded[nbIdx]
				if nb != Air {
					if opaque[nbIdx] {
						continue
					}
					if !opaque[curIdx] && nb == block {
						continue
					}
				}
				var key uint32
				if opts.AmbientOcclusion {
					a0 := vertexAO(opaque[nbIdx-uStride], opaque[nbIdx-vStride], opaque[nbIdx-uStride-vStride])
					a1 := vertexAO(opaque[nbIdx+uStride], opaque[nbIdx-vStride], opaque[nbIdx+uStride-vStride])
					a2 := vertexAO(opaque[nbIdx+uStride], opaque[nbIdx+vStride], opaque[nbIdx+uStride+vStride])
					a3 := vertexAO(opaque[nbIdx-uStride], opaque[nbIdx+vStride], opaque[nbIdx-uStride+vStride])
					aoPacked := uint32(a0) | uint32(a1)<<2 | uint32(a2)<<4 | uint32(a3)<<6
					key = uint32(block) | (aoPacked << 16)
				} else {
					key = uint32(block)
				}
				mask[maskRow+u] = key
				anyVisible = true
			}
		}

		if !anyVisible {
			continue
		}

		planePos := float32(s+spec.planeOffset) * vs

		for v := 0; v < n; v++ {
			for u := 0; u < n; {
				cell := mask[v*n+u]
				if cell == 0 {
					u++
					continue
				}

				qw := 1
				qh := 1
				if !opts.DisableGreedy {
					for u+qw < n && mask[v*n+u+qw] == cell {
						qw++
					}
					done := false
					for v+qh < n && !done {
						row := (v + qh) * n
						for k := 0; k < qw; k++ {
							if mask[row+u+k] != cell {
								done = true
								break
							}
						}
						if !done {
							qh++
						}
					}
				}

				// Clear consumed cells from the mask.
				for dv := 0; dv < qh; dv++ {
					row := (v + dv) * n
					for du := 0; du < qw; du++ {
						mask[row+u+du] = 0
					}
				}

				// Emit one quad.
				baseVert := uint32(len(*positions))
				u0 := float32(u) * vs
				u1 := float32(u+qw) * vs
				v0 := float32(v) * vs
				v1 := float32(v+qh) * vs

				var p0, p1, p2, p3 [3]float32
				p0[spec.d], p0[spec.u], p0[spec.v] = planePos, u0, v0
				p1[spec.d], p1[spec.u], p1[spec.v] = planePos, u1, v0
				p2[spec.d], p2[spec.u], p2[spec.v] = planePos, u1, v1
				p3[spec.d], p3[spec.u], p3[spec.v] = planePos, u0, v1

				*positions = append(*positions,
					math3d.Vec3{X: p0[0] + baseOffset.X, Y: p0[1] + baseOffset.Y, Z: p0[2] + baseOffset.Z},
					math3d.Vec3{X: p1[0] + baseOffset.X, Y: p1[1] + baseOffset.Y, Z: p1[2] + baseOffset.Z},
					math3d.Vec3{X: p2[0] + baseOffset.X, Y: p2[1] + baseOffset.Y, Z: p2[2] + baseOffset.Z},
					math3d.Vec3{X: p3[0] + baseOffset.X, Y: p3[1] + baseOffset.Y, Z: p3[2] + baseOffset.Z},
				)
				*normals = append(*normals, normal, normal, normal, normal)

				uvW, uvH := float32(qw), float32(qh)
				if opts.UVMode == UVUnit {
					uvW, uvH = 1, 1
				}
				*uvs = append(*uvs,
					math3d.Vec2{X: 0, Y: 0},
					math3d.Vec2{X: uvW, Y: 0},
					math3d.Vec2{X: uvW, Y: uvH},
					math3d.Vec2{X: 0, Y: uvH},
				)

				flipDiag := false
				if opts.AmbientOcclusion {
					aoPacked := cell >> 16
					a0 := uint8(aoPacked & 3)
					a1 := uint8((aoPacked >> 2) & 3)
					a2 := uint8((aoPacked >> 4) & 3)
					a3 := uint8((aoPacked >> 6) & 3)
					*aos = append(*aos, a0, a1, a2, a3)
					// Flip diagonal so AO interpolates across the brighter pair.
					if a0+a2 < a1+a3 {
						flipDiag = true
					}
				}

				blockID := BlockID(cell & 0xFFFF)
				matID := reg.MaterialID(blockID)
				bucket := m.matIndices[matID]
				if len(bucket) == 0 {
					m.matOrder = append(m.matOrder, matID)
				}
				if spec.ccwNormal {
					if !flipDiag {
						bucket = append(bucket,
							baseVert+0, baseVert+1, baseVert+2,
							baseVert+0, baseVert+2, baseVert+3,
						)
					} else {
						bucket = append(bucket,
							baseVert+0, baseVert+1, baseVert+3,
							baseVert+1, baseVert+2, baseVert+3,
						)
					}
				} else {
					if !flipDiag {
						bucket = append(bucket,
							baseVert+0, baseVert+2, baseVert+1,
							baseVert+0, baseVert+3, baseVert+2,
						)
					} else {
						bucket = append(bucket,
							baseVert+0, baseVert+3, baseVert+1,
							baseVert+1, baseVert+3, baseVert+2,
						)
					}
				}
				m.matIndices[matID] = bucket

				u += qw
			}
		}
	}
}

func vertexAO(side1, side2, corner bool) uint8 {
	if side1 && side2 {
		return 0
	}
	var occ uint8
	if side1 {
		occ++
	}
	if side2 {
		occ++
	}
	if corner {
		occ++
	}
	return 3 - occ
}
