package editormcp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
)

func TestReviewStatusReportsSeenAndRevertedChanges(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// Before any review opens, review_status reports inactive.
	res := call(t, cs, editormcp.ToolReviewStatus, nil)
	out := mustOK(t, res)
	if !strings.Contains(out, "No agent turn review is active") {
		t.Fatalf("inactive review_status = %q", out)
	}

	beforeScene := []byte(`version: 2
name: level_01
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    objects:
      - id: knight
        x: 120
        "y": 200
        z: 10
      - id: crate_04
        x: 320
        "y": 240
`)
	afterScene := []byte(`version: 2
name: level_01
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    objects:
      - id: knight
        x: 120
        "y": 200
        z: 12
      - id: slime_01
        prefab: prefabs/slime.prefab.yaml
        x: 160
        "y": 278
      - id: slime_02
        prefab: prefabs/slime.prefab.yaml
        x: 240
        "y": 278
`)

	if err := f.ed.StartTurnReview(editor.ReviewTurnInput{
		Request: "Put slime patrols on level_01",
		Files: []editor.ReviewFileInput{
			{
				Path:       "scenes/level_01.scene.yaml",
				BeforeYAML: beforeScene,
				AfterYAML:  afterScene,
				Reasons: map[string]string{
					"slime_01": "Guards the left floor span.",
					"slime_02": "Guards the right floor span.",
					"knight":   "Drawn above slimes.",
					"crate_04": "Blocked the trail.",
				},
			},
		},
	}); err != nil {
		t.Fatalf("StartTurnReview: %v", err)
	}

	// Step once to mark the second change seen, and revert it.
	secondID := f.ed.StepReviewChange(+1)
	if secondID == "" {
		t.Fatal("StepReviewChange(+1) returned empty ID")
	}
	f.ed.ToggleReviewChangeReverted(secondID)

	res = call(t, cs, editormcp.ToolReviewStatus, nil)
	out = mustOK(t, res)
	var snap editor.ReviewSnapshot
	structured(t, res, &snap)

	if !snap.Active || !snap.Open || snap.TotalChanges != 4 {
		t.Fatalf("snap = %+v", snap)
	}
	if snap.SeenCount != 2 || snap.RevertedCount != 1 || snap.KeptCount != 3 {
		t.Fatalf("seen=%d reverted=%d kept=%d, want 2/1/3", snap.SeenCount, snap.RevertedCount, snap.KeptCount)
	}
	foundReverted := false
	for _, ch := range snap.Changes {
		if ch.ID == secondID {
			if !ch.Seen || !ch.Reverted {
				t.Fatalf("second change = %+v, want seen=true reverted=true", ch)
			}
			foundReverted = true
		}
	}
	if !foundReverted {
		t.Fatalf("second change %q not found in review_status changes", secondID)
	}
	if !strings.Contains(out, "2 seen") || !strings.Contains(out, "1 reverted") {
		t.Fatalf("review_status text missing seen/reverted summary:\n%s", out)
	}

	// Pause with Esc and verify review_status reflects pending (not full-window open).
	f.ed.PauseReview()
	res = call(t, cs, editormcp.ToolReviewStatus, nil)
	mustOK(t, res)
	structured(t, res, &snap)
	if !snap.Active || snap.Open || snap.SeenCount != 2 || snap.RevertedCount != 1 {
		t.Fatalf("paused snap = %+v", snap)
	}
}

// #1032 Acceptance 2: type invalid YAML in the review pane, check that Keep and Done are refused,
// apply the offered fix, and find the hand edit tagged as the person's ("you") in the turn read through MCP.
func TestReviewStatusExposesHandEditTaggedYouAndConflictChoice(t *testing.T) {
	f := openFixture(t)

	baseYAML := `version: 3
name: level_01
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    space: world
    order: 0
    objects:
      - id: door_1
        x: 574
        y: 248
        properties:
          on_open:
            - open: true
`
	agentYAML := `version: 3
name: level_01
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    space: world
    order: 0
    objects:
      - id: door_1
        x: 574
        y: 248
        properties:
          on_open:
            - open: true
            - reveal: coin_trail
      - id: slime_03
        prefab: prefabs/slime.prefab.yaml
        x: 452
        y: 208
`
	for rel, content := range map[string]string{
		"scenes/level_01.scene.yaml": baseYAML,
		"prefabs/slime.prefab.yaml":  "version: 1\nid: slime\nname: Slime\n",
	} {
		full := filepath.Join(f.dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.ed.OpenScene("scenes/level_01.scene.yaml"); err != nil {
		t.Fatalf("OpenScene: %v", err)
	}

	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	if err := f.ed.StartTurnReview(editor.ReviewTurnInput{
		Request: "Add slime_03 and reveal coin_trail on door_1",
		Files: []editor.ReviewFileInput{
			{
				Path:       "scenes/level_01.scene.yaml",
				BeforeYAML: []byte(baseYAML),
				AfterYAML:  []byte(agentYAML),
			},
		},
	}); err != nil {
		t.Fatalf("StartTurnReview: %v", err)
	}

	// Person also edits door_1's on_open list concurrently.
	if err := f.ed.SetProperty("door_1", "on_open", []any{
		map[string]any{"open": true},
		map[string]any{"sound": "sfx/door_creak.ogg"},
	}); err != nil {
		t.Fatal(err)
	}

	// Person starts editing slime_03's YAML and types invalid YAML ("x 444").
	if err := f.ed.BeginReviewYAMLEdit("slime_03"); err != nil {
		t.Fatal(err)
	}
	var slimeYAML string
	for _, ch := range f.ed.ReviewStatus().Changes {
		if ch.ObjectID == "slime_03" {
			slimeYAML = ch.HandEditYAML
		}
	}
	broken := strings.Replace(slimeYAML, "x: 452", "x 444", 1)
	if err := f.ed.SetReviewYAMLDraft("slime_03", broken); err == nil {
		t.Fatal("expected invalid YAML parse error")
	}

	// Keep and Done are refused while invalid.
	if err := f.ed.KeepTurnReview(); err == nil {
		t.Fatal("expected KeepTurnReview refused while YAML is broken")
	}
	if err := f.ed.FinishReviewYAMLEdit("slime_03"); err == nil {
		t.Fatal("expected FinishReviewYAMLEdit (Done) refused while YAML is broken")
	}

	// MCP reports the active YAML error blocker.
	res := call(t, cs, editormcp.ToolReviewStatus, map[string]any{})
	mustOK(t, res)
	var snap editor.ReviewSnapshot
	structured(t, res, &snap)
	if len(snap.Blockers) == 0 || snap.Blockers[0].Kind != "yaml_error" {
		t.Fatalf("expected yaml_error blocker in MCP review_status, got %+v", snap.Blockers)
	}

	// Apply the offered fix ("Add the colon"), finish YAML edit, and resolve door_1 conflict with Both.
	if err := f.ed.ApplyReviewYAMLFix("slime_03"); err != nil {
		t.Fatalf("ApplyReviewYAMLFix: %v", err)
	}
	if err := f.ed.FinishReviewYAMLEdit("slime_03"); err != nil {
		t.Fatalf("FinishReviewYAMLEdit: %v", err)
	}
	if err := f.ed.ResolveReviewConflict("door_1", editor.ReviewConflictBoth); err != nil {
		t.Fatalf("ResolveReviewConflict: %v", err)
	}

	// Read the turn through MCP: slime_03 hand edit is tagged "you", door_1 conflict choice is "both".
	res = call(t, cs, editormcp.ToolReviewStatus, map[string]any{})
	out := mustOK(t, res)
	snap = editor.ReviewSnapshot{}
	structured(t, res, &snap)
	if len(snap.Blockers) != 0 {
		t.Fatalf("expected no blockers after fix and conflict resolution, got %+v", snap.Blockers)
	}
	if !strings.Contains(out, "hand-edited by you") {
		t.Fatalf("expected MCP text to mention hand-edited by you:\n%s", out)
	}

	var gotSlime, gotDoor *editor.ReviewChange
	for i := range snap.Changes {
		switch snap.Changes[i].ObjectID {
		case "slime_03":
			gotSlime = &snap.Changes[i]
		case "door_1":
			gotDoor = &snap.Changes[i]
		}
	}
	if gotSlime == nil || !gotSlime.HandEdited || gotSlime.HandEditBy != editor.ReviewAuthorYou || gotSlime.Author != editor.ReviewAuthorYou {
		t.Fatalf("slime_03 MCP report missing 'you' tag: %+v", gotSlime)
	}
	foundXLine := false
	for _, ln := range gotSlime.HandEditLines {
		if strings.Contains(ln.Text, "x: 444") && ln.Author == editor.ReviewAuthorYou {
			foundXLine = true
		}
	}
	if !foundXLine {
		t.Fatalf("expected x: 444 tagged 'you' in HandEditLines: %+v", gotSlime.HandEditLines)
	}
	if gotDoor == nil || gotDoor.ConflictChoice != editor.ReviewConflictBoth {
		t.Fatalf("door_1 MCP report = %+v, want conflictChoice=both", gotDoor)
	}

	// After Keep, the agent can still read the kept turn's hand edit and merge choice in round 2.
	if err := f.ed.KeepTurnReview(); err != nil {
		t.Fatalf("KeepTurnReview: %v", err)
	}
	res = call(t, cs, editormcp.ToolReviewStatus, map[string]any{})
	out = mustOK(t, res)
	snap = editor.ReviewSnapshot{}
	structured(t, res, &snap)
	if snap.Outcome != editor.ReviewOutcomeKept {
		t.Fatalf("outcome = %q, want kept", snap.Outcome)
	}
	if !strings.Contains(out, "hand-edited by you") || !strings.Contains(out, "conflict=both") {
		t.Fatalf("expected kept review_status text to retain hand-edit & conflict choice for round 2:\n%s", out)
	}
	keptSlime := (*editor.ReviewChange)(nil)
	for i := range snap.Changes {
		if snap.Changes[i].ObjectID == "slime_03" {
			keptSlime = &snap.Changes[i]
		}
	}
	if keptSlime == nil || keptSlime.HandEditBy != editor.ReviewAuthorYou {
		t.Fatalf("expected hand edit tagged 'you' to persist in kept turn for round 2: %+v", snap.Changes)
	}
}

func TestReviewStatusRespectsPrivacySwitches(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	if err := f.ed.StartTurnReview(editor.ReviewTurnInput{
		Request: "Add helper in game.go",
		Files: []editor.ReviewFileInput{
			{
				Path:       "game.go",
				BeforeYAML: []byte("package main\n"),
				AfterYAML:  []byte("package main\nfunc helper() {}\n"),
			},
		},
	}); err != nil {
		t.Fatalf("StartTurnReview: %v", err)
	}

	// Turning off Go source privacy must redact .go diff/hand-edit lines from review_status without leaking source code.
	if err := f.ed.SetAgentPrivacy(editor.PrivacyGoSource, false); err != nil {
		t.Fatal(err)
	}
	res := call(t, cs, editormcp.ToolReviewStatus, nil)
	mustOK(t, res)
	var goSnap editor.ReviewSnapshot
	structured(t, res, &goSnap)
	for _, ch := range goSnap.Changes {
		if ch.File == "game.go" {
			if len(ch.YAMLLines) > 0 || len(ch.HandEditLines) > 0 || ch.HandEditYAML != "" || len(ch.Conflicts) > 0 {
				t.Fatalf("expected .go lines redacted when PrivacyGoSource is off, got: %+v", ch)
			}
			for _, v := range ch.Versions {
				if len(v.YAMLLines) > 0 {
					t.Fatalf("expected .go version lines redacted when PrivacyGoSource is off, got: %+v", v)
				}
			}
		}
	}

	// Re-enabling Go source but turning off Scene & Data privacy must refuse review_status.
	if err := f.ed.SetAgentPrivacy(editor.PrivacyGoSource, true); err != nil {
		t.Fatal(err)
	}
	if err := f.ed.SetAgentPrivacy(editor.PrivacySceneAndData, false); err != nil {
		t.Fatal(err)
	}
	res = call(t, cs, editormcp.ToolReviewStatus, nil)
	if !res.IsError {
		t.Fatalf("expected review_status refused when PrivacySceneAndData is off, got OK: %+v", res)
	}
}
