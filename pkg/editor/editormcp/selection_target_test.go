package editormcp_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editormcp"
)

// pinTurn sends an in-place request about ids, so the turn pins them as its selection, the way ⌘L does.
func pinTurn(t *testing.T, ed *editor.Editor, ids ...string) editor.AgentTurn {
	t.Helper()
	if err := ed.OpenAskInPlaceFor(ids...); err != nil {
		t.Fatalf("OpenAskInPlaceFor(%v): %v", ids, err)
	}
	ed.SetAskInPlaceDraft("make these faster")
	turn, err := ed.SubmitAskInPlace()
	if err != nil {
		t.Fatalf("SubmitAskInPlace: %v", err)
	}
	if !slices.Equal(turn.Context.SelectionIDs, ids) {
		t.Fatalf("turn pinned %v, want %v", turn.Context.SelectionIDs, ids)
	}
	return turn
}

// blockedHold makes task t1 hold holdID, and returns a recorder of the objects a later call waited on. While a call waits, the recorder selects reselect, as a person's click would, and releases t1's holds so the call goes on to its write.
func blockedHold(t *testing.T, ed *editor.Editor, s *editormcp.Server, holdID, reselect string) func() []string {
	t.Helper()
	if _, err := ed.SubmitTask(editor.TaskRequest{ID: "t1", Number: 1, Title: "Task 1", StartMode: editor.TaskStartParallel}); err != nil {
		t.Fatalf("SubmitTask t1: %v", err)
	}
	c1 := connectTaskClient(t, s, "t1")
	mustOK(t, call(t, c1, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{map[string]any{"op": "set", "id": holdID, "component": "patrol", "fields": map[string]any{"jump": true}}},
	}))
	if held := s.HeldObjects(); held[holdID] != "t1" {
		t.Fatalf("t1 does not hold %s: %+v", holdID, held)
	}
	s.SetHoldWaitTimeout(3 * time.Second)
	var (
		mu      sync.Mutex
		blocked []string
	)
	s.SetHoldBlockedHook(func(_, objectID, _ string) {
		mu.Lock()
		blocked = append(blocked, objectID)
		mu.Unlock()
		_ = s.Editor(context.Background(), func(ed *editor.Editor) error {
			ed.SelectObject(reselect)
			return nil
		})
		s.ReleaseTaskHolds("t1")
	})
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(blocked)
	}
}

// #1926 Acceptance: inside a turn, an edit tool without id edits, holds and reports the object the turn pinned, even though the person selected another object after sending the request and again while the call waited on its hold.
func TestEditToolsWithoutIDTargetTheTurnsPinnedSelection(t *testing.T) {
	cases := []struct {
		name  string
		tool  string
		pin   []string
		args  map[string]any
		check func(t *testing.T, ed *editor.Editor, res map[string]any)
	}{
		{
			name: "object_set_field",
			tool: editormcp.ToolObjectSetField,
			pin:  []string{"slime_01"},
			args: map[string]any{"field": "x", "value": 77},
			check: func(t *testing.T, ed *editor.Editor, res map[string]any) {
				if res["object"] != "slime_01" {
					t.Fatalf("reported object = %v, want slime_01", res["object"])
				}
				a, _, _ := ed.FindObject("slime_01")
				b, _, _ := ed.FindObject("slime_02")
				c, _, _ := ed.FindObject("slime_03")
				if a.X != 77 || b.X != 292 || c.X != 440 {
					t.Fatalf("x = %v, %v, %v; want only slime_01 at 77", a.X, b.X, c.X)
				}
			},
		},
		{
			name: "object_duplicate",
			tool: editormcp.ToolObjectDuplicate,
			pin:  []string{"slime_01"},
			args: map[string]any{},
			check: func(t *testing.T, ed *editor.Editor, res map[string]any) {
				if res["sourceId"] != "slime_01" {
					t.Fatalf("reported sourceId = %v, want slime_01", res["sourceId"])
				}
				dup, _, _ := ed.FindObject(res["id"].(string))
				if dup == nil || dup.Name != "slime_01" {
					t.Fatalf("duplicate = %+v, want a copy of slime_01", dup)
				}
			},
		},
		{
			name: "object_group",
			tool: editormcp.ToolObjectGroup,
			pin:  []string{"slime_01", "slime_02"},
			args: map[string]any{"name": "Pair"},
			check: func(t *testing.T, ed *editor.Editor, res map[string]any) {
				children, _ := res["children"].([]any)
				if len(children) != 2 || children[0] != "slime_01" || children[1] != "slime_02" {
					t.Fatalf("reported children = %v, want [slime_01 slime_02]", res["children"])
				}
				grp, _, _ := ed.FindObject(res["groupId"].(string))
				if grp == nil || len(grp.Children) != 2 || grp.Children[0].ID != "slime_01" || grp.Children[1].ID != "slime_02" {
					t.Fatalf("group = %+v, want slime_01 and slime_02 under it", grp)
				}
			},
		},
		{
			name: "object_move_under",
			tool: editormcp.ToolObjectMoveUnder,
			pin:  []string{"slime_01"},
			args: map[string]any{"parent": "slime_03"},
			check: func(t *testing.T, ed *editor.Editor, res map[string]any) {
				if res["id"] != "slime_01" {
					t.Fatalf("reported id = %v, want slime_01", res["id"])
				}
				parent, _, _ := ed.FindObject("slime_03")
				if parent == nil || len(parent.Children) != 1 || parent.Children[0].ID != "slime_01" {
					t.Fatalf("slime_03 children = %+v, want [slime_01]", parent)
				}
			},
		},
		{
			name: "object_visibility",
			tool: editormcp.ToolObjectVisibility,
			pin:  []string{"slime_01"},
			args: map[string]any{"hidden": true},
			check: func(t *testing.T, ed *editor.Editor, res map[string]any) {
				if res["id"] != "slime_01" {
					t.Fatalf("reported id = %v, want slime_01", res["id"])
				}
				if !ed.ObjectHidden("slime_01") || ed.ObjectHidden("slime_02") || ed.ObjectHidden("slime_03") {
					t.Fatalf("hidden = %v, %v, %v; want only slime_01", ed.ObjectHidden("slime_01"), ed.ObjectHidden("slime_02"), ed.ObjectHidden("slime_03"))
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ed := newSlimesEditor(t)
			s := editormcp.New(ed, editormcp.Options{})
			t.Cleanup(func() { _ = s.Close() })
			cs := connect(t, s, nil)

			blocked := blockedHold(t, ed, s, "slime_01", "slime_03")
			pinTurn(t, ed, tc.pin...)
			// The person clicks another object after sending the request.
			ed.SelectObject("slime_02")

			res := call(t, cs, tc.tool, tc.args)
			mustOK(t, res)
			if got := blocked(); len(got) == 0 || got[0] != "slime_01" {
				t.Fatalf("%s waited on %v, want the pinned slime_01 held", tc.tool, got)
			}
			var out map[string]any
			structured(t, res, &out)
			tc.check(t, ed, out)
		})
	}
}

// #1926 Acceptance: a click between taking the hold and the write no longer changes which object is written, because both use the one target resolved before the hold.
func TestObjectSetFieldHoldAndWriteUseOneResolvedTarget(t *testing.T) {
	ed := newSlimesEditor(t)
	s := editormcp.New(ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	blocked := blockedHold(t, ed, s, "slime_01", "slime_02")
	pinTurn(t, ed, "slime_01")
	ed.SelectObject("slime_01")

	res := call(t, cs, editormcp.ToolObjectSetField, map[string]any{"field": "y", "value": 11})
	mustOK(t, res)
	if got := blocked(); len(got) == 0 || got[0] != "slime_01" {
		t.Fatalf("object_set_field waited on %v, want slime_01", got)
	}
	if ed.SelectedID != "slime_02" {
		t.Fatalf("the hook did not move the selection: %q", ed.SelectedID)
	}
	var out editormcp.ObjectFieldResult
	structured(t, res, &out)
	a, _, _ := ed.FindObject("slime_01")
	b, _, _ := ed.FindObject("slime_02")
	if out.Object != "slime_01" || a.Y != 11 || b.Y != 260 {
		t.Fatalf("wrote %q (slime_01.y=%v, slime_02.y=%v), want slime_01 only", out.Object, a.Y, b.Y)
	}
}

// #1926 Acceptance: outside a turn, an edit tool without id is refused with an EditError naming the way out, and the scene, its undo history and its file stay as they were.
func TestEditToolsWithoutIDOutsideATurnAreRefused(t *testing.T) {
	tools := []struct {
		name string
		args map[string]any
	}{
		{editormcp.ToolObjectSetField, map[string]any{"field": "x", "value": 5}},
		{editormcp.ToolObjectDuplicate, map[string]any{}},
		{editormcp.ToolObjectGroup, map[string]any{}},
		{editormcp.ToolObjectMoveUnder, map[string]any{"parent": ""}},
		{editormcp.ToolObjectVisibility, map[string]any{"hidden": true}},
	}
	for _, tc := range tools {
		t.Run(tc.name, func(t *testing.T) {
			f := openFixture(t)
			s := editormcp.New(f.ed, editormcp.Options{})
			t.Cleanup(func() { _ = s.Close() })
			cs := connect(t, s, nil)

			f.ed.SelectObject("hero")
			scenePath := filepath.Join(f.dir, "scenes", "room.scene.yaml")
			fileBefore, err := os.ReadFile(scenePath)
			if err != nil {
				t.Fatal(err)
			}
			yamlBefore, err := f.ed.SaveYAML()
			if err != nil {
				t.Fatal(err)
			}
			histBefore := f.ed.History.Index()

			res := call(t, cs, tc.name, tc.args)
			if !res.IsError {
				t.Fatalf("%s without id outside a turn succeeded: %s", tc.name, text(res))
			}
			var out struct {
				Error editormcp.EditError `json:"error"`
			}
			structured(t, res, &out)
			if !strings.Contains(out.Error.Fix, "pass id") || !strings.Contains(out.Error.Fix, "selection_get") {
				t.Fatalf("%s fix = %q, want it to name id and selection_get", tc.name, out.Error.Fix)
			}
			yamlAfter, err := f.ed.SaveYAML()
			if err != nil {
				t.Fatal(err)
			}
			fileAfter, err := os.ReadFile(scenePath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(yamlBefore, yamlAfter) || !bytes.Equal(fileBefore, fileAfter) || f.ed.History.Index() != histBefore {
				t.Fatalf("%s changed the scene, its file or its history", tc.name)
			}
			if f.ed.ObjectHidden("hero") {
				t.Fatalf("%s hid hero", tc.name)
			}
		})
	}
}

// #1926 Acceptance: a pinned target that was deleted is refused with its own problem, distinct from an unknown explicit id.
func TestEditToolsRefuseARemovedPinnedTarget(t *testing.T) {
	ed := newSlimesEditor(t)
	s := editormcp.New(ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	pinTurn(t, ed, "slime_01")
	if err := ed.DeleteObject("slime_01"); err != nil {
		t.Fatal(err)
	}
	ed.SelectObject("slime_02")

	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{editormcp.ToolObjectSetField, map[string]any{"field": "x", "value": 5}},
		{editormcp.ToolObjectDuplicate, map[string]any{}},
		{editormcp.ToolObjectGroup, map[string]any{}},
		{editormcp.ToolObjectMoveUnder, map[string]any{"parent": "slime_03"}},
		{editormcp.ToolObjectVisibility, map[string]any{"hidden": true}},
	} {
		res := call(t, cs, tc.name, tc.args)
		if !res.IsError {
			t.Fatalf("%s on a removed pinned target succeeded: %s", tc.name, text(res))
		}
		var out struct {
			Error editormcp.EditError `json:"error"`
		}
		structured(t, res, &out)
		if !strings.Contains(out.Error.Problem, "removed or renamed") || !strings.Contains(out.Error.Fix, "scene_read") {
			t.Fatalf("%s error = %+v, want the removed-or-renamed problem and a fix naming scene_read", tc.name, out.Error)
		}
	}
	b, _, _ := ed.FindObject("slime_02")
	if b.X != 292 || ed.ObjectHidden("slime_02") {
		t.Fatalf("a refused call edited the selected slime_02: %+v", b)
	}

	res := call(t, cs, editormcp.ToolObjectSetField, map[string]any{"id": "slime_01", "field": "x", "value": 5})
	if !res.IsError || strings.Contains(text(res), "removed or renamed") {
		t.Fatalf("explicit unknown id error = %q, want the plain unknown-object error", text(res))
	}
}

// #1983 Acceptance: an Agent panel turn pins the object selected when it was sent, so an id-less object_set_field in that turn edits it after the person selects another object.
func TestAgentPanelTurnPinsTheSelectionItStartedWith(t *testing.T) {
	ed := newSlimesEditor(t)
	s := editormcp.New(ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	ed.SelectObject("slime_01")
	ed.StartAgentRequest("make this one faster")
	ed.SelectObject("slime_02")

	pin, ok := ed.OpenAgentTurnPin("")
	if !ok || !slices.Equal(pin.SelectionIDs, []string{"slime_01"}) {
		t.Fatalf("OpenAgentTurnPin = %+v, %v; want the panel turn pinning [slime_01]", pin, ok)
	}
	res := call(t, cs, editormcp.ToolObjectSetField, map[string]any{"field": "x", "value": 77})
	mustOK(t, res)
	var out editormcp.ObjectFieldResult
	structured(t, res, &out)
	a, _, _ := ed.FindObject("slime_01")
	b, _, _ := ed.FindObject("slime_02")
	if out.Object != "slime_01" || a.X != 77 || b.X != 292 {
		t.Fatalf("wrote %q (slime_01.x=%v, slime_02.x=%v), want slime_01 only", out.Object, a.X, b.X)
	}
}
