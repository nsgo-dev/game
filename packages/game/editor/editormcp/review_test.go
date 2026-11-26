package editormcp_test

import (
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
