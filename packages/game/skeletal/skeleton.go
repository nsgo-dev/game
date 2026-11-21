package skeletal

import (
	"fmt"
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

// BoneTransform is a bone's position, rotation and scale in a drawing's coordinates.
type BoneTransform struct {
	X, Y           float32
	Rotation       float32
	ScaleX, ScaleY float32
}

// Skeleton is a live instance of a SkeletonData rig: its root placement, active
// skin, per-slot attachment overrides, animation playback and computed bone
// transforms.
//
// NewSkeleton returns a skeleton at full size and opacity in bind pose, with
// the "default" skin active when the rig defines one.
type Skeleton struct {
	// X and Y place the skeleton's root origin in the drawing.
	X, Y float32
	// Rotation turns the skeleton about its root origin, in radians, clockwise on the screen.
	Rotation float32
	// ScaleX and ScaleY scale the skeleton about its root origin; a negative value mirrors it.
	ScaleX, ScaleY float32
	// FlipX and FlipY mirror the skeleton across its root axes before Rotation.
	FlipX, FlipY bool
	// Alpha multiplies every slot's opacity, from 0 (draws nothing) to 1.
	Alpha float32
	// Z is passed to each sprite drawn by Draw.
	Z int

	data  *SkeletonData
	atlas *sprite.Atlas
	skin  string

	localBones       []BoneTransform
	worldBones       []BoneTransform
	slotAlpha        []float32
	slotAnimFrame    []string
	slotAnimHasFrame []bool
	slotOverride     []string
	slotHasOverride  []bool

	// Reusable scratch buffers for animation sampling and crossfading.
	fromBones    []BoneTransform
	toBones      []BoneTransform
	fromAlpha    []float32
	toAlpha      []float32
	fromFrame    []string
	fromHasFrame []bool
	toFrame      []string
	toHasFrame   []bool

	current          trackState
	outgoing         trackState
	outgoingSnapshot bool
	blending         bool
	blendElapsed     time.Duration
	blendDuration    time.Duration

	sprites []sprite.Sprite
	batch   sprite.Batch
}

// NewSkeleton returns a skeleton for data drawing frames from atlas, at full
// scale and opacity in its bind pose. If data defines a "default" skin, it is
// active initially.
func NewSkeleton(data *SkeletonData, atlas *sprite.Atlas) *Skeleton {
	s := &Skeleton{
		ScaleX: 1,
		ScaleY: 1,
		Alpha:  1,
		data:   data,
		atlas:  atlas,
	}
	if data == nil {
		return s
	}
	if data.boneIndex == nil {
		_ = data.compile()
	}

	nb := len(data.Bones)
	ns := len(data.Slots)
	s.localBones = make([]BoneTransform, nb)
	s.worldBones = make([]BoneTransform, nb)
	s.slotAlpha = make([]float32, ns)
	s.slotAnimFrame = make([]string, ns)
	s.slotAnimHasFrame = make([]bool, ns)
	s.slotOverride = make([]string, ns)
	s.slotHasOverride = make([]bool, ns)

	s.fromBones = make([]BoneTransform, nb)
	s.toBones = make([]BoneTransform, nb)
	s.fromAlpha = make([]float32, ns)
	s.toAlpha = make([]float32, ns)
	s.fromFrame = make([]string, ns)
	s.fromHasFrame = make([]bool, ns)
	s.toFrame = make([]string, ns)
	s.toHasFrame = make([]bool, ns)

	s.sprites = make([]sprite.Sprite, ns)
	s.batch = sprite.Batch{
		Atlas:   atlas,
		Sprites: make([]*sprite.Sprite, 0, ns),
	}

	if _, ok := data.Skins["default"]; ok {
		s.skin = "default"
	}
	s.ResetToBindPose()
	return s
}

// Data returns the immutable SkeletonData this skeleton was created from.
func (s *Skeleton) Data() *SkeletonData { return s.data }

// Skin returns the name of the active skin, or empty if none is active.
func (s *Skeleton) Skin() string { return s.skin }

// SetSkin switches the active skin to name. Slots missing from name fall back
// to the "default" skin. It returns an error if data does not define name.
func (s *Skeleton) SetSkin(name string) error {
	if s.data == nil {
		return fmt.Errorf("skeletal: unknown skin %q", name)
	}
	if _, ok := s.data.Skins[name]; !ok {
		return fmt.Errorf("skeletal: unknown skin %q", name)
	}
	s.skin = name
	return nil
}

// SetAttachment overrides the atlas frame drawn by slot. An empty frame hides
// the slot. Unknown slot names are ignored.
func (s *Skeleton) SetAttachment(slot, frame string) {
	if s.data == nil {
		return
	}
	idx, ok := s.data.slotIndex[slot]
	if !ok {
		return
	}
	s.slotOverride[idx] = frame
	s.slotHasOverride[idx] = true
}

// ClearAttachment removes any manual override set on slot by SetAttachment,
// returning it to the active animation or skin attachment.
func (s *Skeleton) ClearAttachment(slot string) {
	if s.data == nil {
		return
	}
	idx, ok := s.data.slotIndex[slot]
	if !ok {
		return
	}
	s.slotOverride[idx] = ""
	s.slotHasOverride[idx] = false
}

// ResetToBindPose restores every bone and slot to its bind pose, clears any
// active animation or crossfade, and updates world transforms.
func (s *Skeleton) ResetToBindPose() {
	if s.data == nil {
		return
	}
	s.current = trackState{}
	s.outgoing = trackState{}
	s.outgoingSnapshot = false
	s.blending = false
	s.blendElapsed = 0
	s.blendDuration = 0
	s.resetPoseToBind(s.localBones, s.slotAlpha, s.slotAnimFrame, s.slotAnimHasFrame)
	s.UpdateWorldTransforms()
}

func (s *Skeleton) resetPoseToBind(bones []BoneTransform, alpha []float32, frames []string, hasFrame []bool) {
	for i, b := range s.data.Bones {
		bones[i] = BoneTransform{
			X:        b.X,
			Y:        b.Y,
			Rotation: b.Rotation,
			ScaleX:   b.ScaleX,
			ScaleY:   b.ScaleY,
		}
	}
	for i, sl := range s.data.Slots {
		alpha[i] = sl.Alpha
		frames[i] = ""
		hasFrame[i] = false
	}
}

// UpdateWorldTransforms computes every bone's world transform down the
// parent-child tree from the skeleton's root transform and current local pose.
func (s *Skeleton) UpdateWorldTransforms() {
	if s.data == nil {
		return
	}
	root := s.rootTransform()
	for _, bi := range s.data.boneOrder {
		lb := s.localBones[bi]
		parent := root
		if pi := s.data.boneParent[bi]; pi >= 0 {
			parent = s.worldBones[pi]
		}
		s.worldBones[bi] = composeTransform(parent, lb.X, lb.Y, lb.Rotation, lb.ScaleX, lb.ScaleY)
	}
}

// WorldBone returns the last computed world transform of the named bone.
// Call UpdateWorldTransforms (or Update) after changing root fields before
// reading it for attachments, hitboxes or particles.
func (s *Skeleton) WorldBone(name string) (BoneTransform, bool) {
	if s.data == nil {
		return BoneTransform{}, false
	}
	idx, ok := s.data.boneIndex[name]
	if !ok {
		return BoneTransform{}, false
	}
	return s.worldBones[idx], true
}

// Draw updates world transforms and draws every visible slot into d in slot
// draw order through a reusable sprite.Batch.
func (s *Skeleton) Draw(d *core.Drawing) {
	if s.data == nil || s.atlas == nil || s.Alpha <= 0 || s.ScaleX == 0 || s.ScaleY == 0 {
		return
	}
	s.UpdateWorldTransforms()
	s.batch.Atlas = s.atlas
	s.batch.Sprites = s.batch.Sprites[:0]

	defSkin := s.data.Skins["default"]
	var activeSkin Skin
	if s.skin != "" && s.skin != "default" {
		activeSkin = s.data.Skins[s.skin]
	}

	for i, slot := range s.data.Slots {
		att := Attachment{Frame: slot.Attachment, ScaleX: 1, ScaleY: 1}
		if a, ok := defSkin[slot.Name]; ok {
			att = a
		}
		if activeSkin != nil {
			if a, ok := activeSkin[slot.Name]; ok {
				att = a
			}
		}

		frameName := att.Frame
		if s.slotAnimHasFrame[i] {
			frameName = s.slotAnimFrame[i]
		}
		if s.slotHasOverride[i] {
			frameName = s.slotOverride[i]
		}
		if frameName == "" {
			continue
		}
		frame, ok := s.atlas.Frames[frameName]
		if !ok {
			continue
		}

		alpha := min(max(s.Alpha*s.slotAlpha[i], 0), 1)
		if alpha <= 0 {
			continue
		}

		wt := composeTransform(s.worldBones[s.data.slotBone[i]], att.X, att.Y, att.Rotation, att.ScaleX, att.ScaleY)
		sp := &s.sprites[i]
		*sp = sprite.Sprite{
			Frame:    frame,
			X:        wt.X,
			Y:        wt.Y,
			OriginX:  att.OriginX,
			OriginY:  att.OriginY,
			Rotation: wt.Rotation,
			ScaleX:   wt.ScaleX,
			ScaleY:   wt.ScaleY,
			Alpha:    alpha,
			Z:        s.Z,
		}
		s.batch.Sprites = append(s.batch.Sprites, sp)
	}
	s.batch.Draw(d)
}

func (s *Skeleton) rootTransform() BoneTransform {
	sx, sy := s.ScaleX, s.ScaleY
	if s.FlipX {
		sx = -sx
	}
	if s.FlipY {
		sy = -sy
	}
	return BoneTransform{
		X:        s.X,
		Y:        s.Y,
		Rotation: s.Rotation,
		ScaleX:   sx,
		ScaleY:   sy,
	}
}

func composeTransform(p BoneTransform, lx, ly, lrot, lsx, lsy float32) BoneTransform {
	sx := lx * p.ScaleX
	sy := ly * p.ScaleY
	var wx, wy float32
	if p.Rotation == 0 {
		wx = p.X + sx
		wy = p.Y + sy
	} else {
		sn, cs := math.Sincos(float64(p.Rotation))
		sin, cos := float32(sn), float32(cs)
		wx = p.X + cos*sx - sin*sy
		wy = p.Y + sin*sx + cos*sy
	}
	rot := p.Rotation + lrot
	if p.ScaleX*p.ScaleY < 0 {
		rot = p.Rotation - lrot
	}
	return BoneTransform{
		X:        wx,
		Y:        wy,
		Rotation: rot,
		ScaleX:   p.ScaleX * lsx,
		ScaleY:   p.ScaleY * lsy,
	}
}
