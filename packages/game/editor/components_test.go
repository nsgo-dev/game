package editor_test

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// physicsComponents registers the "physics" component testSceneDoc uses, plus a "friction" field the hero leaves unset.
func physicsComponents(t *testing.T) *sceneformat.ComponentRegistry {
	t.Helper()
	reg := sceneformat.NewComponentRegistry()
	if err := reg.Register(sceneformat.ComponentType{
		Name: "physics",
		Doc:  "Rigid body settings.",
		Fields: []sceneformat.Field{
			{Name: "solid", Kind: sceneformat.FieldBool, Default: true, Doc: "Blocks other bodies."},
			{Name: "gravity", Kind: sceneformat.FieldFloat, Default: 9.8, Min: sceneformat.Float64(0), Max: sceneformat.Float64(100)},
			{Name: "friction", Kind: sceneformat.FieldFloat, Default: 0.2, Min: sceneformat.Float64(0), Max: sceneformat.Float64(1), Doc: "Ground friction."},
			{Name: "layer", Kind: sceneformat.FieldInt, Default: 0, Min: sceneformat.Float64(0), Max: sceneformat.Float64(31)},
			{Name: "kind", Kind: sceneformat.FieldString, Default: "static"},
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return reg
}

func TestInspectorListsRegisteredComponentFields(t *testing.T) {
	ed := newTestEditor(t)
	if err := ed.SetComponentRegistry(physicsComponents(t)); err != nil {
		t.Fatalf("SetComponentRegistry: %v", err)
	}
	ed.SelectObject("hero")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 1400))

	for _, field := range []string{"solid", "gravity", "friction", "layer", "kind"} {
		if got := app.Find("id:inspector-comp-physics-" + field + "-label").Text; got != field {
			t.Fatalf("label for %s = %q", field, got)
		}
	}
	// friction is not in the scene file; the inspector shows its registered default.
	if got := fieldText(app.Find("id:inspector-comp-physics-friction")); got != "0.2" {
		t.Fatalf("unset friction shows %q, want its default 0.2", got)
	}
	if got := app.Find("id:inspector-comp-physics-kind").Text; got != "dynamic" {
		t.Fatalf("kind shows %q, want dynamic", got)
	}

	// Editing the unset field through its control writes it into the scene.
	app.Clear("id:inspector-comp-physics-kind")
	app.Type("id:inspector-comp-physics-kind", "kinematic")
	app.Post(func() {
		if err := ed.SetComponentField("hero", "physics", "friction", 0.5); err != nil {
			t.Errorf("SetComponentField friction: %v", err)
		}
	})
	app.Settle()
	hero, _, _ := ed.FindObject("hero")
	phys := hero.Components["physics"].(map[string]any)
	if phys["kind"] != "kinematic" || phys["friction"] != 0.5 {
		t.Fatalf("physics after edits = %#v", phys)
	}
	if got := fieldText(app.Find("id:inspector-comp-physics-friction")); got != "0.5" {
		t.Fatalf("friction shows %q after the edit, want 0.5", got)
	}
}

func TestSetComponentFieldWithoutARegistryChecksTheBuiltInPhysicsComponents(t *testing.T) {
	ed := newTestEditor(t)
	if ed.ComponentRegistry() != nil {
		t.Fatal("the test editor has a component registry")
	}
	err := ed.SetComponentField("hero", "body2d", "sped", 3.0)
	var ce *sceneformat.ComponentError
	if !errors.As(err, &ce) || ce.Object != "hero" || ce.Component != "body2d" || ce.Field != "sped" {
		t.Fatalf("misspelled body2d field: err = %v", err)
	}
	if err := ed.SetComponentField("hero", "body2d", "mass", 2); err != nil {
		t.Fatalf("SetComponentField body2d mass: %v", err)
	}
	// A game's own component stays free-form.
	if err := ed.SetComponentField("hero", "health", "hp", 3); err != nil {
		t.Fatalf("SetComponentField health hp: %v", err)
	}
}

func TestSetComponentFieldChecksTheRegistry(t *testing.T) {
	ed := newTestEditor(t)
	if err := ed.SetComponentRegistry(physicsComponents(t)); err != nil {
		t.Fatalf("SetComponentRegistry: %v", err)
	}
	before, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}

	err = ed.SetComponentField("hero", "physics", "gravty", 1.0)
	var ce *sceneformat.ComponentError
	if !errors.As(err, &ce) || ce.Object != "hero" || ce.Field != "gravty" {
		t.Fatalf("misspelled field: err = %v", err)
	}
	if err := ed.SetComponentField("hero", "physics", "layer", 40); err == nil || !strings.Contains(err.Error(), "above the maximum 31") {
		t.Fatalf("out of range: err = %v", err)
	}
	if err := ed.SetComponentField("hero", "physics", "solid", "yes"); err == nil {
		t.Fatal("wrong kind accepted")
	}
	if err := ed.SetComponentField("hero", "audio", "volume", 1.0); err == nil {
		t.Fatal("unregistered component accepted")
	}
	if _, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{ID: "slime", Components: map[string]any{"physics": map[string]any{"slid": true}}}); err == nil {
		t.Fatal("PlaceObject accepted a misspelled component field")
	}
	after, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	if string(after) != string(before) || ed.History.CanUndo() {
		t.Fatal("refused edits changed the scene or the history")
	}

	// An int given to a float field is stored as float64 so the saved file keeps one representation.
	if err := ed.SetComponentField("hero", "physics", "gravity", 3); err != nil {
		t.Fatalf("SetComponentField gravity: %v", err)
	}
	hero, _, _ := ed.FindObject("hero")
	if g := hero.Components["physics"].(map[string]any)["gravity"]; g != 3.0 {
		t.Fatalf("gravity stored as %#v, want float64 3", g)
	}

	other := newTestEditor(t)
	reg := sceneformat.NewComponentRegistry()
	if err := other.SetComponentRegistry(reg); err == nil || other.ComponentRegistry() != nil {
		t.Fatalf("SetComponentRegistry accepted a registry the open scene does not conform to: %v", err)
	}
}

func TestSetComponentRegistryClearsHistory(t *testing.T) {
	ed := newTestEditor(t)
	// Without a registry the misspelled field is accepted; undo removes it so the scene conforms again.
	if err := ed.SetComponentField("hero", "physics", "gravty", 1.0); err != nil {
		t.Fatalf("SetComponentField without a registry: %v", err)
	}
	if !ed.Undo() {
		t.Fatal("Undo failed")
	}
	if err := ed.SetComponentRegistry(physicsComponents(t)); err != nil {
		t.Fatalf("SetComponentRegistry: %v", err)
	}
	if ed.History.CanUndo() || ed.History.CanRedo() {
		t.Fatal("SetComponentRegistry kept the undo/redo history")
	}
	if ed.Redo() {
		t.Fatal("Redo brought back a field the registry refuses")
	}
	if err := sceneformat.ValidateComponents(ed.Doc, ed.ComponentRegistry()); err != nil {
		t.Fatalf("scene no longer conforms: %v", err)
	}

	// Setting the same registry again leaves history alone.
	if err := ed.SetComponentField("hero", "physics", "gravity", 2.0); err != nil {
		t.Fatalf("SetComponentField gravity: %v", err)
	}
	if err := ed.SetComponentRegistry(ed.ComponentRegistry()); err != nil || !ed.History.CanUndo() {
		t.Fatalf("re-setting the same registry: err = %v, CanUndo = %v", err, ed.History.CanUndo())
	}
}

func TestInspectorShowsRegisteredFloatsAtFullPrecision(t *testing.T) {
	ed := newTestEditor(t)
	if err := ed.SetComponentRegistry(physicsComponents(t)); err != nil {
		t.Fatalf("SetComponentRegistry: %v", err)
	}
	if err := ed.SetComponentField("hero", "physics", "gravity", 9.81); err != nil {
		t.Fatalf("SetComponentField gravity: %v", err)
	}
	ed.SelectObject("hero")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 1400))

	if got := fieldText(app.Find("id:inspector-comp-physics-gravity")); got != "9.81" {
		t.Fatalf("gravity shows %q, want 9.81", got)
	}
}

func TestOpenFileReportsMisspelledComponentFieldWithFileAndLine(t *testing.T) {
	ed := newTestEditor(t)
	reg := physicsComponents(t)
	if err := ed.SetComponentRegistry(reg); err != nil {
		t.Fatalf("SetComponentRegistry: %v", err)
	}
	raw, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	lines := strings.Split(string(raw), "\n")
	line := 0
	for i, l := range lines {
		if strings.TrimSpace(l) == "gravity: 9.8" {
			lines[i] = strings.Replace(l, "gravity", "gravty", 1)
			line = i + 1
		}
	}
	if line == 0 {
		t.Fatalf("saved scene has no gravity line:\n%s", raw)
	}
	path := filepath.Join(t.TempDir(), "level.scene.yaml")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	err = ed.OpenFile(path)
	var ce *sceneformat.ComponentError
	if !errors.As(err, &ce) || ce.File != path || ce.Line != line || ce.Field != "gravty" {
		t.Fatalf("OpenFile err = %v, want %s:%d field gravty", err, path, line)
	}
	if !strings.HasPrefix(err.Error(), path+":"+strconv.Itoa(line)+":") {
		t.Fatalf("message %q does not start with the file and line", err)
	}
	if ed.FilePath != "" {
		t.Fatalf("a refused open set FilePath to %q", ed.FilePath)
	}
}

// fieldText returns the text of the first view under v that shows any, which for a ui.NumberInput is its field's value.
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
