package animclip_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/animclip"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

func flipbookAtlas() *sprite.Atlas {
	return &sprite.Atlas{
		Image: core.ImageSource{URI: "asset:hero.png"},
		Frames: map[string]sprite.Frame{
			"walk_0": {Name: "walk_0", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
			"walk_1": {Name: "walk_1", X: 16, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
			"walk_2": {Name: "walk_2", X: 32, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
			"walk_3": {Name: "walk_3", X: 48, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
		},
	}
}

func TestSpriteFrameLookupAndCheck(t *testing.T) {
	p, err := animclip.Lookup(nil, animclip.PropertySpriteFrame)
	if err != nil {
		t.Fatalf("Lookup(nil, sprite.frame): %v", err)
	}
	if p.Name != "sprite.frame" || p.Kind != animclip.KindFrame || !p.IsFrame() || p.IsComponent() || p.Kind.String() != "frame" {
		t.Fatalf("Lookup(sprite.frame) = %+v", p)
	}
	if err := p.CheckValue(0); err == nil || !strings.Contains(err.Error(), "frame names") {
		t.Fatalf("CheckValue on sprite.frame = %v", err)
	}
	if err := p.CheckFrame(""); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("CheckFrame(\"\") = %v", err)
	}
	if err := p.CheckFrame("walk_0"); err != nil {
		t.Fatalf("CheckFrame(walk_0) = %v", err)
	}

	// Numeric keys on sprite.frame are refused by Check.
	badNum := &resource.Animation{
		Header: resource.NewHeader(resource.KindAnimation),
		Length: 1,
		Tracks: []resource.Track{{Property: "sprite.frame", Keys: []resource.Key{{Time: 0, Value: 1}}}},
	}
	if err := animclip.Check(badNum, nil); err == nil || !strings.Contains(err.Error(), "requires frame keys") {
		t.Fatalf("Check numeric keys on sprite.frame = %v", err)
	}

	// Frame keys on a numeric property are refused by Check.
	badFrame := &resource.Animation{
		Header: resource.NewHeader(resource.KindAnimation),
		Length: 1,
		Tracks: []resource.Track{{Property: "x", Frames: []resource.FrameKey{{Time: 0, Frame: "walk_0"}}}},
	}
	if err := animclip.Check(badFrame, nil); err == nil || !strings.Contains(err.Error(), "requires numeric keys") {
		t.Fatalf("Check frame keys on x = %v", err)
	}
}

func TestFromFramesPlaysFlipbookInBuiltScene(t *testing.T) {
	frames := []string{"walk_0", "walk_1", "walk_2", "walk_3"}
	clip, err := animclip.FromFrames("Walk", frames, 10, true)
	if err != nil {
		t.Fatalf("FromFrames: %v", err)
	}
	if clip.Name != "Walk" || !clip.Loop || clip.Length != 0.4 {
		t.Fatalf("clip header = name %q loop %t length %v", clip.Name, clip.Loop, clip.Length)
	}

	// Round-trips unchanged through YAML.
	encoded, err := resource.Encode(clip)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	reloaded, err := resource.ParseAs[*resource.Animation](encoded)
	if err != nil {
		t.Fatalf("ParseAs: %v\n%s", err, encoded)
	}
	if again, err := resource.Encode(reloaded); err != nil || !bytes.Equal(again, encoded) {
		t.Fatalf("round-trip changed bytes (%v):\n%s\n---\n%s", err, encoded, again)
	}

	atlas := flipbookAtlas()
	doc := &sceneformat.SceneDoc{Version: 1, Layers: []sceneformat.LayerDoc{{Name: "main", Objects: []sceneformat.ObjectDoc{
		{ID: "hero", Sprite: &sceneformat.SpriteRef{Atlas: "hero", Frame: "walk_0"}},
	}}}}
	built, err := sceneformat.Build(doc, resolver{atlas: atlas})
	if err != nil {
		t.Fatal(err)
	}
	player, err := animclip.NewPlayer(reloaded, nil, animclip.BindScene(built, "hero"))
	if err != nil {
		t.Fatalf("NewPlayer: %v", err)
	}

	hero := built.Object("hero")
	for _, tc := range []struct {
		at   time.Duration
		want string
	}{
		{0, "walk_0"},
		{50 * time.Millisecond, "walk_0"},
		{99 * time.Millisecond, "walk_0"},
		{100 * time.Millisecond, "walk_1"},
		{199 * time.Millisecond, "walk_1"},
		{200 * time.Millisecond, "walk_2"},
		{299 * time.Millisecond, "walk_2"},
		{300 * time.Millisecond, "walk_3"},
		{399 * time.Millisecond, "walk_3"},
		{400 * time.Millisecond, "walk_3"}, // Seek(Length) shows the end pose
		{450 * time.Millisecond, "walk_0"}, // Looping wraps past Length
	} {
		player.Seek(tc.at)
		if hero.Doc.Sprite.Frame != tc.want || hero.Sprite.Frame.Name != tc.want {
			t.Fatalf("at %v: doc frame %q, sprite frame %q, want %q", tc.at, hero.Doc.Sprite.Frame, hero.Sprite.Frame.Name, tc.want)
		}
		if wantRect := atlas.Frames[tc.want]; hero.Sprite.Frame != wantRect {
			t.Fatalf("at %v: sprite Frame %+v, want %+v", tc.at, hero.Sprite.Frame, wantRect)
		}
	}

	// Stepping with Update cycles cleanly through all four frames and loops back to walk_0.
	player.Restart()
	var seen []string
	record := func() {
		f := hero.Sprite.Frame.Name
		if len(seen) == 0 || seen[len(seen)-1] != f {
			seen = append(seen, f)
		}
	}
	record()
	for range 30 {
		player.Update(20 * time.Millisecond)
		record()
	}
	wantSeq := []string{"walk_0", "walk_1", "walk_2", "walk_3", "walk_0", "walk_1", "walk_2"}
	if strings.Join(seen, ",") != strings.Join(wantSeq, ",") {
		t.Fatalf("flipbook sequence = %v, want %v", seen, wantSeq)
	}

	// BuiltObject.Sync also preserves and updates the sprite's frame when Doc.Sprite.Frame changes.
	hero.Doc.Sprite.Frame = "walk_2"
	hero.Sync()
	if hero.Sprite.Frame != atlas.Frames["walk_2"] {
		t.Fatalf("after Sync(), sprite frame = %+v, want walk_2", hero.Sprite.Frame)
	}
}

func TestFromAsepriteTagMatchesFramesAndDurations(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "sprite", "testdata", "aseprite-hash.json"))
	if err != nil {
		t.Fatalf("read aseprite fixture: %v", err)
	}
	atlas, err := sprite.LoadAtlas(core.ImageSource{URI: "asset:hero.png"}, data)
	if err != nil {
		t.Fatalf("LoadAtlas: %v", err)
	}

	for _, tagName := range []string{"walk", "sway", "fall", "bounce"} {
		t.Run(tagName, func(t *testing.T) {
			tag := atlas.Animations[tagName]
			clip, err := animclip.FromAsepriteTag(atlas, tagName)
			if err != nil {
				t.Fatalf("FromAsepriteTag(%q): %v", tagName, err)
			}
			if clip.Name != tagName {
				t.Fatalf("clip.Name = %q, want %q", clip.Name, tagName)
			}
			if clip.Loop != tag.Loop {
				t.Fatalf("clip.Loop = %t, want %t", clip.Loop, tag.Loop)
			}
			if len(clip.Tracks) != 1 || clip.Tracks[0].Property != animclip.PropertySpriteFrame {
				t.Fatalf("tracks = %+v", clip.Tracks)
			}
			keys := clip.Tracks[0].Frames
			if len(keys) != len(tag.Frames) {
				t.Fatalf("len(frames) = %d, want %d", len(keys), len(tag.Frames))
			}

			var total time.Duration
			for i, af := range tag.Frames {
				if keys[i].Frame != af.Frame.Name {
					t.Fatalf("key %d frame = %q, want %q", i, keys[i].Frame, af.Frame.Name)
				}
				if gotStart := animclip.Seconds(keys[i].Time); gotStart != total {
					t.Fatalf("key %d start = %v, want %v", i, gotStart, total)
				}
				var next time.Duration
				if i+1 < len(keys) {
					next = animclip.Seconds(keys[i+1].Time)
				} else {
					next = animclip.Seconds(clip.Length)
				}
				if gotDur := next - animclip.Seconds(keys[i].Time); gotDur != af.Duration {
					t.Fatalf("key %d duration = %v, want %v", i, gotDur, af.Duration)
				}
				total += af.Duration
			}
			if gotLen := animclip.Seconds(clip.Length); gotLen != total {
				t.Fatalf("clip length = %v, want %v", gotLen, total)
			}

			// Verify animclip.Player matches sprite.Animation frame-for-frame on 10ms steps.
			doc := &sceneformat.SceneDoc{Version: 1, Layers: []sceneformat.LayerDoc{{Name: "main", Objects: []sceneformat.ObjectDoc{
				{ID: "hero", Sprite: &sceneformat.SpriteRef{Atlas: "hero", Frame: tag.Frames[0].Frame.Name}},
			}}}}
			built, err := sceneformat.Build(doc, resolver{atlas: atlas})
			if err != nil {
				t.Fatal(err)
			}
			player, err := animclip.NewPlayer(clip, nil, animclip.BindScene(built, "hero"))
			if err != nil {
				t.Fatal(err)
			}
			ref := tag
			player.Restart()
			step := 10 * time.Millisecond
			for elapsed := time.Duration(0); elapsed < total+200*time.Millisecond; elapsed += step {
				if elapsed > 0 {
					player.Update(step)
					ref.Update(step)
				}
				if got, want := built.Object("hero").Sprite.Frame.Name, ref.Frame().Name; got != want {
					t.Fatalf("at %v: animclip plays %q, sprite.Animation plays %q", elapsed, got, want)
				}
			}
		})
	}

	if _, err := animclip.FromAsepriteTag(atlas, "missing"); err == nil || !strings.Contains(err.Error(), "walk") {
		t.Fatalf("FromAsepriteTag(missing) = %v, want available tags listed", err)
	}
	if _, err := animclip.FromAsepriteTag(nil, "walk"); err == nil {
		t.Fatal("FromAsepriteTag(nil) succeeded")
	}
	if _, err := animclip.FromFrames("empty", nil, 10, true); err == nil {
		t.Fatal("FromFrames(empty) succeeded")
	}
	if _, err := animclip.FromFrames("bad-fps", []string{"a"}, 0, true); err == nil {
		t.Fatal("FromFrames(fps=0) succeeded")
	}
}

func TestBindSceneRefusesMissingFrameOrSprite(t *testing.T) {
	clip, err := animclip.FromFrames("Walk", []string{"walk_0", "missing_frame"}, 10, false)
	if err != nil {
		t.Fatal(err)
	}
	doc := &sceneformat.SceneDoc{Version: 1, Layers: []sceneformat.LayerDoc{{Name: "main", Objects: []sceneformat.ObjectDoc{
		{ID: "hero", Sprite: &sceneformat.SpriteRef{Atlas: "hero", Frame: "walk_0"}},
		{ID: "empty"},
	}}}}
	built, err := sceneformat.Build(doc, resolver{atlas: flipbookAtlas()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := animclip.NewPlayer(clip, nil, animclip.BindScene(built, "hero")); err == nil || !strings.Contains(err.Error(), `no frame "missing_frame"`) {
		t.Fatalf("NewPlayer with missing frame = %v", err)
	}
	valid, err := animclip.FromFrames("Walk", []string{"walk_0"}, 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := animclip.NewPlayer(valid, nil, animclip.BindScene(built, "empty")); err == nil || !strings.Contains(err.Error(), "has no sprite") {
		t.Fatalf("NewPlayer on object without sprite = %v", err)
	}
	if _, err := animclip.NewPlayer(valid, nil, animclip.BindDoc(doc, "empty")); err == nil || !strings.Contains(err.Error(), "has no sprite") {
		t.Fatalf("BindDoc on object without sprite = %v", err)
	}
	// BindDoc updates Doc.Sprite.Frame on objects that have a sprite.
	p, err := animclip.NewPlayer(clip, nil, animclip.BindDoc(doc, "hero"))
	if err != nil {
		t.Fatalf("BindDoc: %v", err)
	}
	p.Seek(150 * time.Millisecond)
	if got := doc.Layers[0].Objects[0].Sprite.Frame; got != "missing_frame" {
		t.Fatalf("BindDoc frame at 150ms = %q, want missing_frame", got)
	}
}
