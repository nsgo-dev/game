package editorcmd_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const macosAgentTurnScene = `version: 3
name: level_01
virtualWidth: 640
virtualHeight: 360
backgroundColor: "#141821"
layers:
  - name: actors
    space: world
    order: 0
    objects:
      - id: floor
        name: Floor
        x: 0
        y: 300
      - id: hero
        name: Hero
        x: 64
        y: 256
      - id: door
        name: Door
        x: 560
        y: 248
`

// TestAgentTurnMacOSScreenshots renders the Agent panel pending summary card (Dark & Light)
// and the Kept state with its Spec Undo toast on the real native macOS host for visual
// verification beside docs/design/editor/Main.dc.html and Spec.dc.html.
func TestAgentTurnMacOSScreenshots(t *testing.T) {
	doc, err := sceneformat.Parse([]byte(macosAgentTurnScene))
	if err != nil {
		t.Fatal(err)
	}
	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.ScenePath = "scenes/level_01.scene.yaml"
	ed.SetActiveLayer("actors")
	ed.SetRightPanelTab(editor.RightPanelAgent)

	if err := ed.StartAgentTurn("Put three slimes patrolling the lower floor and a short coin trail toward the door."); err != nil {
		t.Fatal(err)
	}
	ed.RecordAgentTurnStep("Read scenes/level_01.scene.yaml and prefabs/slime.prefab.yaml")
	ed.RecordAgentTurnStep("Placed 3 slimes and 3 coins on layer actors")
	ed.RecordAgentTurnStep("Ran game check · no problems")
	err = ed.RunAgentSceneEdit("", map[string]string{
		"obj:slime_01": "Patrols left lower floor",
		"obj:slime_02": "Patrols center lower floor",
		"obj:slime_03": "Guards upper plank",
	}, func(target *editor.Editor) error {
		for i, pos := range [][2]float32{{140, 268}, {240, 268}, {340, 224}} {
			id := "slime_0" + string(rune('1'+i))
			if _, err := target.PlaceObject("actors", sceneformat.ObjectDoc{
				ID: id, Name: id, X: pos[0], Y: pos[1],
				Properties: map[string]any{"behavior": "patrol"},
			}); err != nil {
				return err
			}
		}
		for i, pos := range [][2]float32{{410, 220}, {445, 220}, {480, 220}} {
			id := "coin_0" + string(rune('1'+i))
			if _, err := target.PlaceObject("actors", sceneformat.ObjectDoc{
				ID: id, Name: id, X: pos[0], Y: pos[1],
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.EndAgentTurn("Done. The slimes walk the floor between x 140 and 340, and the coins lead toward the door."); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))

	app.Screenshot("agent-turn-pending-dark")

	// Revert one change and switch to Light theme to verify partial-keep & Spec light tokens.
	app.Post(func() {
		snap, ok := ed.CurrentAgentTurn()
		if ok && len(snap.Changes) > 0 {
			_ = ed.RevertAgentTurnChange(snap.Changes[0].ID)
		}
	})
	app.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   false,
		Locale: "en-US",
	})
	app.Settle()
	app.Screenshot("agent-turn-partial-light")

	// Restore, switch back to Dark, Keep, and capture Kept state + Spec Undo toast.
	app.Post(func() {
		snap, ok := ed.CurrentAgentTurn()
		if ok && len(snap.Changes) > 0 {
			_ = ed.RestoreAgentTurnChange(snap.Changes[0].ID)
		}
		_ = ed.KeepAgentTurn()
	})
	app.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	})
	app.Settle()
	app.Screenshot("agent-turn-kept-dark")
}
