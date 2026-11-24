package healthplugin_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/examples/fieldmedic/health"
	"github.com/nsgo-dev/game/examples/fieldmedic/healthplugin"
	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

// copyProject copies Field Medic's game.yaml, nsgo.yaml and scenes into a temporary directory, so a test may save them, and returns it.
func copyProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"game.yaml", "nsgo.yaml", "scenes/ward.scene.yaml"} {
		data, err := os.ReadFile(filepath.Join("..", filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// newEditor returns an editor with the health component and plugin on Field Medic's project, copied into a temporary directory.
func newEditor(t *testing.T) *editor.Editor {
	t.Helper()
	ed, err := editor.New(nil, sceneformat.AtlasMap{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.SetComponentRegistry(health.Components()); err != nil {
		t.Fatal(err)
	}
	if err := ed.RegisterPlugin(healthplugin.Plugin()); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(copyProject(t)); err != nil {
		t.Fatal(err)
	}
	return ed
}

func hp(ed *editor.Editor, id string) int {
	v, _ := ed.ComponentField(id, health.Name, health.FieldHP)
	n, _ := v.(int)
	return n
}

// fieldText returns the first text in v or the views under it: a text field's value.
func fieldText(v *nsgotest.View) string {
	if v.Text != "" {
		return v.Text
	}
	for _, c := range v.Children {
		if s := fieldText(c); s != "" {
			return s
		}
	}
	return ""
}

func TestPluginAddsTheWardPanelTheInspectorAndTheAdmitTool(t *testing.T) {
	ed := newEditor(t)
	if got := ed.ActivePlugins(); !reflect.DeepEqual(got, []string{healthplugin.Name}) {
		t.Fatalf("ActivePlugins = %v, want [health]", got)
	}
	if !ed.SelectObject("bram") {
		t.Fatal("no bram in the ward")
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))

	// The Ward panel docks right of the inspector and lists every patient.
	if !app.Exists("id:plugin-panel-health-ward") {
		t.Fatal("the Ward panel is not docked")
	}
	if got, want := app.Find("id:health-ward").Text, "ada 8/10\nbram 5/12\ncleo 9/10\ndov 4/8"; got != want {
		t.Fatalf("Ward panel = %q, want %q", got, want)
	}

	// The plugin's inspector draws the health component instead of the generic fields.
	if !app.Exists("id:inspector-plugin-health-health") || app.Exists("id:inspector-comp-health-hp") {
		t.Fatal("the health component is not drawn by the plugin's inspector alone")
	}
	if got := fieldText(app.Find("id:health-hp")); got != "5" {
		t.Fatalf("hp field = %q, want 5", got)
	}
	if got := fieldText(app.Find("id:health-max")); got != "12" {
		t.Fatalf("max field = %q, want 12", got)
	}
	if got := app.Find("id:health-bar").Text; got != "█████░░░░░ 5 of 12" {
		t.Fatalf("bar = %q", got)
	}
	app.Clear("id:health-hp")
	app.Type("id:health-hp", "9")
	app.Submit("id:health-hp")
	app.Settle()
	if got := hp(ed, "bram"); got != 9 {
		t.Fatalf("bram after typing = %d hp, want 9", got)
	}
	if got := app.Find("id:health-ward").Text; !strings.Contains(got, "bram 9/12") {
		t.Fatalf("Ward panel after the edit = %q", got)
	}

	// Heal All heals every patient in one step that one Undo takes back.
	undos := ed.History.Index()
	app.Press("id:health-heal-all")
	if got, want := app.Find("id:health-ward").Text, "ada 10/10\nbram 12/12\ncleo 10/10\ndov 8/8"; got != want {
		t.Fatalf("Ward panel after Heal All = %q, want %q", got, want)
	}
	if ed.History.Index() != undos+1 || ed.History.UndoLabel() != "Heal All" {
		t.Fatalf("Heal All added %d steps, labeled %q", ed.History.Index()-undos, ed.History.UndoLabel())
	}
	app.Press("id:toolbar-undo")
	if got, want := app.Find("id:health-ward").Text, "ada 8/10\nbram 9/12\ncleo 9/10\ndov 4/8"; got != want {
		t.Fatalf("Ward panel after Undo = %q, want %q", got, want)
	}

	// The Admit tool places a patient where the viewport is pressed, on the ward's layer.
	app.Press("id:tool-health-admit")
	if plugin, id, ok := ed.PluginTool(); !ok || plugin != healthplugin.Name || id != "admit" {
		t.Fatalf("PluginTool = %q %q %v", plugin, id, ok)
	}
	app.PressAt("id:editor-viewport", core.Point{X: 120, Y: 80})
	ids := health.Objects(ed.Doc)
	if len(ids) != 5 {
		t.Fatalf("patients after Admit = %v", ids)
	}
	admitted, _, _ := ed.FindObject(ids[4])
	if wx, wy := ed.CanvasToWorld(120, 80); admitted.X != wx || admitted.Y != wy || hp(ed, admitted.ID) != healthplugin.AdmitHP {
		t.Fatalf("admitted %+v, want at (%v, %v) with %d hp", admitted, wx, wy, healthplugin.AdmitHP)
	}
	app.Press("id:toolbar-undo")
	if got := health.Objects(ed.Doc); len(got) != 4 {
		t.Fatalf("Undo left the admitted patient: %v", got)
	}
}

func TestHealAllKeepsUnsetFieldsUnsetOnUndoAndSkipsHealthyPatients(t *testing.T) {
	ed := newEditor(t)
	before, err := ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}
	if err := healthplugin.HealAll(ed); err != nil {
		t.Fatal(err)
	}
	if hp(ed, "ada") != 10 || hp(ed, "dov") != 8 {
		t.Fatalf("HealAll left ada %d, dov %d", hp(ed, "ada"), hp(ed, "dov"))
	}
	steps := ed.History.Index()
	if err := healthplugin.HealAll(ed); err != nil || ed.History.Index() != steps {
		t.Fatalf("HealAll of a healed ward = %v and %d new steps", err, ed.History.Index()-steps)
	}
	ed.Undo()
	after, err := ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("Undo of Heal All changed the scene:\n%s\nwant:\n%s", after, before)
	}
	ed.Redo()
	if hp(ed, "bram") != 12 {
		t.Fatalf("Redo left bram at %d", hp(ed, "bram"))
	}
}

func TestHealAllIsRefusedWhilePlaying(t *testing.T) {
	ed := newEditor(t)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))
	app.Press("id:toolbar-play")
	if !ed.Playing() {
		t.Fatal("Play did not start")
	}
	if !app.Find("id:health-heal-all").Props.Disabled {
		t.Fatal("Heal All is enabled while playing")
	}
	if err := healthplugin.HealAll(ed); err != editor.ErrPlaying {
		t.Fatalf("HealAll while playing = %v, want ErrPlaying", err)
	}
	app.Press("id:toolbar-play")
}

func TestBar(t *testing.T) {
	for _, c := range []struct {
		s    health.Stats
		want string
	}{
		{health.Stats{HP: 10, Max: 10}, "██████████"},
		{health.Stats{HP: 1, Max: 10}, "█░░░░░░░░░"},
		{health.Stats{HP: 1, Max: 99}, "█░░░░░░░░░"},
		{health.Stats{HP: 0, Max: 10}, "░░░░░░░░░░"},
		{health.Stats{HP: 5, Max: 0}, "░░░░░░░░░░"},
	} {
		if got := healthplugin.Bar(c.s); got != c.want {
			t.Errorf("Bar(%+v) = %q, want %q", c.s, got, c.want)
		}
	}
}

func TestSummaryOfAWardWithoutPatients(t *testing.T) {
	ed, err := editor.New(&sceneformat.SceneDoc{Version: sceneformat.CurrentVersion, Layers: []sceneformat.LayerDoc{{Name: "a"}}}, sceneformat.AtlasMap{})
	if err != nil {
		t.Fatal(err)
	}
	if got := healthplugin.Summary(ed); got != "No patients" {
		t.Fatalf("Summary = %q", got)
	}
}
