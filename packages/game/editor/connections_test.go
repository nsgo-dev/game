package editor_test

import (
	"bytes"
	"reflect"
	"testing"

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
