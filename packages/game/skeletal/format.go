package skeletal

import (
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// FormatVersion is the schema version that Load reads.
const FormatVersion = 1

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
	// ScaleX and ScaleY are the bone's bind-pose scales, 1 when omitted in JSON.
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
	// Alpha is the slot's bind-pose opacity in [0, 1], 1 when omitted in JSON.
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
	// ScaleX and ScaleY are the attachment's local scales, 1 when omitted in JSON.
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

// Load parses and validates a version-1 skeletal rig from JSON.
//
// It rejects unknown schema versions, duplicate bone or slot names, missing
// parents, cycles in the bone hierarchy, slots or skins referencing missing
// bones or slots, and animations with unknown targets, unsorted keyframes or
// invalid curves.
func Load(data []byte) (*SkeletonData, error) {
	var raw rawSkeleton
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("skeletal: %w", err)
	}
	if raw.Version != FormatVersion {
		return nil, fmt.Errorf("skeletal: unsupported version %d (want %d)", raw.Version, FormatVersion)
	}

	sd := &SkeletonData{
		Version: raw.Version,
		Name:    raw.Name,
		Bones:   make([]BoneData, len(raw.Bones)),
		Slots:   make([]SlotData, len(raw.Slots)),
	}

	for i, rb := range raw.Bones {
		if rb.Name == "" {
			return nil, fmt.Errorf("skeletal: bone %d has no name", i)
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
			return nil, fmt.Errorf("skeletal: slot %d has no name", i)
		}
		alpha := defaultFloat32(rs.Alpha, 1)
		if alpha < 0 || alpha > 1 {
			return nil, fmt.Errorf("skeletal: slot %q alpha %v out of range [0, 1]", rs.Name, alpha)
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
		for animName, ra := range raw.Animations {
			anim, err := decodeAnimation(animName, ra)
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
			return fmt.Errorf("skeletal: bone %d has no name", i)
		}
		if _, dup := sd.boneIndex[b.Name]; dup {
			return fmt.Errorf("skeletal: duplicate bone %q", b.Name)
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
			return fmt.Errorf("skeletal: bone %q has unknown parent %q", b.Name, b.Parent)
		}
		if pi == i {
			return fmt.Errorf("skeletal: bone cycle involving %q", b.Name)
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
			return fmt.Errorf("skeletal: bone cycle involving %q", sd.Bones[i].Name)
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
			return fmt.Errorf("skeletal: slot %d has no name", i)
		}
		if _, dup := sd.slotIndex[s.Name]; dup {
			return fmt.Errorf("skeletal: duplicate slot %q", s.Name)
		}
		bi, ok := sd.boneIndex[s.Bone]
		if !ok {
			return fmt.Errorf("skeletal: slot %q references unknown bone %q", s.Name, s.Bone)
		}
		sd.slotIndex[s.Name] = i
		sd.slotBone[i] = bi
	}

	for skinName, skin := range sd.Skins {
		for slotName := range skin {
			if _, ok := sd.slotIndex[slotName]; !ok {
				return fmt.Errorf("skeletal: skin %q references unknown slot %q", skinName, slotName)
			}
		}
	}

	for animName, anim := range sd.Animations {
		if anim == nil {
			continue
		}
		anim.boneTimelines = anim.boneTimelines[:0]
		for boneName, tl := range anim.Bones {
			bi, ok := sd.boneIndex[boneName]
			if !ok {
				return fmt.Errorf("skeletal: animation %q references unknown bone %q", animName, boneName)
			}
			anim.boneTimelines = append(anim.boneTimelines, indexedBoneTimeline{boneIdx: bi, tl: tl})
		}
		anim.slotTimelines = anim.slotTimelines[:0]
		for slotName, tl := range anim.Slots {
			si, ok := sd.slotIndex[slotName]
			if !ok {
				return fmt.Errorf("skeletal: animation %q references unknown slot %q", animName, slotName)
			}
			anim.slotTimelines = append(anim.slotTimelines, indexedSlotTimeline{slotIdx: si, tl: tl})
		}
	}

	return nil
}

// Raw JSON wire structs. Pointer fields distinguish omitted values from explicit 0.

type rawSkeleton struct {
	Version    int                             `json:"version"`
	Name       string                          `json:"name"`
	Bones      []rawBone                       `json:"bones"`
	Slots      []rawSlot                       `json:"slots"`
	Skins      map[string]map[string]rawAttach `json:"skins"`
	Animations map[string]rawAnimation         `json:"animations"`
}

type rawBone struct {
	Name     string   `json:"name"`
	Parent   string   `json:"parent"`
	X        float32  `json:"x"`
	Y        float32  `json:"y"`
	Rotation float32  `json:"rotation"`
	ScaleX   *float32 `json:"scaleX"`
	ScaleY   *float32 `json:"scaleY"`
	Length   float32  `json:"length"`
}

type rawSlot struct {
	Name       string   `json:"name"`
	Bone       string   `json:"bone"`
	Attachment string   `json:"attachment"`
	Alpha      *float32 `json:"alpha"`
}

type rawAttach struct {
	Frame    string   `json:"frame"`
	X        float32  `json:"x"`
	Y        float32  `json:"y"`
	Rotation float32  `json:"rotation"`
	ScaleX   *float32 `json:"scaleX"`
	ScaleY   *float32 `json:"scaleY"`
	OriginX  float32  `json:"originX"`
	OriginY  float32  `json:"originY"`
}

type rawAnimation struct {
	Duration *float64                   `json:"duration"`
	Loop     bool                       `json:"loop"`
	Bones    map[string]rawBoneTimeline `json:"bones"`
	Slots    map[string]rawSlotTimeline `json:"slots"`
}

type rawBoneTimeline struct {
	Translate []rawVec2Keyframe   `json:"translate"`
	Rotate    []rawRotateKeyframe `json:"rotate"`
	Scale     []rawScaleKeyframe  `json:"scale"`
}

type rawSlotTimeline struct {
	Attachment []rawAttachKeyframe `json:"attachment"`
	Alpha      []rawAlphaKeyframe  `json:"alpha"`
}

type rawVec2Keyframe struct {
	Time   float64   `json:"time"`
	X      float32   `json:"x"`
	Y      float32   `json:"y"`
	Curve  string    `json:"curve"`
	Bezier []float32 `json:"bezier"`
}

type rawScaleKeyframe struct {
	Time   float64   `json:"time"`
	X      *float32  `json:"x"`
	Y      *float32  `json:"y"`
	Curve  string    `json:"curve"`
	Bezier []float32 `json:"bezier"`
}

type rawRotateKeyframe struct {
	Time   float64   `json:"time"`
	Angle  float32   `json:"angle"`
	Curve  string    `json:"curve"`
	Bezier []float32 `json:"bezier"`
}

type rawAttachKeyframe struct {
	Time  float64 `json:"time"`
	Frame string  `json:"frame"`
}

type rawAlphaKeyframe struct {
	Time   float64   `json:"time"`
	Value  float32   `json:"value"`
	Curve  string    `json:"curve"`
	Bezier []float32 `json:"bezier"`
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
		for boneName, rbt := range ra.Bones {
			var bt BoneTimeline
			if len(rbt.Translate) > 0 {
				bt.Translate = make([]Vec2Keyframe, len(rbt.Translate))
				for i, rk := range rbt.Translate {
					t, err := decodeTime(rk.Time, i, bt.Translate, func(k Vec2Keyframe) time.Duration { return k.Time })
					if err != nil {
						return nil, fmt.Errorf("skeletal: animation %q bone %q translate: %w", name, boneName, err)
					}
					c, bz, err := decodeCurve(rk.Curve, rk.Bezier)
					if err != nil {
						return nil, fmt.Errorf("skeletal: animation %q bone %q translate[%d]: %w", name, boneName, i, err)
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
						return nil, fmt.Errorf("skeletal: animation %q bone %q rotate: %w", name, boneName, err)
					}
					c, bz, err := decodeCurve(rk.Curve, rk.Bezier)
					if err != nil {
						return nil, fmt.Errorf("skeletal: animation %q bone %q rotate[%d]: %w", name, boneName, i, err)
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
						return nil, fmt.Errorf("skeletal: animation %q bone %q scale: %w", name, boneName, err)
					}
					c, bz, err := decodeCurve(rk.Curve, rk.Bezier)
					if err != nil {
						return nil, fmt.Errorf("skeletal: animation %q bone %q scale[%d]: %w", name, boneName, i, err)
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
		for slotName, rst := range ra.Slots {
			var st SlotTimeline
			if len(rst.Attachment) > 0 {
				st.Attachment = make([]AttachmentKeyframe, len(rst.Attachment))
				for i, rk := range rst.Attachment {
					t, err := decodeTime(rk.Time, i, st.Attachment, func(k AttachmentKeyframe) time.Duration { return k.Time })
					if err != nil {
						return nil, fmt.Errorf("skeletal: animation %q slot %q attachment: %w", name, slotName, err)
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
						return nil, fmt.Errorf("skeletal: animation %q slot %q alpha: %w", name, slotName, err)
					}
					if rk.Value < 0 || rk.Value > 1 {
						return nil, fmt.Errorf("skeletal: animation %q slot %q alpha[%d]: value %v out of range [0, 1]", name, slotName, i, rk.Value)
					}
					c, bz, err := decodeCurve(rk.Curve, rk.Bezier)
					if err != nil {
						return nil, fmt.Errorf("skeletal: animation %q slot %q alpha[%d]: %w", name, slotName, i, err)
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
			return nil, fmt.Errorf("skeletal: animation %q has negative duration %v", name, *ra.Duration)
		}
		anim.Duration = secondsToDuration(*ra.Duration)
	} else {
		anim.Duration = maxTime
	}
	return anim, nil
}

func decodeTime[T any](sec float64, idx int, prev []T, getTime func(T) time.Duration) (time.Duration, error) {
	if sec < 0 || math.IsNaN(sec) || math.IsInf(sec, 0) {
		return 0, fmt.Errorf("keyframe %d has invalid time %v", idx, sec)
	}
	t := secondsToDuration(sec)
	if idx > 0 && t < getTime(prev[idx-1]) {
		return 0, fmt.Errorf("keyframe %d time %v precedes previous keyframe %v", idx, t, getTime(prev[idx-1]))
	}
	return t, nil
}

func decodeCurve(name string, bezier []float32) (CurveType, [4]float32, error) {
	switch name {
	case "", "linear":
		return CurveLinear, [4]float32{}, nil
	case "stepped":
		return CurveStepped, [4]float32{}, nil
	case "bezier":
		if len(bezier) != 4 {
			return 0, [4]float32{}, fmt.Errorf("bezier curve needs 4 numbers, got %d", len(bezier))
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

func defaultFloat32(p *float32, fallback float32) float32 {
	if p == nil {
		return fallback
	}
	return *p
}
