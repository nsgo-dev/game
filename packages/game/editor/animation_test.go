package editor_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/animclip"
	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// animatedEditor returns an editor over the test scene with the physics components registered, a slime placed beside the hero, and a two-second clip on the hero keyed through the editor's own commands.
func animatedEditor(t *testing.T) *editor.Editor {
	t.Helper()
	ed := newTestEditor(t)
	if err := ed.SetComponentRegistry(physicsComponents(t)); err != nil {
		t.Fatalf("SetComponentRegistry: %v", err)
	}
	if _, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{ID: "slime", X: 80, Y: 40, Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "slime"}}); err != nil {
		t.Fatalf("PlaceObject slime: %v", err)
	}
	if err := ed.NewClip("hero", "Hop", 2*time.Second); err != nil {
		t.Fatalf("NewClip: %v", err)
	}
	key := func(target, property string, at time.Duration, value float64, interp resource.Interp) {
		t.Helper()
		tr, k, err := ed.KeyProperty(target, property, at)
		if err != nil {
			t.Fatalf("KeyProperty(%q, %q, %v): %v", target, property, at, err)
		}
		if err := ed.SetKeyValue(tr, k, value); err != nil {
			t.Fatalf("SetKeyValue(%s @ %v = %v): %v", property, at, value, err)
		}
		if interp != "" {
			if err := ed.SetKeyInterp(tr, k, interp); err != nil {
				t.Fatalf("SetKeyInterp: %v", err)
			}
		}
	}
	key("", "x", 0, 40, resource.InterpSmooth)
	key("", "x", time.Second, 100, "")
	key("", "x", 2*time.Second, 60, "")
	key("", "alpha", 500*time.Millisecond, 0.25, "")
	key("", "alpha", 1500*time.Millisecond, 1, "")
	key("", "flipX", 1200*time.Millisecond, 1, "")
	key("", "physics.gravity", 0, 9.8, "")
	key("", "physics.gravity", 2*time.Second, 50, "")
	key("", "physics.layer", 2*time.Second, 7, "")
	key("", "physics.solid", time.Second, 0, "")
	key("slime", "rotation", 0, 0, "")
	key("slime", "rotation", 1500*time.Millisecond, 1.5, resource.InterpStep)
	if err := ed.AddClipEvent(time.Second, "land"); err != nil {
		t.Fatalf("AddClipEvent: %v", err)
	}
	return ed
}

// pose is what a test reads off a scene at one playback time.
type pose struct {
	heroX, heroSpriteX, heroAlpha, heroSpriteAlpha, slimeRot, slimeSpriteRot float32
	flipX, solid                                                             bool
	gravity                                                                  float64
	layer                                                                    int
}

func readPose(t *testing.T, s *sceneformat.BuiltScene) pose {
	t.Helper()
	hero, slime := s.Object("hero"), s.Object("slime")
	phys := hero.Doc.Components["physics"].(map[string]any)
	return pose{
		heroX: hero.Doc.X, heroSpriteX: hero.Sprite.X,
		heroAlpha: hero.Doc.EffectiveAlpha(), heroSpriteAlpha: hero.Sprite.Alpha,
		slimeRot: slime.Doc.Rotation, slimeSpriteRot: slime.Sprite.Rotation,
		flipX: hero.Sprite.FlipX, solid: phys["solid"].(bool),
		gravity: phys["gravity"].(float64), layer: phys["layer"].(int),
	}
}

// #491: a clip keyed in the editor plays the same in the game and round-trips unchanged.
func TestClipKeyedInEditorPlaysTheSameInGame(t *testing.T) {
	ed := animatedEditor(t)
	if err := ed.ClipError(); err != nil {
		t.Fatalf("ClipError: %v", err)
	}
	clipYAML, err := ed.SaveClipYAML()
	if err != nil {
		t.Fatalf("SaveClipYAML: %v", err)
	}
	sceneYAML, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}

	// The clip file round-trips unchanged, through the resource format and through the editor.
	parsed, err := resource.ParseAs[*resource.Animation](clipYAML)
	if err != nil {
		t.Fatalf("parse saved clip: %v\n%s", err, clipYAML)
	}
	if again, err := resource.Encode(parsed); err != nil || !bytes.Equal(again, clipYAML) {
		t.Fatalf("clip changed on a load and save (%v):\n%s\n---\n%s", err, clipYAML, again)
	}
	reopened := newTestEditor(t)
	if err := reopened.SetComponentRegistry(physicsComponents(t)); err != nil {
		t.Fatal(err)
	}
	if err := reopened.OpenClipYAML(clipYAML, "hero"); err != nil {
		t.Fatalf("OpenClipYAML: %v", err)
	}
	if again, err := reopened.SaveClipYAML(); err != nil || !bytes.Equal(again, clipYAML) {
		t.Fatalf("clip changed on a reopen and save in the editor (%v):\n%s", err, again)
	}
	for _, want := range []string{"property: physics.gravity", "target: slime", "interp: smooth", "name: land"} {
		if !bytes.Contains(clipYAML, []byte(want)) {
			t.Fatalf("saved clip lacks %q:\n%s", want, clipYAML)
		}
	}

	// The game loads the saved scene and clip and plays them on game.Loop's fixed steps.
	reg := physicsComponents(t)
	doc, err := sceneformat.ParseWith(sceneYAML, sceneformat.ParseOptions{Components: reg})
	if err != nil {
		t.Fatalf("parse saved scene: %v", err)
	}
	built, err := sceneformat.Build(doc, testResolver())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	player, err := animclip.NewPlayer(parsed, reg, animclip.BindScene(built, "hero"))
	if err != nil {
		t.Fatalf("NewPlayer: %v", err)
	}
	var events []string
	player.OnEvent = func(name string) { events = append(events, name) }
	player.Seek(0)
	player.Play()
	step := time.Second / 60
	for i := 0; ; i++ {
		if i > 0 {
			player.Update(step)
		}
		ed.SeekClip(player.Time())
		if got, want := readPose(t, built), readPose(t, ed.ActiveScene()); got != want {
			t.Fatalf("at %v the game shows %+v, the editor previews %+v", player.Time(), got, want)
		}
		if player.Done() {
			break
		}
	}
	if len(events) != 1 || events[0] != "land" {
		t.Fatalf("events = %v, want [land]", events)
	}
	end := readPose(t, built)
	if end.heroX != 60 || end.gravity != 50 || end.layer != 7 || end.solid || !end.flipX || end.slimeRot != 1.5 || end.heroAlpha != 1 {
		t.Fatalf("end pose = %+v", end)
	}

	// Previewing never writes animated values into the scene being edited.
	if after, err := ed.SaveYAML(); err != nil || !bytes.Equal(after, sceneYAML) {
		t.Fatalf("previewing the clip changed the scene (%v)", err)
	}
	hero, _, _ := ed.FindObject("hero")
	if hero.X != 40 || ed.Built.Object("hero").Sprite.X != 40 {
		t.Fatalf("edited hero x = %v, sprite %v, want 40", hero.X, ed.Built.Object("hero").Sprite.X)
	}
}

func TestClipEditsUndoAndRedoExactly(t *testing.T) {
	ed := newTestEditor(t)
	if err := ed.SetComponentRegistry(physicsComponents(t)); err != nil {
		t.Fatal(err)
	}
	ed.SelectObject("hero")
	save := func() string {
		t.Helper()
		if ed.Clip() == nil {
			return "<none>"
		}
		data, err := ed.SaveClipYAML()
		if err != nil {
			t.Fatalf("SaveClipYAML: %v", err)
		}
		return string(data)
	}
	states := []string{save()}
	step := func(name string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		states = append(states, save())
	}
	step("NewClip", ed.NewClip("", "Walk", time.Second))
	_, _, err := ed.KeyProperty("", "y", 0)
	step("KeyProperty y@0", err)
	_, _, err = ed.KeyProperty("", "y", 500*time.Millisecond)
	step("KeyProperty y@0.5", err)
	step("SetKeyValue", ed.SetKeyValue(0, 1, 12))
	step("SetKeyInterp", ed.SetKeyInterp(0, 0, resource.InterpSmooth))
	moved, err := ed.MoveKey(0, 0, 750*time.Millisecond)
	step("MoveKey", err)
	if moved != 1 {
		t.Fatalf("MoveKey past its neighbor returned index %d, want 1", moved)
	}
	step("SetClipLoop", ed.SetClipLoop(true))
	step("SetClipName", ed.SetClipName("Walk cycle"))
	step("SetClipLength", ed.SetClipLength(2*time.Second))
	step("AddClipEvent", ed.AddClipEvent(250*time.Millisecond, "step"))
	step("DeleteClipEvent", ed.DeleteClipEvent(0))
	_, _, err = ed.KeyProperty("", "physics.layer", time.Second)
	step("KeyProperty physics.layer", err)
	step("DeleteKey", ed.DeleteKey(1, 0))
	step("RemoveClipTrack", ed.RemoveClipTrack(0))
	step("CloseClip", ed.CloseClip())

	for i := len(states) - 1; i > 0; i-- {
		if !ed.Undo() {
			t.Fatalf("Undo %d failed", i)
		}
		if got := save(); got != states[i-1] {
			t.Fatalf("after undoing step %d:\n%s\nwant\n%s", i, got, states[i-1])
		}
	}
	for i := 1; i < len(states); i++ {
		if !ed.Redo() {
			t.Fatalf("Redo %d failed", i)
		}
		if got := save(); got != states[i] {
			t.Fatalf("after redoing step %d:\n%s\nwant\n%s", i, got, states[i])
		}
	}
}

func TestClipEditsRefuseWhatTheClipCannotHold(t *testing.T) {
	ed := newTestEditor(t)
	if _, _, err := ed.KeyProperty("", "x", 0); !errors.Is(err, editor.ErrNoClip) {
		t.Fatalf("KeyProperty with no clip: %v", err)
	}
	if err := ed.NewClip("", "Nothing selected", time.Second); err == nil {
		t.Fatal("NewClip with no target and no selection succeeded")
	}
	if err := ed.NewClip("ghost", "Missing", time.Second); err == nil {
		t.Fatal("NewClip on a missing object succeeded")
	}
	if err := ed.SetComponentRegistry(physicsComponents(t)); err != nil {
		t.Fatal(err)
	}
	if err := ed.NewClip("hero", "Clip", time.Second); err != nil {
		t.Fatal(err)
	}
	for name, err := range map[string]error{
		"time past the end":     second(ed.KeyProperty("", "x", 2*time.Second)),
		"string field":          second(ed.KeyProperty("", "physics.kind", 0)),
		"unknown property":      second(ed.KeyProperty("", "speed", 0)),
		"unregistered":          second(ed.KeyProperty("", "wheel.speed", 0)),
		"missing target":        second(ed.KeyProperty("ghost", "x", 0)),
		"length cutting a key":  keyThen(ed, func() error { return ed.SetClipLength(100 * time.Millisecond) }),
		"fractional int key":    keyLayerThen(ed, func(tr, k int) error { return ed.SetKeyValue(tr, k, 1.5) }),
		"int key out of range":  keyLayerThen(ed, func(tr, k int) error { return ed.SetKeyValue(tr, k, 40) }),
		"move onto another key": keyLayerThen(ed, func(tr, k int) error { _, err := ed.MoveKey(tr, k, 0); return err }),
		"no such key":           ed.SetKeyValue(9, 9, 0),
		"event without a name":  ed.AddClipEvent(0, ""),
	} {
		if err == nil {
			t.Fatalf("%s: no error", name)
		}
	}
	// A bool key holds its value until the next one.
	tr, k, err := ed.KeyProperty("", "physics.solid", 0)
	if err != nil {
		t.Fatal(err)
	}
	if clip := ed.Clip(); clip.Tracks[tr].Keys[k].Interp != resource.InterpStep || clip.Tracks[tr].Keys[k].Value != 1 {
		t.Fatalf("bool key = %+v, want value 1 with step", clip.Tracks[tr].Keys[k])
	}
	// Edits are refused while the scene plays.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return editor.EditorView(cx, ed) }, nsgotest.Size(1120, 900))
	app.Post(func() {
		if err := ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
		if _, _, err := ed.KeyProperty("", "x", 0); !errors.Is(err, editor.ErrPlaying) {
			t.Errorf("KeyProperty while playing: %v", err)
		}
		if err := ed.PlayClip(); !errors.Is(err, editor.ErrPlaying) {
			t.Errorf("PlayClip while playing: %v", err)
		}
		_ = ed.Stop()
	})
	app.Settle()
}

func second(_, _ int, err error) error { return err }

func keyThen(ed *editor.Editor, fn func() error) error {
	if _, _, err := ed.KeyProperty("", "x", 500*time.Millisecond); err != nil {
		return nil
	}
	return fn()
}

func keyLayerThen(ed *editor.Editor, fn func(tr, k int) error) error {
	if _, _, err := ed.KeyProperty("", "physics.layer", 0); err != nil {
		return nil
	}
	tr, k, err := ed.KeyProperty("", "physics.layer", 500*time.Millisecond)
	if err != nil {
		return nil
	}
	return fn(tr, k)
}

func TestClipFilesOpenAndSave(t *testing.T) {
	ed := animatedEditor(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "hop.animation.yaml")
	if err := ed.SaveClipFile(path); err != nil {
		t.Fatalf("SaveClipFile: %v", err)
	}
	if ed.ClipPath() != path {
		t.Fatalf("ClipPath = %q", ed.ClipPath())
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.CloseClip(); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenClipFile(path, "hero"); err != nil {
		t.Fatalf("OpenClipFile: %v", err)
	}
	if err := ed.SaveClipFile(path); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(path); !bytes.Equal(again, saved) {
		t.Fatalf("open and save changed the clip file:\n%s\n---\n%s", saved, again)
	}

	bad := filepath.Join(dir, "bad.animation.yaml")
	if err := os.WriteFile(bad, bytes.Replace(saved, []byte("physics.gravity"), []byte("physics.gravty"), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenClipFile(bad, "hero"); err == nil || !strings.HasPrefix(err.Error(), bad+": ") || !strings.Contains(err.Error(), `no field "gravty"`) {
		t.Fatalf("OpenClipFile of a misspelled property: %v", err)
	}
	if ed.ClipPath() != path {
		t.Fatalf("a refused open changed ClipPath to %q", ed.ClipPath())
	}
	unknown := filepath.Join(dir, "unknown.animation.yaml")
	if err := os.WriteFile(unknown, append(append([]byte{}, saved...), "speed: 2\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenClipFile(unknown, "hero"); err == nil || !strings.Contains(err.Error(), "speed") {
		t.Fatalf("OpenClipFile with an unknown field: %v", err)
	}
}

func TestClipPreviewPlaysStepByStep(t *testing.T) {
	ed := animatedEditor(t)
	ed.SeekClip(5 * time.Second)
	if ed.ClipTime() != 2*time.Second {
		t.Fatalf("SeekClip past the end put the playhead at %v", ed.ClipTime())
	}
	if err := ed.PlayClip(); err != nil {
		t.Fatalf("PlayClip: %v", err)
	}
	if ed.ClipTime() != 0 || !ed.ClipPlaying() {
		t.Fatalf("PlayClip at the end of a clip that does not loop: at %v playing %t", ed.ClipTime(), ed.ClipPlaying())
	}
	for range 70 {
		ed.StepClip(time.Second / 60)
	}
	if ed.Status != "Clip event: land" {
		t.Fatalf("Status after crossing the event = %q", ed.Status)
	}
	for range 60 {
		ed.StepClip(time.Second / 60)
	}
	if ed.ClipPlaying() || ed.ClipTime() != 2*time.Second {
		t.Fatalf("after the end: playing %t at %v", ed.ClipPlaying(), ed.ClipTime())
	}
	if got := ed.ActiveScene().Object("hero").Sprite.X; got != 60 {
		t.Fatalf("hero at the end = %v, want 60", got)
	}

	// A scene edit rebuilds the preview at the same playhead.
	ed.SeekClip(time.Second)
	if err := ed.MoveObject("slime", 10, 10); err != nil {
		t.Fatal(err)
	}
	if ed.ClipTime() != time.Second || ed.ActiveScene().Object("slime").Doc.X != 10 || ed.ActiveScene().Object("hero").Doc.X != 100 {
		t.Fatalf("after a scene edit the preview is at %v with slime x %v and hero x %v", ed.ClipTime(), ed.ActiveScene().Object("slime").Doc.X, ed.ActiveScene().Object("hero").Doc.X)
	}

	// Deleting an object a track drives leaves the clip open and says why it cannot preview.
	if err := ed.DeleteObject("slime"); err != nil {
		t.Fatal(err)
	}
	if err := ed.ClipError(); err == nil || !strings.Contains(err.Error(), `"slime"`) {
		t.Fatalf("ClipError after deleting the slime = %v", err)
	}
	if ed.ActiveScene() != ed.Built {
		t.Fatal("a clip that cannot preview still replaces the edited scene")
	}
	if err := ed.PlayClip(); err == nil {
		t.Fatal("PlayClip of a clip that cannot preview succeeded")
	}
	ed.Undo()
	if err := ed.ClipError(); err != nil {
		t.Fatalf("ClipError after undoing the delete = %v", err)
	}
}

func TestAnimationPanelKeysAndEditsThroughTheUI(t *testing.T) {
	ed := newTestEditor(t)
	if err := ed.SetComponentRegistry(physicsComponents(t)); err != nil {
		t.Fatal(err)
	}
	ed.SelectObject("hero")
	var savedClip []byte
	ed.OnSaveClip = func(data []byte) error {
		savedClip = append([]byte(nil), data...)
		return nil
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return editor.EditorView(cx, ed) }, nsgotest.Size(1280, 900))
	if app.Exists("id:anim-panel") {
		t.Fatal("the animation panel shows with no clip open")
	}

	app.Press("id:toolbar-animate")
	if ed.Clip() == nil || ed.ClipTarget() != "hero" || !app.Exists("id:anim-panel") {
		t.Fatalf("Animate opened clip %+v on %q, panel shown %t", ed.Clip(), ed.ClipTarget(), app.Exists("id:anim-panel"))
	}
	if got := app.Find("id:anim-target").Text; got != "on hero" {
		t.Fatalf("target text = %q", got)
	}

	app.Press("id:anim-property")
	app.Press("role:option x")
	app.Press("id:anim-key")
	app.Post(func() { ed.SeekClip(500 * time.Millisecond) })
	app.Settle()
	app.Press("id:anim-key")
	clip := ed.Clip()
	if len(clip.Tracks) != 1 || clip.Tracks[0].Property != "x" || len(clip.Tracks[0].Keys) != 2 || clip.Tracks[0].Keys[1].Time != 0.5 || clip.Tracks[0].Keys[1].Value != 40 {
		t.Fatalf("clip after keying x twice = %+v", clip)
	}
	if got := app.Find("id:anim-track-0-label").Text; got != "x" {
		t.Fatalf("track label = %q", got)
	}
	if got := app.Find("id:anim-time").Text; got != "0.50 / 1.00 s" {
		t.Fatalf("time readout = %q", got)
	}

	app.Press("id:anim-key-0-1")
	if tr, k, ok := ed.SelectedClipKey(); !ok || tr != 0 || k != 1 {
		t.Fatalf("selected key = %d, %d, %t", tr, k, ok)
	}
	app.Clear("label:Key Value")
	app.Type("label:Key Value", "90")
	app.Submit("label:Key Value")
	app.Press("id:anim-interp-smooth")
	if k := ed.Clip().Tracks[0].Keys[1]; k.Value != 90 || k.Interp != resource.InterpSmooth {
		t.Fatalf("key after editing = %+v", k)
	}
	if got := ed.ActiveScene().Object("hero").Sprite.X; got != 90 {
		t.Fatalf("preview at the key = %v, want 90", got)
	}

	app.Press("id:anim-save")
	if want, _ := ed.SaveClipYAML(); !bytes.Equal(savedClip, want) || !bytes.Contains(savedClip, []byte("value: 90")) {
		t.Fatalf("OnSaveClip received:\n%s", savedClip)
	}

	app.Press("id:anim-key-delete")
	if got := len(ed.Clip().Tracks[0].Keys); got != 1 {
		t.Fatalf("keys after deleting one = %d", got)
	}
	app.Press("id:toolbar-undo")
	if got := len(ed.Clip().Tracks[0].Keys); got != 2 {
		t.Fatalf("keys after undoing the delete = %d", got)
	}
	app.Press("id:anim-track-0-remove")
	if len(ed.Clip().Tracks) != 0 || app.Exists("id:anim-track-0-label") {
		t.Fatal("the track is still there after Remove")
	}

	app.Press("id:anim-close")
	if ed.Clip() != nil || app.Exists("id:anim-panel") {
		t.Fatal("Close left the clip open")
	}
}
