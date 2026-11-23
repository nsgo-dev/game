package skeletal

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"go.yaml.in/yaml/v3"
)

// Encode serializes sd as a version-1 .skel.yaml document and checks that the result loads.
//
// The output is deterministic: keys follow the schema's order, skins, animations and timelines are sorted by name, every bone, slot, attachment and keyframe sits on one line in flow style, and values equal to their defaults are left out, except that keyframes always spell out their time and values and animations their duration. Encoding the result of Load gives the same bytes back for a file already in this form, and loading the result of Encode gives data equal to sd.
//
// Encode reports an error, and writes nothing, when sd would not load: a missing or dangling name, a cycle, an unknown curve, a non-finite number, a time outside [0, MaxSeconds] or any other rule Load enforces. It does not modify sd.
func Encode(sd *SkeletonData) ([]byte, error) {
	if sd == nil {
		return nil, errors.New("skeletal: encode nil SkeletonData")
	}
	raw, err := encodeRaw(sd)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(raw); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("skeletal: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("skeletal: encode: %w", err)
	}
	out := buf.Bytes()
	// Loading the output applies every validation rule in one place and guarantees Encode never writes a file Load refuses.
	if _, err := Load(out); err != nil {
		return nil, err
	}
	return out, nil
}

func encodeRaw(sd *SkeletonData) (*rawSkeleton, error) {
	if sd.Version != FormatVersion {
		return nil, fmt.Errorf("skeletal: unsupported version %d (want %d)", sd.Version, FormatVersion)
	}
	raw := &rawSkeleton{Version: sd.Version, Name: sd.Name}
	if len(sd.Bones) > 0 {
		raw.Bones = make([]rawBone, len(sd.Bones))
		for i, b := range sd.Bones {
			raw.Bones[i] = rawBone{
				Name:     b.Name,
				Parent:   b.Parent,
				X:        b.X,
				Y:        b.Y,
				Rotation: b.Rotation,
				ScaleX:   omitDefault(b.ScaleX, 1),
				ScaleY:   omitDefault(b.ScaleY, 1),
				Length:   b.Length,
			}
		}
	}
	if len(sd.Slots) > 0 {
		raw.Slots = make([]rawSlot, len(sd.Slots))
		for i, s := range sd.Slots {
			raw.Slots[i] = rawSlot{
				Name:       s.Name,
				Bone:       s.Bone,
				Attachment: s.Attachment,
				Alpha:      omitDefault(s.Alpha, 1),
			}
		}
	}
	if len(sd.Skins) > 0 {
		raw.Skins = make(map[string]map[string]rawAttach, len(sd.Skins))
		for skinName, skin := range sd.Skins {
			slots := make(map[string]rawAttach, len(skin))
			for slotName, a := range skin {
				slots[slotName] = rawAttach{
					Frame:    a.Frame,
					X:        a.X,
					Y:        a.Y,
					Rotation: a.Rotation,
					ScaleX:   omitDefault(a.ScaleX, 1),
					ScaleY:   omitDefault(a.ScaleY, 1),
					OriginX:  a.OriginX,
					OriginY:  a.OriginY,
				}
			}
			raw.Skins[skinName] = slots
		}
	}
	if len(sd.Animations) > 0 {
		raw.Animations = make(map[string]rawAnimation, len(sd.Animations))
		for animName, anim := range sd.Animations {
			if anim == nil {
				return nil, fmt.Errorf("skeletal: animation %q is nil", animName)
			}
			ra, err := encodeAnimation(animName, anim)
			if err != nil {
				return nil, err
			}
			raw.Animations[animName] = ra
		}
	}
	return raw, nil
}

func encodeAnimation(name string, anim *Animation) (rawAnimation, error) {
	dur := durationToSeconds(anim.Duration)
	ra := rawAnimation{Duration: &dur, Loop: anim.Loop}
	if len(anim.Bones) > 0 {
		ra.Bones = make(map[string]rawBoneTimeline, len(anim.Bones))
		for boneName, bt := range anim.Bones {
			var rbt rawBoneTimeline
			if len(bt.Translate) > 0 {
				rbt.Translate = make([]rawVec2Keyframe, len(bt.Translate))
				for i, k := range bt.Translate {
					curve, bezier, err := encodeCurve(k.Curve, k.Bezier)
					if err != nil {
						return rawAnimation{}, fmt.Errorf("skeletal: animation %q bone %q translate[%d]: %w", name, boneName, i, err)
					}
					rbt.Translate[i] = rawVec2Keyframe{Time: durationToSeconds(k.Time), X: k.X, Y: k.Y, Curve: curve, Bezier: bezier}
				}
			}
			if len(bt.Rotate) > 0 {
				rbt.Rotate = make([]rawRotateKeyframe, len(bt.Rotate))
				for i, k := range bt.Rotate {
					curve, bezier, err := encodeCurve(k.Curve, k.Bezier)
					if err != nil {
						return rawAnimation{}, fmt.Errorf("skeletal: animation %q bone %q rotate[%d]: %w", name, boneName, i, err)
					}
					rbt.Rotate[i] = rawRotateKeyframe{Time: durationToSeconds(k.Time), Angle: k.Value, Curve: curve, Bezier: bezier}
				}
			}
			if len(bt.Scale) > 0 {
				rbt.Scale = make([]rawScaleKeyframe, len(bt.Scale))
				for i, k := range bt.Scale {
					curve, bezier, err := encodeCurve(k.Curve, k.Bezier)
					if err != nil {
						return rawAnimation{}, fmt.Errorf("skeletal: animation %q bone %q scale[%d]: %w", name, boneName, i, err)
					}
					x, y := k.X, k.Y
					rbt.Scale[i] = rawScaleKeyframe{Time: durationToSeconds(k.Time), X: &x, Y: &y, Curve: curve, Bezier: bezier}
				}
			}
			ra.Bones[boneName] = rbt
		}
	}
	if len(anim.Slots) > 0 {
		ra.Slots = make(map[string]rawSlotTimeline, len(anim.Slots))
		for slotName, st := range anim.Slots {
			var rst rawSlotTimeline
			if len(st.Attachment) > 0 {
				rst.Attachment = make([]rawAttachKeyframe, len(st.Attachment))
				for i, k := range st.Attachment {
					rst.Attachment[i] = rawAttachKeyframe{Time: durationToSeconds(k.Time), Frame: k.Frame}
				}
			}
			if len(st.Alpha) > 0 {
				rst.Alpha = make([]rawAlphaKeyframe, len(st.Alpha))
				for i, k := range st.Alpha {
					curve, bezier, err := encodeCurve(k.Curve, k.Bezier)
					if err != nil {
						return rawAnimation{}, fmt.Errorf("skeletal: animation %q slot %q alpha[%d]: %w", name, slotName, i, err)
					}
					rst.Alpha[i] = rawAlphaKeyframe{Time: durationToSeconds(k.Time), Value: k.Value, Curve: curve, Bezier: bezier}
				}
			}
			ra.Slots[slotName] = rst
		}
	}
	return ra, nil
}

// encodeCurve names c for the file and returns its control points only for a Bezier curve, the one curve that uses them.
func encodeCurve(c CurveType, bezier [4]float32) (string, []float32, error) {
	switch c {
	case CurveLinear:
		return "", nil, nil
	case CurveStepped:
		return "stepped", nil, nil
	case CurveBezier:
		return "bezier", bezier[:], nil
	default:
		return "", nil, fmt.Errorf("unknown curve type %d", c)
	}
}

// durationToSeconds converts d to seconds with a single rounding, so that secondsToDuration gives d back for any d within MaxSeconds.
func durationToSeconds(d time.Duration) float64 {
	return float64(d) / float64(time.Second)
}

// omitDefault returns nil when v equals the schema default def, so the field is left out of the file.
func omitDefault(v, def float32) *float32 {
	if v == def {
		return nil
	}
	return &v
}

// flowNode encodes v as a single-line flow mapping, used for the schema's records (bones, slots, attachments and keyframes) so each one is one line in a diff.
func flowNode(v any) (*yaml.Node, error) {
	var n yaml.Node
	if err := n.Encode(v); err != nil {
		return nil, err
	}
	n.Style = yaml.FlowStyle
	// Keys are the schema's fixed field names. yaml.v3 quotes "y" because YAML 1.1 read it as a boolean; YAML 1.2 and this decoder read it as a string, so write it plain like the other keys.
	for i := 0; i < len(n.Content); i += 2 {
		n.Content[i].Style = 0
	}
	return &n, nil
}

// MarshalYAML writes a bone as a one-line flow mapping.
func (b rawBone) MarshalYAML() (any, error) {
	type plain rawBone
	return flowNode(plain(b))
}

// MarshalYAML writes a slot as a one-line flow mapping.
func (s rawSlot) MarshalYAML() (any, error) {
	type plain rawSlot
	return flowNode(plain(s))
}

// MarshalYAML writes a skin attachment as a one-line flow mapping.
func (a rawAttach) MarshalYAML() (any, error) {
	type plain rawAttach
	return flowNode(plain(a))
}

// MarshalYAML writes a translate keyframe as a one-line flow mapping.
func (k rawVec2Keyframe) MarshalYAML() (any, error) {
	type plain rawVec2Keyframe
	return flowNode(plain(k))
}

// MarshalYAML writes a scale keyframe as a one-line flow mapping.
func (k rawScaleKeyframe) MarshalYAML() (any, error) {
	type plain rawScaleKeyframe
	return flowNode(plain(k))
}

// MarshalYAML writes a rotate keyframe as a one-line flow mapping.
func (k rawRotateKeyframe) MarshalYAML() (any, error) {
	type plain rawRotateKeyframe
	return flowNode(plain(k))
}

// MarshalYAML writes an attachment keyframe as a one-line flow mapping.
func (k rawAttachKeyframe) MarshalYAML() (any, error) {
	type plain rawAttachKeyframe
	return flowNode(plain(k))
}

// MarshalYAML writes an alpha keyframe as a one-line flow mapping.
func (k rawAlphaKeyframe) MarshalYAML() (any, error) {
	type plain rawAlphaKeyframe
	return flowNode(plain(k))
}
