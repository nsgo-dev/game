package skeletal_test

import (
	"bytes"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/skeletal"
)

// exported copies the exported fields of sd, leaving out the runtime indices whose order depends on map iteration, so two rigs can be compared with reflect.DeepEqual.
func exported(sd *skeletal.SkeletonData) skeletal.SkeletonData {
	out := skeletal.SkeletonData{
		Version: sd.Version,
		Name:    sd.Name,
		Bones:   sd.Bones,
		Slots:   sd.Slots,
		Skins:   sd.Skins,
	}
	if sd.Animations != nil {
		out.Animations = make(map[string]*skeletal.Animation, len(sd.Animations))
		for name, a := range sd.Animations {
			out.Animations[name] = &skeletal.Animation{Name: a.Name, Duration: a.Duration, Loop: a.Loop, Bones: a.Bones, Slots: a.Slots}
		}
	}
	return out
}

func sameRig(t *testing.T, got, want *skeletal.SkeletonData) {
	t.Helper()
	if g, w := exported(got), exported(want); !reflect.DeepEqual(g, w) {
		t.Fatalf("rig differs:\n got %+v\nwant %+v", g, w)
	}
}

func TestSampleRigRoundTripsUnchanged(t *testing.T) {
	data := loadRunner(t)
	out, err := skeletal.Encode(data)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(out, runnerYAML) {
		t.Fatalf("Encode(Load(runner.skel.yaml)) differs from the file:\n%s", out)
	}
	again, err := skeletal.Load(out)
	if err != nil {
		t.Fatalf("Load(Encode): %v", err)
	}
	sameRig(t, again, data)
}

func TestEncodeIsDeterministic(t *testing.T) {
	data := loadRunner(t)
	first, err := skeletal.Encode(data)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 20 {
		out, err := skeletal.Encode(data)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(out, first) {
			t.Fatalf("encode %d differs from the first:\n%s\nfirst:\n%s", i, out, first)
		}
	}
}

func TestLoadAcceptsJSONSyntax(t *testing.T) {
	doc := `{
  "version": 1,
  "name": "legacy",
  "bones": [ { "name": "root" }, { "name": "arm", "parent": "root", "x": 4, "scaleX": 2 } ],
  "slots": [ { "name": "arm", "bone": "arm", "alpha": 0.5 } ],
  "skins": { "default": { "arm": { "frame": "arm", "originX": 1 } } },
  "animations": { "wave": { "loop": true, "bones": { "arm": { "rotate": [ { "time": 0, "angle": 0 }, { "time": 0.25, "angle": 1, "curve": "stepped" } ] } } } }
}`
	data, err := skeletal.Load([]byte(doc))
	if err != nil {
		t.Fatalf("Load(JSON): %v", err)
	}
	if data.Name != "legacy" || data.Bones[1].ScaleX != 2 || data.Bones[1].ScaleY != 1 || data.Slots[0].Alpha != 0.5 {
		t.Fatalf("JSON rig loaded as %+v", data)
	}
	if got := data.Animations["wave"].Duration; got != 250*time.Millisecond {
		t.Fatalf("derived duration %v, want 250ms", got)
	}
	out, err := skeletal.Encode(data)
	if err != nil {
		t.Fatal(err)
	}
	want := `version: 1
name: legacy
bones:
  - {name: root}
  - {name: arm, parent: root, x: 4, scaleX: 2}
slots:
  - {name: arm, bone: arm, alpha: 0.5}
skins:
  default:
    arm: {frame: arm, originX: 1}
animations:
  wave:
    duration: 0.25
    loop: true
    bones:
      arm:
        rotate:
          - {time: 0, angle: 0}
          - {time: 0.25, angle: 1, curve: stepped}
`
	if string(out) != want {
		t.Fatalf("Encode:\n%s\nwant:\n%s", out, want)
	}

	// JSON editors often indent with tabs, which YAML allows inside flow collections.
	tabbed := "{\n\t\"version\": 1,\n\t\"bones\": [\n\t\t{\"name\": \"root\"}\n\t]\n}\n"
	if _, err := skeletal.Load([]byte(tabbed)); err != nil {
		t.Fatalf("Load(tab-indented JSON): %v", err)
	}
}

func TestEncodeHandBuiltRig(t *testing.T) {
	sd := &skeletal.SkeletonData{
		Version: skeletal.FormatVersion,
		Bones: []skeletal.BoneData{
			{Name: "root", ScaleX: 1, ScaleY: 1},
			{Name: "tail", Parent: "root", X: -0.1, Rotation: 0.3, ScaleX: 0, ScaleY: 1.5, Length: 7},
		},
		Slots: []skeletal.SlotData{
			{Name: "tail", Bone: "tail", Attachment: "tail_idle", Alpha: 0},
			{Name: "glow", Bone: "root", Alpha: 1},
		},
		Skins: map[string]skeletal.Skin{
			"default": {"tail": {Frame: "tail", ScaleX: 1, ScaleY: -1, OriginX: 2.5}, "glow": {Frame: "", ScaleX: 1, ScaleY: 1}},
			"empty":   {},
		},
		Animations: map[string]*skeletal.Animation{
			"swish": {
				Name:     "swish",
				Duration: 3*time.Second + time.Nanosecond,
				Bones: map[string]skeletal.BoneTimeline{
					"tail": {
						Translate: []skeletal.Vec2Keyframe{{Time: 0, X: 1, Y: 2, Curve: skeletal.CurveStepped}},
						Rotate: []skeletal.FloatKeyframe{
							{Time: 0, Value: -1, Curve: skeletal.CurveBezier, Bezier: [4]float32{0, -0.5, 1, 1.5}},
							{Time: 333333333, Value: 1},
						},
						Scale: []skeletal.Vec2Keyframe{{Time: time.Second, X: 0, Y: 1}},
					},
					"root": {},
				},
				Slots: map[string]skeletal.SlotTimeline{
					"glow": {
						Attachment: []skeletal.AttachmentKeyframe{{Time: 0, Frame: ""}, {Time: time.Second, Frame: "on"}},
						Alpha:      []skeletal.FloatKeyframe{{Time: 0, Value: 0}, {Time: 3*time.Second + time.Nanosecond, Value: 1}},
					},
				},
			},
		},
	}
	if err := sd.Compile(); err != nil {
		t.Fatal(err)
	}
	out, err := skeletal.Encode(sd)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want := `version: 1
bones:
  - {name: root}
  - {name: tail, parent: root, x: -0.1, rotation: 0.3, scaleX: 0, scaleY: 1.5, length: 7}
slots:
  - {name: tail, bone: tail, attachment: tail_idle, alpha: 0}
  - {name: glow, bone: root}
skins:
  default:
    glow: {frame: ""}
    tail: {frame: tail, scaleY: -1, originX: 2.5}
  empty: {}
animations:
  swish:
    duration: 3.000000001
    bones:
      root: {}
      tail:
        translate:
          - {time: 0, x: 1, y: 2, curve: stepped}
        rotate:
          - {time: 0, angle: -1, curve: bezier, bezier: [0, -0.5, 1, 1.5]}
          - {time: 0.333333333, angle: 1}
        scale:
          - {time: 1, x: 0, y: 1}
    slots:
      glow:
        attachment:
          - {time: 0, frame: ""}
          - {time: 1, frame: "on"}
        alpha:
          - {time: 0, value: 0}
          - {time: 3.000000001, value: 1}
`
	if string(out) != want {
		t.Fatalf("Encode:\n%s\nwant:\n%s", out, want)
	}
	back, err := skeletal.Load(out)
	if err != nil {
		t.Fatalf("Load(Encode): %v", err)
	}
	sameRig(t, back, sd)
}

func TestEncodeRejectsInvalidRigs(t *testing.T) {
	valid := func() *skeletal.SkeletonData {
		return &skeletal.SkeletonData{
			Version: skeletal.FormatVersion,
			Bones:   []skeletal.BoneData{{Name: "root", ScaleX: 1, ScaleY: 1}},
			Slots:   []skeletal.SlotData{{Name: "s", Bone: "root", Alpha: 1}},
		}
	}
	anim := func(tl skeletal.BoneTimeline) map[string]*skeletal.Animation {
		return map[string]*skeletal.Animation{"a": {Name: "a", Duration: time.Second, Bones: map[string]skeletal.BoneTimeline{"root": tl}}}
	}
	cases := []struct {
		name   string
		mutate func(sd *skeletal.SkeletonData)
		want   string
	}{
		{"wrong version", func(sd *skeletal.SkeletonData) { sd.Version = 2 }, "unsupported version 2"},
		{"unnamed bone", func(sd *skeletal.SkeletonData) { sd.Bones = append(sd.Bones, skeletal.BoneData{ScaleX: 1, ScaleY: 1}) }, "has no name"},
		{"duplicate bone", func(sd *skeletal.SkeletonData) { sd.Bones = append(sd.Bones, sd.Bones[0]) }, `duplicate bone "root"`},
		{"dangling slot bone", func(sd *skeletal.SkeletonData) { sd.Slots[0].Bone = "ghost" }, `unknown bone "ghost"`},
		{"NaN bone", func(sd *skeletal.SkeletonData) { sd.Bones[0].X = float32(math.NaN()) }, "not finite"},
		{"alpha out of range", func(sd *skeletal.SkeletonData) { sd.Slots[0].Alpha = 2 }, "out of range"},
		{"nil animation", func(sd *skeletal.SkeletonData) { sd.Animations = map[string]*skeletal.Animation{"a": nil} }, `animation "a" is nil`},
		{"unknown curve type", func(sd *skeletal.SkeletonData) {
			sd.Animations = anim(skeletal.BoneTimeline{Rotate: []skeletal.FloatKeyframe{{Curve: 9}}})
		}, "unknown curve type 9"},
		{"negative time", func(sd *skeletal.SkeletonData) {
			sd.Animations = anim(skeletal.BoneTimeline{Rotate: []skeletal.FloatKeyframe{{Time: -1}}})
		}, "invalid time"},
		{"unsorted keyframes", func(sd *skeletal.SkeletonData) {
			sd.Animations = anim(skeletal.BoneTimeline{Rotate: []skeletal.FloatKeyframe{{Time: time.Second}, {Time: 0}}})
		}, "precedes previous"},
		{"duration beyond MaxSeconds", func(sd *skeletal.SkeletonData) {
			sd.Animations = map[string]*skeletal.Animation{"a": {Name: "a", Duration: (skeletal.MaxSeconds + 1) * time.Second}}
		}, "not a number of seconds"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sd := valid()
			tc.mutate(sd)
			out, err := skeletal.Encode(sd)
			if err == nil {
				t.Fatalf("Encode succeeded with:\n%s", out)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err, tc.want)
			}
		})
	}
	if _, err := skeletal.Encode(nil); err == nil {
		t.Fatal("Encode(nil) succeeded")
	}
}

// FuzzLoadEncodeRoundTrip checks that every document Load accepts encodes, that the encoding loads back to the same rig, and that encoding is then a fixed point.
func FuzzLoadEncodeRoundTrip(f *testing.F) {
	f.Add(runnerYAML)
	f.Add([]byte("version: 1\nbones: [{name: root}]\n"))
	f.Add([]byte("version: 1\nbones: [{name: a}, {name: b, parent: a, scaleX: 0}]\nslots: [{name: s, bone: b, alpha: 0.25}]\nskins: {default: {s: {frame: \"\"}}}\nanimations: {x: {bones: {b: {scale: [{time: 0.1234567891, x: 2}]}}, slots: {s: {alpha: [{time: 0, value: 1, curve: bezier, bezier: [0.1, -3, 0.9, 4]}]}}}}\n"))
	f.Add([]byte(`{"version": 1, "bones": [{"name": "root", "rotation": 1e-7}], "animations": {"a": {"duration": 999999.999999999}}}`))
	f.Fuzz(func(t *testing.T, doc []byte) {
		data, err := skeletal.Load(doc)
		if err != nil {
			return
		}
		out, err := skeletal.Encode(data)
		if err != nil {
			t.Fatalf("Encode of a loaded rig failed: %v\ninput:\n%s", err, doc)
		}
		back, err := skeletal.Load(out)
		if err != nil {
			t.Fatalf("Load(Encode) failed: %v\noutput:\n%s", err, out)
		}
		sameRig(t, back, data)
		again, err := skeletal.Encode(back)
		if err != nil {
			t.Fatalf("second Encode failed: %v", err)
		}
		if !bytes.Equal(again, out) {
			t.Fatalf("encoding is not a fixed point:\nfirst:\n%s\nsecond:\n%s", out, again)
		}
	})
}

func BenchmarkLoadSample(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if _, err := skeletal.Load(runnerYAML); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeSample(b *testing.B) {
	data, err := skeletal.Load(runnerYAML)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := skeletal.Encode(data); err != nil {
			b.Fatal(err)
		}
	}
}
