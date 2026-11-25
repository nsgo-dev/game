package editor_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// reloaded saves ed's scene, opens the file in a new editor with the same registry and returns that editor's object id, failing when saving the reopened scene does not give the same bytes.
func reloaded(t *testing.T, ed *editor.Editor, id string) *sceneformat.ObjectDoc {
	t.Helper()
	data, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	again, err := editor.New(nil, ed.Resolver)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := again.SetComponentRegistry(ed.ComponentRegistry()); err != nil {
		t.Fatalf("SetComponentRegistry: %v", err)
	}
	if err := again.OpenYAML(data); err != nil {
		t.Fatalf("OpenYAML of the saved scene: %v\n%s", err, data)
	}
	resaved, err := again.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML of the reopened scene: %v", err)
	}
	if string(resaved) != string(data) {
		t.Fatalf("the reopened scene saves differently:\n%s\nwant\n%s", resaved, data)
	}
	obj, _, _ := again.FindObject(id)
	if obj == nil {
		t.Fatalf("reloaded scene has no object %q:\n%s", id, data)
	}
	return obj
}

// #865: a newly placed object gets a component, a field edit and a flip through the inspector, loses the component, each step saves and reloads, and Undo walks back through them.
func TestInspectorAddsEditsAndRemovesAComponentOfANewObject(t *testing.T) {
	ed := newTestEditor(t)
	if err := ed.SetComponentRegistry(physicsComponents(t)); err != nil {
		t.Fatalf("SetComponentRegistry: %v", err)
	}
	id, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{X: 10, Y: 20, Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "slime"}})
	if err != nil {
		t.Fatalf("PlaceObject: %v", err)
	}
	ed.SelectObject(id)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 1600))

	// The field filters the types the object lacks; the button adds one with its registered defaults.
	app.Type("id:inspector-add-component", "phy")
	app.Press("id:inspector-add-component-physics")
	want := map[string]any{"solid": true, "gravity": 9.8, "friction": 0.2, "layer": 0, "kind": "static"}
	obj, _, _ := ed.FindObject(id)
	if got := obj.Components["physics"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("physics after Add = %#v, want %#v", got, want)
	}
	if app.Exists("id:inspector-add-component-physics") {
		t.Fatal("the Add Component list still offers a component the object has")
	}
	if got := reloaded(t, ed, id).Components["physics"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("reloaded physics = %#v, want %#v", got, want)
	}

	app.Press("id:inspector-comp-physics-solid")
	if got := reloaded(t, ed, id).Components["physics"].(map[string]any)["solid"]; got != false {
		t.Fatalf("reloaded solid = %v after unchecking it, want false", got)
	}

	app.Press("id:inspector-flip-x")
	if r := reloaded(t, ed, id); !r.FlipX || r.FlipY {
		t.Fatalf("reloaded flips = %v, %v after flipping x", r.FlipX, r.FlipY)
	}

	app.Press("id:inspector-comp-physics-remove")
	if app.Exists("id:inspector-comp-physics") {
		t.Fatal("the inspector still shows the removed component")
	}
	if r := reloaded(t, ed, id); r.Components != nil {
		t.Fatalf("reloaded components = %#v after removing physics, want none", r.Components)
	}

	steps := []struct {
		label string
		check func(o *sceneformat.ObjectDoc) bool
	}{
		{"remove", func(o *sceneformat.ObjectDoc) bool { return o.Components["physics"] != nil && o.FlipX }},
		{"flip", func(o *sceneformat.ObjectDoc) bool {
			return !o.FlipX && o.Components["physics"].(map[string]any)["solid"] == false
		}},
		{"field", func(o *sceneformat.ObjectDoc) bool { return reflect.DeepEqual(o.Components["physics"], want) }},
		{"add", func(o *sceneformat.ObjectDoc) bool { return o.Components == nil }},
	}
	for _, s := range steps {
		app.Post(func() { ed.Undo() })
		app.Settle()
		if o := reloaded(t, ed, id); !s.check(o) {
			t.Fatalf("after undoing the %s: object = %+v", s.label, o)
		}
	}
	if app.Exists("id:inspector-comp-physics") || !app.Exists("id:inspector-add-component-physics") {
		t.Fatal("after undoing the add, the inspector does not offer physics again")
	}
}

// #865: adding a component the object has, or a property with an empty key, is refused in the status bar.
func TestInspectorRefusesDuplicateComponentAndEmptyPropertyKey(t *testing.T) {
	ed := newTestEditor(t)
	if err := ed.SetComponentRegistry(physicsComponents(t)); err != nil {
		t.Fatalf("SetComponentRegistry: %v", err)
	}
	ed.SelectObject("hero")
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 1600))
	before := ed.History.Len()

	app.Type("id:inspector-add-component", "physics")
	app.Submit("id:inspector-add-component")
	if !strings.Contains(ed.Status, `already has component "physics"`) {
		t.Fatalf("status after adding physics again = %q", ed.Status)
	}
	app.Clear("id:inspector-add-component")
	app.Type("id:inspector-add-component", "ghost")
	app.Submit("id:inspector-add-component")
	if !strings.Contains(ed.Status, "unknown component type") {
		t.Fatalf("status after adding an unregistered type = %q", ed.Status)
	}

	app.Press("id:inspector-add-prop")
	if !strings.Contains(ed.Status, "property key must not be empty") {
		t.Fatalf("status after adding an empty key = %q", ed.Status)
	}
	app.Type("id:inspector-add-prop-key", "hp")
	app.Press("id:inspector-add-prop")
	if !strings.Contains(ed.Status, `already has property "hp"`) {
		t.Fatalf("status after adding hp again = %q", ed.Status)
	}
	if ed.History.Len() != before {
		t.Fatalf("refused adds recorded %d steps", ed.History.Len()-before)
	}
}

func TestInspectorAddsAndDeletesProperties(t *testing.T) {
	ed := newTestEditor(t)
	ed.SelectObject("hero")
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 1600))

	app.Type("id:inspector-add-prop-key", "lives")
	app.Press("id:inspector-add-prop-kind")
	app.Press("role:option int")
	app.Submit("id:inspector-add-prop-key")
	hero, _, _ := ed.FindObject("hero")
	if v, ok := hero.Properties["lives"]; !ok || v != 0 {
		t.Fatalf("lives after Add = %#v, %v; want int 0", v, ok)
	}
	if got := fieldText(app.Find("id:inspector-add-prop-key")); got != "" {
		t.Fatalf("key field holds %q after adding, want it cleared", got)
	}
	// The new property has its kind's control.
	app.Clear("id:inspector-prop-lives")
	app.Type("id:inspector-prop-lives", "3")
	app.Submit("id:inspector-prop-lives")
	if hero.Properties["lives"] != 3 {
		t.Fatalf("lives after typing 3 = %#v", hero.Properties["lives"])
	}

	app.Press("id:inspector-prop-title-delete")
	if _, ok := hero.Properties["title"]; ok || app.Exists("id:inspector-prop-title") {
		t.Fatal("title survives its delete button")
	}
	// The key is trimmed, and a float property takes a fractional value and keeps its kind through a reload.
	app.Type("id:inspector-add-prop-key", " drag ")
	app.Press("id:inspector-add-prop-kind")
	app.Press("role:option float")
	app.Press("id:inspector-add-prop")
	if v, ok := hero.Properties["drag"]; !ok || v != 0.0 {
		t.Fatalf("drag after Add = %#v, %v; want float 0", v, ok)
	}
	if r := reloaded(t, ed, "hero"); r.Properties["drag"] != 0.0 {
		t.Fatalf("reloaded drag = %#v, want float 0", r.Properties["drag"])
	}
	app.Clear("id:inspector-prop-drag")
	app.Type("id:inspector-prop-drag", "0.5")
	app.Submit("id:inspector-prop-drag")
	if hero.Properties["drag"] != 0.5 {
		t.Fatalf("drag after typing 0.5 = %#v", hero.Properties["drag"])
	}
	if r := reloaded(t, ed, "hero"); r.Properties["lives"] != 3 || r.Properties["title"] != nil || r.Properties["drag"] != 0.5 {
		t.Fatalf("reloaded properties = %#v", r.Properties)
	}
	// Undo takes back the drag's value, the drag and then the delete.
	app.Post(func() {
		for range 3 {
			ed.Undo()
		}
	})
	app.Settle()
	if _, ok := hero.Properties["drag"]; ok {
		t.Fatalf("drag survives undoing its add: %#v", hero.Properties)
	}
	if hero.Properties["title"] != "Knight" || !app.Exists("id:inspector-prop-title") {
		t.Fatalf("undoing the delete: title = %#v", hero.Properties["title"])
	}
}

func TestAddPropertyKindsAndRefusals(t *testing.T) {
	ed := newTestEditor(t)
	for kind, want := range map[sceneformat.FieldKind]any{
		sceneformat.FieldBool:   false,
		sceneformat.FieldInt:    0,
		sceneformat.FieldFloat:  0.0,
		sceneformat.FieldString: "",
	} {
		key := "new_" + kind.String()
		if err := ed.AddProperty("hero", key, kind); err != nil {
			t.Fatalf("AddProperty %s: %v", kind, err)
		}
		hero, _, _ := ed.FindObject("hero")
		if got := hero.Properties[key]; got != want {
			t.Fatalf("%s = %#v, want %#v", key, got, want)
		}
		if got := ed.History.UndoLabel(); got != "Add property "+key {
			t.Fatalf("undo label = %q", got)
		}
	}
	// Each property keeps its kind through a save and reload, the float's zero included.
	r := reloaded(t, ed, "hero")
	for key, want := range map[string]any{"new_bool": false, "new_int": 0, "new_float": 0.0, "new_string": ""} {
		if got := r.Properties[key]; got != want {
			t.Fatalf("reloaded %s = %#v, want %#v", key, got, want)
		}
	}
	for _, c := range []struct {
		id, key string
		kind    sceneformat.FieldKind
		want    string
	}{
		{"hero", "", sceneformat.FieldInt, "must not be empty"},
		{"hero", "  ", sceneformat.FieldInt, "must not be empty"},
		{"hero", "hp ", sceneformat.FieldInt, "whitespace"},
		{"hero", " tag", sceneformat.FieldInt, "whitespace"},
		{"hero", "hp", sceneformat.FieldInt, "already has property"},
		{"hero", "x", sceneformat.FieldKind(0), "unknown property kind"},
		{"nobody", "x", sceneformat.FieldInt, "not found"},
	} {
		if err := ed.AddProperty(c.id, c.key, c.kind); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("AddProperty(%q, %q, %v) = %v, want %q", c.id, c.key, c.kind, err, c.want)
		}
	}
}

func TestAddComponentWithoutRegistryAndItsRefusals(t *testing.T) {
	ed := newTestEditor(t)
	if err := ed.AddComponent("hero", "tag"); err != nil {
		t.Fatalf("AddComponent without a registry: %v", err)
	}
	hero, _, _ := ed.FindObject("hero")
	if got, ok := hero.Components["tag"].(map[string]any); !ok || len(got) != 0 {
		t.Fatalf("tag = %#v, want an empty component", hero.Components["tag"])
	}
	for _, c := range []struct{ id, comp, want string }{
		{"hero", "", "must not be empty"},
		{"hero", "tag ", "whitespace"},
		{"hero", " tag", "whitespace"},
		{"hero", "a.b", "must not contain '.'"},
		{"hero", "tag", "already has component"},
		{"nobody", "tag", "not found"},
	} {
		if err := ed.AddComponent(c.id, c.comp); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("AddComponent(%q, %q) = %v, want %q", c.id, c.comp, err, c.want)
		}
	}
	if err := ed.RemoveComponent("hero", "ghost"); err == nil || !strings.Contains(err.Error(), "has no component") {
		t.Fatalf("RemoveComponent of a missing component = %v", err)
	}
	if err := ed.SetComponentRegistry(physicsComponents(t)); err == nil {
		t.Fatal("a registry without tag accepted the scene")
	}
	if err := ed.RemoveComponent("hero", "tag"); err != nil {
		t.Fatalf("RemoveComponent: %v", err)
	}
	if err := ed.SetComponentRegistry(physicsComponents(t)); err != nil {
		t.Fatalf("SetComponentRegistry: %v", err)
	}
	var ce *sceneformat.ComponentError
	if err := ed.AddComponent("hero", "tag"); !errors.As(err, &ce) || ce.Object != "hero" || !strings.Contains(ce.Msg, "registered: physics") {
		t.Fatalf("AddComponent of an unregistered type = %v, want a *ComponentError listing physics", err)
	}
}

// switchboard places a lever and a door with a registry whose button emits pressed and whose door opens, connected twice: lever pressed -> door open, and lever button.pressed -> door door.open once.
func switchboard(t *testing.T) *editor.Editor {
	t.Helper()
	ed := newTestEditor(t)
	for _, o := range []sceneformat.ObjectDoc{
		{ID: "lever", Components: map[string]any{"button": map[string]any{}, "light": map[string]any{}}},
		{ID: "door", Components: map[string]any{"door": map[string]any{}}},
	} {
		if _, err := ed.PlaceObject("actors", o); err != nil {
			t.Fatalf("PlaceObject %s: %v", o.ID, err)
		}
	}
	ed.Doc.Connections = []sceneformat.ConnectionDoc{
		{From: "lever", Event: "pressed", To: "door", Handler: "open"},
		{From: "lever", Event: "glow", To: "door", Handler: "open"},
		{From: "lever", Event: "button.pressed", To: "door", Handler: "door.close", Once: true},
	}
	reg := physicsComponents(t)
	for _, ct := range []sceneformat.ComponentType{
		{Name: "button", Events: []sceneformat.Event{{Name: "pressed"}}},
		{Name: "light", Events: []sceneformat.Event{{Name: "glow"}}},
		{Name: "switch", Events: []sceneformat.Event{{Name: "pressed"}}, Fields: []sceneformat.Field{{Name: "on", Kind: sceneformat.FieldBool}}},
		{Name: "door", Handlers: []sceneformat.Handler{{Name: "open"}, {Name: "close"}}},
	} {
		if err := reg.Register(ct); err != nil {
			t.Fatalf("Register %s: %v", ct.Name, err)
		}
	}
	if err := ed.SetComponentRegistry(reg); err != nil {
		t.Fatalf("SetComponentRegistry: %v", err)
	}
	return ed
}

func TestRemoveComponentTakesTheConnectionsThatNeedIt(t *testing.T) {
	ed := switchboard(t)
	all := append([]sceneformat.ConnectionDoc(nil), ed.Doc.Connections...)
	if got := ed.ComponentConnections("lever", "button"); !reflect.DeepEqual(got, []int{0, 2}) {
		t.Fatalf("ComponentConnections(lever, button) = %v, want [0 2]", got)
	}
	if err := ed.RemoveComponent("lever", "button"); err != nil {
		t.Fatalf("RemoveComponent: %v", err)
	}
	if want := all[1:2]; !reflect.DeepEqual(ed.Doc.Connections, want) {
		t.Fatalf("connections after removing button = %+v, want %+v", ed.Doc.Connections, want)
	}
	if !strings.Contains(ed.Status, "and 2 connections") {
		t.Fatalf("status = %q", ed.Status)
	}
	// The scene still binds, so it saves and opens again.
	reloaded(t, ed, "lever")
	if !ed.Undo() || !reflect.DeepEqual(ed.Doc.Connections, all) {
		t.Fatalf("connections after undo = %+v, want %+v", ed.Doc.Connections, all)
	}
	if !ed.Redo() || len(ed.Doc.Connections) != 1 {
		t.Fatalf("connections after redo = %+v", ed.Doc.Connections)
	}
	ed.Undo()

	// The door's component is named by every connection's handler.
	if err := ed.RemoveComponent("door", "door"); err != nil {
		t.Fatalf("RemoveComponent door: %v", err)
	}
	if ed.Doc.Connections != nil {
		t.Fatalf("connections after removing door = %+v, want none", ed.Doc.Connections)
	}
	reloaded(t, ed, "door")
	if !ed.Undo() || !reflect.DeepEqual(ed.Doc.Connections, all) {
		t.Fatalf("connections after undoing the door = %+v", ed.Doc.Connections)
	}
}

func TestAddComponentRefusesToMakeAConnectionAmbiguous(t *testing.T) {
	ed := switchboard(t)
	err := ed.AddComponent("lever", "switch")
	var connErr *sceneformat.ConnectionError
	if !errors.As(err, &connErr) || !strings.Contains(err.Error(), "several components") {
		t.Fatalf("AddComponent(lever, switch) = %v, want a *ConnectionError about several components", err)
	}
	lever, _, _ := ed.FindObject("lever")
	if _, has := lever.Components["switch"]; has || len(lever.Components) != 2 {
		t.Fatalf("lever components after the refusal = %#v", lever.Components)
	}
	// The door names no bare event, so it may take the switch, with its registered defaults.
	if err := ed.AddComponent("door", "switch"); err != nil {
		t.Fatalf("AddComponent(door, switch): %v", err)
	}
	door, _, _ := ed.FindObject("door")
	if got := door.Components["switch"]; !reflect.DeepEqual(got, map[string]any{"on": false}) {
		t.Fatalf("switch = %#v", got)
	}
	reloaded(t, ed, "door")
}

// materialResolver serves testResolver's atlases, listing them for the asset browser, and the material files in lib, as a project's resolver does.
type materialResolver struct {
	sceneformat.AtlasMap
	lib *resource.Library
}

func (r materialResolver) Material(path string) (*resource.Material, error) {
	return r.lib.Material(path)
}

func (r materialResolver) AtlasNames() []string {
	names := make([]string, 0, len(r.AtlasMap))
	for n := range r.AtlasMap {
		names = append(names, n)
	}
	return names
}

// testMaterial is a material resource file's contents.
const testMaterial = "type: material\nversion: 1\nname: Test\nopacity: 0.5\n"

func TestSpriteOriginFlipAndMaterialSetters(t *testing.T) {
	ed := newTestEditor(t)
	// Without a MaterialResolver the scene could not build with a material, so naming one is refused.
	if err := ed.SetObjectMaterial("hero", "materials/glow.material.yaml"); err == nil || !strings.Contains(err.Error(), "MaterialResolver") {
		t.Fatalf("SetObjectMaterial without a MaterialResolver = %v", err)
	}
	ed.Resolver = materialResolver{testResolver(), resource.NewLibrary(fstest.MapFS{
		"materials/glow.material.yaml": {Data: []byte(testMaterial)},
	})}
	start, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	if err := ed.SetObjectSprite("hero", &sceneformat.SpriteRef{Atlas: "actors", Frame: "slime"}); err != nil {
		t.Fatalf("SetObjectSprite: %v", err)
	}
	if err := ed.SetObjectOrigin("hero", 4, 12); err != nil {
		t.Fatalf("SetObjectOrigin: %v", err)
	}
	if err := ed.SetObjectFlip("hero", true, true); err != nil {
		t.Fatalf("SetObjectFlip: %v", err)
	}
	if err := ed.SetObjectMaterial("hero", "materials/glow.material.yaml"); err != nil {
		t.Fatalf("SetObjectMaterial: %v", err)
	}
	if ed.BuildErr != nil {
		t.Fatalf("the scene does not build after the setters: %v", ed.BuildErr)
	}
	r := reloaded(t, ed, "hero")
	if *r.Sprite != (sceneformat.SpriteRef{Atlas: "actors", Frame: "slime"}) || r.OriginX != 4 || r.OriginY != 12 || !r.FlipX || !r.FlipY || r.Material != "materials/glow.material.yaml" {
		t.Fatalf("reloaded hero = %+v", r)
	}
	// Setting the value an object has records nothing.
	n := ed.History.Len()
	_ = ed.SetObjectOrigin("hero", 4, 12)
	_ = ed.SetObjectFlip("hero", true, true)
	_ = ed.SetObjectMaterial("hero", "materials/glow.material.yaml")
	_ = ed.SetObjectSprite("hero", &sceneformat.SpriteRef{Atlas: "actors", Frame: "slime"})
	if ed.History.Len() != n {
		t.Fatal("setting unchanged values recorded steps")
	}
	if err := ed.SetObjectSprite("hero", nil); err != nil {
		t.Fatalf("clearing the sprite: %v", err)
	}
	if hero, _, _ := ed.FindObject("hero"); hero.Sprite != nil {
		t.Fatal("sprite survives clearing")
	}
	for range 5 {
		ed.Undo()
	}
	if got, _ := ed.SaveYAML(); string(got) != string(start) {
		t.Fatalf("undoing every setter gives\n%s\nwant\n%s", got, start)
	}

	for name, err := range map[string]error{
		"missing frame":  ed.SetObjectSprite("hero", &sceneformat.SpriteRef{Atlas: "actors", Frame: "dragon"}),
		"missing atlas":  ed.SetObjectSprite("hero", &sceneformat.SpriteRef{Atlas: "ghosts", Frame: "hero"}),
		"empty frame":    ed.SetObjectSprite("hero", &sceneformat.SpriteRef{Atlas: "actors"}),
		"nan origin":     ed.SetObjectOrigin("hero", float32(nan()), 0),
		"escaping path":  ed.SetObjectMaterial("hero", "../shared.material.yaml"),
		"absolute path":  ed.SetObjectMaterial("hero", "/materials/a.material.yaml"),
		"missing file":   ed.SetObjectMaterial("hero", "materials/none.material.yaml"),
		"missing object": ed.SetObjectFlip("nobody", true, false),
	} {
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func nan() float64 {
	var zero float64
	return zero / zero
}

func TestInspectorSetsSpriteOriginFlipAndMaterial(t *testing.T) {
	dir := writeEditorProject(t)
	for _, f := range []string{"materials/glow.material.yaml", "materials/stone.material.yaml"} {
		p := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(testMaterial), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ed := newTestEditor(t)
	// The material picker lists the open project's material files.
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	ed.SelectObject("hero")
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 1600))

	if app.Enabled("id:inspector-sprite-use-asset") {
		t.Fatal("Use Selected is enabled with nothing selected in the asset browser")
	}
	// The material field is disabled until the resolver can load the project's material files; the sprite edits below show it again.
	if app.Enabled("id:inspector-material") {
		t.Fatal("the material field is enabled with a resolver that cannot load materials")
	}
	app.Post(func() {
		ed.Resolver = materialResolver{testResolver(), resource.NewLibrary(os.DirFS(dir))}
	})
	app.Press("id:asset-actors-slime")
	app.Press("id:inspector-sprite-use-asset")
	hero, _, _ := ed.FindObject("hero")
	if hero.Sprite == nil || *hero.Sprite != (sceneformat.SpriteRef{Atlas: "actors", Frame: "slime"}) {
		t.Fatalf("sprite after Use Selected = %+v", hero.Sprite)
	}
	app.Press("id:inspector-sprite")
	app.Press("role:option tiles/2")
	if *hero.Sprite != (sceneformat.SpriteRef{Atlas: "tiles", Frame: "2"}) {
		t.Fatalf("sprite after picking tiles/2 = %+v", hero.Sprite)
	}

	app.Clear("id:inspector-origin-x")
	app.Type("id:inspector-origin-x", "3")
	app.Submit("id:inspector-origin-x")
	app.Press("id:inspector-flip-y")
	app.Press("id:inspector-material")
	app.Press("role:option materials/stone.material.yaml")
	if hero.OriginX != 3 || hero.OriginY != 8 || hero.FlipX || !hero.FlipY || hero.Material != "materials/stone.material.yaml" {
		t.Fatalf("hero after the inspector edits = %+v", hero)
	}
	if got := fieldText(app.Find("id:inspector-material")); got != "materials/stone.material.yaml" {
		t.Fatalf("material field shows %q", got)
	}
	if ed.BuildErr != nil {
		t.Fatalf("the scene does not build with the material: %v", ed.BuildErr)
	}
	app.Post(func() { ed.Undo() })
	app.Settle()
	if hero.Material != "" || !hero.FlipY {
		t.Fatalf("undoing the material: hero = %+v", hero)
	}
}

// TestInspectorEditsAComponentWithoutARegisteredType checks that a component without a registered type keeps its editable fields and gains a Remove button.
func TestInspectorEditsAComponentWithoutARegisteredType(t *testing.T) {
	ed := newTestEditor(t)
	ed.SelectObject("hero")
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 1600))
	app.Clear("id:inspector-comp-physics-kind")
	app.Type("id:inspector-comp-physics-kind", "kinematic")
	hero, _, _ := ed.FindObject("hero")
	if got := hero.Components["physics"].(map[string]any)["kind"]; got != "kinematic" {
		t.Fatalf("kind = %v", got)
	}
	app.Press("id:inspector-comp-physics-remove")
	if hero.Components != nil {
		t.Fatalf("components after Remove = %#v", hero.Components)
	}
	// Without a registry the field names a component to add, which starts empty.
	app.Type("id:inspector-add-component", "marker")
	app.Submit("id:inspector-add-component")
	if got, ok := hero.Components["marker"].(map[string]any); !ok || len(got) != 0 {
		t.Fatalf("components after adding marker = %#v", hero.Components)
	}
}

func TestObjectEditsAreRefusedWhilePlaying(t *testing.T) {
	ed := newTestEditor(t)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 680))
	app.Press("id:toolbar-play")
	if !ed.Playing() {
		t.Fatal("not playing after pressing Play")
	}
	for name, err := range map[string]error{
		"AddComponent":      ed.AddComponent("hero", "tag"),
		"RemoveComponent":   ed.RemoveComponent("hero", "physics"),
		"AddProperty":       ed.AddProperty("hero", "lives", sceneformat.FieldInt),
		"SetObjectSprite":   ed.SetObjectSprite("hero", nil),
		"SetObjectOrigin":   ed.SetObjectOrigin("hero", 1, 1),
		"SetObjectFlip":     ed.SetObjectFlip("hero", true, false),
		"SetObjectMaterial": ed.SetObjectMaterial("hero", ""),
	} {
		if !errors.Is(err, editor.ErrPlaying) {
			t.Errorf("%s while playing = %v, want ErrPlaying", name, err)
		}
	}
	app.Press("id:toolbar-play")
	if ed.Playing() {
		t.Fatal("still playing after pressing Stop")
	}
}
