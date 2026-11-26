package editor_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func findMenuItem(items []nsgotest.MenuItem, path ...string) (nsgotest.MenuItem, bool) {
	if len(path) == 0 {
		return nsgotest.MenuItem{}, false
	}
	for _, it := range items {
		if it.Label == path[0] {
			if len(path) == 1 {
				return it, true
			}
			return findMenuItem(it.Items, path[1:]...)
		}
	}
	return nsgotest.MenuItem{}, false
}

// #920: Undo, Redo, Save, and Delete keyboard shortcuts have the exact same effect as the buttons
// on both macOS (Cmd/meta) and Windows/Linux (Ctrl), and typing or pressing shortcuts while
// an inspector text field has focus never triggers editor shortcuts.
func TestKeyboardShortcutsUndoRedoSaveDeleteAndTextFieldGuard(t *testing.T) {
	platforms := []struct {
		name string
		os   core.OS
		mod  string
	}{
		{name: "macos", os: core.OSMacOS, mod: "meta"},
		{name: "windows", os: core.OSWindows, mod: "ctrl"},
		{name: "linux", os: core.OSLinux, mod: "ctrl"},
	}

	for _, plat := range platforms {
		t.Run(plat.name, func(t *testing.T) {
			// Run once with keyboard shortcuts and once with toolbar/inspector buttons to assert identical results.
			edKey := newTestEditor(t)
			edBtn := newTestEditor(t)
			edKey.SelectObject("hero")
			edBtn.SelectObject("hero")

			var keySaves, btnSaves [][]byte
			edKey.OnSave = func(data []byte) error {
				keySaves = append(keySaves, append([]byte(nil), data...))
				return nil
			}
			edBtn.OnSave = func(data []byte) error {
				btnSaves = append(btnSaves, append([]byte(nil), data...))
				return nil
			}

			env := nsgotest.Environment(core.Environment{OS: plat.os, Idiom: core.IdiomDesktop})
			appKey := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return editor.EditorView(cx, edKey)
			}, nsgotest.Size(1200, 760), env)
			appBtn := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return editor.EditorView(cx, edBtn)
			}, nsgotest.Size(1200, 760), env)

			// Make the same edit in both editors.
			appKey.Post(func() {
				if err := edKey.MoveObject("hero", 120, 88); err != nil {
					t.Errorf("MoveObject key: %v", err)
				}
			})
			appKey.Settle()
			appBtn.Post(func() {
				if err := edBtn.MoveObject("hero", 120, 88); err != nil {
					t.Errorf("MoveObject btn: %v", err)
				}
			})
			appBtn.Settle()

			// Save via shortcut vs toolbar-save button.
			appKey.Key(plat.mod + "+s")
			appBtn.Press("id:toolbar-save")
			if len(keySaves) != 1 || len(btnSaves) != 1 || !bytes.Equal(keySaves[0], btnSaves[0]) {
				t.Fatalf("Save shortcut bytes != button bytes (key=%d, btn=%d)", len(keySaves), len(btnSaves))
			}

			// Undo via shortcut vs toolbar-undo button.
			appKey.Key(plat.mod + "+z")
			appBtn.Press("id:toolbar-undo")
			heroKey, _, _ := edKey.FindObject("hero")
			heroBtn, _, _ := edBtn.FindObject("hero")
			if heroKey.X != 40 || heroKey.Y != 40 || heroKey.X != heroBtn.X || heroKey.Y != heroBtn.Y {
				t.Fatalf("after Undo: key=(%v,%v), btn=(%v,%v), want (40,40)", heroKey.X, heroKey.Y, heroBtn.X, heroBtn.Y)
			}

			// Redo via shortcut vs toolbar-redo button.
			appKey.Key(plat.mod + "+shift+z")
			appBtn.Press("id:toolbar-redo")
			heroKey, _, _ = edKey.FindObject("hero")
			heroBtn, _, _ = edBtn.FindObject("hero")
			if heroKey.X != 120 || heroKey.Y != 88 || heroKey.X != heroBtn.X || heroKey.Y != heroBtn.Y {
				t.Fatalf("after Redo: key=(%v,%v), btn=(%v,%v), want (120,88)", heroKey.X, heroKey.Y, heroBtn.X, heroBtn.Y)
			}

			// Delete via shortcut vs inspector-delete button.
			appKey.Key("delete")
			appBtn.Press("id:inspector-delete")
			if obj, _, _ := edKey.FindObject("hero"); obj != nil {
				t.Fatal("Delete shortcut did not delete hero")
			}
			if obj, _, _ := edBtn.FindObject("hero"); obj != nil {
				t.Fatal("Delete button did not delete hero")
			}
			if edKey.SelectedID != "" || edBtn.SelectedID != "" {
				t.Fatalf("SelectedID after delete: key=%q btn=%q, want empty", edKey.SelectedID, edBtn.SelectedID)
			}

			// Restore via Undo shortcut, then test backspace also deletes.
			appKey.Key(plat.mod + "+z")
			if obj, _, _ := edKey.FindObject("hero"); obj == nil || edKey.SelectedID != "hero" {
				t.Fatalf("Undo after delete did not restore hero (selected=%q)", edKey.SelectedID)
			}
			appKey.Key("backspace")
			if obj, _, _ := edKey.FindObject("hero"); obj != nil {
				t.Fatal("Backspace shortcut did not delete hero")
			}
			appKey.Key(plat.mod + "+z")
			if obj, _, _ := edKey.FindObject("hero"); obj == nil || edKey.SelectedID != "hero" {
				t.Fatalf("Undo after backspace delete did not restore hero (selected=%q)", edKey.SelectedID)
			}

			// Text-field focus guard: focus inspector-name and type shortcut letters + Backspace/Delete/Undo/Redo/Save/Palette.
			edKey.SetTool(editor.ToolSelect)
			edKey.SetGizmoMode3D(editor.Gizmo3DTranslate)
			savesBefore := len(keySaves)

			appKey.Focus("id:inspector-name")
			if !appKey.Find("id:inspector-name").Focused {
				t.Fatal("inspector-name did not take keyboard focus")
			}
			appKey.Type("id:inspector-name", "mpbegwrsf")
			appKey.Key("backspace")
			appKey.Key("delete")
			appKey.Key(plat.mod + "+s")
			appKey.Key(plat.mod + "+z")
			appKey.Key(plat.mod + "+shift+z")
			appKey.Key(plat.mod + "+d")
			appKey.Key(plat.mod + "+k")
			appKey.Key("f5")

			// Verify NONE of the editor shortcuts fired while inspector-name had focus.
			if obj, _, _ := edKey.FindObject("hero"); obj == nil {
				t.Fatal("Delete/Backspace in inspector-name deleted the selected object")
			}
			if edKey.Tool != editor.ToolSelect {
				t.Fatalf("typing tool letters in inspector-name changed Tool to %v", edKey.Tool)
			}
			if edKey.GizmoMode != editor.Gizmo3DTranslate {
				t.Fatalf("typing gizmo letters in inspector-name changed GizmoMode to %v", edKey.GizmoMode)
			}
			if len(keySaves) != savesBefore {
				t.Fatalf("Save shortcut fired while inspector-name had focus (saves=%d, want %d)", len(keySaves), savesBefore)
			}
			if edKey.CommandPaletteOpen() {
				t.Fatal("Command Palette shortcut fired while inspector-name had focus")
			}
			if edKey.Playing() {
				t.Fatal("Play shortcut fired while inspector-name had focus")
			}
			// Also verify the hero object's position was NOT undone by mod+z while focused in the name field.
			heroKey, _, _ = edKey.FindObject("hero")
			if heroKey.X != 120 || heroKey.Y != 88 {
				t.Fatalf("hero position changed while inspector-name had focus: (%v,%v), want (120,88)", heroKey.X, heroKey.Y)
			}
			if !strings.Contains(heroKey.Name, "mpbegwrs") {
				t.Fatalf("typing in inspector-name did not update Name: got %q", heroKey.Name)
			}
		})
	}
}

// #920: Tool shortcuts, 3D gizmo shortcuts, ViewMode, Duplicate, Frame Selection, Play/Stop, and Run Game.
func TestKeyboardShortcutsToolsGizmosPlayRunAndFrame(t *testing.T) {
	ed := newTestEditor(t)
	ed.SelectObject("hero")
	runCalled := 0
	ed.OnRunGame = func() error {
		runCalled++
		return nil
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1200, 760), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	// 2D tools
	toolKeys := []struct {
		key  string
		want editor.Tool
	}{
		{"m", editor.ToolMove},
		{"p", editor.ToolPlace},
		{"b", editor.ToolPaintTile},
		{"e", editor.ToolEraseTile},
		{"g", editor.ToolFillTile},
		{"v", editor.ToolSelect},
	}
	for _, tc := range toolKeys {
		app.Key(tc.key)
		if ed.Tool != tc.want {
			t.Fatalf("key %q: Tool = %v, want %v", tc.key, ed.Tool, tc.want)
		}
	}

	// Duplicate selected object
	app.Key("meta+d")
	if len(ed.Doc.Layers[1].Objects) != 2 {
		t.Fatalf("after Duplicate shortcut, actors count = %d, want 2", len(ed.Doc.Layers[1].Objects))
	}
	dupID := ed.SelectedID
	if dupID == "" || dupID == "hero" {
		t.Fatalf("after Duplicate shortcut, SelectedID = %q, want new duplicate ID", dupID)
	}
	app.Key("meta+z")
	if len(ed.Doc.Layers[1].Objects) != 1 {
		t.Fatalf("after Undo of Duplicate, actors count = %d, want 1", len(ed.Doc.Layers[1].Objects))
	}

	// Frame selection in 2D (non-destructive via #864 View2D)
	ed.SelectObject("hero")
	app.Key("f")
	v2d := ed.View2D()
	if ed.View2DFollowsCamera() || v2d.X != 40 || v2d.Y != 40 {
		t.Fatalf("2D Frame Selection View2D = %+v (followsCamera=%v), want independent view centered at (40,40)", v2d, ed.View2DFollowsCamera())
	}
	if ed.Doc.Camera != nil {
		t.Fatalf("2D Frame Selection must not mutate Doc.Camera, got %+v", ed.Doc.Camera)
	}

	// Switch to 3D view, test 3D gizmo modes and 3D Frame Selection
	app.Key("alt+3")
	if ed.ViewMode != editor.ViewMode3D {
		t.Fatalf("alt+3 ViewMode = %v, want 3D", ed.ViewMode)
	}
	gizmoKeys := []struct {
		key  string
		want editor.GizmoMode3D
	}{
		{"e", editor.Gizmo3DRotate},
		{"r", editor.Gizmo3DScale},
		{"w", editor.Gizmo3DTranslate},
	}
	for _, tc := range gizmoKeys {
		app.Key(tc.key)
		if ed.GizmoMode != tc.want {
			t.Fatalf("key %q: GizmoMode = %v, want %v", tc.key, ed.GizmoMode, tc.want)
		}
	}
	app.Key("f")
	if ed.Camera3D.Target[0] != 40 || ed.Camera3D.Target[1] != 40 {
		t.Fatalf("3D Frame Selection Target = %v, want (40,40,*)", ed.Camera3D.Target)
	}

	// Switch back to 2D
	app.Key("alt+2")
	if ed.ViewMode != editor.ViewMode2D {
		t.Fatalf("alt+2 ViewMode = %v, want 2D", ed.ViewMode)
	}

	// Run Game hook via mod+r
	app.Key("meta+r")
	if runCalled != 1 {
		t.Fatalf("Run Game hook count = %d, want 1", runCalled)
	}

	// Play / Stop via f5; verify edit shortcuts are disabled while playing.
	app.Key("f5")
	if !ed.Playing() {
		t.Fatal("f5 did not start Play mode")
	}
	app.Key("m")
	if ed.Tool != editor.ToolSelect {
		t.Fatalf("tool shortcut fired while playing: Tool = %v", ed.Tool)
	}
	app.Key("f5")
	if ed.Playing() {
		t.Fatal("second f5 did not stop Play mode")
	}
}

// #920: Desktop menu bar exposes File, Edit, View, Project with matching shortcuts and actions.
func TestDesktopMenuBarMatchesRegistryAndExecutesActions(t *testing.T) {
	// Verify platform shortcut formatting across macOS, Windows, and Linux.
	if got := editor.FormatShortcut("mod+s", core.OSMacOS); got != "⌘S" {
		t.Fatalf("macOS FormatShortcut(mod+s) = %q, want ⌘S", got)
	}
	if got := editor.FormatShortcut("mod+shift+z", core.OSMacOS); got != "⇧⌘Z" {
		t.Fatalf("macOS FormatShortcut(mod+shift+z) = %q, want ⇧⌘Z", got)
	}
	if got := editor.FormatShortcut("mod+s", core.OSWindows); got != "Ctrl+S" {
		t.Fatalf("Windows FormatShortcut(mod+s) = %q, want Ctrl+S", got)
	}
	if got := editor.FormatShortcut("mod+shift+z", core.OSLinux); got != "Ctrl+Shift+Z" {
		t.Fatalf("Linux FormatShortcut(mod+shift+z) = %q, want Ctrl+Shift+Z", got)
	}

	dir := writePluginProject(t, "health")
	ed := newPluginEditor(t, dir)
	pluginCmdRan := 0
	if err := ed.RegisterPlugin(editor.Plugin{
		Name: "tools",
		Commands: []editor.PluginCommand{{
			ID:       "ping",
			Label:    "Ping Plugin",
			Shortcut: "mod+shift+p",
			Run: func(ed *editor.Editor) error {
				pluginCmdRan++
				return nil
			},
		}},
	}); err != nil {
		t.Fatalf("RegisterPlugin tools: %v", err)
	}
	if err := ed.SetPluginEnabled("tools", true); err != nil {
		t.Fatalf("SetPluginEnabled tools: %v", err)
	}
	ed.SelectObject("hero")

	savedCount := 0
	ed.OnSave = func(data []byte) error {
		savedCount++
		return nil
	}
	runCount := 0
	ed.OnRunGame = func() error {
		runCount++
		return nil
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1200, 760), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	if len(app.Menu) != 5 {
		t.Fatalf("top-level menus = %d, want 5 (File, Edit, View, Project, Help)", len(app.Menu))
	}
	wantTop := []string{"File", "Edit", "View", "Project", "Help"}
	for i, want := range wantTop {
		if app.Menu[i].Label != want {
			t.Fatalf("Menu[%d] = %q, want %q", i, app.Menu[i].Label, want)
		}
	}

	// Verify key menu items expose exact shortcuts from the command registry and NotInText guard.
	checks := []struct {
		path     []string
		shortcut string
	}{
		{[]string{"File", "Save"}, "mod+s"},
		{[]string{"Edit", "Undo"}, "mod+z"},
		{[]string{"Edit", "Redo"}, "mod+shift+z"},
		{[]string{"Edit", "Duplicate"}, "mod+d"},
		{[]string{"Edit", "Delete"}, "delete"},
		{[]string{"View", "Command Palette..."}, "mod+k"},
		{[]string{"View", "Frame Selection"}, "f"},
		{[]string{"View", "Tools", "Move"}, "m"},
		{[]string{"View", "3D Gizmo", "Translate"}, "w"},
		{[]string{"View", "3D Gizmo", "Rotate"}, "e"},
		{[]string{"View", "3D Gizmo", "Scale"}, "r"},
		{[]string{"View", "3D Gizmo", "3D Gizmo: Local / Global Space"}, "t"},
		{[]string{"Project", "Play"}, "f5"},
		{[]string{"Project", "Run Game"}, "mod+r"},
		{[]string{"Project", "Plugins", "Ping Plugin"}, "mod+shift+p"},
	}
	for _, c := range checks {
		item, ok := findMenuItem(app.Menu, c.path...)
		if !ok {
			t.Fatalf("menu item %v not found", c.path)
		}
		if item.Shortcut != c.shortcut {
			t.Fatalf("menu item %v shortcut = %q, want %q", c.path, item.Shortcut, c.shortcut)
		}
		if !item.NotInText {
			t.Fatalf("menu item %v must set NotInText=true (MenuNotEditing)", c.path)
		}
	}

	// Drive actions through the desktop menu bar.
	app.Post(func() {
		_ = ed.MoveObject("hero", 99, 77)
	})
	app.Settle()

	app.ChooseMenu("File", "Save")
	if savedCount != 1 {
		t.Fatalf("File > Save did not invoke OnSave (count=%d)", savedCount)
	}

	app.ChooseMenu("Edit", "Undo")
	hero, _, _ := ed.FindObject("hero")
	if hero.X != 40 || hero.Y != 40 {
		t.Fatalf("Edit > Undo did not restore hero position: (%v,%v)", hero.X, hero.Y)
	}

	app.ChooseMenu("Edit", "Redo")
	hero, _, _ = ed.FindObject("hero")
	if hero.X != 99 || hero.Y != 77 {
		t.Fatalf("Edit > Redo did not re-apply hero position: (%v,%v)", hero.X, hero.Y)
	}

	app.ChooseMenu("View", "Tools", "Move")
	if ed.Tool != editor.ToolMove {
		t.Fatalf("View > Tools > Move did not switch tool: %v", ed.Tool)
	}

	app.ChooseMenu("View", "3D Gizmo", "Rotate")
	if ed.GizmoMode != editor.Gizmo3DRotate {
		t.Fatalf("View > 3D Gizmo > Rotate did not switch gizmo: %v", ed.GizmoMode)
	}

	app.ChooseMenu("Project", "Run Game")
	if runCount != 1 {
		t.Fatalf("Project > Run Game count = %d, want 1", runCount)
	}

	app.ChooseMenu("Project", "Plugins", "Ping Plugin")
	if pluginCmdRan != 1 {
		t.Fatalf("Project > Plugins > Ping Plugin count = %d, want 1", pluginCmdRan)
	}

	app.ChooseMenu("Project", "Play")
	if !ed.Playing() {
		t.Fatal("Project > Play did not start playback")
	}
	if _, ok := findMenuItem(app.Menu, "Project", "Stop"); !ok {
		t.Fatal("while playing, Project menu should relabel Play to Stop")
	}
	app.ChooseMenu("Project", "Stop")
	if ed.Playing() {
		t.Fatal("Project > Stop did not stop playback")
	}
}

// #920: Command palette lists built-in commands, plugin tools, plugin panels, plugin commands,
// and project scenes/files; filtering and running a plugin tool by name activates it.
func TestCommandPaletteFindsAndRunsPluginToolPanelCommandAndProjectFiles(t *testing.T) {
	dir := writePluginProject(t, "health")
	// Add a second scene and a non-scene file to the project.
	bossDoc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "Boss Arena",
		VirtualWidth:  320,
		VirtualHeight: 180,
		Layers:        []sceneformat.LayerDoc{{Name: "main", Space: sceneformat.SpaceWorld}},
	}
	bossBytes, err := sceneformat.Encode(bossDoc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scenes", "boss.scene.yaml"), bossBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "design-notes.txt"), []byte("level notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.SetComponentRegistry(healthComponents(t)); err != nil {
		t.Fatal(err)
	}
	healAllCount := 0
	p := healthPlugin(nil)
	p.Commands = []editor.PluginCommand{{
		ID:          "heal-all",
		Label:       "Heal All Units",
		Description: "Restore every unit to full HP",
		Shortcut:    "mod+shift+h",
		Run: func(ed *editor.Editor) error {
			healAllCount++
			return nil
		},
	}}
	if err := ed.RegisterPlugin(p); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1200, 760), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	// 1. Open command palette with mod+k, filter for plugin tool "Spawn" by name, run it.
	app.Key("meta+k")
	if !ed.CommandPaletteOpen() {
		t.Fatal("meta+k did not open Command Palette")
	}
	app.Type("id:editor-command-palette >> role:textbox", "Spawn")
	app.Key("enter")
	if ed.CommandPaletteOpen() {
		t.Fatal("selecting a palette item should close the Command Palette")
	}
	if ed.Tool != editor.ToolPlugin {
		t.Fatalf("Tool after running plugin tool from palette = %v, want ToolPlugin", ed.Tool)
	}
	if pName, tID, ok := ed.PluginTool(); !ok || pName != "health" || tID != "spawn" {
		t.Fatalf("PluginTool() = (%q, %q, %v), want (health, spawn, true)", pName, tID, ok)
	}

	// 2. Open command palette, filter for plugin panel "Health", run it.
	app.Key("meta+k")
	app.Type("id:editor-command-palette >> role:textbox", "Health")
	app.Key("enter")
	if pName, panelID, ok := ed.FocusedPluginPanel(); !ok || pName != "health" || panelID != "overview" {
		t.Fatalf("FocusedPluginPanel() = (%q, %q, %v), want (health, overview, true)", pName, panelID, ok)
	}

	// 3. Open command palette, filter for plugin command "Heal All Units", run it.
	app.Key("meta+k")
	app.Type("id:editor-command-palette >> role:textbox", "Heal All")
	app.Key("enter")
	if healAllCount != 1 {
		t.Fatalf("plugin command run count = %d, want 1", healAllCount)
	}

	// 4. Trigger the same plugin command via its registered keyboard shortcut!
	app.Key("meta+shift+h")
	if healAllCount != 2 {
		t.Fatalf("plugin command shortcut count = %d, want 2", healAllCount)
	}

	// 5. Open command palette, filter for a scene file by name ("boss.scene"), open it.
	app.Key("meta+k")
	app.Type("id:editor-command-palette >> role:textbox", "boss.scene")
	app.Key("enter")
	if ed.ScenePath != "scenes/boss.scene.yaml" || ed.Doc.Name != "Boss Arena" {
		t.Fatalf("opening scene from palette: ScenePath=%q Doc.Name=%q, want scenes/boss.scene.yaml / Boss Arena", ed.ScenePath, ed.Doc.Name)
	}

	// 6. Open command palette, filter for a non-scene project file ("design-notes"), select it.
	app.Key("meta+k")
	app.Type("id:editor-command-palette >> role:textbox", "design-notes")
	app.Key("enter")
	if ed.SelectedFile != "design-notes.txt" {
		t.Fatalf("selecting project file from palette: SelectedFile=%q, want design-notes.txt", ed.SelectedFile)
	}

	// 7. Escape closes the command palette cleanly.
	app.Key("meta+k")
	if !ed.CommandPaletteOpen() {
		t.Fatal("meta+k did not reopen Command Palette")
	}
	app.Key("escape")
	if ed.CommandPaletteOpen() {
		t.Fatal("Escape did not close empty Command Palette")
	}
}

// #920: Registering a plugin command (or custom command) with a clashing shortcut or invalid fields is refused.
func TestPluginCommandShortcutClashRefused(t *testing.T) {
	ed := newTestEditor(t)

	noop := func(*editor.Editor) error { return nil }

	// Clashes with built-in Save ("mod+s") via mod+s, cmd+s, and ctrl+s.
	for _, sc := range []string{"mod+s", "cmd+s", "ctrl+s", "CTRL + S", "delete", "backspace", "v", "f5"} {
		err := ed.RegisterPlugin(editor.Plugin{
			Name: "clash-" + strings.ReplaceAll(strings.ToLower(sc), " ", ""),
			Commands: []editor.PluginCommand{{
				ID:       "bad",
				Label:    "Clashing Command",
				Shortcut: sc,
				Run:      noop,
			}},
		})
		if err == nil {
			t.Fatalf("RegisterPlugin with clashing shortcut %q succeeded, want error", sc)
		}
		if !strings.Contains(err.Error(), "clashes") {
			t.Fatalf("RegisterPlugin(%q) error = %v, want clash explanation", sc, err)
		}
	}
	if len(ed.RegisteredPlugins()) != 0 {
		t.Fatalf("failed registrations must not add plugins, got %v", ed.RegisteredPlugins())
	}

	// Register a valid plugin with shortcut "alt+h".
	if err := ed.RegisterPlugin(editor.Plugin{
		Name: "alpha",
		Commands: []editor.PluginCommand{{
			ID:       "first",
			Label:    "Alpha Action",
			Shortcut: "alt+h",
			Run:      noop,
		}},
	}); err != nil {
		t.Fatalf("RegisterPlugin alpha: %v", err)
	}

	// A second plugin trying to claim the same shortcut "option+h" (alias of "alt+h") must be refused.
	if err := ed.RegisterPlugin(editor.Plugin{
		Name: "beta",
		Commands: []editor.PluginCommand{{
			ID:       "second",
			Label:    "Beta Action",
			Shortcut: "option+h",
			Run:      noop,
		}},
	}); err == nil {
		t.Fatal("RegisterPlugin beta with shortcut clashing alpha succeeded, want error")
	}

	// A single plugin with two internal commands sharing a shortcut must be refused.
	if err := ed.RegisterPlugin(editor.Plugin{
		Name: "gamma",
		Commands: []editor.PluginCommand{
			{ID: "one", Label: "One", Shortcut: "alt+j", Run: noop},
			{ID: "two", Label: "Two", Shortcut: "alt+j", Run: noop},
		},
	}); err == nil {
		t.Fatal("RegisterPlugin gamma with duplicate internal shortcut succeeded, want error")
	}

	// Invalid shortcut syntax must be refused.
	if err := ed.RegisterPlugin(editor.Plugin{
		Name: "delta",
		Commands: []editor.PluginCommand{{
			ID:       "badkey",
			Label:    "Bad Key",
			Shortcut: "bogus+x",
			Run:      noop,
		}},
	}); err == nil {
		t.Fatal("RegisterPlugin delta with invalid modifier succeeded, want error")
	}

	// RegisterCommand also refuses duplicate ID and clashing shortcut.
	if err := ed.RegisterCommand(editor.EditorCommand{
		ID:       editor.CmdSave,
		Label:    "Dup Save",
		Shortcut: "alt+u",
		Run:      func() error { return nil },
	}); err == nil {
		t.Fatal("RegisterCommand with duplicate built-in ID succeeded, want error")
	}
	if err := ed.RegisterCommand(editor.EditorCommand{
		ID:       "custom.clash",
		Label:    "Custom Clash",
		Shortcut: "alt+h",
		Run:      func() error { return nil },
	}); err == nil {
		t.Fatal("RegisterCommand with shortcut clashing plugin alpha succeeded, want error")
	}

	// ExecuteCommand reports ErrCommandNotFound and ErrCommandDisabled cleanly.
	if err := ed.ExecuteCommand("does.not.exist"); !errors.Is(err, editor.ErrCommandNotFound) {
		t.Fatalf("ExecuteCommand unknown = %v, want ErrCommandNotFound", err)
	}
	if err := ed.ExecuteCommand(editor.CmdUndo); !errors.Is(err, editor.ErrCommandDisabled) {
		t.Fatalf("ExecuteCommand disabled Undo = %v, want ErrCommandDisabled", err)
	}

	// Only "alpha" should remain registered.
	if got := ed.RegisteredPlugins(); len(got) != 1 || got[0] != "alpha" {
		t.Fatalf("RegisteredPlugins() = %v, want [alpha]", got)
	}

	// Unused project import guard (project is used via writePluginProject / FileName checks).
	_ = project.FileName
}
