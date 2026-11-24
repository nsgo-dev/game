package sprite_test

import (
	_ "embed"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

var (
	//go:embed testdata/aseprite-hash.json
	asepriteHash []byte
	//go:embed testdata/aseprite-array.json
	asepriteArray []byte
	//go:embed testdata/texturepacker-hash.json
	texturePackerHash []byte
	//go:embed testdata/texturepacker-array.json
	texturePackerArray []byte
)

var heroImage = core.ImageSource{URI: "asset:hero.png"}

func TestAnAsepriteExportLoadsWithItsTagsAsAnimations(t *testing.T) {
	frame := func(n int) sprite.Frame {
		return []sprite.Frame{
			{Name: "hero 0.aseprite", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
			{Name: "hero 1.aseprite", X: 16, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
			{Name: "hero 2.aseprite", X: 32, Y: 0, W: 12, H: 14, OffsetX: 2, OffsetY: 1, Width: 16, Height: 16},
			{Name: "hero 3.aseprite", X: 44, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
		}[n]
	}
	durations := []time.Duration{100 * time.Millisecond, 100 * time.Millisecond, 150 * time.Millisecond, 200 * time.Millisecond}
	animation := func(loop bool, frames ...int) sprite.Animation {
		a := sprite.Animation{Loop: loop}
		for _, n := range frames {
			a.Frames = append(a.Frames, sprite.AnimationFrame{Frame: frame(n), Duration: durations[n]})
		}
		return a
	}
	want := map[string]sprite.Animation{
		"walk": animation(true, 0, 1, 2, 3),
		// Ping-pong loops back without showing either end twice.
		"sway": animation(true, 1, 2, 3, 2),
		"fall": animation(false, 2, 1, 0),
		// Three passes, each way counting as one, starting backward.
		"bounce": animation(false, 2, 1, 0, 1, 2, 1, 0),
	}
	for _, tc := range []struct {
		name string
		data []byte
	}{{"hash", asepriteHash}, {"array", asepriteArray}} {
		t.Run(tc.name, func(t *testing.T) {
			a, err := sprite.LoadAtlas(heroImage, tc.data)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(a.Image, heroImage) {
				t.Errorf("image %+v, want %+v", a.Image, heroImage)
			}
			if len(a.Frames) != 4 {
				t.Errorf("%d frames, want 4", len(a.Frames))
			}
			for n := range 4 {
				if got := a.Frames[frame(n).Name]; got != frame(n) {
					t.Errorf("frame %q is %+v, want %+v", frame(n).Name, got, frame(n))
				}
			}
			if !reflect.DeepEqual(a.Animations, want) {
				t.Errorf("animations:\n%+v\nwant:\n%+v", a.Animations, want)
			}
		})
	}
}

func TestATexturePackerExportLoadsInTheImagesPoints(t *testing.T) {
	// meta.scale is 2: the 2x image's pixels are halved into points, and pivots scale with the untrimmed size in points.
	want := map[string]sprite.Frame{
		"gem.png":  {Name: "gem.png", X: 1, Y: 1, W: 20, H: 20, Width: 20, Height: 20, PivotX: 10, PivotY: 10},
		"coin.png": {Name: "coin.png", X: 22, Y: 1, W: 12, H: 14, OffsetX: 4, OffsetY: 3, Width: 20, Height: 20, PivotX: 10, PivotY: 10},
		"flag.png": {Name: "flag.png", X: 35, Y: 1, W: 10, H: 18, OffsetX: 2, OffsetY: 1, Width: 16, Height: 20, PivotX: 4, PivotY: 20},
		"key.png":  {Name: "key.png", X: 46, Y: 1, W: 8, H: 12, Width: 8, Height: 12},
	}
	for _, tc := range []struct {
		name string
		data []byte
	}{{"hash", texturePackerHash}, {"array", texturePackerArray}} {
		t.Run(tc.name, func(t *testing.T) {
			a, err := sprite.LoadAtlas(core.ImageSource{URI: "asset:items@2x.png"}, tc.data)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(a.Frames, want) {
				t.Errorf("frames %+v, want %+v", a.Frames, want)
			}
			if a.Animations != nil {
				t.Errorf("animations %+v, want none", a.Animations)
			}
		})
	}
}

func TestLoadAtlasRefusesWhatItCannotDraw(t *testing.T) {
	frame := `"frame": {"x":0,"y":0,"w":8,"h":8}, "sourceSize": {"w":8,"h":8}, "duration": 100`
	for _, tc := range []struct {
		name, json, err string
		field           string // the field the error names, checked with its line in the indented document
	}{
		{"no frames", `{"meta": {}}`, "no frames", ""},
		{"rotated", `{"frames": {"a": {` + frame + `, "rotated": true}}}`, "rotated", "frames.a.rotated"},
		{"duplicate", `{"frames": [{"filename": "a", ` + frame + `}, {"filename": "a", ` + frame + `}]}`, "two frames", "frames[1]"},
		{"nameless", `{"frames": [{` + frame + `}]}`, "no filename", "frames[0]"},
		{"bad frame", `{"frames": {"a": {"frame": "big"}}}`, "cannot unmarshal", `frames.a`},
		{"tag past the frames", `{"frames": {"a": {` + frame + `}}, "meta": {"frameTags": [{"name": "t", "from": 0, "to": 1}]}}`, "runs from frame 0 to 1 of 1", "meta.frameTags[0]"},
		{"unknown direction", `{"frames": {"a": {` + frame + `}}, "meta": {"frameTags": [{"name": "t", "from": 0, "to": 0, "direction": "sideways"}]}}`, "unknown direction", "meta.frameTags[0].direction"},
		{"bad repeat", `{"frames": {"a": {` + frame + `}}, "meta": {"frameTags": [{"name": "t", "from": 0, "to": 0, "repeat": "-1"}]}}`, "repeats", "meta.frameTags[0].repeat"},
		{"bad scale", `{"frames": {"a": {` + frame + `}}, "meta": {"scale": "0"}}`, "meta.scale", "meta.scale"},
		{"tag frame without a duration", `{"frames": {"a": {"frame": {"x":0,"y":0,"w":8,"h":8}}}, "meta": {"frameTags": [{"name": "t", "from": 0, "to": 0}]}}`, "no duration", "frames.a.duration"},
		{"slice center outside bounds", `{"frames": {"a": {` + frame + `}}, "meta": {"slices": [{"name": "bad", "keys": [{"frame": 0, "bounds": {"x":0,"y":0,"w":8,"h":8}, "center": {"x":2,"y":2,"w":8,"h":4}}]}]}}`, "outside its bounds", "meta.slices[0].keys[0].center"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := loaderrtest.Indent(tc.json)
			_, err := sprite.LoadAtlas(heroImage, data)
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Fatalf("error %v, want one saying %q", err, tc.err)
			}
			loaderrtest.ExpectField(t, loaderr.WithFile(err, "hero.json"), "hero.json", data, tc.field)
		})
	}
	t.Run("not JSON", func(t *testing.T) {
		_, err := sprite.LoadAtlas(heroImage, []byte("{\n  \"frames\": [\n"))
		if err == nil || !strings.Contains(err.Error(), "unexpected end") {
			t.Fatalf("error %v, want one saying the document ends early", err)
		}
		if e := loaderrtest.Expect(t, loaderr.WithFile(err, "hero.json"), "hero.json", 2); e.Fix != loaderr.FixTruncated {
			t.Fatalf("Fix = %q, want FixTruncated", e.Fix)
		}
	})
}

func TestLoadAtlasReadsAsepriteNineSlices(t *testing.T) {
	data := `{
		"frames": {
			"panel.png": {"frame": {"x":0,"y":0,"w":64,"h":48}, "sourceSize": {"w":64,"h":48}, "duration": 100}
		},
		"meta": {
			"scale": "2",
			"slices": [
				{
					"name": "panel",
					"keys": [{"frame": 0, "bounds": {"x":0,"y":0,"w":64,"h":48}, "center": {"x":12,"y":8,"w":36,"h":30}}]
				},
				{
					"name": "hitbox",
					"keys": [{"frame": 0, "bounds": {"x":4,"y":4,"w":20,"h":20}}]
				}
			]
		}
	}`
	a, err := sprite.LoadAtlas(heroImage, []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]sprite.Insets{
		"panel": {Left: 6, Top: 4, Right: 8, Bottom: 5},
	}
	if !reflect.DeepEqual(a.Slices, want) {
		t.Fatalf("slices %+v, want %+v", a.Slices, want)
	}
}
