package editor_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/theme"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/resource"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func desktopEnv() nsgotest.Option {
	return nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop})
}

// assertSpecButtonControl verifies a control is a real focusable button with an accessible label
// and meets Spec.dc.html's minimum 28×28 hit area.
func assertSpecButtonControl(t *testing.T, app *nsgotest.App, testID string) {
	t.Helper()
	v := app.Find("id:" + testID)
	if v.Props.Role != core.RoleButton {
		t.Fatalf("%s role = %v, want RoleButton", testID, v.Props.Role)
	}
	if strings.TrimSpace(v.Props.Label) == "" && strings.TrimSpace(allText(v)) == "" {
		t.Fatalf("%s has neither aria-label nor text", testID)
	}
	b := v.Bounds()
	if b.Size.Width < 27.5 || b.Size.Height < 27.5 {
		t.Fatalf("%s hit area = %.1f×%.1f, want >= 28×28", testID, b.Size.Width, b.Size.Height)
	}
}

// #1063 acceptance: Animation panel empty, loading, and error states plus keyboard Tab/arrow
// reachability, focus, and Space/Return activation on keys, events, and interpolation controls.
func TestAnimationPanelSpecStatesAndKeyboardActivation(t *testing.T) {
	ed := newTestEditor(t)
	if err := ed.SetComponentRegistry(physicsComponents(t)); err != nil {
		t.Fatal(err)
	}
	ed.SelectObject("hero")
	if err := ed.NewClip("hero", "EmptyClip", time.Second); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), desktopEnv())
	app.Settle()

	// Empty state shown when clip has no tracks or events.
	if !app.Exists("id:anim-empty") {
		t.Fatal("expected anim-empty state for clip without tracks or events")
	}

	// Loading state shown while ClipLoading is true.
	app.Post(func() { ed.SetClipLoading(true) })
	app.Settle()
	if !app.Exists("id:anim-loading") {
		t.Fatal("expected anim-loading state while ClipLoading is true")
	}
	app.Post(func() { ed.SetClipLoading(false) })
	app.Settle()
	if app.Exists("id:anim-loading") {
		t.Fatal("anim-loading still shown after ClipLoading cleared")
	}

	// Switch to a populated clip with tracks, keys, and an event.
	ed2 := animatedEditor(t)
	app2 := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed2)
	}, nsgotest.Size(1440, 900), desktopEnv())
	app2.Settle()

	if app2.Exists("id:anim-empty") {
		t.Fatal("anim-empty should not show when tracks exist")
	}

	// Verify real button roles and >= 28x28 hit targets on animation keys, events, and header controls.
	for _, id := range []string{"anim-play", "anim-loop", "anim-save", "anim-close", "anim-key-0-0", "anim-key-0-1", "anim-event-0", "anim-track-0-remove"} {
		assertSpecButtonControl(t, app2, id)
	}

	// Keyboard focus + Space activation on keyframe buttons (previously plain clickable Views).
	app2.Focus("id:anim-key-0-0")
	if !app2.Find("id:anim-key-0-0").Focused {
		t.Fatal("anim-key-0-0 did not take keyboard focus")
	}
	app2.Key("Space")
	app2.Settle()
	if tr, k, ok := ed2.SelectedClipKey(); !ok || tr != 0 || k != 0 {
		t.Fatalf("after Space on anim-key-0-0: selected = (%d, %d, %t), want (0, 0, true)", tr, k, ok)
	}

	// Tab moves to the next keyframe button and Space selects it.
	app2.Key("Tab")
	if !app2.Find("id:anim-key-0-1").Focused {
		t.Fatal("Tab from anim-key-0-0 did not move focus to anim-key-0-1")
	}
	app2.Key("Space")
	app2.Settle()
	if tr, k, ok := ed2.SelectedClipKey(); !ok || tr != 0 || k != 1 {
		t.Fatalf("after Tab+Space to anim-key-0-1: selected = (%d, %d, %t), want (0, 1, true)", tr, k, ok)
	}

	// Arrow keys move inside the horizontal Interpolation ToggleGroup and Space activates Smooth.
	app2.Focus("id:anim-interp-linear")
	if !app2.Find("id:anim-interp-linear").Focused {
		t.Fatal("anim-interp-linear did not take keyboard focus")
	}
	app2.Key("Right")
	if !app2.Find("id:anim-interp-smooth").Focused {
		t.Fatal("Right arrow did not move focus from anim-interp-linear to anim-interp-smooth")
	}
	app2.Key("Space")
	app2.Settle()
	if got := ed2.Clip().Tracks[0].Keys[1].Interp; got != resource.InterpSmooth {
		t.Fatalf("interp after Right+Space = %v, want InterpSmooth", got)
	}

	// Keyboard focus + Space on event marker button selects the event and enables Delete Event.
	app2.Focus("id:anim-event-0")
	if !app2.Find("id:anim-event-0").Focused {
		t.Fatal("anim-event-0 did not take keyboard focus")
	}
	app2.Key("Space")
	app2.Settle()
	if !app2.Enabled("id:anim-event-delete") {
		t.Fatal("anim-event-delete remained disabled after keyboard-activating anim-event-0")
	}

	// Error state: deleting a tracked object surfaces anim-error banner; Undo clears it.
	app2.Post(func() {
		if err := ed2.DeleteObject("slime"); err != nil {
			t.Error(err)
		}
	})
	app2.Settle()
	if !app2.Exists("id:anim-error") {
		t.Fatal("expected anim-error banner after deleting tracked object slime")
	}
	if got := allText(app2.Find("id:anim-error")); !strings.Contains(got, "slime") {
		t.Fatalf("anim-error text = %q, want mention of slime", got)
	}
	app2.Post(func() { ed2.Undo() })
	app2.Settle()
	if app2.Exists("id:anim-error") {
		t.Fatal("anim-error remained after undoing delete")
	}
}

// #1063 acceptance: Data Table empty (no rows & no filter matches), loading, and error states,
// plus keyboard focus and Space/Return activation.
func TestDataTablePanelSpecStatesAndKeyboardActivation(t *testing.T) {
	ed := newTestEditor(t)
	if err := ed.OpenDataTableYAML("data/empty.yaml", []byte("version: 1\nrows: []\n")); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), desktopEnv())
	app.Settle()

	// Empty state for zero-row table.
	if !app.Exists("id:datatable-empty") {
		t.Fatal("expected datatable-empty for zero-row table")
	}
	// Loading state.
	app.Post(func() { ed.SetDataTableLoading(true) })
	app.Settle()
	if !app.Exists("id:datatable-loading") {
		t.Fatal("expected datatable-loading while DataTableLoading is true")
	}
	app.Post(func() { ed.SetDataTableLoading(false) })
	app.Settle()

	// Open populated typed table to verify filter-empty, error + dismiss, and keyboard activation.
	if err := editor.RegisterDataTableType[testEnemyRow](ed, "data/*.yaml"); err != nil {
		t.Fatal(err)
	}
	app.Post(func() {
		if err := ed.OpenDataTableYAML("data/enemies.yaml", []byte(sampleEnemiesTable)); err != nil {
			t.Error(err)
		}
	})
	app.Settle()

	for _, id := range []string{"datatable-add-row", "datatable-save", "datatable-close", "datatable-col-hp", "datatable-row-0-up", "datatable-row-0-down", "datatable-row-0-delete"} {
		assertSpecButtonControl(t, app, id)
	}

	// Filter with no matches shows datatable-empty + Clear Filter button; keyboard Space clears it.
	app.Post(func() { ed.SetDataTableFilter("nonexistent_xyz") })
	app.Settle()
	if !app.Exists("id:datatable-empty") || !app.Exists("id:datatable-clear-filter") {
		t.Fatal("expected datatable-empty and datatable-clear-filter for unmatched filter")
	}
	app.Focus("id:datatable-clear-filter")
	app.Key("Space")
	app.Settle()
	if app.Exists("id:datatable-empty") || ed.DataTableFilter() != "" {
		t.Fatalf("Clear Filter button did not clear filter: %q", ed.DataTableFilter())
	}

	// Error state + dismiss via keyboard.
	app.Post(func() {
		_ = ed.SetDataTableCellText(0, "hp", "bad_int")
	})
	app.Settle()
	if !app.Exists("id:datatable-error") || !app.Exists("id:datatable-error-dismiss") {
		t.Fatal("expected datatable-error banner and dismiss button")
	}
	app.Focus("id:datatable-error-dismiss")
	app.Key("Space")
	app.Settle()
	if app.Exists("id:datatable-error") || ed.DataTableError() != nil {
		t.Fatal("datatable-error-dismiss did not clear error banner")
	}

	// Keyboard activation on sort column header and Add Row.
	app.Focus("id:datatable-col-hp")
	app.Key("Space")
	app.Settle()
	if col, order := ed.DataTableSort(); col != "hp" || order != editor.SortAsc {
		t.Fatalf("after Space on datatable-col-hp: sort = (%q, %v), want (hp, asc)", col, order)
	}
	app.Focus("id:datatable-add-row")
	app.Key("Space")
	app.Settle()
	if got := ed.DataTable().RowCount(); got != 4 {
		t.Fatalf("RowCount after keyboard Space on datatable-add-row = %d, want 4", got)
	}
}

// #1063 acceptance: Localization panel empty, loading, error, missing-translation icon/text,
// and keyboard activation.
func TestLocalizationPanelSpecStatesAndKeyboardActivation(t *testing.T) {
	ed := newTestEditor(t)
	if err := ed.NewLocalizations("", "en", "zh-Hans"); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), desktopEnv())
	app.Settle()

	// Empty state when locales exist but no keys exist.
	if !app.Exists("id:l10n-empty") {
		t.Fatal("expected l10n-empty when no translation keys exist")
	}

	// Loading state.
	app.Post(func() { ed.SetLocalizationLoading(true) })
	app.Settle()
	if !app.Exists("id:l10n-loading") {
		t.Fatal("expected l10n-loading while LocalizationLoading is true")
	}
	app.Post(func() { ed.SetLocalizationLoading(false) })
	app.Settle()

	// Error state: saving with empty directory fails and displays l10n-error banner with dismiss button.
	app.Press("id:l10n-save")
	app.Settle()
	if !app.Exists("id:l10n-error") || !app.Exists("id:l10n-error-dismiss") {
		t.Fatal("expected l10n-error banner when saving without a localization directory")
	}
	app.Focus("id:l10n-error-dismiss")
	app.Key("Space")
	app.Settle()
	if app.Exists("id:l10n-error") || ed.LocalizationError() != nil {
		t.Fatal("l10n-error-dismiss did not clear error banner")
	}

	// Adding a key clears empty state and shows missing-translation badge with explicit text (not color alone).
	app.Post(func() {
		if err := ed.AddLocalizationKey("menu.play", false); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	if app.Exists("id:l10n-empty") {
		t.Fatal("l10n-empty should not show once a key is added")
	}
	missingBadge := app.Find("id:l10n-missing-zh-Hans-menu.play")
	if !strings.Contains(allText(missingBadge), "Missing") {
		t.Fatalf("missing badge text = %q, want non-color Missing label", allText(missingBadge))
	}

	for _, id := range []string{"l10n-add-key-plural", "l10n-add-key", "l10n-add-locale", "l10n-save", "l10n-close", "l10n-remove-locale-zh-Hans", "l10n-remove-key-menu.play"} {
		assertSpecButtonControl(t, app, id)
	}

	// Keyboard focus + Space on Remove Key removes the row.
	app.Focus("id:l10n-remove-key-menu.play")
	if !app.Find("id:l10n-remove-key-menu.play").Focused {
		t.Fatal("l10n-remove-key-menu.play did not take focus")
	}
	app.Key("Space")
	app.Settle()
	if len(ed.LocalizationKeys()) != 0 {
		t.Fatalf("keys after Space on remove-key = %v, want empty", ed.LocalizationKeys())
	}
}

// #1063 acceptance: Search & References panel empty, loading, and error states plus keyboard
// arrow/Tab navigation and Space/Return activation on result and reference rows.
func TestSearchPanelSpecStatesAndKeyboardActivation(t *testing.T) {
	dir := writeSearchFixture(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed.WaitForProblems)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), desktopEnv())

	app.Post(func() {
		if err := ed.OpenProject(dir); err != nil {
			t.Error(err)
			return
		}
		ed.SetSearchOpen(true)
	})
	app.Settle()

	// Empty state when query is blank.
	if !app.Exists("id:search-empty") {
		t.Fatal("expected search-empty with blank query")
	}

	// Loading state.
	app.Post(func() { ed.SetSearchLoading(true) })
	app.Settle()
	if !app.Exists("id:search-loading") {
		t.Fatal("expected search-loading while SearchLoading is true")
	}
	app.Post(func() { ed.SetSearchLoading(false) })
	app.Settle()

	// Empty state when query has zero matches.
	app.Post(func() { _ = ed.SetSearchQuery("no_such_symbol_zzz") })
	app.Settle()
	if !app.Exists("id:search-empty") {
		t.Fatal("expected search-empty when query has zero matches")
	}

	// Search for goblin_king -> row is a real button, keyboard-focusable and Space-activatable.
	app.Post(func() { _ = ed.SetSearchQuery("goblin_king") })
	app.Settle()
	for _, id := range []string{"search-tab-search", "search-refresh", "search-close", "search-result-0"} {
		assertSpecButtonControl(t, app, id)
	}
	app.Focus("id:search-result-0")
	if !app.Find("id:search-result-0").Focused {
		t.Fatal("search-result-0 did not take keyboard focus")
	}
	app.Key("Space")
	app.Settle()
	if ed.SelectedID != "goblin_king" || ed.ScenePath != "scenes/dungeon.scene.yaml" {
		t.Fatalf("after Space on search-result-0: ScenePath=%q SelectedID=%q", ed.ScenePath, ed.SelectedID)
	}

	// Find references -> arrow keys switch between Search & References tabs in ToggleGroup,
	// and keyboard Space activates refs-row-0.
	app.Post(func() {
		_, _ = ed.ShowReferences("materials/stone.material.yaml")
		ed.SetSearchTab(editor.SearchTabSearch)
	})
	app.Settle()
	app.Focus("id:search-tab-search")
	app.Key("Right")
	if !app.Find("id:search-tab-refs").Focused {
		t.Fatal("Right arrow did not move focus from search-tab-search to search-tab-refs")
	}
	app.Key("Space")
	app.Settle()
	if ed.SearchTab() != editor.SearchTabReferences {
		t.Fatalf("SearchTab after Right+Space = %v, want SearchTabReferences", ed.SearchTab())
	}
	assertSpecButtonControl(t, app, "refs-row-0")
	app.Focus("id:refs-row-0")
	app.Key("Space")
	app.Settle()

	// References empty state for unreferenced file (game.yaml).
	app.Post(func() { _, _ = ed.ShowReferences(project.FileName) })
	app.Settle()
	if !app.Exists("id:refs-empty") {
		t.Fatal("expected refs-empty for unreferenced file")
	}

	// Error state: removing the project directory and refreshing surfaces search-error + retry button.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	app.Post(func() { _ = ed.RefreshSearch() })
	app.Settle()
	if !app.Exists("id:search-error") || !app.Exists("id:search-error-retry") {
		t.Fatal("expected search-error banner and retry button after failing project scan")
	}
}

// #1063 acceptance: Problems panel keyboard focus + activation on problem rows and refresh button.
func TestProblemsPanelSpecKeyboardActivation(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFiles(t, dir, map[string]string{
		project.FileName:          fixtureProjectYAML,
		project.NSGoFileName:      fixtureNSGoYAML,
		"art/actors.json":         fixtureAtlasJSON,
		"scenes/room.scene.yaml":  fixtureBrokenFrameScene,
		"scenes/other.scene.yaml": fixtureCleanOtherScene,
	}, map[string][]byte{
		"art/actors.png": encodeTestPNG(t, 32, 16),
	})

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed.WaitForProblems)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), desktopEnv())

	app.Post(func() {
		if err := ed.OpenProject(dir); err != nil {
			t.Error(err)
			return
		}
		_ = ed.OpenScene("scenes/other.scene.yaml")
	})
	app.WaitUntil(10*time.Second, func() bool {
		return !ed.CheckingProblems() && ed.ProblemCount() == 1
	})
	app.Settle()

	for _, id := range []string{"problems-refresh", "problems-row-0"} {
		assertSpecButtonControl(t, app, id)
	}

	// Keyboard focus + Space on problems-row-0 opens scenes/room.scene.yaml.
	app.Focus("id:problems-row-0")
	if !app.Find("id:problems-row-0").Focused {
		t.Fatal("problems-row-0 did not take keyboard focus")
	}
	app.Key("Space")
	app.Settle()
	if ed.ScenePath != "scenes/room.scene.yaml" {
		t.Fatalf("ScenePath after Space on problems-row-0 = %q, want scenes/room.scene.yaml", ed.ScenePath)
	}

	// Clean project empty state is already covered in TestProblemsPanelMissingAtlasFrameFixAndSave;
	// check that the Check button takes keyboard focus cleanly.
	app.Focus("id:problems-refresh")
	if !app.Find("id:problems-refresh").Focused {
		t.Fatal("problems-refresh did not take keyboard focus")
	}
}

// #1063 + #1192 regression: all five bottom panels must follow Spec Light and Dark palettes live.
func TestBottomPanelsSpecLightThemeParity(t *testing.T) {
	dir := writeSearchFixture(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed.WaitForProblems)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 1200), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   false,
		Locale: "en-US",
	}))

	app.Post(func() {
		if err := ed.OpenProject(dir); err != nil {
			t.Error(err)
			return
		}
		ed.SetSearchOpen(true)
		if err := ed.OpenDataTableYAML("data/items.yaml", []byte("version: 1\nrows:\n  - name: Sword\n    hp: 10\n")); err != nil {
			t.Error(err)
			return
		}
		if err := ed.NewLocalizations("", "en"); err != nil {
			t.Error(err)
			return
		}
		if err := ed.AddLocalizationKey("menu.play", false); err != nil {
			t.Error(err)
			return
		}
		if err := ed.NewClip("hero", "Idle", time.Second); err != nil {
			t.Error(err)
			return
		}
	})
	app.Settle()

	assertPalette := func(label string, want theme.Palette) {
		t.Helper()
		panels := []struct {
			key string
			id  string
		}{
			{editor.PanelProblems, "problems-panel"},
			{editor.PanelSearch, "search-panel"},
			{editor.PanelDataTable, "datatable-panel"},
			{editor.PanelLocalization, "l10n-panel"},
			{editor.PanelAnimation, "anim-panel"},
		}
		for _, p := range panels {
			app.Press("id:dock-tab-" + p.key)
			app.Settle()
			v := app.Find("id:" + p.id)
			if v.Style.Background != want.Panel {
				t.Fatalf("%s: %s background = #%08X, want #%08X", label, p.id, uint32(v.Style.Background), uint32(want.Panel))
			}
			if v.Style.BorderColors.Top != want.Line {
				t.Fatalf("%s: %s top border = #%08X, want #%08X", label, p.id, uint32(v.Style.BorderColors.Top), uint32(want.Line))
			}
		}
		playBtn := app.Find("id:anim-play")
		if playBtn.Style.Background != want.TextPrimary {
			t.Fatalf("%s: anim-play background = #%08X, want #%08X", label, uint32(playBtn.Style.Background), uint32(want.TextPrimary))
		}
	}

	assertPalette("light", theme.Light())

	app.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	})
	app.Settle()
	assertPalette("dark", theme.Dark())
}

// #1063 regression: anim-play and datatable column sort icons must be memoized so unrelated rev ticks cause 0 icon remounts.
func TestBottomPanelsMemoizedIconsNoRemountOnUnrelatedTicks(t *testing.T) {
	ed := animatedEditor(t)
	if err := ed.OpenDataTableYAML("data/enemies.yaml", []byte(sampleEnemiesTable)); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 1200), desktopEnv())
	app.Settle()

	firstIconID := func(buttonID string) uint64 {
		t.Helper()
		btn := app.Find("id:" + buttonID)
		var findIcon func(v *nsgotest.View) (uint64, bool)
		findIcon = func(v *nsgotest.View) (uint64, bool) {
			if v.Kind == nsgotest.KindCanvas || v.Kind == nsgotest.KindImage {
				return uint64(v.ID), true
			}
			for _, ch := range v.Children {
				if id, ok := findIcon(ch); ok {
					return id, true
				}
			}
			return 0, false
		}
		id, ok := findIcon(btn)
		if !ok {
			t.Fatalf("no icon child found under %s", buttonID)
		}
		return id
	}

	// Switch to Animation tab and verify anim-play icon does not remount on unrelated rev ticks.
	app.Press("id:dock-tab-" + editor.PanelAnimation)
	app.Settle()
	playIconBefore := firstIconID("anim-play")

	app.Post(func() {
		ed.SeekClip(250 * time.Millisecond)
		_ = ed.SetClipName("Renamed")
	})
	app.Settle()
	if got := firstIconID("anim-play"); got != playIconBefore {
		t.Fatalf("anim-play icon remounted on unrelated rev ticks: id %d -> %d", playIconBefore, got)
	}
	app.Post(func() {
		_ = ed.PlayClip()
	})
	app.Settle()
	if !ed.ClipPlaying() {
		t.Fatal("expected ClipPlaying() to be true")
	}
	if got := firstIconID("anim-play"); got == playIconBefore {
		t.Fatal("expected anim-play icon to update when ClipPlaying changed")
	}
	app.Post(ed.PauseClip)
	app.Settle()

	// Switch to Data Table tab and verify datatable-col-hp icon does not remount on unrelated rev ticks.
	app.Press("id:dock-tab-" + editor.PanelDataTable)
	app.Settle()
	colIconBefore := firstIconID("datatable-col-hp")

	app.Post(func() {
		_ = ed.SetDataTableCellText(0, "hp", "42")
	})
	app.Settle()
	if got := firstIconID("datatable-col-hp"); got != colIconBefore {
		t.Fatalf("datatable-col-hp icon remounted on unrelated rev ticks: id %d -> %d", colIconBefore, got)
	}
	app.Post(func() {
		ed.SetDataTableSort("hp", editor.SortAsc)
	})
	app.Settle()
	if got := firstIconID("datatable-col-hp"); got == colIconBefore {
		t.Fatal("expected datatable-col-hp icon to update when sort order changed")
	}
}

// #1063 regression: ClipLoading and SearchLoading reset on transitions, and Play mode disables anim-save & anim-scrub.
func TestBottomPanelsLoadingResetsAndPlayModeGuards(t *testing.T) {
	dir := writeSearchFixture(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed.WaitForProblems)
	ed.ProjectAssets = true
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	// ClipLoading clears on NewClip, CloseClip, and OpenDemo.
	if err := ed.NewClip("hero", "Walk", time.Second); err != nil {
		t.Fatal(err)
	}
	ed.SetClipLoading(true)
	if !ed.ClipLoading() {
		t.Fatal("expected ClipLoading() == true")
	}
	if err := ed.CloseClip(); err != nil {
		t.Fatal(err)
	}
	if ed.ClipLoading() {
		t.Fatal("expected ClipLoading() cleared on CloseClip")
	}
	if err := ed.NewClip("hero", "Walk", time.Second); err != nil {
		t.Fatal(err)
	}
	ed.SetClipLoading(true)
	if err := ed.NewClip("hero", "Run", time.Second); err != nil {
		t.Fatal(err)
	}
	if ed.ClipLoading() {
		t.Fatal("expected ClipLoading() cleared on NewClip")
	}

	// SearchLoading clears when SetSearchQuery, RefreshSearch, or ShowReferences finishes.
	ed.SetSearchOpen(true)
	ed.SetSearchLoading(true)
	if err := ed.SetSearchQuery("goblin_king"); err != nil {
		t.Fatal(err)
	}
	if ed.SearchLoading() {
		t.Fatal("expected SearchLoading() cleared after SetSearchQuery")
	}
	ed.SetSearchLoading(true)
	if err := ed.RefreshSearch(); err != nil {
		t.Fatal(err)
	}
	if ed.SearchLoading() {
		t.Fatal("expected SearchLoading() cleared after RefreshSearch")
	}
	ed.SetSearchLoading(true)
	if _, err := ed.ShowReferences("materials/stone.material.yaml"); err != nil {
		t.Fatal(err)
	}
	if ed.SearchLoading() {
		t.Fatal("expected SearchLoading() cleared after ShowReferences")
	}

	// Play mode disables anim-save and anim-scrub.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), desktopEnv())
	app.Settle()

	if !app.Enabled("id:anim-save") || !app.Enabled("id:anim-scrub") {
		t.Fatal("expected anim-save and anim-scrub enabled while not playing")
	}
	app.Post(func() {
		if err := ed.Play(); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	if app.Enabled("id:anim-save") {
		t.Fatal("expected anim-save disabled during Play mode")
	}
	if app.Enabled("id:anim-scrub") {
		t.Fatal("expected anim-scrub disabled during Play mode")
	}
	app.Post(func() { _ = ed.Stop() })
	app.Settle()

	// Switching to a demo clears both clipLoading and searchLoading.
	app.Post(func() {
		ed.SetClipLoading(true)
		ed.SetSearchLoading(true)
		if err := ed.OpenDemo(); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	if ed.ClipLoading() || ed.SearchLoading() {
		t.Fatalf("expected OpenDemo to clear loading flags: clip=%t search=%t", ed.ClipLoading(), ed.SearchLoading())
	}
}

// #1063 regression: typing/clearing a filter on a 0-row Data Table updates datatable-empty between "No rows yet" and "No matching rows".
func TestDataTableZeroRowFilterEmptyStateToggle(t *testing.T) {
	ed := newTestEditor(t)
	if err := ed.OpenDataTableYAML("data/empty.yaml", []byte("version: 1\nrows: []\n")); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), desktopEnv())
	app.Settle()

	if got := allText(app.Find("id:datatable-empty")); !strings.Contains(got, "No rows yet") {
		t.Fatalf("0-row unfiltered empty state = %q, want 'No rows yet'", got)
	}
	if !app.Exists("id:datatable-empty-add") {
		t.Fatal("expected datatable-empty-add when 0-row table is unfiltered")
	}

	app.Post(func() { ed.SetDataTableFilter("dragon") })
	app.Settle()
	if got := allText(app.Find("id:datatable-empty")); !strings.Contains(got, "No matching rows") {
		t.Fatalf("0-row filtered empty state = %q, want 'No matching rows'", got)
	}
	if !app.Exists("id:datatable-clear-filter") {
		t.Fatal("expected datatable-clear-filter when 0-row table is filtered")
	}

	app.Press("id:datatable-clear-filter")
	app.Settle()
	if got := allText(app.Find("id:datatable-empty")); !strings.Contains(got, "No rows yet") {
		t.Fatalf("0-row cleared-filter empty state = %q, want 'No rows yet'", got)
	}
}
