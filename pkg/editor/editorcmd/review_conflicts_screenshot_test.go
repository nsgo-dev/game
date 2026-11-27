package editorcmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const macosReviewBaseScene = `version: 3
name: level_01
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    space: world
    order: 0
    objects:
      - id: plank
        x: 336
        y: 226
      - id: knight
        name: Knight
        x: 352
        y: 192
        z: 10
      - id: door_1
        name: door_1
        x: 574
        y: 248
        properties:
          on_open:
            - open: true
      - id: crate_04
        x: 504
        y: 270
`

const macosReviewAgentScene = `version: 3
name: level_01
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    space: world
    order: 0
    objects:
      - id: plank
        x: 336
        y: 226
      - id: knight
        name: Knight
        x: 352
        y: 192
        z: 12
      - id: door_1
        name: door_1
        x: 574
        y: 248
        properties:
          on_open:
            - open: true
            - reveal: coin_trail
      - id: slime_01
        prefab: prefabs/slime.prefab.yaml
        x: 160
        y: 278
      - id: slime_02
        prefab: prefabs/slime.prefab.yaml
        x: 232
        y: 278
      - id: slime_03
        prefab: prefabs/slime.prefab.yaml
        x: 452
        y: 208
      - id: coin_01
        prefab: prefabs/coin.prefab.yaml
        x: 486
        y: 280
      - id: coin_02
        prefab: prefabs/coin.prefab.yaml
        x: 512
        y: 270
      - id: coin_03
        prefab: prefabs/coin.prefab.yaml
        x: 538
        y: 280
`

// TestReviewConflictsAndYAMLEditMacOSScreenshots renders the Review screen on the real native
// macOS host and captures screenshots of:
//  1. Unresolved conflict on door_1 ("Choose how door_1 merges", Mine/Agent's/Both, disabled Keep)
//  2. Resolved conflict ("Merged both, yours first")
//  3. In-place YAML parse error ("Line N isn't valid YAML", "Add the colon", "Undo my typing")
//  4. Fixed YAML hand edit tagged "you" with live viewport update
//  5. Spec Light appearance parity
func TestReviewConflictsAndYAMLEditMacOSScreenshots(t *testing.T) {
	dir := t.TempDir()
	for rel, content := range map[string]string{
		"game.yaml": `version: 1
name: review-conflicts
id: dev.nsgo.review-conflicts
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: level_01
  scenes:
    - name: level_01
      path: scenes/level_01.scene.yaml
`,
		"scenes/level_01.scene.yaml": macosReviewBaseScene,
		"prefabs/slime.prefab.yaml":  "version: 1\nid: slime\nname: Slime\n",
		"prefabs/coin.prefab.yaml":   "version: 1\nid: coin\nname: Coin\n",
	} {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	ed.SetAppearance(editor.AppearanceDark)

	if err := ed.StartTurnReview(editor.ReviewTurnInput{
		Request: "Put slime patrols on the lower floors of level_01, with a coin trail toward door_1.",
		Files: []editor.ReviewFileInput{
			{
				Path:       "scenes/level_01.scene.yaml",
				BeforeYAML: []byte(macosReviewBaseScene),
				AfterYAML:  []byte(macosReviewAgentScene),
				Reasons: map[string]string{
					"slime_01": "Two slimes share the 120 px floor span, half a cycle apart, so there is always a gap to pass.",
					"slime_02": "Two slimes share the 120 px floor span, half a cycle apart, so there is always a gap to pass.",
					"slime_03": "The plank is the only other floor wide enough for a patrol; this slime guards the coin trail from above.",
					"knight":   "Slimes are drawn at z 11; without this the Knight disappears behind slime_03 on the plank.",
					"door_1":   "The trail stays hidden until the door opens, so it reads as a reward rather than a hint.",
					"crate_04": "The crate blocked the second coin; nothing else references it.",
				},
			},
		},
	}); err != nil {
		t.Fatalf("StartTurnReview: %v", err)
	}

	// Person edits door_1's on_open list concurrently.
	if err := ed.SetProperty("door_1", "on_open", []any{
		map[string]any{"open": true},
		map[string]any{"sound": "sfx/door_creak.ogg"},
	}); err != nil {
		t.Fatal(err)
	}
	if !ed.FocusReviewChange("door_1") {
		t.Fatal("FocusReviewChange(door_1) failed")
	}

	var doorID, slimeID string
	for _, ch := range ed.ReviewStatus().Changes {
		switch ch.ObjectID {
		case "door_1":
			doorID = ch.ID
		case "slime_03":
			slimeID = ch.ID
		}
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	app.Settle()
	app.Screenshot("review-conflict-unresolved")

	// Pick Both on door_1.
	app.Press("id:review-conflict-both-door_1")
	app.Settle()
	app.Screenshot("review-conflict-both-resolved")

	// Focus slime_03, start editing YAML, type invalid "x 444" without colon.
	app.Press("id:review-card-focus-" + slimeID)
	app.Press("id:review-yaml-edit")
	app.Settle()
	app.Post(func() {
		for _, ch := range ed.ReviewStatus().Changes {
			if ch.ObjectID == "slime_03" {
				broken := strings.Replace(ch.HandEditYAML, "x: 452", "x 444", 1)
				_ = ed.SetReviewYAMLDraft("slime_03", broken)
			}
		}
	})
	app.Settle()
	app.Screenshot("review-yaml-error")

	// Apply "Add the colon" one-step fix.
	app.Press("id:review-yaml-fix")
	app.Settle()
	app.Screenshot("review-yaml-fixed-you")

	// Capture Spec Light parity with conflict card visible.
	app.Press("id:review-yaml-done")
	app.Press("id:review-card-focus-" + doorID)
	app.Post(func() {
		ed.SetAppearance(editor.AppearanceLight)
	})
	app.Settle()
	app.Screenshot("review-conflict-light")
}
