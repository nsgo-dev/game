package editor_test

import (
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/base/dock"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func dockSurfaceBounds(v *nsgotest.View) core.Bounds {
	b := v.Frame
	for p := v.Parent; p != nil; p = p.Parent {
		b.Origin.X += p.Frame.Origin.X - p.Offset.X
		b.Origin.Y += p.Frame.Origin.Y - p.Offset.Y
	}
	return b
}

func dockRectCenter(b core.Bounds) core.Point {
	return core.Point{X: b.Origin.X + b.Size.Width/2, Y: b.Origin.Y + b.Size.Height/2}
}

func findGroupInNode(n *dock.Node, panel string) *dock.Node {
	if n == nil {
		return nil
	}
	if n.Tabs {
		if slices.Contains(n.Panels, panel) {
			return n
		}
		return nil
	}
	for i := range n.Children {
		if g := findGroupInNode(&n.Children[i], panel); g != nil {
			return g
		}
	}
	return nil
}

func findGroupInLayout(l dock.Layout, panel string) (dock.Placement, *dock.Node) {
	if g := findGroupInNode(&l.Center, panel); g != nil {
		return dock.Center, g
	}
	for _, p := range []dock.Placement{dock.Left, dock.Right, dock.Bottom} {
		if r := l.Region(p); r != nil {
			if g := findGroupInNode(&r.Root, panel); g != nil {
				return p, g
			}
		}
	}
	return dock.Center, nil
}

// Acceptance #1 (#919): A test moves the inspector into a tab with the files panel,
// restarts the editor from the saved file, and finds the same layout.
func TestDockLayoutMoveInspectorWithFilesAndRestart(t *testing.T) {
	dir := writeEditorProject(t)
	layoutFile := filepath.Join(t.TempDir(), "nsgo-game", editor.LayoutFileName)

	ed1, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed1.SetLayoutPath(layoutFile); err != nil {
		t.Fatalf("SetLayoutPath: %v", err)
	}
	if err := ed1.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	ed1.SetFileBrowserOpen(true)

	app1 := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed1)
	}, nsgotest.Size(1280, 760), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	// Drag the Inspector tab onto the middle of the Files panel to tab them together.
	from := dockRectCenter(dockSurfaceBounds(app1.Find("id:dock-tab-inspector")))
	to := dockRectCenter(dockSurfaceBounds(app1.Find("id:file-browser")))
	app1.Drag("id:dock-tab-inspector", core.Point{X: 10}, core.Point{X: to.X - from.X, Y: to.Y - from.Y})
	app1.Settle()

	p1, g1 := findGroupInLayout(ed1.DockLayout(), editor.PanelInspector)
	if p1 != dock.Left || g1 == nil || !slices.Equal(g1.Panels, []string{editor.PanelFiles, editor.PanelInspector}) || g1.Active != 1 {
		t.Fatalf("after drag: placement=%v group=%+v, want Left [files inspector] active=1", p1, g1)
	}

	// Verify the layout YAML file was saved outside the project directory and parses strictly.
	raw, err := os.ReadFile(layoutFile)
	if err != nil {
		t.Fatalf("saved layout file not written at %s: %v", layoutFile, err)
	}
	if _, err := editor.ParseLayout(raw); err != nil {
		t.Fatalf("saved layout file does not parse strictly: %v\n%s", err, raw)
	}
	if _, err := os.Stat(filepath.Join(dir, editor.LayoutFileName)); !os.IsNotExist(err) {
		t.Fatalf("layout.yaml must live outside the project directory, found in project: %v", err)
	}

	// Restart a brand-new editor from the saved layout file and verify the exact same layout is restored.
	ed2, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed2.SetLayoutPath(layoutFile); err != nil {
		t.Fatalf("restarted SetLayoutPath: %v", err)
	}
	if err := ed2.OpenProject(dir); err != nil {
		t.Fatalf("restarted OpenProject: %v", err)
	}
	app2 := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed2)
	}, nsgotest.Size(1280, 760), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	p2, g2 := findGroupInLayout(ed2.DockLayout(), editor.PanelInspector)
	if p2 != dock.Left || g2 == nil || !slices.Equal(g2.Panels, []string{editor.PanelFiles, editor.PanelInspector}) || g2.Active != 1 {
		t.Fatalf("after restart: placement=%v group=%+v, want Left [files inspector] active=1", p2, g2)
	}
	if !app2.Exists("id:editor-inspector") {
		t.Fatal("restarted editor should display inspector as the active tab")
	}
	if app2.Exists("id:file-browser") {
		t.Fatal("files panel should be an inactive tab behind inspector until selected")
	}

	// Switching to the Files tab in that same group shows the file browser.
	app2.Press("id:dock-tab-files")
	app2.Settle()
	if !app2.Exists("id:file-browser") || app2.Exists("id:editor-inspector") {
		t.Fatal("clicking the files tab did not switch tabs in the shared group")
	}
}

// Acceptance #2 (#919): A plugin panel appears at its declared dock on first use,
// and where the user moved it afterwards.
func TestDockLayoutPluginPanelDefaultDockThenRememberedMove(t *testing.T) {
	dir := writePluginProject(t, "health")
	layoutFile := filepath.Join(t.TempDir(), "layout.yaml")
	panelKey := editor.PluginPanelKey("health", "overview")

	// First use (no saved layout file yet): plugin panel appears at its declared DockRight.
	ed1 := newPluginEditor(t, dir)
	if err := ed1.SetLayoutPath(layoutFile); err != nil {
		t.Fatalf("SetLayoutPath: %v", err)
	}
	ed1.SetFileBrowserOpen(true)
	app1 := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed1)
	}, nsgotest.Size(1400, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	if !app1.Exists("id:plugin-panel-health-overview") {
		t.Fatal("plugin panel did not appear on first use")
	}
	inspBounds := app1.Find("id:editor-inspector").Bounds()
	pluginBounds := app1.Find("id:plugin-panel-health-overview").Bounds()
	if pluginBounds.Origin.X < inspBounds.Origin.X+inspBounds.Size.Width-2 {
		t.Fatalf("first use: plugin panel %+v should appear at its declared right dock beside inspector %+v", pluginBounds, inspBounds)
	}

	// User drags the plugin panel into the Left dock onto the Files panel.
	from := dockRectCenter(dockSurfaceBounds(app1.Find("id:dock-tab-" + panelKey)))
	to := dockRectCenter(dockSurfaceBounds(app1.Find("id:file-browser")))
	app1.Drag("id:dock-tab-"+panelKey, core.Point{X: 10}, core.Point{X: to.X - from.X, Y: to.Y - from.Y})
	app1.Settle()

	p1, g1 := findGroupInLayout(ed1.DockLayout(), panelKey)
	if p1 != dock.Left || g1 == nil || !slices.Contains(g1.Panels, panelKey) || !slices.Contains(g1.Panels, editor.PanelFiles) {
		t.Fatalf("after drag: placement=%v group=%+v, want Left tabbed with files", p1, g1)
	}

	// Restart a new editor from the saved layout file: plugin panel must appear where the user moved it (Left), not back at DockRight.
	ed2 := newPluginEditor(t, dir)
	if err := ed2.SetLayoutPath(layoutFile); err != nil {
		t.Fatalf("restarted SetLayoutPath: %v", err)
	}
	app2 := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed2)
	}, nsgotest.Size(1400, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	p2, g2 := findGroupInLayout(ed2.DockLayout(), panelKey)
	if p2 != dock.Left || g2 == nil || !slices.Contains(g2.Panels, panelKey) || !slices.Contains(g2.Panels, editor.PanelFiles) {
		t.Fatalf("after restart: placement=%v group=%+v, want Left tabbed with files", p2, g2)
	}
	if !app2.Exists("id:plugin-panel-health-overview") {
		t.Fatal("restarted editor did not show plugin panel at its moved location")
	}
	movedBounds := app2.Find("id:plugin-panel-health-overview").Bounds()
	vpBounds := app2.Find("id:editor-viewport").Bounds()
	if movedBounds.Origin.X+movedBounds.Size.Width > vpBounds.Origin.X+2 {
		t.Fatalf("restarted plugin panel %+v should remain on the left of viewport %+v", movedBounds, vpBounds)
	}
}

func TestDockLayoutMenuShowHideResizeAndReset(t *testing.T) {
	dir := writeEditorProject(t)
	layoutFile := filepath.Join(t.TempDir(), "layout.yaml")

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.SetLayoutPath(layoutFile); err != nil {
		t.Fatalf("SetLayoutPath: %v", err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 760), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	// Hide Inspector via View > Panels > Inspector.
	if !app.Exists("id:editor-inspector") {
		t.Fatal("inspector should be visible initially")
	}
	app.ChooseMenu("View", "Panels", "Inspector")
	app.Settle()
	if app.Exists("id:editor-inspector") || ed.PanelVisible(editor.PanelInspector) {
		t.Fatal("View > Panels > Inspector did not hide inspector")
	}

	// Show Inspector again via View > Panels > Inspector.
	app.ChooseMenu("View", "Panels", "Inspector")
	app.Settle()
	if !app.Exists("id:editor-inspector") || !ed.PanelVisible(editor.PanelInspector) {
		t.Fatal("View > Panels > Inspector did not restore inspector")
	}

	// Show auxiliary Files and Layers panels via View > Panels.
	app.ChooseMenu("View", "Panels", "Files")
	app.Settle()
	app.ChooseMenu("View", "Panels", "Layers")
	app.Settle()
	if !app.Exists("id:file-browser") || !ed.FileBrowserOpen() {
		t.Fatal("View > Panels > Files did not open file browser")
	}
	if !app.Exists("id:layers-panel") || !ed.LayersPanelOpen() {
		t.Fatal("View > Panels > Layers did not open layers panel")
	}

	// Resize Left dock through AreaRef and verify the new size persists across restart.
	app.Post(func() {
		ed.DockAreaRef().SetDockSize(dock.Left, 360)
	})
	app.Settle()
	if got := ed.DockLayout().Left.Size; got != 360 {
		t.Fatalf("Left dock size = %v, want 360", got)
	}

	// Move Inspector into Files and hide Layers, then Reset Layout via View > Panels > Reset Layout.
	app.Post(func() {
		if !ed.MovePanel(editor.PanelInspector, editor.PanelFiles) {
			t.Error("MovePanel(inspector, files) failed")
		}
		ed.SetPanelVisible(editor.PanelLayers, false)
	})
	app.Settle()
	if app.Exists("id:layers-panel") {
		t.Fatal("layers panel should be hidden")
	}

	app.ChooseMenu("View", "Panels", "Reset Layout")
	app.Settle()

	if !app.Exists("id:editor-scene-column") || !app.Exists("id:editor-inspector") {
		t.Fatal("Reset Layout did not restore default panels")
	}
	if app.Exists("id:layers-panel") || app.Exists("id:file-browser") {
		t.Fatal("Reset Layout should restore AI-first default with auxiliary panels closed")
	}
	p, g := findGroupInLayout(ed.DockLayout(), editor.PanelInspector)
	if p != dock.Center || g == nil || !slices.Equal(g.Panels, []string{editor.PanelInspector}) {
		t.Fatalf("after Reset Layout: inspector placement=%v group=%+v, want Center [inspector]", p, g)
	}
	if got := ed.DockLayout().Left.Size; got != 232 {
		t.Fatalf("after Reset Layout: Left dock size = %v, want 232", got)
	}
}

func TestDockLayoutDropsPanelsThatNoLongerExist(t *testing.T) {
	layoutFile := filepath.Join(t.TempDir(), "layout.yaml")
	yamlWithGonePanels := `version: 1
center:
  axis: horizontal
  sizes: [0, 272]
  children:
    - panels:
        - viewport
        - deleted_builtin_panel
    - panels:
        - inspector
left:
  size: 444
  root:
    axis: horizontal
    sizes: [220, 224]
    children:
      - panels:
          - scene_tree
          - plugin:removed_plugin:ghost
      - panels:
          - assets
right:
  size: 200
  root:
    panels:
      - plugin:uninstalled:only_panel
hidden:
  - layers
  - gone_hidden_panel
`
	if err := os.WriteFile(layoutFile, []byte(yamlWithGonePanels), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.SetLayoutPath(layoutFile); err != nil {
		t.Fatalf("SetLayoutPath: %v", err)
	}

	l := ed.DockLayout()
	if l.Right != nil {
		t.Fatalf("Right dock containing only a removed plugin panel should be dropped, got %+v", l.Right)
	}
	for _, gone := range []string{"deleted_builtin_panel", "plugin:removed_plugin:ghost", "plugin:uninstalled:only_panel", "gone_hidden_panel"} {
		if _, g := findGroupInLayout(l, gone); g != nil {
			t.Fatalf("removed panel %q was not dropped from DockLayout", gone)
		}
	}

	// Saving drops the non-existent panels from disk as well.
	if err := ed.SaveLayout(); err != nil {
		t.Fatalf("SaveLayout: %v", err)
	}
	saved, err := os.ReadFile(layoutFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"deleted_builtin_panel", "plugin:removed_plugin:ghost", "plugin:uninstalled:only_panel", "gone_hidden_panel"} {
		if strings.Contains(string(saved), gone) {
			t.Fatalf("saved layout still contains dropped panel %q:\n%s", gone, saved)
		}
	}
}

func TestParseLayoutRefusesInvalidDocuments(t *testing.T) {
	cases := map[string]string{
		"empty":           "",
		"missing version": "center:\n  panels: [viewport]\n",
		"zero version":    "version: 0\ncenter:\n  panels: [viewport]\n",
		"future version":  "version: 2\ncenter:\n  panels: [viewport]\n",
		"unknown field":   "version: 1\nextra: true\ncenter:\n  panels: [viewport]\n",
		"duplicate key":   "version: 1\nversion: 1\ncenter:\n  panels: [viewport]\n",
		"trailing doc":    "version: 1\ncenter:\n  panels: [viewport]\n---\nversion: 1\n",
		"mixed node":      "version: 1\ncenter:\n  panels: [viewport]\n  axis: horizontal\n",
		"empty node":      "version: 1\ncenter: {}\n",
		"bad axis":        "version: 1\ncenter:\n  axis: diagonal\n  children:\n    - panels: [viewport]\n",
		"bad active":      "version: 1\ncenter:\n  panels: [viewport]\n  active: 2\n",
		"dup panel":       "version: 1\ncenter:\n  panels: [viewport]\nleft:\n  size: 200\n  root:\n    panels: [viewport]\n",
		"bad sizes len":   "version: 1\ncenter:\n  axis: horizontal\n  sizes: [100]\n  children:\n    - panels: [viewport]\n    - panels: [inspector]\n",
		"negative size":   "version: 1\ncenter:\n  panels: [viewport]\nleft:\n  size: -10\n  root:\n    panels: [files]\n",
		"nan size":        "version: 1\ncenter:\n  panels: [viewport]\nleft:\n  size: .nan\n  root:\n    panels: [files]\n",
		"inf size":        "version: 1\ncenter:\n  panels: [viewport]\nleft:\n  size: .inf\n  root:\n    panels: [files]\n",
		"yaml anchor":     "version: 1\ncenter: &c\n  panels: [viewport]\n",
		"yaml alias":      "version: 1\ncenter: &c\n  panels: [viewport]\nleft:\n  size: 200\n  root: *c\n",
		"yaml merge key":  "version: 1\ncenter:\n  panels: [viewport]\nleft:\n  <<: {size: 200}\n  root:\n    panels: [files]\n",
		"hidden viewport": "version: 1\ncenter:\n  panels: [viewport]\nhidden:\n  - viewport\n",
		"split active":    "version: 1\ncenter:\n  axis: horizontal\n  active: 1\n  children:\n    - panels: [viewport]\n",
		"huge region":     "version: 1\ncenter:\n  panels: [viewport]\nleft:\n  size: 100001\n  root:\n    panels: [files]\n",
		"huge split size": "version: 1\ncenter:\n  axis: horizontal\n  sizes: [0, 100001]\n  children:\n    - panels: [viewport]\n    - panels: [inspector]\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := editor.ParseLayout([]byte(body)); err == nil {
				t.Fatalf("ParseLayout accepted invalid document %q", name)
			}
		})
	}

	t.Run("excessive depth", func(t *testing.T) {
		var b strings.Builder
		b.WriteString("version: 1\ncenter:\n")
		for i := 0; i < 70; i++ {
			indent := strings.Repeat("  ", i+1)
			b.WriteString(indent + "axis: horizontal\n")
			b.WriteString(indent + "children:\n")
			b.WriteString(indent + "  -\n")
		}
		b.WriteString(strings.Repeat("  ", 71) + "panels: [viewport]\n")
		if _, err := editor.ParseLayout([]byte(b.String())); err == nil {
			t.Fatal("ParseLayout accepted split tree deeper than maxLayoutDepth")
		}
	})

	t.Run("oversized document", func(t *testing.T) {
		huge := []byte("version: 1\ncenter:\n  panels: [viewport]\n" + strings.Repeat(" ", 1<<20+1))
		if _, err := editor.ParseLayout(huge); err == nil {
			t.Fatal("ParseLayout accepted oversized document")
		}
		dir := t.TempDir()
		p := filepath.Join(dir, "huge.yaml")
		if err := os.WriteFile(p, huge, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := editor.LoadLayoutFile(p); err == nil {
			t.Fatal("LoadLayoutFile accepted oversized file")
		}
		if _, err := editor.LoadLayoutFile(dir); err == nil {
			t.Fatal("LoadLayoutFile accepted directory path")
		}
	})
}

func TestSaveLayoutFileRefusesEmptyPath(t *testing.T) {
	f := editor.DefaultLayoutFile()
	for _, p := range []string{"", "   ", "\t\n"} {
		if err := editor.SaveLayoutFile(p, f); err == nil {
			t.Fatalf("SaveLayoutFile(%q) succeeded, want error", p)
		}
	}
}

func TestSetDockLayoutSanitizesNonFiniteAndNegativeSizes(t *testing.T) {
	layoutFile := filepath.Join(t.TempDir(), "layout.yaml")
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.SetLayoutPath(layoutFile); err != nil {
		t.Fatalf("SetLayoutPath: %v", err)
	}

	dirty := dock.Layout{
		Center: dock.Node{
			Axis:     base.Horizontal,
			Children: []dock.Node{dock.Tabs(editor.PanelViewport), dock.Tabs(editor.PanelInspector)},
			Sizes:    []core.Points{core.Points(math.NaN()), core.Points(math.Inf(1))},
		},
		Left: &dock.Region{
			Size: core.Points(-42),
			Root: dock.Tabs(editor.PanelSceneTree),
		},
	}
	ed.SetDockLayout(dirty)

	saved, err := os.ReadFile(layoutFile)
	if err != nil {
		t.Fatalf("layout was not saved after SetDockLayout: %v", err)
	}
	parsed, err := editor.ParseLayout(saved)
	if err != nil {
		t.Fatalf("ParseLayout rejected saved layout: %v\n%s", err, saved)
	}
	if parsed.Left == nil || parsed.Left.Size <= 0 {
		t.Fatalf("expected positive sanitized left dock size, got %+v", parsed.Left)
	}
}

func TestDockAreaRendersSceneTabsAndContextualPanels(t *testing.T) {
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.NewMaterial(); err != nil {
		t.Fatalf("NewMaterial: %v", err)
	}
	if err := ed.NewParticleEmitter(); err != nil {
		t.Fatalf("NewParticleEmitter: %v", err)
	}
	if err := ed.NewRig("hero", ""); err != nil {
		t.Fatalf("NewRig: %v", err)
	}
	if err := ed.NewTerrain(editor.NewTerrainOptions{Name: "hills", Cols: 9, Rows: 9, CellSize: 1}); err != nil {
		t.Fatalf("NewTerrain: %v", err)
	}
	ed.SetEnvironmentPanelOpen(true)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))
	app.Settle()

	if !app.Exists("id:scene-tabs") {
		t.Fatal("SceneTabsView (id:scene-tabs) missing inside docked PanelViewport")
	}

	checks := []struct {
		panelKey string
		testID   string
	}{
		{editor.PanelMaterial, "id:material-panel"},
		{editor.PanelParticles, "id:particles-panel"},
		{editor.PanelRig, "id:rig-panel"},
		{editor.PanelTerrain, "id:terrain-panel"},
		{editor.PanelEnvironment, "id:environment-panel"},
	}
	for _, tc := range checks {
		ed.SetPanelVisible(tc.panelKey, true)
		app.Settle()
		if !ed.PanelVisible(tc.panelKey) {
			t.Fatalf("panel %q not visible after SetPanelVisible(true)", tc.panelKey)
		}
		if !app.Exists(tc.testID) {
			t.Fatalf("panel %q view (%s) not mounted in EditorView", tc.panelKey, tc.testID)
		}
	}
	for _, tc := range checks {
		ed.SetPanelVisible(tc.panelKey, false)
		app.Settle()
		if ed.PanelVisible(tc.panelKey) {
			t.Fatalf("panel %q still visible after SetPanelVisible(false)", tc.panelKey)
		}
	}
}

func TestDockLocalizedResetLayoutAndPluginPanelTitle(t *testing.T) {
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.RegisterPlugin(editor.Plugin{
		Name: "locpanel",
		LocaleYAML: [][]byte{
			[]byte("version: 1\nlocale: en\nstrings:\n  panel.title: Health Panel\n"),
			[]byte("version: 1\nlocale: zh\nstrings:\n  panel.title: 生命面板\n"),
		},
		Panels: []editor.Panel{{
			ID:       "main",
			Title:    "Health Panel",
			TitleKey: "panel.title",
			Dock:     editor.DockRight,
			View: func(cx *core.Scope, ed *editor.Editor, rev *core.Signal[uint64]) core.Node {
				return core.Text{Content: "ok"}
			},
		}},
	}); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	root := writeEditorProject(t)
	if err := ed.OpenProject(root); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if err := ed.SetPluginEnabled("locpanel", true); err != nil {
		t.Fatalf("SetPluginEnabled: %v", err)
	}
	if err := ed.SetLanguage(editor.LanguageChinese); err != nil {
		t.Fatalf("SetLanguage(zh): %v", err)
	}

	foundPlugin := false
	for _, info := range ed.Panels() {
		if info.Key == editor.PluginPanelKey("locpanel", "main") {
			foundPlugin = true
			if info.Title != "生命面板" {
				t.Fatalf("plugin panel title in zh = %q, want %q", info.Title, "生命面板")
			}
		}
	}
	if !foundPlugin {
		t.Fatal("active plugin panel missing from Panels()")
	}

	foundCmd := false
	for _, c := range ed.Commands() {
		if c.ID == editor.CmdResetLayout {
			foundCmd = true
			if c.Label != "重置布局" {
				t.Fatalf("CmdResetLayout label in zh = %q, want %q", c.Label, "重置布局")
			}
		}
	}
	if !foundCmd {
		t.Fatal("CmdResetLayout missing from Commands()")
	}
}

func TestDockLayoutHardeningMoveViewportDuplicatesUnavailableAndReveal(t *testing.T) {
	dir := writeEditorProject(t)
	layoutFile := filepath.Join(t.TempDir(), "layout.yaml")

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.SetLayoutPath(layoutFile); err != nil {
		t.Fatalf("SetLayoutPath: %v", err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	// MovePanel must refuse moving viewport or an empty targetPanel (never duplicating viewport).
	if ed.MovePanel(editor.PanelViewport, editor.PanelFiles) {
		t.Fatal("MovePanel allowed moving viewport out of Center")
	}
	if ed.MovePanel(editor.PanelInspector, "") {
		t.Fatal("MovePanel allowed empty targetPanel")
	}
	if err := editor.ValidateLayout(ed.LayoutFile()); err != nil {
		t.Fatalf("layout invalid after refused MovePanel: %v", err)
	}

	// SetDockLayout must deduplicate duplicate panel keys and clamp oversized sizes so SaveLayout succeeds.
	ed.SetDockLayout(dock.Layout{
		Center: dock.Node{
			Axis:     base.Horizontal,
			Children: []dock.Node{dock.Tabs(editor.PanelViewport, editor.PanelViewport), dock.Tabs(editor.PanelInspector)},
			Sizes:    []core.Points{0, 9999999},
		},
		Left: &dock.Region{
			Size: 9999999,
			Root: dock.Tabs(editor.PanelSceneTree, editor.PanelInspector),
		},
	})
	raw, err := os.ReadFile(layoutFile)
	if err != nil {
		t.Fatalf("saved layout missing after SetDockLayout: %v", err)
	}
	parsed, err := editor.ParseLayout(raw)
	if err != nil {
		t.Fatalf("ParseLayout rejected sanitized layout: %v\n%s", err, raw)
	}
	if parsed.Left == nil || parsed.Left.Size > 100000 {
		t.Fatalf("left size was not clamped: %+v", parsed.Left)
	}

	// Requesting an unavailable contextual panel (animation with no open clip) must not inject it into LayoutFile.
	ed.SetPanelVisible(editor.PanelAnimation, true)
	if ed.PanelVisible(editor.PanelAnimation) {
		t.Fatal("animation panel should remain hidden when no clip is open")
	}
	if data, err := editor.EncodeLayout(ed.LayoutFile()); err != nil {
		t.Fatalf("EncodeLayout: %v", err)
	} else if strings.Contains(string(data), editor.PanelAnimation) {
		t.Fatalf("unavailable animation panel leaked into persisted layout:\n%s", data)
	}

	// Hiding Inspector and invoking History / Tasks / Inspector commands reveals Inspector cleanly.
	ed.SetPanelVisible(editor.PanelInspector, false)
	if ed.PanelVisible(editor.PanelInspector) {
		t.Fatal("expected inspector hidden")
	}
	if err := ed.ExecuteCommand(editor.CmdHistoryPanel); err != nil {
		t.Fatalf("CmdHistoryPanel: %v", err)
	}
	if !ed.PanelVisible(editor.PanelInspector) {
		t.Fatal("CmdHistoryPanel did not reveal hidden inspector panel")
	}
	ed.ToggleHistoryPanel()

	ed.SetPanelVisible(editor.PanelInspector, false)
	if err := ed.ExecuteCommand(editor.CmdToggleTasks); err != nil {
		t.Fatalf("CmdToggleTasks: %v", err)
	}
	if !ed.PanelVisible(editor.PanelInspector) {
		t.Fatal("CmdToggleTasks did not reveal hidden inspector panel")
	}
	ed.ToggleTasksPanel()

	ed.SetPanelVisible(editor.PanelInspector, false)
	if err := ed.ExecuteCommand(editor.CmdInspectorTab); err != nil {
		t.Fatalf("CmdInspectorTab: %v", err)
	}
	if !ed.PanelVisible(editor.PanelInspector) {
		t.Fatal("CmdInspectorTab did not reveal hidden inspector panel")
	}
}

func TestDockLiveLanguageSwitchAndSpecThemeParity(t *testing.T) {
	dir := writeEditorProject(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.RegisterPlugin(editor.Plugin{
		Name: "loclive",
		LocaleYAML: [][]byte{
			[]byte("version: 1\nlocale: en\nstrings:\n  panel.title: Live Panel\n"),
			[]byte("version: 1\nlocale: zh\nstrings:\n  panel.title: 实时面板\n"),
		},
		Panels: []editor.Panel{{
			ID:       "main",
			Title:    "Live Panel",
			TitleKey: "panel.title",
			Dock:     editor.DockRight,
			View: func(cx *core.Scope, ed *editor.Editor, rev *core.Signal[uint64]) core.Node {
				return core.Text{Content: "body"}
			},
		}},
	}); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if err := ed.SetPluginEnabled("loclive", true); err != nil {
		t.Fatalf("SetPluginEnabled: %v", err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 760), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	tabSel := "id:dock-tab-" + editor.PluginPanelKey("loclive", "main")
	if !app.Exists(tabSel) {
		t.Fatal("plugin dock tab did not mount")
	}
	if !app.Exists("text:Live Panel") {
		t.Fatal("expected English plugin tab title initially")
	}

	app.Post(func() {
		if err := ed.SetLanguage(editor.LanguageChinese); err != nil {
			t.Errorf("SetLanguage(zh): %v", err)
		}
		if err := ed.SetAppearance(editor.AppearanceLight); err != nil {
			t.Errorf("SetAppearance(light): %v", err)
		}
	})
	app.Settle()

	if !app.Exists("text:实时面板") {
		t.Fatal("plugin dock tab title did not update live to Chinese")
	}
	if !app.Exists("id:editor-dock-area") {
		t.Fatal("dock area missing in Spec Light appearance")
	}
}
