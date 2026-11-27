package gltf

import (
	"math"

	"github.com/nsgo-dev/game/pkg/math3d"
)

// MorphResult holds the evaluated vertex attributes and object-space bounding
// box for a morphed [Primitive]. Reuse one MorphResult across frames to
// evaluate morph targets with zero steady-state allocations.
type MorphResult struct {
	Positions []math3d.Vec3
	Normals   []math3d.Vec3
	Tangents  []math3d.Vec4
	Bounds    math3d.AABB
}

// EvaluateMorphTangents computes the morphed vertex tangents for prim given
// morph target weights:
//
//	out[i].xyz = normalize(prim.Tangents[i].xyz + sum_k(weights[k] * prim.Targets[k].Tangents[i]))
//	out[i].w   = prim.Tangents[i].w
//
// Per glTF 2.0, morph target TANGENT deltas displace only the XYZ components
// and leave the W handedness sign unchanged. If out has sufficient capacity it
// is reused without allocation; otherwise a new slice is allocated and returned.
func EvaluateMorphTangents(prim Primitive, weights []float32, out []math3d.Vec4) []math3d.Vec4 {
	n := len(prim.Tangents)
	if n == 0 {
		return out[:0]
	}
	if cap(out) >= n {
		out = out[:n]
	} else {
		out = make([]math3d.Vec4, n)
	}
	copy(out, prim.Tangents)

	limit := min(len(weights), len(prim.Targets))
	morphed := false
	for k := range limit {
		w := weights[k]
		if w == 0 {
			continue
		}
		deltas := prim.Targets[k].Tangents
		m := min(n, len(deltas))
		if m > 0 {
			morphed = true
		}
		for i := range m {
			out[i].X += deltas[i].X * w
			out[i].Y += deltas[i].Y * w
			out[i].Z += deltas[i].Z * w
		}
	}
	if morphed {
		for i := range out {
			v := math3d.Vec3{X: out[i].X, Y: out[i].Y, Z: out[i].Z}.Norm()
			out[i].X = v.X
			out[i].Y = v.Y
			out[i].Z = v.Z
		}
	}
	return out
}

// ApplyMorphWeights evaluates base's morph targets with weights into dst and
// returns dst (allocating one if dst is nil). It blends Positions, renormalizes
// blended Normals, blends and renormalizes Tangents.xyz while preserving
// Tangents.w handedness, and recomputes Bounds from the resulting Positions.
// Reusing dst across calls performs zero heap allocations once its slices are sized.
func ApplyMorphWeights(base Primitive, weights []float32, dst *MorphResult) *MorphResult {
	if dst == nil {
		dst = &MorphResult{}
	}
	dst.Positions = EvaluateMorphPositions(base, weights, dst.Positions[:0])
	dst.Normals = EvaluateMorphNormals(base, weights, dst.Normals[:0])
	dst.Tangents = EvaluateMorphTangents(base, weights, dst.Tangents[:0])
	dst.Bounds = computeAABB(dst.Positions)
	return dst
}

// ResolveWeights returns the effective morph target weights for doc.Nodes[nodeIndex]
// following glTF 2.0 precedence: non-empty node.Weights override mesh.Weights,
// and missing weights default to zeros matching the mesh's morph target count.
// Returns nil when doc is nil, nodeIndex is out of range, the node has no mesh,
// or the mesh has no morph targets.
func ResolveWeights(doc *Document, nodeIndex int) []float32 {
	out := ResolveWeightsInto(doc, nodeIndex, nil)
	if len(out) == 0 {
		return nil
	}
	return out
}

// ResolveWeightsInto resolves the effective morph target weights for
// doc.Nodes[nodeIndex] into dst following glTF 2.0 precedence (node.Weights
// overrides mesh.Weights, else zeros matching the target count). If dst has
// sufficient capacity it is reused without allocation.
func ResolveWeightsInto(doc *Document, nodeIndex int, dst []float32) []float32 {
	if doc == nil || nodeIndex < 0 || nodeIndex >= len(doc.Nodes) {
		return dst[:0]
	}
	node := &doc.Nodes[nodeIndex]
	if node.Mesh < 0 || node.Mesh >= len(doc.Meshes) {
		return dst[:0]
	}
	mesh := &doc.Meshes[node.Mesh]
	if len(mesh.Primitives) == 0 {
		return dst[:0]
	}
	targetCount := len(mesh.Primitives[0].Targets)
	if targetCount == 0 {
		return dst[:0]
	}

	if cap(dst) >= targetCount {
		dst = dst[:targetCount]
	} else {
		dst = make([]float32, targetCount)
	}
	clear(dst)

	switch {
	case len(node.Weights) > 0:
		copy(dst, node.Weights)
	case len(mesh.Weights) > 0:
		copy(dst, mesh.Weights)
	}
	return dst
}

// SampleMorphWeights evaluates the [PathWeights] channel targeting nodeIndex in
// anim at time t (in seconds) into dst and reports whether a matching channel
// was found and sampled.
//
// It supports [InterpolationStep], [InterpolationLinear], and
// [InterpolationCubicSpline] (unpacking 3*N values per keyframe in glTF order:
// in-tangent, value, out-tangent), clamping outside the channel's keyframe time
// range. If dst has sufficient capacity it is reused without allocation.
func SampleMorphWeights(anim Animation, nodeIndex int, t float32, dst []float32) ([]float32, bool) {
	var ch *AnimationChannel
	for i := range anim.Channels {
		c := &anim.Channels[i]
		if c.TargetNode == nodeIndex && c.Path == PathWeights {
			ch = c
			break
		}
	}
	if ch == nil {
		return dst[:0], false
	}

	n := len(ch.Times)
	stride := ch.Stride
	if n == 0 || stride <= 0 {
		return dst[:0], false
	}

	isCubic := ch.Interpolation == InterpolationCubicSpline
	keyMult := 1
	if isCubic {
		keyMult = 3
	}
	if len(ch.Values) < n*keyMult*stride {
		return dst[:0], false
	}

	if cap(dst) >= stride {
		dst = dst[:stride]
	} else {
		dst = make([]float32, stride)
	}

	valueOffset := func(key int) int {
		if isCubic {
			return (key*3 + 1) * stride
		}
		return key * stride
	}

	if math.IsNaN(float64(t)) || t <= ch.Times[0] || n == 1 {
		off := valueOffset(0)
		copy(dst, ch.Values[off:off+stride])
		return dst, true
	}
	if t >= ch.Times[n-1] {
		off := valueOffset(n - 1)
		copy(dst, ch.Values[off:off+stride])
		return dst, true
	}

	// Binary search for largest i with Times[i] <= t < Times[i+1].
	lo, hi := 0, n-1
	for lo+1 < hi {
		mid := int(uint(lo+hi) >> 1)
		if ch.Times[mid] <= t {
			lo = mid
		} else {
			hi = mid
		}
	}
	i := lo

	if ch.Interpolation == InterpolationStep {
		off := valueOffset(i)
		copy(dst, ch.Values[off:off+stride])
		return dst, true
	}

	dt := ch.Times[i+1] - ch.Times[i]
	if dt <= 0 {
		off := valueOffset(i)
		copy(dst, ch.Values[off:off+stride])
		return dst, true
	}
	s := math3d.Clamp((t-ch.Times[i])/dt, 0, 1)

	if isCubic {
		v0Off := (i*3 + 1) * stride
		b0Off := (i*3 + 2) * stride
		a1Off := ((i+1)*3 + 0) * stride
		v1Off := ((i+1)*3 + 1) * stride

		s2 := s * s
		s3 := s2 * s
		h00 := 2*s3 - 3*s2 + 1
		h10 := s3 - 2*s2 + s
		h01 := -2*s3 + 3*s2
		h11 := s3 - s2

		for k := range stride {
			v0 := ch.Values[v0Off+k]
			b0 := ch.Values[b0Off+k]
			a1 := ch.Values[a1Off+k]
			v1 := ch.Values[v1Off+k]
			dst[k] = h00*v0 + h10*dt*b0 + h01*v1 + h11*dt*a1
		}
		return dst, true
	}

	// LINEAR interpolation (default).
	off0 := i * stride
	off1 := (i + 1) * stride
	for k := range stride {
		dst[k] = math3d.Lerp(ch.Values[off0+k], ch.Values[off1+k], s)
	}
	return dst, true
}
