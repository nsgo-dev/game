package editor_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// #1005 Acceptance 1: ⌘, opens Settings (with or without an open project) and Esc closes it
// with keyboard focus returned to the exact control that had focus before opening.
func TestSettingsWindowCmdCommaAndEscRestoresFocus(t *testing.T) {
	dir := writeProjectSettingsFixture(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))

	// 1. Focus a toolbar control, press ⌘, to open Settings, and press Esc to restore exact focus.
	app.Focus("id:toolbar-history")
	app.Settle()
	if !viewOrDescendantFocused(app.Find("id:toolbar-history")) {
		t.Fatal("toolbar-history did not receive initial focus")
	}

	app.Key("mod+,")
	app.Settle()
	if !ed.SettingsOpen() || !app.Exists("id:settings-view") {
		t.Fatal("⌘, did not open Settings view")
	}
	// Sidebar has AGENT, PROJECT · game.yaml, and EDITOR · THIS COMPUTER groups in Spec order.
	sidebarTxt := allText(app.Find("id:settings-sidebar"))
	idxAgent := strings.Index(sidebarTxt, "AGENT")
	idxProject := strings.Index(sidebarTxt, "PROJECT · game.yaml")
	idxEditor := strings.Index(sidebarTxt, "EDITOR · THIS COMPUTER")
	if idxAgent < 0 || idxProject <= idxAgent || idxEditor <= idxProject {
		t.Fatalf("unexpected sidebar group order in %q", sidebarTxt)
	}
	if !app.Exists("id:settings-nav-general-meta") || !app.Exists("id:settings-nav-viewport-meta") {
		t.Fatal("expected meta lines on General and Viewport navigation entries")
	}

	// Pressing Escape while focused inside settings-search still closes Settings and restores toolbar-history.
	app.Focus("id:settings-search")
	app.Settle()
	app.Key("escape")
	app.Settle()
	if ed.SettingsOpen() || app.Exists("id:settings-view") {
		t.Fatal("Escape from settings-search did not close Settings view")
	}
	if !viewOrDescendantFocused(app.Find("id:toolbar-history")) {
		t.Fatal("Escape did not return focus to toolbar-history")
	}

	// 2. Without an open project, ⌘, still opens Settings and omits empty PROJECT · game.yaml group.
	edNoProj, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	appNoProj := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, edNoProj)
	}, nsgotest.Size(1280, 800), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))
	appNoProj.Focus("id:toolbar-palette")
	appNoProj.Settle()
	appNoProj.Key("mod+,")
	appNoProj.Settle()
	if !edNoProj.SettingsOpen() || !appNoProj.Exists("id:settings-view") {
		t.Fatal("⌘, did not open Settings without a project")
	}
	if appNoProj.Exists("id:settings-nav-group-project") {
		t.Fatal("empty PROJECT · game.yaml group should not be shown without a project")
	}
	appNoProj.Key("escape")
	appNoProj.Settle()
	if edNoProj.SettingsOpen() {
		t.Fatal("Escape did not close Settings without a project")
	}
	if !viewOrDescendantFocused(appNoProj.Find("id:toolbar-palette")) {
		t.Fatal("Escape did not return focus to toolbar-palette")
	}

	// 3. On the Start screen (top-level Settings mount), ⌘, opens Settings and Esc restores focus to the Start screen opener.
	appNoProj.Post(func() { edNoProj.SetStartScreenOpen(true) })
	appNoProj.Settle()
	appNoProj.Focus("id:start-filter-all")
	appNoProj.Settle()
	if !viewOrDescendantFocused(appNoProj.Find("id:start-filter-all")) {
		t.Fatal("start-filter-all did not receive initial focus")
	}
	appNoProj.Key("mod+,")
	appNoProj.Settle()
	if !edNoProj.SettingsOpen() || !appNoProj.Exists("id:settings-view") {
		t.Fatal("⌘, did not open top-level Settings from Start screen")
	}
	appNoProj.Key("escape")
	appNoProj.Settle()
	if edNoProj.SettingsOpen() || appNoProj.Exists("id:settings-view") {
		t.Fatal("Escape did not close top-level Settings on Start screen")
	}
	if !viewOrDescendantFocused(appNoProj.Find("id:start-filter-all")) {
		t.Fatal("Escape from top-level Settings did not return focus to start-filter-all")
	}
}

// #1005 Acceptance 2: Changing snap step, angle, grid, show agent changes, 3D navigation,
// interface size, and overlay legend writes the per-person YAML file, survives a restart,
// round-trips byte-for-byte unchanged, and refuses invalid fields/values.
func TestSettingsViewportSnapPersistsSurvivesRestartAndRoundTrips(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "user-config", "nsgo-game", "editor.yaml")
	projDir := writeProjectSettingsFixture(t)

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.LoadSettings(settingsPath); err != nil {
		t.Fatalf("LoadSettings (missing): %v", err)
	}
	if err := ed.OpenProject(projDir); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))

	// Open Settings -> General: change Interface size to Large.
	app.Key("mod+,")
	app.Settle()
	app.Press("id:editor-settings-size-large")
	app.Settle()
	if ed.InterfaceSize() != editor.InterfaceSizeLarge {
		t.Fatalf("InterfaceSize = %q, want large", ed.InterfaceSize())
	}

	// Switch to Viewport page and verify title, subtitle, scope chip, and policy column.
	app.Press("id:settings-nav-viewport")
	app.Settle()
	if ed.SettingsPage() != editor.SettingsPageViewport {
		t.Fatalf("SettingsPage = %q, want viewport", ed.SettingsPage())
	}
	if got := app.Find("id:settings-page-title").Text; got != "Viewport" {
		t.Fatalf("Viewport page title = %q, want Viewport", got)
	}
	if got := allText(app.Find("id:settings-page-scope")); !strings.Contains(got, "This computer") {
		t.Fatalf("Viewport scope chip = %q, want This computer", got)
	}
	if got := allText(app.Find("id:editor-settings-viewport")); !strings.Contains(got, "You only") {
		t.Fatalf("Viewport rows missing You only policy column: %q", got)
	}

	// Change Snap on, step = 16 px, angle = 30 deg, Grid on, Show agent changes off, 3D navigation = Blender, and overlay legend.
	app.Press("id:editor-settings-snap-switch")
	app.Clear("id:editor-settings-snap-step")
	app.Type("id:editor-settings-snap-step", "16")
	app.Submit("id:editor-settings-snap-step")
	app.Clear("id:editor-settings-snap-angle")
	app.Type("id:editor-settings-snap-angle", "30")
	app.Submit("id:editor-settings-snap-angle")
	app.Press("id:editor-settings-grid-switch")
	app.Press("id:editor-settings-agent-changes-switch")
	app.Press("id:editor-settings-nav3d-blender")
	app.Press("id:editor-settings-overlay-" + editor.OverlayConnections)
	app.Press("id:editor-settings-overlay-" + editor.OverlayNavigation)
	app.Settle()

	// Invalid snap step shows inline error without mutating GridSize.
	app.Clear("id:editor-settings-snap-step")
	app.Type("id:editor-settings-snap-step", "-5")
	app.Submit("id:editor-settings-snap-step")
	app.Settle()
	if ed.EffectiveGridSize() != 16 || !app.Exists("id:editor-settings-snap-error") {
		t.Fatalf("invalid snap step mutated grid size (%v) or omitted inline error", ed.EffectiveGridSize())
	}

	// Background rev bumps while typing in snap step must not clobber the in-progress draft or steal focus.
	app.Focus("id:editor-settings-snap-step")
	app.Clear("id:editor-settings-snap-step")
	app.Type("id:editor-settings-snap-step", "24")
	app.Post(func() {
		ed.SetShowGrid(false)
		ed.SetShowGrid(true)
	})
	app.Settle()
	if !viewOrDescendantFocused(app.Find("id:editor-settings-snap-step")) {
		t.Fatal("background rev bump stole focus from editor-settings-snap-step")
	}
	if got := app.Find("id:editor-settings-snap-step").Text; got != "24" {
		t.Fatalf("background rev bump clobbered snap step draft: got %q, want 24", got)
	}
	// Restore 16 before checking persisted file contents below.
	app.Clear("id:editor-settings-snap-step")
	app.Type("id:editor-settings-snap-step", "16")
	app.Submit("id:editor-settings-snap-step")
	app.Settle()

	// Verify YAML file on disk and lossless byte-for-byte round trip.
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read %s: %v", settingsPath, err)
	}
	text := string(raw)
	for _, wantSub := range []string{
		editor.SettingsSchemaComment,
		"version: 1",
		"interfaceSize: large",
		"snap:",
		"grid: true",
		"gridSize: 16",
		"rotation: true",
		"rotationStepDeg: 30",
		"showGrid: true",
		"showAgentChanges: false",
		"navigation3D: blender",
		"viewportOverlays:",
	} {
		if !strings.Contains(text, wantSub) {
			t.Fatalf("persisted editor.yaml missing %q:\n%s", wantSub, text)
		}
	}

	parsed, err := editor.ParseEditorSettings(raw)
	if err != nil {
		t.Fatalf("ParseEditorSettings: %v", err)
	}
	reencoded, err := editor.EncodeEditorSettings(parsed)
	if err != nil {
		t.Fatalf("EncodeEditorSettings: %v", err)
	}
	if !bytes.Equal(reencoded, raw) {
		t.Fatalf("load-save round trip changed file:\n--- got ---\n%s\n--- want ---\n%s", reencoded, raw)
	}

	// Verify invalid YAML values and unknown fields are refused with *loaderr.Error.
	badCases := map[string]string{
		"badInterfaceSize": "version: 1\nlanguage: en\nappearance: dark\ninterfaceSize: giant\n",
		"badNavigation3D":  "version: 1\nlanguage: en\nappearance: dark\nnavigation3D: unreal\n",
		"negGridSize":      "version: 1\nlanguage: en\nappearance: dark\nsnap:\n  gridSize: -8\n",
		"negRotStep":       "version: 1\nlanguage: en\nappearance: dark\nsnap:\n  rotationStepDeg: -15\n",
		"unknownSnapKey":   "version: 1\nlanguage: en\nappearance: dark\nsnap:\n  unknownKey: true\n",
	}
	for name, badYAML := range badCases {
		_, err := editor.ParseEditorSettings([]byte(badYAML))
		if err == nil {
			t.Fatalf("ParseEditorSettings(%s) succeeded, want error", name)
		}
		var le *loaderr.Error
		if !errors.As(err, &le) {
			t.Fatalf("ParseEditorSettings(%s) error %T is not *loaderr.Error", name, err)
		}
	}

	// Simulate a full restart: fresh Editor loading settingsPath, then opening a project without .game/editor.yaml.
	restarted, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.LoadSettings(settingsPath); err != nil {
		t.Fatalf("restarted.LoadSettings: %v", err)
	}
	if err := restarted.OpenProject(projDir); err != nil {
		t.Fatalf("restarted.OpenProject: %v", err)
	}
	if !restarted.SnapToGrid || restarted.EffectiveGridSize() != 16 || !restarted.SnapRotation || restarted.EffectiveRotationSnapDeg() != 30 || !restarted.ShowGrid {
		t.Fatalf("snap settings did not survive restart: %+v", restarted.SnapSettings())
	}
	if restarted.ShowAgentChanges() {
		t.Fatal("ShowAgentChanges=false did not survive restart")
	}
	if restarted.Navigation3D() != editor.Navigation3DBlender {
		t.Fatalf("Navigation3D = %q, want blender", restarted.Navigation3D())
	}
	if restarted.InterfaceSize() != editor.InterfaceSizeLarge {
		t.Fatalf("InterfaceSize = %q, want large", restarted.InterfaceSize())
	}
	if !restarted.ViewportOverlayEnabled(editor.OverlayConnections) || restarted.ViewportOverlayEnabled(editor.OverlayNavigation) {
		t.Fatalf("overlay legend choices did not survive restart: %v", restarted.ActiveViewportOverlays())
	}
}

// #1005 Acceptance 3: Typing in the search field matches setting labels and help across pages,
// narrows both navigation (without empty groups) and rows, and offers sending unmatched queries
// to the agent when the agent panel exists.
func TestSettingsSearchNarrowsNavigationRowsAndOffersAgent(t *testing.T) {
	dir := writeProjectSettingsFixture(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	var sent editor.SettingsAgentRequest
	ed.OnSettingsAgentRequest = func(req editor.SettingsAgentRequest) {
		sent = req
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))

	app.Key("mod+,")
	app.Settle()

	// 1. Searching "snap" narrows sidebar to EDITOR · THIS COMPUTER › Viewport (no empty AGENT or PROJECT groups)
	// and narrows Viewport rows to Snap + Grid (hiding 3D navigation and Show agent changes).
	app.Type("id:settings-search", "snap")
	app.Settle()
	if app.Exists("id:settings-nav-group-agent") || app.Exists("id:settings-nav-group-project") {
		t.Fatal("empty AGENT or PROJECT sidebar groups should be hidden while searching 'snap'")
	}
	if !app.Exists("id:settings-nav-viewport") || app.Exists("id:settings-nav-general") {
		t.Fatal("sidebar did not narrow to Viewport for query 'snap'")
	}
	if ed.SettingsPage() != editor.SettingsPageViewport {
		t.Fatalf("active page = %q, want auto-switched viewport for 'snap'", ed.SettingsPage())
	}
	if !app.Exists("id:editor-settings-snap") || !app.Exists("id:editor-settings-grid") {
		t.Fatal("expected Snap and Grid rows visible for query 'snap'")
	}
	if app.Exists("id:editor-settings-nav3d") || app.Exists("id:editor-settings-agent-changes") {
		t.Fatal("non-matching Viewport rows were not narrowed out for query 'snap'")
	}

	// 2. Clear search -> all groups and rows return.
	app.Press("id:settings-search-clear")
	app.Settle()
	if !app.Exists("id:settings-nav-group-agent") || !app.Exists("id:settings-nav-group-project") || !app.Exists("id:settings-nav-group-editor") {
		t.Fatal("clearing search did not restore all sidebar groups")
	}

	// 3. Searching by help text ("2 s") narrows to General › Save automatically only.
	app.Type("id:settings-search", "2 s")
	app.Settle()
	if ed.SettingsPage() != editor.SettingsPageGeneral {
		t.Fatalf("active page = %q, want general for help query '2 s'", ed.SettingsPage())
	}
	if !app.Exists("id:editor-settings-autosave") {
		t.Fatal("expected Save automatically row visible for '2 s'")
	}
	if app.Exists("id:editor-settings-language") || app.Exists("id:editor-settings-appearance") || app.Exists("id:editor-settings-interface-size") {
		t.Fatal("non-matching General rows were not narrowed out for '2 s'")
	}

	// 3b. Searching "automation" narrows to AGENT › Automations and switches to SettingsPageAutomations.
	app.Press("id:settings-search-clear")
	app.Type("id:settings-search", "automation")
	app.Settle()
	if !app.Exists("id:settings-nav-group-agent") || app.Exists("id:settings-nav-group-project") || app.Exists("id:settings-nav-group-editor") {
		t.Fatal("expected only AGENT sidebar group visible for query 'automation'")
	}
	if !app.Exists("id:settings-nav-automations") {
		t.Fatal("expected Automations nav item visible for query 'automation'")
	}
	if ed.SettingsPage() != editor.SettingsPageAutomations {
		t.Fatalf("active page = %q, want automations for query 'automation'", ed.SettingsPage())
	}

	// 4. Query with no matching setting hides all empty groups and offers sending to the agent.
	app.Press("id:settings-search-clear")
	app.Type("id:settings-search", "make the boss jump higher")
	app.Settle()
	if app.Exists("id:settings-nav-group-agent") || app.Exists("id:settings-nav-group-project") || app.Exists("id:settings-nav-group-editor") {
		t.Fatal("empty sidebar groups should not render when no setting matches")
	}
	if !app.Exists("id:settings-search-no-match") || !app.Exists("id:settings-search-send-agent") {
		t.Fatal("expected no-match state with Send to agent action when agent panel exists")
	}
	app.Press("id:settings-search-send-agent")
	app.Settle()
	if sent.Prompt != "make the boss jump higher" || sent.Source != "search" {
		t.Fatalf("Send to agent dispatched %+v, want prompt 'make the boss jump higher'", sent)
	}

	// 5. When the agent panel is disabled (AgentModeNone), no-match search does not offer sending to the agent.
	app.Post(func() {
		ed.SetConnectChoice(editor.AgentModeNone)
		ed.CompleteConnectDialog()
	})
	app.Settle()
	app.Type("id:settings-search", "zzzz no match")
	app.Settle()
	if !app.Exists("id:settings-search-no-match") {
		t.Fatal("expected no-match view with AgentModeNone")
	}
	if app.Exists("id:settings-search-send-agent") || app.Exists("id:settings-search-ask") {
		t.Fatal("agent send buttons must not appear when AgentModeNone")
	}
}

// #1005 Accessibility & keyboard: arrow-key navigation in the left list, switches have role
// switch with aria-checked, every General & Viewport row is reachable via Tab, and Interface size
// scales text and controls together.
func TestSettingsKeyboardNavigationSwitchesAriaAndTabReachability(t *testing.T) {
	dir := writeProjectSettingsFixture(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))

	app.Key("mod+,")
	app.Settle()

	// 1. Arrow-key navigation moves through the left navigation list and switches pages.
	app.Focus("id:settings-nav-models")
	app.Settle()
	wantOrder := []editor.SettingsPage{
		editor.SettingsPagePermissions,
		editor.SettingsPageBrief,
		editor.SettingsPageRules,
		editor.SettingsPageTools,
		editor.SettingsPageAutomations,
		editor.SettingsPageUsage,
		editor.SettingsPageProject,
		editor.SettingsPageInput,
		editor.SettingsPageAudio,
		editor.SettingsPageExport,
		editor.SettingsPageGeneral,
		editor.SettingsPageViewport,
		editor.SettingsPageShortcuts,
	}
	for _, want := range wantOrder {
		app.Key("down")
		app.Settle()
		if ed.SettingsPage() != want {
			t.Fatalf("ArrowDown landed on %q, want %q", ed.SettingsPage(), want)
		}
	}
	app.Key("up")
	app.Settle()
	if ed.SettingsPage() != editor.SettingsPageViewport {
		t.Fatalf("ArrowUp from shortcuts landed on %q, want viewport", ed.SettingsPage())
	}
	app.Key("home")
	app.Settle()
	if ed.SettingsPage() != editor.SettingsPageModels {
		t.Fatalf("Home landed on %q, want models", ed.SettingsPage())
	}
	app.Key("end")
	app.Settle()
	if ed.SettingsPage() != editor.SettingsPageShortcuts {
		t.Fatalf("End landed on %q, want shortcuts", ed.SettingsPage())
	}

	// 2. General page: switch role/aria-checked, Tab reachability, and Interface size scaling.
	app.Press("id:settings-nav-general")
	app.Settle()
	autoSw := app.Find("id:editor-settings-autosave-switch")
	if autoSw == nil || autoSw.Props.Role != core.RoleSwitch || !autoSw.Props.Checked {
		t.Fatalf("autosave switch role/checked = (%v, %v), want (RoleSwitch, true)", autoSw.Props.Role, autoSw.Props.Checked)
	}
	app.Press("id:editor-settings-autosave-switch")
	app.Settle()
	if app.Find("id:editor-settings-autosave-switch").Props.Checked {
		t.Fatal("autosave switch aria-checked did not flip to false")
	}

	defaultTitleSize := app.Find("id:settings-page-title").TextStyle.Font.Size
	defaultNavHeight := app.Find("id:settings-nav-general").Bounds().Size.Height
	app.Press("id:editor-settings-size-large")
	app.Settle()
	largeTitleSize := app.Find("id:settings-page-title").TextStyle.Font.Size
	largeNavHeight := app.Find("id:settings-nav-general").Bounds().Size.Height
	if !(largeTitleSize > defaultTitleSize && largeNavHeight > defaultNavHeight) {
		t.Fatalf("Interface size Large did not scale text and controls together: title %v->%v, navHeight %v->%v", defaultTitleSize, largeTitleSize, defaultNavHeight, largeNavHeight)
	}
	app.Press("id:editor-settings-size-default")
	app.Settle()

	generalTargets := []string{
		"editor-settings-lang-system",
		"editor-settings-appearance-system",
		"editor-settings-autosave-switch",
		"editor-settings-size-default",
		"editor-settings-close",
	}
	app.Focus("id:settings-search")
	seenGeneral := map[string]bool{}
	for range 60 {
		for _, id := range generalTargets {
			if viewOrDescendantFocused(app.Find("id:" + id)) {
				seenGeneral[id] = true
			}
		}
		app.Key("tab")
	}
	for _, id := range generalTargets {
		if !seenGeneral[id] {
			t.Errorf("General control %q was NOT reached by Tab", id)
		}
	}

	// 3. Viewport page: every switch has RoleSwitch + aria-checked, and every row is Tab-reachable.
	app.Press("id:settings-nav-viewport")
	app.Settle()
	switchChecks := []struct {
		id      string
		wantDef bool
	}{
		{"editor-settings-snap-switch", false},
		{"editor-settings-grid-switch", false},
		{"editor-settings-agent-changes-switch", true},
		{"editor-settings-overlay-" + editor.OverlayCollisionShapes, true},
		{"editor-settings-overlay-" + editor.OverlayConnections, false},
	}
	for _, sc := range switchChecks {
		v := app.Find("id:" + sc.id)
		if v == nil {
			t.Fatalf("missing switch %s", sc.id)
		}
		if v.Props.Role != core.RoleSwitch || v.Props.Checked != sc.wantDef {
			t.Fatalf("switch %s: role=%v checked=%v, want RoleSwitch/%v", sc.id, v.Props.Role, v.Props.Checked, sc.wantDef)
		}
	}

	viewportTargets := []string{
		"editor-settings-snap-switch",
		"editor-settings-snap-step",
		"editor-settings-snap-angle",
		"editor-settings-grid-switch",
		"editor-settings-agent-changes-switch",
		"editor-settings-nav3d-default",
		"editor-settings-overlay-" + editor.OverlayCollisionShapes,
		"editor-settings-overlay-" + editor.OverlayLabels,
		"editor-settings-close",
	}
	app.Focus("id:settings-search")
	seenViewport := map[string]bool{}
	for range 80 {
		for _, id := range viewportTargets {
			if viewOrDescendantFocused(app.Find("id:" + id)) {
				seenViewport[id] = true
			}
		}
		app.Key("tab")
	}
	for _, id := range viewportTargets {
		if !seenViewport[id] {
			t.Errorf("Viewport control %q was NOT reached by Tab", id)
		}
	}

	// Verify SettingsPageContext includes Viewport and Agent pages (including Automations).
	gotID, _, gotScope, gotFile := ed.SettingsPageContext()
	if gotID != "viewport" || gotScope != "This computer" || gotFile != "" {
		t.Fatalf("SettingsPageContext(viewport) = (%q, %q, %q), want (viewport, This computer, '')", gotID, gotScope, gotFile)
	}
	app.Post(func() { ed.OpenSettingsPage(editor.SettingsPageAutomations) })
	app.Settle()
	gotID, _, _, gotFile = ed.SettingsPageContext()
	if gotID != "automations" || gotFile != editor.AutomationsFileName {
		t.Fatalf("SettingsPageContext(automations) = (%q, %q), want (automations, %q)", gotID, gotFile, editor.AutomationsFileName)
	}
	if !slices.Contains(ed.ActiveViewportOverlays(), editor.OverlayCollisionShapes) {
		t.Fatal("expected default collision_shapes overlay active")
	}
}

// #1005 Acceptance 4: Real nsgotest screenshots on macOS of Settings › General, Viewport (Dark & Light),
// and search narrowing at 1440x900 beside docs/design/editor/Settings.dc.html, verifying Spec tokens only.
func TestSettingsWindowScreenshots(t *testing.T) {
	dir := writeProjectSettingsFixture(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetProjectName("Dungeon Run"); err != nil {
		t.Fatal(err)
	}
	ed.SetSnapToGrid(true)
	ed.SetGridSize(16)
	ed.SetSnapRotation(true)
	ed.SetRotationSnapDeg(15)
	ed.SetShowGrid(true)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))

	dark := editor.DarkSpecPalette()
	light := editor.LightSpecPalette()

	// 1. General (Dark)
	app.Key("mod+,")
	app.Settle()
	if got := app.Find("id:settings-view").Style.Background; got != dark.Bg {
		t.Fatalf("dark settings-view bg = %#x, want Spec Bg %#x", uint32(got), uint32(dark.Bg))
	}
	app.Screenshot("settings-general-dark")

	// 2. Viewport (Dark)
	app.Press("id:settings-nav-viewport")
	app.Settle()
	app.Screenshot("settings-viewport-dark")

	// 3. Search narrowed ("snap")
	app.Type("id:settings-search", "snap")
	app.Settle()
	app.Screenshot("settings-search-narrowed")
	app.Press("id:settings-search-clear")
	app.Settle()

	// 4. Viewport (Light)
	app.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   false,
		Locale: "en-US",
	})
	if got := app.Find("id:settings-view").Style.Background; got != light.Bg {
		t.Fatalf("light settings-view bg = %#x, want Spec Light Bg %#x", uint32(got), uint32(light.Bg))
	}
	app.Screenshot("settings-viewport-light")
}
