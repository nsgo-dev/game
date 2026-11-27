package skeletal

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"slices"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/nsgo-dev/game/pkg/loaderr"
)

// FormatVersion is the schema version that Load reads and Encode writes.
const FormatVersion = 1

// MaxSeconds is the longest keyframe time or animation duration, in seconds, that a rig may hold. Within it, a time written in seconds by Encode loads back to the same nanosecond.
const MaxSeconds = 1_000_000

// SkeletonData is the immutable description of a 2D skeletal rig: its bones,
// draw-ordered slots, skins and animations. Share one SkeletonData across as
// many Skeleton instances as need it.
type SkeletonData struct {
	// Version is the schema version of the rig, FormatVersion.
	Version int
	// Name is the rig's name, if the file gave one.
	Name string
	// Bones are the rig's bones in bind pose.
	Bones []BoneData
	// Slots are the rig's slots in draw order, back to front.
	Slots []SlotData
	// Skins maps each skin's name to the attachments it places on slots.
	Skins map[string]Skin
	// Animations maps each animation's name to its timelines.
	Animations map[string]*Animation

	boneIndex  map[string]int
	slotIndex  map[string]int
	boneParent []int
	boneOrder  []int
	slotBone   []int
}

// BoneData describes one bone's bind pose relative to its parent.
type BoneData struct {
	// Name uniquely identifies the bone in the skeleton.
	Name string
	// Parent is the parent bone's Name, or empty for a root bone.
	Parent string
	// X and Y are the bone's bind-pose offset in its parent's space.
	X, Y float32
	// Rotation is the bone's bind-pose rotation in radians, clockwise on the screen.
	Rotation float32
	// ScaleX and ScaleY are the bone's bind-pose scales, 1 when omitted in the file.
	ScaleX, ScaleY float32
	// Length is an optional bone length for editors and debug overlays.
	Length float32
}

// SlotData describes one draw slot attached to a bone.
type SlotData struct {
	// Name uniquely identifies the slot in the skeleton.
	Name string
	// Bone is the name of the bone whose transform the slot follows.
	Bone string
	// Attachment is an optional default atlas frame name when no skin supplies one.
	Attachment string
	// Alpha is the slot's bind-pose opacity in [0, 1], 1 when omitted in the file.
	Alpha float32
}

// Skin maps slot names to their attachments.
type Skin map[string]Attachment

// Attachment places one atlas frame on a slot, relative to the slot's bone.
type Attachment struct {
	// Frame is the atlas frame name to draw; empty hides the slot.
	Frame string
	// X and Y are the attachment's offset in its bone's local space.
	X, Y float32
	// Rotation is the attachment's local rotation in radians, clockwise on the screen.
	Rotation float32
	// ScaleX and ScaleY are the attachment's local scales, 1 when omitted in the file.
	ScaleX, ScaleY float32
	// OriginX and OriginY are the pivot point of the untrimmed atlas frame, matching sprite.Sprite.
	OriginX, OriginY float32
}

// CurveType selects how a keyframe interpolates to the next keyframe.
type CurveType uint8

const (
	// CurveLinear interpolates evenly between keyframes.
	CurveLinear CurveType = iota
	// CurveStepped holds the keyframe's value until the next keyframe's time.
	CurveStepped
	// CurveBezier eases with a cubic Bezier curve defined by [cx1, cy1, cx2, cy2].
	CurveBezier
)

// Animation holds the bone and slot timelines for one named motion.
type Animation struct {
	// Name is the animation's key in SkeletonData.Animations.
	Name string
	// Duration is the length of one pass of the animation.
	Duration time.Duration
	// Loop is the file's default looping preference for this animation.
	Loop bool
	// Bones maps bone names to their transform timelines.
	Bones map[string]BoneTimeline
	// Slots maps slot names to their attachment and alpha timelines.
	Slots map[string]SlotTimeline

	boneTimelines []indexedBoneTimeline
	slotTimelines []indexedSlotTimeline
}

type indexedBoneTimeline struct {
	boneIdx int
	tl      BoneTimeline
}

type indexedSlotTimeline struct {
	slotIdx int
	tl      SlotTimeline
}

// BoneTimeline holds the optional transform tracks for one bone, applied
// relative to its bind pose: Translate and Rotate add; Scale multiplies.
type BoneTimeline struct {
	Translate []Vec2Keyframe
	Rotate    []FloatKeyframe
	Scale     []Vec2Keyframe
}

// SlotTimeline holds the optional attachment and opacity tracks for one slot.
type SlotTimeline struct {
	Attachment []AttachmentKeyframe
	Alpha      []FloatKeyframe
}

// Vec2Keyframe is a two-component keyframe (translation offset or scale factor).
type Vec2Keyframe struct {
	Time   time.Duration
	X, Y   float32
	Curve  CurveType
	Bezier [4]float32
}

// FloatKeyframe is a scalar keyframe (rotation angle in radians or slot alpha).
type FloatKeyframe struct {
	Time   time.Duration
	Value  float32
	Curve  CurveType
	Bezier [4]float32
}

// AttachmentKeyframe swaps a slot's atlas frame at Time; an empty Frame hides the slot.
type AttachmentKeyframe struct {
	Time  time.Duration
	Frame string
}

// Load parses and validates a version-1 skeletal rig from YAML (.skel.yaml).
//
// It rejects empty input, unknown fields, duplicate keys, trailing YAML documents, unknown schema versions, non-finite numbers, duplicate bone or slot names, missing parents, cycles in the bone hierarchy, slots or skins referencing missing bones or slots, and animations with unknown targets, unsorted or out-of-range keyframes, or invalid curves. A version-1 rig written as JSON loads too when the JSON is also valid YAML: keys spelled exactly as the schema spells them (matching is case-sensitive) and no `\/` escape, which YAML does not have.
//
// Its errors are *loaderr.Error values that give the line and field of the problem and how to fix it; a caller that read data from a file names it with loaderr.WithFile.
func Load(data []byte) (*SkeletonData, error) {
	sd, err := load(data)
	return sd, loaderr.Locate(err, "", data, "correct the rig to match the version 1 format in docs/en/skeletal.md")
}

// Fixes shared by several checks.
const (
	fixKeyTime = "give keyframes finite times of 0 or more seconds, in increasing order"
	fixCurve   = `use curve "linear", "stepped" or "bezier" with bezier: [cx1, cy1, cx2, cy2] and cx1, cx2 in [0, 1]`
	fixFinite  = "write a finite number"
)

func load(data []byte) (*SkeletonData, error) {
	raw, err := decodeRaw(data)
	if err != nil {
		return nil, err
	}
	if raw.Version != FormatVersion {
		return nil, &loaderr.Error{Field: "version", Problem: fmt.Sprintf("unsupported version %d", raw.Version), Expected: fmt.Sprint(FormatVersion), Fix: fmt.Sprintf(`set "version": %d`, FormatVersion)}
	}

	sd := &SkeletonData{
		Version: raw.Version,
		Name:    raw.Name,
		Bones:   make([]BoneData, len(raw.Bones)),
		Slots:   make([]SlotData, len(raw.Slots)),
	}

	for i, rb := range raw.Bones {
		if rb.Name == "" {
			return nil, loaderr.Errorf(loaderr.Path("bones", i), `give the bone a "name"`, "bone %d has no name", i)
		}
		if err := checkFinite(rb.X, rb.Y, rb.Rotation, defaultFloat32(rb.ScaleX, 1), defaultFloat32(rb.ScaleY, 1), rb.Length); err != nil {
			return nil, loaderr.Errorf(loaderr.Path("bones", i), fixFinite, "skeletal: bone %q: %w", rb.Name, err)
		}
		sd.Bones[i] = BoneData{
			Name:     rb.Name,
			Parent:   rb.Parent,
			X:        rb.X,
			Y:        rb.Y,
			Rotation: rb.Rotation,
			ScaleX:   defaultFloat32(rb.ScaleX, 1),
			ScaleY:   defaultFloat32(rb.ScaleY, 1),
			Length:   rb.Length,
		}
	}

	for i, rs := range raw.Slots {
		if rs.Name == "" {
			return nil, loaderr.Errorf(loaderr.Path("slots", i), `give the slot a "name"`, "slot %d has no name", i)
		}
		alpha := defaultFloat32(rs.Alpha, 1)
		if !(alpha >= 0 && alpha <= 1) {
			return nil, loaderr.Errorf(loaderr.Path("slots", i, "alpha"), "use an alpha from 0 (transparent) to 1 (opaque)", "slot %q alpha %v out of range [0, 1]", rs.Name, alpha)
		}
		sd.Slots[i] = SlotData{
			Name:       rs.Name,
			Bone:       rs.Bone,
			Attachment: rs.Attachment,
			Alpha:      alpha,
		}
	}

	if len(raw.Skins) > 0 {
		sd.Skins = make(map[string]Skin, len(raw.Skins))
		for skinName, rawSlots := range raw.Skins {
			skin := make(Skin, len(rawSlots))
			for slotName, ra := range rawSlots {
				if err := checkFinite(ra.X, ra.Y, ra.Rotation, defaultFloat32(ra.ScaleX, 1), defaultFloat32(ra.ScaleY, 1), ra.OriginX, ra.OriginY); err != nil {
					return nil, loaderr.Errorf(loaderr.Path("skins", skinName, slotName), fixFinite, "skeletal: skin %q slot %q: %w", skinName, slotName, err)
				}
				skin[slotName] = Attachment{
					Frame:    ra.Frame,
					X:        ra.X,
					Y:        ra.Y,
					Rotation: ra.Rotation,
					ScaleX:   defaultFloat32(ra.ScaleX, 1),
					ScaleY:   defaultFloat32(ra.ScaleY, 1),
					OriginX:  ra.OriginX,
					OriginY:  ra.OriginY,
				}
			}
			sd.Skins[skinName] = skin
		}
	}

	if len(raw.Animations) > 0 {
		sd.Animations = make(map[string]*Animation, len(raw.Animations))
		// Sorted, so the same animation is reported every time when several are wrong.
		for _, animName := range slices.Sorted(maps.Keys(raw.Animations)) {
			anim, err := decodeAnimation(animName, raw.Animations[animName])
			if err != nil {
				return nil, err
			}
			sd.Animations[animName] = anim
		}
	}

	if err := sd.compile(); err != nil {
		return nil, err
	}
	return sd, nil
}

// Compile validates cross-references in a hand-built SkeletonData and builds
// the dense lookup indices and topological bone evaluation order its runtime
// needs. Data loaded from a document is compiled already, and calling Compile
// again is harmless.
func (sd *SkeletonData) Compile() error { return sd.compile() }

// compile validates cross-references and builds dense lookup indices and a
// topological bone evaluation order.
func (sd *SkeletonData) compile() error {
	sd.boneIndex = make(map[string]int, len(sd.Bones))
	for i, b := range sd.Bones {
		if b.Name == "" {
			return loaderr.Errorf(loaderr.Path("bones", i), `give the bone a "name"`, "bone %d has no name", i)
		}
		if _, dup := sd.boneIndex[b.Name]; dup {
			return loaderr.Errorf(loaderr.Path("bones", i, "name"), "rename one of the bones; bone names are unique in a rig", "duplicate bone %q", b.Name)
		}
		sd.boneIndex[b.Name] = i
	}

	sd.boneParent = make([]int, len(sd.Bones))
	for i, b := range sd.Bones {
		if b.Parent == "" {
			sd.boneParent[i] = -1
			continue
		}
		pi, ok := sd.boneIndex[b.Parent]
		if !ok {
			return loaderr.Errorf(loaderr.Path("bones", i, "parent"), "name a bone defined in bones, or remove parent for a root bone", "bone %q has unknown parent %q", b.Name, b.Parent)
		}
		if pi == i {
			return loaderr.Errorf(loaderr.Path("bones", i, "parent"), "name another bone as the parent, or remove parent for a root bone", "bone cycle involving %q", b.Name)
		}
		sd.boneParent[i] = pi
	}

	// 3-color DFS detects cycles and appends parents before children.
	const (
		unvisited = 0
		visiting  = 1
		visited   = 2
	)
	state := make([]uint8, len(sd.Bones))
	sd.boneOrder = make([]int, 0, len(sd.Bones))
	var visit func(i int) error
	visit = func(i int) error {
		switch state[i] {
		case visited:
			return nil
		case visiting:
			return loaderr.Errorf(loaderr.Path("bones", i, "parent"), "break the cycle so every bone's parent chain ends at a root bone without a parent", "bone cycle involving %q", sd.Bones[i].Name)
		}
		state[i] = visiting
		if p := sd.boneParent[i]; p >= 0 {
			if err := visit(p); err != nil {
				return err
			}
		}
		state[i] = visited
		sd.boneOrder = append(sd.boneOrder, i)
		return nil
	}
	for i := range sd.Bones {
		if err := visit(i); err != nil {
			return err
		}
	}

	sd.slotIndex = make(map[string]int, len(sd.Slots))
	sd.slotBone = make([]int, len(sd.Slots))
	for i, s := range sd.Slots {
		if s.Name == "" {
			return loaderr.Errorf(loaderr.Path("slots", i), `give the slot a "name"`, "slot %d has no name", i)
		}
		if _, dup := sd.slotIndex[s.Name]; dup {
			return loaderr.Errorf(loaderr.Path("slots", i, "name"), "rename one of the slots; slot names are unique in a rig", "duplicate slot %q", s.Name)
		}
		bi, ok := sd.boneIndex[s.Bone]
		if !ok {
			return loaderr.Errorf(loaderr.Path("slots", i, "bone"), "name a bone defined in bones", "slot %q references unknown bone %q", s.Name, s.Bone)
		}
		sd.slotIndex[s.Name] = i
		sd.slotBone[i] = bi
	}

	// Maps are walked in sorted order, so the same problem is reported every time when several are there.
	for _, skinName := range slices.Sorted(maps.Keys(sd.Skins)) {
		for _, slotName := range slices.Sorted(maps.Keys(sd.Skins[skinName])) {
			if _, ok := sd.slotIndex[slotName]; !ok {
				return loaderr.Errorf(loaderr.Path("skins", skinName, slotName), "key the skin's attachments by slot names defined in slots", "skin %q references unknown slot %q", skinName, slotName)
			}
		}
	}

	for _, animName := range slices.Sorted(maps.Keys(sd.Animations)) {
		anim := sd.Animations[animName]
		if anim == nil {
			continue
		}
		anim.boneTimelines = anim.boneTimelines[:0]
		for _, boneName := range slices.Sorted(maps.Keys(anim.Bones)) {
			bi, ok := sd.boneIndex[boneName]
			if !ok {
				return loaderr.Errorf(loaderr.Path("animations", animName, "bones", boneName), "key the animation's bone timelines by bone names defined in bones", "animation %q references unknown bone %q", animName, boneName)
			}
			anim.boneTimelines = append(anim.boneTimelines, indexedBoneTimeline{boneIdx: bi, tl: anim.Bones[boneName]})
		}
		anim.slotTimelines = anim.slotTimelines[:0]
		for _, slotName := range slices.Sorted(maps.Keys(anim.Slots)) {
			si, ok := sd.slotIndex[slotName]
			if !ok {
				return loaderr.Errorf(loaderr.Path("animations", animName, "slots", slotName), "key the animation's slot timelines by slot names defined in slots", "animation %q references unknown slot %q", animName, slotName)
			}
			anim.slotTimelines = append(anim.slotTimelines, indexedSlotTimeline{slotIdx: si, tl: anim.Slots[slotName]})
		}
	}

	return nil
}

// decodeRaw strictly decodes one YAML document into the raw schema.
func decodeRaw(data []byte) (*rawSkeleton, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("skeletal: empty rig document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var raw rawSkeleton
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("skeletal: decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("skeletal: unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("skeletal: decode: %w", err)
	}
	return &raw, nil
}

// Raw wire structs for the YAML schema. Pointer fields distinguish omitted values from explicit 0. Field order is the order Encode writes keys in, and named maps are sortedMaps so Encode writes their keys in byte order.

type rawSkeleton struct {
	Version    int                             `yaml:"version"`
	Name       string                          `yaml:"name,omitempty"`
	Bones      []rawBone                       `yaml:"bones,omitempty"`
	Slots      []rawSlot                       `yaml:"slots,omitempty"`
	Skins      sortedMap[sortedMap[rawAttach]] `yaml:"skins,omitempty"`
	Animations sortedMap[rawAnimation]         `yaml:"animations,omitempty"`
}

type rawBone struct {
	Name     string   `yaml:"name"`
	Parent   string   `yaml:"parent,omitempty"`
	X        float32  `yaml:"x,omitempty"`
	Y        float32  `yaml:"y,omitempty"`
	Rotation float32  `yaml:"rotation,omitempty"`
	ScaleX   *float32 `yaml:"scaleX,omitempty"`
	ScaleY   *float32 `yaml:"scaleY,omitempty"`
	Length   float32  `yaml:"length,omitempty"`
}

type rawSlot struct {
	Name       string   `yaml:"name"`
	Bone       string   `yaml:"bone"`
	Attachment string   `yaml:"attachment,omitempty"`
	Alpha      *float32 `yaml:"alpha,omitempty"`
}

type rawAttach struct {
	Frame    string   `yaml:"frame"`
	X        float32  `yaml:"x,omitempty"`
	Y        float32  `yaml:"y,omitempty"`
	Rotation float32  `yaml:"rotation,omitempty"`
	ScaleX   *float32 `yaml:"scaleX,omitempty"`
	ScaleY   *float32 `yaml:"scaleY,omitempty"`
	OriginX  float32  `yaml:"originX,omitempty"`
	OriginY  float32  `yaml:"originY,omitempty"`
}

type rawAnimation struct {
	Duration *float64                   `yaml:"duration,omitempty"`
	Loop     bool                       `yaml:"loop,omitempty"`
	Bones    sortedMap[rawBoneTimeline] `yaml:"bones,omitempty"`
	Slots    sortedMap[rawSlotTimeline] `yaml:"slots,omitempty"`
}

type rawBoneTimeline struct {
	Translate []rawVec2Keyframe   `yaml:"translate,omitempty"`
	Rotate    []rawRotateKeyframe `yaml:"rotate,omitempty"`
	Scale     []rawScaleKeyframe  `yaml:"scale,omitempty"`
}

type rawSlotTimeline struct {
	Attachment []rawAttachKeyframe `yaml:"attachment,omitempty"`
	Alpha      []rawAlphaKeyframe  `yaml:"alpha,omitempty"`
}

type rawVec2Keyframe struct {
	Time   float64   `yaml:"time"`
	X      float32   `yaml:"x"`
	Y      float32   `yaml:"y"`
	Curve  string    `yaml:"curve,omitempty"`
	Bezier []float32 `yaml:"bezier,flow,omitempty"`
}

type rawScaleKeyframe struct {
	Time   float64   `yaml:"time"`
	X      *float32  `yaml:"x,omitempty"`
	Y      *float32  `yaml:"y,omitempty"`
	Curve  string    `yaml:"curve,omitempty"`
	Bezier []float32 `yaml:"bezier,flow,omitempty"`
}

type rawRotateKeyframe struct {
	Time   float64   `yaml:"time"`
	Angle  float32   `yaml:"angle"`
	Curve  string    `yaml:"curve,omitempty"`
	Bezier []float32 `yaml:"bezier,flow,omitempty"`
}

type rawAttachKeyframe struct {
	Time  float64 `yaml:"time"`
	Frame string  `yaml:"frame"`
}

type rawAlphaKeyframe struct {
	Time   float64   `yaml:"time"`
	Value  float32   `yaml:"value"`
	Curve  string    `yaml:"curve,omitempty"`
	Bezier []float32 `yaml:"bezier,flow,omitempty"`
}

func decodeAnimation(name string, ra rawAnimation) (*Animation, error) {
	anim := &Animation{
		Name: name,
		Loop: ra.Loop,
	}
	var maxTime time.Duration
	trackMax := func(t time.Duration) {
		if t > maxTime {
			maxTime = t
		}
	}

	if len(ra.Bones) > 0 {
		anim.Bones = make(map[string]BoneTimeline, len(ra.Bones))
		for _, boneName := range slices.Sorted(maps.Keys(ra.Bones)) {
			rbt := ra.Bones[boneName]
			at := loaderr.Path("animations", name, "bones", boneName)
			var bt BoneTimeline
			if len(rbt.Translate) > 0 {
				bt.Translate = make([]Vec2Keyframe, len(rbt.Translate))
				for i, rk := range rbt.Translate {
					t, err := decodeTime(rk.Time, i, bt.Translate, func(k Vec2Keyframe) time.Duration { return k.Time })
					if err != nil {
						return nil, loaderr.Errorf(loaderr.Join(at, "translate", i, "time"), fixKeyTime, "%w", err)
					}
					c, bz, err := decodeCurve(rk.Curve, rk.Bezier)
					if err != nil {
						return nil, loaderr.Errorf(loaderr.Join(at, "translate", i, "curve"), fixCurve, "%w", err)
					}
					if err := checkFinite(rk.X, rk.Y); err != nil {
						return nil, loaderr.Errorf(loaderr.Join(at, "translate", i), fixFinite, "skeletal: animation %q bone %q translate[%d]: %w", name, boneName, i, err)
					}
					bt.Translate[i] = Vec2Keyframe{Time: t, X: rk.X, Y: rk.Y, Curve: c, Bezier: bz}
					trackMax(t)
				}
			}
			if len(rbt.Rotate) > 0 {
				bt.Rotate = make([]FloatKeyframe, len(rbt.Rotate))
				for i, rk := range rbt.Rotate {
					t, err := decodeTime(rk.Time, i, bt.Rotate, func(k FloatKeyframe) time.Duration { return k.Time })
					if err != nil {
						return nil, loaderr.Errorf(loaderr.Join(at, "rotate", i, "time"), fixKeyTime, "%w", err)
					}
					c, bz, err := decodeCurve(rk.Curve, rk.Bezier)
					if err != nil {
						return nil, loaderr.Errorf(loaderr.Join(at, "rotate", i, "curve"), fixCurve, "%w", err)
					}
					if err := checkFinite(rk.Angle); err != nil {
						return nil, loaderr.Errorf(loaderr.Join(at, "rotate", i), fixFinite, "skeletal: animation %q bone %q rotate[%d]: %w", name, boneName, i, err)
					}
					bt.Rotate[i] = FloatKeyframe{Time: t, Value: rk.Angle, Curve: c, Bezier: bz}
					trackMax(t)
				}
			}
			if len(rbt.Scale) > 0 {
				bt.Scale = make([]Vec2Keyframe, len(rbt.Scale))
				for i, rk := range rbt.Scale {
					t, err := decodeTime(rk.Time, i, bt.Scale, func(k Vec2Keyframe) time.Duration { return k.Time })
					if err != nil {
						return nil, loaderr.Errorf(loaderr.Join(at, "scale", i, "time"), fixKeyTime, "%w", err)
					}
					c, bz, err := decodeCurve(rk.Curve, rk.Bezier)
					if err != nil {
						return nil, loaderr.Errorf(loaderr.Join(at, "scale", i, "curve"), fixCurve, "%w", err)
					}
					if err := checkFinite(defaultFloat32(rk.X, 1), defaultFloat32(rk.Y, 1)); err != nil {
						return nil, loaderr.Errorf(loaderr.Join(at, "scale", i), fixFinite, "skeletal: animation %q bone %q scale[%d]: %w", name, boneName, i, err)
					}
					bt.Scale[i] = Vec2Keyframe{
						Time:   t,
						X:      defaultFloat32(rk.X, 1),
						Y:      defaultFloat32(rk.Y, 1),
						Curve:  c,
						Bezier: bz,
					}
					trackMax(t)
				}
			}
			anim.Bones[boneName] = bt
		}
	}

	if len(ra.Slots) > 0 {
		anim.Slots = make(map[string]SlotTimeline, len(ra.Slots))
		for _, slotName := range slices.Sorted(maps.Keys(ra.Slots)) {
			rst := ra.Slots[slotName]
			at := loaderr.Path("animations", name, "slots", slotName)
			var st SlotTimeline
			if len(rst.Attachment) > 0 {
				st.Attachment = make([]AttachmentKeyframe, len(rst.Attachment))
				for i, rk := range rst.Attachment {
					t, err := decodeTime(rk.Time, i, st.Attachment, func(k AttachmentKeyframe) time.Duration { return k.Time })
					if err != nil {
						return nil, loaderr.Errorf(loaderr.Join(at, "attachment", i, "time"), fixKeyTime, "%w", err)
					}
					st.Attachment[i] = AttachmentKeyframe{Time: t, Frame: rk.Frame}
					trackMax(t)
				}
			}
			if len(rst.Alpha) > 0 {
				st.Alpha = make([]FloatKeyframe, len(rst.Alpha))
				for i, rk := range rst.Alpha {
					t, err := decodeTime(rk.Time, i, st.Alpha, func(k FloatKeyframe) time.Duration { return k.Time })
					if err != nil {
						return nil, loaderr.Errorf(loaderr.Join(at, "alpha", i, "time"), fixKeyTime, "%w", err)
					}
					if !(rk.Value >= 0 && rk.Value <= 1) {
						return nil, loaderr.Errorf(loaderr.Join(at, "alpha", i, "value"), "use an alpha from 0 (transparent) to 1 (opaque)", "value %v out of range [0, 1]", rk.Value)
					}
					c, bz, err := decodeCurve(rk.Curve, rk.Bezier)
					if err != nil {
						return nil, loaderr.Errorf(loaderr.Join(at, "alpha", i, "curve"), fixCurve, "%w", err)
					}
					st.Alpha[i] = FloatKeyframe{Time: t, Value: rk.Value, Curve: c, Bezier: bz}
					trackMax(t)
				}
			}
			anim.Slots[slotName] = st
		}
	}

	if ra.Duration != nil {
		if *ra.Duration < 0 {
			return nil, loaderr.Errorf(loaderr.Path("animations", name, "duration"), "use a duration of 0 or more seconds, or remove it to end at the last keyframe", "animation %q has negative duration %v", name, *ra.Duration)
		}
		if !(*ra.Duration <= MaxSeconds) {
			return nil, loaderr.Errorf(loaderr.Path("animations", name, "duration"), fmt.Sprintf("use a duration of at most %d seconds", MaxSeconds), "skeletal: animation %q duration %v is not a number of seconds in [0, %d]", name, *ra.Duration, MaxSeconds)
		}
		anim.Duration = secondsToDuration(*ra.Duration)
	} else {
		anim.Duration = maxTime
	}
	return anim, nil
}

func decodeTime[T any](sec float64, idx int, prev []T, getTime func(T) time.Duration) (time.Duration, error) {
	if !(sec >= 0 && sec <= MaxSeconds) {
		return 0, fmt.Errorf("keyframe %d has invalid time %v", idx, sec)
	}
	t := secondsToDuration(sec)
	if idx > 0 && t < getTime(prev[idx-1]) {
		return 0, fmt.Errorf("keyframe %d time %v precedes previous keyframe %v", idx, t, getTime(prev[idx-1]))
	}
	return t, nil
}

func decodeCurve(name string, bezier []float32) (CurveType, [4]float32, error) {
	if name != "bezier" && len(bezier) > 0 {
		return 0, [4]float32{}, fmt.Errorf("bezier control points given for a %q curve", name)
	}
	switch name {
	case "", "linear":
		return CurveLinear, [4]float32{}, nil
	case "stepped":
		return CurveStepped, [4]float32{}, nil
	case "bezier":
		if len(bezier) != 4 {
			return 0, [4]float32{}, fmt.Errorf("bezier curve needs 4 numbers, got %d", len(bezier))
		}
		if err := checkFinite(bezier...); err != nil {
			return 0, [4]float32{}, fmt.Errorf("bezier: %w", err)
		}
		if bezier[0] < 0 || bezier[0] > 1 || bezier[2] < 0 || bezier[2] > 1 {
			return 0, [4]float32{}, fmt.Errorf("bezier x control points (%v, %v) must lie in [0, 1]", bezier[0], bezier[2])
		}
		return CurveBezier, [4]float32{bezier[0], bezier[1], bezier[2], bezier[3]}, nil
	default:
		return 0, [4]float32{}, fmt.Errorf("unknown curve %q", name)
	}
}

func secondsToDuration(sec float64) time.Duration {
	return time.Duration(math.Round(sec * float64(time.Second)))
}

// checkFinite reports the first NaN or infinite value, which YAML can spell as .nan and .inf.
func checkFinite(vs ...float32) error {
	for _, v := range vs {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return fmt.Errorf("number %v is not finite", v)
		}
	}
	return nil
}

func defaultFloat32(p *float32, fallback float32) float32 {
	if p == nil {
		return fallback
	}
	return *p
}
