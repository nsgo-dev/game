package gltf

import (
	"encoding/json"

	"github.com/nsgo-dev/game/pkg/math3d"
)

// PrimitiveMode specifies the topology of a mesh primitive.
type PrimitiveMode int

const (
	ModePoints        PrimitiveMode = 0
	ModeLines         PrimitiveMode = 1
	ModeLineLoop      PrimitiveMode = 2
	ModeLineStrip     PrimitiveMode = 3
	ModeTriangles     PrimitiveMode = 4
	ModeTriangleStrip PrimitiveMode = 5
	ModeTriangleFan   PrimitiveMode = 6
)

// MorphTarget holds vertex attribute displacement deltas for a single morph target.
type MorphTarget struct {
	Positions []math3d.Vec3
	Normals   []math3d.Vec3
	Tangents  []math3d.Vec3
}

// Primitive holds decoded geometry attributes, indices, bounding volume,
// morph targets, and KHR_materials_variants mappings for a single draw primitive.
// Material is -1 when no default material is assigned.
type Primitive struct {
	Mode            PrimitiveMode
	Material        int
	Positions       []math3d.Vec3
	Normals         []math3d.Vec3
	Tangents        []math3d.Vec4
	TexCoords0      []math3d.Vec2
	TexCoords1      []math3d.Vec2
	Colors0         []math3d.Vec4
	Joints0         [][4]uint16
	Weights0        []math3d.Vec4
	Indices         []uint32
	Bounds          math3d.AABB
	Targets         []MorphTarget
	VariantMappings map[int]int
	Attributes      map[string]int
	Extras          json.RawMessage
}

// Mesh groups one or more geometry primitives and optional default morph weights.
type Mesh struct {
	Name       string
	Primitives []Primitive
	Weights    []float32
	Extras     json.RawMessage
}

// Skin defines skeletal joints and their inverse bind matrices.
// Skeleton is -1 when unset.
type Skin struct {
	Name                string
	Joints              []int
	Skeleton            int
	InverseBindMatrices []math3d.Mat4
	Extras              json.RawMessage
}

// EvaluateMorphPositions computes the morphed vertex positions for prim given
// morph target weights:
//
//	out[i] = prim.Positions[i] + sum_k(weights[k] * prim.Targets[k].Positions[i])
//
// If out has sufficient capacity it is reused without allocation; otherwise a
// new slice is allocated and returned.
func EvaluateMorphPositions(prim Primitive, weights []float32, out []math3d.Vec3) []math3d.Vec3 {
	n := len(prim.Positions)
	if n == 0 {
		return out[:0]
	}
	if cap(out) >= n {
		out = out[:n]
	} else {
		out = make([]math3d.Vec3, n)
	}
	copy(out, prim.Positions)

	limit := min(len(weights), len(prim.Targets))
	for k := range limit {
		w := weights[k]
		if w == 0 {
			continue
		}
		deltas := prim.Targets[k].Positions
		m := min(n, len(deltas))
		for i := range m {
			out[i] = out[i].Add(deltas[i].Scale(w))
		}
	}
	return out
}

// EvaluateMorphNormals computes the morphed and re-normalized vertex normals
// for prim given morph target weights. If out has sufficient capacity it is
// reused without allocation.
func EvaluateMorphNormals(prim Primitive, weights []float32, out []math3d.Vec3) []math3d.Vec3 {
	n := len(prim.Normals)
	if n == 0 {
		return out[:0]
	}
	if cap(out) >= n {
		out = out[:n]
	} else {
		out = make([]math3d.Vec3, n)
	}
	copy(out, prim.Normals)

	limit := min(len(weights), len(prim.Targets))
	morphed := false
	for k := range limit {
		w := weights[k]
		if w == 0 {
			continue
		}
		deltas := prim.Targets[k].Normals
		m := min(n, len(deltas))
		if m > 0 {
			morphed = true
		}
		for i := range m {
			out[i] = out[i].Add(deltas[i].Scale(w))
		}
	}
	if morphed {
		for i := range out {
			out[i] = out[i].Norm()
		}
	}
	return out
}

func computeAABB(positions []math3d.Vec3) math3d.AABB {
	box := math3d.AABBEmpty()
	for _, p := range positions {
		box = box.ExpandPoint(p)
	}
	return box
}
