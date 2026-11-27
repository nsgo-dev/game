package editor_test

import (
	"errors"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/sceneformat"
)

// Group records the edits its function makes as one step that one Undo reverts and one Redo applies again.
func TestHistoryGroupIsOneStep(t *testing.T) {
	ed, err := editor.New(testSceneDoc(), testResolver())
	if err != nil {
		t.Fatal(err)
	}
	before := ed.History.Index()
	err = ed.History.Group("Agent: two edits", func() error {
		if _, err := ed.PlaceObject("", sceneformat.ObjectDoc{ID: "crate", X: 1, Y: 2}); err != nil {
			return err
		}
		return ed.MoveObject("crate", 5, 6)
	})
	if err != nil {
		t.Fatal(err)
	}
	if ed.History.Index() != before+1 || ed.History.UndoLabel() != "Agent: two edits" {
		t.Fatalf("history %d %q", ed.History.Index(), ed.History.UndoLabel())
	}
	if crate, _, _ := ed.FindObject("crate"); crate == nil || crate.X != 5 || ed.Built.Object("crate") == nil {
		t.Fatalf("crate %+v", crate)
	}
	if !ed.Undo() {
		t.Fatal("nothing to undo")
	}
	if crate, _, _ := ed.FindObject("crate"); crate != nil || ed.History.Index() != before {
		t.Fatalf("after undo: %+v", crate)
	}
	if !ed.Redo() {
		t.Fatal("nothing to redo")
	}
	if crate, _, _ := ed.FindObject("crate"); crate == nil || crate.X != 5 || crate.Y != 6 {
		t.Fatalf("after redo: %+v", crate)
	}
}

// A Group whose function fails undoes what it did and records nothing, keeping the redo tail; an empty group and a nested one record no step of their own.
func TestHistoryGroupFailureAndNesting(t *testing.T) {
	ed, err := editor.New(testSceneDoc(), testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ed.PlaceObject("", sceneformat.ObjectDoc{ID: "crate"}); err != nil {
		t.Fatal(err)
	}
	ed.Undo()
	want, err := ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}
	index, length := ed.History.Index(), ed.History.Len()
	refused := errors.New("refused")
	err = ed.History.Group("Agent", func() error {
		if _, err := ed.PlaceObject("", sceneformat.ObjectDoc{ID: "barrel"}); err != nil {
			return err
		}
		return refused
	})
	if !errors.Is(err, refused) {
		t.Fatalf("Group = %v", err)
	}
	got, err := ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) || ed.History.Index() != index || ed.History.Len() != length || !ed.History.CanRedo() {
		t.Fatalf("a failed group left history %d/%d and\n%s", ed.History.Index(), ed.History.Len(), got)
	}
	if err := ed.History.Group("empty", func() error { return nil }); err != nil || ed.History.Len() != length {
		t.Fatalf("an empty group: %v, %d steps", err, ed.History.Len())
	}
	err = ed.History.Group("outer", func() error {
		return ed.History.Group("inner", func() error {
			_, err := ed.PlaceObject("", sceneformat.ObjectDoc{ID: "barrel"})
			return err
		})
	})
	if err != nil || ed.History.UndoLabel() != "outer" || ed.History.CanRedo() {
		t.Fatalf("nested group: %v, %q", err, ed.History.UndoLabel())
	}
}
