package editor_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
	openFileBrowserInUI(app, ed)
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

// TestAgentTurnHardeningSecurityLifecycleAndDiffs verifies symlink containment, .git and You-only
// refusal, payload size cap, History preview guards, new-file undo deletion, long game.yaml diffing,
// staged-to-tab handoff, cross-tab active-scene safety, OpenDemo lifecycle reset, and zh localization.
func TestAgentTurnHardeningSecurityLifecycleAndDiffs(t *testing.T) {
	ed, dir := openTestProject(t)

	// 1. HistoryPreviewActive() must block starting a turn.
	if err := ed.MoveObject("hero", 70, 50); err != nil {
		t.Fatal(err)
	}
	entries := ed.HistoryEntries()
	if len(entries) == 0 {
		t.Fatal("expected history entry")
	}
	if err := ed.PreviewHistoryEntry(entries[0].ID); err != nil {
		t.Fatalf("PreviewHistoryEntry: %v", err)
	}
	if err := ed.StartAgentTurn("Blocked during preview"); !errors.Is(err, editor.ErrHistoryPreview) {
		t.Fatalf("StartAgentTurn during history preview = %v, want ErrHistoryPreview", err)
	}
	ed.ExitHistoryPreview()

	// 2. Security: symlink escape, .git/**, You-only permissions.yaml, and >2 MiB payload refused.
	if err := ed.StartAgentTurn("Security and diff hardening"); err != nil {
		t.Fatal(err)
	}
	if err := ed.RecordAgentFileEdit(".git/config", []byte("core:\n  bare: false\n"), ""); err == nil {
		t.Fatal("expected .git/config edit to be refused")
	}
	if err := ed.RecordAgentFileEdit("permissions.yaml", []byte("version: 1\nprofile: autonomous\n"), ""); err == nil {
		t.Fatal("expected You-only permissions.yaml edit to be refused")
	}
	if err := ed.RecordAgentFileEdit("data/huge.yaml", bytes.Repeat([]byte("a"), 2<<20+16), ""); err == nil {
		t.Fatal("expected >2 MiB payload to be refused")
	}
	if runtime.GOOS != "windows" {
		outside := t.TempDir()
		outsideFile := filepath.Join(outside, "escape.yaml")
		if err := os.WriteFile(outsideFile, []byte("version: 1\nrows: []\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "data"), 0o755); err != nil {
			t.Fatal(err)
		}
		linkFile := filepath.Join(dir, "data", "escape.yaml")
		if err := os.Symlink(outsideFile, linkFile); err != nil {
			t.Fatal(err)
		}
		if err := ed.RecordAgentFileEdit("data/escape.yaml", []byte("version: 1\nrows: []\n"), ""); err == nil {
			t.Fatal("expected symlink escaping project root to be refused")
		}
		linkDir := filepath.Join(dir, "escape_dir")
		if err := os.Symlink(outside, linkDir); err != nil {
			t.Fatal(err)
		}
		if err := ed.RecordAgentFileEdit("escape_dir/new.yaml", []byte("version: 1\nrows: []\n"), ""); err == nil {
			t.Fatal("expected new file under symlinked escaping dir to be refused")
		}
	}

	// 3. Long game.yaml value (>64 bytes in autoload.scenes) must NOT be truncated before equality comparison.
	gameYAML, err := os.ReadFile(filepath.Join(dir, "game.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	thirdScene := filepath.Join(dir, "scenes", "bonus.scene.yaml")
	if err := os.WriteFile(thirdScene, []byte("version: 2\nname: bonus\nlayers:\n  - name: actors\n    objects: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	editedGame := strings.TrimSpace(string(gameYAML)) + "\n    - name: bonus\n      path: scenes/bonus.scene.yaml\n"
	if err := ed.RecordAgentFileEdit("game.yaml", []byte(editedGame), "Register bonus scene"); err != nil {
		t.Fatalf("RecordAgentFileEdit(game.yaml): %v", err)
	}

	// 4. Create a brand-new file data/loot.yaml in memory, keep, save, undo, save -> must delete file (not write 0 bytes).
	lootPath := filepath.Join(dir, "data", "loot.yaml")
	lootYAML := "version: 1\nrows:\n  - id: gem\n    value: 10\n"
	if err := ed.RecordAgentFileEdit("data/loot.yaml", []byte(lootYAML), "Add loot table"); err != nil {
		t.Fatalf("RecordAgentFileEdit(data/loot.yaml): %v", err)
	}
	if err := ed.EndAgentTurn("Added bonus scene and loot table"); err != nil {
		t.Fatal(err)
	}
	snap, ok := ed.CurrentAgentTurn()
	if !ok {
		t.Fatal("missing turn snapshot")
	}
	foundScenesKey := false
	for _, ch := range snap.Changes {
		if ch.File == "game.yaml" && ch.Field == "autoload.scenes" {
			foundScenesKey = true
		}
	}
	if !foundScenesKey {
		t.Fatalf("long autoload.scenes change past 64 bytes was missed: %+v", snap.Changes)
	}

	if err := ed.KeepAgentTurn(); err != nil {
		t.Fatalf("KeepAgentTurn: %v", err)
	}
	if err := ed.SaveAll(); err != nil {
		t.Fatalf("SaveAll after Keep: %v", err)
	}
	if _, err := os.Stat(lootPath); err != nil {
		t.Fatalf("expected data/loot.yaml on disk after Keep+SaveAll: %v", err)
	}
	if !ed.Undo() {
		t.Fatal("Undo failed")
	}
	if _, staged := ed.StagedFileYAML("data/loot.yaml"); staged {
		t.Fatal("StagedFileYAML should report false for undone newly-created file")
	}
	if err := ed.SaveAll(); err != nil {
		t.Fatalf("SaveAll after Undo: %v", err)
	}
	if _, err := os.Stat(lootPath); !os.IsNotExist(err) {
		t.Fatalf("expected data/loot.yaml to be removed on disk after Undo+SaveAll, got err=%v", err)
	}
	if !ed.Redo() {
		t.Fatal("Redo failed")
	}
	if err := ed.SaveAll(); err != nil {
		t.Fatalf("SaveAll after Redo: %v", err)
	}
	if got, err := os.ReadFile(lootPath); err != nil || !strings.Contains(string(got), "gem") {
		t.Fatalf("expected data/loot.yaml restored on disk after Redo+SaveAll, err=%v got=%q", err, got)
	}

	// 5. Opening a kept-staged scene in a tab must clear the staged entry so later tab edits+SaveAll aren't clobbered.
	if err := ed.StartAgentTurn("Edit other scene"); err != nil {
		t.Fatal(err)
	}
	if err := ed.RunAgentSceneEdit("scenes/other.scene.yaml", nil, func(target *editor.Editor) error {
		_, err := target.PlaceObject(target.ActiveLayer, sceneformat.ObjectDoc{ID: "staged_slime", X: 10, Y: 20})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := ed.EndAgentTurn("Added staged_slime"); err != nil {
		t.Fatal(err)
	}
	if err := ed.KeepAgentTurn(); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenScene("scenes/other.scene.yaml"); err != nil {
		t.Fatalf("OpenScene(other): %v", err)
	}
	if _, err := ed.PlaceObject(ed.ActiveLayer, sceneformat.ObjectDoc{ID: "manual_coin", X: 30, Y: 40}); err != nil {
		t.Fatal(err)
	}
	if err := ed.SaveAll(); err != nil {
		t.Fatalf("SaveAll after tab edit: %v", err)
	}
	otherDisk, err := os.ReadFile(filepath.Join(dir, "scenes", "other.scene.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(otherDisk), "manual_coin") || !strings.Contains(string(otherDisk), "staged_slime") {
		t.Fatalf("staged entry clobbered open tab save:\n%s", otherDisk)
	}

	// 6. Switching tabs while a turn is pending must NOT apply Scene A's delta onto Scene B on Revert.
	if err := ed.SelectTab(0); err != nil {
		t.Fatal(err)
	}
	if err := ed.StartAgentTurn("Touch room only"); err != nil {
		t.Fatal(err)
	}
	if err := ed.RunAgentSceneEdit("", nil, func(target *editor.Editor) error {
		_, err := target.PlaceObject(target.ActiveLayer, sceneformat.ObjectDoc{ID: "room_temp", X: 5, Y: 5})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := ed.EndAgentTurn("Added room_temp"); err != nil {
		t.Fatal(err)
	}
	// Programmatically switch to tab 1 (other.scene.yaml) then revert the turn!
	if err := ed.SelectTab(1); err != nil {
		t.Fatal(err)
	}
	if err := ed.RevertAgentTurn(); err != nil {
		t.Fatal(err)
	}
	// Tab 1 (other.scene.yaml) must still have manual_coin intact!
	if obj, _, _ := ed.FindObject("manual_coin"); obj == nil {
		t.Fatal("reverting turn while on another tab corrupted tab 1")
	}
	// Tab 0 (room.yaml) must have room_temp removed!
	if err := ed.SelectTab(0); err != nil {
		t.Fatal(err)
	}
	if obj, _, _ := ed.FindObject("room_temp"); obj != nil {
		t.Fatal("room_temp was not reverted on tab 0")
	}

	// 7. OpenDemo() must completely reset agentTurn, lastRevertedTurn, stagedFiles, and pendingTurnPrompt.
	if err := ed.StartAgentTurn("Leave staged before OpenDemo"); err != nil {
		t.Fatal(err)
	}
	if err := ed.RecordAgentFileEdit("data/leaked.yaml", []byte("version: 1\nrows:\n  - id: leak\n"), ""); err != nil {
		t.Fatal(err)
	}
	if err := ed.EndAgentTurn("Pending before demo"); err != nil {
		t.Fatal(err)
	}
	if err := ed.KeepAgentTurn(); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenDemo(); err != nil {
		t.Fatalf("OpenDemo: %v", err)
	}
	if _, has := ed.CurrentAgentTurn(); has {
		t.Fatal("OpenDemo did not clear agentTurn")
	}
	if ed.HasUnsavedChanges() {
		t.Fatal("OpenDemo leaked dirty stagedFiles into HasUnsavedChanges")
	}
	if _, staged := ed.StagedFileYAML("data/leaked.yaml"); staged {
		t.Fatal("OpenDemo leaked stagedFiles")
	}
	if err := ed.SaveAll(); err != nil {
		t.Fatalf("SaveAll in Demo: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "leaked.yaml")); !os.IsNotExist(err) {
		t.Fatalf("OpenDemo + SaveAll wrote leaked file into previous project: err=%v", err)
	}
}

// TestAgentTurnLocalizationSwitch verifies live en/zh switching updates the summary card cleanly.
func TestAgentTurnLocalizationSwitch(t *testing.T) {
	ed, _ := setupTurnUIEditor(t)
	ed.SetRightPanelTab(editor.RightPanelAgent)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800))

	app.Post(func() {
		_ = ed.StartAgentTurn("Add slime")
		_ = ed.RunAgentSceneEdit("", map[string]string{"obj:slime_zh": "Guard exit"}, func(target *editor.Editor) error {
			_, err := target.PlaceObject(target.ActiveLayer, sceneformat.ObjectDoc{ID: "slime_zh", X: 10, Y: 20})
			return err
		})
		_ = ed.EndAgentTurn("Done")
	})
	app.Settle()

	if err := func() error {
		app.Post(func() { _ = ed.SetLanguage(editor.LanguageChinese) })
		app.Settle()
		return nil
	}(); err != nil {
		t.Fatal(err)
	}
	cardText := allText(app.Find("id:agent-turn-card"))
	if !strings.Contains(cardText, "保留") || !strings.Contains(cardText, "还原") || !strings.Contains(cardText, "原因：Guard exit") {
		t.Fatalf("Chinese summary card missing localized labels: %q", cardText)
	}
	hintText := allText(app.Find("id:agent-composer-hint"))
	if !strings.Contains(hintText, "在你保留之前") {
		t.Fatalf("Chinese composer hint not localized: %q", hintText)
	}
}
