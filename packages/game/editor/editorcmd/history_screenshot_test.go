package editorcmd_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestHistoryPanelMacOSScreenshots renders the History panel on the real native macOS host
// across All (day-grouped with Now, agent turns, manual edits, and Saved marker), Agent filter,
// and point-in-time read-only Preview (Dark and Light appearances) for visual inspection beside
// docs/design/editor/AskInPlace.dc.html, Checkpoints.dc.html, and Spec.dc.html.
func TestHistoryPanelMacOSScreenshots(t *testing.T) {
	dir := t.TempDir()
	projYAML := "version: 1\nname: dungeon-run\nid: dev.example.dungeonrun\nappVersion: 1.0.0\nwindow:\n  width: 640\n  height: 360\nautoload:\n  mainScene: level_01\n  scenes:\n    - name: level_01\n      path: scenes/level_01.scene.yaml\n"
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(projYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "level_01",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#181C24",
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{ID: "knight", Name: "Knight", X: 416, Y: 220},
					{ID: "door", Name: "Door", X: 560, Y: 204},
				},
			},
		},
	}
	rawScene, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scenes", "level_01.scene.yaml"), rawScene, 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	now := time.Date(2026, 11, 25, 18, 12, 0, 0, time.UTC)
	ed.SetHistoryClock(func() time.Time { return now })
	if err := ed.SetObjectZ("door", 2); err != nil {
		t.Fatal(err)
	}

	now = time.Date(2026, 11, 26, 14, 28, 0, 0, time.UTC)
	if err := ed.Save(); err != nil {
		t.Fatal(err)
	}

	now = time.Date(2026, 11, 26, 14, 31, 0, 0, time.UTC)
	if err := ed.RecordAgentTurn(editor.AgentTurnOptions{
		Title:       "built the first room",
		Detail:      "from the template",
		ChangeCount: 18,
		KeptCount:   18,
	}, func() error {
		_, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{ID: "torch_01", Name: "Torch", X: 180, Y: 160})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	now = time.Date(2026, 11, 26, 14, 39, 0, 0, time.UTC)
	if err := ed.MoveObject("knight", 452, 220); err != nil {
		t.Fatal(err)
	}
	manualID := ed.HistoryEntries()[0].ID

	now = time.Date(2026, 11, 26, 14, 41, 0, 0, time.UTC)
	if err := ed.RecordAgentTurn(editor.AgentTurnOptions{
		Title:       "slimes and coins",
		ChangeCount: 6,
		KeptCount:   5,
		Files:       []string{"prefabs/coin.prefab.yaml"},
	}, func() error {
		if _, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{ID: "slime_01", Name: "Slime 1", X: 260, Y: 220}); err != nil {
			return err
		}
		_, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{ID: "slime_02", Name: "Slime 2", X: 330, Y: 220})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	now = time.Date(2026, 11, 26, 14, 43, 0, 0, time.UTC)
	ed.SetHistoryPanelOpen(true)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{Dark: true}))

	app.Screenshot("history-panel-all-dark")

	app.Press("id:history-filter-agent")
	app.Settle()
	app.Screenshot("history-panel-agent-dark")

	app.Press("id:history-filter-all")
	app.Settle()
	app.Press(fmt.Sprintf("id:history-row-%d", manualID))
	app.Settle()
	app.Screenshot("history-panel-preview-dark")

	app.Post(func() {
		_ = ed.SetAppearance(editor.AppearanceLight)
	})
	app.Settle()
	app.Screenshot("history-panel-preview-light")
}
