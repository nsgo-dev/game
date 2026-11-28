package editormcp_test

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editormcp"
	"github.com/nsgo-dev/game/pkg/sceneformat"
)

// bareCrateYAML is a bare object mapping with no top-level id and no layer of its own, so object_paste_yaml holds the destination layer and only the call or the turn can name that layer.
const bareCrateYAML = "name: crate\nx: 12\ny: 34\n"

// newTwoLayerEditor returns newSlimesEditor's scene with a second layer, Props, holding crate_01, so a paste has two layers to land on.
func newTwoLayerEditor(t *testing.T) *editor.Editor {
	t.Helper()
	ed := newSlimesEditor(t)
	ed.Doc.Layers = append(ed.Doc.Layers, sceneformat.LayerDoc{
		Name:    "Props",
		Space:   sceneformat.SpaceWorld,
		Objects: []sceneformat.ObjectDoc{{ID: "crate_01", Name: "crate_01", X: 40, Y: 40}},
	})
	ed.ActiveLayer = "Actors"
	return ed
}

// layerOf returns the name of the layer that holds id in the open scene, or "" when no layer does.
func layerOf(ed *editor.Editor, id string) string {
	if _, li, _ := ed.FindObject(id); li >= 0 {
		return ed.Doc.Layers[li].Name
	}
	return ""
}

// #1982 Acceptance: inside a turn pinned on an Actors object, object_paste_yaml without layer holds layer:Actors and pastes onto Actors, even though the person selected a Props object and made Props the active layer while the call waited on its hold.
func TestObjectPasteYAMLHoldAndWriteUseOneResolvedLayer(t *testing.T) {
	ed := newTwoLayerEditor(t)
	s := editormcp.New(ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	if _, err := ed.SubmitTask(editor.TaskRequest{ID: "t1", Number: 1, Title: "Task 1", StartMode: editor.TaskStartParallel}); err != nil {
		t.Fatalf("SubmitTask t1: %v", err)
	}
	c1 := connectTaskClient(t, s, "t1")
	mustOK(t, call(t, c1, editormcp.ToolObjectPasteYAML, map[string]any{"yaml": bareCrateYAML, "layer": "Actors"}))
	if held := s.HeldObjects(); held["layer:Actors"] != "t1" {
		t.Fatalf("t1 does not hold layer:Actors: %+v", held)
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
			ed.SelectObject("crate_01")
			ed.ActiveLayer = "Props"
			return nil
		})
		s.ReleaseTaskHolds("t1")
	})

	pinTurn(t, ed, "slime_01")
	ed.SelectObject("slime_01")

	res := call(t, cs, editormcp.ToolObjectPasteYAML, map[string]any{"yaml": bareCrateYAML})
	mustOK(t, res)
	mu.Lock()
	got := append([]string(nil), blocked...)
	mu.Unlock()
	if len(got) == 0 || got[0] != "layer:Actors" {
		t.Fatalf("object_paste_yaml waited on %v, want layer:Actors held", got)
	}
	if ed.ActiveLayer != "Props" {
		t.Fatalf("the hook did not move the active layer: %q", ed.ActiveLayer)
	}
	var out editormcp.ObjectPasteYAMLResult
	structured(t, res, &out)
	if l := layerOf(ed, out.ID); l != "Actors" {
		t.Fatalf("pasted %s onto %q, want the held Actors", out.ID, l)
	}
	if out.Layer != "Actors" {
		t.Fatalf("reported layer = %q, want Actors", out.Layer)
	}
	if !strings.Contains(text(res), "Actors") {
		t.Fatalf("result text %q does not name the layer", text(res))
	}
}

// #1982 Acceptance: the result names the layer the object was pasted into, for an explicit layer and for the layer a copied scene YAML names.
func TestObjectPasteYAMLReportsTheLayer(t *testing.T) {
	ed := newTwoLayerEditor(t)
	s := editormcp.New(ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)
	ed.SelectObject("slime_01")

	res := call(t, cs, editormcp.ToolObjectPasteYAML, map[string]any{"yaml": bareCrateYAML, "layer": "Props"})
	mustOK(t, res)
	var out editormcp.ObjectPasteYAMLResult
	structured(t, res, &out)
	if out.Layer != "Props" || layerOf(ed, out.ID) != "Props" {
		t.Fatalf("explicit layer paste = %+v on %q, want Props", out, layerOf(ed, out.ID))
	}

	copied, _, err := ed.CopyObjectYAML("crate_01")
	if err != nil {
		t.Fatal(err)
	}
	res = call(t, cs, editormcp.ToolObjectPasteYAML, map[string]any{"yaml": copied})
	mustOK(t, res)
	out = editormcp.ObjectPasteYAMLResult{}
	structured(t, res, &out)
	if out.Layer != "Props" || layerOf(ed, out.ID) != "Props" {
		t.Fatalf("copied scene YAML paste = %+v on %q, want the Props layer it names, not the selected slime_01's Actors", out, layerOf(ed, out.ID))
	}
}

// #1982: outside a turn, a paste whose YAML names no layer is refused with an EditError naming layer, and the scene and its history stay as they were, rather than landing on the selected object's layer or the active layer.
func TestObjectPasteYAMLWithoutLayerOutsideATurnIsRefused(t *testing.T) {
	ed := newTwoLayerEditor(t)
	s := editormcp.New(ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)
	ed.SelectObject("crate_01")

	before, err := ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}
	histBefore := ed.History.Index()
	res := call(t, cs, editormcp.ToolObjectPasteYAML, map[string]any{"yaml": bareCrateYAML})
	if !res.IsError {
		t.Fatalf("object_paste_yaml without layer outside a turn succeeded: %s", text(res))
	}
	var out struct {
		Error editormcp.EditError `json:"error"`
	}
	structured(t, res, &out)
	if !strings.Contains(out.Error.Fix, "pass layer") {
		t.Fatalf("fix = %q, want it to name layer", out.Error.Fix)
	}
	after, err := ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || ed.History.Index() != histBefore {
		t.Fatal("a refused paste changed the scene or its history")
	}
}

// #1982: a turn pinned on an object that was since removed refuses an id-less paste with the removed-or-renamed problem instead of pasting onto the selection's layer.
func TestObjectPasteYAMLRefusesARemovedPinnedTarget(t *testing.T) {
	ed := newTwoLayerEditor(t)
	s := editormcp.New(ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	pinTurn(t, ed, "slime_01")
	if err := ed.DeleteObject("slime_01"); err != nil {
		t.Fatal(err)
	}
	ed.SelectObject("crate_01")

	res := call(t, cs, editormcp.ToolObjectPasteYAML, map[string]any{"yaml": bareCrateYAML})
	if !res.IsError {
		t.Fatalf("paste with a removed pinned target succeeded: %s", text(res))
	}
	var out struct {
		Error editormcp.EditError `json:"error"`
	}
	structured(t, res, &out)
	if !strings.Contains(out.Error.Problem, "removed or renamed") || !strings.Contains(out.Error.Fix, "scene_read") {
		t.Fatalf("error = %+v, want the removed-or-renamed problem and a fix naming scene_read", out.Error)
	}
}
