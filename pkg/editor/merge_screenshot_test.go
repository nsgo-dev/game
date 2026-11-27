package editor_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/tooling/assetcheck"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const macosMergeBaseScene = `version: 3
name: level_01
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    space: world
    order: 0
    objects:
      - id: floor
        x: 0
        y: 300
      - id: plank
        x: 344
        y: 256
      - id: knight
        name: Knight
        x: 400
        y: 222
        z: 10
      - id: door
        name: Door
        prefab: prefabs/door.prefab.yaml
        x: 584
        y: 260
        components:
          trigger:
            kind: exit
      - id: slime_01
        prefab: prefabs/slime.prefab.yaml
        x: 170
        y: 280
      - id: slime_02
        prefab: prefabs/slime.prefab.yaml
        x: 240
        y: 280
        components:
          patrol:
            from: 220
            to: 340
            speed: 24
      - id: torch_02
        x: 120
        y: 264
        properties:
          light: 48
`

const macosMergeMineScene = `version: 3
name: level_01
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    space: world
    order: 0
    objects:
      - id: floor
        x: 0
        y: 300
      - id: plank
        x: 344
        y: 256
      - id: knight
        name: Knight
        x: 400
        y: 222
        z: 12
      - id: door
        name: Door
        prefab: prefabs/door.prefab.yaml
        x: 560
        y: 260
        components:
          trigger:
            kind: exit
        properties:
          open_sfx: audio/door_creak.ogg
      - id: slime_01
        prefab: prefabs/slime.prefab.yaml
        x: 170
        y: 280
      - id: slime_02
        prefab: prefabs/slime.prefab.yaml
        x: 240
        y: 280
        components:
          patrol:
            from: 220
            to: 340
            speed: 30
      - id: slime_03
        prefab: prefabs/slime.prefab.yaml
        x: 452
        y: 236
      - id: coin_01
        x: 496
        y: 282
      - id: coin_02
        x: 516
        y: 282
      - id: coin_03
        x: 536
        y: 282
      - id: torch_02
        x: 120
        y: 264
        properties:
          light: 48
`

const macosMergeTheirsScene = `version: 3
name: level_01
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    space: world
    order: 0
    objects:
      - id: floor
        x: 0
        y: 300
      - id: plank
        x: 344
        y: 256
      - id: knight
        name: Knight
        x: 400
        y: 222
        z: 11
      - id: door
        name: Door
        prefab: prefabs/door.prefab.yaml
        x: 600
        y: 260
        components:
          trigger:
            kind: exit
        properties:
          locked_by: lever_01
      - id: slime_01
        prefab: prefabs/slime.prefab.yaml
        x: 170
        y: 280
      - id: lever_01
        x: 360
        y: 238
      - id: torch_02
        x: 120
        y: 264
        properties:
          light: 64
`

// TestSceneMergeConflictMacOSScreenshots renders the Scene Merge Conflict screen on the real
// native macOS host (linking github.com/nsgo-dev/nsgo through editor_test.go) and captures
// unresolved, resolved, and merged-verified screenshots for visual review beside VersionControl.dc.html.
func TestSceneMergeConflictMacOSScreenshots(t *testing.T) {
	baseDoc, err := sceneformat.Parse([]byte(macosMergeBaseScene))
	if err != nil {
		t.Fatal(err)
	}
	mineDoc, err := sceneformat.Parse([]byte(macosMergeMineScene))
	if err != nil {
		t.Fatal(err)
	}
	theirsDoc, err := sceneformat.Parse([]byte(macosMergeTheirsScene))
	if err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(mineDoc.Clone(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.MergeCheckRunner = func(dir, scenePath string, doc *sceneformat.SceneDoc) ([]assetcheck.Problem, error) {
		return nil, nil
	}
	ed.MergePlaytestRunner = func(dir, scenePath string, doc *sceneformat.SceneDoc) (editor.MergePlaytestResult, error) {
		return editor.MergePlaytestResult{
			Duration: 10 * time.Second,
			Steps:    600,
			Summary:  "The agent ran game check and a 10 s playtest on the result: no problems, 3 of 3 coins reachable.",
		}, nil
	}

	if err := ed.OpenMergeConflictSession(editor.MergeConflictSessionConfig{
		ScenePath:   "scenes/level_01.scene.yaml",
		ProjectName: "dungeon-run",
		SceneName:   "level_01",
		Mine:        editor.MergeSideMeta{Branch: "feat/slime-patrol", Author: "you", Commit: "9f3c2d1"},
		Theirs:      editor.MergeSideMeta{Branch: "origin/main", Author: "mia", Commit: "7c21f0e", Message: "Thin out the lower floor for the lever"},
		BaseDoc:     baseDoc,
		MineDoc:     mineDoc,
		TheirsDoc:   theirsDoc,
	}); err != nil {
		t.Fatalf("OpenMergeConflictSession: %v", err)
	}
	if err := ed.ResolveMergeConflict("slime_02", editor.MergeChoiceTheirs); err != nil {
		t.Fatal(err)
	}
	if err := ed.FocusMergeConflict("door"); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	app.Screenshot("merge-conflict-unresolved")

	app.Press("id:merge-take-all-agent")
	app.Screenshot("merge-conflict-resolved")

	app.Press("id:merge-finish")
	app.Screenshot("merge-conflict-finished")
}

// TestSceneRebaseConflictMacOSScreenshots renders the Scene Merge Conflict screen during a
// git rebase on the native macOS host, confirming Mine shows the person's commit being replayed
// and Upstream shows the target branch, with Continue rebase / Abort rebase controls.
func TestSceneRebaseConflictMacOSScreenshots(t *testing.T) {
	baseDoc, err := sceneformat.Parse([]byte(macosMergeBaseScene))
	if err != nil {
		t.Fatal(err)
	}
	mineDoc, err := sceneformat.Parse([]byte(macosMergeMineScene))
	if err != nil {
		t.Fatal(err)
	}
	theirsDoc, err := sceneformat.Parse([]byte(macosMergeTheirsScene))
	if err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(mineDoc.Clone(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.MergeCheckRunner = func(dir, scenePath string, doc *sceneformat.SceneDoc) ([]assetcheck.Problem, error) {
		return nil, nil
	}
	ed.MergePlaytestRunner = func(dir, scenePath string, doc *sceneformat.SceneDoc) (editor.MergePlaytestResult, error) {
		return editor.MergePlaytestResult{
			Duration: 10 * time.Second,
			Steps:    600,
			Summary:  "The agent ran game check and a 10 s playtest on the result: no problems, 3 of 3 coins reachable.",
		}, nil
	}

	if err := ed.OpenMergeConflictSession(editor.MergeConflictSessionConfig{
		ScenePath:   "scenes/level_01.scene.yaml",
		ProjectName: "dungeon-run",
		SceneName:   "level_01",
		Operation:   editor.MergeOperationRebase,
		Mine: editor.MergeSideMeta{
			Branch:  "feat/slime-patrol",
			Author:  "you",
			Commit:  "9f3c2d1",
			Message: "Add a plank guard and a coin trail to level_01",
		},
		Theirs: editor.MergeSideMeta{
			Branch:  "origin/main",
			Author:  "mia",
			Commit:  "7c21f0e",
			Message: "Thin out the lower floor for the lever",
		},
		BaseDoc:   baseDoc,
		MineDoc:   mineDoc,
		TheirsDoc: theirsDoc,
	}); err != nil {
		t.Fatalf("OpenMergeConflictSession: %v", err)
	}
	if err := ed.ResolveMergeConflict("slime_02", editor.MergeChoiceTheirs); err != nil {
		t.Fatal(err)
	}
	if err := ed.FocusMergeConflict("door"); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	app.Screenshot("rebase-conflict-unresolved")

	app.Press("id:merge-take-all-agent")
	app.Screenshot("rebase-conflict-resolved")

	app.Press("id:merge-finish")
	app.Screenshot("rebase-conflict-finished")
}
