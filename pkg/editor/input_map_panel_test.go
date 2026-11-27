package editor_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// #908 Acceptance 2: add an action in the Input Map panel, bind it by capturing a key, save, and read the binding back from game.yaml, then undo/redo cleanly.
func TestInputMapPanelAddActionCaptureKeySaveAndUndo(t *testing.T) {
	dir := writeEditorProject(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 900), nsgotest.Environment(core.Environment{OS: core.OSWindows, Idiom: core.IdiomDesktop}))

	if app.Exists("id:input-map-panel") {
		t.Fatal("input map panel should start closed")
	}

	// Open the Input Map panel from the toolbar.
	runEditorCommand(t, app, ed, editor.CmdInputMap)
	app.Settle()
	if !app.Exists("id:input-map-panel") {
		t.Fatal("pressing toolbar-input-map did not open the panel")
	}

	// Add action "jump" in the panel.
	app.Type("id:input-new-action-name", "jump")
	app.Press("id:input-add-action")
	app.Settle()
	if acts := ed.InputActions(); len(acts) != 1 || acts[0].Name != "jump" {
		t.Fatalf("InputActions after Add = %+v, want [jump]", acts)
	}

	// Enter "press a key" capture mode and press "space".
	app.Press("id:input-capture-key")
	app.Settle()
	if !ed.CapturingKey() || !app.Exists("id:input-key-capture-target") {
		t.Fatal("pressing input-capture-key did not enter key capture mode")
	}
	app.KeyDown("space")
	app.KeyUp("space")
	app.Settle()
	if ed.CapturingKey() {
		t.Fatal("key capture stayed active after pressing space")
	}
	acts := ed.InputActions()
	if len(acts) != 1 || len(acts[0].Bindings) != 1 || acts[0].Bindings[0] != (project.Binding{Kind: project.KindKey, Key: "space"}) {
		t.Fatalf("bindings after key capture = %+v, want key space", acts)
	}

	// Save via toolbar Save and read the binding back from game.yaml on disk.
	saveInUI(app, ed)
	app.Settle()
	saved, err := project.Check(os.DirFS(dir))
	if err != nil {
		t.Fatalf("project.Check after Save: %v", err)
	}
	if saved.Input == nil || len(saved.Input.Actions) != 1 {
		t.Fatalf("saved game.yaml input = %+v, want 1 action", saved.Input)
	}
	wantAction := project.Action{
		Name:     "jump",
		Bindings: []project.Binding{{Kind: project.KindKey, Key: "space"}},
	}
	if !reflect.DeepEqual(saved.Input.Actions[0], wantAction) {
		t.Fatalf("saved action = %+v, want %+v", saved.Input.Actions[0], wantAction)
	}

	// Verify the saved game.yaml binds into input.Actions properly.
	bound := input.NewActions()
	saved.BindInput(bound)
	if got := bound.Bindings("jump"); !reflect.DeepEqual(got, []input.Input{input.Key("space")}) {
		t.Fatalf("bound jump = %+v, want [Key(space)]", got)
	}

	// Undo removes the binding, then removes the action (and prunes empty input section).
	undoInUI(app, ed)
	app.Settle()
	if got := ed.InputActions(); len(got) != 1 || len(got[0].Bindings) != 0 {
		t.Fatalf("after 1st undo: %+v, want jump with 0 bindings", got)
	}
	undoInUI(app, ed)
	app.Settle()
	if ed.Project.Input != nil {
		t.Fatalf("after 2nd undo: Project.Input = %+v, want nil", ed.Project.Input)
	}

	// Redo brings both the action and the captured key binding back.
	redoInUI(app, ed)
	redoInUI(app, ed)
	app.Settle()
	if got := ed.InputActions(); len(got) != 1 || len(got[0].Bindings) != 1 || got[0].Bindings[0].Key != "space" {
		t.Fatalf("after Redo: %+v, want jump bound to space", got)
	}
}

func TestInputMapPanelRenameActionUpdatesAxesAndUndoRedo(t *testing.T) {
	dir := writeEditorProject(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}

	// Without a project, edits return ErrNoProject.
	if err := ed.AddInputAction("left"); !errors.Is(err, editor.ErrNoProject) {
		t.Fatalf("AddInputAction without project = %v, want ErrNoProject", err)
	}

	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	// Add left, right actions and a move axis reading them.
	if err := ed.AddInputAction("left"); err != nil {
		t.Fatal(err)
	}
	if err := ed.AddInputAction("right"); err != nil {
		t.Fatal(err)
	}
	if err := ed.AddInputAxis(project.Axis{Name: "move", Negative: "left", Positive: "right"}); err != nil {
		t.Fatal(err)
	}

	// Deleting an action still referenced by an axis is refused without mutating state.
	if err := ed.DeleteInputAction("left"); err == nil {
		t.Fatal("DeleteInputAction succeeded while axis still referenced it")
	}

	// Renaming "left" to "strafeLeft" updates the "move" axis in the same undoable step.
	if err := ed.RenameInputAction("left", "strafeLeft"); err != nil {
		t.Fatalf("RenameInputAction: %v", err)
	}
	axes := ed.InputAxes()
	if len(axes) != 1 || axes[0].Negative != "strafeLeft" || axes[0].Positive != "right" {
		t.Fatalf("axes after rename = %+v, want negative=strafeLeft positive=right", axes)
	}
	// Duplicate or invalid renames are refused and leave axes untouched.
	if err := ed.RenameInputAction("strafeLeft", "right"); err == nil {
		t.Fatal("RenameInputAction duplicate succeeded")
	}
	if err := ed.RenameInputAction("strafeLeft", "bad name"); err == nil {
		t.Fatal("RenameInputAction with space succeeded")
	}
	if got := ed.InputAxes()[0].Negative; got != "strafeLeft" {
		t.Fatalf("axis after failed rename = %q, want strafeLeft", got)
	}

	// One Undo restores both the action name and the axis reference together.
	if !ed.Undo() {
		t.Fatal("Undo returned false")
	}
	if acts := ed.InputActions(); acts[0].Name != "left" {
		t.Fatalf("action 0 after Undo = %q, want left", acts[0].Name)
	}
	if axes := ed.InputAxes(); axes[0].Negative != "left" {
		t.Fatalf("axis negative after Undo = %q, want left", axes[0].Negative)
	}

	// Redo re-applies both atomically.
	if !ed.Redo() {
		t.Fatal("Redo returned false")
	}
	if acts := ed.InputActions(); acts[0].Name != "strafeLeft" {
		t.Fatalf("action 0 after Redo = %q, want strafeLeft", acts[0].Name)
	}
	if axes := ed.InputAxes(); axes[0].Negative != "strafeLeft" {
		t.Fatalf("axis negative after Redo = %q, want strafeLeft", axes[0].Negative)
	}

	// Save and reload verifies the renamed action + axis round-trip on disk.
	if err := ed.SaveProject(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, project.FileName))
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := project.Parse(raw)
	if err != nil {
		t.Fatalf("Parse saved game.yaml: %v", err)
	}
	if reloaded.Input.Axes[0].Negative != "strafeLeft" {
		t.Fatalf("reloaded axis negative = %q, want strafeLeft", reloaded.Input.Axes[0].Negative)
	}
}

func TestInputMapPanelRegionsMotionAndGamepadAxes(t *testing.T) {
	dir := writeEditorProject(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	// Configure virtual display size 320x180.
	if err := ed.EditProject("Set Display", func(f *project.File) {
		f.Display = &project.Display{Width: 320, Height: 180}
	}); err != nil {
		t.Fatal(err)
	}
	if w, h := ed.InputMapDisplaySize(); w != 320 || h != 180 {
		t.Fatalf("InputMapDisplaySize = %v×%v, want 320×180", w, h)
	}

	// Add an action and draw a touchRegion by dragging on empty space over the 320x180 display.
	if err := ed.AddInputAction("shoot"); err != nil {
		t.Fatal(err)
	}
	ed.BeginRegionDrag(20, 100)
	ed.UpdateRegionDrag(100, 160)
	if err := ed.EndRegionDrag(); err != nil {
		t.Fatalf("EndRegionDrag create: %v", err)
	}
	acts := ed.InputActions()
	if len(acts[0].Bindings) != 1 || acts[0].Bindings[0].Kind != project.KindTouchRegion {
		t.Fatalf("bindings after draw = %+v", acts[0].Bindings)
	}
	wantRect := project.Rect{X: 20, Y: 100, Width: 80, Height: 60}
	if *acts[0].Bindings[0].Region != wantRect {
		t.Fatalf("drawn region = %+v, want %+v", *acts[0].Bindings[0].Region, wantRect)
	}

	// Drag inside the region to move it by (+30, -10).
	ed.BeginRegionDrag(50, 120)
	ed.UpdateRegionDrag(80, 110)
	if err := ed.EndRegionDrag(); err != nil {
		t.Fatalf("EndRegionDrag move: %v", err)
	}
	wantMoved := project.Rect{X: 50, Y: 90, Width: 80, Height: 60}
	if got := *ed.InputActions()[0].Bindings[0].Region; got != wantMoved {
		t.Fatalf("moved region = %+v, want %+v", got, wantMoved)
	}

	// Drag its bottom-right handle (130, 150) by (+20, +10) to resize it.
	ed.BeginRegionDrag(130, 150)
	ed.UpdateRegionDrag(150, 160)
	if err := ed.EndRegionDrag(); err != nil {
		t.Fatalf("EndRegionDrag resize: %v", err)
	}
	wantResized := project.Rect{X: 50, Y: 90, Width: 100, Height: 70}
	if got := *ed.InputActions()[0].Bindings[0].Region; got != wantResized {
		t.Fatalf("resized region = %+v, want %+v", got, wantResized)
	}

	// Undo the resize in a single step.
	if !ed.Undo() {
		t.Fatal("Undo resize failed")
	}
	if got := *ed.InputActions()[0].Bindings[0].Region; got != wantMoved {
		t.Fatalf("region after undo resize = %+v, want %+v", got, wantMoved)
	}

	// Draw a mouseRegion by switching draw kind.
	ed.SetInputMapRegionDrawKind(project.KindMouseRegion)
	ed.BeginRegionDrag(200, 10)
	ed.UpdateRegionDrag(280, 50)
	if err := ed.EndRegionDrag(); err != nil {
		t.Fatalf("EndRegionDrag mouseRegion: %v", err)
	}
	if got := ed.InputActions()[0].Bindings[1]; got.Kind != project.KindMouseRegion || got.Button != "left" || *got.Region != (project.Rect{X: 200, Y: 10, Width: 80, Height: 40}) {
		t.Fatalf("mouseRegion binding = %+v", got)
	}

	// Add gamepadButton, keyCode, motionAxis and gamepadAxis, and verify BuildInputActions binds all of them.
	if _, err := ed.AddInputBinding("shoot", project.Binding{Kind: project.KindGamepadButton, Button: "rightTrigger"}); err != nil {
		t.Fatal(err)
	}
	if err := ed.BeginKeyCapture("shoot", project.KindKeyCode); err != nil {
		t.Fatal(err)
	}
	if ok, err := ed.CaptureKeyEvent(core.KeyEvent{Key: "f", Code: "KeyF"}); !ok || err != nil {
		t.Fatalf("CaptureKeyEvent KeyCode = %v, %v", ok, err)
	}
	if err := ed.AddInputMotionAxis(project.MotionAxis{Name: "steer", Axis: "tiltX", DeadZone: 0.1, Invert: true}); err != nil {
		t.Fatal(err)
	}
	if err := ed.AddInputGamepadAxis(project.GamepadAxis{Name: "aimX", Axis: "rightX", DeadZone: 0.2, Axial: true}); err != nil {
		t.Fatal(err)
	}

	live := ed.BuildInputActions()
	if binds := live.Bindings("shoot"); len(binds) != 4 {
		t.Fatalf("live shoot bindings = %+v, want 4", binds)
	}
	if binds := live.Bindings("shoot"); binds[2] != input.GamepadButton(gamepad.ButtonRT) || binds[3] != input.KeyCode("KeyF") {
		t.Fatalf("live gamepad/keyCode bindings = %+v", binds[2:])
	}
	if ax, opts, ok := live.MotionAxisBinding("steer"); !ok || ax != input.TiltX || !opts.Invert {
		t.Fatalf("live motion axis = %v, %+v, %v", ax, opts, ok)
	}
	if ax, opts, ok := live.GamepadAxisBinding("aimX"); !ok || ax != gamepad.AxisRightX || !opts.Axial {
		t.Fatalf("live gamepad axis = %v, %+v, %v", ax, opts, ok)
	}

	// Render both tabs of the Input Map panel in a live nsgotest app to verify UI wiring for Axes and Region canvas.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 900))
	runEditorCommand(t, app, ed, editor.CmdInputMap)
	app.Settle()
	if !app.Exists("id:input-region-canvas") {
		t.Fatal("input-region-canvas not rendered")
	}
	app.Press("id:input-tab-axes")
	app.Settle()
	if !app.Exists("id:input-motion-0") || !app.Exists("id:input-gamepad-axis-0") {
		t.Fatal("motion or gamepad axis rows not rendered in Axes tab")
	}
}
