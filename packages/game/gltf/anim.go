package gltf

import (
	"encoding/json"
	"math"
	"sort"

	"github.com/nsgo-dev/game/packages/game/math3d"
)

// Interpolation defines the keyframe interpolation mode for an animation channel.
type Interpolation string

const (
	InterpolationLinear      Interpolation = "LINEAR"
	InterpolationStep        Interpolation = "STEP"
	InterpolationCubicSpline Interpolation = "CUBICSPLINE"
)

// TargetPath identifies which node property an animation channel drives.
type TargetPath string

const (
	PathTranslation TargetPath = "translation"
	PathRotation    TargetPath = "rotation"
	PathScale       TargetPath = "scale"
	PathWeights     TargetPath = "weights"
)

// AnimationChannel holds a single decoded keyframe track targeting one node property.
// For InterpolationCubicSpline, Values stores 3*Stride floats per keyframe in
// glTF order: [inTangent, value, outTangent].
type AnimationChannel struct {
	TargetNode    int
	Path          TargetPath
	Interpolation Interpolation
	Times         []float32
	Values        []float32
	Stride        int
}

// ChannelSample holds the evaluated value of one animation channel at time t.
type ChannelSample struct {
	Node        int
	Path        TargetPath
	Translation math3d.Vec3
	Rotation    math3d.Quat
	Scale       math3d.Vec3
	Weights     []float32
}

// Animation represents a glTF 2.0 keyframe animation clip.
type Animation struct {
	Name     string
	Duration float32
	Channels []AnimationChannel
	Extras   json.RawMessage
}

// Sample evaluates all channels of the animation at time t (in seconds) and
// returns one ChannelSample per channel with a valid TargetNode (>= 0).
func (a *Animation) Sample(t float32) []ChannelSample {
	if len(a.Channels) == 0 {
		return nil
	}
	out := make([]ChannelSample, 0, len(a.Channels))
	for i := range a.Channels {
		ch := &a.Channels[i]
		if ch.TargetNode < 0 {
			continue
		}
		out = append(out, ch.Sample(t))
	}
	return out
}

// Apply evaluates the animation at time t and writes sampled translation,
// rotation, scale, and morph target weights directly onto matching nodes in doc.
// Call doc.ComputeWorldMatrices afterwards to propagate updated transforms.
func (a *Animation) Apply(doc *Document, t float32) {
	if doc == nil {
		return
	}
	for i := range a.Channels {
		ch := &a.Channels[i]
		if ch.TargetNode < 0 || ch.TargetNode >= len(doc.Nodes) {
			continue
		}
		s := ch.Sample(t)
		node := &doc.Nodes[ch.TargetNode]
		switch s.Path {
		case PathTranslation:
			node.Translation = s.Translation
			node.HasMatrix = false
		case PathRotation:
			node.Rotation = s.Rotation
			node.HasMatrix = false
		case PathScale:
			node.Scale = s.Scale
			node.HasMatrix = false
		case PathWeights:
			node.Weights = append(node.Weights[:0], s.Weights...)
		}
	}
}

// Sample evaluates this channel at time t (in seconds), clamping to the first
// and last keyframes outside the channel's time range.
func (ch *AnimationChannel) Sample(t float32) ChannelSample {
	sample := ChannelSample{
		Node: ch.TargetNode,
		Path: ch.Path,
	}
	n := len(ch.Times)
	stride := ch.Stride
	if n == 0 || stride <= 0 {
		return sample
	}

	isCubic := ch.Interpolation == InterpolationCubicSpline
	keyMultiplier := 1
	if isCubic {
		keyMultiplier = 3
	}
	if len(ch.Values) < n*keyMultiplier*stride {
		return sample
	}

	valueOffset := func(key int) int {
		if isCubic {
			return (key*3 + 1) * stride
		}
		return key * stride
	}

	if math.IsNaN(float64(t)) || t <= ch.Times[0] || n == 1 {
		return fillSample(sample, ch.Values[valueOffset(0):valueOffset(0)+stride])
	}
	if t >= ch.Times[n-1] {
		off := valueOffset(n - 1)
		return fillSample(sample, ch.Values[off:off+stride])
	}

	// Find largest i such that Times[i] <= t < Times[i+1].
	i := sort.Search(n, func(k int) bool { return ch.Times[k] > t }) - 1
	if i < 0 {
		i = 0
	}
	if i >= n-1 {
		i = n - 2
	}

	if ch.Interpolation == InterpolationStep {
		off := valueOffset(i)
		return fillSample(sample, ch.Values[off:off+stride])
	}

	dt := ch.Times[i+1] - ch.Times[i]
	if dt <= 0 {
		off := valueOffset(i)
		return fillSample(sample, ch.Values[off:off+stride])
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

		buf := make([]float32, stride)
		for k := range stride {
			v0 := ch.Values[v0Off+k]
			b0 := ch.Values[b0Off+k]
			a1 := ch.Values[a1Off+k]
			v1 := ch.Values[v1Off+k]
			buf[k] = h00*v0 + h10*dt*b0 + h01*v1 + h11*dt*a1
		}
		return fillSample(sample, buf)
	}

	// LINEAR interpolation.
	off0 := i * stride
	off1 := (i + 1) * stride
	v0 := ch.Values[off0 : off0+stride]
	v1 := ch.Values[off1 : off1+stride]

	switch ch.Path {
	case PathTranslation:
		if stride >= 3 {
			a := math3d.Vec3{X: v0[0], Y: v0[1], Z: v0[2]}
			b := math3d.Vec3{X: v1[0], Y: v1[1], Z: v1[2]}
			sample.Translation = a.Lerp(b, s)
		}
	case PathScale:
		if stride >= 3 {
			a := math3d.Vec3{X: v0[0], Y: v0[1], Z: v0[2]}
			b := math3d.Vec3{X: v1[0], Y: v1[1], Z: v1[2]}
			sample.Scale = a.Lerp(b, s)
		}
	case PathRotation:
		if stride >= 4 {
			a := math3d.Quat{X: v0[0], Y: v0[1], Z: v0[2], W: v0[3]}
			b := math3d.Quat{X: v1[0], Y: v1[1], Z: v1[2], W: v1[3]}
			sample.Rotation = a.Slerp(b, s)
		}
	case PathWeights:
		w := make([]float32, stride)
		for k := range stride {
			w[k] = math3d.Lerp(v0[k], v1[k], s)
		}
		sample.Weights = w
	}
	return sample
}

func fillSample(sample ChannelSample, vals []float32) ChannelSample {
	switch sample.Path {
	case PathTranslation:
		if len(vals) >= 3 {
			sample.Translation = math3d.Vec3{X: vals[0], Y: vals[1], Z: vals[2]}
		}
	case PathScale:
		if len(vals) >= 3 {
			sample.Scale = math3d.Vec3{X: vals[0], Y: vals[1], Z: vals[2]}
		}
	case PathRotation:
		if len(vals) >= 4 {
			sample.Rotation = math3d.Quat{X: vals[0], Y: vals[1], Z: vals[2], W: vals[3]}.Norm()
		}
	case PathWeights:
		w := make([]float32, len(vals))
		copy(w, vals)
		sample.Weights = w
	}
	return sample
}
