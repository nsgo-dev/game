package skeletal

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/game/internal/yamlzero"
	"go.yaml.in/yaml/v3"
)

// Encode serializes sd as a version-1 .skel.yaml document and checks that the result loads.
//
// The output is deterministic: keys follow the schema's order, skins, attachments, animations and timelines are sorted by name in byte order, numbers are written in plain decimal notation without an exponent, every bone, slot, attachment and keyframe sits on one line in flow style, and values equal to their defaults are left out, except that keyframes always spell out their time and values and animations their duration. Encoding the result of Load gives the same bytes back for a file already in this form, and loading the result of Encode gives data equal to sd.
//
// Encode reports an error, and writes nothing, when sd would not load: a missing or dangling name, a cycle, an unknown curve, a non-finite number, a time outside [0, MaxSeconds], Bezier control points on a curve that is not CurveBezier, an Animation whose Name differs from its key in Animations, or any other rule Load enforces. It does not modify sd.
func Encode(sd *SkeletonData) ([]byte, error) {
	if sd == nil {
		return nil, errors.New("skeletal: encode nil SkeletonData")
	}
	raw, err := encodeRaw(sd)
	if err != nil {
		return nil, err
	}
	raw = yamlzero.Clean(raw)
	var doc yaml.Node
	if err := doc.Encode(raw); err != nil {
		return nil, fmt.Errorf("skeletal: encode: %w", err)
	}
	plainDecimals(&doc)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
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
		raw.Skins = make(sortedMap[sortedMap[rawAttach]], len(sd.Skins))
		for skinName, skin := range sd.Skins {
			slots := make(sortedMap[rawAttach], len(skin))
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
			// The file keys an animation only by its map key, which Load copies into Name, so a different Name would not survive a round trip.
			if anim.Name != "" && anim.Name != animName {
				return nil, fmt.Errorf("skeletal: animation %q has Name %q, which differs from its key", animName, anim.Name)
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

// encodeCurve names c for the file and returns its control points only for a Bezier curve, the one curve that uses them. Like Load, it refuses control points on any other curve rather than dropping them.
func encodeCurve(c CurveType, bezier [4]float32) (string, []float32, error) {
	switch c {
	case CurveLinear:
		if bezier != [4]float32{} {
			return "", nil, errors.New(`bezier control points given for a "linear" curve`)
		}
		return "", nil, nil
	case CurveStepped:
		if bezier != [4]float32{} {
			return "", nil, errors.New(`bezier control points given for a "stepped" curve`)
		}
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

// sortedMap is a string-keyed map of the schema that Encode writes with its keys in plain byte order. yaml.v3 would otherwise sort them in its own natural order, which compares runs of digits by value and puts letters before punctuation, so a file sorted by an ordinary string sort would not be canonical.
type sortedMap[V any] map[string]V

// MarshalYAML writes m as a block mapping with its keys sorted by sort.Strings.
func (m sortedMap[V]) MarshalYAML() (any, error) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	n := &yaml.Node{Kind: yaml.MappingNode, Content: make([]*yaml.Node, 0, 2*len(keys))}
	for _, k := range keys {
		var kn, vn yaml.Node
		// Encoding the key, rather than building a plain scalar, keeps yaml.v3's quoting of names that would otherwise read as another type, such as "true" or "1".
		if err := kn.Encode(k); err != nil {
			return nil, err
		}
		if err := vn.Encode(m[k]); err != nil {
			return nil, err
		}
		n.Content = append(n.Content, &kn, &vn)
	}
	return n, nil
}

// plainDecimals rewrites every plain float scalar under n that yaml.v3 wrote with an exponent (1e+06, 1e-07) in plain decimal notation (1000000, 0.0000001). The digits are moved, never recomputed, so the text still reads back to the same float32 or float64, and YAML 1.1 readers such as PyYAML, which need a '.' in a float, still read a number.
func plainDecimals(n *yaml.Node) {
	if n.Kind == yaml.ScalarNode && n.Style == 0 && n.ShortTag() == "!!float" {
		n.Value = expandExponent(n.Value)
		// Clear the !!float tag the parse left, so a whole number such as 1000000 is written plain rather than as "!!float 1000000"; it reads back as a number either way.
		n.Tag = ""
	}
	for _, c := range n.Content {
		plainDecimals(c)
	}
}

// expandExponent rewrites a number in Go's 'g' notation, such as -1.5e-07, in 'f' notation with the same digits, such as -0.00000015. Text without an exponent, including .inf and .nan, is returned unchanged.
func expandExponent(s string) string {
	i := strings.IndexAny(s, "eE")
	if i < 0 {
		return s
	}
	exp, err := strconv.Atoi(s[i+1:])
	if err != nil {
		return s
	}
	mant, sign := s[:i], ""
	if mant != "" && (mant[0] == '-' || mant[0] == '+') {
		if mant[0] == '-' {
			sign = "-"
		}
		mant = mant[1:]
	}
	intPart, frac, _ := strings.Cut(mant, ".")
	digits := strings.TrimLeft(intPart+frac, "0")
	if digits == "" {
		return sign + "0"
	}
	// point is where the decimal point falls within digits once the exponent is applied.
	point := len(intPart) - (len(intPart+frac) - len(digits)) + exp
	digits = strings.TrimRight(digits, "0")
	var out string
	switch {
	case point <= 0:
		out = "0." + strings.Repeat("0", -point) + digits
	case point >= len(digits):
		out = digits + strings.Repeat("0", point-len(digits))
	default:
		out = digits[:point] + "." + digits[point:]
	}
	return sign + out
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
