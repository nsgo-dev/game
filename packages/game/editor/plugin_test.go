package editor_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
	"github.com/nsgo-dev/nsgo/ui"
)

// healthComponents registers the sample plugin's "health" component: hit points and their maximum.
func healthComponents(t *testing.T) *sceneformat.ComponentRegistry {
	t.Helper()
	reg := physicsComponents(t)
	if err := reg.Register(sceneformat.ComponentType{
		Name: "health",
		Doc:  "Hit points an object loses when it is hurt.",
		Fields: []sceneformat.Field{
			{Name: "hp", Kind: sceneformat.FieldInt, Default: 10, Min: sceneformat.Float64(0), Max: sceneformat.Float64(999)},
			{Name: "max", Kind: sceneformat.FieldInt, Default: 10, Min: sceneformat.Float64(1), Max: sceneformat.Float64(999)},
		},
	}); err != nil {
		t.Fatalf("Register health: %v", err)
	}
	return reg
}

// intField reads an int component field through ComponentField, so an unset field reads its registered default.
func intField(ed *editor.Editor, id, field string) int {
	v, _ := ed.ComponentField(id, "health", field)
	n, _ := v.(int)
	return n
}

// healthPlugin is the sample plugin: a panel listing every object's hit points with a button that heals them all, an inspector for the health component, and a tool that places a wounded object where it is pressed. Each time the tool draws over the viewport it counts the draw in draws, when draws is not nil.
func healthPlugin(draws *int) editor.Plugin {
	return editor.Plugin{
		Name: "health",
		Panels: []editor.Panel{{
			ID:    "overview",
			Title: "Health",
			Dock:  editor.DockRight,
			View: func(cx *core.Scope, ed *editor.Editor, rev *core.Signal[uint64]) core.Node {
				return core.View{
					Style: core.Style{Gap: 6},
					Children: []core.Node{
						core.Text{
							TestID: "health-overview",
							ContentFunc: func() string {
								_ = rev.Get()
								var rows []string
								for _, id := range healthObjects(ed) {
									rows = append(rows, fmt.Sprintf("%s %d/%d", id, intField(ed, id, "hp"), intField(ed, id, "max")))
								}
								return strings.Join(rows, "; ")
							},
						},
						ui.Button{
							TestID: "health-heal-all",
							Text:   "Heal All",
							Disabled: func() bool {
								_ = rev.Get()
								return ed.Playing()
							},
							OnPress: func() {
								for _, id := range healthObjects(ed) {
									_ = ed.SetComponentField(id, "health", "hp", intField(ed, id, "max"))
								}
							},
						},
					},
				}
			},
		}},
		Inspectors: []editor.Inspector{{
			Component: "health",
			View: func(cx *core.Scope, ed *editor.Editor, rev *core.Signal[uint64], id string) core.Node {
				return core.View{
					Style: core.Style{Gap: 6},
					Children: []core.Node{
						ui.NumberInput{
							TestID: "health-hp",
							Label:  "Hit points",
							Step:   1,
							Value: func() float64 {
								_ = rev.Get()
								return float64(intField(ed, id, "hp"))
							},
							OnChange: func(v float64) {
								_ = ed.SetComponentField(id, "health", "hp", int(v))
							},
						},
						core.Text{
							TestID: "health-bar",
							ContentFunc: func() string {
								_ = rev.Get()
								return strconv.Itoa(intField(ed, id, "hp")) + " of " + strconv.Itoa(intField(ed, id, "max"))
							},
						},
					},
				}
			},
		}},
		Tools: []editor.PluginTool{{
			ID:    "spawn",
			Label: "Spawn",
			Press: func(ed *editor.Editor, x, y float32) {
				wx, wy := ed.CanvasToWorld(x, y)
				_, _ = ed.PlaceObject(ed.ActiveLayer, sceneformat.ObjectDoc{
					X: wx, Y: wy,
					Components: map[string]any{"health": map[string]any{"hp": 5}},
				})
			},
			Draw: func(ed *editor.Editor, d *core.Drawing) {
				if draws != nil {
					*draws++
				}
				d.SetStrokeColor(core.RGBA(0xEF4444, 1)).StrokeRect(0, 0, 8, 8)
			},
		}},
	}
}

// allText joins the texts of v and the views under it.
func allText(v *nsgotest.View) string {
	parts := []string{v.Text}
	for _, c := range v.Children {
		parts = append(parts, allText(c))
	}
	return strings.Join(parts, " ")
}

// healthObjects returns the IDs of the objects with a health component, in scene order.
func healthObjects(ed *editor.Editor) []string {
	var ids []string
	for _, layer := range ed.Doc.Layers {
		for _, o := range layer.Objects {
			if _, ok := o.Components["health"]; ok {
				ids = append(ids, o.ID)
			}
		}
	}
	return ids
}

// writePluginProject writes the project writeEditorProject writes with plugins listed under editor.plugins, and a health component on the hero, and returns its directory.
func writePluginProject(t *testing.T, plugins ...string) string {
	t.Helper()
	dir := writeEditorProject(t)
	doc := testSceneDoc()
	doc.Layers[1].Objects[0].Components["health"] = map[string]any{"hp": 3, "max": 12}
	scene, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scenes", "room.yaml"), scene, 0o644); err != nil {
		t.Fatal(err)
	}
	data := projectYAML
	if len(plugins) > 0 {
		data += "editor:\n  plugins: [" + strings.Join(plugins, ", ") + "]\n"
	}
	if err := os.WriteFile(filepath.Join(dir, project.FileName), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// newPluginEditor returns an editor with the health component registered and the sample plugin registered, on the project in dir; the plugin's tool counts its draws in draws when it is not nil.
func newPluginEditor(t *testing.T, dir string, draws ...*int) *editor.Editor {
	t.Helper()
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.SetComponentRegistry(healthComponents(t)); err != nil {
		t.Fatal(err)
	}
	var counter *int
	if len(draws) > 0 {
		counter = draws[0]
	}
	if err := ed.RegisterPlugin(healthPlugin(counter)); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.SetActiveLayer("actors")
	return ed
}

func TestSamplePluginAddsAPanelAnInspectorAndATool(t *testing.T) {
	var draws int
	ed := newPluginEditor(t, writePluginProject(t, "health"), &draws)
	if got := ed.ActivePlugins(); !reflect.DeepEqual(got, []string{"health"}) {
		t.Fatalf("ActivePlugins = %v, want [health]", got)
	}
	ed.SelectObject("hero")
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))

	// The panel docks right of the inspector and lists the hero's hit points.
	if !app.Exists("id:plugin-dock-right") || !app.Exists("id:plugin-panel-health-overview") {
		t.Fatal("the health panel is not docked")
	}
	if app.Exists("id:plugin-dock-left") || app.Exists("id:plugin-dock-bottom") {
		t.Fatal("a dock without panels is shown")
	}
	if got := app.Find("id:health-overview").Text; got != "hero 3/12" {
		t.Fatalf("overview = %q, want hero 3/12", got)
	}

	// The inspector draws the health component with the plugin's controls instead of the generic fields, and keeps the generic fields of the physics component.
	if !app.Exists("id:inspector-plugin-health-health") {
		t.Fatal("the plugin's inspector does not draw the health component")
	}
	if app.Exists("id:inspector-comp-health-hp") {
		t.Fatal("the generic hp field is shown beside the plugin's inspector")
	}
	if !app.Exists("id:inspector-comp-physics-solid") {
		t.Fatal("the physics component lost its generic fields")
	}
	if got := fieldText(app.Find("id:health-hp")); got != "3" {
		t.Fatalf("hp field shows %q, want 3", got)
	}
	app.Clear("id:health-hp")
	app.Type("id:health-hp", "7")
	app.Submit("id:health-hp")
	app.Settle()
	if got := intField(ed, "hero", "hp"); got != 7 {
		t.Fatalf("hp after typing = %d, want 7", got)
	}
	if got := app.Find("id:health-bar").Text; got != "7 of 12" {
		t.Fatalf("bar = %q, want 7 of 12", got)
	}
	if got := app.Find("id:health-overview").Text; got != "hero 7/12" {
		t.Fatalf("overview after the edit = %q, want hero 7/12", got)
	}

	// The panel's button edits through the editor, so Undo takes it back.
	app.Press("id:health-heal-all")
	if got := intField(ed, "hero", "hp"); got != 12 {
		t.Fatalf("hp after Heal All = %d, want 12", got)
	}
	app.Press("id:toolbar-undo")
	if got := intField(ed, "hero", "hp"); got != 7 {
		t.Fatalf("hp after Undo = %d, want 7", got)
	}
	if got := fieldText(app.Find("id:health-hp")); got != "7" {
		t.Fatalf("hp field after Undo shows %q, want 7", got)
	}

	// The plugin's tool sits in the tool bar, places an object where the viewport is pressed and names itself in the status bar.
	if draws != 0 {
		t.Fatalf("the tool drew %d times before it was chosen", draws)
	}
	app.Press("id:tool-health-spawn")
	if plugin, id, ok := ed.PluginTool(); !ok || plugin != "health" || id != "spawn" || ed.Tool != editor.ToolPlugin {
		t.Fatalf("PluginTool = %q %q %v, Tool %v", plugin, id, ok, ed.Tool)
	}
	if got := allText(app.Find("id:editor-status")); !strings.Contains(got, "Tool: Spawn") {
		t.Fatalf("status = %q, want it to name the Spawn tool", got)
	}
	if draws == 0 {
		t.Fatal("the active tool did not draw over the viewport")
	}
	before := len(healthObjects(ed))
	app.PressAt("id:editor-viewport", core.Point{X: 100, Y: 60})
	ids := healthObjects(ed)
	if len(ids) != before+1 {
		t.Fatalf("health objects after a press = %v", ids)
	}
	spawned, _, _ := ed.FindObject(ids[len(ids)-1])
	if wx, wy := ed.CanvasToWorld(100, 60); spawned.X != wx || spawned.Y != wy || intField(ed, spawned.ID, "hp") != 5 {
		t.Fatalf("spawned %+v, want at (%v, %v) with 5 hp", spawned, wx, wy)
	}
	app.Press("id:toolbar-undo")
	if got := healthObjects(ed); len(got) != before {
		t.Fatalf("Undo left the spawned object: %v", got)
	}

	// Choosing a built-in tool ends the plugin's tool.
	app.Press("id:tool-select")
	if _, _, ok := ed.PluginTool(); ok || ed.Tool != editor.ToolSelect {
		t.Fatalf("after Select the tool is %v", ed.Tool)
	}
}

func TestPluginsFollowTheProjectsEditorPlugins(t *testing.T) {
	dir := writePluginProject(t)
	ed := newPluginEditor(t, dir)
	if got := ed.ActivePlugins(); got != nil {
		t.Fatalf("ActivePlugins of a project that lists none = %v", got)
	}
	ed.SelectObject("hero")
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))

	// A registered plugin the project does not name stays off: no panel, no tool, the generic fields.
	if app.Exists("id:plugin-panel-health-overview") || app.Exists("id:tool-health-spawn") || app.Exists("id:inspector-plugin-health-health") {
		t.Fatal("a plugin the project does not name is on")
	}
	if !app.Exists("id:inspector-comp-health-hp") {
		t.Fatal("the health component lacks its generic fields while the plugin is off")
	}

	// Turning it on edits game.yaml as one undoable step and brings in its panel, tool and inspector.
	app.Post(func() {
		if err := ed.SetPluginEnabled("health", true); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	if !ed.PluginEnabled("health") || ed.Project.Editor == nil || !reflect.DeepEqual(ed.Project.Editor.Plugins, []string{"health"}) {
		t.Fatalf("after turning it on: enabled %v, project editor %+v", ed.PluginEnabled("health"), ed.Project.Editor)
	}
	if !app.Exists("id:plugin-panel-health-overview") || !app.Exists("id:tool-health-spawn") || !app.Exists("id:inspector-plugin-health-health") {
		t.Fatal("turning the plugin on did not bring in its panel, tool and inspector")
	}
	if got := ed.History.UndoLabel(); got != "Turn on plugin health" {
		t.Fatalf("UndoLabel = %q", got)
	}
	app.Press("id:tool-health-spawn")

	// Undo turns it off again, ends its tool and restores the generic fields.
	app.Press("id:toolbar-undo")
	if ed.PluginEnabled("health") || ed.Project.Editor != nil {
		t.Fatalf("after Undo: enabled %v, project editor %+v", ed.PluginEnabled("health"), ed.Project.Editor)
	}
	if ed.Tool != editor.ToolSelect {
		t.Fatalf("Tool after its plugin turned off = %v, want ToolSelect", ed.Tool)
	}
	if app.Exists("id:plugin-panel-health-overview") || app.Exists("id:tool-health-spawn") || !app.Exists("id:inspector-comp-health-hp") {
		t.Fatal("Undo left the plugin's views in place")
	}
	if ed.ProjectDirty() {
		t.Fatal("turning a plugin on and Undo left the project dirty")
	}

	// Save writes the plugin into game.yaml, where the next OpenProject finds it.
	app.Press("id:toolbar-redo")
	app.Press("id:toolbar-save")
	f, err := project.Check(os.DirFS(dir))
	if err != nil || f.Editor == nil || !reflect.DeepEqual(f.Editor.Plugins, []string{"health"}) {
		t.Fatalf("saved project: %+v, %v", f, err)
	}
	reopened := newPluginEditor(t, dir)
	if !reopened.PluginEnabled("health") {
		t.Fatal("the saved plugin is off in a reopened project")
	}

	// Turning it off removes the editor section it added, so game.yaml reads as before.
	if err := reopened.SetPluginEnabled("health", false); err != nil {
		t.Fatal(err)
	}
	if reopened.Project.Editor != nil {
		t.Fatalf("editor section after turning the last plugin off = %+v", reopened.Project.Editor)
	}
}

func TestOpenProjectNamesPluginsTheEditorLacks(t *testing.T) {
	ed := newPluginEditor(t, writePluginProject(t, "terrain", "health", "paths"))
	if got := ed.MissingPlugins(); !reflect.DeepEqual(got, []string{"terrain", "paths"}) {
		t.Fatalf("MissingPlugins = %v", got)
	}
	if got := ed.ActivePlugins(); !reflect.DeepEqual(got, []string{"health"}) {
		t.Fatalf("ActivePlugins = %v", got)
	}
	if !strings.Contains(ed.Status, "plugins not built into this editor: terrain, paths") {
		t.Fatalf("status = %q", ed.Status)
	}
	if err := ed.SetPluginEnabled("terrain", true); err == nil {
		t.Fatal("turning on a plugin that is not registered succeeded")
	}
	// A plugin the editor lacks can still be turned off, which drops it from game.yaml.
	if err := ed.SetPluginEnabled("terrain", false); err != nil {
		t.Fatal(err)
	}
	if got := ed.Project.Editor.Plugins; !reflect.DeepEqual(got, []string{"health", "paths"}) {
		t.Fatalf("plugins after turning terrain off = %v", got)
	}
}

func TestPluginsWithoutAProject(t *testing.T) {
	ed := newTestEditor(t)
	if err := ed.RegisterPlugin(healthPlugin(nil)); err != nil {
		t.Fatal(err)
	}
	if got := ed.RegisteredPlugins(); !reflect.DeepEqual(got, []string{"health"}) {
		t.Fatalf("RegisteredPlugins = %v", got)
	}
	if ed.PluginEnabled("health") || ed.ActivePlugins() != nil {
		t.Fatal("a plugin is on without a project")
	}
	if err := ed.SetPluginEnabled("health", true); !errors.Is(err, editor.ErrNoProject) {
		t.Fatalf("SetPluginEnabled without a project = %v, want ErrNoProject", err)
	}
	if err := ed.SetPluginTool("health", "spawn"); err == nil {
		t.Fatal("SetPluginTool of a plugin that is off succeeded")
	}
	ed.SetTool(editor.ToolPlugin)
	if ed.Tool != editor.ToolSelect {
		t.Fatalf("SetTool(ToolPlugin) changed the tool to %v", ed.Tool)
	}
}

func TestSetPluginToolRefusesUnknownTools(t *testing.T) {
	ed := newPluginEditor(t, writePluginProject(t, "health"))
	if err := ed.SetPluginTool("health", "erase"); err == nil {
		t.Fatal("SetPluginTool of a tool the plugin lacks succeeded")
	}
	if err := ed.SetPluginTool("terrain", "spawn"); err == nil {
		t.Fatal("SetPluginTool of a plugin that is not registered succeeded")
	}
	if err := ed.SetPluginTool("health", "spawn"); err != nil {
		t.Fatal(err)
	}
	if got := ed.Tool.String(); got != "Plugin" {
		t.Fatalf("Tool.String() = %q", got)
	}
}

func TestPluginEditsAreRefusedWhilePlaying(t *testing.T) {
	ed := newPluginEditor(t, writePluginProject(t))
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))
	app.Press("id:toolbar-play")
	if !ed.Playing() {
		t.Fatal("Play did not start")
	}
	if err := ed.SetPluginEnabled("health", true); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("SetPluginEnabled while playing = %v, want ErrPlaying", err)
	}
	app.Press("id:toolbar-play")
}

func TestRegisterPluginRefusesInvalidPlugins(t *testing.T) {
	view := func(*core.Scope, *editor.Editor, *core.Signal[uint64]) core.Node { return nil }
	inspect := func(*core.Scope, *editor.Editor, *core.Signal[uint64], string) core.Node { return nil }
	press := func(*editor.Editor, float32, float32) {}
	cases := map[string]editor.Plugin{
		"no name":             {},
		"spaced name":         {Name: "level tools"},
		"control name":        {Name: "tools\x00"},
		"panel without ID":    {Name: "p", Panels: []editor.Panel{{Title: "T", View: view}}},
		"panel title":         {Name: "p", Panels: []editor.Panel{{ID: "a", View: view}}},
		"panel view":          {Name: "p", Panels: []editor.Panel{{ID: "a", Title: "T"}}},
		"panel dock":          {Name: "p", Panels: []editor.Panel{{ID: "a", Title: "T", View: view, Dock: editor.Dock(7)}}},
		"duplicate panel":     {Name: "p", Panels: []editor.Panel{{ID: "a", Title: "T", View: view}, {ID: "a", Title: "U", View: view}}},
		"inspector type":      {Name: "p", Inspectors: []editor.Inspector{{View: inspect}}},
		"inspector view":      {Name: "p", Inspectors: []editor.Inspector{{Component: "health"}}},
		"duplicate inspector": {Name: "p", Inspectors: []editor.Inspector{{Component: "health", View: inspect}, {Component: "health", View: inspect}}},
		"tool without ID":     {Name: "p", Tools: []editor.PluginTool{{Label: "T", Press: press}}},
		"tool label":          {Name: "p", Tools: []editor.PluginTool{{ID: "a", Press: press}}},
		"tool press":          {Name: "p", Tools: []editor.PluginTool{{ID: "a", Label: "T"}}},
		"duplicate tool":      {Name: "p", Tools: []editor.PluginTool{{ID: "a", Label: "T", Press: press}, {ID: "a", Label: "U", Press: press}}},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			ed := newTestEditor(t)
			if err := ed.RegisterPlugin(p); err == nil {
				t.Fatal("RegisterPlugin succeeded")
			}
			if got := ed.RegisteredPlugins(); len(got) != 0 {
				t.Fatalf("a refused plugin was registered: %v", got)
			}
		})
	}
	ed := newTestEditor(t)
	if err := ed.RegisterPlugin(healthPlugin(nil)); err != nil {
		t.Fatal(err)
	}
	if err := ed.RegisterPlugin(editor.Plugin{Name: "health"}); err == nil {
		t.Fatal("a second plugin named health was registered")
	}
}

func TestDockString(t *testing.T) {
	for d, want := range map[editor.Dock]string{editor.DockLeft: "left", editor.DockRight: "right", editor.DockBottom: "bottom", editor.Dock(9): "Dock(9)"} {
		if got := d.String(); got != want {
			t.Errorf("Dock(%d).String() = %q, want %q", int(d), got, want)
		}
	}
}

func TestPluginPanelsDockLeftAndBelow(t *testing.T) {
	text := func(id string) func(*core.Scope, *editor.Editor, *core.Signal[uint64]) core.Node {
		return func(*core.Scope, *editor.Editor, *core.Signal[uint64]) core.Node {
			return core.Text{TestID: id, Content: id}
		}
	}
	ed := newTestEditor(t)
	for _, p := range []editor.Plugin{
		{Name: "outline", Panels: []editor.Panel{{ID: "tree", Title: "Outline", Dock: editor.DockLeft, View: text("outline-tree")}}},
		{Name: "console", Panels: []editor.Panel{{ID: "log", Title: "Console", Dock: editor.DockBottom, View: text("console-log")}}},
	} {
		if err := ed.RegisterPlugin(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := ed.OpenProject(writeBareProject(t, "Bare")); err != nil {
		t.Fatal(err)
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))
	if app.Exists("id:plugin-dock-left") || app.Exists("id:plugin-dock-bottom") {
		t.Fatal("docks are shown before any plugin is on")
	}
	app.Post(func() {
		for _, name := range []string{"console", "outline"} {
			if err := ed.SetPluginEnabled(name, true); err != nil {
				t.Error(err)
			}
		}
	})
	app.Settle()
	if !app.Exists("id:plugin-dock-left") || !app.Exists("id:outline-tree") || !app.Exists("id:plugin-dock-bottom") || !app.Exists("id:console-log") {
		t.Fatal("the left and bottom panels are not docked")
	}
	if got := ed.ActivePlugins(); !reflect.DeepEqual(got, []string{"console", "outline"}) {
		t.Fatalf("ActivePlugins = %v, want the order they were turned on", got)
	}
	left, view := app.Find("id:plugin-dock-left").Bounds(), app.Find("id:editor-viewport").Bounds()
	if left.Origin.X+left.Size.Width > view.Origin.X {
		t.Fatalf("left dock %+v overlaps the viewport %+v", left, view)
	}
	bottom := app.Find("id:plugin-dock-bottom").Bounds()
	if bottom.Origin.Y < view.Origin.Y+view.Size.Height {
		t.Fatalf("bottom dock %+v is not below the viewport %+v", bottom, view)
	}
}
