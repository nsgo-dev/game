package editor

import (
	"slices"
	"testing"

	"github.com/nsgo-dev/game/pkg/sceneformat"
)

// #1983: a turn about the open scene pins its selection, and a turn about another scene, as a background task queued from it can be, pins none, since the open scene's IDs are not about that scene.
func TestCaptureAgentTurnContextSkipsTheSelectionOfAnotherScene(t *testing.T) {
	doc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "level_01",
		VirtualWidth:  640,
		VirtualHeight: 360,
		Layers: []sceneformat.LayerDoc{{
			Name:    "Actors",
			Space:   sceneformat.SpaceWorld,
			Objects: []sceneformat.ObjectDoc{{ID: "slime_01", Name: "Slime"}, {ID: "slime_02"}},
		}},
	}
	ed, err := New(doc, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	ed.ScenePath = "scenes/level_01.scene.yaml"
	ed.SelectObjects([]string{"slime_01", "slime_02"})

	open := ed.captureAgentTurnContext(ed.ScenePath)
	if !slices.Equal(open.SelectionIDs, []string{"slime_01", "slime_02"}) || !slices.Equal(open.SelectionNames, []string{"Slime", "slime_02"}) {
		t.Fatalf("open scene pinned %v %v, want [slime_01 slime_02] [Slime slime_02]", open.SelectionIDs, open.SelectionNames)
	}
	if open.ScenePath != ed.ScenePath || open.SceneName != "level_01" {
		t.Fatalf("open scene context = %+v, want path %s and name level_01", open, ed.ScenePath)
	}

	other := ed.captureAgentTurnContext("scenes/level_02.scene.yaml")
	if other.ScenePath != "scenes/level_02.scene.yaml" || len(other.SelectionIDs) != 0 || len(other.SelectionNames) != 0 || other.SceneName != "" {
		t.Fatalf("other scene context = %+v, want only its path and no selection", other)
	}
}
