package editor_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// #1061 Acceptance 1: Rebinding Frame (CmdFrameSelection) from F to G survives a restart,
// immediately shows G in tooltips, menu bar, Command Palette, and the ⌘/ shortcuts sheet,
// and pressing G triggers Frame while F no longer frames.
func TestShortcutsRebindFramePersistsAcrossRestartAndUpdatesEverySurface(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "user-config", "editor.yaml")

	ed := newTestEditor(t)
	if err := ed.LoadSettings(settingsPath); err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	ed.SelectObject("hero")

	macEnv := nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true, Locale: "en-US"})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), macEnv)

	// Initially Frame Selection is bound to "f" and Move tool tooltip shows "Move · W".
	if got := ed.EffectiveShortcut(editor.CmdFrameSelection); got != "f" {
		t.Fatalf("initial EffectiveShortcut(Frame) = %q, want f", got)
	}
	app.Hover("id:tool-move")
	app.Advance(time.Second)
	if !app.Exists("text:Move · W") {
		t.Fatal("initial tool-move tooltip did not show \"Move · W\"")
	}

	// Open Editor Settings -> Shortcuts tab, search for Frame, press its binding button, and press "g".
	// "g" is used by Tool: Fill Tile in ScopeTile (a DIFFERENT scope from ScopeViewport),
	// so rebinding Frame to "g" must succeed immediately without a conflict prompt.
	runEditorCommand(t, app, ed, editor.CmdEditorSettings)
	app.Press("id:editor-settings-tab-shortcuts")
	app.Type("id:shortcuts-search", "Frame")
	app.Press("id:shortcut-bind-" + editor.CmdFrameSelection)
	if !app.Exists("id:shortcut-capture-" + editor.CmdFrameSelection) {
		t.Fatal("pressing shortcut-bind-view.frame did not enter capture mode")
	}
	app.Key("g")

	if app.Exists("id:shortcut-conflict-" + editor.CmdFrameSelection) {
		t.Fatal("binding Frame (viewport) to G (tile scope) unexpectedly flagged a same-scope conflict")
	}
	if got := ed.EffectiveShortcut(editor.CmdFrameSelection); got != "g" {
		t.Fatalf("after rebinding, EffectiveShortcut(Frame) = %q, want g", got)
	}
	// Tool: Fill Tile in ScopeTile remains bound to "g".
	if got := ed.EffectiveShortcut(editor.CmdToolFillTile); got != "g" {
		t.Fatalf("CmdToolFillTile shortcut changed to %q, want g", got)
	}

	// Close Editor Settings dialog and verify every UI surface reflects G immediately.
	app.Press("id:editor-settings-close")

	// 1. Desktop menu bar.
	if item, ok := findMenuItem(app.Menu, "View", "Frame Selection"); !ok || item.Shortcut != "g" {
		t.Fatalf("menu View > Frame Selection = (%+v, %v), want shortcut g", item, ok)
	}

	// 3. Command Palette.
	foundPalette := false
	for _, it := range ed.PaletteItems() {
		if it.ID == "cmd:"+editor.CmdFrameSelection {
			foundPalette = true
			if it.Shortcut != "g" {
				t.Fatalf("palette Frame Selection shortcut = %q, want g", it.Shortcut)
			}
		}
	}
	if !foundPalette {
		t.Fatal("Frame Selection missing from PaletteItems")
	}

	// 4. ⌘/ Shortcuts sheet shows G immediately.
	app.Key("meta+/")
	if !ed.ShortcutsSheetOpen() {
		t.Fatal("meta+/ did not open ShortcutsSheetView")
	}
	if got := app.Find("id:shortcuts-sheet-key-" + editor.CmdFrameSelection).Text; got != "G" {
		t.Fatalf("⌘/ sheet Frame key = %q, want G", got)
	}
	app.Press("id:shortcuts-sheet-close")

	// 5. Pressing "f" no longer frames, and pressing "g" triggers Frame (centering View2D on hero at 40,40)!
	app.Post(func() {
		ed.ResetView2D()
	})
	app.Settle()
	app.Key("f")
	if !ed.View2DFollowsCamera() {
		t.Fatalf("pressing old key f still framed selection: View2D=%+v", ed.View2D())
	}
	app.Key("g")
	v2d := ed.View2D()
	if ed.View2DFollowsCamera() || v2d.X != 40 || v2d.Y != 40 {
		t.Fatalf("pressing rebound key g did not frame hero: View2D=%+v followsCamera=%v", v2d, ed.View2DFollowsCamera())
	}

	// 6. Verify persisted YAML on disk outside any project and byte-for-byte round-trip.
	diskBytes, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read persisted settings: %v", err)
	}
	if !strings.Contains(string(diskBytes), "view.frame: g") {
		t.Fatalf("persisted editor.yaml missing view.frame: g:\n%s", diskBytes)
	}
	parsed, err := editor.ParseEditorSettings(diskBytes)
	if err != nil {
		t.Fatalf("ParseEditorSettings(disk): %v", err)
	}
	reencoded, err := editor.EncodeEditorSettings(parsed)
	if err != nil {
		t.Fatalf("EncodeEditorSettings(parsed): %v", err)
	}
	if !bytes.Equal(diskBytes, reencoded) {
		t.Fatalf("settings round-trip mismatch:\ngot:\n%s\nwant:\n%s", reencoded, diskBytes)
	}

	// 7. Full restart: fresh Editor + fresh UI session loading the saved settings file.
	restarted := newTestEditor(t)
	if err := restarted.LoadSettings(settingsPath); err != nil {
		t.Fatalf("restarted.LoadSettings: %v", err)
	}
	restarted.SelectObject("hero")

	app2 := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, restarted)
	}, nsgotest.Size(1440, 900), macEnv)

	if got := restarted.EffectiveShortcut(editor.CmdFrameSelection); got != "g" {
		t.Fatalf("after restart, EffectiveShortcut(Frame) = %q, want g", got)
	}
	if item, ok := findMenuItem(app2.Menu, "View", "Frame Selection"); !ok || item.Shortcut != "g" {
		t.Fatalf("after restart, menu View > Frame Selection = (%+v, %v), want shortcut g", item, ok)
	}
	app2.Key("meta+/")
	if got := app2.Find("id:shortcuts-sheet-key-" + editor.CmdFrameSelection).Text; got != "G" {
		t.Fatalf("after restart, ⌘/ sheet Frame key = %q, want G", got)
	}
	app2.Press("id:shortcuts-sheet-close")

	app2.Key("g")
	v2d2 := restarted.View2D()
	if restarted.View2DFollowsCamera() || v2d2.X != 40 || v2d2.Y != 40 {
		t.Fatalf("after restart, pressing g did not frame hero: View2D=%+v followsCamera=%v", v2d2, restarted.View2DFollowsCamera())
	}
}

// #1061 Acceptance 2: Binding a key used in the SAME scope flags the conflict in place and offers
// Swap (leaving both commands bound); binding a key used only in a DIFFERENT scope succeeds
// immediately without a prompt; per-row Reset and Reset all restore defaults; Escape cancels capture.
func TestShortcutsSameScopeConflictSwapAndDifferentScopeAllowed(t *testing.T) {
	ed := newTestEditor(t)
	ed.SelectObject("hero")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true, Locale: "en-US"}))

	app.Post(func() { ed.OpenShortcutsSettings() })
	app.Settle()

	// Escape during capture cancels capture without closing Editor Settings.
	app.Press("id:shortcut-bind-" + editor.CmdFrameSelection)
	if !app.Exists("id:shortcut-capture-" + editor.CmdFrameSelection) {
		t.Fatal("capture box did not open")
	}
	app.Key("escape")
	if app.Exists("id:shortcut-capture-" + editor.CmdFrameSelection) {
		t.Fatal("Escape did not cancel key capture")
	}
	if !ed.EditorSettingsOpen() {
		t.Fatal("Escape during key capture closed the Editor Settings dialog")
	}

	// Same-scope conflict: Frame Selection (ScopeViewport, "f") -> "v" (Tool: Select, ScopeViewport).
	app.Press("id:shortcut-bind-" + editor.CmdFrameSelection)
	app.Key("v")

	if !app.Exists("id:shortcut-conflict-" + editor.CmdFrameSelection) {
		t.Fatal("binding Frame to V (same ScopeViewport) did not show in-place conflict banner")
	}
	conflictText := allText(app.Find("id:shortcut-conflict-" + editor.CmdFrameSelection))
	if !strings.Contains(conflictText, "Tool: Select") || !strings.Contains(conflictText, "Viewport") {
		t.Fatalf("conflict banner text = %q, want Tool: Select and Viewport", conflictText)
	}
	// Bindings remain unchanged until Swap is pressed.
	if ed.EffectiveShortcut(editor.CmdFrameSelection) != "f" || ed.EffectiveShortcut(editor.CmdToolSelect) != "v" {
		t.Fatalf("bindings mutated before Swap: frame=%q select=%q", ed.EffectiveShortcut(editor.CmdFrameSelection), ed.EffectiveShortcut(editor.CmdToolSelect))
	}

	// Press Swap -> both commands stay bound to each other's keys!
	app.Press("id:shortcut-conflict-swap")
	if app.Exists("id:shortcut-conflict-" + editor.CmdFrameSelection) {
		t.Fatal("conflict banner remained after Swap")
	}
	if ed.EffectiveShortcut(editor.CmdFrameSelection) != "v" || ed.EffectiveShortcut(editor.CmdToolSelect) != "f" {
		t.Fatalf("after Swap: frame=%q select=%q, want v and f", ed.EffectiveShortcut(editor.CmdFrameSelection), ed.EffectiveShortcut(editor.CmdToolSelect))
	}

	// Verify both swapped shortcuts work live in the editor!
	app.Press("id:editor-settings-close")
	ed.SetTool(editor.ToolMove)
	app.Key("f")
	if ed.Tool != editor.ToolSelect {
		t.Fatalf("swapped Select key f did not switch to ToolSelect (got %v)", ed.Tool)
	}
	app.Key("v")
	if v := ed.View2D(); ed.View2DFollowsCamera() || v.X != 40 || v.Y != 40 {
		t.Fatalf("swapped Frame key v did not frame hero: View2D=%+v", v)
	}

	// Different-scope binding: Frame (ScopeViewport) -> "1" (3D View: Front, ScopeViewport3D) binds immediately!
	app.Post(func() { ed.OpenShortcutsSettings() })
	app.Settle()
	app.Press("id:shortcut-bind-" + editor.CmdFrameSelection)
	app.Key("1")
	if app.Exists("id:shortcut-conflict-" + editor.CmdFrameSelection) {
		t.Fatal("binding Frame (viewport) to 1 (viewport3d) unexpectedly flagged a conflict")
	}
	if ed.EffectiveShortcut(editor.CmdFrameSelection) != "1" || ed.EffectiveShortcut(editor.CmdView3DFront) != "1" {
		t.Fatalf("cross-scope bindings: frame=%q front3d=%q, want 1 and 1", ed.EffectiveShortcut(editor.CmdFrameSelection), ed.EffectiveShortcut(editor.CmdView3DFront))
	}

	// Per-row Reset restores Frame Selection back to "f" (and swaps Tool: Select if it held "f").
	app.Press("id:shortcut-reset-" + editor.CmdFrameSelection)
	if ed.EffectiveShortcut(editor.CmdFrameSelection) != "f" {
		t.Fatalf("after per-row Reset, Frame = %q, want f", ed.EffectiveShortcut(editor.CmdFrameSelection))
	}

	// Rebind Save to mod+shift+s, then test Reset all restores everything cleanly.
	app.Post(func() {
		if err := ed.RebindShortcut(editor.CmdSave, "mod+shift+s"); err != nil {
			t.Errorf("RebindShortcut(Save): %v", err)
		}
	})
	app.Settle()
	if !ed.HasCustomShortcuts() {
		t.Fatal("HasCustomShortcuts = false after custom bindings")
	}
	app.Press("id:shortcuts-reset-all")
	if ed.HasCustomShortcuts() {
		t.Fatal("HasCustomShortcuts = true after Reset all")
	}
	if ed.EffectiveShortcut(editor.CmdSave) != "mod+s" || ed.EffectiveShortcut(editor.CmdFrameSelection) != "f" || ed.EffectiveShortcut(editor.CmdToolSelect) != "v" {
		t.Fatalf("after Reset all: save=%q frame=%q select=%q", ed.EffectiveShortcut(editor.CmdSave), ed.EffectiveShortcut(editor.CmdFrameSelection), ed.EffectiveShortcut(editor.CmdToolSelect))
	}
}

// #1061: Settings › Shortcuts search filters by command name, key/shortcut, and scope.
func TestShortcutsSearchByNameKeyAndScope(t *testing.T) {
	ed := newTestEditor(t)

	// Search by name
	byName := ed.FilterShortcutCommands("Frame Selection", core.OSMacOS)
	if len(byName) == 0 || byName[0].ID != editor.CmdFrameSelection {
		t.Fatalf("FilterShortcutCommands(name) = %v, want Frame Selection", byName)
	}

	// Search by formatted platform key (⌘S) and canonical key (mod+s)
	byMacKey := ed.FilterShortcutCommands("⌘S", core.OSMacOS)
	foundSave := false
	for _, c := range byMacKey {
		if c.ID == editor.CmdSave {
			foundSave = true
		}
	}
	if !foundSave {
		t.Fatalf("FilterShortcutCommands(⌘S) did not match CmdSave")
	}
	byWinKey := ed.FilterShortcutCommands("Ctrl+S", core.OSWindows)
	foundSave = false
	for _, c := range byWinKey {
		if c.ID == editor.CmdSave {
			foundSave = true
		}
	}
	if !foundSave {
		t.Fatalf("FilterShortcutCommands(Ctrl+S) did not match CmdSave")
	}

	// Search by scope ("tile")
	byScope := ed.FilterShortcutCommands("tile", core.OSMacOS)
	foundFill := false
	for _, c := range byScope {
		if c.ID == editor.CmdToolFillTile {
			foundFill = true
		}
	}
	if !foundFill {
		t.Fatalf("FilterShortcutCommands(tile) did not match CmdToolFillTile")
	}
}

// #1061 Acceptance 3: Plugin commands appear in Settings › Shortcuts, rebind, swap, persist, and reset
// identically to built-in commands.
func TestShortcutsPluginCommandsAppearRebindAndReset(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "editor.yaml")
	dir := writePluginProject(t, "tools")
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.LoadSettings(settingsPath); err != nil {
		t.Fatal(err)
	}
	pingCount := 0
	if err := ed.RegisterPlugin(editor.Plugin{
		Name: "tools",
		Commands: []editor.PluginCommand{{
			ID:       "ping",
			Label:    "Ping Plugin",
			Shortcut: "alt+h",
			Run: func(ed *editor.Editor) error {
				pingCount++
				return nil
			},
		}},
	}); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true, Locale: "en-US"}))

	pluginCmdID := "plugin.tools.ping"
	app.Post(func() { ed.OpenShortcutsSettings() })
	app.Settle()

	app.Type("id:shortcuts-search", "Ping Plugin")
	if !app.Exists("id:shortcut-row-" + pluginCmdID) {
		t.Fatal("plugin command missing from Settings › Shortcuts")
	}

	// Rebind plugin command from alt+h to alt+j via key capture.
	app.Press("id:shortcut-bind-" + pluginCmdID)
	app.Key("alt+j")

	if got := ed.EffectiveShortcut(pluginCmdID); got != "alt+j" {
		t.Fatalf("plugin command EffectiveShortcut = %q, want alt+j", got)
	}
	app.Press("id:editor-settings-close")

	// Menu bar and live keyboard shortcut immediately use alt+j.
	if item, ok := findMenuItem(app.Menu, "Project", "Plugins", "Ping Plugin"); !ok || item.Shortcut != "alt+j" {
		t.Fatalf("plugin menu item = (%+v, %v), want shortcut alt+j", item, ok)
	}
	app.Key("alt+h")
	if pingCount != 0 {
		t.Fatalf("old plugin shortcut alt+h still fired (count=%d)", pingCount)
	}
	app.Key("alt+j")
	if pingCount != 1 {
		t.Fatalf("rebound plugin shortcut alt+j did not fire (count=%d)", pingCount)
	}

	// Reset restores alt+h.
	app.Post(func() { ed.OpenShortcutsSettings() })
	app.Settle()
	app.Press("id:shortcut-reset-" + pluginCmdID)
	if got := ed.EffectiveShortcut(pluginCmdID); got != "alt+h" {
		t.Fatalf("after reset, plugin command shortcut = %q, want alt+h", got)
	}
}

// #1061: Programmatic API validation, ShortcutFromKeyEvent across OSes, and EditorSettings strict YAML rules.
func TestShortcutsAPIAndSettingsValidation(t *testing.T) {
	// ShortcutFromKeyEvent maps Meta->mod on macOS and Ctrl->mod on Windows/Linux, ignoring bare modifiers.
	if sc, ok := editor.ShortcutFromKeyEvent(core.KeyEvent{Key: "shift", Modifiers: core.Modifiers{Shift: true}}, core.OSMacOS); ok || sc != "" {
		t.Fatalf("bare shift returned (%q, %v), want empty/false", sc, ok)
	}
	if sc, ok := editor.ShortcutFromKeyEvent(core.KeyEvent{Key: "g", Modifiers: core.Modifiers{Platform: true}}, core.OSMacOS); !ok || sc != "mod+g" {
		t.Fatalf("macOS Meta+g = (%q, %v), want (mod+g, true)", sc, ok)
	}
	if sc, ok := editor.ShortcutFromKeyEvent(core.KeyEvent{Key: "1", Modifiers: core.Modifiers{Control: true}}, core.OSMacOS); !ok || sc != "ctrl+1" {
		t.Fatalf("macOS Ctrl+1 = (%q, %v), want (ctrl+1, true)", sc, ok)
	}
	if sc, ok := editor.ShortcutFromKeyEvent(core.KeyEvent{Key: "g", Modifiers: core.Modifiers{Control: true, Shift: true}}, core.OSWindows); !ok || sc != "mod+shift+g" {
		t.Fatalf("Windows Ctrl+Shift+g = (%q, %v), want (mod+shift+g, true)", sc, ok)
	}

	// Programmatic RebindShortcut returns *ShortcutConflictError on same-scope conflict.
	ed := newTestEditor(t)
	err := ed.RebindShortcut(editor.CmdFrameSelection, "v")
	var conflictErr *editor.ShortcutConflictError
	if !errors.As(err, &conflictErr) || conflictErr.ConflictID != editor.CmdToolSelect || conflictErr.Scope != editor.ScopeViewport {
		t.Fatalf("RebindShortcut same-scope conflict err = %v, want ShortcutConflictError(tool.select, viewport)", err)
	}

	// Unknown command returns ErrCommandNotFound.
	if err := ed.RebindShortcut("no.such.command", "g"); !errors.Is(err, editor.ErrCommandNotFound) {
		t.Fatalf("RebindShortcut unknown = %v, want ErrCommandNotFound", err)
	}

	// EditorSettings YAML accepts valid shortcuts map and refuses invalid shortcut syntax or bad command ID.
	validYAML := []byte("# yaml-language-server: $schema=https://game.nsgo.dev/schemas/editor-settings.v1.json\nversion: 1\nlanguage: en\nappearance: dark\nshortcuts:\n  view.frame: g\n")
	parsed, err := editor.ParseEditorSettings(validYAML)
	if err != nil {
		t.Fatalf("ParseEditorSettings(valid shortcuts): %v", err)
	}
	if parsed.Shortcuts[editor.CmdFrameSelection] != "g" {
		t.Fatalf("parsed Shortcuts = %v, want view.frame: g", parsed.Shortcuts)
	}
	out, err := editor.EncodeEditorSettings(parsed)
	if err != nil {
		t.Fatalf("EncodeEditorSettings: %v", err)
	}
	if !bytes.Equal(out, validYAML) {
		t.Fatalf("canonical round-trip mismatch:\ngot:\n%s\nwant:\n%s", out, validYAML)
	}

	for name, bad := range map[string]string{
		"badShortcut": "version: 1\nlanguage: en\nappearance: dark\nshortcuts:\n  view.frame: bogus+g\n",
		"badCmdID":    "version: 1\nlanguage: en\nappearance: dark\nshortcuts:\n  \"bad id\": g\n",
	} {
		if _, err := editor.ParseEditorSettings([]byte(bad)); err == nil {
			t.Fatalf("ParseEditorSettings(%s) succeeded, want error", name)
		}
	}
}

// #1061 Acceptance 4: Real nsgotest screenshots on macOS (Settings › Shortcuts, same-scope conflict
// with Swap, and ⌘/ Shortcuts sheet) plus compact 2D toolbar regression check at 1120x680.
func TestShortcutsScreenshotsAndToolbarCompact(t *testing.T) {
	ed := newTestEditor(t)
	ed.SelectObject("hero")

	// Check 2D toolbar stays compact (<= 2 rows at 1440x900 and bounded at 1120x680).
	compactApp := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 680), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true, Locale: "en-US"}))
	if h := compactApp.Find("id:editor-toolbar").Bounds().Size.Height; h > 140 {
		t.Fatalf("2D editor-toolbar height at 1120x680 = %v pt, want <= 140 pt", h)
	}

	// Capture macOS screenshots at 1440x900 matching Settings.dc.html / Spec.dc.html.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true, Locale: "en-US"}))
	if h := app.Find("id:editor-toolbar").Bounds().Size.Height; h > 80 {
		t.Fatalf("2D editor-toolbar height at 1440x900 = %v pt, want <= 80 pt (<= 2 rows)", h)
	}

	app.Post(func() { ed.OpenShortcutsSettings() })
	app.Settle()
	app.Screenshot("shortcuts-settings")

	// Trigger same-scope conflict on Frame Selection -> V to verify in-place Swap banner visually.
	app.Type("id:shortcuts-search", "Frame")
	app.Press("id:shortcut-bind-" + editor.CmdFrameSelection)
	app.Key("v")
	app.Screenshot("shortcuts-conflict")

	// Swap, close Settings, and open the ⌘/ shortcuts sheet.
	app.Press("id:shortcut-conflict-swap")
	app.Press("id:editor-settings-close")
	app.Key("meta+/")
	app.Screenshot("shortcuts-sheet")
}

func TestShortcutsSearchFocusOnBlurCancel60HzTickStabilityAndSidebarBadge(t *testing.T) {
	ed := newTestEditor(t)
	ed.SelectObject("hero")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true, Locale: "en-US"}))

	app.Post(func() { ed.OpenShortcutsSettings() })
	app.Settle()

	// 1. Typing character-by-character into shortcuts-search never remounts ui.Input or drops focus.
	app.Focus("id:shortcuts-search")
	searchBefore := app.Find("id:shortcuts-search")
	if !searchBefore.Focused {
		t.Fatal("shortcuts-search should be focused")
	}
	for _, ch := range []string{"F", "r", "a", "m", "e"} {
		app.Type("id:shortcuts-search", ch)
		searchAfter := app.Find("id:shortcuts-search")
		if searchAfter.ID != searchBefore.ID {
			t.Fatalf("typing %q remounted shortcuts-search: ID %d -> %d", ch, searchBefore.ID, searchAfter.ID)
		}
		if !searchAfter.Focused {
			t.Fatalf("typing %q dropped focus on shortcuts-search", ch)
		}
	}

	// 2. Unrelated 60 Hz frame/viewport rev ticks do not remount the capture box or drop capture focus.
	app.Press("id:shortcut-bind-" + editor.CmdFrameSelection)
	capBefore := app.Find("id:shortcut-capture-" + editor.CmdFrameSelection)
	if !capBefore.Focused {
		t.Fatal("shortcut-capture-view.frame should be focused")
	}
	app.Post(func() {
		ed.SetShowGrid(true)
		ed.SetShowGrid(false)
		ed.SelectObject("hero")
	})
	app.Settle()
	capAfter := app.Find("id:shortcut-capture-" + editor.CmdFrameSelection)
	if capAfter.ID != capBefore.ID || !capAfter.Focused {
		t.Fatalf("unrelated rev ticks remounted/blurred capture box: before=(%d,%v) after=(%d,%v)", capBefore.ID, capBefore.Focused, capAfter.ID, capAfter.Focused)
	}

	// 3. Blurring the capture box (moving focus to shortcuts-search) cancels capture cleanly so shortcuts never stay stuck.
	app.Focus("id:shortcuts-search")
	if app.Exists("id:shortcut-capture-" + editor.CmdFrameSelection) {
		t.Fatal("blurring capture box did not cancel key capture")
	}

	// 4. Rebinding updates the sidebar "Shortcuts · 1" badge live without remounting shortcuts-search.
	app.Press("id:shortcut-bind-" + editor.CmdFrameSelection)
	app.Key("g")
	if got := allText(app.Find("id:editor-settings-tab-shortcuts")); !strings.Contains(got, "1") {
		t.Fatalf("sidebar shortcuts tab badge = %q, want count 1", got)
	}
	if afterRebind := app.Find("id:shortcuts-search"); afterRebind.ID != searchBefore.ID {
		t.Fatalf("rebinding remounted shortcuts-search: ID %d -> %d", searchBefore.ID, afterRebind.ID)
	}

	// 5. EditorSettings and ShortcutsSheet are mutually exclusive (never stack).
	app.Post(func() { ed.SetShortcutsSheetOpen(true) })
	app.Settle()
	if !ed.ShortcutsSheetOpen() || ed.EditorSettingsOpen() {
		t.Fatalf("expected ShortcutsSheet open and EditorSettings closed, got sheet=%v settings=%v", ed.ShortcutsSheetOpen(), ed.EditorSettingsOpen())
	}
	app.Post(func() { ed.OpenShortcutsSettings() })
	app.Settle()
	if !ed.EditorSettingsOpen() || ed.ShortcutsSheetOpen() {
		t.Fatalf("expected EditorSettings open and ShortcutsSheet closed, got settings=%v sheet=%v", ed.EditorSettingsOpen(), ed.ShortcutsSheetOpen())
	}

	// 6. Entering Play mode closes Editor Settings and clears active capture.
	app.Press("id:shortcut-bind-" + editor.CmdFrameSelection)
	app.Post(func() {
		if err := ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
	})
	app.Settle()
	if ed.EditorSettingsOpen() || ed.ShortcutsSheetOpen() {
		t.Fatalf("Play should close settings/sheet, got settings=%v sheet=%v", ed.EditorSettingsOpen(), ed.ShortcutsSheetOpen())
	}
}

func TestShortcutsRejectWhitespaceReservedKeysAndRoundTripWithOverlays(t *testing.T) {
	// 1. Whitespace inside key tokens is refused by NormalizeShortcut.
	for _, bad := range []string{"mod+a b", "foo bar", "shift+f 1"} {
		if _, err := editor.NormalizeShortcut(bad); err == nil {
			t.Fatalf("NormalizeShortcut(%q) succeeded, want error", bad)
		}
	}

	// 2. Reserved modal/focus/OS shortcuts are refused by RebindShortcut, SwapShortcut, and ParseEditorSettings.
	ed := newTestEditor(t)
	reserved := []string{"escape", "esc", "tab", "shift+tab", "mod+q", "meta+q", "alt+f4", "alt+tab", "meta+tab", "mod+tab", "ctrl+alt+delete"}
	for _, sc := range reserved {
		if err := ed.RebindShortcut(editor.CmdFrameSelection, sc); err == nil {
			t.Fatalf("RebindShortcut(%q) succeeded, want reserved error", sc)
		}
		if err := ed.SwapShortcut(editor.CmdFrameSelection, sc); err == nil {
			t.Fatalf("SwapShortcut(%q) succeeded, want reserved error", sc)
		}
		yamlBytes := []byte("version: 1\nlanguage: en\nappearance: dark\nshortcuts:\n  view.frame: " + sc + "\n")
		if _, err := editor.ParseEditorSettings(yamlBytes); err == nil {
			t.Fatalf("ParseEditorSettings(reserved %q) succeeded, want error", sc)
		}
	}

	// 3. ValidateEditorSettings reports deterministic errors across repeated calls when multiple shortcuts are invalid.
	badSettings := &editor.EditorSettings{
		Version:    editor.SettingsVersion,
		Language:   editor.LanguageEnglish,
		Appearance: editor.AppearanceDark,
		Shortcuts: map[string]string{
			"z.cmd": "bogus+z",
			"a.cmd": "bogus+a",
			"m.cmd": "bogus+m",
		},
	}
	firstErr := editor.ValidateEditorSettings(badSettings)
	if firstErr == nil {
		t.Fatal("ValidateEditorSettings should fail on invalid shortcuts")
	}
	for i := 0; i < 20; i++ {
		if got := editor.ValidateEditorSettings(badSettings); got == nil || got.Error() != firstErr.Error() {
			t.Fatalf("ValidateEditorSettings non-deterministic error: got %v, want %v", got, firstErr)
		}
	}

	// 4. Combined ViewportOverlays + Shortcuts YAML round-trips byte-for-byte.
	combined := []byte("# yaml-language-server: $schema=https://game.nsgo.dev/schemas/editor-settings.v1.json\nversion: 1\nlanguage: system\nappearance: system\nviewportOverlays:\n  - collision_shapes\n  - connections\n  - navigation\n  - triggers\nshortcuts:\n  view.frame: g\n")
	parsed, err := editor.ParseEditorSettings(combined)
	if err != nil {
		t.Fatalf("ParseEditorSettings(combined): %v", err)
	}
	encoded, err := editor.EncodeEditorSettings(parsed)
	if err != nil {
		t.Fatalf("EncodeEditorSettings(combined): %v", err)
	}
	if !bytes.Equal(encoded, combined) {
		t.Fatalf("combined overlays+shortcuts round-trip mismatch:\ngot:\n%s\nwant:\n%s", encoded, combined)
	}

	// 5. Newly merged commands carry accurate scopes.
	wantScopes := map[string]string{
		editor.CmdToggleShowGrid:     editor.ScopeViewport2D,
		editor.CmdToggleSnapPixel:    editor.ScopeViewport2D,
		editor.CmdToggleSnapRotation: editor.ScopeViewport,
		editor.CmdToggleSnapScale:    editor.ScopeViewport,
		editor.CmdViewportOverlays:   editor.ScopeViewport2D,
		editor.CmdAddReflectionProbe: editor.ScopeViewport3D,
		editor.CmdAddLightProbeGrid:  editor.ScopeViewport3D,
		editor.CmdBakeProbes3D:       editor.ScopeViewport3D,
		editor.CmdFindUsages:         editor.ScopeGlobal,
		editor.CmdPlaytestReport:     editor.ScopeGlobal,
		editor.CmdChanges:            editor.ScopeGlobal,
		editor.CmdToggleAssetsMode:   editor.ScopeGlobal,
	}
	for id, want := range wantScopes {
		cmd, ok := ed.Command(id)
		if !ok {
			t.Fatalf("command %q not registered", id)
		}
		if cmd.Scope != want {
			t.Fatalf("command %q Scope = %q, want %q", id, cmd.Scope, want)
		}
	}
}
