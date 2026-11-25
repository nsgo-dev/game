package editor_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func writeProjectSettingsFixture(t *testing.T) string {
	t.Helper()
	dir := writeEditorProject(t)

	// Add a second scene and a data resource so Autoload pickers list multiple project files.
	second := testSceneDoc()
	second.Name = "Hall"
	hallBytes, err := sceneformat.Encode(second)
	if err != nil {
		t.Fatal(err)
	}
	extra := map[string]string{
		"scenes/hall.yaml": string(hallBytes),
		"data/items.yaml":  "version: 1\nrows:\n  - id: potion\n",
	}
	for rel, content := range extra {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Include a second autoload scene and a missing plugin in game.yaml alongside comments.
	gameYAML := `# Hand-authored project file with comments.
version: 1
name: Demo
id: dev.example.demo
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: room
  scenes:
    - name: room
      path: scenes/room.yaml
    - name: hall
      path: scenes/hall.yaml
editor:
  plugins: [absent]
`
	if err := os.WriteFile(filepath.Join(dir, project.FileName), []byte(gameYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// #907 Acceptance 1: change main scene, display size, a plugin switch (and a synced nsgo.yaml field) in the Project Settings panel, save, read back game.yaml and nsgo.yaml, and undo each cleanly.
func TestProjectSettingsPanelEditSaveSyncAndUndo(t *testing.T) {
	dir := writeProjectSettingsFixture(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.RegisterPlugin(editor.Plugin{
		Name: "tools",
		Panels: []editor.Panel{{
			ID:    "info",
			Title: "Tools Info",
			Dock:  editor.DockRight,
			View: func(cx *core.Scope, _ *editor.Editor, _ *core.Signal[uint64]) core.Node {
				return core.Text{Content: "Tools active"}
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 900))

	if app.Exists("id:project-settings-panel") {
		t.Fatal("project settings panel should start closed")
	}

	// Open the Project Settings panel via the toolbar button.
	app.Press("id:toolbar-project-settings")
	app.Settle()
	if !app.Exists("id:project-settings-panel") {
		t.Fatal("pressing toolbar-project-settings did not open the panel")
	}

	// Before the first save, display the canonical YAML save notice mentioning comments.
	if !app.Exists("id:project-settings-canonical-notice") {
		t.Fatal("expected canonical YAML save notice before first save")
	}
	if notice := allText(app.Find("id:project-settings-canonical-notice")); !strings.Contains(strings.ToLower(notice), "canonical") || !strings.Contains(strings.ToLower(notice), "comment") {
		t.Fatalf("canonical save notice = %q, want mention of canonical form and comments", notice)
	}

	// 1) Change project Name (which syncs to nsgo.yaml) in General.
	app.Clear("id:project-settings-name")
	app.Type("id:project-settings-name", "Demo Deluxe")
	app.Submit("id:project-settings-name")
	app.Settle()
	if ed.Project.Name != "Demo Deluxe" {
		t.Fatalf("Project.Name = %q, want Demo Deluxe", ed.Project.Name)
	}

	// 2) Change window appearance (which also syncs to nsgo.yaml) in Window.
	app.Press("id:project-settings-tab-window")
	app.Settle()
	app.Press("id:project-settings-window-appearance")
	app.Press("role:option Dark")
	app.Settle()
	if ed.Project.Window.Appearance != "dark" {
		t.Fatalf("Window.Appearance = %q, want dark", ed.Project.Window.Appearance)
	}

	// 3) Enable virtual display and change display size to 320x180 in Display.
	app.Press("id:project-settings-tab-display")
	app.Settle()
	app.Press("id:project-settings-display-enabled")
	app.Settle()
	if ed.Project.Display == nil {
		t.Fatal("Display remained nil after enabling virtual display")
	}
	app.Clear("label:Display Virtual Width")
	app.Type("label:Display Virtual Width", "320")
	app.Submit("label:Display Virtual Width")
	app.Clear("label:Display Virtual Height")
	app.Type("label:Display Virtual Height", "180")
	app.Submit("label:Display Virtual Height")
	app.Settle()
	if ed.Project.Display.Width != 320 || ed.Project.Display.Height != 180 {
		t.Fatalf("Display size = (%v, %v), want (320, 180)", ed.Project.Display.Width, ed.Project.Display.Height)
	}

	// 4) Change main scene to "hall" in Autoload.
	app.Press("id:project-settings-tab-autoload")
	app.Settle()
	app.Press("id:project-settings-main-scene")
	app.Press("role:option hall")
	app.Settle()
	if ed.Project.Autoload == nil || ed.Project.Autoload.MainScene != "hall" {
		t.Fatalf("Autoload.MainScene = %+v, want hall", ed.Project.Autoload)
	}

	// 5) Toggle the registered "tools" plugin switch ON in Plugins, and verify missing plugin "absent" is listed.
	app.Press("id:project-settings-tab-plugins")
	app.Settle()
	if !app.Exists("id:project-settings-missing-plugin-absent") {
		t.Fatal("expected missing plugin 'absent' to be displayed in Plugins section")
	}
	app.Press("id:project-settings-plugin-switch-tools")
	app.Settle()
	if !ed.PluginEnabled("tools") {
		t.Fatal("expected plugin 'tools' to be enabled after toggling switch")
	}

	// Save via toolbar Save and read back game.yaml and nsgo.yaml from disk.
	app.Press("id:toolbar-save")
	app.Settle()

	// Canonical save notice disappears after the first save.
	if app.Exists("id:project-settings-canonical-notice") {
		t.Fatal("canonical save notice should hide after first save")
	}

	saved, err := project.Check(os.DirFS(dir))
	if err != nil {
		t.Fatalf("project.Check after Save: %v", err)
	}
	if saved.Name != "Demo Deluxe" {
		t.Fatalf("saved Name = %q, want Demo Deluxe", saved.Name)
	}
	if saved.Window.Appearance != "dark" {
		t.Fatalf("saved Window.Appearance = %q, want dark", saved.Window.Appearance)
	}
	if saved.Display == nil || saved.Display.Width != 320 || saved.Display.Height != 180 {
		t.Fatalf("saved Display = %+v, want 320x180", saved.Display)
	}
	if saved.Autoload == nil || saved.Autoload.MainScene != "hall" {
		t.Fatalf("saved Autoload = %+v, want mainScene hall", saved.Autoload)
	}
	if saved.Editor == nil || !slices.Equal(saved.Editor.Plugins, []string{"absent", "tools"}) {
		t.Fatalf("saved Editor.Plugins = %+v, want [absent tools]", saved.Editor)
	}

	nsgoBytes, err := os.ReadFile(filepath.Join(dir, project.NSGoFileName))
	if err != nil {
		t.Fatal(err)
	}
	nsgoText := string(nsgoBytes)
	if !strings.Contains(nsgoText, "name: Demo Deluxe\n") || !strings.Contains(nsgoText, "appearance: dark\n") || !strings.Contains(nsgoText, "target: native") {
		t.Fatalf("nsgo.yaml not synced properly:\n%s", nsgoText)
	}

	// Undo each change one by one and verify each restores cleanly:
	// 1. Undo plugin toggle
	app.Press("id:toolbar-undo")
	app.Settle()
	if ed.PluginEnabled("tools") {
		t.Fatal("Undo did not restore plugin 'tools' to off")
	}
	// 2. Undo mainScene change
	app.Press("id:toolbar-undo")
	app.Settle()
	if ed.Project.Autoload.MainScene != "room" {
		t.Fatalf("Undo did not restore mainScene: got %q, want room", ed.Project.Autoload.MainScene)
	}
	// 3. Undo display height
	app.Press("id:toolbar-undo")
	app.Settle()
	if ed.Project.Display == nil || ed.Project.Display.Height != 360 {
		t.Fatalf("Undo did not restore display height: got %+v", ed.Project.Display)
	}
	// 4. Undo display width
	app.Press("id:toolbar-undo")
	app.Settle()
	if ed.Project.Display == nil || ed.Project.Display.Width != 640 {
		t.Fatalf("Undo did not restore display width: got %+v", ed.Project.Display)
	}
	// 5. Undo display enabled
	app.Press("id:toolbar-undo")
	app.Settle()
	if ed.Project.Display != nil {
		t.Fatalf("Undo did not restore Display to nil: got %+v", ed.Project.Display)
	}
	// 6. Undo window appearance
	app.Press("id:toolbar-undo")
	app.Settle()
	if ed.Project.Window.Appearance != "" {
		t.Fatalf("Undo did not restore Window.Appearance: got %q", ed.Project.Window.Appearance)
	}
	// 7. Undo project name
	app.Press("id:toolbar-undo")
	app.Settle()
	if ed.Project.Name != "Demo" {
		t.Fatalf("Undo did not restore Project.Name: got %q, want Demo", ed.Project.Name)
	}

	// Saving after full Undo writes restored values to both game.yaml and nsgo.yaml.
	app.Press("id:toolbar-save")
	app.Settle()
	restored, err := project.Check(os.DirFS(dir))
	if err != nil {
		t.Fatalf("project.Check after Undo+Save: %v", err)
	}
	if restored.Name != "Demo" || restored.Window.Appearance != "" || restored.Display != nil || restored.Autoload.MainScene != "room" || !slices.Equal(restored.Editor.Plugins, []string{"absent"}) {
		t.Fatalf("restored game.yaml = %+v", restored)
	}
	nsgoAfterUndo, err := os.ReadFile(filepath.Join(dir, project.NSGoFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(nsgoAfterUndo), "name: Demo\n") || strings.Contains(string(nsgoAfterUndo), "appearance:") {
		t.Fatalf("nsgo.yaml after Undo+Save:\n%s", nsgoAfterUndo)
	}
}

// #907 Acceptance 2: an invalid value (such as an id with spaces) is refused in the panel with an error displayed next to the field and without mutating the project.
func TestProjectSettingsPanelRefusesInvalidValuesWithoutMutatingProject(t *testing.T) {
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
	}, nsgotest.Size(1280, 900))

	app.Press("id:toolbar-project-settings")
	app.Settle()

	// Enter an invalid ID containing spaces.
	app.Clear("id:project-settings-id")
	app.Type("id:project-settings-id", "dev example bad")
	app.Submit("id:project-settings-id")
	app.Settle()

	if ed.Project.ID != "dev.example.demo" {
		t.Fatalf("Project.ID mutated to %q after invalid input, want dev.example.demo", ed.Project.ID)
	}
	if ed.ProjectDirty() || ed.History.CanUndo() {
		t.Fatalf("invalid ID dirtied project (%v) or pushed history (%v)", ed.ProjectDirty(), ed.History.CanUndo())
	}
	if !app.Exists("id:project-settings-id-error") {
		t.Fatal("expected inline validation error next to ID field")
	}
	if msg := app.Find("id:project-settings-id-error").Text; !strings.Contains(msg, "invalid character") && !strings.Contains(msg, "id") {
		t.Fatalf("unexpected ID error message: %q", msg)
	}

	// Enter an invalid empty name and an appVersion with spaces.
	app.Clear("id:project-settings-name")
	app.Submit("id:project-settings-name")
	app.Settle()
	if ed.Project.Name != "Demo" || !app.Exists("id:project-settings-name-error") {
		t.Fatalf("empty name should be refused with inline error: name=%q hasErr=%v", ed.Project.Name, app.Exists("id:project-settings-name-error"))
	}

	app.Clear("id:project-settings-app-version")
	app.Type("id:project-settings-app-version", "1.0 beta")
	app.Submit("id:project-settings-app-version")
	app.Settle()
	if ed.Project.AppVersion != "1.0.0" || !app.Exists("id:project-settings-app-version-error") {
		t.Fatalf("spaced appVersion should be refused with inline error: version=%q hasErr=%v", ed.Project.AppVersion, app.Exists("id:project-settings-app-version-error"))
	}

	// Entering a valid ID clears its inline error and updates the project.
	app.Clear("id:project-settings-id")
	app.Type("id:project-settings-id", "dev.example.valid")
	app.Submit("id:project-settings-id")
	app.Settle()
	if ed.Project.ID != "dev.example.valid" {
		t.Fatalf("Project.ID = %q, want dev.example.valid", ed.Project.ID)
	}
	if app.Exists("id:project-settings-id-error") {
		t.Fatal("ID error did not clear after valid input")
	}

	// Window section: zero width is refused without mutating window width.
	app.Press("id:project-settings-tab-window")
	app.Settle()
	app.Clear("label:Window Width")
	app.Type("label:Window Width", "0")
	app.Submit("label:Window Width")
	app.Settle()
	if ed.Project.Window.Width != 640 || !app.Exists("id:project-settings-window-width-error") {
		t.Fatalf("zero window width should be refused with error: width=%v hasErr=%v", ed.Project.Window.Width, app.Exists("id:project-settings-window-width-error"))
	}

	// Display section: invalid barColor is refused with inline error.
	app.Press("id:project-settings-tab-display")
	app.Settle()
	app.Press("id:project-settings-display-enabled")
	app.Settle()
	app.Clear("id:project-settings-display-bar-color")
	app.Type("id:project-settings-display-bar-color", "not-a-color")
	app.Submit("id:project-settings-display-bar-color")
	app.Settle()
	if ed.Project.Display.BarColor != "" || !app.Exists("id:project-settings-display-bar-color-error") {
		t.Fatalf("invalid barColor should be refused with error: barColor=%q hasErr=%v", ed.Project.Display.BarColor, app.Exists("id:project-settings-display-bar-color-error"))
	}
}

func TestProjectSettingsWindowDisplayAutoloadAndPluginsAllFields(t *testing.T) {
	dir := writeProjectSettingsFixture(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}

	// Without a project, setters fail with ErrNoProject.
	if err := ed.SetProjectName("X"); !errors.Is(err, editor.ErrNoProject) {
		t.Fatalf("SetProjectName without project = %v, want ErrNoProject", err)
	}
	if err := ed.SetProjectDisplaySize(320, 180); !errors.Is(err, editor.ErrNoProject) {
		t.Fatalf("SetProjectDisplaySize without project = %v, want ErrNoProject", err)
	}
	if err := ed.SetProjectMainScene("room"); !errors.Is(err, editor.ErrNoProject) {
		t.Fatalf("SetProjectMainScene without project = %v, want ErrNoProject", err)
	}

	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if !ed.ProjectCanonicalSavePending() {
		t.Fatal("ProjectCanonicalSavePending should be true right after OpenProject")
	}

	// Verify file listers return project files excluding game.yaml and nsgo.yaml.
	sceneFiles, err := ed.ProjectSceneFiles()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(sceneFiles, []string{"data/items.yaml", "scenes/hall.yaml", "scenes/room.yaml"}) {
		t.Fatalf("ProjectSceneFiles = %v", sceneFiles)
	}
	resFiles, err := ed.ProjectResourceFiles()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(resFiles, []string{"data/items.yaml", "scenes/hall.yaml", "scenes/room.yaml"}) {
		t.Fatalf("ProjectResourceFiles = %v", resFiles)
	}

	// Window fields: title, size, limits, resizable, appearance.
	if err := ed.SetProjectWindowTitle("Custom Title"); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetProjectWindowSize(800, 600); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetProjectWindowLimits(320, 240, 1920, 1080); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetProjectWindowLimits(900, 240, 1920, 1080); err == nil {
		t.Fatal("SetProjectWindowLimits with minWidth > width succeeded")
	}
	if err := ed.SetProjectWindowResizable(false); err != nil {
		t.Fatal(err)
	}
	if ed.Project.Window.Resizable == nil || *ed.Project.Window.Resizable {
		t.Fatalf("Window.Resizable = %v, want false", ed.Project.Window.Resizable)
	}
	if err := ed.SetProjectWindowResizable(true); err != nil {
		t.Fatal(err)
	}
	if ed.Project.Window.Resizable != nil {
		t.Fatalf("Window.Resizable after re-enabling = %v, want nil (default true)", ed.Project.Window.Resizable)
	}
	if err := ed.SetProjectWindowAppearance("light"); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetProjectWindowAppearance("neon"); err == nil {
		t.Fatal("SetProjectWindowAppearance(neon) succeeded")
	}

	// Display fields: mode, integerScale, pixelSnap, barColor.
	if err := ed.SetProjectDisplayMode("fill"); err == nil {
		t.Fatal("SetProjectDisplayMode while display is nil succeeded")
	}
	if err := ed.SetProjectDisplaySize(320, 180); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetProjectDisplayMode("expand"); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetProjectDisplayMode("bogus"); err == nil {
		t.Fatal("SetProjectDisplayMode(bogus) succeeded")
	}
	if err := ed.SetProjectDisplayIntegerScale(true); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetProjectDisplayPixelSnap(true); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetProjectDisplayBarColor("#112233"); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetProjectDisplayBarColor("nope"); err == nil {
		t.Fatal("SetProjectDisplayBarColor(nope) succeeded")
	}

	// Autoload UI & Go operations: add resource picked from project files, add/update/remove scenes, remove missing plugin.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 900))

	app.Press("id:toolbar-project-settings")
	app.Press("id:project-settings-tab-autoload")
	app.Settle()

	// Add an autoload resource through the UI file picker.
	app.Type("id:project-settings-add-resource-name", "items")
	app.Press("id:project-settings-add-resource-path")
	app.Press("role:option data/items.yaml")
	app.Press("id:project-settings-add-resource")
	app.Settle()
	if got := ed.Project.Autoload.Resources; len(got) != 1 || got[0].Name != "items" || got[0].Path != "data/items.yaml" {
		t.Fatalf("Autoload.Resources = %+v, want [{items data/items.yaml}]", got)
	}

	// Duplicate scene name via UI shows inline error and does not mutate project.
	app.Type("id:project-settings-add-scene-name", "room")
	app.Press("id:project-settings-add-scene-path")
	app.Press("role:option scenes/hall.yaml")
	app.Press("id:project-settings-add-scene")
	app.Settle()
	if len(ed.Project.Autoload.Scenes) != 2 || !app.Exists("id:project-settings-add-scene-error") {
		t.Fatalf("duplicate autoload scene should be refused with inline error: scenes=%d hasErr=%v", len(ed.Project.Autoload.Scenes), app.Exists("id:project-settings-add-scene-error"))
	}

	// Rename main scene "room" -> "entry": mainScene follows automatically.
	app.Clear("id:project-settings-scene-0-name")
	app.Type("id:project-settings-scene-0-name", "entry")
	app.Submit("id:project-settings-scene-0-name")
	app.Settle()
	if ed.Project.Autoload.MainScene != "entry" || ed.Project.Autoload.Scenes[0].Name != "entry" {
		t.Fatalf("renaming main scene did not update mainScene: %+v", ed.Project.Autoload)
	}

	// Remove missing plugin "absent" from Plugins tab.
	app.Press("id:project-settings-tab-plugins")
	app.Settle()
	app.Press("id:project-settings-missing-plugin-remove-absent")
	app.Settle()
	if len(ed.MissingPlugins()) != 0 || ed.Project.Editor != nil {
		t.Fatalf("removing missing plugin failed: missing=%v editor=%+v", ed.MissingPlugins(), ed.Project.Editor)
	}

	// Save, verify ProjectCanonicalSavePending becomes false, then reopen and verify it resets to true.
	app.Press("id:toolbar-save")
	app.Settle()
	if ed.ProjectCanonicalSavePending() {
		t.Fatal("ProjectCanonicalSavePending should be false after Save")
	}
	saved, err := project.Check(os.DirFS(dir))
	if err != nil {
		t.Fatalf("project.Check: %v", err)
	}
	if saved.Window.Title != "Custom Title" || saved.Window.Width != 800 || saved.Window.MinWidth != 320 || saved.Window.MaxHeight != 1080 || saved.Window.Appearance != "light" {
		t.Fatalf("saved Window = %+v", saved.Window)
	}
	if saved.Display == nil || saved.Display.Mode != "expand" || !saved.Display.IntegerScale || !saved.Display.PixelSnap || saved.Display.BarColor != "#112233" {
		t.Fatalf("saved Display = %+v", saved.Display)
	}
	if saved.Autoload == nil || saved.Autoload.MainScene != "entry" || len(saved.Autoload.Resources) != 1 {
		t.Fatalf("saved Autoload = %+v", saved.Autoload)
	}

	// Play mode refuses project settings edits.
	app.Post(func() {
		if err := ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
		if err := ed.SetProjectName("Blocked"); !errors.Is(err, editor.ErrPlaying) {
			t.Errorf("SetProjectName while playing = %v, want ErrPlaying", err)
		}
		if err := ed.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
		if err := ed.OpenProject(dir); err != nil {
			t.Errorf("reopen OpenProject: %v", err)
		}
	})
	app.Settle()
	if !ed.ProjectCanonicalSavePending() || !app.Exists("id:project-settings-canonical-notice") {
		t.Fatal("reopening project should show canonical YAML save notice again")
	}

	// Close button hides the panel.
	app.Press("id:project-settings-close")
	app.Settle()
	if app.Exists("id:project-settings-panel") {
		t.Fatal("Close button did not hide project settings panel")
	}
}
