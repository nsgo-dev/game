package editor_test

import (
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func setupTurnUIEditor(t *testing.T) (*editor.Editor, string) {
	t.Helper()
	ed, dir := openTestProject(t)
	if err := ed.SetAppearance(editor.AppearanceDark); err != nil {
		t.Fatal(err)
	}
	return ed, dir
}

// TestAgentTurnSummaryCardUIAndShortcuts verifies the Agent panel summary card (Main.dc.html),
// per-change revert/restore, Keep (⌘↵) & Revert (⌘⌫), Kept/Reverted states, Spec toast with Undo,
// and captures real macOS nsgotest screenshots.
func TestAgentTurnSummaryCardUIAndShortcuts(t *testing.T) {
	ed, _ := setupTurnUIEditor(t)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	// Switch to Agent tab via click (and verify Inspector tab starts active by default).
	if !app.Exists("id:editor-inspector") {
		t.Fatal("expected Inspector tab active by default")
	}
	app.Press("id:right-tab-agent")
	app.Settle()
	if !app.Exists("id:editor-agent-panel") {
		t.Fatal("Agent panel did not open on Agent tab press")
	}

	// Simulate an agent turn matching Main.dc.html ("6 changes · 1 file"):
	// 3 slimes + 3 coins added, plus progress steps & completion message.
	app.Post(func() {
		if err := ed.StartAgentTurn("Put three slimes patrolling the lower floor and a short coin trail toward the door."); err != nil {
			t.Error(err)
			return
		}
		ed.RecordAgentTurnStep("Read scenes/room.yaml and prefabs/slime.prefab.yaml")
		ed.RecordAgentTurnStep("Placed 3 slimes and 3 coins on layer main")
		ed.RecordAgentTurnStep("Ran game check · no problems")
		err := ed.RunAgentSceneEdit("", map[string]string{
			"obj:slime_01": "Patrols left lower floor",
			"obj:slime_02": "Patrols center lower floor",
			"obj:slime_03": "Guards upper plank",
		}, func(target *editor.Editor) error {
			layer := target.ActiveLayer
			for i, pos := range [][2]float32{{120, 140}, {180, 140}, {240, 96}} {
				id := "slime_0" + string(rune('1'+i))
				if _, err := target.PlaceObject(layer, sceneformat.ObjectDoc{
					ID: id, Name: id, X: pos[0], Y: pos[1],
					Properties: map[string]any{"behavior": "patrol"},
				}); err != nil {
					return err
				}
			}
			for i, pos := range [][2]float32{{210, 82}, {235, 82}, {260, 82}} {
				id := "coin_0" + string(rune('1'+i))
				if _, err := target.PlaceObject(layer, sceneformat.ObjectDoc{
					ID: id, Name: id, X: pos[0], Y: pos[1],
				}); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Error(err)
			return
		}
		if err := ed.EndAgentTurn("Done. The slimes walk the floor between x 120 and 240, and the coins lead toward the door."); err != nil {
			t.Error(err)
		}
	})
	app.Settle()

	if !app.Exists("id:agent-turn-card") {
		t.Fatal("summary card did not appear for pending turn")
	}
	cardText := allText(app.Find("id:agent-turn-card"))
	if !strings.Contains(cardText, "6 changes · 1 file") {
		t.Fatalf("summary card header = %q, want '6 changes · 1 file'", cardText)
	}
	if !strings.Contains(cardText, "Keep") || !strings.Contains(cardText, "Review") || !strings.Contains(cardText, "Revert") {
		t.Fatalf("summary card missing Keep/Review/Revert buttons: %q", cardText)
	}
	hintText := allText(app.Find("id:agent-composer-hint"))
	if !strings.Contains(hintText, "Edits stay unsaved until you keep them") {
		t.Fatalf("composer hint = %q", hintText)
	}

	// Capture real macOS screenshot of the pending summary card state.
	app.Screenshot("agent-turn-pending")

	// Revert one change via the per-change button in the summary card.
	app.Press("id:agent-turn-toggle-0")
	app.Settle()
	cardText = allText(app.Find("id:agent-turn-card"))
	if !strings.Contains(cardText, "5 of 6 changes · 1 file") || !strings.Contains(cardText, "Keep 5 of 6") || !strings.Contains(cardText, "Restore") {
		t.Fatalf("after reverting 1 change in card: %q", cardText)
	}

	// Restore that change back via the Restore button.
	app.Press("id:agent-turn-toggle-0")
	app.Settle()
	cardText = allText(app.Find("id:agent-turn-card"))
	if !strings.Contains(cardText, "6 changes · 1 file") {
		t.Fatalf("after restoring change in card: %q", cardText)
	}

	// Keep via the Keep button -> Kept state + Spec toast with Undo!
	app.Press("id:agent-turn-keep")
	app.Settle()
	if app.Exists("id:agent-turn-card") {
		t.Fatal("summary card should close after Keep")
	}
	if !app.Exists("id:agent-turn-kept") {
		t.Fatal("expected Kept status text after Keep")
	}
	if got := allText(app.Find("id:agent-turn-kept")); !strings.Contains(got, "one undo step") {
		t.Fatalf("kept status = %q, want 'one undo step'", got)
	}
	if !app.Exists("id:editor-toast") || !app.Exists("id:editor-toast-action") {
		t.Fatal("expected Spec toast with Undo action after Keep")
	}

	app.Screenshot("agent-turn-kept")

	// Click toast Undo -> undoes the entire 6-object kept turn in ONE step!
	app.Press("id:editor-toast-action")
	app.Settle()
	if obj, _, _ := ed.FindObject("slime_01"); obj != nil {
		t.Fatal("toast Undo after Keep did not undo slime_01")
	}
	if obj, _, _ := ed.FindObject("coin_03"); obj != nil {
		t.Fatal("toast Undo after Keep did not undo coin_03")
	}
}

// TestAgentTurnPendingPromptsGuardNavigation verifies that opening another scene or another project
// with a pending turn asks to Keep, Revert, or Cancel (as unsaved edits do in #906), and verifies
// ⌘↵ keeps, ⌘⌫ reverts, and ⌘. stops a working turn.
func TestAgentTurnPendingPromptsGuardNavigation(t *testing.T) {
	ed, dir := setupTurnUIEditor(t)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1200, 760), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	// Start a turn, make an edit, and stop it with ⌘. (CmdStopAgentTurn).
	app.Post(func() {
		if err := ed.StartAgentTurn("Add guard"); err != nil {
			t.Error(err)
			return
		}
		if err := ed.RunAgentSceneEdit("", nil, func(target *editor.Editor) error {
			_, err := target.PlaceObject(target.ActiveLayer, sceneformat.ObjectDoc{ID: "guard_01", X: 50, Y: 50})
			return err
		}); err != nil {
			t.Error(err)
		}
	})
	app.Settle()

	app.Key("mod+.")
	app.Settle()
	snap, ok := ed.CurrentAgentTurn()
	if !ok || snap.Status != editor.AgentTurnStopped {
		t.Fatalf("after mod+.: status=%v (ok=%v), want stopped", snap.Status, ok)
	}

	// Opening another scene from the file browser with a pending turn must prompt Keep / Revert / Cancel!
	app.DoublePress("id:files-row-scenes/other.scene.yaml")
	app.Settle()
	if !ed.PendingTurnPromptOpen() || !app.Exists("id:pending-turn-dialog") {
		t.Fatal("opening another scene with a pending turn did not show pending-turn-dialog")
	}
	// Cancel keeps us on room.yaml with the turn still pending.
	app.Press("id:pending-turn-cancel")
	app.Settle()
	if ed.PendingTurnPromptOpen() || ed.ScenePath != "scenes/room.yaml" || !ed.HasPendingAgentTurn() {
		t.Fatalf("after Cancel: prompt=%v scene=%q pending=%v", ed.PendingTurnPromptOpen(), ed.ScenePath, ed.HasPendingAgentTurn())
	}

	// Opening another project with a pending turn also prompts Keep / Revert / Cancel.
	app.Post(func() {
		_ = ed.RequestOpenProject(dir)
	})
	app.Settle()
	if !ed.PendingTurnPromptOpen() || !app.Exists("id:pending-turn-dialog") {
		t.Fatal("opening another project with a pending turn did not show pending-turn-dialog")
	}
	app.Press("id:pending-turn-cancel")
	app.Settle()

	// Test Checkpoints.dc.html rule: in a review, ⌘Z undoes person's edits inside the review first, then reverts the review.
	app.Post(func() {
		ed.SetRightPanelTab(editor.RightPanelAgent)
		ed.SetAgentTurnReviewOpen(true)
		if err := ed.MoveObject("hero", 99, 88); err != nil {
			t.Error(err)
		}
	})
	app.Settle()

	// First Undo undoes person's hero move inside the review.
	app.Post(func() { ed.Undo() })
	app.Settle()
	if hero, _, _ := ed.FindObject("hero"); hero == nil || hero.X == 99 {
		t.Fatalf("first Undo in review did not undo person edit: %+v", hero)
	}
	if !ed.HasPendingAgentTurn() {
		t.Fatal("first Undo in review should leave the review open")
	}

	// Second Undo (no person review edits left) reverts the review and shows Revert toast!
	app.Post(func() { ed.Undo() })
	app.Settle()
	if ed.HasPendingAgentTurn() {
		t.Fatal("second Undo in review should have reverted the review")
	}
	if !app.Exists("id:agent-turn-reverted") || !app.Exists("id:editor-toast-action") {
		t.Fatal("expected Reverted state and Undo toast after reverting review")
	}

	// Restore via toast Undo, then verify ⌘↵ keeps the open turn!
	app.Press("id:editor-toast-action")
	app.Settle()
	if !ed.HasPendingAgentTurn() {
		t.Fatal("toast Undo did not restore pending turn")
	}
	app.Key("mod+enter")
	app.Settle()
	if snap, _ := ed.CurrentAgentTurn(); snap.Status != editor.AgentTurnKept {
		t.Fatalf("after mod+enter: status=%s, want kept", snap.Status)
	}

	// Start another turn and verify ⌘⌫ reverts it!
	app.Post(func() {
		_ = ed.StartAgentTurn("Second request")
		_ = ed.RunAgentSceneEdit("", nil, func(target *editor.Editor) error {
			_, err := target.PlaceObject(target.ActiveLayer, sceneformat.ObjectDoc{ID: "temp_obj", X: 10, Y: 10})
			return err
		})
		_ = ed.EndAgentTurn("Added temp_obj")
	})
	app.Settle()
	app.Key("mod+backspace")
	app.Settle()
	if snap, _ := ed.CurrentAgentTurn(); snap.Status != editor.AgentTurnReverted {
		t.Fatalf("after mod+backspace: status=%s, want reverted", snap.Status)
	}
	if obj, _, _ := ed.FindObject("temp_obj"); obj != nil {
		t.Fatal("temp_obj still present after mod+backspace revert")
	}
}
