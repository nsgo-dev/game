package anim3d

import (
	"reflect"
	"sort"
	"time"

	"github.com/nsgo-dev/game/pkg/math3d"
)

// Pose holds local TRS transforms for each joint in a skeleton.
type Pose struct {
	Joints []math3d.Transform
}

// NewPose returns a Pose with jointCount joints initialized to identity transforms.
func NewPose(jointCount int) Pose {
	if jointCount <= 0 {
		return Pose{}
	}
	joints := make([]math3d.Transform, jointCount)
	id := math3d.TransformIdentity()
	for i := range joints {
		joints[i] = id
	}
	return Pose{Joints: joints}
}

// Clone returns a deep copy of p.
func (p Pose) Clone() Pose {
	if len(p.Joints) == 0 {
		return Pose{}
	}
	out := make([]math3d.Transform, len(p.Joints))
	copy(out, p.Joints)
	return Pose{Joints: out}
}

// BlendPoses interpolates between poses a (t=0) and b (t=1), clamping t to [0, 1].
// Translation and scale are linearly interpolated; rotations use shortest-path Slerp.
// If a and b have different joint counts, missing joints default to TransformIdentity.
func BlendPoses(a, b Pose, t float32) Pose {
	alpha := math3d.Clamp(t, 0, 1)
	n := max(len(a.Joints), len(b.Joints))
	if n == 0 {
		return Pose{}
	}
	id := math3d.TransformIdentity()
	out := make([]math3d.Transform, n)
	for i := range n {
		ja := id
		if i < len(a.Joints) {
			ja = a.Joints[i]
		}
		jb := id
		if i < len(b.Joints) {
			jb = b.Joints[i]
		}
		out[i] = ja.Lerp(jb, alpha)
	}
	return Pose{Joints: out}
}

// Vec3Keyframe is a timed 3D vector sample for a translation or scale channel.
type Vec3Keyframe struct {
	Time  time.Duration
	Value math3d.Vec3
}

// QuatKeyframe is a timed rotation sample for a joint orientation channel.
type QuatKeyframe struct {
	Time  time.Duration
	Value math3d.Quat
}

// JointTrack stores keyframed local TRS channels for a single joint.
// Keyframes in each channel must be ordered by ascending Time. Any empty channel
// falls back to the corresponding identity component.
type JointTrack struct {
	JointIndex   int
	Translations []Vec3Keyframe
	Rotations    []QuatKeyframe
	Scales       []Vec3Keyframe
}

// Clip is a keyframed 3D skeletal animation clip.
type Clip struct {
	Name       string
	Duration   time.Duration
	Loop       bool
	JointCount int
	Tracks     []JointTrack
	Events     []ClipEvent
}

// AddEvent appends ev to the clip's Events slice and keeps Events sorted by Time.
func (c *Clip) AddEvent(ev ClipEvent) {
	if c == nil {
		return
	}
	c.Events = append(c.Events, ev)
	SortClipEvents(c.Events)
}

// jointCount returns the effective number of joints covered by the clip.
func (c *Clip) jointCount() int {
	if c == nil {
		return 0
	}
	n := c.JointCount
	for _, tr := range c.Tracks {
		if tr.JointIndex+1 > n {
			n = tr.JointIndex + 1
		}
	}
	return n
}

// NormalizeTime maps t into the clip's playable range: wrapped into [0, Duration)
// when Loop is true, or clamped to [0, Duration] otherwise.
func (c *Clip) NormalizeTime(t time.Duration) time.Duration {
	if c == nil || c.Duration <= 0 {
		return 0
	}
	if c.Loop {
		return modDuration(t, c.Duration)
	}
	if t < 0 {
		return 0
	}
	if t > c.Duration {
		return c.Duration
	}
	return t
}

// Sample evaluates the clip at time t and returns the resulting Pose.
// Translations and scales are linearly interpolated; rotations use Quat.Slerp.
func (c *Clip) Sample(t time.Duration) Pose {
	if c == nil {
		return Pose{}
	}
	return c.sampleClamped(c.NormalizeTime(t))
}

// sampleClamped evaluates the clip at t clamped to [0, Duration] without wrapping
// Duration back to 0.
func (c *Clip) sampleClamped(t time.Duration) Pose {
	if c == nil {
		return Pose{}
	}
	n := c.jointCount()
	pose := NewPose(n)
	if n == 0 {
		return pose
	}
	if t < 0 {
		t = 0
	}
	if c.Duration > 0 && t > c.Duration {
		t = c.Duration
	}
	for _, tr := range c.Tracks {
		if tr.JointIndex < 0 || tr.JointIndex >= n {
			continue
		}
		pose.Joints[tr.JointIndex] = sampleTrack(tr, t)
	}
	return pose
}

// SampleJoint evaluates a single joint's local transform at time t (normalized
// according to the clip's Loop setting).
func (c *Clip) SampleJoint(jointIndex int, t time.Duration) math3d.Transform {
	if c == nil {
		return math3d.TransformIdentity()
	}
	return c.sampleJointClamped(jointIndex, c.NormalizeTime(t))
}

// sampleJointClamped evaluates jointIndex at t clamped to [0, Duration] without
// wrapping Duration back to 0. This is used by root motion extraction to read
// exact end-of-loop values.
func (c *Clip) sampleJointClamped(jointIndex int, t time.Duration) math3d.Transform {
	if c == nil {
		return math3d.TransformIdentity()
	}
	if t < 0 {
		t = 0
	}
	if c.Duration > 0 && t > c.Duration {
		t = c.Duration
	}
	for _, tr := range c.Tracks {
		if tr.JointIndex == jointIndex {
			return sampleTrack(tr, t)
		}
	}
	return math3d.TransformIdentity()
}

func sampleTrack(tr JointTrack, t time.Duration) math3d.Transform {
	out := math3d.TransformIdentity()
	if len(tr.Translations) > 0 {
		out.Pos = sampleVec3Track(tr.Translations, t)
	}
	if len(tr.Rotations) > 0 {
		out.Rot = sampleQuatTrack(tr.Rotations, t)
	}
	if len(tr.Scales) > 0 {
		out.Scale = sampleVec3Track(tr.Scales, t)
	}
	return out
}

func sampleVec3Track(kfs []Vec3Keyframe, t time.Duration) math3d.Vec3 {
	n := len(kfs)
	if n == 1 || t <= kfs[0].Time {
		return kfs[0].Value
	}
	if t >= kfs[n-1].Time {
		return kfs[n-1].Value
	}
	idx := sort.Search(n, func(i int) bool { return kfs[i].Time >= t })
	if kfs[idx].Time == t {
		return kfs[idx].Value
	}
	prev := kfs[idx-1]
	next := kfs[idx]
	span := next.Time - prev.Time
	if span <= 0 {
		return next.Value
	}
	alpha := float32(t-prev.Time) / float32(span)
	return prev.Value.Lerp(next.Value, alpha)
}

func sampleQuatTrack(kfs []QuatKeyframe, t time.Duration) math3d.Quat {
	n := len(kfs)
	if n == 1 || t <= kfs[0].Time {
		return kfs[0].Value.Norm()
	}
	if t >= kfs[n-1].Time {
		return kfs[n-1].Value.Norm()
	}
	idx := sort.Search(n, func(i int) bool { return kfs[i].Time >= t })
	if kfs[idx].Time == t {
		return kfs[idx].Value.Norm()
	}
	prev := kfs[idx-1]
	next := kfs[idx]
	span := next.Time - prev.Time
	if span <= 0 {
		return next.Value.Norm()
	}
	alpha := float32(t-prev.Time) / float32(span)
	return prev.Value.Slerp(next.Value, alpha)
}

func modDuration(d, period time.Duration) time.Duration {
	if period <= 0 {
		return 0
	}
	r := d % period
	if r < 0 {
		r += period
	}
	return r
}

func scaleDuration(dt time.Duration, speed float32) time.Duration {
	if speed == 0 {
		return dt
	}
	return time.Duration(float64(dt) * float64(speed))
}

// State binds a named state in a StateMachine to an animation Clip, a
// BlendSpace1D, a BlendSpace2D, or a custom PoseSampler.
//
// Resolution priority when more than one pose source is populated is:
// Blend1D > Blend2D > Sampler > Clip.
type State struct {
	Name       string
	Clip       *Clip
	Blend1D    *BlendSpace1D
	Blend2D    *BlendSpace2D
	Sampler    PoseSampler
	ParamName  string  // optional StateMachine parameter name for Blend1D
	ParamXName string  // optional StateMachine X parameter name for Blend2D
	ParamYName string  // optional StateMachine Y parameter name for Blend2D
	Speed      float32 // playback speed multiplier; 0 defaults to 1.0
}

// Transition defines a conditional crossfade between two states in a StateMachine.
// An empty From string matches any state other than To.
type Transition struct {
	From          string
	To            string
	BlendDuration time.Duration
	When          func() bool
}

// StateMachine drives 3D skeletal clip and blend-space playback, event dispatch,
// and smooth crossfades between named animation states on fixed Update steps.
type StateMachine struct {
	states      map[string]State
	transitions []Transition
	params      map[string]float32

	current     string
	currentTime time.Duration

	blending      bool
	blendElapsed  time.Duration
	blendDuration time.Duration
	prevState     string
	prevTime      time.Duration
	hasSnapshot   bool
	prevSnapshot  Pose

	dispatcher     EventDispatcher
	eventMinWeight float32
}

// NewStateMachine creates a StateMachine starting in initialState.
func NewStateMachine(initialState string) *StateMachine {
	return &StateMachine{
		states:  make(map[string]State),
		params:  make(map[string]float32),
		current: initialState,
	}
}

// AddState registers or replaces a named state. If the machine has no active
// state yet, state.Name becomes the current state. When registering the active
// state for the first time, playback starts at Duration for a non-looping clip
// with negative Speed and at 0 otherwise. When replacing the active state, if a
// non-looping clip's effective speed changes sign while parked at a clamped end,
// playback jumps to the opposite end so it plays instead of staying clamped.
func (sm *StateMachine) AddState(state State) {
	if sm.states == nil {
		sm.states = make(map[string]State)
	}
	prev, existed := sm.states[state.Name]
	sm.states[state.Name] = state
	if sm.current == "" {
		sm.current = state.Name
		sm.currentTime = initialStateTime(state)
		return
	}
	if state.Name != sm.current {
		return
	}
	if !existed {
		sm.currentTime = initialStateTime(state)
		return
	}
	if clip := stateNonLoopingClip(state); clip != nil {
		prevNeg := prev.Speed < 0
		nextNeg := state.Speed < 0
		if !prevNeg && nextNeg && sm.currentTime <= 0 {
			sm.currentTime = clip.Duration
		} else if prevNeg && !nextNeg && sm.currentTime >= clip.Duration {
			sm.currentTime = 0
		}
	}
}

// AddTransition appends a conditional transition evaluated in registration order.
func (sm *StateMachine) AddTransition(tr Transition) {
	sm.transitions = append(sm.transitions, tr)
}

// SetParam sets a named float32 parameter on the state machine. Blend-space
// states bound to that parameter name automatically read it during Update and Pose.
func (sm *StateMachine) SetParam(name string, value float32) {
	if sm.params == nil {
		sm.params = make(map[string]float32)
	}
	sm.params[name] = value
}

// Param returns the value of the named float32 parameter, or 0 if unset.
func (sm *StateMachine) Param(name string) float32 {
	if sm.params == nil {
		return 0
	}
	return sm.params[name]
}

// OnEvent registers a callback invoked synchronously whenever an animation event
// fires during Update.
func (sm *StateMachine) OnEvent(fn func(FiredEvent)) {
	sm.dispatcher.OnEvent(fn)
}

// DrainEvents returns all FiredEvents collected since the last DrainEvents call
// and clears the internal event buffer.
func (sm *StateMachine) DrainEvents() []FiredEvent {
	return sm.dispatcher.Drain()
}

// SetEventWeightThreshold configures the minimum active blend weight in [0, 1]
// required for a state (or blend-space clip) to fire events during crossfades.
// A threshold of 0 (the default) fires events for any clip with weight > 0.
func (sm *StateMachine) SetEventWeightThreshold(minWeight float32) {
	sm.eventMinWeight = math3d.Clamp(minWeight, 0, 1)
}

// EventWeightThreshold returns the configured minimum blend weight threshold
// for event firing.
func (sm *StateMachine) EventWeightThreshold() float32 {
	return sm.eventMinWeight
}

// SetState transitions directly to name over blendDuration. It returns false if
// name is not a registered state.
func (sm *StateMachine) SetState(name string, blendDuration time.Duration) bool {
	if _, ok := sm.states[name]; !ok {
		return false
	}
	if name == sm.current {
		// Asking for the state that is already playing (or fading in) is a
		// no-op; a caller that wants to replay it calls Restart.
		return true
	}
	sm.beginTransition(name, blendDuration)
	return true
}

// Restart replays a state from its beginning, crossfading from itself when
// name is the current state and blendDuration is positive.
func (sm *StateMachine) Restart(name string, blendDuration time.Duration) bool {
	if _, ok := sm.states[name]; !ok {
		return false
	}
	if name != sm.current {
		return sm.SetState(name, blendDuration)
	}
	sm.beginTransition(name, blendDuration)
	return true
}

// CurrentState returns the name of the active (or target, while blending) state.
func (sm *StateMachine) CurrentState() string { return sm.current }

// PreviousState returns the name of the state being blended out of, or "" when
// not blending.
func (sm *StateMachine) PreviousState() string {
	if !sm.blending {
		return ""
	}
	return sm.prevState
}

// CurrentTime returns the playback time of the current state's clip.
func (sm *StateMachine) CurrentTime() time.Duration { return sm.currentTime }

// IsBlending reports whether a crossfade transition is currently in progress.
func (sm *StateMachine) IsBlending() bool { return sm.blending }

// BlendProgress returns the normalized crossfade weight in [0, 1]. It returns 1
// when no crossfade is active.
func (sm *StateMachine) BlendProgress() float32 {
	if !sm.blending || sm.blendDuration <= 0 {
		return 1
	}
	return math3d.Clamp(float32(sm.blendElapsed)/float32(sm.blendDuration), 0, 1)
}

// Update evaluates conditional transitions, advances clip and blend-space playback
// and any active crossfade by dt, dispatches triggered animation events, and
// returns the resulting Pose.
//
// Update(0) is a paused step: it re-evaluates the pose but neither advances time nor starts conditional transitions, so a paused character does not change state. During a crossfade both states fire events at their crossfade weights at the midpoint of the part of the step that lies inside the crossfade, so the step that completes a crossfade still fires the outgoing state's events. When both states of a crossfade are driven by the same blend space or sampler, it is advanced once per step, not twice.
func (sm *StateMachine) Update(dt time.Duration) Pose {
	if dt < 0 {
		dt = 0
	}
	if dt == 0 {
		return sm.Pose()
	}

	// Evaluate conditional transitions before advancing dt so a transition with
	// BlendDuration D completes after exactly D of Update steps.
	for _, tr := range sm.transitions {
		if tr.When == nil {
			continue
		}
		if tr.From != "" && tr.From != sm.current {
			continue
		}
		if tr.To == sm.current {
			continue
		}
		if _, ok := sm.states[tr.To]; !ok {
			continue
		}
		if tr.When() {
			sm.beginTransition(tr.To, tr.BlendDuration)
			break
		}
	}

	// Compute active weights for event threshold filtering across this step.
	// Event weights are the crossfade weights at the midpoint of the part of this step that lies inside the crossfade. Using the weight at the end of the step gave the outgoing state 0 on the step that completes the crossfade, which dropped that step's events (#546).
	currWeight := float32(1)
	prevWeight := float32(0)
	if sm.blending && sm.blendDuration > 0 {
		end := min(sm.blendElapsed+dt, sm.blendDuration)
		mid := (float64(sm.blendElapsed) + float64(end)) / 2
		currWeight = math3d.Clamp(float32(mid/float64(sm.blendDuration)), 0, 1)
		prevWeight = 1 - currWeight
	}

	st, hasCurr := sm.states[sm.current]
	if hasCurr {
		sm.currentTime = sm.advanceState(st, sm.currentTime, dt, currWeight)
	}

	if sm.blending {
		if !sm.hasSnapshot {
			if prev, ok := sm.states[sm.prevState]; ok {
				if hasCurr && sharesSource(prev, st) {
					// One blend space or sampler drives both states; advancing it again would play it at double speed (#546).
					sm.prevTime += scaleDuration(dt, prev.Speed)
				} else {
					sm.prevTime = sm.advanceState(prev, sm.prevTime, dt, prevWeight)
				}
			}
		}
		sm.blendElapsed += dt
		if sm.blendElapsed >= sm.blendDuration {
			sm.blending = false
			sm.blendElapsed = 0
			sm.blendDuration = 0
			sm.prevState = ""
			sm.hasSnapshot = false
			sm.prevSnapshot = Pose{}
		}
	}

	return sm.Pose()
}

// Pose evaluates the current state's pose (or blended pose during a crossfade)
// without advancing time.
func (sm *StateMachine) Pose() Pose {
	curr := sm.sampleState(sm.current, sm.currentTime)
	if !sm.blending {
		return curr
	}
	var prev Pose
	if sm.hasSnapshot {
		prev = sm.prevSnapshot
	} else {
		prev = sm.sampleState(sm.prevState, sm.prevTime)
	}
	return BlendPoses(prev, curr, sm.BlendProgress())
}

func (sm *StateMachine) beginTransition(to string, blendDuration time.Duration) {
	startTime := initialStateTime(sm.states[to])
	if blendDuration <= 0 {
		sm.current = to
		sm.currentTime = startTime
		sm.blending = false
		sm.blendElapsed = 0
		sm.blendDuration = 0
		sm.prevState = ""
		sm.hasSnapshot = false
		sm.prevSnapshot = Pose{}
		sm.resetState(to)
		return
	}

	if sm.blending {
		// Snapshot the in-progress blended pose so interrupting a crossfade
		// never pops.
		sm.prevSnapshot = sm.Pose()
		sm.hasSnapshot = true
	} else {
		sm.hasSnapshot = false
		sm.prevSnapshot = Pose{}
	}
	sm.prevState = sm.current
	sm.prevTime = sm.currentTime
	sm.current = to
	sm.currentTime = startTime
	sm.blending = true
	sm.blendElapsed = 0
	sm.blendDuration = blendDuration
	sm.resetState(to)
}

// stateNonLoopingClip returns st's Clip when st resolves to a non-looping clip
// with positive Duration (respecting Blend1D > Blend2D > Sampler > Clip priority),
// or nil otherwise.
func stateNonLoopingClip(st State) *Clip {
	if st.Blend1D != nil || st.Blend2D != nil || st.Sampler != nil || st.Clip == nil {
		return nil
	}
	if st.Clip.Loop || st.Clip.Duration <= 0 {
		return nil
	}
	return st.Clip
}

// initialStateTime returns the starting playback time when entering st:
// Duration for a non-looping clip state with negative Speed, and 0 otherwise.
func initialStateTime(st State) time.Duration {
	if clip := stateNonLoopingClip(st); clip != nil && st.Speed < 0 {
		return clip.Duration
	}
	return 0
}

func (sm *StateMachine) resetState(name string) {
	st, ok := sm.states[name]
	if !ok {
		return
	}
	switch {
	case st.Blend1D != nil:
		st.Blend1D.Reset()
	case st.Blend2D != nil:
		st.Blend2D.Reset()
	case st.Sampler != nil:
		if r, ok := st.Sampler.(interface{ Reset() }); ok {
			r.Reset()
		}
	}
}

func (sm *StateMachine) syncStateParams(st State) {
	if st.Blend1D != nil {
		sm.syncBlend1DParam(st.Blend1D, st.ParamName)
		return
	}
	if st.Blend2D != nil {
		sm.syncBlend2DParams(st.Blend2D, st.ParamXName, st.ParamYName)
		return
	}
	switch s := st.Sampler.(type) {
	case *BlendSpace1D:
		sm.syncBlend1DParam(s, st.ParamName)
	case *BlendSpace2D:
		sm.syncBlend2DParams(s, st.ParamXName, st.ParamYName)
	}
}

func (sm *StateMachine) syncBlend1DParam(bs *BlendSpace1D, stateParam string) {
	name := stateParam
	if name == "" {
		name = bs.ParamName
	}
	if name != "" {
		bs.SetParam(sm.Param(name))
	}
}

func (sm *StateMachine) syncBlend2DParams(bs *BlendSpace2D, stateParamX, stateParamY string) {
	nx := stateParamX
	if nx == "" {
		nx = bs.ParamXName
	}
	ny := stateParamY
	if ny == "" {
		ny = bs.ParamYName
	}
	if nx == "" && ny == "" {
		return
	}
	x, y := bs.Params()
	if nx != "" {
		x = sm.Param(nx)
	}
	if ny != "" {
		y = sm.Param(ny)
	}
	bs.SetParams(x, y)
}

func (sm *StateMachine) advanceState(st State, curr time.Duration, dt time.Duration, weight float32) time.Duration {
	sm.syncStateParams(st)
	scaled := scaleDuration(dt, st.Speed)

	switch {
	case st.Blend1D != nil:
		st.Blend1D.updateWithEvents(scaled, st.Name, weight, sm.eventMinWeight, sm.dispatcher.Emit)
		return curr + scaled
	case st.Blend2D != nil:
		st.Blend2D.updateWithEvents(scaled, st.Name, weight, sm.eventMinWeight, sm.dispatcher.Emit)
		return curr + scaled
	case st.Sampler != nil:
		switch s := st.Sampler.(type) {
		case *BlendSpace1D:
			s.updateWithEvents(scaled, st.Name, weight, sm.eventMinWeight, sm.dispatcher.Emit)
		case *BlendSpace2D:
			s.updateWithEvents(scaled, st.Name, weight, sm.eventMinWeight, sm.dispatcher.Emit)
		default:
			st.Sampler.Update(scaled)
		}
		return curr + scaled
	case st.Clip != nil:
		next := advanceClipTime(st.Clip, curr, scaled)
		if scaled != 0 && st.Clip.Duration > 0 && len(st.Clip.Events) > 0 && weightPassesThreshold(weight, sm.eventMinWeight) {
			// Negative speeds play backward and fire the events they cross in reverse order, like forward playback does (#546).
			for _, ev := range collectDirected(st.Clip.Events, curr.Seconds(), (curr + scaled).Seconds(), st.Clip.Duration.Seconds(), st.Clip.Loop) {
				sm.dispatcher.Emit(FiredEvent{Event: ev, State: st.Name, Clip: st.Clip.Name, Weight: weight})
			}
		}
		return next
	default:
		return curr
	}
}

func (sm *StateMachine) sampleState(name string, t time.Duration) Pose {
	st, ok := sm.states[name]
	if !ok {
		return Pose{}
	}
	sm.syncStateParams(st)
	switch {
	case st.Blend1D != nil:
		return st.Blend1D.Pose()
	case st.Blend2D != nil:
		return st.Blend2D.Pose()
	case st.Sampler != nil:
		return st.Sampler.Pose()
	case st.Clip != nil:
		return st.Clip.Sample(t)
	default:
		return Pose{}
	}
}

// sharesSource reports whether two states are driven by the same blend space or pointer sampler, which must advance once per step.
func sharesSource(a, b State) bool {
	sa, sb := stateSource(a), stateSource(b)
	return sa != nil && sa == sb
}

// stateSource returns the stateful pose source that resolves for st (Blend1D > Blend2D > Sampler), or nil when it has none or its sampler is not a pointer and so cannot be shared.
func stateSource(st State) any {
	switch {
	case st.Blend1D != nil:
		return st.Blend1D
	case st.Blend2D != nil:
		return st.Blend2D
	case st.Sampler != nil:
		if reflect.ValueOf(st.Sampler).Kind() == reflect.Pointer {
			return st.Sampler
		}
	}
	return nil
}

func advanceClipTime(c *Clip, curr, delta time.Duration) time.Duration {
	if c == nil || c.Duration <= 0 {
		return 0
	}
	return c.NormalizeTime(curr + delta)
}
