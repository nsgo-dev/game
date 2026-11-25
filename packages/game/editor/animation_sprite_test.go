package editor_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/animclip"
	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func asepriteTestAtlas(t *testing.T) *sprite.Atlas {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "sprite", "testdata", "aseprite-hash.json"))
	if err != nil {
		t.Fatalf("read aseprite fixture: %v", err)
	}
	atlas, err := sprite.LoadAtlas(core.ImageSource{URI: "asset:hero.png"}, data)
	if err != nil {
		t.Fatalf("LoadAtlas: %v", err)
	}
	return atlas
}

func spriteFlipbookEditor(t *testing.T) (*editor.Editor, *sprite.Atlas) {
	t.Helper()
	atlas := asepriteTestAtlas(t)
	doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "Flipbook Scene",
		VirtualWidth:    160,
		VirtualHeight:   120,
		BackgroundColor: "#141824",
		Layers: []sceneformat.LayerDoc{{
			Name:  "actors",
			Space: sceneformat.SpaceWorld,
			Order: 0,
			Objects: []sceneformat.ObjectDoc{
				{
					ID:     "hero",
					Name:   "Hero",
					X:      40,
					Y:      40,
					Sprite: &sceneformat.SpriteRef{Atlas: "hero", Frame: "hero 0.aseprite"},
				},
				{
					ID:   "anchor",
					Name: "Anchor",
					X:    10,
					Y:    10,
				},
			},
		}},
	}
	ed, err := editor.New(doc, sceneformat.AtlasMap{"hero": atlas})
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SelectObject("hero")
	return ed, atlas
}

// #881 acceptance: a clip of four frames keyed in the editor plays as a flipbook in the editor preview and in the game through animclip, and round-trips byte-for-byte unchanged.
func TestFourFrameClipKeyedInEditorPlaysAsFlipbookAndRoundTrips(t *testing.T) {
	ed, atlas := spriteFlipbookEditor(t)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return editor.EditorView(cx, ed) }, nsgotest.Size(1280, 900))

	app.Press("id:toolbar-animate")
	app.Post(func() {
		if err := ed.SetClipName("Hero Walk"); err != nil {
			t.Errorf("SetClipName: %v", err)
		}
		if err := ed.SetClipLength(400 * time.Millisecond); err != nil {
			t.Errorf("SetClipLength: %v", err)
		}
		if err := ed.SetClipLoop(true); err != nil {
			t.Errorf("SetClipLoop: %v", err)
		}
	})
	app.Settle()

	// Pick sprite.frame and key all four frames via the frame picker in the UI.
	app.Press("id:anim-property")
	app.Press("role:option sprite.frame")
	if !app.Exists("id:anim-frame-picker") {
		t.Fatal("frame picker did not appear when sprite.frame was selected")
	}

	wantFrames := []string{"hero 0.aseprite", "hero 1.aseprite", "hero 2.aseprite", "hero 3.aseprite"}
	for i, fn := range wantFrames {
		at := time.Duration(i*100) * time.Millisecond
		app.Post(func() { ed.SeekClip(at) })
		app.Settle()
		app.Press("id:anim-frame-picker")
		app.Press("role:option " + fn)
		app.Press("id:anim-key")
	}

	// Timeline shows a thumbnail canvas for each of the four keyed frames.
	for i := range wantFrames {
		id := "id:anim-frame-thumb-0-" + string(rune('0'+i))
		if !app.Exists(id) {
			t.Fatalf("missing timeline frame thumbnail %s", id)
		}
	}

	// Clicking a frame thumbnail selects it, shows frame editor controls (not numeric interp), and seeks to it.
	app.Press("id:anim-key-0-2")
	if tr, k, ok := ed.SelectedClipKey(); !ok || tr != 0 || k != 2 {
		t.Fatalf("selected key = %d, %d, %t", tr, k, ok)
	}
	if !app.Exists("id:anim-key-frame") || !app.Exists("id:anim-frame-duration") {
		t.Fatal("selecting a sprite.frame key did not show frame editor controls")
	}
	if app.Exists("id:anim-interp-linear") {
		t.Fatal("sprite.frame key editor should not show numeric interpolation controls")
	}
	if got := app.Find("id:anim-frame-duration").Text; got != "100 ms" {
		t.Fatalf("frame duration label = %q, want 100 ms", got)
	}
	if got := ed.ActiveScene().Object("hero").Sprite.Frame.Name; got != "hero 2.aseprite" {
		t.Fatalf("preview frame at key 2 = %q, want hero 2.aseprite", got)
	}

	// Save YAML and verify byte-for-byte round-trip through resource and through reopening in the editor.
	clipYAML, err := ed.SaveClipYAML()
	if err != nil {
		t.Fatalf("SaveClipYAML: %v", err)
	}
	sceneYAML, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	parsed, err := resource.ParseAs[*resource.Animation](clipYAML)
	if err != nil {
		t.Fatalf("ParseAs: %v\n%s", err, clipYAML)
	}
	if again, err := resource.Encode(parsed); err != nil || !bytes.Equal(again, clipYAML) {
		t.Fatalf("clip YAML changed on round-trip (%v):\n%s\n---\n%s", err, clipYAML, again)
	}
	reopened, _ := spriteFlipbookEditor(t)
	if err := reopened.OpenClipYAML(clipYAML, "hero"); err != nil {
		t.Fatalf("OpenClipYAML: %v", err)
	}
	if again, err := reopened.SaveClipYAML(); err != nil || !bytes.Equal(again, clipYAML) {
		t.Fatalf("clip YAML changed on editor reopen+save (%v):\n%s\n---\n%s", err, clipYAML, again)
	}

	// Play in the game through animclip and verify exact frame-by-frame agreement with the editor preview.
	gameDoc, err := sceneformat.Parse(sceneYAML)
	if err != nil {
		t.Fatal(err)
	}
	built, err := sceneformat.Build(gameDoc, sceneformat.AtlasMap{"hero": atlas})
	if err != nil {
		t.Fatal(err)
	}
	player, err := animclip.NewPlayer(parsed, nil, animclip.BindScene(built, "hero"))
	if err != nil {
		t.Fatalf("animclip.NewPlayer: %v", err)
	}
	player.Restart()

	var seenGame, seenEditor []string
	step := 25 * time.Millisecond
	for i := range 20 {
		if i > 0 {
			player.Update(step)
		}
		app.Post(func() { ed.SeekClip(player.Time()) })
		app.Settle()

		gf := built.Object("hero").Sprite.Frame.Name
		ef := ed.ActiveScene().Object("hero").Sprite.Frame.Name
		if gf != ef {
			t.Fatalf("at %v: game frame = %q, editor preview = %q", player.Time(), gf, ef)
		}
		if len(seenGame) == 0 || seenGame[len(seenGame)-1] != gf {
			seenGame = append(seenGame, gf)
		}
		if len(seenEditor) == 0 || seenEditor[len(seenEditor)-1] != ef {
			seenEditor = append(seenEditor, ef)
		}
	}
	wantCycle := []string{"hero 0.aseprite", "hero 1.aseprite", "hero 2.aseprite", "hero 3.aseprite", "hero 0.aseprite"}
	if strings.Join(seenGame, "|") != strings.Join(wantCycle, "|") {
		t.Fatalf("game flipbook frames = %v, want %v", seenGame, wantCycle)
	}

	// Previewing never mutates the scene being edited.
	if hero, _, _ := ed.FindObject("hero"); hero.Sprite.Frame != "hero 0.aseprite" {
		t.Fatalf("edited scene hero frame mutated to %q", hero.Sprite.Frame)
	}
}

// #881 acceptance: a clip made from an Aseprite tag matches the tag's frames and durations, both via Go API and UI.
func TestNewClipFromAsepriteTagMatchesTagFramesAndDurations(t *testing.T) {
	ed, atlas := spriteFlipbookEditor(t)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return editor.EditorView(cx, ed) }, nsgotest.Size(1280, 900))
	app.Press("id:toolbar-animate")

	if !app.Exists("id:anim-aseprite-tag") || !app.Exists("id:anim-from-tag") {
		t.Fatal("Aseprite tag controls did not appear for an atlas with tags")
	}
	app.Press("id:anim-aseprite-tag")
	app.Press("role:option walk")
	app.Press("id:anim-from-tag")

	clip := ed.Clip()
	if clip == nil || clip.Name != "walk" || !clip.Loop {
		t.Fatalf("clip from tag walk = %+v", clip)
	}
	tag := atlas.Animations["walk"]
	if len(clip.Tracks) != 1 || len(clip.Tracks[0].Frames) != len(tag.Frames) {
		t.Fatalf("tracks = %+v, want %d frames", clip.Tracks, len(tag.Frames))
	}
	for i, af := range tag.Frames {
		if got := clip.Tracks[0].Frames[i].Frame; got != af.Frame.Name {
			t.Fatalf("frame %d = %q, want %q", i, got, af.Frame.Name)
		}
		dur, ok := ed.FrameDuration(0, i)
		if !ok || dur != af.Duration {
			t.Fatalf("frame %d duration = %v (ok=%t), want %v", i, dur, ok, af.Duration)
		}
	}

	// Retiming a frame with +10ms / -10ms works and undoes cleanly.
	app.Press("id:anim-key-0-1")
	app.Press("id:anim-frame-longer")
	if dur, _ := ed.FrameDuration(0, 1); dur != 110*time.Millisecond {
		t.Fatalf("after +10ms: frame 1 duration = %v, want 110ms", dur)
	}
	app.Press("id:anim-frame-shorter")
	if dur, _ := ed.FrameDuration(0, 1); dur != 100*time.Millisecond {
		t.Fatalf("after -10ms: frame 1 duration = %v, want 100ms", dur)
	}

	// Undo/Redo restores previous clip state.
	app.Press("id:toolbar-undo")
	if dur, _ := ed.FrameDuration(0, 1); dur != 110*time.Millisecond {
		t.Fatalf("after undo -10ms: frame 1 duration = %v, want 110ms", dur)
	}

	// Creating from a run of frames at a fixed FPS in the UI works too.
	app.Clear("label:Frame rate (FPS)")
	app.Type("label:Frame rate (FPS)", "8")
	app.Submit("label:Frame rate (FPS)")
	app.Press("id:anim-from-frames")
	runClip := ed.Clip()
	if runClip == nil || len(runClip.Tracks) != 1 || len(runClip.Tracks[0].Frames) != 4 {
		t.Fatalf("clip from frames = %+v", runClip)
	}
	if dur, _ := ed.FrameDuration(0, 0); dur != 125*time.Millisecond {
		t.Fatalf("8 fps frame duration = %v, want 125ms", dur)
	}
}

func TestSpriteFrameEditsUndoRedoAndValidation(t *testing.T) {
	ed, _ := spriteFlipbookEditor(t)

	// Refusals before clip or on invalid targets/frames.
	if err := ed.NewClipFromAsepriteTag("anchor", "walk"); err == nil || !strings.Contains(err.Error(), "has no sprite") {
		t.Fatalf("NewClipFromAsepriteTag on spriteless object = %v", err)
	}
	if err := ed.NewClipFromFrames("hero", "Bad", []string{"nonexistent"}, 10, true); err == nil || !strings.Contains(err.Error(), "no frame") {
		t.Fatalf("NewClipFromFrames with missing frame = %v", err)
	}

	save := func() string {
		t.Helper()
		if ed.Clip() == nil {
			return "<none>"
		}
		b, err := ed.SaveClipYAML()
		if err != nil {
			t.Fatalf("SaveClipYAML: %v", err)
		}
		return string(b)
	}

	states := []string{save()}
	record := func(name string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		states = append(states, save())
	}

	record("NewClip", ed.NewClip("hero", "Flip", time.Second))
	_, _, err := ed.KeyProperty("", animclip.PropertySpriteFrame, 0)
	record("KeyProperty sprite.frame@0", err)
	_, _, err = ed.KeySpriteFrame("", "hero 1.aseprite", 200*time.Millisecond)
	record("KeySpriteFrame@200ms", err)
	record("SetKeyFrame", ed.SetKeyFrame(0, 1, "hero 2.aseprite"))
	moved, err := ed.MoveKey(0, 1, 300*time.Millisecond)
	record("MoveKey", err)
	if moved != 1 {
		t.Fatalf("MoveKey returned %d, want 1", moved)
	}
	record("SetFrameDuration", ed.SetFrameDuration(0, 0, 250*time.Millisecond))
	record("DeleteKey", ed.DeleteKey(0, 1))
	record("NewClipFromAsepriteTag", ed.NewClipFromAsepriteTag("hero", "sway"))
	record("NewClipFromFrames", ed.NewClipFromFrames("hero", "Run", []string{"hero 0.aseprite", "hero 1.aseprite"}, 10, false))

	// Refusals on a frame track.
	if err := ed.SetKeyValue(0, 0, 1); err == nil || !strings.Contains(err.Error(), "SetKeyFrame") {
		t.Fatalf("SetKeyValue on frame track = %v", err)
	}
	if err := ed.SetKeyInterp(0, 0, resource.InterpSmooth); err == nil || !strings.Contains(err.Error(), "discretely") {
		t.Fatalf("SetKeyInterp on frame track = %v", err)
	}
	if err := ed.SetKeyFrame(0, 0, "bogus"); err == nil || !strings.Contains(err.Error(), "no frame") {
		t.Fatalf("SetKeyFrame(bogus) = %v", err)
	}
	if _, _, err := ed.KeySpriteFrame("anchor", "hero 0.aseprite", 0); err == nil || !strings.Contains(err.Error(), "has no sprite") {
		t.Fatalf("KeySpriteFrame on spriteless object = %v", err)
	}

	for i := len(states) - 1; i > 0; i-- {
		if !ed.Undo() {
			t.Fatalf("Undo %d failed", i)
		}
		if got := save(); got != states[i-1] {
			t.Fatalf("after undo %d:\n%s\nwant\n%s", i, got, states[i-1])
		}
	}
	for i := 1; i < len(states); i++ {
		if !ed.Redo() {
			t.Fatalf("Redo %d failed", i)
		}
		if got := save(); got != states[i] {
			t.Fatalf("after redo %d:\n%s\nwant\n%s", i, got, states[i])
		}
	}
}
