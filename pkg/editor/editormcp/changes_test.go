package editormcp_test

import (
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor/editormcp"
)

// #1934: scene_edit reports each field it set with the value read back before and after, and whether it changed.
func TestSceneEditReportsFieldChanges(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	res := call(t, cs, editormcp.ToolSceneEdit, map[string]any{"edits": []any{
		map[string]any{"op": "set", "id": "hero", "fields": map[string]any{"x": 10, "name": "Hero", "sprite": map[string]any{"atlas": "actors", "frame": "slime"}}},
		map[string]any{"op": "set", "id": "hero", "component": "body", "fields": map[string]any{"speed": 60}},
		map[string]any{"op": "set_property", "id": "hero", "key": "team", "value": "red"},
	}})
	mustOK(t, res)
	var out editormcp.EditResult
	structured(t, res, &out)
	want := []editormcp.FieldChange{
		{Object: "hero", Field: "name", Before: "Hero", After: "Hero", Changed: false},
		{Object: "hero", Field: "sprite", Before: map[string]any{"atlas": "actors", "frame": "hero"}, After: map[string]any{"atlas": "actors", "frame": "slime"}, Changed: true},
		{Object: "hero", Field: "x", Before: float64(64), After: float64(10), Changed: true},
		{Object: "hero", Component: "body", Field: "speed", Before: float64(48), After: float64(60), Changed: true},
		{Object: "hero", Property: true, Field: "team", Before: nil, After: "red", Changed: true},
	}
	if !reflect.DeepEqual(out.Changes, want) {
		t.Fatalf("scene_edit changes =\n%#v\nwant\n%#v", out.Changes, want)
	}
}

// #1934: renaming an object reads the value after the edit from the renamed object.
func TestSceneEditReportsRename(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	res := call(t, cs, editormcp.ToolSceneEdit, map[string]any{"edits": []any{
		map[string]any{"op": "set", "id": "hero", "fields": map[string]any{"id": "player", "x": 70}},
	}})
	mustOK(t, res)
	var out editormcp.EditResult
	structured(t, res, &out)
	want := []editormcp.FieldChange{
		{Object: "hero", Field: "id", Before: "hero", After: "player", Changed: true},
		{Object: "hero", Field: "x", Before: float64(64), After: float64(70), Changed: true},
	}
	if !reflect.DeepEqual(out.Changes, want) {
		t.Fatalf("scene_edit changes =\n%#v\nwant\n%#v", out.Changes, want)
	}
}

// #1934: object_set_field reports its change inside an agent turn too, where the edit is staged rather than recorded as an undo step.
func TestObjectSetFieldReportsChangeInTurn(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	mustOK(t, call(t, cs, editormcp.ToolTurnStart, map[string]any{"prompt": "Move the hero"}))
	res := call(t, cs, editormcp.ToolObjectSetField, map[string]any{"id": "hero", "field": "y", "value": 90})
	mustOK(t, res)
	var out editormcp.ObjectFieldResult
	structured(t, res, &out)
	want := editormcp.FieldChange{Object: "hero", Field: "y", Before: float64(40), After: float64(90), Changed: true}
	if !reflect.DeepEqual(out.Change, want) {
		t.Fatalf("object_set_field change = %+v, want %+v", out.Change, want)
	}
}
