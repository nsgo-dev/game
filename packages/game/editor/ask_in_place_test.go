package editor_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func newAskInPlaceProjectEditor(t *testing.T) *editor.Editor {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	gameYAML := []byte(`version: 1
name: dungeon-run
id: com.example.dungeonrun
appVersion: 0.1.0
window:
  title: Dungeon Run
  width: 640
  height: 360
autoload:
  mainScene: level_01
  scenes:
    - name: level_01
      path: scenes/level_01.scene.yaml
`)
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), gameYAML, 0o644); err != nil {
		t.Fatal(err)
	}
	sceneYAML := []byte(`version: 4
name: level_01
virtualWidth: 640
virtualHeight: 360
backgroundColor: "#141824"
layers:
  - name: actors
    space: world
    objects:
      - id: knight
        name: Knight
        x: 120
        y: 220
        sprite:
          atlas: actors
          frame: hero
      - id: slime_01
        name: slime_01
        prefab: prefabs/slime.prefab.yaml
        x: 240
        y: 220
        sprite:
          atlas: actors
          frame: slime
      - id: slime_02
        name: slime_02
        prefab: prefabs/slime.prefab.yaml
        x: 320
        y: 220
        sprite:
          atlas: actors
          frame: slime
      - id: edge_slime
        name: edge_slime
        x: 620
        y: 340
        sprite:
          atlas: actors
          frame: slime
`)
	if err := os.WriteFile(filepath.Join(dir, "scenes", "level_01.scene.yaml"), sceneYAML, 0o644); err != nil {
		t.Fatal(err)
	}

	sheet := image.NewRGBA(image.Rect(0, 0, 48, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 24; x++ {
			sheet.SetRGBA(x, y, color.RGBA{R: 0x3b, G: 0x7b, B: 0xff, A: 0xff})
		}
		for x := 24; x < 48; x++ {
			sheet.SetRGBA(x, y, color.RGBA{R: 0x4f, G: 0xd3, B: 0x8a, A: 0xff})
		}
	}
	actors := &sprite.Atlas{
		Image: core.ImageSource{Pixels: sheet},
		Frames: map[string]sprite.Frame{
			"hero":  {Name: "hero", X: 0, Y: 0, W: 24, H: 24, Width: 24, Height: 24},
			"slime": {Name: "slime", X: 24, Y: 0, W: 24, H: 24, Width: 24, Height: 24},
		},
	}

	ed, err := editor.New(nil, sceneformat.AtlasMap{"actors": actors})
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	ed.WaitForProblems()
	return ed
}

// #1037: Selecting two objects (slime_01, slime_02), pressing ⌘L, typing a request (including ⇧↵ newline)
// and sending with ↵ starts an agent turn flagged SkipPlan=true with object IDs, scene path/YAML, and a
// valid rendered viewport PNG, and displays the turn in the Agent panel's thread.
func TestAskInPlaceSelectTwoObjectsSendAndShowsInAgentPanel(t *testing.T) {
	ed := newAskInPlaceProjectEditor(t)
	var received []editor.AgentTurn
	ed.OnAgentTurn = func(turn editor.AgentTurn) {
		received = append(received, turn)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true, Locale: "en-US"}))

	app.Post(func() {
		ed.SelectObjects([]string{"slime_01", "slime_02"})
	})
	app.Settle()

	// Press ⌘L with focus outside a text field.
	app.Key("meta+l")
	if !ed.AskInPlaceOpen() {
		t.Fatal("meta+l did not open the in-place composer")
	}
	if !app.Exists("id:ask-in-place-composer") {
		t.Fatal("ask-in-place-composer node not rendered")
	}

	// Verify announced selection chip and real labelled multi-line input.
	chip := app.Find("id:ask-in-place-selection-chip")
	if chip.Props.Role != core.RoleStatus || chip.Props.LiveRegion != core.LiveRegionPolite {
		t.Fatalf("selection chip role/live = (%v, %v), want (Status, Polite)", chip.Props.Role, chip.Props.LiveRegion)
	}
	if !strings.Contains(chip.Props.Label, "2 selected") || !strings.Contains(chip.Props.Label, "slime_01") || !strings.Contains(chip.Props.Label, "slime_02") {
		t.Fatalf("selection chip announcement label = %q, want count + slime_01 + slime_02", chip.Props.Label)
	}
	chipText := allText(chip)
	if !strings.Contains(chipText, "2 selected") || !strings.Contains(chipText, "slime_01") || !strings.Contains(chipText, "slime_02") {
		t.Fatalf("selection chip text = %q, want 2 selected + slime_01 + slime_02", chipText)
	}

	input := app.Find("id:ask-in-place-input")
	if input.Props.Label == "" {
		t.Fatal("ask-in-place-input must have an accessible label")
	}
	if !input.Focused {
		t.Fatal("ask-in-place-input should take keyboard focus on open")
	}

	// Verify contextual suggestions (up to 2) and hint line.
	if !app.Exists("id:ask-in-place-suggestion-0") || !app.Exists("id:ask-in-place-suggestion-1") || app.Exists("id:ask-in-place-suggestion-2") {
		t.Fatal("expected exactly 2 contextual suggestions for multi-select")
	}
	hintText := allText(app.Find("id:ask-in-place-hint"))
	if !strings.Contains(hintText, "⌘L") || !strings.Contains(hintText, "⌘K") || !strings.Contains(hintText, "Ask the agent") {
		t.Fatalf("hint text = %q, want ⌘L / ⌘K / Ask the agent", hintText)
	}

	// Multi-line input: ⇧↵ inserts newline without sending.
	app.Type("id:ask-in-place-input", "make these two hop instead of walk")
	app.Key("shift+enter")
	if !ed.AskInPlaceOpen() {
		t.Fatal("shift+enter unexpectedly submitted/closed the composer")
	}
	if !strings.HasSuffix(ed.AskInPlaceSnapshot().Draft, "\n") {
		t.Fatalf("shift+enter did not insert newline: draft=%q", ed.AskInPlaceSnapshot().Draft)
	}

	// Replace with single-line prompt and send with ↵.
	app.Clear("id:ask-in-place-input")
	app.Type("id:ask-in-place-input", "make these two hop instead of walk, and a bit faster")
	app.Key("enter")

	if ed.AskInPlaceOpen() {
		t.Fatal("composer stayed open after Enter send")
	}
	if len(received) != 1 {
		t.Fatalf("OnAgentTurn count = %d, want 1", len(received))
	}
	turn := received[0]
	if turn.Prompt != "make these two hop instead of walk, and a bit faster" {
		t.Fatalf("turn.Prompt = %q", turn.Prompt)
	}
	if turn.Source != "in_place" || !turn.SkipPlan {
		t.Fatalf("turn source/skipPlan = (%q, %v), want (in_place, true)", turn.Source, turn.SkipPlan)
	}
	if !slices.Equal(turn.Context.SelectionIDs, []string{"slime_01", "slime_02"}) {
		t.Fatalf("turn SelectionIDs = %v, want [slime_01 slime_02]", turn.Context.SelectionIDs)
	}
	if turn.Context.ScenePath != "scenes/level_01.scene.yaml" {
		t.Fatalf("turn ScenePath = %q, want scenes/level_01.scene.yaml", turn.Context.ScenePath)
	}
	if !strings.Contains(turn.Context.SceneYAML, "id: slime_01") || !strings.Contains(turn.Context.SceneYAML, "id: slime_02") {
		t.Fatalf("turn SceneYAML missing selected objects:\n%s", turn.Context.SceneYAML)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(turn.Context.ViewportPNG))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		t.Fatalf("turn ViewportPNG invalid: err=%v cfg=%+v len=%d", err, cfg, len(turn.Context.ViewportPNG))
	}

	// Verify the turn shows in the Agent panel's thread.
	if ed.RightPanelTab() != editor.RightPanelAgent {
		t.Fatalf("RightPanelTab = %v, want RightPanelAgent", ed.RightPanelTab())
	}
	if !app.Exists("id:editor-agent-panel") || !app.Exists("id:agent-turn-"+turn.ID) {
		t.Fatalf("Agent panel or turn %s not rendered", turn.ID)
	}
	panelText := allText(app.Find("id:agent-turn-" + turn.ID))
	if !strings.Contains(panelText, "make these two hop instead of walk, and a bit faster") || !strings.Contains(panelText, "slime_01") || !strings.Contains(panelText, "slime_02") {
		t.Fatalf("Agent panel turn text = %q, want prompt + slime_01 + slime_02", panelText)
	}
	if !app.Exists("id:agent-turn-skip-plan-" + turn.ID) {
		t.Fatal("Agent panel turn missing skip-plan indicator")
	}

	// Verify ⌘↵ also sends a second in-place request.
	app.Key("meta+l")
	app.Type("id:ask-in-place-input", "tint them green")
	app.Key("meta+enter")
	if ed.AskInPlaceOpen() {
		t.Fatal("meta+enter did not send and close composer")
	}
	if len(ed.AgentTurns()) != 2 {
		t.Fatalf("AgentTurns count = %d, want 2", len(ed.AgentTurns()))
	}
}

// #1037: ⌘L while typing in the inspector does NOT open the composer;
// Esc closes the composer without sending and returns focus to where it was.
func TestAskInPlaceInspectorTextFieldGuardAndEscapeRestoresFocus(t *testing.T) {
	ed := newAskInPlaceProjectEditor(t)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true, Locale: "en-US"}))

	app.Post(func() {
		ed.SelectObject("slime_01")
	})
	app.Settle()

	// 1. Focus inspector-name (a text field in the Inspector) and press ⌘L -> must NOT open composer.
	app.Focus("id:inspector-name")
	if !app.Find("id:inspector-name").Focused {
		t.Fatal("inspector-name did not gain focus")
	}
	app.Key("meta+l")
	if ed.AskInPlaceOpen() || app.Exists("id:ask-in-place-composer") {
		t.Fatal("meta+l opened the in-place composer while focus was in inspector-name")
	}

	// 2. Focus a non-text control (toolbar-editor-settings), press ⌘L -> opens composer;
	// type text, press Esc -> closes WITHOUT sending, keeps selection, and restores focus!
	app.Focus("id:toolbar-editor-settings")
	if !app.Find("id:toolbar-editor-settings").Focused {
		t.Fatal("toolbar-editor-settings did not gain focus")
	}
	app.Key("meta+l")
	if !ed.AskInPlaceOpen() || !app.Find("id:ask-in-place-input").Focused {
		t.Fatal("meta+l did not open and focus ask-in-place-input")
	}
	app.Type("id:ask-in-place-input", "never send this")
	app.Key("escape")

	if ed.AskInPlaceOpen() || app.Exists("id:ask-in-place-composer") {
		t.Fatal("Esc did not close the in-place composer")
	}
	if len(ed.AgentTurns()) != 0 {
		t.Fatalf("Esc sent a turn unexpectedly: %+v", ed.AgentTurns())
	}
	if ed.SelectedID != "slime_01" {
		t.Fatalf("Esc cleared selection: SelectedID=%q, want slime_01", ed.SelectedID)
	}
	if !app.Find("id:toolbar-editor-settings").Focused {
		t.Fatal("Esc did not return focus to the previously focused control (toolbar-editor-settings)")
	}
}

// #1037: No selection opens asking about the open scene; context menu in both viewport and scene tree
// opens the same composer; arrows + ↵ pick suggestions without blocking typing; clamping keeps the
// composer on screen and never overlapping the right panel.
func TestAskInPlaceNoSelectionContextMenuSuggestionsAndClamping(t *testing.T) {
	ed := newAskInPlaceProjectEditor(t)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true, Locale: "en-US"}))

	// 1. Without a selection, ⌘L opens anchored to the viewport asking about the open scene.
	app.Post(func() { ed.ClearSelection() })
	app.Settle()
	app.Key("meta+l")
	if !ed.AskInPlaceOpen() {
		t.Fatal("meta+l with no selection did not open scene-level composer")
	}
	snap := ed.AskInPlaceSnapshot()
	if len(snap.TargetIDs) != 0 || !strings.Contains(snap.ChipSummary, "level_01") {
		t.Fatalf("scene-level snapshot = %+v, want empty targets and scene name level_01", snap)
	}
	if len(snap.Suggestions) != 2 {
		t.Fatalf("scene-level suggestions = %d, want 2", len(snap.Suggestions))
	}
	app.Key("escape")
	if ed.AskInPlaceOpen() {
		t.Fatal("Esc did not close scene-level composer")
	}

	// 2. Context menu "Ask the agent about this" from viewport opens the same composer for multi-selection.
	app.Post(func() { ed.SelectObjects([]string{"slime_01", "slime_02"}) })
	app.Settle()
	app.Key("shift+f10")
	app.Press("id:context-menu-ask-agent")
	if !ed.AskInPlaceOpen() {
		t.Fatal("viewport context menu 'Ask the agent about this' did not open in-place composer")
	}
	if got := ed.AskInPlaceSnapshot().TargetIDs; !slices.Equal(got, []string{"slime_01", "slime_02"}) {
		t.Fatalf("viewport context menu composer targets = %v, want [slime_01 slime_02]", got)
	}

	// 3. Arrow keys navigate suggestions without blocking free typing, and Enter picks highlighted suggestion.
	app.Key("down")
	if got := ed.AskInPlaceSelectedSuggestion(); got != 0 {
		t.Fatalf("after 1st Down, SelectedSuggestion = %d, want 0", got)
	}
	app.Key("down")
	if got := ed.AskInPlaceSelectedSuggestion(); got != 1 {
		t.Fatalf("after 2nd Down, SelectedSuggestion = %d, want 1", got)
	}
	// Typing clears the highlight and never blocks free typing.
	app.Type("id:ask-in-place-input", "free typing")
	if got := ed.AskInPlaceSelectedSuggestion(); got != -1 {
		t.Fatalf("typing did not clear SelectedSuggestion: got %d, want -1", got)
	}
	if ed.AskInPlaceSnapshot().Draft != "free typing" {
		t.Fatalf("draft = %q, want 'free typing'", ed.AskInPlaceSnapshot().Draft)
	}
	app.Clear("id:ask-in-place-input")
	app.Key("down")
	app.Key("down")
	if got := ed.AskInPlaceSelectedSuggestion(); got != 1 {
		t.Fatalf("SelectedSuggestion before Enter = %d, want 1", got)
	}
	app.Key("enter")
	if ed.AskInPlaceOpen() {
		t.Fatal("Enter on highlighted suggestion did not send and close")
	}
	last, ok := ed.LatestAgentTurn()
	if !ok || last.Prompt != "Space them evenly" {
		t.Fatalf("picked suggestion turn = (%+v, %v), want 'Space them evenly'", last, ok)
	}

	// 4. Context menu in Scene Tree also opens the same in-place composer.
	app.Post(func() { ed.SelectObject("knight") })
	app.Settle()
	app.RightClick("id:scene-tree-row-knight")
	app.Press("id:context-menu-ask-agent")
	if !ed.AskInPlaceOpen() {
		t.Fatal("scene tree context menu 'Ask the agent about this' did not open in-place composer")
	}
	if got := ed.AskInPlaceSnapshot().TargetIDs; !slices.Equal(got, []string{"knight"}) {
		t.Fatalf("scene tree context menu targets = %v, want [knight]", got)
	}
	app.Key("escape")

	// 5. Clamping: select edge_slime near the far-right/bottom of the viewport and verify composer stays
	// strictly inside the viewport and NEVER overlaps the right panel (inspector/agent panel).
	app.Post(func() {
		ed.SelectObject("edge_slime")
		ed.ResetView2D()
	})
	app.Settle()
	app.Key("meta+l")
	if !ed.AskInPlaceOpen() {
		t.Fatal("meta+l did not open for edge_slime")
	}
	composerBounds := app.Find("id:ask-in-place-composer").Bounds()
	vpBounds := app.Find("id:editor-viewport-container").Bounds()
	rightBounds := app.Find("id:editor-right-panel").Bounds()

	composerRight := composerBounds.Origin.X + composerBounds.Size.Width
	composerBottom := composerBounds.Origin.Y + composerBounds.Size.Height
	vpRight := vpBounds.Origin.X + vpBounds.Size.Width
	vpBottom := vpBounds.Origin.Y + vpBounds.Size.Height

	if composerBounds.Origin.X < vpBounds.Origin.X || composerRight > vpRight+0.5 {
		t.Fatalf("composer horizontally outside viewport: composer=[%v,%v] vp=[%v,%v]", composerBounds.Origin.X, composerRight, vpBounds.Origin.X, vpRight)
	}
	if composerBounds.Origin.Y < vpBounds.Origin.Y || composerBottom > vpBottom+0.5 {
		t.Fatalf("composer vertically outside viewport: composer=[%v,%v] vp=[%v,%v]", composerBounds.Origin.Y, composerBottom, vpBounds.Origin.Y, vpBottom)
	}
	if composerRight > rightBounds.Origin.X+0.5 {
		t.Fatalf("composer overlaps right panel: composerRight=%v rightPanelLeft=%v", composerRight, rightBounds.Origin.X)
	}
}

func TestComputeAskInPlacePlacementClampingEdgeCases(t *testing.T) {
	// Near right + bottom corner: flips above selection and clamps left inside viewport.
	p := editor.ComputeAskInPlacePlacement(700, 500, [4]float32{650, 440, 690, 480}, true, 420, 152)
	if p.Left < 12 || p.Left+p.Width > 700-12 {
		t.Fatalf("clamped horizontal bounds invalid: left=%v width=%v (vw=700)", p.Left, p.Width)
	}
	if p.Top < 12 || p.Top+p.Height > 500-12 {
		t.Fatalf("clamped vertical bounds invalid: top=%v height=%v (vh=500)", p.Top, p.Height)
	}
	if p.Top >= 440 {
		t.Fatalf("expected composer to flip above bottom selection, got top=%v (selMinY=440)", p.Top)
	}

	// Narrow viewport shrinks card cleanly inside viewport margins.
	narrow := editor.ComputeAskInPlacePlacement(300, 320, [4]float32{250, 100, 290, 140}, true, 420, 152)
	if narrow.Left < 0 || narrow.Left+narrow.Width > 300 {
		t.Fatalf("narrow clamping escaped viewport: %+v", narrow)
	}

	// No selection centers horizontally.
	noSel := editor.ComputeAskInPlacePlacement(800, 600, [4]float32{}, false, 420, 152)
	if noSel.Left < 180 || noSel.Left > 200 {
		t.Fatalf("no-selection left = %v, want centered (~190)", noSel.Left)
	}
}
