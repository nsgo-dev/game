package render3d

import (
	"cmp"
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/math3d"
)

// BlendMode controls how a [DrawItem] is partitioned and sorted in the render
// queue (#245, #248).
type BlendMode uint8

const (
	// BlendOpaque renders fully opaque geometry with depth writes enabled,
	// sorted by pipeline/material/mesh and front-to-back depth.
	BlendOpaque BlendMode = iota
	// BlendAlphaMask renders alpha-tested cutout geometry in the opaque pass
	// (separate batch key from BlendOpaque).
	BlendAlphaMask
	// BlendAlphaBlend renders translucent geometry strictly back-to-front by
	// distance from the camera.
	BlendAlphaBlend
)

// String returns a human-readable blend mode name.
func (b BlendMode) String() string {
	switch b {
	case BlendOpaque:
		return "opaque"
	case BlendAlphaMask:
		return "alpha_mask"
	case BlendAlphaBlend:
		return "alpha_blend"
	default:
		return "unknown"
	}
}

// DrawItem describes a single renderable 3D object submitted to [Queue.Build].
type DrawItem struct {
	ID          uint64
	MeshID      uint32
	MaterialID  uint32
	PipelineID  uint32
	Blend       BlendMode
	Transform   math3d.Mat4
	WorldBounds math3d.AABB
	Triangles   uint32
	Vertices    uint32
	LODs        []LODLevel // optional; ordered from finest (index 0) to coarsest
}

// ResolvedDraw is a visible draw item after frustum culling and LOD selection.
type ResolvedDraw struct {
	ID         uint64
	MeshID     uint32
	MaterialID uint32
	PipelineID uint32
	Blend      BlendMode
	LODIndex   int // selected index into DrawItem.LODs, or -1 if LODs is empty
	Transform  math3d.Mat4
	DistanceSq float32
	Triangles  uint32
	Vertices   uint32
}

// InstanceBatch groups consecutive opaque/alpha-mask draws sharing an identical
// (PipelineID, MaterialID, MeshID, Blend) state key so a GPU backend can submit
// them in a single instanced draw call (#246).
type InstanceBatch struct {
	PipelineID uint32
	MaterialID uint32
	MeshID     uint32
	Blend      BlendMode
	Transforms []math3d.Mat4
	Triangles  uint32 // per-instance triangle count
	Vertices   uint32 // per-instance vertex count
}

// RenderPlan is the deterministic CPU draw plan produced by [Queue.Build].
//
// Its slices alias internal buffers owned by [Queue] to keep steady-state frame
// builds allocation-free; they remain valid until the next call to [Queue.Build].
type RenderPlan struct {
	Opaque      []ResolvedDraw
	Batches     []InstanceBatch
	Transparent []ResolvedDraw
	Stats       RenderStats
}

// Queue performs frustum culling, LOD selection, opaque sorting, instanced
// batching, back-to-front transparency sorting, and statistics accumulation
// using reusable internal storage (0 allocs/op in steady state).
type Queue struct {
	// LODHysteresis is the relative deadband [0, 0.5] used by [SelectLOD] when
	// DrawItem.ID != 0 to prevent LOD popping near distance/screen thresholds.
	LODHysteresis float32
	// FrustumNO is kept for source compatibility and ignored: [Queue.Build]
	// always extracts the [-1, 1] form, which is conservative for both depth
	// conventions. A [0, 1] extraction of an OpenGL [-1, 1] matrix, such as
	// [math3d.OrthoNO] or [math3d.PerspectiveNO], puts the near plane halfway
	// into the view and culls visible geometry; the [-1, 1] form only admits
	// a thin slab in front of a [0, 1] projection's near plane, which the
	// depth test still discards.
	FrustumNO bool
	// DisableInstancing emits one InstanceBatch per opaque draw when true.
	DisableInstancing bool
	// Occluder is an optional software depth-buffer / Hi-Z occlusion buffer
	// (#380). When non-nil, [Queue.Build] skips frustum-visible items whose
	// WorldBounds fail [OcclusionBuffer.TestAABB] and records them in
	// [RenderStats.Occluded].
	Occluder *OcclusionBuffer
	// TextureBytes and BufferBytes are optional resident GPU memory totals
	// copied into RenderPlan.Stats for budget checking.
	TextureBytes uint64
	BufferBytes  uint64

	opaque      []ResolvedDraw
	transparent []ResolvedDraw
	batches     []InstanceBatch
	transforms  []math3d.Mat4
	prevLOD     map[uint64]int
}

// ResetLODHistory clears cached per-ID LOD indices used for hysteresis.
func (q *Queue) ResetLODHistory() {
	clear(q.prevLOD)
}

// Build culls items against the frustum of viewProj (and against [Queue.Occluder]
// when non-nil), selects active LODs, sorts opaque and transparent queues,
// builds instanced batches, and computes exact [RenderStats].
func (q *Queue) Build(viewProj math3d.Mat4, camPos math3d.Vec3, items []DrawItem) RenderPlan {
	return q.BuildWithOcclusion(viewProj, camPos, items, q.Occluder)
}

// BuildWithOcclusion performs the same culling, LOD selection, sorting, and
// batching pipeline as [Queue.Build] using the provided [OcclusionBuffer]
// (which may be nil to disable occlusion culling). Items that pass frustum
// culling but fail [OcclusionBuffer.TestAABB] are skipped and counted in both
// [RenderStats.Occluded] and [RenderStats.CulledItems].
func (q *Queue) BuildWithOcclusion(viewProj math3d.Mat4, camPos math3d.Vec3, items []DrawItem, occluder *OcclusionBuffer) RenderPlan {
	q.opaque = q.opaque[:0]
	q.transparent = q.transparent[:0]
	q.batches = q.batches[:0]
	q.transforms = q.transforms[:0]

	// Always use the [-1, 1] extraction: it is correct for NO projections and
	// conservative for ZO ones, while the [0, 1] extraction silently culls the
	// near half of an NO projection (#546).
	frustum := math3d.FrustumFromMat4NO(viewProj)

	stats := RenderStats{
		TotalItems:   len(items),
		TextureBytes: q.TextureBytes,
		BufferBytes:  q.BufferBytes,
	}

	for i := range items {
		item := &items[i]
		if !frustum.IntersectsAABB(item.WorldBounds) {
			stats.CulledItems++
			continue
		}
		if occluder != nil && !occluder.TestAABB(item.WorldBounds, viewProj) {
			stats.CulledItems++
			stats.Occluded++
			continue
		}

		center := item.WorldBounds.Center()
		distSq := center.DistSq(camPos)

		meshID := item.MeshID
		tris := item.Triangles
		verts := item.Vertices
		lodIdx := -1
		if len(item.LODs) > 0 {
			dist := float32(math.Sqrt(float64(distSq)))
			screenRatio := ProjectScreenRatio(item.WorldBounds, viewProj)
			prev := -1
			if q.LODHysteresis > 0 && item.ID != 0 && q.prevLOD != nil {
				if p, ok := q.prevLOD[item.ID]; ok {
					prev = p
				}
			}
			lodIdx = SelectLOD(item.LODs, dist, screenRatio, q.LODHysteresis, prev)
			if lodIdx >= 0 {
				lv := item.LODs[lodIdx]
				meshID = lv.MeshID
				if lv.Triangles > 0 || lv.Vertices > 0 {
					tris = lv.Triangles
					verts = lv.Vertices
				}
				if q.LODHysteresis > 0 && item.ID != 0 {
					if q.prevLOD == nil {
						q.prevLOD = make(map[uint64]int)
					}
					q.prevLOD[item.ID] = lodIdx
				}
			}
		}

		draw := ResolvedDraw{
			ID:         item.ID,
			MeshID:     meshID,
			MaterialID: item.MaterialID,
			PipelineID: item.PipelineID,
			Blend:      item.Blend,
			LODIndex:   lodIdx,
			Transform:  item.Transform,
			DistanceSq: distSq,
			Triangles:  tris,
			Vertices:   verts,
		}

		stats.Triangles += uint64(tris)
		stats.Vertices += uint64(verts)

		if item.Blend == BlendAlphaBlend {
			q.transparent = append(q.transparent, draw)
		} else {
			q.opaque = append(q.opaque, draw)
		}
	}

	// Sort opaque draws to minimize pipeline/material/mesh state changes and
	// draw front-to-back within each state group (#245).
	slices.SortFunc(q.opaque, cmpOpaque)

	// Sort transparent draws strictly back-to-front by distance from camPos (#248).
	slices.SortFunc(q.transparent, cmpTransparent)

	// Populate contiguous transform buffer first so batch subslices never alias
	// a reallocated backing array.
	for i := range q.opaque {
		q.transforms = append(q.transforms, q.opaque[i].Transform)
	}

	// Group consecutive identical (PipelineID, MaterialID, MeshID, Blend) opaque
	// draws into InstanceBatches (#246).
	for i := 0; i < len(q.opaque); {
		first := q.opaque[i]
		end := i + 1
		if !q.DisableInstancing {
			for end < len(q.opaque) && canInstanceTogether(first, q.opaque[end]) {
				end++
			}
		}
		q.batches = append(q.batches, InstanceBatch{
			PipelineID: first.PipelineID,
			MaterialID: first.MaterialID,
			MeshID:     first.MeshID,
			Blend:      first.Blend,
			Transforms: q.transforms[i:end:end],
			Triangles:  first.Triangles,
			Vertices:   first.Vertices,
		})
		i = end
	}

	stats.OpaqueDraws = len(q.opaque)
	stats.TransparentDraws = len(q.transparent)
	stats.VisibleItems = stats.OpaqueDraws + stats.TransparentDraws
	stats.InstancedBatches = len(q.batches)
	stats.TotalDrawCalls = stats.InstancedBatches + stats.TransparentDraws

	// Count exact pipeline and material state switches across submission order
	// (instanced opaque pass followed by back-to-front transparent pass).
	var (
		hasState bool
		curPipe  uint32
		curMat   uint32
	)
	recordState := func(pipe, mat uint32) {
		if !hasState {
			hasState = true
			curPipe = pipe
			curMat = mat
			return
		}
		if pipe != curPipe {
			stats.PipelineSwitches++
			curPipe = pipe
		}
		if mat != curMat {
			stats.MaterialSwitches++
			curMat = mat
		}
	}
	for i := range q.batches {
		recordState(q.batches[i].PipelineID, q.batches[i].MaterialID)
	}
	for i := range q.transparent {
		recordState(q.transparent[i].PipelineID, q.transparent[i].MaterialID)
	}

	return RenderPlan{
		Opaque:      q.opaque,
		Batches:     q.batches,
		Transparent: q.transparent,
		Stats:       stats,
	}
}

func canInstanceTogether(a, b ResolvedDraw) bool {
	return a.PipelineID == b.PipelineID &&
		a.MaterialID == b.MaterialID &&
		a.MeshID == b.MeshID &&
		a.Blend == b.Blend
}

func cmpOpaque(a, b ResolvedDraw) int {
	if c := cmp.Compare(a.PipelineID, b.PipelineID); c != 0 {
		return c
	}
	if c := cmp.Compare(a.MaterialID, b.MaterialID); c != 0 {
		return c
	}
	if c := cmp.Compare(a.MeshID, b.MeshID); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Blend, b.Blend); c != 0 {
		return c
	}
	if c := cmp.Compare(a.DistanceSq, b.DistanceSq); c != 0 {
		return c
	}
	return cmp.Compare(a.ID, b.ID)
}

func cmpTransparent(a, b ResolvedDraw) int {
	// Strictly back-to-front: larger DistanceSq comes first.
	if c := cmp.Compare(b.DistanceSq, a.DistanceSq); c != 0 {
		return c
	}
	if c := cmp.Compare(a.PipelineID, b.PipelineID); c != 0 {
		return c
	}
	if c := cmp.Compare(a.MaterialID, b.MaterialID); c != 0 {
		return c
	}
	if c := cmp.Compare(a.MeshID, b.MeshID); c != 0 {
		return c
	}
	return cmp.Compare(a.ID, b.ID)
}
