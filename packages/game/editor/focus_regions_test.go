package editor_test

import (
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/nsgotest"
	"github.com/nsgo-dev/nsgo/ui"
)

var wantSpecAccent = core.RGB(0x3B7BFF)

var focusDesktopEnv = nsgotest.Environment(core.Environment{
	OS:    core.OSMacOS,
	Idiom: core.IdiomDesktop,
	Dark:  true,
})

func newFocusTestScene() *sceneformat.SceneDoc {
	return &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "Focus Test",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#15181D",
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "main",
				Space: sceneformat.SpaceWorld,
				Order: 0,
				Objects: []sceneformat.ObjectDoc{
					{ID: "hero", Name: "Player", X: 120, Y: 180},
					{ID: "knight", Name: "Knight", X: 260, Y: 180},
					{ID: "slime", Name: "Slime", X: 400, Y: 180},
					{ID: "hidden_obj", Name: "Hidden", X: 480, Y: 180},
				},
			},
		},
	}
}

func focusedView(app *nsgotest.App) *nsgotest.View {
	root := app.FindMounted("id:scene-editor")
	for root != nil && root.Parent != nil {
		root = root.Parent
	}
	var found *nsgotest.View
	var walk func(v *nsgotest.View)
	walk = func(v *nsgotest.View) {
		if v == nil || found != nil {
			return
		}
		if v.Focused {
			found = v
			return
		}
		for _, c := range v.Children {
			walk(c)
		}
	}
	walk(root)
	return found
}

func isDescendantOf(v, ancestor *nsgotest.View) bool {
	for cur := v; cur != nil; cur = cur.Parent {
		if cur == ancestor {
			return true
		}
	}
	return false
}

// TestToolbarTabFocusRingKeyboardOnlyVsClick verifies Acceptance Criterion 1:
// tabbing through the toolbar draws the 2 px #3B7BFF ring with +2 px offset on each
// focused control after a key press, and mouse clicks draw no focus ring.
func TestToolbarTabFocusRingKeyboardOnlyVsClick(t *testing.T) {
	ed, err := editor.New(newFocusTestScene(), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	t.Cleanup(func() { _ = ed.Close() })

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, focusDesktopEnv, nsgotest.Size(1440, 900))
	app.Settle()

	toolbar := app.Find("id:editor-toolbar")

	// Clicking a toolbar button with the mouse must NOT draw a focus ring.
	app.Press("id:toolbar-history")
	app.Settle()
	if v := app.Find("id:toolbar-history"); v.Style.Outline.Width != 0 {
		t.Fatalf("mouse click drew focus ring on toolbar-history: %+v", v.Style.Outline)
	}
	app.Press("id:toolbar-history") // restore
	app.Settle()

	// Tab through toolbar controls and verify every focused toolbar control shows the
	// 2 px accent (#3B7BFF) ring at +2 px offset.
	visited := make([]string, 0, 8)
	for step := 0; step < 30; step++ {
		app.Key("tab")
		app.Settle()
		fv := focusedView(app)
		if fv == nil {
			t.Fatalf("step %d: expected a focused view after Tab", step)
		}
		if !isDescendantOf(fv, toolbar) {
			if len(visited) > 0 {
				break
			}
			continue
		}
		want := core.Outline{Width: 2, Color: wantSpecAccent, Offset: 2}
		if fv.Style.Outline != want {
			t.Fatalf("toolbar control %q (%s) outline after Tab = %+v, want %+v", fv.Props.TestID, fv.Props.Label, fv.Style.Outline, want)
		}
		id := fv.Props.TestID
		if id == "" {
			id = fv.Props.Label
		}
		if !slices.Contains(visited, id) {
			visited = append(visited, id)
		}
		if len(visited) >= 5 {
			break
		}
	}
	if len(visited) < 5 {
		t.Fatalf("expected to Tab across at least 5 toolbar controls, visited %v", visited)
	}

	// Mouse click clears keyboard focus ring on toolbar controls.
	app.Press("id:toolbar-history")
	app.Settle()
	if v := app.Find("id:toolbar-history"); v.Style.Outline.Width != 0 {
		t.Fatalf("mouse click after Tab drew focus ring on toolbar-history: %+v", v.Style.Outline)
	}
	for _, id := range []string{"toolbar-history", "toolbar-play", "toolbar-palette", "tool-select"} {
		if app.Exists("id:" + id) {
			if v := app.Find("id:" + id); v.Style.Outline.Width != 0 {
				t.Fatalf("stale focus ring remained on %q after mouse click: %+v", id, v.Style.Outline)
			}
		}
	}
}

// TestF6CyclesRegionsInSpecOrderAndRestoresLastControl verifies Acceptance Criterion 2:
// F6 visits regions in fixed Spec order (Header -> Scene tree -> Viewport -> Inspector ->
// Status bar -> Toast when shown), draws the inset -2 px ring on the active region,
// Shift-F6 walks back, and each region remembers and restores its last focused control.
func TestF6CyclesRegionsInSpecOrderAndRestoresLastControl(t *testing.T) {
	ed, err := editor.New(newFocusTestScene(), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	t.Cleanup(func() { _ = ed.Close() })

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, focusDesktopEnv, nsgotest.Size(1440, 900))
	app.Settle()

	regionViewID := map[editor.EditorRegion]string{
		editor.RegionHeader:    "editor-toolbar",
		editor.RegionSceneTree: "editor-region-scene-tree",
		editor.RegionViewport:  "editor-region-viewport",
		editor.RegionInspector: "editor-region-inspector",
		editor.RegionStatusBar: "editor-status",
		editor.RegionToast:     "editor-region-toast",
	}
	wantInset := core.Outline{Width: 2, Color: wantSpecAccent, Offset: -2}

	assertRegionRing := func(step string, want editor.EditorRegion) {
		t.Helper()
		if got := ed.ActiveRegion(); got != want {
			t.Fatalf("%s: ActiveRegion() = %q, want %q", step, got, want)
		}
		if got := ed.ActiveRegionRing(); got != want {
			t.Fatalf("%s: ActiveRegionRing() = %q, want %q", step, got, want)
		}
		v := app.Find("id:" + regionViewID[want])
		if v.Style.Outline != wantInset {
			t.Fatalf("%s: region %q outline = %+v, want inset %+v", step, want, v.Style.Outline, wantInset)
		}
		// Other mounted regions must NOT show the region ring at the same time.
		for r, tid := range regionViewID {
			if r == want || !app.Exists("id:"+tid) {
				continue
			}
			if other := app.Find("id:" + tid); other.Style.Outline.Width != 0 {
				t.Fatalf("%s: non-active region %q still has outline %+v", step, r, other.Style.Outline)
			}
		}
	}

	// Without a toast: 5 regions in fixed Spec order, wrapping back to Header.
	forward5 := []editor.EditorRegion{
		editor.RegionHeader,
		editor.RegionSceneTree,
		editor.RegionViewport,
		editor.RegionInspector,
		editor.RegionStatusBar,
		editor.RegionHeader,
	}
	for i, want := range forward5 {
		app.Key("f6")
		app.Settle()
		assertRegionRing("F6(no-toast)["+string(want)+"]", want)
		if i == 0 {
			// Inside Header: focus toolbar-history so we can verify restoration later.
			app.Focus("id:toolbar-history")
			app.Settle()
		}
		if i == 1 {
			// Inside Scene tree: focus scene-tree-eye-hero so we can verify restoration later.
			app.Focus("id:scene-tree-eye-hero")
			app.Settle()
		}
	}

	// Returning to Header via F6 must restore focus to toolbar-history.
	if fv := focusedView(app); fv == nil || fv.Props.TestID != "toolbar-history" {
		gotID := "<nil>"
		if fv != nil {
			gotID = fv.Props.TestID
		}
		t.Fatalf("after F6 wrap to Header, focused = %q, want toolbar-history", gotID)
	}

	// Moving forward into SceneTree must restore focus to scene-tree-eye-hero.
	app.Key("f6")
	app.Settle()
	assertRegionRing("F6-restore-scene-tree", editor.RegionSceneTree)
	if fv := focusedView(app); fv == nil || fv.Props.TestID != "scene-tree-eye-hero" {
		gotID := "<nil>"
		if fv != nil {
			gotID = fv.Props.TestID
		}
		t.Fatalf("after F6 to SceneTree, focused = %q, want scene-tree-eye-hero", gotID)
	}

	// Shift-F6 walks backwards in exact reverse order and restores Header's remembered control.
	app.Key("shift+f6")
	app.Settle()
	assertRegionRing("Shift-F6-back-to-header", editor.RegionHeader)
	if fv := focusedView(app); fv == nil || fv.Props.TestID != "toolbar-history" {
		gotID := "<nil>"
		if fv != nil {
			gotID = fv.Props.TestID
		}
		t.Fatalf("after Shift-F6 to Header, focused = %q, want toolbar-history", gotID)
	}

	// Show a toast: must NOT steal focus from Header, and must now appear after StatusBar in F6 order.
	ed.ShowToast(editor.Toast{
		Kind:        editor.ToastSuccess,
		Message:     "Kept 3 changes",
		ActionLabel: "Undo",
	})
	app.Settle()
	if fv := focusedView(app); fv == nil || fv.Props.TestID != "toolbar-history" {
		t.Fatalf("ShowToast stole focus from toolbar-history: got %v", fv)
	}

	// Shift-F6 from Header wraps directly to Toast (the last region while shown).
	app.Key("shift+f6")
	app.Settle()
	assertRegionRing("Shift-F6-to-toast", editor.RegionToast)
	if fv := focusedView(app); fv == nil || fv.Props.TestID != "editor-toast-action" {
		gotID := "<nil>"
		if fv != nil {
			gotID = fv.Props.TestID
		}
		t.Fatalf("F6/Shift-F6 into Toast focused %q, want editor-toast-action", gotID)
	}

	// Tab to the dismiss button inside Toast, leave with Shift-F6 to StatusBar, return with F6 -> restores dismiss button!
	app.Key("tab")
	app.Settle()
	if fv := focusedView(app); fv == nil || fv.Props.TestID != "editor-toast-dismiss" {
		gotID := "<nil>"
		if fv != nil {
			gotID = fv.Props.TestID
		}
		t.Fatalf("Tab inside Toast focused %q, want editor-toast-dismiss", gotID)
	}
	app.Key("shift+f6")
	app.Settle()
	assertRegionRing("Shift-F6-toast-to-status", editor.RegionStatusBar)
	app.Key("f6")
	app.Settle()
	assertRegionRing("F6-status-back-to-toast", editor.RegionToast)
	if fv := focusedView(app); fv == nil || fv.Props.TestID != "editor-toast-dismiss" {
		gotID := "<nil>"
		if fv != nil {
			gotID = fv.Props.TestID
		}
		t.Fatalf("returning to Toast did not restore last focused control: got %q, want editor-toast-dismiss", gotID)
	}

	ed.DismissToast()
	app.Settle()
}

// TestViewportTabCyclesObjectsWithoutCanvasRing verifies that in the viewport,
// focus is the selection: no focus ring draws on the canvas itself, and Tab /
// Shift-Tab select the next and previous visible, unlocked object.
func TestViewportTabCyclesObjectsWithoutCanvasRing(t *testing.T) {
	ed, err := editor.New(newFocusTestScene(), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	if err := ed.SetObjectHidden("hidden_obj", true); err != nil {
		t.Fatalf("SetObjectHidden: %v", err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, focusDesktopEnv, nsgotest.Size(1440, 900))
	app.Settle()

	// F6 three times: Header -> Scene tree -> Viewport.
	app.Key("f6")
	app.Key("f6")
	app.Key("f6")
	app.Settle()

	if got := ed.ActiveRegion(); got != editor.RegionViewport {
		t.Fatalf("ActiveRegion() = %q, want viewport", got)
	}
	// The canvas itself and its viewport frame must draw NO focus ring.
	if vp := app.Find("id:editor-viewport"); vp.Style.Outline.Width != 0 {
		t.Fatalf("viewport canvas must not draw a focus ring, got %+v", vp.Style.Outline)
	}
	if frame := app.Find("id:editor-viewport-frame"); frame.Style.Outline.Width != 0 {
		t.Fatalf("viewport frame must not draw a focus ring, got %+v", frame.Style.Outline)
	}
	// The region wrapper draws the F6 inset -2 px ring.
	if reg := app.Find("id:editor-region-viewport"); reg.Style.Outline != (core.Outline{Width: 2, Color: wantSpecAccent, Offset: -2}) {
		t.Fatalf("viewport region outline = %+v, want inset -2 px accent", reg.Style.Outline)
	}

	// Tab cycles hero -> knight -> slime -> hero (skipping hidden_obj).
	for _, want := range []string{"hero", "knight", "slime", "hero"} {
		app.Key("tab")
		app.Settle()
		if ed.SelectedID != want {
			t.Fatalf("after Tab in viewport, SelectedID = %q, want %q", ed.SelectedID, want)
		}
	}
	// Shift-Tab cycles backwards: hero -> slime -> knight.
	for _, want := range []string{"slime", "knight"} {
		app.Key("shift+tab")
		app.Settle()
		if ed.SelectedID != want {
			t.Fatalf("after Shift-Tab in viewport, SelectedID = %q, want %q", ed.SelectedID, want)
		}
	}
}

// TestDialogTrapsFocusAndEscRestoresOpener verifies Acceptance Criterion 3:
// dialogs trap Tab inside themselves, suppress global F6 behind the modal while
// still allowing Mod+. to stop the agent, and closing with Esc returns focus to
// the exact control that opened the dialog.
func TestDialogTrapsFocusAndEscRestoresOpener(t *testing.T) {
	ed, err := editor.New(newFocusTestScene(), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	t.Cleanup(func() { _ = ed.Close() })

	agentStopped := 0
	ed.OnStopAgent = func() { agentStopped++ }

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, focusDesktopEnv, nsgotest.Size(1440, 900))
	app.Settle()

	// 1. Open Keyboard Shortcuts modal (⌘/) while focused on toolbar-history.
	app.Focus("id:toolbar-history")
	app.Settle()
	app.Key("meta+/")
	app.Settle()
	if !ed.ShortcutsSheetOpen() {
		t.Fatal("expected Shortcuts Sheet dialog to open on Cmd+/")
	}

	dialog := app.Find("id:editor-shortcuts-sheet")

	// Tab must stay trapped inside the modal dialog across repeated Tab presses.
	for i := 0; i < 6; i++ {
		app.Key("tab")
		app.Settle()
		fv := focusedView(app)
		if fv == nil || !isDescendantOf(fv, dialog) {
			t.Fatalf("Tab %d escaped dialog: focused=%v", i, fv)
		}
	}

	// F6 must NOT cycle regions behind the open dialog.
	app.Key("f6")
	app.Settle()
	if ed.ActiveRegionRing() != editor.RegionNone {
		t.Fatalf("F6 fired behind modal dialog: ActiveRegionRing() = %q", ed.ActiveRegionRing())
	}

	// Mod+. MUST still stop the agent even while a dialog is open (Spec rule 04).
	app.Key("mod+.")
	app.Settle()
	if agentStopped != 1 {
		t.Fatalf("mod+. behind dialog ran OnStopAgent %d times, want 1", agentStopped)
	}

	// Closing with Esc must return focus to toolbar-history.
	app.Key("escape")
	app.Advance(250 * time.Millisecond)
	app.Settle()
	if ed.ShortcutsSheetOpen() {
		t.Fatal("expected Esc to close Shortcuts Sheet dialog")
	}
	if fv := focusedView(app); fv == nil || fv.Props.TestID != "toolbar-history" {
		gotID := "<nil>"
		if fv != nil {
			gotID = fv.Props.TestID
		}
		t.Fatalf("after Esc closed Shortcuts Sheet, focus = %q, want toolbar-history", gotID)
	}

	// 2. Open unified Settings view via Cmd+, while focused on toolbar-history, verify F6 is blocked, and Esc restores toolbar-history.
	app.Key("mod+,")
	app.Settle()
	if !ed.SettingsOpen() {
		t.Fatal("expected Settings view to open on Cmd+,")
	}
	app.Key("f6")
	app.Settle()
	if ed.ActiveRegionRing() != editor.RegionNone {
		t.Fatalf("F6 fired while Settings view open: ActiveRegionRing() = %q", ed.ActiveRegionRing())
	}
	app.Key("escape")
	app.Advance(250 * time.Millisecond)
	app.Settle()
	if ed.SettingsOpen() {
		t.Fatal("expected Esc to close Settings view")
	}
	if fv := focusedView(app); fv == nil || fv.Props.TestID != "toolbar-history" {
		gotID := "<nil>"
		if fv != nil {
			gotID = fv.Props.TestID
		}
		t.Fatalf("after Esc closed Settings view, focus = %q, want toolbar-history", gotID)
	}

	// 3. Open Command Palette via mouse click on toolbar-palette, close with Esc -> returns focus to toolbar-palette.
	app.Press("id:toolbar-palette")
	app.Settle()
	if !ed.CommandPaletteOpen() {
		t.Fatal("expected Command Palette to open")
	}
	app.Key("escape")
	app.Advance(250 * time.Millisecond)
	app.Settle()
	if ed.CommandPaletteOpen() {
		t.Fatal("expected Esc to close Command Palette")
	}
	if fv := focusedView(app); fv == nil || fv.Props.TestID != "toolbar-palette" {
		gotID := "<nil>"
		if fv != nil {
			gotID = fv.Props.TestID
		}
		t.Fatalf("after Esc closed Command Palette, focus = %q, want toolbar-palette", gotID)
	}
}

// TestSpecControlsFocusRingVariants checks Button (+2), Icon button (+2), Switch (+2),
// selected+focused Tree row (-2 with selection fill preserved), Field (-2), and
// Composer card (:focus-within +2), and verifies mouse clicks never draw control rings.
func TestSpecControlsFocusRingVariants(t *testing.T) {
	on := true
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetTheme(editor.FocusTheme())
		onSig := core.NewSignal(cx, on)
		txt := core.NewSignal(cx, "Make the slimes slower")
		return core.View{
			Style: core.Style{Padding: core.All(16), Gap: 12, Background: core.RGB(0x101216)},
			Children: []core.Node{
				ui.Button{
					TestID:  "demo-btn",
					Text:    "Play",
					Variant: ui.ButtonPrimary,
					Style: core.Style{
						FocusVisible: &core.Style{OutlineWidth: 2, OutlineColor: wantSpecAccent, OutlineOffset: 2},
					},
				},
				editor.SpecIconButton{
					TestID:  "demo-icon-btn",
					Icon:    lucide.RotateCcw,
					Label:   "History",
					Tooltip: "History · ⌘Y",
				},
				editor.SpecSwitch{
					TestID:  "demo-switch",
					Label:   "Snap to grid",
					Checked: onSig.Get,
					OnChange: func(v bool) {
						onSig.Set(v)
					},
				},
				core.View{
					TestID:   "demo-tree-row",
					Selected: func() bool { return true },
					OnPress:  func(core.PressEvent) {},
					Style: core.Style{
						Height:       core.Pt(28),
						CornerRadius: 6,
						Background:   core.RGB(0x1C2638),
						FocusVisible: &core.Style{OutlineWidth: 2, OutlineColor: wantSpecAccent, OutlineOffset: -2},
					},
					Children: []core.Node{
						core.Text{Content: "Knight", Style: core.Style{Color: core.RGB(0xFFFFFF)}},
					},
				},
				ui.Input{
					TestID: "demo-field",
					Label:  "Object name",
					Value:  txt.Get,
					OnChange: func(s string) {
						txt.Set(s)
					},
					Style: core.Style{
						FocusVisible: &core.Style{OutlineWidth: 2, OutlineColor: wantSpecAccent, OutlineOffset: -2},
					},
				},
				editor.SpecFocusWithinCard{
					TestID: "demo-composer-card",
					Style:  core.Style{Height: core.Pt(40), Padding: core.Symmetric(0, 12), JustifyContent: core.JustifyCenter},
					Children: []core.Node{
						core.Input{
							TestID:   "demo-composer-input",
							Label:    "Composer",
							Value:    txt.Get,
							OnChange: txt.Set,
							Style:    core.Style{Height: core.Pt(28), Color: core.RGB(0xD9DDE5)},
						},
					},
				},
			},
		}
	}, focusDesktopEnv, nsgotest.Size(640, 480))
	app.Settle()

	// Mouse clicks on button, icon button, and switch must NOT draw a focus ring.
	for _, id := range []string{"demo-btn", "demo-icon-btn", "demo-switch"} {
		app.Press("id:" + id)
		app.Settle()
		if got := app.Find("id:" + id).Style.Outline; got.Width != 0 {
			t.Fatalf("mouse click on %q drew outline %+v", id, got)
		}
	}

	wantOut := core.Outline{Width: 2, Color: wantSpecAccent, Offset: 2}
	wantIn := core.Outline{Width: 2, Color: wantSpecAccent, Offset: -2}

	// Keyboard focus on button, icon button, switch -> +2 px offset accent ring.
	for _, id := range []string{"demo-btn", "demo-icon-btn", "demo-switch"} {
		app.Focus("id:" + id)
		app.Settle()
		if got := app.Find("id:" + id).Style.Outline; got != wantOut {
			t.Fatalf("keyboard focus on %q outline = %+v, want %+v", id, got, wantOut)
		}
	}

	// Selected and focused tree row shows BOTH selection fill (#1C2638) AND inset -2 px ring.
	app.Focus("id:demo-tree-row")
	app.Settle()
	row := app.Find("id:demo-tree-row")
	if row.Style.Background != core.RGB(0x1C2638) {
		t.Fatalf("selected+focused tree row lost selection fill: got %v", row.Style.Background)
	}
	if row.Style.Outline != wantIn {
		t.Fatalf("selected+focused tree row outline = %+v, want %+v", row.Style.Outline, wantIn)
	}

	// Text field shows inset -2 px ring on focus.
	app.Focus("id:demo-field")
	app.Settle()
	if got := app.Find("id:demo-field").Style.Outline; got != wantIn {
		t.Fatalf("focused field outline = %+v, want %+v", got, wantIn)
	}

	// Focusing the composer input draws the +2 px ring on the composer card (:focus-within).
	app.Focus("id:demo-composer-input")
	app.Settle()
	if got := app.Find("id:demo-composer-card").Style.Outline; got != wantOut {
		t.Fatalf("composer card :focus-within outline = %+v, want %+v", got, wantOut)
	}
}

// TestFocusStabilityDuringDragTypingAndAgentPrompt verifies that nothing steals focus
// during a drag or while typing, and a new agent permission card / question takes
// focus ONLY when focus was already inside the agent panel.
func TestFocusStabilityDuringDragTypingAndAgentPrompt(t *testing.T) {
	ed, err := editor.New(newFocusTestScene(), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	ed.SelectObject("hero")

	agentRef := &core.Ref{}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		edView := editor.EditorView(cx, ed)
		ed.SetAgentPanelRef(agentRef)
		return core.View{
			Style: core.Style{Width: core.Pct(100), Height: core.Pct(100)},
			Children: []core.Node{
				core.View{Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)}, Children: []core.Node{edView}},
				core.View{
					Ref:    agentRef,
					TestID: "test-agent-panel",
					Style:  core.Style{Height: core.Pt(40), Direction: core.Row},
					Children: []core.Node{
						ui.Button{TestID: "test-agent-btn", Text: "Agent"},
					},
				},
			},
		}
	}, focusDesktopEnv, nsgotest.Size(1440, 940))
	app.Settle()

	// 1. Focus in Header (outside agent panel): permission card must NOT take focus.
	app.Focus("id:toolbar-play")
	app.Settle()
	if ed.FocusInAgentPanel() || ed.ShouldFocusAgentPrompt() {
		t.Fatal("agent prompt must not take focus while focus is in toolbar")
	}

	// 2. Focus inside a text input (inspector-name): IsTyping=true, CanAutoMoveFocus=false.
	app.Focus("id:inspector-name")
	app.Settle()
	if !ed.IsTyping() || ed.CanAutoMoveFocus() || ed.ShouldFocusAgentPrompt() {
		t.Fatalf("while typing: IsTyping=%v CanAutoMoveFocus=%v ShouldFocusAgentPrompt=%v", ed.IsTyping(), ed.CanAutoMoveFocus(), ed.ShouldFocusAgentPrompt())
	}

	// 3. Focus inside Agent panel (and not dragging/typing): permission card MAY take focus.
	app.Focus("id:test-agent-btn")
	app.Settle()
	if ed.IsTyping() {
		t.Fatal("expected IsTyping false after leaving text input")
	}
	if !ed.FocusInAgentPanel() || !ed.ShouldFocusAgentPrompt() {
		t.Fatalf("when focus is in agent panel: FocusInAgentPanel=%v ShouldFocusAgentPrompt=%v, want true/true", ed.FocusInAgentPanel(), ed.ShouldFocusAgentPrompt())
	}

	// 4. During an active 2D marquee drag, nothing may move focus (including F6 and agent prompts).
	ed.BeginMarquee2D(10, 10)
	if !ed.IsDragging() || ed.CanAutoMoveFocus() || ed.ShouldFocusAgentPrompt() {
		t.Fatalf("during drag: IsDragging=%v CanAutoMoveFocus=%v ShouldFocusAgentPrompt=%v", ed.IsDragging(), ed.CanAutoMoveFocus(), ed.ShouldFocusAgentPrompt())
	}
	app.Key("f6")
	app.Settle()
	if fv := focusedView(app); fv == nil || fv.Props.TestID != "test-agent-btn" {
		t.Fatalf("F6 moved focus during active drag: got %v", fv)
	}
	ed.CancelMarquee2D()
}

// TestFocusRingGalleryScreenshots captures real macOS screenshots (under
// `go tool nsgo test -target macos -run TestFocusRingGalleryScreenshots ./editor`)
// showing the 6 Spec "Focus ring" controls (button, icon button, switch, tree row,
// field / composer card, and region via F6) plus the live editor with an F6 region ring.
func TestFocusRingGalleryScreenshots(t *testing.T) {
	// 1. Spec "Focus ring" 6-card gallery matching docs/design/editor/Spec.dc.html tokens only.
	var (
		bg           = core.RGB(0x101216)
		panel        = core.RGB(0x12141A)
		raised       = core.RGB(0x171A21)
		line         = core.RGB(0x1F232A)
		lineRaised   = core.RGB(0x262B35)
		textStrong   = core.RGB(0xFFFFFF)
		textPrimary  = core.RGB(0xD9DDE5)
		textSec      = core.RGB(0xB4BAC6)
		textMuted    = core.RGB(0x8B92A0)
		accent       = core.RGB(0x3B7BFF)
		changed      = core.RGB(0xE9A84A)
		selectedFill = core.RGB(0x1C2638)
	)
	ringOut := core.Style{OutlineWidth: 2, OutlineColor: accent, OutlineOffset: 2}
	ringIn := core.Style{OutlineWidth: 2, OutlineColor: accent, OutlineOffset: -2}

	card := func(caption string, body core.Node) core.Node {
		return core.View{
			Style: core.Style{
				FlexGrow:       1,
				FlexBasis:      core.Pt(0),
				Height:         core.Pt(148),
				CornerRadius:   12,
				Background:     panel,
				BorderWidth:    1,
				BorderColor:    line,
				Padding:        core.Symmetric(16, 20),
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				Gap:            16,
			},
			Children: []core.Node{
				body,
				core.Text{
					Content: caption,
					Style:   core.Style{FontSize: 12.5, Color: textMuted},
				},
			},
		}
	}

	gallery := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{
			TestID: "focus-ring-spec-gallery",
			Style: core.Style{
				Width:      core.Pct(100),
				Height:     core.Pct(100),
				Background: bg,
				Padding:    core.All(32),
				Gap:        20,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 16},
					Children: []core.Node{
						core.Text{Content: "Focus ring", Style: core.Style{FontSize: 22, FontWeight: 600, Color: textStrong}},
						core.Text{Content: "2 px accent ring (#3B7BFF), +2 px offset on controls, -2 px inset on rows, fields & regions", Style: core.Style{FontSize: 13, Color: textMuted}},
					},
				},
				// Row 1: Button, Icon button, Switch
				core.View{
					Style: core.Style{Direction: core.Row, Gap: 16},
					Children: []core.Node{
						card("Button · offset 2", core.View{
							TestID: "gallery-button",
							Style: core.Style{
								Direction:      core.Row,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								Height:         core.Pt(32),
								Padding:        core.Symmetric(0, 14),
								Gap:            8,
								CornerRadius:   8,
								Background:     textStrong,
							}.Merge(ringOut),
							Children: []core.Node{
								core.Icon{SVG: lucide.Play(), Style: core.Style{Width: core.Pt(12), Height: core.Pt(12), Color: bg}},
								core.Text{Content: "Play", Style: core.Style{FontSize: 12.5, FontWeight: 600, Color: bg}},
							},
						}),
						card("Icon button · offset 2", core.View{
							TestID: "gallery-icon-button",
							Style: core.Style{
								Width:          core.Pt(30),
								Height:         core.Pt(30),
								CornerRadius:   8,
								Background:     raised,
								BorderWidth:    1,
								BorderColor:    lineRaised,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
							}.Merge(ringOut),
							Children: []core.Node{
								core.Icon{SVG: lucide.RotateCcw(), Style: core.Style{Width: core.Pt(14), Height: core.Pt(14), Color: textPrimary}},
							},
						}),
						card("Switch · offset 2", core.View{
							Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12},
							Children: []core.Node{
								core.View{
									TestID: "gallery-switch",
									Style: core.Style{
										Width:        core.Pt(32),
										Height:       core.Pt(18),
										CornerRadius: 9,
										Background:   accent,
										Padding:      core.All(2),
									}.Merge(ringOut),
									Children: []core.Node{
										core.View{
											Style: core.Style{
												Width:        core.Pt(14),
												Height:       core.Pt(14),
												CornerRadius: 7,
												Background:   textStrong,
												Transform:    core.Transform{TranslateX: 14},
											},
										},
									},
								},
								core.Text{Content: "Snap to grid", Style: core.Style{FontSize: 12.5, Color: textPrimary}},
							},
						}),
					},
				},
				// Row 2: Tree row (-2), Field (-2), Composer card (:focus-within +2), Region via F6 (-2)
				core.View{
					Style: core.Style{Direction: core.Row, Gap: 16},
					Children: []core.Node{
						card("Tree row · inset, offset -2", core.View{
							Style: core.Style{Width: core.Pct(100), Gap: 2},
							Children: []core.Node{
								core.View{
									Style: core.Style{
										Height:       core.Pt(28),
										Direction:    core.Row,
										AlignItems:   core.AlignCenter,
										Gap:          8,
										Padding:      core.Symmetric(0, 10),
										CornerRadius: 6,
									},
									Children: []core.Node{
										core.View{Style: core.Style{Width: core.Pt(7), Height: core.Pt(7), CornerRadius: 2, Background: accent}},
										core.Text{Content: "Player", Style: core.Style{FontSize: 12.5, Color: textSec}},
									},
								},
								core.View{
									TestID: "gallery-tree-row",
									Style: core.Style{
										Height:       core.Pt(28),
										Direction:    core.Row,
										AlignItems:   core.AlignCenter,
										Gap:          8,
										Padding:      core.Symmetric(0, 10),
										CornerRadius: 6,
										Background:   selectedFill,
									}.Merge(ringIn),
									Children: []core.Node{
										core.View{Style: core.Style{Width: core.Pt(7), Height: core.Pt(7), CornerRadius: 2, Background: changed}},
										core.Text{Content: "Knight", Style: core.Style{FontSize: 12.5, Color: textStrong}},
									},
								},
							},
						}),
						card("Field · inset, offset -2", core.View{
							TestID: "gallery-field",
							Style: core.Style{
								Width:          core.Pct(100),
								Height:         core.Pt(32),
								CornerRadius:   6,
								Background:     raised,
								BorderWidth:    1,
								BorderColor:    lineRaised,
								Padding:        core.Symmetric(0, 10),
								JustifyContent: core.JustifyCenter,
							}.Merge(ringIn),
							Children: []core.Node{
								core.Text{Content: "Knight", Style: core.Style{FontSize: 12.5, Color: textPrimary}},
							},
						}),
						card("Composer · card, focus-within", core.View{
							TestID: "gallery-composer",
							Style: core.Style{
								Width:        core.Pct(100),
								Height:       core.Pt(40),
								CornerRadius: 12,
								Background:   raised,
								BorderWidth:  1,
								BorderColor:  lineRaised,
								Direction:    core.Row,
								AlignItems:   core.AlignCenter,
								Padding:      core.Symmetric(0, 12),
							}.Merge(ringOut),
							Children: []core.Node{
								core.Text{Content: "Make the slimes slower", Style: core.Style{FontSize: 12.5, Color: textPrimary}},
								core.View{Style: core.Style{Width: core.Pt(1.5), Height: core.Pt(16), Background: textStrong, Margin: core.Edges{Left: 2}}},
							},
						}),
						card("Region via F6 · inset, offset -2", core.View{
							TestID: "gallery-region",
							Style: core.Style{
								Width:        core.Pct(100),
								Height:       core.Pt(64),
								CornerRadius: 4,
								Background:   core.RGB(0x15181D),
								Padding:      core.All(10),
							}.Merge(ringIn),
							Children: []core.Node{
								core.Text{Content: "Viewport", Style: core.Style{FontSize: 11, Color: textMuted}},
							},
						}),
					},
				},
			},
		}
	}, focusDesktopEnv, nsgotest.Size(1280, 440))
	gallery.Settle()
	gallery.Screenshot("focus-ring-gallery")

	// 2. Live EditorView screenshot with F6 region ring on Viewport + active object selection.
	ed, err := editor.New(newFocusTestScene(), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	_ = ed.SetAppearance(editor.AppearanceDark)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, focusDesktopEnv, nsgotest.Size(1440, 900))
	app.Settle()

	app.Key("f6") // Header
	app.Settle()
	app.Screenshot("editor-f6-header")

	app.Key("f6") // Scene tree
	app.Key("f6") // Viewport
	app.Key("tab")
	app.Settle()
	app.Screenshot("editor-f6-viewport")
}

// TestFocusSpecLightCaptureGuardsUnmountTypingAndScrub verifies:
// 1. Spec Light appearance uses LightSpecPalette().Accent.Edge (#1D5CE6) on focus rings and F6 regions.
// 2. Active shortcut capture in Settings › Shortcuts suppresses F6, Shift-F6, and Mod+.
// 3. Unmounting a focused text input (switching selection while typing) clears IsTyping() cleanly.
// 4. Chrome remounts (appearance switch) do not desync subsequent modal opener focus restoration.
// 5. Active inspector scrubbing (BeginInspectorScrub) counts as IsDragging() and blocks F6 cycling.
func TestFocusSpecLightCaptureGuardsUnmountTypingAndScrub(t *testing.T) {
	lightAccent := editor.LightSpecPalette().Accent.Edge
	lightEnv := nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: false})

	// 1. Spec Light controls draw #1D5CE6 focus rings (never dark #3B7BFF).
	lightApp := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetTheme(editor.FocusTheme())
		onSig := core.NewSignal(cx, true)
		return core.View{
			Style: core.Style{Padding: core.All(16), Gap: 12},
			Children: []core.Node{
				editor.SpecSwitch{TestID: "light-switch", Label: "Snap", Checked: onSig.Get, OnChange: onSig.Set},
				editor.SpecIconButton{TestID: "light-icon-btn", Icon: lucide.Settings, Label: "Settings", OnPress: func() {}},
				editor.SpecFocusWithinCard{
					TestID:   "light-card",
					Children: []core.Node{ui.Input{TestID: "light-card-input", Label: "Prompt"}},
				},
			},
		}
	}, lightEnv, nsgotest.Size(600, 300))
	lightApp.Settle()

	for _, id := range []string{"light-switch", "light-icon-btn"} {
		lightApp.Key("tab")
		lightApp.Settle()
		o := lightApp.Find("id:" + id).Style.Outline
		if o.Width != 2 || o.Color != lightAccent || o.Offset != 2 {
			t.Fatalf("%s light outline = %+v, want (2, %v, 2)", id, o, lightAccent)
		}
	}
	lightApp.Key("tab")
	lightApp.Settle()
	if o := lightApp.Find("id:light-card").Style.Outline; o.Width != 2 || o.Color != lightAccent || o.Offset != 2 {
		t.Fatalf("light-card FocusWithin outline = %+v, want (2, %v, 2)", o, lightAccent)
	}

	// 2. Full EditorView in Spec Light: F6 region ring uses #1D5CE6, shortcut capture suppresses F6/Mod+.,
	// unmounting a focused input clears IsTyping(), appearance toggle doesn't break dialog focus restore,
	// and inspector scrubbing blocks F6.
	ed, err := editor.New(newFocusTestScene(), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	_ = ed.SetAppearance(editor.AppearanceLight)
	ed.SelectObject("hero")

	agentStopped := 0
	ed.OnStopAgent = func() { agentStopped++ }

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, lightEnv, nsgotest.Size(1440, 900))
	app.Settle()

	// F6 region ring in Spec Light must use #1D5CE6.
	app.Key("f6")
	app.Settle()
	if o := app.Find("id:editor-toolbar").Style.Outline; o.Width != 2 || o.Color != lightAccent || o.Offset != -2 {
		t.Fatalf("Spec Light Header F6 ring = %+v, want (2, %v, -2)", o, lightAccent)
	}

	// Shortcut capture in Settings › Shortcuts must suppress F6 and Mod+.
	app.Post(func() {
		ed.OpenSettingsPage(editor.SettingsPageShortcuts)
	})
	app.Settle()
	app.Press("id:shortcut-bind-" + string(editor.CmdFrameSelection))
	app.Settle()
	app.Key("f6") // captured as new binding rather than cycling regions
	app.Settle()
	if ed.ActiveRegionRing() != editor.RegionNone {
		t.Fatalf("F6 fired region ring during shortcut capture: %q", ed.ActiveRegionRing())
	}
	app.Press("id:shortcut-bind-" + string(editor.CmdFrameSelection))
	app.Settle()
	app.Key("mod+.") // suppressed while capturing a shortcut
	app.Settle()
	if agentStopped != 0 {
		t.Fatalf("mod+. fired OnStopAgent (%d times) during shortcut capture", agentStopped)
	}
	app.Key("escape") // cancel capture
	app.Settle()
	app.Post(func() {
		_ = ed.ResetShortcut(editor.CmdFrameSelection)
		ed.CloseSettings()
	})
	app.Settle()

	// Unmounting a focused text input (switching selection to "") must not stick IsTyping() == true.
	app.Focus("id:inspector-name")
	app.Settle()
	if !ed.IsTyping() {
		t.Fatal("expected IsTyping() true while inspector-name is focused")
	}
	app.Post(func() { ed.SelectObject("") })
	app.Settle()
	if ed.IsTyping() {
		t.Fatal("IsTyping() remained stuck true after focused inspector-name unmounted")
	}

	// Inspector scrub counts as IsDragging() and blocks F6 cycling.
	ed.SelectObject("hero")
	app.Focus("id:toolbar-play")
	app.Settle()
	app.Post(func() {
		if err := ed.BeginInspectorScrub(editor.InspectorScrubField{ObjectID: "hero", Field: "x", Step: 1}); err != nil {
			t.Errorf("BeginInspectorScrub: %v", err)
		}
	})
	app.Settle()
	if !ed.IsDragging() || ed.CanAutoMoveFocus() {
		t.Fatalf("during inspector scrub: IsDragging=%v CanAutoMoveFocus=%v", ed.IsDragging(), ed.CanAutoMoveFocus())
	}
	app.Key("f6")
	app.Settle()
	if fv := focusedView(app); fv == nil || fv.Props.TestID != "toolbar-play" {
		t.Fatalf("F6 moved focus during inspector scrub: got %v", fv)
	}
	app.Post(func() { ed.CancelInspectorScrub() })
	app.Settle()

	// Toggling appearance while Command Palette is open remounts workspace; closing and opening a fresh dialog must still restore focus cleanly.
	app.Focus("id:toolbar-palette")
	app.Settle()
	app.Key("enter")
	app.Settle()
	app.Post(func() {
		_ = ed.SetAppearance(editor.AppearanceDark)
		ed.SetCommandPaletteOpen(false)
	})
	app.Advance(250 * time.Millisecond)
	app.Settle()

	app.Focus("id:toolbar-history")
	app.Settle()
	app.Key("meta+/")
	app.Settle()
	app.Key("escape")
	app.Advance(250 * time.Millisecond)
	app.Settle()
	if fv := focusedView(app); fv == nil || fv.Props.TestID != "toolbar-history" {
		gotID := "<nil>"
		if fv != nil {
			gotID = fv.Props.TestID
		}
		t.Fatalf("after chrome remount + ShortcutsSheet Esc, focus = %q, want toolbar-history", gotID)
	}
}
