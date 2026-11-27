package editor_test

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func setupMainLayoutProject(t *testing.T) (*editor.Editor, string) {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("game.yaml", `version: 1
name: Dungeon
id: dev.example.dungeon
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: level_01
  scenes:
    - name: level_01
      path: scenes/level_01.scene.yaml
`)
	write("prefabs/slime.scene.yaml", "version: 2\nname: Slime\nlayers:\n  - name: root\n    objects:\n      - id: body\n        x: 0\n        y: 0\n")
	write("scenes/level_01.scene.yaml", `version: 3
name: level_01
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    space: world
    order: 0
    objects:
      - id: knight
        name: Knight
        x: 180
        y: 220
      - id: slime_01
        name: Slime 01
        prefab: prefabs/slime.scene.yaml
        x: 340
        y: 240
      - id: door_trigger
        name: Door Trigger
        x: 520
        y: 220
        components:
          trigger: {}
`)

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	_ = ed.SetAutoSave(false)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{
		ID:        "sess-1",
		Name:      "Claude Code",
		Transport: "stdio",
	})
	return ed, dir
}

// TestMainLayoutAt1440x900 verifies every region, dimension, breadcrumb, tool radio group,
// left rail SCENE + FILES, viewport camera caption & bottom bar, right rail Agent/Inspector
// tabs + connection chip, and status bar at 1440x900 (#1007).
func TestMainLayoutAt1440x900(t *testing.T) {
	ed, _ := setupMainLayoutProject(t)
	ed.SetProposedNewObjectIDs([]string{"slime_01"})
	ed.SetViewportPointer2D(312, 184)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), focusDesktopEnv)
	app.Settle()

	// Exact Spec layout dimensions at 1440x900.
	if h := app.Find("id:editor-toolbar").Bounds().Size.Height; h != 44 {
		t.Fatalf("header height = %v, want 44", h)
	}
	if w := app.Find("id:editor-scene-column").Bounds().Size.Width; w != 232 {
		t.Fatalf("left rail width = %v, want 232", w)
	}
	if w := app.Find("id:right-rail-inspector-wrap").Bounds().Size.Width; w != 380 {
		t.Fatalf("right panel width = %v, want 380", w)
	}
	if h := app.Find("id:editor-status").Bounds().Size.Height; h != 32 {
		t.Fatalf("status bar height = %v, want 32", h)
	}

	// Header breadcrumb + unsaved dot (aria-label: "unsaved changes").
	if got := allText(app.Find("id:toolbar-project-name")); got != "Dungeon" {
		t.Fatalf("project breadcrumb = %q, want Dungeon", got)
	}
	if got := strings.TrimSpace(allText(app.Find("id:toolbar-file"))); got != "level_01" {
		t.Fatalf("scene breadcrumb = %q, want level_01", got)
	}
	if app.Exists("id:toolbar-unsaved") {
		t.Fatal("unsaved dot visible before edits")
	}
	app.Post(func() {
		if err := ed.MoveObject("knight", 10, 0); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	unsaved := app.Find("id:toolbar-unsaved")
	if unsaved.Props.Label != "unsaved changes" {
		t.Fatalf("unsaved dot label = %q, want %q", unsaved.Props.Label, "unsaved changes")
	}

	// Header tools (Select, Move, Rotate, Scale, Pan), 2D/3D, History, Search ⌘K, Play.
	for _, id := range []string{
		"header-tool-group", "tool-select", "tool-move", "tool-rotate", "tool-scale", "tool-pan",
		"header-view-mode-group", "view-mode-2d", "view-mode-3d",
		"toolbar-history", "toolbar-palette", "toolbar-play",
	} {
		if !app.Exists("id:" + id) {
			t.Fatalf("missing header control %q", id)
		}
	}
	if got := allText(app.Find("id:toolbar-search-shortcut")); got != "⌘K" {
		t.Fatalf("search shortcut text = %q, want ⌘K", got)
	}

	// Tool radio group switches active tool.
	app.Press("id:tool-move")
	app.Settle()
	if ed.Tool != editor.ToolMove || !app.Find("id:tool-move").Props.Checked {
		t.Fatalf("after pressing tool-move: Tool=%v checked=%v", ed.Tool, app.Find("id:tool-move").Props.Checked)
	}
	app.Press("id:tool-pan")
	app.Settle()
	if ed.Tool != editor.ToolPan || !app.Find("id:tool-pan").Props.Checked {
		t.Fatalf("after pressing tool-pan: Tool=%v checked=%v", ed.Tool, app.Find("id:tool-pan").Props.Checked)
	}
	app.Press("id:tool-select")
	app.Settle()

	// 2D / 3D segmented radio group switches ViewMode.
	app.Press("id:view-mode-3d")
	app.Settle()
	if ed.ViewMode != editor.ViewMode3D || !app.Find("id:view-mode-3d").Props.Checked {
		t.Fatalf("after pressing view-mode-3d: ViewMode=%v checked=%v", ed.ViewMode, app.Find("id:view-mode-3d").Props.Checked)
	}
	app.Press("id:view-mode-2d")
	app.Settle()

	// Left rail: SCENE · N nodes, kind tags, NEW badge on proposed object, and FILES section.
	if got := allText(app.Find("id:scene-rail-title")); got != "SCENE" {
		t.Fatalf("left rail title = %q, want SCENE", got)
	}
	if got := allText(app.Find("id:scene-rail-node-count")); !strings.Contains(got, "nodes") {
		t.Fatalf("left rail node count = %q, want N nodes", got)
	}
	if !app.Exists("id:scene-tree-new-slime_01") {
		t.Fatal("missing NEW tag on proposed object slime_01")
	}
	if got := allText(app.Find("id:scene-tree-kind-slime_01")); got != "prefab" {
		t.Fatalf("slime_01 kind tag = %q, want prefab", got)
	}
	if got := allText(app.Find("id:scene-tree-kind-door_trigger")); got != "trigger" {
		t.Fatalf("door_trigger kind tag = %q, want trigger", got)
	}

	if got := allText(app.Find("id:left-rail-files-title")); got != "FILES" {
		t.Fatalf("FILES title = %q, want FILES", got)
	}
	if got := strings.TrimSpace(allText(app.Find("id:rail-file-scene"))); got != "scenes/level_01.scene.yaml" {
		t.Fatalf("rail scene file = %q, want scenes/level_01.scene.yaml", got)
	}
	if got := strings.TrimSpace(allText(app.Find("id:rail-file-prefab-0"))); got != "prefabs/slime.scene.yaml" {
		t.Fatalf("rail prefab file = %q, want prefabs/slime.scene.yaml", got)
	}
	if got := strings.TrimSpace(allText(app.Find("id:rail-file-project"))); got != "game.yaml" {
		t.Fatalf("rail project file = %q, want game.yaml", got)
	}

	// Viewport camera caption and bottom bar (Zoom out, %, Zoom in, Snap switch, Fit, Overlays N).
	if got := allText(app.Find("id:viewport-camera-caption")); got != "Camera · 640 × 360" {
		t.Fatalf("viewport camera caption = %q, want %q", got, "Camera · 640 × 360")
	}
	for _, id := range []string{
		"viewport-zoom-out", "viewport-zoom-label", "viewport-zoom-in",
		"viewport-bar-snap", "viewport-bar-fit", "viewport-overlays-button",
	} {
		if !app.Exists("id:" + id) {
			t.Fatalf("missing viewport bottom bar control %q", id)
		}
	}
	snapNode := app.Find("id:viewport-bar-snap")
	if snapNode.Props.Role != core.RoleSwitch {
		t.Fatalf("viewport-bar-snap role = %v, want RoleSwitch", snapNode.Props.Role)
	}
	beforeSnap := ed.SnapToGrid
	app.Press("id:viewport-bar-snap")
	app.Settle()
	if ed.SnapToGrid == beforeSnap || app.Find("id:viewport-bar-snap").Props.Checked != ed.SnapToGrid {
		t.Fatalf("Snap switch failed to toggle: before=%v after=%v checked=%v", beforeSnap, ed.SnapToGrid, app.Find("id:viewport-bar-snap").Props.Checked)
	}

	// Right rail: switch to Agent tab, verify Connected · MCP chip & 380px width; picking a tree row switches back to Inspector.
	app.Press("id:right-tab-agent")
	app.Settle()
	if ed.RightRailTab() != editor.RightTabAgent {
		t.Fatalf("after clicking Agent tab: tab = %v, want RightTabAgent", ed.RightRailTab())
	}
	if w := app.Find("id:agent-panel").Bounds().Size.Width; w != 380 {
		t.Fatalf("agent right rail width = %v, want 380", w)
	}
	if got := allText(app.Find("id:agent-status-chip")); !strings.Contains(got, "Connected") || !strings.Contains(got, "MCP") {
		t.Fatalf("agent status chip = %q, want Connected · MCP", got)
	}
	app.Press("id:scene-tree-label-knight")
	app.Settle()
	if ed.RightRailTab() != editor.RightTabInspector {
		t.Fatalf("picking tree row did not switch right rail to Inspector: tab = %v", ed.RightRailTab())
	}
	if w := app.Find("id:right-rail-inspector-wrap").Bounds().Size.Width; w != 380 {
		t.Fatalf("inspector right rail width = %v, want 380", w)
	}

	// Status bar: active layer, pointer coordinates, current tool, Problems, Output, Debugger.
	app.Post(func() { ed.SetViewportPointer2D(312, 184) })
	app.Settle()
	if got := allText(app.Find("id:status-active-layer")); !strings.Contains(got, "actors") {
		t.Fatalf("status active layer = %q, want actors", got)
	}
	if wx, wy, ok := ed.ViewportPointer2DWorld(); !ok {
		t.Fatal("ViewportPointer2DWorld() reported ok=false after SetViewportPointer2D")
	} else {
		wantCoords := fmt.Sprintf("%d, %d", int(math.Round(float64(wx))), int(math.Round(float64(wy))))
		if got := allText(app.Find("id:status-pointer-coords")); got != wantCoords {
			t.Fatalf("status pointer coords = %q, want %q", got, wantCoords)
		}
	}
	if got := allText(app.Find("id:editor-status-mode")); got != "Select" {
		t.Fatalf("status tool = %q, want Select", got)
	}
	app.Press("id:status-problems-pill")
	app.Settle()
	if !ed.ProblemAgentPanelOpen() {
		t.Fatal("status-problems-pill did not open Problems panel")
	}
	app.Press("id:status-output")
	app.Settle()
	if !ed.OutputPanelOpen() {
		t.Fatal("status-output did not open Output panel")
	}
	app.Press("id:status-debugger")
	app.Settle()
	if !ed.ProfilerOpen() {
		t.Fatal("status-debugger did not open Debugger/Profiler panel")
	}
}

// TestMainLayoutSpecControlsA11yAndHitAreas verifies >=28x28 hit targets, accessible labels,
// radio group arrow navigation, switch semantics, and 5-region F6 traversal (#1007).
func TestMainLayoutSpecControlsA11yAndHitAreas(t *testing.T) {
	ed, _ := setupMainLayoutProject(t)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), focusDesktopEnv)
	app.Settle()

	controls := []string{
		"toolbar-file",
		"tool-select", "tool-move", "tool-rotate", "tool-scale", "tool-pan",
		"view-mode-2d", "view-mode-3d",
		"toolbar-history", "toolbar-palette", "toolbar-play",
		"rail-file-scene", "rail-file-prefab-0", "rail-file-project",
		"viewport-zoom-out", "viewport-zoom-in", "viewport-bar-snap", "viewport-bar-fit", "viewport-overlays-button",
		"right-tab-agent", "right-tab-inspector", "agent-status-chip",
		"status-active-layer", "status-problems-pill", "status-output", "status-debugger",
	}
	for _, id := range controls {
		v := app.Find("id:" + id)
		sz := v.Bounds().Size
		if sz.Width < 28 || sz.Height < 28 {
			t.Errorf("control %q hit area = %.1fx%.1f, want >= 28x28", id, sz.Width, sz.Height)
		}
		if strings.TrimSpace(v.Props.Label) == "" && strings.TrimSpace(allText(v)) == "" {
			t.Errorf("control %q has neither accessible Label nor text", id)
		}
	}

	// Verify radio groups use RoleRadioGroup / RoleRadio and respond to arrow keys.
	if got := app.Find("id:header-tool-group").Props.Role; got != core.RoleRadioGroup {
		t.Fatalf("header-tool-group role = %v, want RoleRadioGroup", got)
	}
	if got := app.Find("id:header-view-mode-group").Props.Role; got != core.RoleRadioGroup {
		t.Fatalf("header-view-mode-group role = %v, want RoleRadioGroup", got)
	}
	if got := app.Find("id:right-rail-tabs").Props.Role; got != core.RoleRadioGroup {
		t.Fatalf("right-rail-tabs role = %v, want RoleRadioGroup", got)
	}
	app.Focus("id:tool-select")
	app.Key("right")
	app.Settle()
	if ed.Tool != editor.ToolMove {
		t.Fatalf("Right arrow in tool radio group gave Tool=%v, want ToolMove", ed.Tool)
	}
	app.Focus("id:view-mode-2d")
	app.Key("right")
	app.Settle()
	if ed.ViewMode != editor.ViewMode3D {
		t.Fatalf("Right arrow in 2D/3D radio group gave ViewMode=%v, want ViewMode3D", ed.ViewMode)
	}
	app.Key("left")
	app.Settle()

	// Verify keyboard activation of Snap switch via Space.
	beforeSnap := ed.SnapToGrid
	app.Focus("id:viewport-bar-snap")
	app.Key("space")
	app.Settle()
	if ed.SnapToGrid == beforeSnap {
		t.Fatalf("Space on focused viewport-bar-snap did not toggle SnapToGrid (stayed %v)", beforeSnap)
	}

	// Verify F6 cycles through all 5 main regions (Header, Scene tree, Viewport, Agent/Inspector, Status bar).
	wantRegions := []editor.EditorRegion{
		editor.RegionHeader,
		editor.RegionSceneTree,
		editor.RegionViewport,
		editor.RegionInspector,
		editor.RegionStatusBar,
	}
	seen := make(map[editor.EditorRegion]bool)
	for i := 0; i < 6; i++ {
		app.Key("f6")
		app.Settle()
		if r := ed.ActiveRegion(); r != "" {
			seen[r] = true
		}
	}
	for _, r := range wantRegions {
		if !seen[r] {
			t.Errorf("F6 did not visit region %q (visited: %+v)", r, seen)
		}
	}
}

// TestMainLayoutLegacyPanelsRemainReachable verifies every view previously opened by the
// legacy toolbar remains reachable via Command Palette / Status bar / Settings (#1007).
func TestMainLayoutLegacyPanelsRemainReachable(t *testing.T) {
	ed, _ := setupMainLayoutProject(t)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), focusDesktopEnv)
	app.Settle()

	ed.SelectObject("knight")
	cases := []struct {
		name  string
		cmdID string
		check func() bool
		close func()
	}{
		{"Project Settings", editor.CmdProjectSettings, ed.ProjectSettingsOpen, func() { ed.SetProjectSettingsOpen(false) }},
		{"Editor Settings", editor.CmdEditorSettings, ed.SettingsOpen, func() { ed.CloseSettings() }},
		{"Audio Mixer", editor.CmdAudioMixer, ed.AudioMixerOpen, func() { ed.SetAudioMixerOpen(false) }},
		{"Input Map", editor.CmdInputMap, ed.InputMapOpen, func() { ed.SetInputMapOpen(false) }},
		{"Strings / Localization", editor.CmdLocalization, ed.LocalizationOpen, func() { _ = ed.ExecuteCommand(editor.CmdLocalization) }},
		{"Export", editor.CmdExport, ed.ExportPanelOpen, func() { ed.SetExportPanelOpen(false) }},
		{"Data Table", editor.CmdDataTable, func() bool { return ed.DataTable() != nil }, func() { _ = ed.ExecuteCommand(editor.CmdDataTable) }},
		{"Project Search", editor.CmdSearchPanel, ed.SearchOpen, func() { ed.SetSearchOpen(false) }},
		{"Output", editor.CmdOutputPanel, ed.OutputPanelOpen, func() { ed.SetOutputPanelOpen(false) }},
		{"Problems", editor.CmdProblemsPanel, ed.ProblemAgentPanelOpen, func() { ed.SetProblemAgentPanelOpen(false) }},
		{"Debugger / Profiler", editor.CmdProfiler, ed.ProfilerOpen, func() { ed.SetProfilerOpen(false) }},
		{"Layers", editor.CmdLayersPanel, ed.LayersPanelOpen, func() { ed.SetLayersPanelOpen(false) }},
		{"Files Browser", editor.CmdFileBrowser, ed.FileBrowserOpen, func() { ed.SetFileBrowserOpen(false) }},
		{"Asset Browser", editor.CmdAssetBrowser, ed.AssetBrowserOpen, func() { ed.SetAssetBrowserOpen(false) }},
		{"Particles", editor.CmdParticleEditor, ed.ParticleEmitterOpen, func() { _ = ed.ExecuteCommand(editor.CmdParticleEditor) }},
		{"Material", editor.CmdMaterialEditor, ed.MaterialOpen, func() { _ = ed.ExecuteCommand(editor.CmdMaterialEditor) }},
		{"HUD Layout", editor.CmdHUDLayout, ed.HUDLayoutOpen, func() { _ = ed.ExecuteCommand(editor.CmdHUDLayout) }},
		{"Terrain", editor.CmdTerrainEditor, ed.TerrainOpen, func() { _ = ed.ExecuteCommand(editor.CmdTerrainEditor) }},
		{"NavMesh", editor.CmdNavMeshPanel, ed.NavMeshPanelOpen, func() { ed.SetNavMeshPanelOpen(false) }},
		{"History", editor.CmdHistoryPanel, ed.HistoryPanelOpen, func() { ed.SetHistoryPanelOpen(false) }},
		{"Animate", editor.CmdAnimate, func() bool { return ed.Clip() != nil }, func() { ed.CloseClip() }},
	}

	for _, tc := range cases {
		runEditorCommand(t, app, ed, tc.cmdID)
		if !tc.check() {
			t.Errorf("%s (%s) did not open", tc.name, tc.cmdID)
		}
		app.Post(tc.close)
		app.Settle()
	}

	// Library command is registered in the Command Palette and toggles when a Library client is configured.
	foundLib := false
	for _, cmd := range ed.Commands() {
		if cmd.ID == editor.CmdLibraryPanel {
			foundLib = true
			break
		}
	}
	if !foundLib {
		t.Fatal("CmdLibraryPanel missing from Command Palette registry")
	}
}

// TestMainLayoutEmptyLoadingAndErrorStates verifies empty (no project, no scene), loading,
// and scene-open error states across regions, including opening a missing scene and a
// malformed YAML scene (matching open_errors_test.go) and recovering via Retry (#1007).
func TestMainLayoutEmptyLoadingAndErrorStates(t *testing.T) {
	// 1. No project open -> designed empty state across regions.
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	ed.SetRightRailTab(editor.RightTabInspector)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), focusDesktopEnv)
	app.Settle()

	if ed.WorkspaceState() != editor.WorkspaceNoProject {
		t.Fatalf("WorkspaceState = %v, want WorkspaceNoProject", ed.WorkspaceState())
	}
	for _, id := range []string{"viewport-empty-no-project", "left-rail-empty-no-project", "inspector-empty-no-project"} {
		if !app.Exists("id:" + id) {
			t.Fatalf("NoProject state missing %q", id)
		}
	}

	// 2. Project loading -> designed loading state across regions.
	app.Post(func() { ed.SetProjectLoading(true, "/tmp/demo/game.yaml") })
	app.Settle()
	if ed.WorkspaceState() != editor.WorkspaceLoading {
		t.Fatalf("WorkspaceState = %v, want WorkspaceLoading", ed.WorkspaceState())
	}
	for _, id := range []string{"viewport-loading-state", "left-rail-loading", "inspector-loading-state"} {
		if !app.Exists("id:" + id) {
			t.Fatalf("Loading state missing %q", id)
		}
	}
	app.Post(func() { ed.SetProjectLoading(false, "") })
	app.Settle()

	// 3. Bare project with no scene -> designed no-scene empty state across regions.
	bareDir := writeBareProject(t, "BareProj")
	app.Post(func() {
		if err := ed.OpenProject(bareDir); err != nil {
			t.Error(err)
		}
		ed.SetRightRailTab(editor.RightTabInspector)
	})
	app.Settle()
	if ed.WorkspaceState() != editor.WorkspaceNoScene {
		t.Fatalf("WorkspaceState = %v, want WorkspaceNoScene", ed.WorkspaceState())
	}
	for _, id := range []string{"viewport-empty-no-scene", "left-rail-empty-no-scene", "inspector-empty-no-scene"} {
		if !app.Exists("id:" + id) {
			t.Fatalf("NoScene state missing %q", id)
		}
	}

	// 4. Open a missing scene -> designed RoleAlert error state (not a blank viewport).
	missingPath := filepath.Join(bareDir, "scenes", "missing.scene.yaml")
	var openErr error
	app.Post(func() {
		openErr = ed.OpenFile(missingPath)
	})
	app.Settle()
	if openErr == nil {
		t.Fatal("expected error opening missing scene")
	}
	if ed.WorkspaceState() != editor.WorkspaceSceneError {
		t.Fatalf("WorkspaceState after missing scene = %v, want WorkspaceSceneError", ed.WorkspaceState())
	}
	errCard := app.Find("id:viewport-error-state")
	if errCard.Props.Role != core.RoleAlert {
		t.Fatalf("viewport-error-state role = %v, want RoleAlert", errCard.Props.Role)
	}
	if got := allText(app.Find("id:viewport-error-path")); !strings.Contains(got, "missing.scene.yaml") {
		t.Fatalf("viewport-error-path = %q, want missing.scene.yaml", got)
	}
	if !app.Exists("id:viewport-error-message") || !app.Exists("id:viewport-error-fix") || !app.Exists("id:left-rail-scene-error") || !app.Exists("id:inspector-error-state") {
		t.Fatal("missing designed error-state details across regions")
	}

	// 5. Open a malformed YAML scene (open_errors_test.go case) -> path:line shown, Retry recovers once fixed.
	badPath := filepath.Join(bareDir, "scenes", "cave.scene.yaml")
	if err := os.MkdirAll(filepath.Dir(badPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(badPath, []byte("version: 1\nlayers:\n  - name: bg\n    space: isometric\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app.Post(func() {
		openErr = ed.OpenFile(badPath)
	})
	app.Settle()
	if openErr == nil {
		t.Fatal("expected error opening malformed scene")
	}
	if got := allText(app.Find("id:viewport-error-path")); !strings.Contains(got, "cave.scene.yaml:4") {
		t.Fatalf("malformed scene error path = %q, want cave.scene.yaml:4", got)
	}

	// Fix the file and click Retry -> returns to WorkspaceReady and clears the error card.
	if err := os.WriteFile(badPath, []byte("version: 1\nlayers:\n  - name: bg\n    space: world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app.Press("id:viewport-error-retry")
	app.Settle()
	if ed.WorkspaceState() != editor.WorkspaceReady {
		t.Fatalf("after Retry on fixed scene: WorkspaceState = %v, want WorkspaceReady", ed.WorkspaceState())
	}
	if app.Exists("id:viewport-error-state") {
		t.Fatal("error card remained visible after successful Retry")
	}

	// Dismissing an error state also clears it cleanly.
	app.Post(func() { _ = ed.OpenScene("scenes/does_not_exist.scene.yaml") })
	app.Settle()
	if !app.Exists("id:viewport-error-state") {
		t.Fatal("expected error state after OpenScene on missing project scene")
	}
	if app.Exists("id:viewport-overlay-bar") {
		t.Fatal("viewport bottom bar remained mounted behind error overlay")
	}
	app.Press("id:viewport-error-dismiss")
	app.Settle()
	if app.Exists("id:viewport-error-state") || ed.SceneOpenError() != nil {
		t.Fatal("Dismiss did not clear scene-open error state")
	}
}

// TestMainLayoutHardening verifies Agent tab preservation on non-tree selection,
// deterministic multi-component kind classification, and lifecycle resets for
// loading/error/proposed-new states (#1007).
func TestMainLayoutHardening(t *testing.T) {
	ed, dir := setupMainLayoutProject(t)
	ed.SetProposedNewObjectIDs([]string{"slime_01"})
	ed.SetRightRailTab(editor.RightTabAgent)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), focusDesktopEnv)
	app.Settle()

	// 1. Selecting an object outside the scene tree (e.g. canvas click / agent action)
	// must NOT yank the right rail away from the Agent tab.
	app.Post(func() { ed.SelectObject("knight") })
	app.Settle()
	if ed.RightRailTab() != editor.RightTabAgent {
		t.Fatalf("programmatic/canvas selection switched right rail to %v, want RightTabAgent", ed.RightRailTab())
	}
	// Clicking a row in the scene tree DOES switch to Inspector.
	app.Press("id:scene-tree-label-door_trigger")
	app.Settle()
	if ed.RightRailTab() != editor.RightTabInspector {
		t.Fatalf("scene tree row click gave right rail %v, want RightTabInspector", ed.RightRailTab())
	}

	// 2. SceneRailObjectKind is deterministic even when an object carries multiple kind components.
	app.Post(func() {
		if err := ed.SetComponentField("door_trigger", "light", "intensity", 1.0); err != nil {
			t.Error(err)
		}
		if err := ed.SetComponentField("door_trigger", "camera", "zoom", 1.0); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	for i := 0; i < 32; i++ {
		if got := ed.SceneRailObjectKind("door_trigger"); got != "camera" {
			t.Fatalf("iteration %d: SceneRailObjectKind = %q, want deterministic \"camera\"", i, got)
		}
	}

	// 3. Failed OpenProjectScene while ProjectLoading is true transitions to WorkspaceSceneError
	// (never stuck in WorkspaceLoading), and SelectTab / OpenDemo clear stale errors & proposedNewObjects.
	app.Post(func() {
		ed.SetProjectLoading(true, filepath.Join(dir, "game.yaml"))
		_ = ed.OpenProjectScene(dir, filepath.Join(dir, "scenes", "missing.scene.yaml"))
	})
	app.Settle()
	if ed.ProjectLoading() {
		t.Fatal("ProjectLoading remained true after failed OpenProjectScene")
	}
	if ed.WorkspaceState() != editor.WorkspaceSceneError {
		t.Fatalf("WorkspaceState = %v, want WorkspaceSceneError", ed.WorkspaceState())
	}
	app.Post(func() {
		if err := ed.SelectTab(ed.ActiveTab()); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	if ed.SceneOpenError() != nil {
		t.Fatal("SelectTab on active valid tab did not clear SceneOpenError")
	}

	// OpenDemo clears proposedNewObjects and sceneOpenErr cleanly.
	app.Post(func() {
		ed.SetProposedNewObjectIDs([]string{"slime_01"})
		_ = ed.OpenScene("scenes/missing.scene.yaml")
		if err := ed.OpenDemo(); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	if ed.SceneOpenError() != nil {
		t.Fatal("OpenDemo did not clear SceneOpenError")
	}
	if ed.IsObjectProposedNew("slime_01") {
		t.Fatal("OpenDemo did not clear proposedNewObjects")
	}
}
