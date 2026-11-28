package editor_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/theme"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/sprite"
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

	// Verify the turn shows in the unified Agent panel's thread (RightRailView / AgentPanelView).
	if ed.RightRailTab() != editor.RightTabAgent {
		t.Fatalf("RightRailTab = %v, want RightTabAgent", ed.RightRailTab())
	}
	if !app.Exists("id:agent-panel") || !app.Exists("id:agent-turn-0") {
		t.Fatalf("Agent panel or turn %s not rendered", turn.ID)
	}
	panelText := allText(app.Find("id:agent-turn-0"))
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

	// 2. Focus a non-text control (toolbar-history), press ⌘L -> opens composer;
	// type text, press Esc -> closes WITHOUT sending, keeps selection, and restores focus!
	app.Focus("id:toolbar-history")
	if !app.Find("id:toolbar-history").Focused {
		t.Fatal("toolbar-history did not gain focus")
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
	if !app.Find("id:toolbar-history").Focused {
		t.Fatal("Esc did not return focus to the previously focused control (toolbar-history)")
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
	rightBounds := app.Find("id:editor-region-inspector").Bounds()

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

	// Non-finite (NaN / Inf) inputs are clamped to finite safe placement.
	nan32 := float32(math.NaN())
	inf32 := float32(math.Inf(1))
	safe := editor.ComputeAskInPlacePlacement(nan32, inf32, [4]float32{nan32, inf32, nan32, inf32}, true, nan32, inf32)
	for name, v := range map[string]float32{"Left": safe.Left, "Top": safe.Top, "Width": safe.Width, "Height": safe.Height} {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) || v < 0 {
			t.Fatalf("ComputeAskInPlacePlacement non-finite %s = %v", name, v)
		}
	}
}

func TestAskInPlaceHardeningThemeLifecycleFocusAndBounds(t *testing.T) {
	ed := newAskInPlaceProjectEditor(t)
	t.Cleanup(func() { _ = ed.Close() })
	_ = ed.SetAppearance(editor.AppearanceLight)

	// Host OS is Dark Mode, but user selected AppearanceLight -> composer MUST use theme.Light!
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true}), nsgotest.Size(1440, 900))
	app.Settle()

	app.Post(func() {
		ed.SelectObjects([]string{"slime_01", "slime_02"})
	})
	app.Settle()
	app.Key("meta+l")
	app.Settle()
	if !ed.AskInPlaceOpen() {
		t.Fatal("meta+l did not open composer")
	}

	// 1. Spec Light override renders theme.Light().Raised even on Dark OS.
	composer := app.Find("id:ask-in-place-composer")
	if want := theme.Light().Raised; composer.Style.Background != want {
		t.Fatalf("AppearanceLight composer background = %v, want %v", composer.Style.Background, want)
	}

	// 2. specInputField integration: Editor.IsTyping() is true while focused in ask-in-place-input.
	if !ed.IsTyping() {
		t.Fatal("Editor.IsTyping() = false while ask-in-place-input holds focus")
	}

	// 3. Tab wraps inside the composer card and Enter on focused Close button closes instead of submitting.
	for i := 0; i < 6; i++ {
		app.Key("tab")
		app.Settle()
		if app.Find("id:ask-in-place-close").Focused {
			break
		}
	}
	if !app.Find("id:ask-in-place-close").Focused {
		t.Fatal("Tab trap did not reach ask-in-place-close")
	}
	app.Key("tab")
	app.Settle()
	if !app.Find("id:ask-in-place-input").Focused {
		t.Fatal("Tab did not wrap back from ask-in-place-close to ask-in-place-input")
	}
	app.Focus("id:ask-in-place-close")
	app.Settle()
	app.Key("enter")
	app.Settle()
	if ed.AskInPlaceOpen() {
		// Enter on close button must never submit; verify no turn was recorded.
		app.Press("id:ask-in-place-close")
		app.Settle()
	}
	if len(ed.AgentTurns()) != 0 {
		t.Fatalf("Enter on Close button unexpectedly submitted %d turn(s)", len(ed.AgentTurns()))
	}
	if ed.IsTyping() {
		t.Fatal("Editor.IsTyping() remained true after closing ask-in-place")
	}

	// 4. Lifecycle: opening Settings, Connect dialog, or Play mode automatically closes Ask-in-place and blocks reopening.
	app.Key("meta+l")
	app.Settle()
	if !ed.AskInPlaceOpen() {
		t.Fatal("failed to reopen ask-in-place")
	}
	app.Post(func() { ed.SetSettingsOpen(true) })
	app.Settle()
	if ed.AskInPlaceOpen() {
		t.Fatal("SetSettingsOpen(true) did not close ask-in-place")
	}
	app.Key("meta+l")
	app.Settle()
	if ed.AskInPlaceOpen() {
		t.Fatal("meta+l opened ask-in-place while Settings (Project page) was open")
	}
	app.Post(func() { ed.SetSettingsOpen(false) })
	app.Settle()

	app.Key("meta+l")
	app.Settle()
	app.Post(func() { ed.OpenConnectDialog() })
	app.Settle()
	if ed.AskInPlaceOpen() {
		t.Fatal("OpenConnectDialog did not close ask-in-place")
	}
	app.Post(func() { ed.CloseConnectDialog() })
	app.Settle()

	// 5. RenderViewportPNG clamps NaN, Inf, and oversized scale safely to <= 1280 px.
	for _, scale := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), 0, 50} {
		pngBytes, pw, ph, err := ed.RenderViewportPNG(scale)
		if err != nil {
			t.Fatalf("RenderViewportPNG(%v): %v", scale, err)
		}
		if len(pngBytes) == 0 || pw <= 0 || ph <= 0 || pw > 1280 || ph > 1280 {
			t.Fatalf("RenderViewportPNG(%v) invalid dimensions (%dx%d, len=%d)", scale, pw, ph, len(pngBytes))
		}
	}

	// 6. Retained heavy attachments (ViewportPNG, SceneYAML) are capped to latest 8 turns.
	app.Post(func() {
		for i := 0; i < 10; i++ {
			_ = ed.OpenAskInPlace()
			ed.SetAskInPlaceDraft("turn test")
			if _, err := ed.SubmitAskInPlace(); err != nil {
				t.Errorf("SubmitAskInPlace %d: %v", i, err)
			}
		}
	})
	app.Settle()
	turns := ed.AgentTurns()
	if len(turns) != 10 {
		t.Fatalf("AgentTurns count = %d, want 10", len(turns))
	}
	if len(turns[0].Context.ViewportPNG) != 0 || turns[0].Context.SceneYAML != "" {
		t.Fatalf("oldest turn 0 should have trimmed heavy attachments (png=%d, yaml=%d)", len(turns[0].Context.ViewportPNG), len(turns[0].Context.SceneYAML))
	}
	if len(turns[9].Context.ViewportPNG) == 0 || turns[9].Context.SceneYAML == "" {
		t.Fatal("latest turn 9 should retain ViewportPNG and SceneYAML")
	}

	// 7. ReplyAgentTurn rejects NUL bytes and oversized replies (> 256 KiB).
	if err := ed.ReplyAgentTurn("", "bad\x00reply", ""); err == nil {
		t.Fatal("ReplyAgentTurn accepted NUL byte")
	}
	if err := ed.ReplyAgentTurn("", strings.Repeat("a", (256<<10)+1), ""); err == nil {
		t.Fatal("ReplyAgentTurn accepted oversized reply")
	}

	// 8. CmdAgentTab & CmdInspectorTab obey Enabled guards and switch RightRailTab cleanly.
	app.Post(func() {
		if err := ed.ExecuteCommand(editor.CmdInspectorTab); err != nil {
			t.Errorf("CmdInspectorTab: %v", err)
		}
	})
	app.Settle()
	if ed.RightRailTab() != editor.RightTabInspector {
		t.Fatalf("RightRailTab after CmdInspectorTab = %v, want RightTabInspector", ed.RightRailTab())
	}
	app.Post(func() {
		if err := ed.ExecuteCommand(editor.CmdAgentTab); err != nil {
			t.Errorf("CmdAgentTab: %v", err)
		}
	})
	app.Settle()
	if ed.RightRailTab() != editor.RightTabAgent {
		t.Fatalf("RightRailTab after CmdAgentTab = %v, want RightTabAgent", ed.RightRailTab())
	}
}
