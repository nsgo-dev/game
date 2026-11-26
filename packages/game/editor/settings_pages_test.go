package editor_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// #1060: Toolbar Settings, Input, Audio, Export and Preferences triggers open the matching unified Settings pages,
// sidebar navigation switches cleanly between "PROJECT · game.yaml" and "EDITOR · THIS COMPUTER", and Escape closes Settings.
func TestSettingsPagesNavigationAndToolbarTriggers(t *testing.T) {
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
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true}))

	if ed.SettingsOpen() || app.Exists("id:settings-view") {
		t.Fatal("settings view should start closed")
	}

	// Toolbar triggers open the matching Settings pages instead of bottom panels.
	cases := []struct {
		btnID     string
		page      editor.SettingsPage
		panelID   string
		wantTitle string
		wantScope string
	}{
		{"toolbar-project-settings", editor.SettingsPageProject, "project-settings-panel", "Project", "Project · game.yaml"},
		{"toolbar-input-map", editor.SettingsPageInput, "input-map-panel", "Input map", "Project · game.yaml"},
		{"toolbar-audio-mixer", editor.SettingsPageAudio, "audio-mixer-panel", "Audio buses", "Project · game.yaml"},
		{"toolbar-export", editor.SettingsPageExport, "export-panel", "Export", "Project · game.yaml"},
		{"toolbar-editor-settings", editor.SettingsPageGeneral, "editor-settings-dialog", "General", "This computer"},
	}

	for _, tc := range cases {
		app.Press("id:" + tc.btnID)
		app.Settle()
		if !ed.SettingsOpen() || ed.SettingsPage() != tc.page {
			t.Fatalf("pressing %s: open=%v page=%q, want true/%q", tc.btnID, ed.SettingsOpen(), ed.SettingsPage(), tc.page)
		}
		if !app.Exists("id:settings-view") || !app.Exists("id:"+tc.panelID) {
			t.Fatalf("pressing %s: missing settings-view or %s", tc.btnID, tc.panelID)
		}
		if got := app.Find("id:settings-page-title").Text; got != tc.wantTitle {
			t.Fatalf("page title for %s = %q, want %q", tc.page, got, tc.wantTitle)
		}
		if got := allText(app.Find("id:settings-page-scope")); !strings.Contains(got, tc.wantScope) {
			t.Fatalf("page scope for %s = %q, want %q", tc.page, got, tc.wantScope)
		}
	}

	// Sidebar navigation switches pages cleanly.
	app.Press("id:settings-nav-project")
	app.Settle()
	if ed.SettingsPage() != editor.SettingsPageProject || !ed.ProjectSettingsOpen() {
		t.Fatalf("sidebar Project nav failed: page=%q", ed.SettingsPage())
	}
	if txt := allText(app.Find("id:settings-sidebar")); !strings.Contains(txt, "PROJECT · game.yaml") || !strings.Contains(txt, "EDITOR · THIS COMPUTER") {
		t.Fatalf("sidebar missing group headings: %q", txt)
	}
	if txt := allText(app.Find("id:project-settings-panel")); !strings.Contains(txt, "Agent may change") || !strings.Contains(txt, "You only") {
		t.Fatalf("Project page missing policy badges: %q", txt)
	}

	app.Press("id:settings-nav-input")
	app.Settle()
	if ed.SettingsPage() != editor.SettingsPageInput || !ed.InputMapOpen() {
		t.Fatalf("sidebar Input map nav failed: page=%q", ed.SettingsPage())
	}

	app.Press("id:settings-nav-audio")
	app.Settle()
	if ed.SettingsPage() != editor.SettingsPageAudio || !ed.AudioMixerOpen() {
		t.Fatalf("sidebar Audio buses nav failed: page=%q", ed.SettingsPage())
	}

	app.Press("id:settings-nav-export")
	app.Settle()
	if ed.SettingsPage() != editor.SettingsPageExport || !ed.ExportPanelOpen() {
		t.Fatalf("sidebar Export nav failed: page=%q", ed.SettingsPage())
	}

	// Escape closes Settings.
	app.Key("escape")
	app.Settle()
	if ed.SettingsOpen() || app.Exists("id:settings-view") {
		t.Fatal("Escape did not close Settings view")
	}
}

// #1060 Acceptance 1 & 2: edits on each of the four pages (Project, Input map, Audio buses, Export)
// save to game.yaml (and nsgo.yaml for Project Name) and undo cleanly.
func TestSettingsPagesEditSaveAndUndoAcrossAllFourPages(t *testing.T) {
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
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true}))

	// 1. Project page: change project Name -> Save writes both game.yaml and nsgo.yaml -> Undo restores both.
	app.Press("id:toolbar-project-settings")
	app.Settle()
	app.Clear("id:project-settings-name")
	app.Type("id:project-settings-name", "Dungeon Run")
	app.Submit("id:project-settings-name")
	app.Settle()

	// Also verify Window size and Main scene are directly editable on the Project overview page.
	app.Clear("id:project-settings-overview-window-width")
	app.Type("id:project-settings-overview-window-width", "960")
	app.Submit("id:project-settings-overview-window-width")
	app.Press("id:project-settings-overview-main-scene")
	app.Press("role:option hall")
	app.Settle()

	// 2. Input map page: add action "jump" and capture "space".
	app.Press("id:settings-nav-input")
	app.Settle()
	app.Type("id:input-new-action-name", "jump")
	app.Press("id:input-add-action")
	app.Press("id:input-capture-key")
	app.KeyDown("space")
	app.KeyUp("space")
	app.Settle()
	if !app.Exists("id:settings-input-row-0") {
		t.Fatal("expected action overview row on Input map page")
	}
	if got := allText(app.Find("id:settings-input-row-0")); !strings.Contains(got, "jump") || !strings.Contains(got, "space") {
		t.Fatalf("settings-input-row-0 = %q, want jump and space", got)
	}

	// 3. Audio buses page: add bus "music" and change its level.
	app.Press("id:settings-nav-audio")
	app.Settle()
	app.Type("id:mixer-new-bus-name", "music")
	app.Press("id:mixer-add-bus")
	app.Settle()
	if err := ed.SetAudioBusVolume("music", 0.5); err != nil {
		t.Fatal(err)
	}
	app.Settle()
	if got := app.Find("id:mixer-bus-0-volume-value").Text; !strings.Contains(got, "dB") {
		t.Fatalf("mixer-bus-0-volume-value = %q, want dB level readout", got)
	}

	// 4. Export page: add web preset.
	app.Press("id:settings-nav-export")
	app.Settle()
	app.Press("id:export-add-platform")
	app.Press("role:option web")
	app.Press("id:export-add-preset")
	app.Settle()

	// Save writes game.yaml and synced nsgo.yaml.
	app.Press("id:toolbar-save")
	app.Settle()

	saved, err := project.Check(os.DirFS(dir))
	if err != nil {
		t.Fatalf("project.Check after Save: %v", err)
	}
	if saved.Name != "Dungeon Run" || saved.Window.Width != 960 || saved.Autoload == nil || saved.Autoload.MainScene != "hall" {
		t.Fatalf("saved project metadata mismatch: %+v", saved)
	}
	if saved.Input == nil || len(saved.Input.Actions) != 1 || saved.Input.Actions[0].Name != "jump" {
		t.Fatalf("saved input mismatch: %+v", saved.Input)
	}
	if saved.Audio == nil || len(saved.Audio.Buses) != 1 || saved.Audio.Buses[0].Name != "music" {
		t.Fatalf("saved audio mismatch: %+v", saved.Audio)
	}
	if saved.Export == nil || len(saved.Export.Presets) != 1 || saved.Export.Presets[0].Platform != "web" {
		t.Fatalf("saved export mismatch: %+v", saved.Export)
	}
	nsgoBytes, err := os.ReadFile(filepath.Join(dir, project.NSGoFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(nsgoBytes), "name: Dungeon Run\n") {
		t.Fatalf("nsgo.yaml did not sync project name:\n%s", nsgoBytes)
	}

	// Undo all 8 edits (export preset, audio vol, audio bus, key binding, input action, main scene, window width, project name).
	for range 8 {
		app.Press("id:toolbar-undo")
	}
	app.Settle()

	if ed.Project.Name != "Demo" || ed.Project.Window.Width != 640 || ed.Project.Autoload.MainScene != "room" {
		t.Fatalf("Undo did not restore Project page edits: %+v", ed.Project)
	}
	if ed.Project.Input != nil || ed.Project.Audio != nil || ed.Project.Export != nil {
		t.Fatalf("Undo did not restore Input/Audio/Export edits: input=%+v audio=%+v export=%+v", ed.Project.Input, ed.Project.Audio, ed.Project.Export)
	}

	// Save after Undo writes restored name back to both game.yaml and nsgo.yaml.
	app.Press("id:toolbar-save")
	app.Settle()
	nsgoRestored, err := os.ReadFile(filepath.Join(dir, project.NSGoFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(nsgoRestored), "name: Demo\n") {
		t.Fatalf("nsgo.yaml did not restore project name after Undo+Save:\n%s", nsgoRestored)
	}
}

// #1060 Acceptance 3 & Export readiness: invalid values show inline errors without changing the project;
// Export page shows "Ready" vs "Set up" and a warning meta badge in the navigation sidebar.
func TestSettingsExportReadinessAndInlineValidation(t *testing.T) {
	dir := writeProjectSettingsFixture(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if err := ed.AddExportPreset("macos", "macos_arm64"); err != nil {
		t.Fatal(err)
	}
	if err := ed.AddExportPreset("web"); err != nil {
		t.Fatal(err)
	}
	if err := ed.AddExportPreset("ios"); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true}))

	app.Press("id:toolbar-export")
	app.Settle()

	if got := strings.TrimSpace(allText(app.Find("id:export-preset-readiness-macos"))); got != "Ready" {
		t.Fatalf("macos readiness = %q, want Ready", got)
	}
	if got := strings.TrimSpace(allText(app.Find("id:export-preset-readiness-web"))); got != "Ready" {
		t.Fatalf("web readiness = %q, want Ready", got)
	}
	if got := strings.TrimSpace(allText(app.Find("id:export-preset-readiness-ios"))); got != "Set up" {
		t.Fatalf("ios readiness = %q, want Set up", got)
	}
	if got := app.Find("id:settings-nav-export-meta").Text; got != "iOS" {
		t.Fatalf("settings-nav-export-meta = %q, want iOS warning badge", got)
	}

	// Invalid export output shows inline error without mutating the project.
	app.Type("id:export-output-input", "../escape")
	app.Press("id:export-output-apply")
	app.Settle()
	if ed.ExportOutput() != "" {
		t.Fatalf("invalid export output mutated project to %q", ed.ExportOutput())
	}
	if !app.Exists("id:export-output-error") {
		t.Fatal("expected inline validation error for invalid export output")
	}

	// Invalid window width on Project overview shows inline error without mutating project.
	app.Press("id:settings-nav-project")
	app.Settle()
	app.Clear("id:project-settings-overview-window-width")
	app.Type("id:project-settings-overview-window-width", "0")
	app.Submit("id:project-settings-overview-window-width")
	app.Settle()
	if ed.Project.Window.Width != 640 || !app.Exists("id:project-settings-overview-window-error") {
		t.Fatalf("zero window width should show inline error without mutating: width=%v hasErr=%v", ed.Project.Window.Width, app.Exists("id:project-settings-overview-window-error"))
	}
}

// #1060 Acceptance 4: Every control on the four project Settings pages is reachable with Tab,
// uses real buttons (no clickable core.View RoleButton divs without focus rings), and shows focus rings.
func TestSettingsFourPagesTabReachableWithFocusRings(t *testing.T) {
	dir := writeProjectSettingsFixture(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if err := ed.AddInputAction("move"); err != nil {
		t.Fatal(err)
	}
	if _, err := ed.AddInputBinding("move", project.Binding{Kind: project.KindKey, Key: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := ed.AddInputAction("jump"); err != nil {
		t.Fatal(err)
	}
	if _, err := ed.AddInputBinding("jump", project.Binding{Kind: project.KindKey, Key: "space"}); err != nil {
		t.Fatal(err)
	}
	if err := ed.AddAudioBus("music"); err != nil {
		t.Fatal(err)
	}
	if err := ed.AddAudioBus("sfx"); err != nil {
		t.Fatal(err)
	}
	if err := ed.AddExportPreset("macos", "macos_arm64"); err != nil {
		t.Fatal(err)
	}
	if err := ed.AddExportPreset("ios"); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true}))

	// Tab through each of the 4 pages and confirm required controls are reached by Tab and show focus rings.
	pageTargets := []struct {
		page     editor.SettingsPage
		controls []string
	}{
		{
			page: editor.SettingsPageProject,
			controls: []string{
				"settings-nav-project",
				"settings-nav-input",
				"settings-nav-audio",
				"settings-nav-export",
				"settings-nav-general",
				"settings-search",
				"project-settings-name",
				"project-settings-id",
				"project-settings-overview-window-width",
				"project-settings-overview-window-height",
				"project-settings-overview-main-scene",
				"project-settings-app-version",
				"project-settings-close",
			},
		},
		{
			page: editor.SettingsPageInput,
			controls: []string{
				"settings-input-row-0-edit",
				"input-new-action-name",
				"input-add-action",
				"input-action-0-select",
				"input-action-1-select",
				"input-action-0-delete",
				"input-capture-key",
				"input-binding-0-select",
				"input-binding-0-remove",
				"input-map-close",
			},
		},
		{
			page: editor.SettingsPageAudio,
			controls: []string{
				"mixer-master-slider",
				"mixer-audition-file",
				"mixer-audition-loop",
				"mixer-new-bus-name",
				"mixer-add-bus",
				"mixer-bus-0-select",
				"mixer-bus-1-select",
				"mixer-bus-0-volume",
				"mixer-bus-0-down",
				"mixer-bus-0-delete",
				"mixer-add-effect",
				"mixer-close",
			},
		},
		{
			page: editor.SettingsPageExport,
			controls: []string{
				"export-output-input",
				"export-output-apply",
				"export-channel-select",
				"export-notes-input",
				"export-dry-run",
				"export-build",
				"export-open-output",
				"export-add-platform",
				"export-add-preset",
				"export-target-macos-macos_arm64",
				"export-preset-remove-macos",
				"export-credits-refresh",
				"export-close",
			},
		},
	}

	for _, pt := range pageTargets {
		app.Post(func() { ed.OpenSettingsPage(pt.page) })
		app.Settle()

		// Focus the first sidebar nav button, then press Tab repeatedly and record every TestID that receives focus.
		app.Focus("id:settings-nav-project")
		seen := map[string]bool{}
		for step := 0; step < 120; step++ {
			for _, id := range pt.controls {
				if v := app.Find("id:" + id); viewOrDescendantFocused(v) {
					seen[id] = true
				}
			}
			app.Key("tab")
		}
		for _, id := range pt.controls {
			if !seen[id] {
				t.Errorf("page %q: control %q was NOT reached by Tab", pt.page, id)
			}
		}

		// Verify previously non-button clickable rows activate with keyboard Space/Return when focused.
		switch pt.page {
		case editor.SettingsPageInput:
			app.Focus("id:input-action-1-select")
			app.Key("enter")
			app.Settle()
			if ed.SelectedInputAction() != "jump" {
				t.Errorf("pressing Enter on focused input-action-1-select selected %q, want jump", ed.SelectedInputAction())
			}
			app.Focus("id:input-binding-0-select")
			app.Key("enter")
			app.Settle()
			if ed.SelectedInputBinding() != 0 {
				t.Errorf("pressing Enter on focused input-binding-0-select selected %d, want 0", ed.SelectedInputBinding())
			}
		case editor.SettingsPageAudio:
			app.Focus("id:mixer-bus-1-select")
			app.Key("enter")
			app.Settle()
			if ed.SelectedAudioBus() != "sfx" {
				t.Errorf("pressing Enter on focused mixer-bus-1-select selected %q, want sfx", ed.SelectedAudioBus())
			}
		}
	}
}

func viewOrDescendantFocused(v *nsgotest.View) bool {
	if v == nil {
		return false
	}
	if v.Focused {
		return true
	}
	for _, ch := range v.Children {
		if viewOrDescendantFocused(ch) {
			return true
		}
	}
	return false
}

// #1060 Acceptance 5: Real nsgotest screenshots on macOS of the four PROJECT · game.yaml Settings pages
// (`env -u GOROOT TMPDIR=/tmp go tool nsgo test -target macos -run TestSettingsProjectPagesScreenshots ./editor`).
func TestSettingsProjectPagesScreenshots(t *testing.T) {
	dir := writeProjectSettingsFixture(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	// Populate realistic project data matching Settings.dc.html.
	if err := ed.SetProjectName("Dungeon Run"); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetProjectID("dev.nsgo.dungeonrun"); err != nil {
		t.Fatal(err)
	}
	for _, act := range []struct {
		name string
		key  string
		btn  string
	}{
		{"move", "a", "dpadLeft"},
		{"jump", "space", "south"},
		{"attack", "j", "west"},
		{"pause", "escape", "start"},
	} {
		if err := ed.AddInputAction(act.name); err != nil {
			t.Fatal(err)
		}
		if _, err := ed.AddInputBinding(act.name, project.Binding{Kind: project.KindKey, Key: act.key}); err != nil {
			t.Fatal(err)
		}
		if _, err := ed.AddInputBinding(act.name, project.Binding{Kind: project.KindGamepadButton, Button: act.btn}); err != nil {
			t.Fatal(err)
		}
	}
	for _, bus := range []struct {
		name string
		vol  float32
	}{
		{"Music", 0.50},
		{"Effects", 0.71},
		{"Voice", 1.00},
	} {
		if err := ed.AddAudioBus(bus.name); err != nil {
			t.Fatal(err)
		}
		if err := ed.SetAudioBusVolume(bus.name, bus.vol); err != nil {
			t.Fatal(err)
		}
	}
	if err := ed.AddExportPreset("macos", "macos_arm64", "macos_x64"); err != nil {
		t.Fatal(err)
	}
	if err := ed.AddExportPreset("windows", "windows_x64"); err != nil {
		t.Fatal(err)
	}
	if err := ed.AddExportPreset("web"); err != nil {
		t.Fatal(err)
	}
	if err := ed.AddExportPreset("ios"); err != nil {
		t.Fatal(err)
	}
	if err := ed.SaveProject(); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true}))

	for _, shot := range []struct {
		page editor.SettingsPage
		name string
	}{
		{editor.SettingsPageProject, "settings-project"},
		{editor.SettingsPageInput, "settings-input-map"},
		{editor.SettingsPageAudio, "settings-audio-buses"},
		{editor.SettingsPageExport, "settings-export"},
	} {
		app.Post(func() { ed.OpenSettingsPage(shot.page) })
		app.Settle()
		app.Screenshot(shot.name)
	}
}

func TestSettingsVisibleFromStartScreenAndShortcutsEscapeCapture(t *testing.T) {
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	ed.SetStartScreenOpen(true)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 760), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   false,
		Locale: "en-US",
	}))

	// Opening Settings while Start Screen is active must render SettingsView at top level.
	app.Post(func() { ed.OpenSettingsPage(editor.SettingsPageGeneral) })
	app.Settle()
	if !app.Exists("id:settings-view") || !app.Exists("id:editor-settings-dialog") {
		t.Fatal("SettingsView did not render over Start Screen")
	}

	// Switch to Shortcuts page inside unified Settings; EditorSettingsOpen must stay true.
	app.Press("id:editor-settings-tab-shortcuts")
	if ed.SettingsPage() != editor.SettingsPageShortcuts {
		t.Fatalf("SettingsPage = %q, want shortcuts", ed.SettingsPage())
	}
	if !ed.EditorSettingsOpen() {
		t.Fatal("EditorSettingsOpen() = false on Shortcuts page, want true")
	}

	// Start key capture, then press Escape: must cancel capture WITHOUT closing SettingsView.
	app.Press("id:shortcut-bind-" + editor.CmdFrameSelection)
	if !app.Exists("id:shortcut-capture-" + editor.CmdFrameSelection) {
		t.Fatal("shortcut capture did not activate")
	}
	app.Key("Escape")
	if app.Exists("id:shortcut-capture-" + editor.CmdFrameSelection) {
		t.Fatal("Escape did not cancel shortcut capture")
	}
	if !ed.SettingsOpen() || !app.Exists("id:settings-view") {
		t.Fatal("Escape during shortcut capture unexpectedly closed SettingsView")
	}

	// Pressing editor-settings-close from Shortcuts closes Settings cleanly.
	app.Press("id:editor-settings-close")
	if ed.SettingsOpen() || app.Exists("id:settings-view") {
		t.Fatal("editor-settings-close did not close SettingsView from Shortcuts page")
	}
}

func TestSettingsSpecLightThemeLiveRepaint(t *testing.T) {
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
	}, nsgotest.Size(1280, 760), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   false,
		Locale: "en-US",
	}))

	light := editor.LightSpecPalette()
	dark := editor.DarkSpecPalette()

	app.Post(func() { ed.OpenSettingsPage(editor.SettingsPageProject) })
	app.Settle()
	if got := app.Find("id:settings-view").Style.Background; got != light.Bg {
		t.Fatalf("light settings-view bg = %#x, want %#x", uint32(got), uint32(light.Bg))
	}
	if got := app.Find("id:settings-sidebar").Style.Background; got != light.Panel {
		t.Fatalf("light settings-sidebar bg = %#x, want %#x", uint32(got), uint32(light.Panel))
	}

	// Switching live to Dark repaints the open SettingsView immediately.
	app.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	})
	if got := app.Find("id:settings-view").Style.Background; got != dark.Bg {
		t.Fatalf("dark settings-view bg = %#x, want %#x", uint32(got), uint32(dark.Bg))
	}
	if got := app.Find("id:settings-sidebar").Style.Background; got != dark.Panel {
		t.Fatalf("dark settings-sidebar bg = %#x, want %#x", uint32(got), uint32(dark.Panel))
	}
}

func TestSettingsExportAppleTeamIDCacheAndLifecycleReset(t *testing.T) {
	dir := writeProjectSettingsFixture(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if err := ed.AddExportPreset("ios"); err != nil {
		t.Fatal(err)
	}

	// Initial check caches missing team_id -> not ready.
	if ready, _ := ed.ExportPresetReadiness("ios"); ready {
		t.Fatal("expected ios preset not ready without team_id")
	}

	// Mutating nsgo.yaml on disk must NOT trigger re-reads until explicit refresh/save.
	nsgoPath := filepath.Join(dir, project.NSGoFileName)
	if err := os.WriteFile(nsgoPath, []byte("name: Test\nid: dev.example.test\nversion: 0.1.0\nteam_id: ABCDE12345\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ready, _ := ed.ExportPresetReadiness("ios"); ready {
		t.Fatal("ExportPresetReadiness re-read nsgo.yaml without cache refresh")
	}

	// Explicit CheckExportCredits refreshes the cache and picks up team_id.
	ed.CheckExportCredits()
	if ready, _ := ed.ExportPresetReadiness("ios"); !ready {
		t.Fatal("expected ios preset ready after CheckExportCredits with team_id")
	}

	// Symlink nsgo.yaml must be refused by non-symlink Lstat check.
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outside, []byte("team_id: SYMLINK123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(nsgoPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, nsgoPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	ed.CheckExportCredits()
	if ready, _ := ed.ExportPresetReadiness("ios"); ready {
		t.Fatal("expected symlink nsgo.yaml to be refused")
	}

	// Opening Demo while Settings is open must close Settings and clear cached readiness.
	ed.OpenSettingsPage(editor.SettingsPageExport)
	if !ed.SettingsOpen() {
		t.Fatal("expected SettingsOpen before OpenDemo")
	}
	if err := ed.OpenDemo(); err != nil {
		t.Fatal(err)
	}
	if ed.SettingsOpen() {
		t.Fatal("OpenDemo left SettingsOpen == true")
	}
}

func TestSettingsPlayClosesSettingsAndPageSwitchResetsCapture(t *testing.T) {
	dir := writeProjectSettingsFixture(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if err := ed.AddInputAction("jump"); err != nil {
		t.Fatal(err)
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 760), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))

	app.Post(func() {
		ed.OpenSettingsPage(editor.SettingsPageInput)
		if err := ed.BeginKeyCapture("jump", project.KindKey); err != nil {
			t.Errorf("BeginKeyCapture: %v", err)
		}
		// Switching to Project page must cancel input key capture.
		ed.OpenSettingsPage(editor.SettingsPageProject)
	})
	app.Settle()
	if ed.CapturingKey() {
		t.Fatal("switching away from Input map page did not cancel key capture")
	}
	if !app.Exists("id:settings-view") {
		t.Fatal("expected settings-view visible before Play")
	}

	// Starting Play mode hides SettingsView so the game viewport is unobstructed.
	app.Post(func() {
		if err := ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
	})
	app.Settle()
	if app.Exists("id:settings-view") {
		t.Fatal("Play() left settings-view visible over playing viewport")
	}
	app.Post(func() {
		_ = ed.Stop()
		// Opening General/Shortcuts and entering Play closes EditorSettingsOpen permanently.
		ed.OpenSettingsPage(editor.SettingsPageShortcuts)
		if err := ed.Play(); err != nil {
			t.Errorf("Play (shortcuts): %v", err)
		}
	})
	app.Settle()
	if ed.SettingsOpen() || ed.EditorSettingsOpen() {
		t.Fatal("Play() did not close Shortcuts/General Settings")
	}
	app.Post(func() { _ = ed.Stop() })
	app.Settle()
}
