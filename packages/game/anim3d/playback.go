package anim3d

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
)

// Playback errors returned by glTF clip conversion and Player operations.
var (
	// ErrNilDocument is returned when a nil *gltf.Document is passed.
	ErrNilDocument = errors.New("anim3d: nil gltf document")
	// ErrInvalidAnimation is returned when an animation index is out of range.
	ErrInvalidAnimation = errors.New("anim3d: animation index out of range")
	// ErrAnimationNotFound is returned when no animation matches the given name.
	ErrAnimationNotFound = errors.New("anim3d: animation not found")
	// ErrInvalidSkin is returned when a skin index or skinned node is out of range.
	ErrInvalidSkin = errors.New("anim3d: skin index out of range")
)

// ClipFromGLTF converts doc.Animations[animIndex] into an anim3d.Clip whose
// JointTrack indices match doc.Nodes indices (0..len(doc.Nodes)-1).
//
// For every node targeted by the animation, any TRS component not driven by a
// channel is seeded from that node's rest transform in doc so sampling preserves
// bone offsets and bind orientations. Untargeted nodes are omitted from Tracks
// so BoneMask and LayerStack overrides leave them untouched.
//
// CUBICSPLINE channels extract their keyframe values; use Player directly when
// exact STEP or CUBICSPLINE interpolation, morph target weights, or per-node
// world and skin joint matrices are needed. Any timed events stored in the
// animation's glTF extras (see LoadEventsFromGLTFExtras) are attached to the
// returned Clip. Loop defaults to false.
func ClipFromGLTF(doc *gltf.Document, animIndex int) (*Clip, error) {
	if doc == nil {
		return nil, ErrNilDocument
	}
	if animIndex < 0 || animIndex >= len(doc.Animations) {
		return nil, fmt.Errorf("%w: %d (count %d)", ErrInvalidAnimation, animIndex, len(doc.Animations))
	}

	anim := &doc.Animations[animIndex]
	trackByNode := make(map[int]*JointTrack)
	getTrack := func(nodeIdx int) *JointTrack {
		tr, ok := trackByNode[nodeIdx]
		if !ok {
			tr = &JointTrack{JointIndex: nodeIdx}
			trackByNode[nodeIdx] = tr
		}
		return tr
	}

	var maxSec float32 = anim.Duration
	for ci := range anim.Channels {
		ch := &anim.Channels[ci]
		if ch.TargetNode < 0 || ch.TargetNode >= len(doc.Nodes) {
			continue
		}
		n := len(ch.Times)
		if n == 0 || ch.Stride <= 0 {
			continue
		}
		if last := ch.Times[n-1]; last > maxSec {
			maxSec = last
		}

		isCubic := ch.Interpolation == gltf.InterpolationCubicSpline
		keyMult := 1
		if isCubic {
			keyMult = 3
		}
		if len(ch.Values) < n*keyMult*ch.Stride {
			continue
		}

		valOffset := func(k int) int {
			if isCubic {
				return (k*3 + 1) * ch.Stride
			}
			return k * ch.Stride
		}

		switch ch.Path {
		case gltf.PathTranslation:
			if ch.Stride < 3 {
				continue
			}
			tr := getTrack(ch.TargetNode)
			kfs := make([]Vec3Keyframe, n)
			for k := range n {
				off := valOffset(k)
				kfs[k] = Vec3Keyframe{
					Time:  durationFromSeconds(ch.Times[k]),
					Value: math3d.Vec3{X: ch.Values[off], Y: ch.Values[off+1], Z: ch.Values[off+2]},
				}
			}
			tr.Translations = kfs
		case gltf.PathRotation:
			if ch.Stride < 4 {
				continue
			}
			tr := getTrack(ch.TargetNode)
			kfs := make([]QuatKeyframe, n)
			for k := range n {
				off := valOffset(k)
				kfs[k] = QuatKeyframe{
					Time:  durationFromSeconds(ch.Times[k]),
					Value: math3d.Quat{X: ch.Values[off], Y: ch.Values[off+1], Z: ch.Values[off+2], W: ch.Values[off+3]}.Norm(),
				}
			}
			tr.Rotations = kfs
		case gltf.PathScale:
			if ch.Stride < 3 {
				continue
			}
			tr := getTrack(ch.TargetNode)
			kfs := make([]Vec3Keyframe, n)
			for k := range n {
				off := valOffset(k)
				kfs[k] = Vec3Keyframe{
					Time:  durationFromSeconds(ch.Times[k]),
					Value: math3d.Vec3{X: ch.Values[off], Y: ch.Values[off+1], Z: ch.Values[off+2]},
				}
			}
			tr.Scales = kfs
		}
	}

	nodeIndices := make([]int, 0, len(trackByNode))
	for idx := range trackByNode {
		nodeIndices = append(nodeIndices, idx)
	}
	sort.Ints(nodeIndices)

	tracks := make([]JointTrack, len(nodeIndices))
	for i, nodeIdx := range nodeIndices {
		tr := trackByNode[nodeIdx]
		rest := doc.Nodes[nodeIdx]
		if len(tr.Translations) == 0 {
			tr.Translations = []Vec3Keyframe{{Time: 0, Value: rest.Translation}}
		}
		if len(tr.Rotations) == 0 {
			tr.Rotations = []QuatKeyframe{{Time: 0, Value: rest.Rotation.Norm()}}
		}
		if len(tr.Scales) == 0 {
			tr.Scales = []Vec3Keyframe{{Time: 0, Value: rest.Scale}}
		}
		tracks[i] = *tr
	}

	clip := &Clip{
		Name:       anim.Name,
		Duration:   durationFromSeconds(maxSec),
		JointCount: len(doc.Nodes),
		Tracks:     tracks,
	}

	if len(anim.Extras) > 0 {
		var extras gltfAnimationExtras
		if err := json.Unmarshal(anim.Extras, &extras); err == nil {
			if len(extras.Events) > 0 {
				clip.Events = append(clip.Events, extras.Events...)
			}
			if len(extras.NsgoEvents) > 0 {
				clip.Events = append(clip.Events, extras.NsgoEvents...)
			}
			if len(clip.Events) > 0 {
				SortClipEvents(clip.Events)
			}
		}
	}

	return clip, nil
}

// ClipsFromGLTF converts every animation in doc into an anim3d.Clip in document
// order.
func ClipsFromGLTF(doc *gltf.Document) ([]*Clip, error) {
	if doc == nil {
		return nil, ErrNilDocument
	}
	clips := make([]*Clip, len(doc.Animations))
	for i := range doc.Animations {
		clip, err := ClipFromGLTF(doc, i)
		if err != nil {
			return nil, err
		}
		clips[i] = clip
	}
	return clips, nil
}

// Player drives glTF 2.0 node animation playback on fixed time.Duration steps
// matching game.Loop.Update.
//
// Player evaluates translation, rotation (shortest-path quaternion Slerp for
// LINEAR and Hermite spline normalization for CUBICSPLINE), scale, and morph
// target weight channels across STEP, LINEAR, and CUBICSPLINE interpolation
// modes. It maintains its own per-instance node transforms and world matrices
// without mutating the underlying *gltf.Document, allowing multiple Players to
// animate the same loaded asset independently.
//
// Player also implements PoseSampler via Update and Pose.
type Player struct {
	doc *gltf.Document

	parents   []int
	evalOrder []int

	restLocal     []math3d.Transform
	restHasMatrix []bool
	restMatrix    []math3d.Mat4
	restWeights   [][]float32

	animDurations []time.Duration

	local          []math3d.Transform
	localHasMatrix []bool
	localMatrix    []math3d.Mat4
	world          []math3d.Mat4
	weights        [][]float32

	blendLocal        []math3d.Transform
	blendHasMatrix    []bool
	blendMatrix       []math3d.Mat4
	blendWeights      [][]float32
	snapshotLocal     []math3d.Transform
	snapshotHasMatrix []bool
	snapshotMatrix    []math3d.Mat4
	snapshotWeights   [][]float32

	animIndex int
	time      time.Duration
	playing   bool
	loop      bool
	speed     float32

	blending      bool
	blendElapsed  time.Duration
	blendDuration time.Duration
	prevAnim      int
	prevTime      time.Duration
	hasSnapshot   bool
}

// NewPlayer creates a Player bound to doc's node hierarchy, skins, and
// animations. The player validates the node hierarchy (rejecting out-of-range
// children, multiple parents, and cycles), initializes all node transforms to
// doc's bind pose, and computes initial world matrices without mutating doc.
//
// Loop defaults to true and Speed defaults to 1.0. If doc contains animations,
// animation 0 is selected in the stopped state until Play or Crossfade is
// called.
func NewPlayer(doc *gltf.Document) (*Player, error) {
	if doc == nil {
		return nil, ErrNilDocument
	}

	n := len(doc.Nodes)
	parents, evalOrder, err := buildNodeOrder(doc.Nodes)
	if err != nil {
		return nil, err
	}

	restLocal := make([]math3d.Transform, n)
	restHasMatrix := make([]bool, n)
	restMatrix := make([]math3d.Mat4, n)
	restWeights := make([][]float32, n)
	for i := range doc.Nodes {
		node := &doc.Nodes[i]
		tr := math3d.Transform{
			Pos:   node.Translation,
			Rot:   node.Rotation.Norm(),
			Scale: node.Scale,
		}
		if node.HasMatrix {
			restHasMatrix[i] = true
			restMatrix[i] = node.LocalMatrix
			if pos, rot, scale, ok := node.LocalMatrix.Decompose(); ok {
				tr = math3d.Transform{Pos: pos, Rot: rot, Scale: scale}
			}
		} else {
			restMatrix[i] = tr.ToMat4()
		}
		restLocal[i] = tr
		if len(node.Weights) > 0 {
			w := make([]float32, len(node.Weights))
			copy(w, node.Weights)
			restWeights[i] = w
		} else if node.Mesh >= 0 && node.Mesh < len(doc.Meshes) && len(doc.Meshes[node.Mesh].Weights) > 0 {
			mw := doc.Meshes[node.Mesh].Weights
			w := make([]float32, len(mw))
			copy(w, mw)
			restWeights[i] = w
		}
	}

	animDurations := make([]time.Duration, len(doc.Animations))
	for i := range doc.Animations {
		animDurations[i] = animationDuration(&doc.Animations[i])
	}

	p := &Player{
		doc:               doc,
		parents:           parents,
		evalOrder:         evalOrder,
		restLocal:         restLocal,
		restHasMatrix:     restHasMatrix,
		restMatrix:        restMatrix,
		restWeights:       restWeights,
		animDurations:     animDurations,
		local:             make([]math3d.Transform, n),
		localHasMatrix:    make([]bool, n),
		localMatrix:       make([]math3d.Mat4, n),
		world:             make([]math3d.Mat4, n),
		weights:           make([][]float32, n),
		blendLocal:        make([]math3d.Transform, n),
		blendHasMatrix:    make([]bool, n),
		blendMatrix:       make([]math3d.Mat4, n),
		blendWeights:      make([][]float32, n),
		snapshotLocal:     make([]math3d.Transform, n),
		snapshotHasMatrix: make([]bool, n),
		snapshotMatrix:    make([]math3d.Mat4, n),
		snapshotWeights:   make([][]float32, n),
		animIndex:         -1,
		loop:              true,
		speed:             1,
		prevAnim:          -1,
	}
	if len(doc.Animations) > 0 {
		p.animIndex = 0
	}

	p.resetState(p.local, p.localHasMatrix, p.localMatrix, p.weights)
	p.computeWorldMatrices()
	return p, nil
}

// Play starts or resumes playback of doc.Animations[animIndex].
//
// Switching to a different animation resets Time to 0 and cancels any active
// crossfade. Calling Play with the already-selected animation resumes from the
// current Time if paused, or replays from the start if a non-looping clip has
// already finished. The pose and world transforms are evaluated immediately.
func (p *Player) Play(animIndex int) error {
	if p == nil || p.doc == nil {
		return ErrNilDocument
	}
	if animIndex < 0 || animIndex >= len(p.doc.Animations) {
		return fmt.Errorf("%w: %d (count %d)", ErrInvalidAnimation, animIndex, len(p.doc.Animations))
	}

	if animIndex != p.animIndex {
		p.animIndex = animIndex
		p.time = 0
		p.clearCrossfade()
	} else if !p.playing && !p.loop {
		dur := p.animDurations[animIndex]
		if p.speed >= 0 && dur > 0 && p.time >= dur {
			p.time = 0
		} else if p.speed < 0 && p.time <= 0 {
			p.time = dur
		}
	}

	p.playing = true
	p.evaluate()
	return nil
}

// PlayByName finds the first animation in doc whose Name equals name and calls
// Play on it.
func (p *Player) PlayByName(name string) error {
	idx, err := p.findAnimation(name)
	if err != nil {
		return err
	}
	return p.Play(idx)
}

// Crossfade smoothly blends from the current pose into doc.Animations[animIndex]
// over duration.
//
// If duration <= 0, no animation is active yet, or animIndex is already the
// active animation, Crossfade switches immediately and resets Time to 0.
// Interrupting an in-progress crossfade snapshots the current blended pose so
// transitions never pop.
func (p *Player) Crossfade(animIndex int, duration time.Duration) error {
	if p == nil || p.doc == nil {
		return ErrNilDocument
	}
	if animIndex < 0 || animIndex >= len(p.doc.Animations) {
		return fmt.Errorf("%w: %d (count %d)", ErrInvalidAnimation, animIndex, len(p.doc.Animations))
	}

	if duration <= 0 || p.animIndex < 0 || (animIndex == p.animIndex && p.playing) {
		p.animIndex = animIndex
		p.time = 0
		p.clearCrossfade()
		p.playing = true
		p.evaluate()
		return nil
	}

	if p.blending || !p.playing {
		copy(p.snapshotLocal, p.local)
		copy(p.snapshotHasMatrix, p.localHasMatrix)
		copy(p.snapshotMatrix, p.localMatrix)
		copyWeightSlices(p.snapshotWeights, p.weights)
		p.hasSnapshot = true
	} else {
		p.hasSnapshot = false
	}

	p.prevAnim = p.animIndex
	p.prevTime = p.time
	p.animIndex = animIndex
	p.time = 0
	p.blending = true
	p.blendElapsed = 0
	p.blendDuration = duration
	p.playing = true
	p.evaluate()
	return nil
}

// CrossfadeByName finds the first animation in doc whose Name equals name and
// calls Crossfade on it over duration.
func (p *Player) CrossfadeByName(name string, duration time.Duration) error {
	idx, err := p.findAnimation(name)
	if err != nil {
		return err
	}
	return p.Crossfade(idx, duration)
}

// Pause halts playback while keeping the current Time, pose, and world
// transforms intact.
func (p *Player) Pause() {
	if p == nil {
		return
	}
	p.playing = false
}

// Stop halts playback, resets Time to 0, cancels any active crossfade, and
// restores all nodes to their rest (bind) pose.
func (p *Player) Stop() {
	if p == nil {
		return
	}
	p.playing = false
	p.time = 0
	p.clearCrossfade()
	p.resetState(p.local, p.localHasMatrix, p.localMatrix, p.weights)
	p.computeWorldMatrices()
}

// Seek moves playback to t (wrapped into [0, Duration) when Loop is true, or
// clamped to [0, Duration] otherwise) and immediately re-evaluates node
// transforms and world matrices without changing Playing.
func (p *Player) Seek(t time.Duration) {
	if p == nil || p.animIndex < 0 || p.animIndex >= len(p.animDurations) {
		return
	}
	dur := p.animDurations[p.animIndex]
	if dur <= 0 {
		p.time = 0
	} else if p.loop {
		p.time = modDuration(t, dur)
	} else {
		if t < 0 {
			t = 0
		} else if t > dur {
			t = dur
		}
		p.time = t
	}
	p.evaluate()
}

// Step advances playback and any active crossfade by dt (matching
// game.Loop.Update) and recomputes local node transforms and world matrices.
//
// When paused, stopped, or given dt <= 0, Step is a no-op.
func (p *Player) Step(dt time.Duration) {
	if p == nil || !p.playing || dt <= 0 {
		return
	}
	if p.animIndex < 0 || p.animIndex >= len(p.doc.Animations) {
		return
	}

	delta := scalePlayerDuration(dt, p.speed)
	dur := p.animDurations[p.animIndex]
	nextTime, finished := advancePlayerTime(p.time, delta, dur, p.loop)
	p.time = nextTime

	if p.blending {
		if !p.hasSnapshot && p.prevAnim >= 0 && p.prevAnim < len(p.doc.Animations) {
			prevDur := p.animDurations[p.prevAnim]
			p.prevTime, _ = advancePlayerTime(p.prevTime, delta, prevDur, p.loop)
		}
		p.blendElapsed += dt
		if p.blendElapsed >= p.blendDuration {
			p.clearCrossfade()
		}
	}

	p.evaluate()
	if finished && !p.blending {
		p.playing = false
	}
}

// Update advances playback by dt and returns the resulting node local Pose,
// implementing PoseSampler.
func (p *Player) Update(dt time.Duration) Pose {
	p.Step(dt)
	return p.Pose()
}

// Pose returns a snapshot of every node's current local TRS transform.
func (p *Player) Pose() Pose {
	if p == nil || len(p.local) == 0 {
		return Pose{}
	}
	joints := make([]math3d.Transform, len(p.local))
	copy(joints, p.local)
	return Pose{Joints: joints}
}

// SetLoop controls whether playback wraps at animation boundaries (true) or
// clamps and stops at the end (false).
func (p *Player) SetLoop(loop bool) {
	if p == nil {
		return
	}
	p.loop = loop
}

// Loop reports whether playback wraps at animation boundaries.
func (p *Player) Loop() bool {
	if p == nil {
		return false
	}
	return p.loop
}

// SetSpeed sets the playback speed multiplier. Negative values play in reverse;
// 0 freezes animation time while leaving crossfades able to finish. Non-finite
// values are ignored.
func (p *Player) SetSpeed(speed float32) {
	if p == nil || math.IsNaN(float64(speed)) || math.IsInf(float64(speed), 0) {
		return
	}
	p.speed = speed
}

// Speed returns the current playback speed multiplier.
func (p *Player) Speed() float32 {
	if p == nil {
		return 0
	}
	return p.speed
}

// Playing reports whether the player is actively advancing on Step calls.
func (p *Player) Playing() bool {
	if p == nil {
		return false
	}
	return p.playing
}

// AnimationIndex returns the active animation index, or -1 if doc has no
// animations.
func (p *Player) AnimationIndex() int {
	if p == nil {
		return -1
	}
	return p.animIndex
}

// AnimationName returns the name of the active animation, or "" if none is
// active or the animation is unnamed.
func (p *Player) AnimationName() string {
	if p == nil || p.doc == nil || p.animIndex < 0 || p.animIndex >= len(p.doc.Animations) {
		return ""
	}
	return p.doc.Animations[p.animIndex].Name
}

// Time returns the current playback timestamp of the active animation.
func (p *Player) Time() time.Duration {
	if p == nil {
		return 0
	}
	return p.time
}

// Duration returns the duration of the active animation, or 0 if none is
// active.
func (p *Player) Duration() time.Duration {
	if p == nil || p.animIndex < 0 || p.animIndex >= len(p.animDurations) {
		return 0
	}
	return p.animDurations[p.animIndex]
}

// IsCrossfading reports whether a crossfade transition is in progress.
func (p *Player) IsCrossfading() bool {
	if p == nil {
		return false
	}
	return p.blending
}

// CrossfadeProgress returns the normalized crossfade blend weight in [0, 1],
// or 1 when no crossfade is active.
func (p *Player) CrossfadeProgress() float32 {
	if p == nil || !p.blending || p.blendDuration <= 0 {
		return 1
	}
	return math3d.Clamp(float32(p.blendElapsed)/float32(p.blendDuration), 0, 1)
}

// NodeCount returns the number of nodes tracked by the player.
func (p *Player) NodeCount() int {
	if p == nil {
		return 0
	}
	return len(p.local)
}

// LocalTransform returns the current local TRS transform for nodeIndex, or
// TransformIdentity if nodeIndex is out of range.
func (p *Player) LocalTransform(nodeIndex int) math3d.Transform {
	if p == nil || nodeIndex < 0 || nodeIndex >= len(p.local) {
		return math3d.TransformIdentity()
	}
	return p.local[nodeIndex]
}

// LocalTransforms returns a copy of every node's current local TRS transform.
func (p *Player) LocalTransforms() []math3d.Transform {
	if p == nil || len(p.local) == 0 {
		return nil
	}
	out := make([]math3d.Transform, len(p.local))
	copy(out, p.local)
	return out
}

// WorldTransform returns the current world-space 4x4 matrix for nodeIndex, or
// Mat4Identity if nodeIndex is out of range.
func (p *Player) WorldTransform(nodeIndex int) math3d.Mat4 {
	if p == nil || nodeIndex < 0 || nodeIndex >= len(p.world) {
		return math3d.Mat4Identity()
	}
	return p.world[nodeIndex]
}

// WorldTransforms returns a copy of every node's current world-space 4x4
// transformation matrix.
func (p *Player) WorldTransforms() []math3d.Mat4 {
	if p == nil || len(p.world) == 0 {
		return nil
	}
	out := make([]math3d.Mat4, len(p.world))
	copy(out, p.world)
	return out
}

// MorphWeights returns a copy of the current morph target weights for
// nodeIndex, or nil if nodeIndex has no morph weights or is out of range.
func (p *Player) MorphWeights(nodeIndex int) []float32 {
	if p == nil || nodeIndex < 0 || nodeIndex >= len(p.weights) || len(p.weights[nodeIndex]) == 0 {
		return nil
	}
	out := make([]float32, len(p.weights[nodeIndex]))
	copy(out, p.weights[nodeIndex])
	return out
}

// SkinMatrices computes the glTF 2.0 joint skinning matrices for
// doc.Skins[skinIndex]:
//
//	jointMatrix[i] = inverse(worldMatrix[meshNode]) * worldMatrix[joint[i]] * inverseBindMatrix[i]
//
// where meshNode is the first node in doc.Nodes referencing skinIndex. If no
// node references skinIndex (or the mesh node's world matrix is singular),
// inverse(worldMatrix[meshNode]) is omitted and worldMatrix[joint[i]] *
// inverseBindMatrix[i] is returned directly.
//
// Use SkinMatricesForNode when multiple mesh nodes share the same skin index.
func (p *Player) SkinMatrices(skinIndex int) ([]math3d.Mat4, error) {
	if p == nil || p.doc == nil {
		return nil, ErrNilDocument
	}
	if skinIndex < 0 || skinIndex >= len(p.doc.Skins) {
		return nil, fmt.Errorf("%w: %d (count %d)", ErrInvalidSkin, skinIndex, len(p.doc.Skins))
	}

	meshNode := -1
	for i := range p.doc.Nodes {
		if p.doc.Nodes[i].Skin == skinIndex {
			meshNode = i
			break
		}
	}
	return p.computeSkinMatrices(skinIndex, meshNode)
}

// SkinMatricesForNode computes the glTF 2.0 joint skinning matrices for the
// skin attached to doc.Nodes[meshNodeIndex], normalizing joint world matrices
// by inverse(worldMatrix[meshNodeIndex]).
func (p *Player) SkinMatricesForNode(meshNodeIndex int) ([]math3d.Mat4, error) {
	if p == nil || p.doc == nil {
		return nil, ErrNilDocument
	}
	if meshNodeIndex < 0 || meshNodeIndex >= len(p.doc.Nodes) {
		return nil, fmt.Errorf("%w: mesh node %d out of range", ErrInvalidSkin, meshNodeIndex)
	}
	skinIndex := p.doc.Nodes[meshNodeIndex].Skin
	if skinIndex < 0 || skinIndex >= len(p.doc.Skins) {
		return nil, fmt.Errorf("%w: node %d has invalid skin %d", ErrInvalidSkin, meshNodeIndex, skinIndex)
	}
	return p.computeSkinMatrices(skinIndex, meshNodeIndex)
}

func (p *Player) computeSkinMatrices(skinIndex, meshNode int) ([]math3d.Mat4, error) {
	skin := &p.doc.Skins[skinIndex]
	jointCount := len(skin.Joints)
	if jointCount == 0 {
		return nil, nil
	}

	var invMesh math3d.Mat4
	hasInvMesh := false
	if meshNode >= 0 && meshNode < len(p.world) {
		if inv, ok := p.world[meshNode].Inverse(); ok {
			invMesh = inv
			hasInvMesh = true
		}
	}

	out := make([]math3d.Mat4, jointCount)
	for i, jointNode := range skin.Joints {
		if jointNode < 0 || jointNode >= len(p.world) {
			return nil, fmt.Errorf("anim3d: skin %d joint %d references out-of-range node %d", skinIndex, i, jointNode)
		}
		ibm := math3d.Mat4Identity()
		if i < len(skin.InverseBindMatrices) {
			ibm = skin.InverseBindMatrices[i]
		}
		jm := p.world[jointNode].Mul(ibm)
		if hasInvMesh {
			jm = invMesh.Mul(jm)
		}
		out[i] = jm
	}
	return out, nil
}

// ApplyToScene copies current local TRS transforms onto nodes in scene whose
// Name matches a named node in the glTF document, refreshes scene transforms,
// and returns the number of scene nodes updated.
func (p *Player) ApplyToScene(scene *scene3d.Scene) int {
	if p == nil || p.doc == nil || scene == nil || len(p.local) == 0 {
		return 0
	}

	byName := make(map[string]*scene3d.Node, len(p.local))
	scene.Walk(func(n *scene3d.Node) bool {
		if n.Name != "" {
			if _, exists := byName[n.Name]; !exists {
				byName[n.Name] = n
			}
		}
		return true
	})

	updated := 0
	for i := range p.doc.Nodes {
		name := p.doc.Nodes[i].Name
		if name == "" {
			continue
		}
		if sn, ok := byName[name]; ok {
			sn.SetLocal(p.local[i])
			updated++
		}
	}
	if updated > 0 {
		scene.UpdateTransforms()
	}
	return updated
}

// ApplyToNodes copies current local TRS transforms by index onto nodes (where
// nodes[i] corresponds to doc.Nodes[i]) and returns the number of non-nil nodes
// updated.
func (p *Player) ApplyToNodes(nodes []*scene3d.Node) int {
	if p == nil || len(nodes) == 0 {
		return 0
	}
	limit := min(len(nodes), len(p.local))
	updated := 0
	for i := range limit {
		if nodes[i] != nil {
			nodes[i].SetLocal(p.local[i])
			updated++
		}
	}
	return updated
}

func (p *Player) findAnimation(name string) (int, error) {
	if p == nil || p.doc == nil {
		return -1, ErrNilDocument
	}
	for i := range p.doc.Animations {
		if p.doc.Animations[i].Name == name {
			return i, nil
		}
	}
	return -1, fmt.Errorf("%w: %q", ErrAnimationNotFound, name)
}

func (p *Player) clearCrossfade() {
	p.blending = false
	p.blendElapsed = 0
	p.blendDuration = 0
	p.prevAnim = -1
	p.prevTime = 0
	p.hasSnapshot = false
}

func (p *Player) evaluate() {
	if p.animIndex < 0 || p.animIndex >= len(p.doc.Animations) {
		p.resetState(p.local, p.localHasMatrix, p.localMatrix, p.weights)
		p.computeWorldMatrices()
		return
	}

	if !p.blending {
		p.sampleAnimationInto(p.animIndex, p.time, p.local, p.localHasMatrix, p.localMatrix, p.weights)
		p.computeWorldMatrices()
		return
	}

	alpha := p.CrossfadeProgress()
	p.sampleAnimationInto(p.animIndex, p.time, p.blendLocal, p.blendHasMatrix, p.blendMatrix, p.blendWeights)

	var prevLocal []math3d.Transform
	var prevHasMatrix []bool
	var prevMatrix []math3d.Mat4
	var prevWeights [][]float32
	if p.hasSnapshot {
		prevLocal = p.snapshotLocal
		prevHasMatrix = p.snapshotHasMatrix
		prevMatrix = p.snapshotMatrix
		prevWeights = p.snapshotWeights
	} else if p.prevAnim >= 0 && p.prevAnim < len(p.doc.Animations) {
		p.sampleAnimationInto(p.prevAnim, p.prevTime, p.local, p.localHasMatrix, p.localMatrix, p.weights)
		prevLocal = p.local
		prevHasMatrix = p.localHasMatrix
		prevMatrix = p.localMatrix
		prevWeights = p.weights
	} else {
		prevLocal = p.restLocal
		prevHasMatrix = p.restHasMatrix
		prevMatrix = p.restMatrix
		prevWeights = p.restWeights
	}

	for i := range p.local {
		if prevHasMatrix[i] && p.blendHasMatrix[i] && prevMatrix[i] == p.blendMatrix[i] {
			p.local[i] = p.blendLocal[i]
			p.localHasMatrix[i] = true
			p.localMatrix[i] = p.blendMatrix[i]
		} else {
			p.local[i] = prevLocal[i].Lerp(p.blendLocal[i], alpha)
			p.localHasMatrix[i] = false
		}

		pw := prevWeights[i]
		cw := p.blendWeights[i]
		wLen := max(len(pw), len(cw))
		if wLen == 0 {
			p.weights[i] = p.weights[i][:0]
		} else {
			dst := growFloat32(p.weights[i], wLen)
			for k := range wLen {
				var a, b float32
				if k < len(pw) {
					a = pw[k]
				}
				if k < len(cw) {
					b = cw[k]
				}
				dst[k] = math3d.Lerp(a, b, alpha)
			}
			p.weights[i] = dst
		}
	}

	p.computeWorldMatrices()
}

func (p *Player) resetState(
	dstLocal []math3d.Transform,
	dstHasMatrix []bool,
	dstMatrix []math3d.Mat4,
	dstWeights [][]float32,
) {
	copy(dstLocal, p.restLocal)
	copy(dstHasMatrix, p.restHasMatrix)
	copy(dstMatrix, p.restMatrix)
	copyWeightSlices(dstWeights, p.restWeights)
}

func (p *Player) sampleAnimationInto(
	animIndex int,
	t time.Duration,
	dstLocal []math3d.Transform,
	dstHasMatrix []bool,
	dstMatrix []math3d.Mat4,
	dstWeights [][]float32,
) {
	p.resetState(dstLocal, dstHasMatrix, dstMatrix, dstWeights)

	anim := &p.doc.Animations[animIndex]
	tSec := t.Seconds()
	for ci := range anim.Channels {
		ch := &anim.Channels[ci]
		nodeIdx := ch.TargetNode
		if nodeIdx < 0 || nodeIdx >= len(dstLocal) {
			continue
		}
		applyChannelSample(ch, tSec, &dstLocal[nodeIdx], &dstHasMatrix[nodeIdx], &dstWeights[nodeIdx])
	}
}

func (p *Player) computeWorldMatrices() {
	for _, idx := range p.evalOrder {
		var localMat math3d.Mat4
		if p.localHasMatrix[idx] {
			localMat = p.localMatrix[idx]
		} else {
			localMat = p.local[idx].ToMat4()
		}
		parent := p.parents[idx]
		if parent < 0 {
			p.world[idx] = localMat
		} else {
			p.world[idx] = p.world[parent].Mul(localMat)
		}
	}
}

// applyChannelSample evaluates ch at tSec (in seconds) and writes the result
// directly into the target node's local TRS or morph target weights without
// heap allocations for TRS channels.
func applyChannelSample(
	ch *gltf.AnimationChannel,
	tSec float64,
	dstLocal *math3d.Transform,
	dstHasMatrix *bool,
	dstWeights *[]float32,
) {
	n := len(ch.Times)
	stride := ch.Stride
	if n == 0 || stride <= 0 {
		return
	}

	isCubic := ch.Interpolation == gltf.InterpolationCubicSpline
	keyMult := 1
	if isCubic {
		keyMult = 3
	}
	if len(ch.Values) < n*keyMult*stride {
		return
	}

	valOffset := func(key int) int {
		if isCubic {
			return (key*3 + 1) * stride
		}
		return key * stride
	}

	if math.IsNaN(tSec) || math.IsInf(tSec, 0) || tSec <= float64(ch.Times[0]) || n == 1 {
		off := valOffset(0)
		applyRawValues(ch.Path, ch.Values[off:off+stride], dstLocal, dstHasMatrix, dstWeights)
		return
	}
	if tSec >= float64(ch.Times[n-1]) {
		off := valOffset(n - 1)
		applyRawValues(ch.Path, ch.Values[off:off+stride], dstLocal, dstHasMatrix, dstWeights)
		return
	}

	// Largest i such that Times[i] <= tSec < Times[i+1].
	i := sort.Search(n, func(k int) bool { return float64(ch.Times[k]) > tSec }) - 1
	if i < 0 {
		i = 0
	}
	if i >= n-1 {
		i = n - 2
	}

	if ch.Interpolation == gltf.InterpolationStep {
		off := valOffset(i)
		applyRawValues(ch.Path, ch.Values[off:off+stride], dstLocal, dstHasMatrix, dstWeights)
		return
	}

	t0 := float64(ch.Times[i])
	t1 := float64(ch.Times[i+1])
	dt := t1 - t0
	if dt <= 0 {
		off := valOffset(i)
		applyRawValues(ch.Path, ch.Values[off:off+stride], dstLocal, dstHasMatrix, dstWeights)
		return
	}
	s := float32((tSec - t0) / dt)
	s = math3d.Clamp(s, 0, 1)

	if isCubic {
		dt32 := float32(dt)
		v0Off := (i*3 + 1) * stride
		b0Off := (i*3 + 2) * stride
		a1Off := ((i+1)*3 + 0) * stride
		v1Off := ((i+1)*3 + 1) * stride

		s2 := s * s
		s3 := s2 * s
		h00 := 2*s3 - 3*s2 + 1
		h10 := (s3 - 2*s2 + s) * dt32
		h01 := -2*s3 + 3*s2
		h11 := (s3 - s2) * dt32

		eval := func(k int) float32 {
			return h00*ch.Values[v0Off+k] + h10*ch.Values[b0Off+k] + h01*ch.Values[v1Off+k] + h11*ch.Values[a1Off+k]
		}

		switch ch.Path {
		case gltf.PathTranslation:
			if stride >= 3 {
				dstLocal.Pos = math3d.Vec3{X: eval(0), Y: eval(1), Z: eval(2)}
				*dstHasMatrix = false
			}
		case gltf.PathScale:
			if stride >= 3 {
				dstLocal.Scale = math3d.Vec3{X: eval(0), Y: eval(1), Z: eval(2)}
				*dstHasMatrix = false
			}
		case gltf.PathRotation:
			if stride >= 4 {
				// glTF 2.0 Appendix C: normalize the Hermite-interpolated quaternion.
				dstLocal.Rot = math3d.Quat{X: eval(0), Y: eval(1), Z: eval(2), W: eval(3)}.Norm()
				*dstHasMatrix = false
			}
		case gltf.PathWeights:
			dst := growFloat32(*dstWeights, stride)
			for k := range stride {
				dst[k] = eval(k)
			}
			*dstWeights = dst
		}
		return
	}

	// LINEAR interpolation.
	off0 := i * stride
	off1 := (i + 1) * stride
	v0 := ch.Values[off0 : off0+stride]
	v1 := ch.Values[off1 : off1+stride]

	switch ch.Path {
	case gltf.PathTranslation:
		if stride >= 3 {
			a := math3d.Vec3{X: v0[0], Y: v0[1], Z: v0[2]}
			b := math3d.Vec3{X: v1[0], Y: v1[1], Z: v1[2]}
			dstLocal.Pos = a.Lerp(b, s)
			*dstHasMatrix = false
		}
	case gltf.PathScale:
		if stride >= 3 {
			a := math3d.Vec3{X: v0[0], Y: v0[1], Z: v0[2]}
			b := math3d.Vec3{X: v1[0], Y: v1[1], Z: v1[2]}
			dstLocal.Scale = a.Lerp(b, s)
			*dstHasMatrix = false
		}
	case gltf.PathRotation:
		if stride >= 4 {
			a := math3d.Quat{X: v0[0], Y: v0[1], Z: v0[2], W: v0[3]}
			b := math3d.Quat{X: v1[0], Y: v1[1], Z: v1[2], W: v1[3]}
			// Quat.Slerp normalizes both inputs and performs shortest-path
			// hemisphere alignment when a.Dot(b) < 0.
			dstLocal.Rot = a.Slerp(b, s)
			*dstHasMatrix = false
		}
	case gltf.PathWeights:
		dst := growFloat32(*dstWeights, stride)
		for k := range stride {
			dst[k] = math3d.Lerp(v0[k], v1[k], s)
		}
		*dstWeights = dst
	}
}

func applyRawValues(
	path gltf.TargetPath,
	vals []float32,
	dstLocal *math3d.Transform,
	dstHasMatrix *bool,
	dstWeights *[]float32,
) {
	switch path {
	case gltf.PathTranslation:
		if len(vals) >= 3 {
			dstLocal.Pos = math3d.Vec3{X: vals[0], Y: vals[1], Z: vals[2]}
			*dstHasMatrix = false
		}
	case gltf.PathScale:
		if len(vals) >= 3 {
			dstLocal.Scale = math3d.Vec3{X: vals[0], Y: vals[1], Z: vals[2]}
			*dstHasMatrix = false
		}
	case gltf.PathRotation:
		if len(vals) >= 4 {
			dstLocal.Rot = math3d.Quat{X: vals[0], Y: vals[1], Z: vals[2], W: vals[3]}.Norm()
			*dstHasMatrix = false
		}
	case gltf.PathWeights:
		dst := growFloat32(*dstWeights, len(vals))
		copy(dst, vals)
		*dstWeights = dst
	}
}

// buildNodeOrder validates the glTF node tree and returns each node's parent
// index (-1 for roots) and a parent-before-child topological evaluation order.
func buildNodeOrder(nodes []gltf.Node) (parents []int, evalOrder []int, err error) {
	n := len(nodes)
	if n == 0 {
		return nil, nil, nil
	}

	parents = make([]int, n)
	for i := range parents {
		parents[i] = -1
	}

	for parentIdx := range nodes {
		for _, childIdx := range nodes[parentIdx].Children {
			if childIdx < 0 || childIdx >= n {
				return nil, nil, fmt.Errorf("anim3d: node %d references out-of-range child %d", parentIdx, childIdx)
			}
			if childIdx == parentIdx {
				return nil, nil, fmt.Errorf("anim3d: node %d is a child of itself (cycle)", parentIdx)
			}
			if parents[childIdx] != -1 {
				return nil, nil, fmt.Errorf("anim3d: node %d has multiple parents (%d and %d)", childIdx, parents[childIdx], parentIdx)
			}
			parents[childIdx] = parentIdx
		}
	}

	// 0 = unvisited, 1 = visiting, 2 = visited
	state := make([]uint8, n)
	evalOrder = make([]int, 0, n)

	var visit func(idx int) error
	visit = func(idx int) error {
		switch state[idx] {
		case 1:
			return fmt.Errorf("anim3d: cycle detected involving node %d", idx)
		case 2:
			return fmt.Errorf("anim3d: node %d visited more than once", idx)
		}
		state[idx] = 1
		evalOrder = append(evalOrder, idx)
		for _, childIdx := range nodes[idx].Children {
			if err := visit(childIdx); err != nil {
				return err
			}
		}
		state[idx] = 2
		return nil
	}

	for i := range nodes {
		if parents[i] == -1 {
			if err := visit(i); err != nil {
				return nil, nil, err
			}
		}
	}
	for i, s := range state {
		if s != 2 {
			return nil, nil, fmt.Errorf("anim3d: cycle detected in node hierarchy at node %d", i)
		}
	}

	return parents, evalOrder, nil
}

func animationDuration(anim *gltf.Animation) time.Duration {
	if anim == nil {
		return 0
	}
	maxSec := anim.Duration
	for i := range anim.Channels {
		times := anim.Channels[i].Times
		if len(times) > 0 && times[len(times)-1] > maxSec {
			maxSec = times[len(times)-1]
		}
	}
	return durationFromSeconds(maxSec)
}

func durationFromSeconds(sec float32) time.Duration {
	f := float64(sec)
	if sec <= 0 || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	ns := f * float64(time.Second)
	if ns >= float64(math.MaxInt64/2) {
		return time.Duration(math.MaxInt64 / 2)
	}
	return time.Duration(math.Round(ns))
}

func scalePlayerDuration(dt time.Duration, speed float32) time.Duration {
	if speed == 0 || dt == 0 {
		return 0
	}
	if speed == 1 {
		return dt
	}
	scaled := float64(dt) * float64(speed)
	const limit = float64(math.MaxInt64 / 2)
	if scaled >= limit {
		return time.Duration(math.MaxInt64 / 2)
	}
	if scaled <= -limit {
		return -time.Duration(math.MaxInt64 / 2)
	}
	return time.Duration(math.Round(scaled))
}

func advancePlayerTime(curr, delta, dur time.Duration, loop bool) (time.Duration, bool) {
	if dur <= 0 {
		if loop {
			return 0, false
		}
		return 0, delta != 0
	}
	next := curr + delta
	if loop {
		return modDuration(next, dur), false
	}
	if delta > 0 && next >= dur {
		return dur, true
	}
	if delta < 0 && next <= 0 {
		return 0, true
	}
	if next < 0 {
		return 0, false
	}
	if next > dur {
		return dur, false
	}
	return next, false
}

func copyWeightSlices(dst, src [][]float32) {
	for i := range dst {
		if len(src[i]) == 0 {
			dst[i] = dst[i][:0]
			continue
		}
		buf := growFloat32(dst[i], len(src[i]))
		copy(buf, src[i])
		dst[i] = buf
	}
}

func growFloat32(buf []float32, n int) []float32 {
	if cap(buf) >= n {
		return buf[:n]
	}
	return make([]float32, n)
}
