package editor_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/animclip"
	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func knightSpriteAtlas(t *testing.T) *sprite.Atlas {
	t.Helper()
	names := []string{
		"idle_0", "idle_1", "idle_2", "idle_3",
		"run_0", "run_1", "run_2", "run_3", "run_4", "run_5",
		"jump_0", "jump_1",
		"fall_0", "fall_1",
		"atk_0", "atk_1", "atk_2", "atk_3",
		"hurt_0", "hurt_1",
	}
	img := image.NewRGBA(image.Rect(0, 0, len(names)*16, 16))
	palette := []color.RGBA{
		{R: 0xD7, G: 0xDE, B: 0xEA, A: 0xFF}, // idle silver
		{R: 0x3B, G: 0x7B, B: 0xFF, A: 0xFF}, // run accent
		{R: 0x4F, G: 0xD3, B: 0x8A, A: 0xFF}, // jump green
		{R: 0xE9, G: 0xA8, B: 0x4A, A: 0xFF}, // fall gold
		{R: 0xF2, G: 0xC1, B: 0x4E, A: 0xFF}, // attack yellow
		{R: 0xFF, G: 0x6B, B: 0x6B, A: 0xFF}, // hurt red
	}
	frames := make(map[string]sprite.Frame, len(names))
	for i, name := range names {
		col := palette[0]
		switch {
		case strings.HasPrefix(name, "run_"):
			col = palette[1]
		case strings.HasPrefix(name, "jump_"):
			col = palette[2]
		case strings.HasPrefix(name, "fall_"):
			col = palette[3]
		case strings.HasPrefix(name, "atk_"):
			col = palette[4]
		case strings.HasPrefix(name, "hurt_"):
			col = palette[5]
		}
		for y := 2; y < 14; y++ {
			for x := i*16 + 3; x < i*16+13; x++ {
				img.SetRGBA(x, y, col)
			}
		}
		frames[name] = sprite.Frame{Name: name, X: float32(i * 16), Y: 0, W: 16, H: 16}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return &sprite.Atlas{
		Image:  core.ImageSource{Data: buf.Bytes(), Size: core.Size{Width: core.Points(len(names) * 16), Height: 16}},
		Frames: frames,
	}
}

func knightClipMap(t *testing.T) animclip.ClipMap {
	t.Helper()
	mk := func(name string, frames []string, ms int, loop bool) *resource.Animation {
		c, err := animclip.FromFrames(name, frames, 1000.0/float64(ms), loop)
		if err != nil {
			t.Fatalf("FromFrames(%s): %v", name, err)
		}
		return c
	}
	return animclip.ClipMap{
		"anim/knight_idle.animation.yaml":   mk("idle", []string{"idle_0", "idle_1", "idle_2", "idle_3"}, 150, true),
		"anim/knight_run.animation.yaml":    mk("run", []string{"run_0", "run_1", "run_2", "run_3", "run_4", "run_5"}, 100, true),
		"anim/knight_jump.animation.yaml":   mk("jump", []string{"jump_0", "jump_1"}, 120, false),
		"anim/knight_fall.animation.yaml":   mk("fall", []string{"fall_0", "fall_1"}, 120, true),
		"anim/knight_attack.animation.yaml": mk("attack", []string{"atk_0", "atk_1", "atk_2", "atk_3"}, 80, false),
		"anim/knight_hurt.animation.yaml":   mk("hurt", []string{"hurt_0", "hurt_1"}, 90, false),
	}
}

const knightStatesYAML = `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/states.v1.json
version: 1
name: Knight
entry: idle
parameters:
  - name: move.x
    type: float
  - name: velocity.y
    type: float
  - name: on_floor
    type: bool
    default: true
  - name: jump
    type: trigger
  - name: attack
    type: trigger
  - name: hurt
    type: trigger
states:
  - name: idle
    clip: anim/knight_idle.animation.yaml
    x: 40
    "y": 230
  - name: run
    clip: anim/knight_run.animation.yaml
    x: 330
    "y": 230
  - name: jump
    clip: anim/knight_jump.animation.yaml
    x: 190
    "y": 70
  - name: fall
    clip: anim/knight_fall.animation.yaml
    x: 430
    "y": 70
  - name: attack
    clip: anim/knight_attack.animation.yaml
    x: 190
    "y": 400
  - name: hurt
    clip: anim/knight_hurt.animation.yaml
    x: 430
    "y": 400
transitions:
  - from: idle
    to: run
    label: moving
    when: '|move.x| > 0.1'
  - from: run
    to: idle
    label: stopped
    when: '|move.x| <= 0.1'
  - from: idle
    to: jump
    label: jump pressed, on floor
    when: jump && on_floor
  - from: run
    to: jump
    label: jump pressed, on floor
    when: jump && on_floor
  - from: jump
    to: fall
    label: falling
    when: velocity.y > 0
  - from: fall
    to: run
    label: lands
    when: on_floor
  - from: idle
    to: attack
    label: attack pressed
    when: attack
  - from: run
    to: attack
    label: attack pressed
    when: attack
  - from: attack
    to: idle
    label: clip ends
    onEnd: true
  - from: '*'
    to: hurt
    label: took damage
    when: hurt
  - from: hurt
    to: idle
    label: clip ends
    onEnd: true
`

func knightStatesFixture(t *testing.T) (*editor.Editor, *sprite.Atlas, animclip.ClipMap, []byte) {
	t.Helper()
	atlas := knightSpriteAtlas(t)
	clips := knightClipMap(t)
	statesYAML := []byte(knightStatesYAML)
	doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "Dungeon Level 01",
		VirtualWidth:    320,
		VirtualHeight:   180,
		BackgroundColor: "#15181d",
		Layers: []sceneformat.LayerDoc{{
			Name:  "actors",
			Space: sceneformat.SpaceWorld,
			Order: 0,
			Objects: []sceneformat.ObjectDoc{{
				ID:     "knight",
				Name:   "Knight",
				X:      120,
				Y:      90,
				Sprite: &sceneformat.SpriteRef{Atlas: "knight", Frame: "idle_0"},
				Components: map[string]any{
					sceneformat.AnimatorComponentName: map[string]any{
						"states":    "anim/knight.states.yaml",
						"startClip": "idle",
					},
				},
			}},
		}},
	}
	ed, err := editor.New(doc, sceneformat.AtlasMap{"knight": atlas})
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.Clips = clips
	ed.SelectObject("knight")
	return ed, atlas, clips, statesYAML
}

// #1054 acceptance: a fixture knight goes idle -> run -> jump -> fall -> run under scripted input,
// identically in the editor's preview and in the game runtime.
func TestKnightStateMachineIdleRunJumpFallRunMatchesEditorAndGame(t *testing.T) {
	ed, atlas, clips, statesYAML := knightStatesFixture(t)
	if err := ed.OpenStatesYAML(statesYAML, "knight"); err != nil {
		t.Fatalf("OpenStatesYAML: %v", err)
	}
	ed.SetStatesDriveWithKeys(true)

	// Build an identical game-side runtime using animclip.NewAnimator + animclip.InputDriver.
	sceneYAML, err := ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}
	gameDoc, err := sceneformat.Parse(sceneYAML)
	if err != nil {
		t.Fatal(err)
	}
	gameBuilt, err := sceneformat.Build(gameDoc, sceneformat.AtlasMap{"knight": atlas})
	if err != nil {
		t.Fatal(err)
	}
	parsedStates, err := animclip.ParseStateMachine(statesYAML)
	if err != nil {
		t.Fatal(err)
	}
	gameSM, err := animclip.NewAnimator(gameBuilt, "knight", animclip.StatesMap{"anim/knight.states.yaml": parsedStates}, clips, nil)
	if err != nil {
		t.Fatalf("NewAnimator: %v", err)
	}
	gameActions := input.NewActions()
	gameActions.Bind("move_right", input.Key("d"))
	gameActions.Bind("jump", input.Key("space"))
	gameDriver := animclip.NewInputDriver()

	var gameTrace []string
	gameTrace = append(gameTrace, gameSM.State())
	gameSM.OnTransition = func(_, to string) {
		gameTrace = append(gameTrace, to)
	}

	// Step both editor preview and game identically at 60 Hz:
	// - steps 0..11 (200ms): idle
	// - steps 12..95 (1.4s): hold right -> run; tap jump at step 30 -> jump -> fall -> run
	dt := time.Second / 60
	for step := range 100 {
		switch step {
		case 12:
			ed.PressStatesAction("move_right")
			gameActions.Press("move_right")
		case 30:
			ed.TriggerStatesAction("jump")
			gameActions.Trigger("jump")
		}

		ed.StepStates(dt)

		gameActions.Step()
		gameDriver.StepActions(gameSM, gameActions, dt)
		gameSM.Update(dt)

		if ed.StatesCurrentState() != gameSM.State() {
			t.Fatalf("step %d: editor state = %q, game state = %q", step, ed.StatesCurrentState(), gameSM.State())
		}
		if ed.StatesActiveFrame() != gameBuilt.Object("knight").Sprite.Frame.Name {
			t.Fatalf("step %d: editor frame = %q, game frame = %q", step, ed.StatesActiveFrame(), gameBuilt.Object("knight").Sprite.Frame.Name)
		}
	}

	want := []string{"idle", "run", "jump", "fall", "run"}
	if !slices.Equal(ed.StatesTransitionTrace(), want) {
		t.Fatalf("editor trace = %v, want %v", ed.StatesTransitionTrace(), want)
	}
	if !slices.Equal(gameTrace, want) {
		t.Fatalf("game trace = %v, want %v", gameTrace, want)
	}
}

// #1054 acceptance: editor UI shows the state graph, active state highlight, Drive with keys, LEAVING <STATE>,
// undoable edits, lossless YAML round-trip, and renders cleanly on macOS nsgotest.
func TestStatesGraphUIAndDriveWithKeysAndScreenshot(t *testing.T) {
	ed, _, _, statesYAML := knightStatesFixture(t)
	if err := ed.OpenStatesYAML(statesYAML, "knight"); err != nil {
		t.Fatalf("OpenStatesYAML: %v", err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	if !app.Exists("id:states-panel") || !app.Exists("id:states-graph-canvas") {
		t.Fatal("state machine graph panel did not render")
	}
	for _, st := range []string{"idle", "run", "jump", "fall", "attack", "hurt"} {
		if !app.Exists("id:states-node-" + st) {
			t.Fatalf("missing state node %q", st)
		}
	}
	if !app.Exists("id:states-node-any") {
		t.Fatal("missing Any state badge")
	}
	if got := app.Find("id:states-leaving-header").Text; got != "LEAVING IDLE" {
		t.Fatalf("LEAVING header = %q, want LEAVING IDLE", got)
	}

	// Click 'run' state node -> active state and LEAVING header update.
	app.Press("id:states-node-run")
	if got := ed.StatesCurrentState(); got != "run" {
		t.Fatalf("StatesCurrentState after clicking run = %q, want run", got)
	}
	if got := app.Find("id:states-leaving-header").Text; got != "LEAVING RUN" {
		t.Fatalf("LEAVING header after clicking run = %q, want LEAVING RUN", got)
	}
	if !app.Exists("id:states-leaving-0") {
		t.Fatal("missing outgoing transition row in LEAVING RUN")
	}

	// Toggle Drive with keys ON -> interactive key pills appear.
	app.Press("id:states-drive-toggle")
	if !ed.StatesDriveWithKeys() {
		t.Fatal("Drive with keys did not turn on")
	}
	for _, id := range []string{"id:states-key-move", "id:states-key-jump", "id:states-key-attack", "id:states-key-release"} {
		if !app.Exists(id) {
			t.Fatalf("missing Drive with keys pill %s", id)
		}
	}

	// Capture a real screenshot when run under `go tool nsgo test -target macos` (no-op headless).
	app.Screenshot("states-graph")

	// Undoable graph edits and lossless YAML round-trip.
	app.Post(func() {
		ed.PauseStates()
		ed.SetStatesDriveWithKeys(false)
		if err := ed.AddStatesState(animclip.StateDoc{Name: "dash", Clip: "anim/knight_run.animation.yaml"}); err != nil {
			t.Errorf("AddStatesState: %v", err)
		}
	})
	app.Settle()
	if !app.Exists("id:states-node-dash") {
		t.Fatal("added state node 'dash' did not appear")
	}
	app.Press("id:toolbar-undo")
	if app.Exists("id:states-node-dash") {
		t.Fatal("undo did not remove added state node 'dash'")
	}
	saved, err := ed.SaveStatesYAML()
	if err != nil {
		t.Fatalf("SaveStatesYAML: %v", err)
	}
	if !bytes.Equal(saved, statesYAML) {
		t.Fatalf("round-trip YAML changed after undo:\ngot:\n%s\nwant:\n%s", saved, statesYAML)
	}
}

func TestStatesDriveKeysShortcutPassthroughEscapeAndPlayModeGuards(t *testing.T) {
	ed, _, _, statesYAML := knightStatesFixture(t)
	if err := ed.OpenStatesYAML(statesYAML, "knight"); err != nil {
		t.Fatalf("OpenStatesYAML: %v", err)
	}
	if ed.StatesDirty() {
		t.Fatal("freshly opened state machine should not be dirty")
	}

	var rootScope *core.Scope
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		rootScope = cx
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 720))

	app.Post(func() {
		ed.SetStatesDriveWithKeys(true)
	})
	app.Settle()
	if !ed.StatesDriveWithKeys() {
		t.Fatal("SetStatesDriveWithKeys(true) did not activate")
	}

	// Bound key is consumed.
	var consumed bool
	app.Post(func() {
		consumed = ed.StatesHandleKeyEvent(core.KeyEvent{Key: "ArrowRight"}, true)
	})
	app.Settle()
	if !consumed {
		t.Fatal("expected bound ArrowRight key to be consumed")
	}
	if !ed.StatesActionHeld("move_right") {
		t.Fatal("expected move_right to be held after ArrowRight down")
	}

	// Unbound keys and modifier chords must NOT be swallowed.
	for _, ev := range []core.KeyEvent{
		{Key: "x"},
		{Key: "z", Modifiers: core.Modifiers{Platform: true}},
		{Key: "s", Modifiers: core.Modifiers{Control: true}},
		{Key: "a", Modifiers: core.Modifiers{Alt: true}},
	} {
		var sw bool
		app.Post(func() { sw = ed.StatesHandleKeyEvent(ev, true) })
		app.Settle()
		if sw {
			t.Fatalf("StatesHandleKeyEvent(%+v) swallowed unbound/modifier key", ev)
		}
	}

	// Escape releases held keys and exits Drive with keys.
	var esc bool
	app.Post(func() { esc = ed.StatesHandleKeyEvent(core.KeyEvent{Key: "Escape"}, true) })
	app.Settle()
	if !esc {
		t.Fatal("expected Escape to be handled in Drive with keys mode")
	}
	if ed.StatesDriveWithKeys() || ed.StatesActionHeld("move_right") {
		t.Fatalf("Escape did not exit Drive with keys or release move_right: drive=%v held=%v", ed.StatesDriveWithKeys(), ed.StatesActionHeld("move_right"))
	}

	// Play mode disables Drive with keys, ignores keys, and refuses SaveStatesFile.
	var playErr error
	var playKey bool
	var saveErr error
	outPath := filepath.Join(t.TempDir(), "x.states.yaml")
	app.Post(func() {
		ed.SetStatesDriveWithKeys(true)
		playErr = ed.PlayIn(rootScope)
		if playErr == nil {
			if ed.StatesDriveWithKeys() {
				t.Error("Play() should disable Drive with keys")
			}
			ed.SetStatesDriveWithKeys(true)
			if ed.StatesDriveWithKeys() {
				t.Error("SetStatesDriveWithKeys(true) must be ignored during Play mode")
			}
			playKey = ed.StatesHandleKeyEvent(core.KeyEvent{Key: "ArrowRight"}, true)
			saveErr = ed.SaveStatesFile(outPath)
			ed.Stop()
		}
	})
	app.Settle()
	if playErr != nil {
		t.Fatalf("PlayIn: %v", playErr)
	}
	if playKey {
		t.Fatal("StatesHandleKeyEvent must return false during Play mode")
	}
	if !errors.Is(saveErr, editor.ErrPlaying) {
		t.Fatalf("SaveStatesFile during Play = %v, want ErrPlaying", saveErr)
	}

	// Cached StatesDirty tracks edits and save accurately.
	var addErr, saveOK error
	savedPath := filepath.Join(t.TempDir(), "saved.states.yaml")
	app.Post(func() {
		addErr = ed.AddStatesState(animclip.StateDoc{Name: "dash", Clip: "anim/knight_run.animation.yaml"})
	})
	app.Settle()
	if addErr != nil {
		t.Fatalf("AddStatesState: %v", addErr)
	}
	if !ed.StatesDirty() {
		t.Fatal("expected StatesDirty() == true after edit")
	}
	app.Post(func() {
		saveOK = ed.SaveStatesFile(savedPath)
	})
	app.Settle()
	if saveOK != nil {
		t.Fatalf("SaveStatesFile: %v", saveOK)
	}
	if ed.StatesDirty() {
		t.Fatal("expected StatesDirty() == false after SaveStatesFile")
	}
}
