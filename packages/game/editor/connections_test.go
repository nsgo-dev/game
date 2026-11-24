package editor_test

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

func TestDeleteObjectTakesItsConnectionsAndUndoPutsThemBack(t *testing.T) {
	ed := newTestEditor(t)
	for _, o := range []sceneformat.ObjectDoc{
		{ID: "button"},
		{ID: "door", Children: []sceneformat.ObjectDoc{{ID: "hinge"}}},
		{ID: "lamp"},
	} {
		if _, err := ed.PlaceObject("actors", o); err != nil {
			t.Fatalf("PlaceObject %s: %v", o.ID, err)
		}
	}
	ed.Doc.Connections = []sceneformat.ConnectionDoc{
		{From: "button", Event: "pressed", To: "lamp", Handler: "on"},
		{From: "button", Event: "pressed", To: "door", Handler: "open", Args: map[string]any{"speed": 2}},
		{From: "hinge", Event: "creaked", To: "lamp", Handler: "flicker"},
		{From: "lamp", Event: "lit", To: "button", Handler: "glow"},
	}
	before, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}

	if err := ed.DeleteObject("door"); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
	want := []sceneformat.ConnectionDoc{
		{From: "button", Event: "pressed", To: "lamp", Handler: "on"},
		{From: "lamp", Event: "lit", To: "button", Handler: "glow"},
	}
	if !reflect.DeepEqual(ed.Doc.Connections, want) {
		t.Fatalf("connections after delete = %+v, want %+v", ed.Doc.Connections, want)
	}
	if _, err := ed.SaveYAML(); err != nil {
		t.Fatalf("SaveYAML after delete: %v", err)
	}

	if !ed.Undo() {
		t.Fatal("Undo delete failed")
	}
	if after, err := ed.SaveYAML(); err != nil || !bytes.Equal(after, before) {
		t.Fatalf("undoing the delete did not restore the connections (%v):\n%s\nwant:\n%s", err, after, before)
	}
	if !ed.Redo() {
		t.Fatal("Redo delete failed")
	}
	if !reflect.DeepEqual(ed.Doc.Connections, want) {
		t.Fatalf("connections after redo = %+v, want %+v", ed.Doc.Connections, want)
	}

	// Deleting the lamp takes both connections that are left, since each names it.
	if err := ed.DeleteObject("lamp"); err != nil {
		t.Fatalf("DeleteObject lamp: %v", err)
	}
	if len(ed.Doc.Connections) != 0 {
		t.Fatalf("connections after deleting lamp = %+v", ed.Doc.Connections)
	}
	if !ed.Undo() || !reflect.DeepEqual(ed.Doc.Connections, want) {
		t.Fatalf("undoing the lamp delete: connections = %+v", ed.Doc.Connections)
	}
}

func TestDeleteObjectKeepsASiblingWhoseIDHasItAsPrefix(t *testing.T) {
	ed := newTestEditor(t)
	for _, o := range []sceneformat.ObjectDoc{{ID: "door"}, {ID: "door/x"}, {ID: "lamp"}} {
		if _, err := ed.PlaceObject("actors", o); err != nil {
			t.Fatalf("PlaceObject %s: %v", o.ID, err)
		}
	}
	ed.Doc.Connections = []sceneformat.ConnectionDoc{
		{From: "door", Event: "opened", To: "lamp", Handler: "on"},
		{From: "door/x", Event: "creaked", To: "lamp", Handler: "flicker"},
		{From: "lamp", Event: "lit", To: "door/x", Handler: "glow"},
	}
	if err := ed.DeleteObject("door"); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
	want := []sceneformat.ConnectionDoc{
		{From: "door/x", Event: "creaked", To: "lamp", Handler: "flicker"},
		{From: "lamp", Event: "lit", To: "door/x", Handler: "glow"},
	}
	if !reflect.DeepEqual(ed.Doc.Connections, want) {
		t.Fatalf("connections after deleting door = %+v, want %+v", ed.Doc.Connections, want)
	}
}

func TestDeleteObjectTakesTheConnectionsOfTheSceneItInstances(t *testing.T) {
	doc := testSceneDoc()
	resolver := sceneFiles{AtlasMap: testResolver(), files: map[string]string{"scenes/slime.scene.yaml": slimeScene}}
	ed, err := editor.New(doc, resolver)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	inst, err := ed.PlaceScene("actors", "scenes/slime.scene.yaml", 0, 0)
	if err != nil {
		t.Fatalf("PlaceScene: %v", err)
	}
	if _, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{ID: "lamp"}); err != nil {
		t.Fatalf("PlaceObject lamp: %v", err)
	}
	ed.Doc.Connections = []sceneformat.ConnectionDoc{
		{From: inst + "/body", Event: "hit", To: "lamp", Handler: "on"},
		{From: "lamp", Event: "lit", To: "lamp", Handler: "glow"},
	}
	if err := ed.DeleteObject(inst); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
	want := []sceneformat.ConnectionDoc{{From: "lamp", Event: "lit", To: "lamp", Handler: "glow"}}
	if !reflect.DeepEqual(ed.Doc.Connections, want) {
		t.Fatalf("connections after deleting %s = %+v, want %+v", inst, ed.Doc.Connections, want)
	}
}

func TestSetComponentRegistryChecksTheOpenScenesConnections(t *testing.T) {
	ed, err := editor.New(testSceneDoc(), testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SetActiveLayer("actors")
	for _, o := range []sceneformat.ObjectDoc{
		{ID: "button", Components: map[string]any{"button": map[string]any{}}},
		{ID: "door", Components: map[string]any{"door": map[string]any{}}},
	} {
		if _, err := ed.PlaceObject("actors", o); err != nil {
			t.Fatalf("PlaceObject %s: %v", o.ID, err)
		}
	}
	ed.Doc.Connections = []sceneformat.ConnectionDoc{{From: "button", Event: "pressed", To: "door", Handler: "open"}}

	// physicsComponents covers the components testSceneDoc's objects already have.
	registry := func(handlers ...sceneformat.Handler) *sceneformat.ComponentRegistry {
		reg := physicsComponents(t)
		for _, ct := range []sceneformat.ComponentType{
			{Name: "button", Events: []sceneformat.Event{{Name: "pressed"}}},
			{Name: "door", Handlers: handlers},
		} {
			if err := reg.Register(ct); err != nil {
				t.Fatalf("Register %s: %v", ct.Name, err)
			}
		}
		return reg
	}

	var connErr *sceneformat.ConnectionError
	if err := ed.SetComponentRegistry(registry()); !errors.As(err, &connErr) || connErr.Field != "handler" {
		t.Fatalf("SetComponentRegistry with a registry lacking door.open = %v, want a *ConnectionError on the handler", err)
	}
	if ed.ComponentRegistry() != nil {
		t.Fatal("a refused registry was set")
	}
	reg := registry(sceneformat.Handler{Name: "open"})
	if err := ed.SetComponentRegistry(reg); err != nil {
		t.Fatalf("SetComponentRegistry: %v", err)
	}
	data, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	if err := ed.OpenYAML(data); err != nil {
		t.Fatalf("OpenYAML of the saved scene with the same registry: %v", err)
	}
}
