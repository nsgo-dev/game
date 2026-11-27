package anim3d

import (
	"math"
	"sort"
	"time"

	"github.com/nsgo-dev/game/pkg/math3d"
)

// PoseSampler is implemented by animated pose sources that advance with dt and
// evaluate to a Pose (such as BlendSpace1D, BlendSpace2D, LayerStack, and StateMachine).
type PoseSampler interface {
	Update(dt time.Duration) Pose
	Pose() Pose
}

// BlendPosesWeighted blends poses using the corresponding non-negative weights.
// Weights are normalized internally so their sum equals 1. Translation and scale
// are linearly weighted; rotations use hemisphere-aligned normalized quaternion
// blending so the result is smooth and order-independent.
func BlendPosesWeighted(poses []Pose, weights []float32) Pose {
	count := min(len(poses), len(weights))
	if count == 0 {
		return Pose{}
	}

	var totalWeight float32
	jointCount := 0
	activeCount := 0
	lastActive := -1
	for i := range count {
		if weights[i] <= 0 {
			continue
		}
		totalWeight += weights[i]
		activeCount++
		lastActive = i
		if len(poses[i].Joints) > jointCount {
			jointCount = len(poses[i].Joints)
		}
	}
	if activeCount == 0 || totalWeight <= 0 || jointCount == 0 {
		return Pose{}
	}
	if activeCount == 1 {
		return poses[lastActive].Clone()
	}

	invTotal := 1 / totalWeight
	id := math3d.TransformIdentity()
	out := make([]math3d.Transform, jointCount)

	for j := range jointCount {
		var pos math3d.Vec3
		var scale math3d.Vec3
		var rot math3d.Quat
		// The hemisphere reference is pose 0's joint, independent of that
		// pose's weight: taking the first active pose made the reference flip
		// when a weight crossed zero, popping the blend up to 180° (#546).
		refRot := id.Rot.Norm()
		if len(poses[0].Joints) > j {
			refRot = poses[0].Joints[j].Rot.Norm()
		}

		for i := range count {
			w := weights[i]
			if w <= 0 {
				continue
			}
			nw := w * invTotal

			jt := id
			if j < len(poses[i].Joints) {
				jt = poses[i].Joints[j]
			}
			pos = pos.Add(jt.Pos.Scale(nw))
			scale = scale.Add(jt.Scale.Scale(nw))

			q := jt.Rot.Norm()
			if q.Dot(refRot) < 0 {
				q = math3d.Quat{X: -q.X, Y: -q.Y, Z: -q.Z, W: -q.W}
			}
			rot.X += q.X * nw
			rot.Y += q.Y * nw
			rot.Z += q.Z * nw
			rot.W += q.W * nw
		}

		out[j] = math3d.Transform{
			Pos:   pos,
			Rot:   rot.Norm(),
			Scale: scale,
		}
	}

	return Pose{Joints: out}
}

// BlendSample1D binds a clip and normalized foot-phase offset to a scalar
// parameter position in a BlendSpace1D.
type BlendSample1D struct {
	Param           float32
	Clip            *Clip
	FootPhaseOffset float32 // normalized in [0, 1)
}

// BlendSpace1D blends N clips sorted along a single scalar parameter with
// foot-phase synchronization across clips of different durations.
type BlendSpace1D struct {
	// ParamName optionally names a StateMachine parameter to read when attached
	// to a State without an explicit State.ParamName.
	ParamName string
	// Speed multiplies phase progression rate; 0 defaults to 1.0.
	Speed float32

	samples []BlendSample1D
	param   float32
	phase   float32
	played  float64
}

// NewBlendSpace1D creates a 1D blend space with samples sorted by ascending Param.
func NewBlendSpace1D(samples ...BlendSample1D) *BlendSpace1D {
	copied := make([]BlendSample1D, len(samples))
	for i, s := range samples {
		s.FootPhaseOffset = wrapPhase(s.FootPhaseOffset)
		copied[i] = s
	}
	sort.SliceStable(copied, func(i, j int) bool {
		return copied[i].Param < copied[j].Param
	})
	return &BlendSpace1D{
		samples: copied,
	}
}

// Samples returns a copy of the sorted samples in the blend space.
func (bs *BlendSpace1D) Samples() []BlendSample1D {
	if bs == nil || len(bs.samples) == 0 {
		return nil
	}
	out := make([]BlendSample1D, len(bs.samples))
	copy(out, bs.samples)
	return out
}

// SetParam updates the active scalar parameter used by Update and Pose.
func (bs *BlendSpace1D) SetParam(value float32) {
	if bs == nil {
		return
	}
	bs.param = value
}

// Param returns the current scalar parameter.
func (bs *BlendSpace1D) Param() float32 {
	if bs == nil {
		return 0
	}
	return bs.param
}

// Phase returns the current normalized cycle phase in [0, 1).
func (bs *BlendSpace1D) Phase() float32 {
	if bs == nil {
		return 0
	}
	return bs.phase
}

// SetPhase sets the normalized cycle phase (wrapped into [0, 1)).
func (bs *BlendSpace1D) SetPhase(phase float32) {
	if bs == nil {
		return
	}
	bs.phase = wrapPhase(phase)
	bs.played = float64(bs.phase)
}

// Reset resets the normalized cycle phase back to 0.
func (bs *BlendSpace1D) Reset() {
	if bs == nil {
		return
	}
	bs.phase = 0
	bs.played = 0
}

// Weights computes the normalized blend weights for param (clamped at endpoints). Samples that share the same Param split that position's weight equally.
func (bs *BlendSpace1D) Weights(param float32) []float32 {
	if bs == nil || len(bs.samples) == 0 {
		return nil
	}
	weights := make([]float32, len(bs.samples))
	i0, i1, alpha := bs.bracket(param)
	if i0 == i1 {
		bs.spread(weights, i0, 1)
	} else {
		bs.spread(weights, i0, 1-alpha)
		bs.spread(weights, i1, alpha)
	}
	return weights
}

// spread shares weight w equally among the samples whose Param equals sample i's, so samples placed at the same parameter are all reachable instead of the later ones being shadowed (#546).
func (bs *BlendSpace1D) spread(weights []float32, i int, w float32) {
	p := bs.samples[i].Param
	lo, hi := i, i+1
	for lo > 0 && bs.samples[lo-1].Param == p {
		lo--
	}
	for hi < len(bs.samples) && bs.samples[hi].Param == p {
		hi++
	}
	share := w / float32(hi-lo)
	for k := lo; k < hi; k++ {
		weights[k] += share
	}
}

// CycleSpeed returns the weighted normalized cycle speed sum(w_i / dur_i) in
// cycles per second at param.
func (bs *BlendSpace1D) CycleSpeed(param float32) float32 {
	if bs == nil || len(bs.samples) == 0 {
		return 0
	}
	var speed float32
	for i, w := range bs.Weights(param) {
		if w > 0 {
			speed += w * clipCycleRate(bs.samples[i].Clip)
		}
	}
	return speed
}

// Evaluate samples the blend space at param and normalized phase without
// mutating internal state.
func (bs *BlendSpace1D) Evaluate(param float32, phase float32) Pose {
	if bs == nil || len(bs.samples) == 0 {
		return Pose{}
	}
	p := wrapPhase(phase)
	return bs.evaluate(param, p, float64(p))
}

// evaluate samples at wrapped phase p; played is the phase advanced since Reset, which non-looping clips clamp against.
func (bs *BlendSpace1D) evaluate(param, p float32, played float64) Pose {
	i0, i1, alpha := bs.bracket(param)
	if i0 == i1 && !bs.duplicated(i0) {
		s := bs.samples[i0]
		return sampleClipAtPhase(s.Clip, p, played, s.FootPhaseOffset)
	}
	if !bs.duplicated(i0) && !bs.duplicated(i1) {
		s0, s1 := bs.samples[i0], bs.samples[i1]
		p0 := sampleClipAtPhase(s0.Clip, p, played, s0.FootPhaseOffset)
		p1 := sampleClipAtPhase(s1.Clip, p, played, s1.FootPhaseOffset)
		return BlendPoses(p0, p1, alpha)
	}
	weights := bs.Weights(param)
	poses := make([]Pose, 0, len(weights))
	active := make([]float32, 0, len(weights))
	for i, w := range weights {
		if w <= 0 {
			continue
		}
		s := bs.samples[i]
		poses = append(poses, sampleClipAtPhase(s.Clip, p, played, s.FootPhaseOffset))
		active = append(active, w)
	}
	return BlendPosesWeighted(poses, active)
}

// duplicated reports whether another sample shares sample i's Param.
func (bs *BlendSpace1D) duplicated(i int) bool {
	p := bs.samples[i].Param
	return (i > 0 && bs.samples[i-1].Param == p) || (i+1 < len(bs.samples) && bs.samples[i+1].Param == p)
}

// Update advances the synchronized foot phase by dt using the weighted cycle
// speed at the current Param and returns the evaluated Pose.
func (bs *BlendSpace1D) Update(dt time.Duration) Pose {
	if bs == nil || len(bs.samples) == 0 {
		return Pose{}
	}
	if dt > 0 {
		scaled := scaleDuration(dt, bs.Speed)
		bs.advance(float64(bs.CycleSpeed(bs.param)) * scaled.Seconds())
	}
	return bs.evaluate(bs.param, bs.phase, bs.played)
}

func (bs *BlendSpace1D) advance(deltaPhase float64) {
	bs.phase = wrapPhase64(float64(bs.phase) + deltaPhase)
	bs.played = advancePlayed(bs.played, deltaPhase)
}

// Pose evaluates the blend space at its current Param and Phase without advancing time.
func (bs *BlendSpace1D) Pose() Pose {
	if bs == nil {
		return Pose{}
	}
	return bs.evaluate(bs.param, bs.phase, bs.played)
}

func (bs *BlendSpace1D) bracket(param float32) (int, int, float32) {
	n := len(bs.samples)
	if n == 1 || param <= bs.samples[0].Param {
		return 0, 0, 0
	}
	if param >= bs.samples[n-1].Param {
		return n - 1, n - 1, 0
	}
	idx := sort.Search(n, func(i int) bool { return bs.samples[i].Param >= param })
	if idx <= 0 {
		return 0, 0, 0
	}
	if idx >= n {
		// A NaN or +Inf parameter never compares true, so Search returns n;
		// clamp to the last sample instead of indexing past the slice.
		return n - 1, n - 1, 0
	}
	if bs.samples[idx].Param == param {
		return idx, idx, 0
	}
	lo := idx - 1
	hi := idx
	span := bs.samples[hi].Param - bs.samples[lo].Param
	if span <= 0 {
		return lo, lo, 0
	}
	alpha := math3d.Clamp((param-bs.samples[lo].Param)/span, 0, 1)
	return lo, hi, alpha
}

// updateWithEvents advances phase and collects clip events from active weighted clips.
func (bs *BlendSpace1D) updateWithEvents(dt time.Duration, stateName string, stateWeight, minWeight float32, emit func(FiredEvent)) {
	if bs == nil || len(bs.samples) == 0 || dt <= 0 {
		return
	}
	prevPhase, prevPlayed := float64(bs.phase), bs.played
	scaled := scaleDuration(dt, bs.Speed)
	deltaPhase := float64(bs.CycleSpeed(bs.param)) * scaled.Seconds()
	bs.advance(deltaPhase)

	if emit == nil || deltaPhase == 0 {
		return
	}
	weights := bs.Weights(bs.param)
	for i, s := range bs.samples {
		w := weights[i] * stateWeight
		if s.Clip == nil || s.Clip.Duration <= 0 || len(s.Clip.Events) == 0 || !weightPassesThreshold(w, minWeight) {
			continue
		}
		collectPhaseEvents(s.Clip, s.FootPhaseOffset, prevPhase, prevPlayed, bs.played, deltaPhase, stateName, w, emit)
	}
}

// BlendSample2D binds a clip and normalized foot-phase offset to a 2D point
// (e.g. speed/direction or strafe/forward) in a BlendSpace2D.
type BlendSample2D struct {
	X, Y            float32
	Clip            *Clip
	FootPhaseOffset float32 // normalized in [0, 1)
}

// BlendSpace2D blends N clips in a 2D parameter space using Cartesian gradient-band
// interpolation with normalized weights and foot-phase synchronization.
type BlendSpace2D struct {
	// ParamXName and ParamYName optionally name StateMachine parameters to read
	// when attached to a State without explicit State.ParamXName / ParamYName.
	ParamXName string
	ParamYName string
	// Speed multiplies phase progression rate; 0 defaults to 1.0.
	Speed float32

	samples []BlendSample2D
	x, y    float32
	phase   float32
	played  float64
}

// NewBlendSpace2D creates a 2D blend space from the given samples.
func NewBlendSpace2D(samples ...BlendSample2D) *BlendSpace2D {
	copied := make([]BlendSample2D, len(samples))
	for i, s := range samples {
		s.FootPhaseOffset = wrapPhase(s.FootPhaseOffset)
		copied[i] = s
	}
	return &BlendSpace2D{
		samples: copied,
	}
}

// Samples returns a copy of the samples in the 2D blend space.
func (bs *BlendSpace2D) Samples() []BlendSample2D {
	if bs == nil || len(bs.samples) == 0 {
		return nil
	}
	out := make([]BlendSample2D, len(bs.samples))
	copy(out, bs.samples)
	return out
}

// SetParams updates the active (X, Y) coordinates used by Update and Pose.
func (bs *BlendSpace2D) SetParams(x, y float32) {
	if bs == nil {
		return
	}
	bs.x = x
	bs.y = y
}

// Params returns the current (X, Y) coordinates.
func (bs *BlendSpace2D) Params() (x, y float32) {
	if bs == nil {
		return 0, 0
	}
	return bs.x, bs.y
}

// Phase returns the current normalized cycle phase in [0, 1).
func (bs *BlendSpace2D) Phase() float32 {
	if bs == nil {
		return 0
	}
	return bs.phase
}

// SetPhase sets the normalized cycle phase (wrapped into [0, 1)).
func (bs *BlendSpace2D) SetPhase(phase float32) {
	if bs == nil {
		return
	}
	bs.phase = wrapPhase(phase)
	bs.played = float64(bs.phase)
}

// Reset resets the normalized cycle phase back to 0.
func (bs *BlendSpace2D) Reset() {
	if bs == nil {
		return
	}
	bs.phase = 0
	bs.played = 0
}

// Weights computes smooth normalized Cartesian gradient-band weights summing to 1
// for query point (x, y). At any sample's exact position, that sample receives
// weight 1 (or equal shares if duplicate sample coordinates exist).
func (bs *BlendSpace2D) Weights(x, y float32) []float32 {
	if bs == nil || len(bs.samples) == 0 {
		return nil
	}
	n := len(bs.samples)
	weights := make([]float32, n)
	if n == 1 {
		weights[0] = 1
		return weights
	}

	const epsSq = 1e-12
	p := math3d.Vec2{X: x, Y: y}

	// Exact sample hit check (including coincident samples).
	exactCount := 0
	for i, s := range bs.samples {
		d := p.Sub(math3d.Vec2{X: s.X, Y: s.Y})
		if d.LenSq() <= epsSq {
			weights[i] = 1
			exactCount++
		}
	}
	if exactCount > 0 {
		inv := 1 / float32(exactCount)
		for i := range weights {
			weights[i] *= inv
		}
		return weights
	}

	// Cartesian gradient-band interpolation (Johansen):
	// h_i = clamp(min_{j != i} (1 - dot(p - p_i, p_j - p_i) / |p_j - p_i|^2), 0, 1)
	var sum float32
	for i, si := range bs.samples {
		pi := math3d.Vec2{X: si.X, Y: si.Y}
		v := p.Sub(pi)
		minBand := float32(1)
		for j, sj := range bs.samples {
			if i == j {
				continue
			}
			edge := math3d.Vec2{X: sj.X - si.X, Y: sj.Y - si.Y}
			edgeLenSq := edge.LenSq()
			if edgeLenSq <= epsSq {
				continue
			}
			band := 1 - v.Dot(edge)/edgeLenSq
			if band < minBand {
				minBand = band
			}
			if minBand <= 0 {
				minBand = 0
				break
			}
		}
		if minBand > 0 {
			weights[i] = minBand
			sum += minBand
		}
	}

	if sum <= 0 {
		// Defensive fallback: assign weight 1 to nearest sample.
		bestIdx := 0
		bestDistSq := float32(math.MaxFloat32)
		for i, s := range bs.samples {
			dsq := p.DistSq(math3d.Vec2{X: s.X, Y: s.Y})
			if dsq < bestDistSq {
				bestDistSq = dsq
				bestIdx = i
			}
		}
		weights[bestIdx] = 1
		return weights
	}

	invSum := 1 / sum
	for i := range weights {
		weights[i] *= invSum
	}
	return weights
}

// CycleSpeed returns the weighted normalized cycle speed sum(w_i / dur_i) in
// cycles per second at (x, y).
func (bs *BlendSpace2D) CycleSpeed(x, y float32) float32 {
	if bs == nil || len(bs.samples) == 0 {
		return 0
	}
	weights := bs.Weights(x, y)
	var speed float32
	for i, s := range bs.samples {
		if weights[i] <= 0 {
			continue
		}
		speed += weights[i] * clipCycleRate(s.Clip)
	}
	return speed
}

// Evaluate samples the 2D blend space at (x, y) and normalized phase without
// mutating internal state.
func (bs *BlendSpace2D) Evaluate(x, y, phase float32) Pose {
	if bs == nil || len(bs.samples) == 0 {
		return Pose{}
	}
	p := wrapPhase(phase)
	return bs.evaluate(x, y, p, float64(p))
}

// evaluate samples at wrapped phase p; played is the phase advanced since Reset, which non-looping clips clamp against.
func (bs *BlendSpace2D) evaluate(x, y, p float32, played float64) Pose {
	weights := bs.Weights(x, y)
	poses := make([]Pose, 0, len(bs.samples))
	activeWeights := make([]float32, 0, len(bs.samples))
	for i, s := range bs.samples {
		if weights[i] <= 0 {
			continue
		}
		poses = append(poses, sampleClipAtPhase(s.Clip, p, played, s.FootPhaseOffset))
		activeWeights = append(activeWeights, weights[i])
	}
	return BlendPosesWeighted(poses, activeWeights)
}

// Update advances the synchronized foot phase by dt using the weighted cycle
// speed at the current (X, Y) and returns the evaluated Pose.
func (bs *BlendSpace2D) Update(dt time.Duration) Pose {
	if bs == nil || len(bs.samples) == 0 {
		return Pose{}
	}
	if dt > 0 {
		scaled := scaleDuration(dt, bs.Speed)
		bs.advance(float64(bs.CycleSpeed(bs.x, bs.y)) * scaled.Seconds())
	}
	return bs.evaluate(bs.x, bs.y, bs.phase, bs.played)
}

func (bs *BlendSpace2D) advance(deltaPhase float64) {
	bs.phase = wrapPhase64(float64(bs.phase) + deltaPhase)
	bs.played = advancePlayed(bs.played, deltaPhase)
}

// Pose evaluates the 2D blend space at its current (X, Y) and Phase without advancing time.
func (bs *BlendSpace2D) Pose() Pose {
	if bs == nil {
		return Pose{}
	}
	return bs.evaluate(bs.x, bs.y, bs.phase, bs.played)
}

// updateWithEvents advances phase and collects clip events from active weighted clips.
func (bs *BlendSpace2D) updateWithEvents(dt time.Duration, stateName string, stateWeight, minWeight float32, emit func(FiredEvent)) {
	if bs == nil || len(bs.samples) == 0 || dt <= 0 {
		return
	}
	prevPhase, prevPlayed := float64(bs.phase), bs.played
	scaled := scaleDuration(dt, bs.Speed)
	deltaPhase := float64(bs.CycleSpeed(bs.x, bs.y)) * scaled.Seconds()
	bs.advance(deltaPhase)

	if emit == nil || deltaPhase == 0 {
		return
	}
	weights := bs.Weights(bs.x, bs.y)
	for i, s := range bs.samples {
		w := weights[i] * stateWeight
		if s.Clip == nil || s.Clip.Duration <= 0 || len(s.Clip.Events) == 0 || !weightPassesThreshold(w, minWeight) {
			continue
		}
		collectPhaseEvents(s.Clip, s.FootPhaseOffset, prevPhase, prevPlayed, bs.played, deltaPhase, stateName, w, emit)
	}
}

func clipCycleRate(c *Clip) float32 {
	if c == nil || c.Duration <= 0 {
		return 0
	}
	return float32(1.0 / c.Duration.Seconds())
}

// maxPlayed caps the unwrapped phase a blend space tracks for non-looping clips: past 1 plus the largest foot-phase offset every such clip is already held on its last frame.
const maxPlayed = 2

// advancePlayed moves the unwrapped phase, kept in [0, maxPlayed].
func advancePlayed(played, deltaPhase float64) float64 {
	return math.Min(math.Max(played+deltaPhase, 0), maxPlayed)
}

// sampleClipAtPhase samples c at the shared phase. A looping clip wraps phase+offset into one cycle; a clip with Loop false plays once over the phase advanced since the blend space's Reset (played) and then holds its last frame (#546).
func sampleClipAtPhase(c *Clip, phase float32, played float64, footPhaseOffset float32) Pose {
	if c == nil {
		return Pose{}
	}
	if c.Duration <= 0 {
		return c.sampleClamped(0)
	}
	var cp float64
	if c.Loop {
		cp = float64(wrapPhase(phase + footPhaseOffset))
	} else {
		cp = math.Min(math.Max(played+float64(footPhaseOffset), 0), 1)
	}
	t := time.Duration(cp * float64(c.Duration))
	return c.sampleClamped(t)
}

func wrapPhase(p float32) float32 {
	return wrapPhase64(float64(p))
}

func wrapPhase64(p float64) float32 {
	r := math.Mod(p, 1.0)
	if r < 0 {
		r += 1.0
	}
	if r >= 1.0 {
		r = 0
	}
	return float32(r)
}

func collectPhaseEvents(clip *Clip, footPhaseOffset float32, prevPhase, prevPlayed, nextPlayed, deltaPhase float64, stateName string, weight float32, emit func(FiredEvent)) {
	durSec := clip.Duration.Seconds()
	var prevSec, nextSec float64
	if clip.Loop {
		startPhase := float64(wrapPhase64(prevPhase + float64(footPhaseOffset)))
		prevSec = startPhase * durSec
		nextSec = (startPhase + deltaPhase) * durSec
	} else {
		prevSec = (prevPlayed + float64(footPhaseOffset)) * durSec
		nextSec = (nextPlayed + float64(footPhaseOffset)) * durSec
	}
	fired := collectDirected(clip.Events, prevSec, nextSec, durSec, clip.Loop)
	for _, ev := range fired {
		emit(FiredEvent{
			Event:  ev,
			State:  stateName,
			Clip:   clip.Name,
			Weight: weight,
		})
	}
}
