package anim3d

import (
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/math3d"
)

// BoneMask stores per-joint blend weights in [0, 1] used to restrict an
// animation layer or additive pose to a subset of joints (e.g. upper body).
type BoneMask struct {
	Weights []float32
}

// NewBoneMask creates a BoneMask for jointCount joints initialized to
// defaultWeight (clamped to [0, 1]).
func NewBoneMask(jointCount int, defaultWeight float32) *BoneMask {
	if jointCount <= 0 {
		return &BoneMask{}
	}
	w := math3d.Clamp(defaultWeight, 0, 1)
	weights := make([]float32, jointCount)
	for i := range weights {
		weights[i] = w
	}
	return &BoneMask{Weights: weights}
}

// Clone returns a deep copy of m, or nil if m is nil.
func (m *BoneMask) Clone() *BoneMask {
	if m == nil {
		return nil
	}
	out := make([]float32, len(m.Weights))
	copy(out, m.Weights)
	return &BoneMask{Weights: out}
}

// Weight returns the clamped blend weight for joint idx.
// A nil BoneMask represents an unmasked weight of 1.0 for every joint.
// Out-of-range indices on a non-nil mask return 0.
func (m *BoneMask) Weight(idx int) float32 {
	if m == nil {
		return 1
	}
	if idx < 0 || idx >= len(m.Weights) {
		return 0
	}
	return math3d.Clamp(m.Weights[idx], 0, 1)
}

// SetJoint sets the blend weight for a single joint, clamped to [0, 1].
// Out-of-range indices are ignored.
func (m *BoneMask) SetJoint(idx int, weight float32) {
	if m == nil || idx < 0 || idx >= len(m.Weights) {
		return
	}
	m.Weights[idx] = math3d.Clamp(weight, 0, 1)
}

// SetBranch sets the blend weight (clamped to [0, 1]) for rootJoint and all of
// its descendants according to parents, where parents[j] is the parent joint
// index of joint j (or < 0 for root joints).
func (m *BoneMask) SetBranch(parents []int, rootJoint int, weight float32) {
	if m == nil || rootJoint < 0 || rootJoint >= len(m.Weights) {
		return
	}
	w := math3d.Clamp(weight, 0, 1)
	maxDepth := len(parents) + 1
	for j := range m.Weights {
		curr := j
		for range maxDepth {
			if curr == rootJoint {
				m.Weights[j] = w
				break
			}
			if curr < 0 || curr >= len(parents) {
				break
			}
			next := parents[curr]
			if next == curr {
				break
			}
			curr = next
		}
	}
}

// LayerBlendMode controls how an AnimLayer combines with lower layers in a LayerStack.
type LayerBlendMode int

const (
	// LayerOverride blends the layer pose over the accumulated pose using
	// linear interpolation for translation/scale and Slerp for rotation.
	LayerOverride LayerBlendMode = iota
	// LayerAdditive applies the layer pose as an additive delta over the
	// accumulated pose.
	LayerAdditive
)

// MakeAdditivePose computes the exact additive delta pose transforming reference
// into target:
//   - Pos:   target.Pos - reference.Pos
//   - Rot:   target.Rot * reference.Rot.Conj() (normalized)
//   - Scale: component-wise target.Scale / reference.Scale (1 when reference scale is 0)
func MakeAdditivePose(target Pose, reference Pose) Pose {
	n := max(len(target.Joints), len(reference.Joints))
	if n == 0 {
		return Pose{}
	}
	id := math3d.TransformIdentity()
	out := make([]math3d.Transform, n)
	for i := range n {
		tj := id
		if i < len(target.Joints) {
			tj = target.Joints[i]
		}
		rj := id
		if i < len(reference.Joints) {
			rj = reference.Joints[i]
		}

		tRot := tj.Rot.Norm()
		rRot := rj.Rot.Norm()
		deltaRot := tRot.Mul(rRot.Conj()).Norm()
		if deltaRot.W < 0 {
			deltaRot = math3d.Quat{X: -deltaRot.X, Y: -deltaRot.Y, Z: -deltaRot.Z, W: -deltaRot.W}
		}

		out[i] = math3d.Transform{
			Pos: tj.Pos.Sub(rj.Pos),
			Rot: deltaRot,
			Scale: math3d.Vec3{
				X: safeDivOne(tj.Scale.X, rj.Scale.X),
				Y: safeDivOne(tj.Scale.Y, rj.Scale.Y),
				Z: safeDivOne(tj.Scale.Z, rj.Scale.Z),
			},
		}
	}
	return Pose{Joints: out}
}

// ApplyAdditivePose applies an additive delta pose onto base, scaled by strength
// (clamped to [0, 1]) and multiplied per joint by mask (if non-nil).
//   - Pos:   base.Pos + additive.Pos * w
//   - Rot:   Slerp(Identity, additive.Rot, w) * base.Rot
//   - Scale: base.Scale * Lerp(Vec3One, additive.Scale, w)
func ApplyAdditivePose(base Pose, additive Pose, strength float32, mask *BoneMask) Pose {
	n := max(len(base.Joints), len(additive.Joints))
	if n == 0 {
		return Pose{}
	}
	s := math3d.Clamp(strength, 0, 1)
	id := math3d.TransformIdentity()
	qId := math3d.QuatIdentity()
	one := math3d.Vec3One()
	out := make([]math3d.Transform, n)

	for i := range n {
		bj := id
		if i < len(base.Joints) {
			bj = base.Joints[i]
		}
		w := s
		if mask != nil {
			w *= mask.Weight(i)
		}
		if w <= 0 || i >= len(additive.Joints) {
			out[i] = bj
			continue
		}
		aj := additive.Joints[i]
		scaledRot := qId.Slerp(aj.Rot.Norm(), w)
		scaledScale := one.Lerp(aj.Scale, w)
		out[i] = math3d.Transform{
			Pos:   bj.Pos.Add(aj.Pos.Scale(w)),
			Rot:   scaledRot.Mul(bj.Rot.Norm()).Norm(),
			Scale: bj.Scale.Mul(scaledScale),
		}
	}
	return Pose{Joints: out}
}

// BlendPosesMasked blends overlay over base by weight (clamped to [0, 1])
// multiplied per joint by mask (if non-nil).
func BlendPosesMasked(base, overlay Pose, weight float32, mask *BoneMask) Pose {
	n := max(len(base.Joints), len(overlay.Joints))
	if n == 0 {
		return Pose{}
	}
	baseW := math3d.Clamp(weight, 0, 1)
	id := math3d.TransformIdentity()
	out := make([]math3d.Transform, n)
	for i := range n {
		bj := id
		if i < len(base.Joints) {
			bj = base.Joints[i]
		}
		oj := id
		if i < len(overlay.Joints) {
			oj = overlay.Joints[i]
		}
		w := baseW
		if mask != nil {
			w *= mask.Weight(i)
		}
		switch {
		case w <= 0:
			out[i] = bj
		case w >= 1:
			out[i] = oj
		default:
			out[i] = bj.Lerp(oj, w)
		}
	}
	return Pose{Joints: out}
}

// AnimLayer configures a single layer inside a LayerStack.
//
// If Sampler is non-nil, it drives the layer's pose and time advancement;
// otherwise Clip is sampled at Time (in seconds) with Loop and Speed.
// For LayerAdditive layers, if ReferencePose is non-nil, MakeAdditivePose is
// computed relative to ReferencePose; if nil, the sampled pose is treated as an
// already-delta additive pose (equivalently, relative to identity).
type AnimLayer struct {
	Name          string
	Mode          LayerBlendMode
	Weight        float32
	Mask          *BoneMask
	Clip          *Clip
	ReferencePose *Pose
	Sampler       PoseSampler
	Time          float32 // playback time in seconds when using Clip
	Loop          bool
	Speed         float32 // playback speed multiplier; 0 defaults to 1.0
}

// LayerStack composes multiple ordered AnimLayers (base locomotion, masked
// upper-body overrides, and parameterized additive layers) into a single Pose.
type LayerStack struct {
	jointCount int
	layers     []AnimLayer
}

// NewLayerStack creates an empty LayerStack targeting skeletons with jointCount joints.
func NewLayerStack(jointCount int) *LayerStack {
	if jointCount < 0 {
		jointCount = 0
	}
	return &LayerStack{
		jointCount: jointCount,
	}
}

// AddLayer appends a layer to the top of the stack.
func (ls *LayerStack) AddLayer(layer AnimLayer) {
	if ls == nil {
		return
	}
	ls.layers = append(ls.layers, layer)
}

// Layer returns a pointer to the first layer matching name, or nil if not found.
func (ls *LayerStack) Layer(name string) *AnimLayer {
	if ls == nil {
		return nil
	}
	for i := range ls.layers {
		if ls.layers[i].Name == name {
			return &ls.layers[i]
		}
	}
	return nil
}

// RemoveLayer removes the first layer matching name. It returns true if removed.
func (ls *LayerStack) RemoveLayer(name string) bool {
	if ls == nil {
		return false
	}
	for i := range ls.layers {
		if ls.layers[i].Name == name {
			ls.layers = append(ls.layers[:i], ls.layers[i+1:]...)
			return true
		}
	}
	return false
}

// SetLayerWeight updates the Weight of the named layer (clamped to [0, 1]).
// It returns false if no layer with name exists.
func (ls *LayerStack) SetLayerWeight(name string, weight float32) bool {
	layer := ls.Layer(name)
	if layer == nil {
		return false
	}
	layer.Weight = math3d.Clamp(weight, 0, 1)
	return true
}

// LayerWeight returns the current Weight of the named layer, or 0 if not found.
func (ls *LayerStack) LayerWeight(name string) float32 {
	layer := ls.Layer(name)
	if layer == nil {
		return 0
	}
	return layer.Weight
}

// Update advances all layers by dt and returns the composed Pose.
func (ls *LayerStack) Update(dt time.Duration) Pose {
	if ls == nil {
		return Pose{}
	}
	if dt < 0 {
		dt = 0
	}
	for i := range ls.layers {
		layer := &ls.layers[i]
		scaled := scaleDuration(dt, layer.Speed)
		if layer.Sampler != nil {
			layer.Sampler.Update(scaled)
			continue
		}
		if layer.Clip != nil && layer.Clip.Duration > 0 {
			dur := layer.Clip.Duration.Seconds()
			next := float64(layer.Time) + scaled.Seconds()
			if layer.Loop {
				next = math.Mod(next, dur)
				if next < 0 {
					next += dur
				}
			} else {
				if next < 0 {
					next = 0
				}
				if next > dur {
					next = dur
				}
			}
			layer.Time = float32(next)
		}
	}
	return ls.Evaluate()
}

// Evaluate composes all active layers at their current times without advancing playback.
func (ls *LayerStack) Evaluate() Pose {
	if ls == nil {
		return Pose{}
	}
	pose := NewPose(ls.jointCount)
	for i := range ls.layers {
		layer := &ls.layers[i]
		w := math3d.Clamp(layer.Weight, 0, 1)
		if w <= 0 {
			continue
		}
		var layerPose Pose
		switch {
		case layer.Sampler != nil:
			layerPose = layer.Sampler.Pose()
		case layer.Clip != nil:
			layerPose = sampleLayerClip(layer.Clip, layer.Time, layer.Loop)
		default:
			continue
		}
		if len(pose.Joints) == 0 && len(layerPose.Joints) > 0 {
			pose = NewPose(len(layerPose.Joints))
		}
		switch layer.Mode {
		case LayerAdditive:
			addPose := layerPose
			if layer.ReferencePose != nil {
				addPose = MakeAdditivePose(layerPose, *layer.ReferencePose)
			}
			pose = ApplyAdditivePose(pose, addPose, w, layer.Mask)
		default: // LayerOverride
			pose = BlendPosesMasked(pose, layerPose, w, layer.Mask)
		}
	}
	return pose
}

// Pose evaluates the composed layer stack without advancing time (implements PoseSampler).
func (ls *LayerStack) Pose() Pose {
	return ls.Evaluate()
}

func sampleLayerClip(clip *Clip, tSec float32, loop bool) Pose {
	if clip == nil {
		return Pose{}
	}
	if clip.Duration <= 0 {
		return clip.sampleClamped(0)
	}
	dur := clip.Duration.Seconds()
	t := float64(tSec)
	if loop {
		t = math.Mod(t, dur)
		if t < 0 {
			t += dur
		}
	} else {
		if t < 0 {
			t = 0
		}
		if t > dur {
			t = dur
		}
	}
	return clip.sampleClamped(time.Duration(t * float64(time.Second)))
}

func safeDivOne(num, den float32) float32 {
	if den == 0 {
		return 1
	}
	return num / den
}
