package editormcp_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editormcp"
	"github.com/nsgo-dev/game/pkg/sceneformat"
)

// Acceptance test for #1031 over MCP:
//   - Leaves notes on two changes (plus adds and removes a third note before sending),
//   - Verifies neither review_status nor review_notes_get leaks unsent drafts,
//   - Sends notes and verifies via review_notes_get that ONLY the two remaining notes reach the agent,
//   - Refuses review_redo on an un-noted change,
//   - Has the agent redo one noted change through MCP review_redo,
//   - Checks that only the noted change changed in Round 2, Round 1 still shows the first version,
//     notes are never written into any file, and Keep then one Undo restores every file as before.
func TestReviewNotesAndSecondRoundViaMCP(t *testing.T) {
	f := openFixture(t)
	beforeScene, err := os.ReadFile(f.scenePath())
	if err != nil {
		t.Fatal(err)
	}
	beforeItems := []byte("version: 1\nrows:\n  - id: sword\n    damage: 3\n")
	itemsPath := filepath.Join(f.dir, "data", "items.yaml")
	if err := os.WriteFile(itemsPath, beforeItems, 0o644); err != nil {
		t.Fatal(err)
	}

	afterSceneDoc, err := sceneformat.Parse(beforeScene)
	if err != nil {
		t.Fatal(err)
	}
	slime1 := sceneformat.ObjectDoc{ID: "slime_01", Name: "slime_01", X: 160, Y: 278, Z: 11}
	slime3 := sceneformat.ObjectDoc{ID: "slime_03", Name: "slime_03", X: 452, Y: 208, Z: 11}
	afterSceneDoc.Layers[0].Objects = append(afterSceneDoc.Layers[0].Objects, slime1, slime3)
	afterScene, err := sceneformat.Encode(afterSceneDoc)
	if err != nil {
		t.Fatal(err)
	}
	afterItems := []byte("version: 1\nrows:\n  - id: sword\n    damage: 5\n")

	if err := f.ed.StartTurnReview(editor.ReviewTurnInput{
		Request: "Add two slimes and buff the sword",
		Files: []editor.ReviewFileInput{
			{
				Path:       "scenes/room.scene.yaml",
				BeforeYAML: beforeScene,
				AfterYAML:  afterScene,
				Reasons: map[string]string{
					"slime_01": "Floor patrol on left.",
					"slime_03": "Guards the plank.",
				},
			},
			{
				Path:       "data/items.yaml",
				BeforeYAML: beforeItems,
				AfterYAML:  afterItems,
				Reasons: map[string]string{
					"sword": "Matches the new enemies.",
				},
			},
		},
	}); err != nil {
		t.Fatalf("StartTurnReview: %v", err)
	}

	snap0 := f.ed.ReviewStatus()
	var s1ID, s3ID, swordID string
	for _, ch := range snap0.Changes {
		switch ch.Title {
		case "slime_01":
			s1ID = ch.ID
		case "slime_03":
			s3ID = ch.ID
		case "sword":
			swordID = ch.ID
		}
	}
	if s1ID == "" || s3ID == "" || swordID == "" {
		t.Fatalf("missing expected change IDs: s1=%q s3=%q sword=%q", s1ID, s3ID, swordID)
	}
	f.ed.FocusReviewChange(s3ID)

	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// Add a note on s1 and remove it before sending; add notes on s3 and sword.
	if err := f.ed.SetReviewNote(s1ID, "Temporary note that gets removed"); err != nil {
		t.Fatal(err)
	}
	if err := f.ed.RemoveReviewNote(s1ID); err != nil {
		t.Fatal(err)
	}
	if err := f.ed.SetReviewNote(s3ID, "Move it about 40 px left."); err != nil {
		t.Fatal(err)
	}
	if err := f.ed.SetReviewNote(swordID, "Set damage to 4 instead."); err != nil {
		t.Fatal(err)
	}

	// Before sending, NEITHER review_notes_get NOR review_status may leak unsent draft notes!
	resBeforeSend := call(t, cs, editormcp.ToolReviewNotesGet, map[string]any{})
	mustOK(t, resBeforeSend)
	var statusBefore editormcp.ReviewNotesStatusResult
	structured(t, resBeforeSend, &statusBefore)
	if len(statusBefore.SentNotes) != 0 {
		t.Fatalf("unsent notes leaked via review_notes_get before Send: %+v", statusBefore.SentNotes)
	}
	if strings.Contains(text(resBeforeSend), "Move it about 40 px left") {
		t.Fatalf("unsent note leaked in review_notes_get text: %s", text(resBeforeSend))
	}

	resStatusBefore := call(t, cs, editormcp.ToolReviewStatus, map[string]any{})
	mustOK(t, resStatusBefore)
	var rawSnapBefore editor.ReviewSnapshot
	structured(t, resStatusBefore, &rawSnapBefore)
	for _, ch := range rawSnapBefore.Changes {
		if ch.Note != "" {
			t.Fatalf("unsent draft note leaked via review_status on %s: %q", ch.ID, ch.Note)
		}
	}

	// Send the notes.
	if _, err := f.ed.SendReviewNotes(); err != nil {
		t.Fatalf("SendReviewNotes: %v", err)
	}

	resAfterSend := call(t, cs, editormcp.ToolReviewNotesGet, map[string]any{})
	mustOK(t, resAfterSend)
	var statusAfter editormcp.ReviewNotesStatusResult
	structured(t, resAfterSend, &statusAfter)
	if len(statusAfter.SentNotes) != 2 {
		t.Fatalf("sent notes = %+v, want 2 (s3 and sword; removed s1 must never arrive)", statusAfter.SentNotes)
	}
	for _, n := range statusAfter.SentNotes {
		if n.ChangeID == s1ID || strings.Contains(n.Note, "Temporary") {
			t.Fatalf("removed note reached agent: %+v", n)
		}
	}
	if statusAfter.SentNotes[0].ChangeID != s3ID || statusAfter.SentNotes[0].File != "scenes/room.scene.yaml" || statusAfter.SentNotes[0].Object != "slime_03" {
		t.Fatalf("note[0] = %+v", statusAfter.SentNotes[0])
	}
	if statusAfter.SentNotes[1].ChangeID != swordID || statusAfter.SentNotes[1].File != "data/items.yaml" || statusAfter.SentNotes[1].Field != "damage" {
		t.Fatalf("note[1] = %+v", statusAfter.SentNotes[1])
	}

	// Trying to redo un-noted change s1 through MCP must fail.
	badRes := call(t, cs, editormcp.ToolReviewRedo, map[string]any{
		"changes": []any{
			map[string]any{"change_id": s1ID, "x": 120},
		},
	})
	if !badRes.IsError || !strings.Contains(text(badRes), "only changes you left notes on") {
		t.Fatalf("expected error redoing un-noted change, got isErr=%v text=%q", badRes.IsError, text(badRes))
	}

	// Redo ONLY s3 through MCP review_redo.
	redoRes := call(t, cs, editormcp.ToolReviewRedo, map[string]any{
		"changes": []any{
			map[string]any{
				"change_id": s3ID,
				"detail":    "patrols the plank x 400–450",
				"x":         412,
			},
		},
	})
	mustOK(t, redoRes)
	var redoOut editormcp.ReviewRedoResult
	structured(t, redoRes, &redoOut)
	if redoOut.Round != 2 || len(redoOut.RedoneIDs) != 1 || redoOut.RedoneIDs[0] != s3ID {
		t.Fatalf("redoOut = %+v", redoOut)
	}

	// Check Round 1 vs Round 2 in Editor.
	snap := f.ed.ReviewStatus()
	if snap.Round != 2 || snap.ViewRound != 2 {
		t.Fatalf("snap round/viewRound = %d/%d", snap.Round, snap.ViewRound)
	}
	for _, ch := range snap.Changes {
		switch ch.ID {
		case s3ID:
			if ch.Seen {
				t.Fatal("redone s3 should be unseen again")
			}
			if ch.VersionFor(1).X != 452 || ch.VersionFor(2).X != 412 {
				t.Fatalf("s3 versions: r1=%v r2=%v", ch.VersionFor(1).X, ch.VersionFor(2).X)
			}
			if ch.Why != "Redone from your note: Move it about 40 px left." {
				t.Fatalf("s3 Round 2 Why = %q", ch.Why)
			}
		case s1ID:
			if ch.VersionFor(1).X != 160 || ch.VersionFor(2).X != 160 || ch.RedoneInRound != 0 {
				t.Fatalf("un-noted s1 changed unexpectedly: %+v", ch)
			}
		case swordID:
			if ch.RedoneInRound != 0 {
				t.Fatalf("non-redone sword changed unexpectedly: %+v", ch)
			}
		}
	}

	// Keep applies Round 2 (latest round), and ONE Undo restores both files to exact pre-request bytes.
	if err := f.ed.KeepTurnReview(); err != nil {
		t.Fatalf("KeepTurnReview: %v", err)
	}
	keptScene, err := os.ReadFile(f.scenePath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(keptScene), "x: 412") {
		t.Fatalf("kept scene missing Round 2 x: 412:\n%s", keptScene)
	}
	if strings.Contains(string(keptScene), "Move it about 40 px left") {
		t.Fatal("note text leaked into saved scene YAML")
	}

	if !f.ed.Undo() {
		t.Fatal("expected single Undo to succeed")
	}
	restoredScene, err := os.ReadFile(f.scenePath())
	if err != nil {
		t.Fatal(err)
	}
	restoredItems, err := os.ReadFile(filepath.Join(f.dir, "data", "items.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restoredScene, beforeScene) {
		t.Fatalf("scene after 1 Undo differs:\ngot:\n%s\nwant:\n%s", restoredScene, beforeScene)
	}
	if !bytes.Equal(restoredItems, beforeItems) {
		t.Fatalf("items.yaml after 1 Undo differs:\ngot:\n%s\nwant:\n%s", restoredItems, beforeItems)
	}
}

func TestReviewNotesAndRedoPrivacyAndPermissions(t *testing.T) {
	f := openFixture(t)
	beforeScene, err := os.ReadFile(f.scenePath())
	if err != nil {
		t.Fatal(err)
	}
	afterSceneDoc, err := sceneformat.Parse(beforeScene)
	if err != nil {
		t.Fatal(err)
	}
	afterSceneDoc.Layers[0].Objects = append(afterSceneDoc.Layers[0].Objects, sceneformat.ObjectDoc{
		ID: "slime_03", Name: "slime_03", X: 452, Y: 208,
	})
	afterScene, err := sceneformat.Encode(afterSceneDoc)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.ed.StartTurnReview(editor.ReviewTurnInput{
		Request: "Add slime and gameplay code",
		Files: []editor.ReviewFileInput{
			{Path: "scenes/room.scene.yaml", BeforeYAML: beforeScene, AfterYAML: afterScene},
			{Path: "gameplay/patrol.go", BeforeYAML: []byte("package gameplay\n// old\n"), AfterYAML: []byte("package gameplay\n// new\n")},
		},
	}); err != nil {
		t.Fatal(err)
	}

	snap := f.ed.ReviewStatus()
	var s3ID, goID string
	for _, ch := range snap.Changes {
		if ch.ObjectID == "slime_03" {
			s3ID = ch.ID
		}
		if ch.File == "gameplay/patrol.go" {
			goID = ch.ID
		}
	}
	if err := f.ed.SetReviewNote(s3ID, "Move left"); err != nil {
		t.Fatal(err)
	}
	if err := f.ed.SetReviewNote(goID, "Adjust turn margin"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ed.SendReviewNotes(); err != nil {
		t.Fatal(err)
	}

	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// 1. Turning off GoSource privacy redacts .go diff lines from review_status and refuses review_redo on .go files.
	if err := f.ed.SetAgentPrivacy(editor.PrivacyGoSource, false); err != nil {
		t.Fatal(err)
	}
	stRes := call(t, cs, editormcp.ToolReviewStatus, map[string]any{})
	mustOK(t, stRes)
	var stOut editor.ReviewSnapshot
	structured(t, stRes, &stOut)
	for _, ch := range stOut.Changes {
		if ch.File == "gameplay/patrol.go" && len(ch.YAMLLines) > 0 {
			t.Fatalf("GoSource privacy off but review_status leaked Go diff lines: %+v", ch.YAMLLines)
		}
	}
	goRedoRes := call(t, cs, editormcp.ToolReviewRedo, map[string]any{
		"changes": []any{map[string]any{"change_id": goID, "file_content": "package gameplay\n// redone\n"}},
	})
	if !goRedoRes.IsError {
		t.Fatalf("expected review_redo on .go file to be refused when GoSource privacy is off")
	}

	// 2. Turning off SceneAndData privacy refuses review_status, review_notes_get, and review_redo.
	if err := f.ed.SetAgentPrivacy(editor.PrivacySceneAndData, false); err != nil {
		t.Fatal(err)
	}
	for _, toolName := range []string{editormcp.ToolReviewStatus, editormcp.ToolReviewNotesGet} {
		r := call(t, cs, toolName, map[string]any{})
		if !r.IsError {
			t.Fatalf("expected %s to be refused when SceneAndData privacy is off", toolName)
		}
	}
	redoBlocked := call(t, cs, editormcp.ToolReviewRedo, map[string]any{
		"changes": []any{map[string]any{"change_id": s3ID, "x": 412}},
	})
	if !redoBlocked.IsError {
		t.Fatalf("expected review_redo to be refused when SceneAndData privacy is off")
	}
}
