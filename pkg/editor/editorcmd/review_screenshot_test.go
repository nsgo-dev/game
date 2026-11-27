package editorcmd_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestReviewMacOSScreenshots renders the full-window change-by-change agent turn Review screen
// on the real native macOS host (linking github.com/nsgo-dev/nsgo through editorcmd) and captures
// scene, prefab-impact, data-table, revert-all dialog, paused-editor, and light-theme screenshots
// for visual inspection beside docs/design/editor/Review.dc.html and Spec.dc.html (#1014).
func TestReviewMacOSScreenshots(t *testing.T) {
	dir := t.TempDir()
	writeFile := func(rel, content string) []byte {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		b := []byte(content)
		if err := os.WriteFile(full, b, 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
		return b
	}

	writeFile("game.yaml", `version: 1
name: dungeon-run
id: dev.nsgo.dungeon-run
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: level_01
  scenes:
    - name: level_01
      path: scenes/level_01.scene.yaml
    - name: level_02
      path: scenes/level_02.scene.yaml
    - name: level_03
      path: scenes/level_03.scene.yaml
`)

	level1Before := writeFile("scenes/level_01.scene.yaml", `version: 2
name: level_01
virtualWidth: 640
virtualHeight: 360
backgroundColor: "#181C24"
layers:
  - name: actors
    objects:
      - id: knight
        x: 352
        "y": 192
        z: 10
      - id: door_1
        x: 574
        "y": 248
      - id: crate_04
        x: 504
        "y": 270
`)

	level1After := []byte(`version: 2
name: level_01
virtualWidth: 640
virtualHeight: 360
backgroundColor: "#181C24"
layers:
  - name: actors
    objects:
      - id: knight
        x: 352
        "y": 192
        z: 12
      - id: door_1
        x: 574
        "y": 248
      - id: slime_01
        prefab: prefabs/slime.prefab.yaml
        x: 160
        "y": 278
      - id: slime_02
        prefab: prefabs/slime.prefab.yaml
        x: 232
        "y": 278
      - id: slime_03
        prefab: prefabs/slime.prefab.yaml
        x: 452
        "y": 208
      - id: coin_01
        x: 486
        "y": 280
`)

	level2Before := writeFile("scenes/level_02.scene.yaml", `version: 2
name: level_02
virtualWidth: 640
virtualHeight: 360
backgroundColor: "#191B26"
layers:
  - name: actors
    objects:
      - id: spawn
        x: 64
        "y": 262
      - id: door_2
        x: 600
        "y": 236
`)

	level2After := []byte(`version: 2
name: level_02
virtualWidth: 640
virtualHeight: 360
backgroundColor: "#191B26"
layers:
  - name: actors
    objects:
      - id: spawn
        x: 48
        "y": 262
      - id: door_2
        x: 600
        "y": 236
      - id: slime_04
        prefab: prefabs/slime.prefab.yaml
        x: 150
        "y": 278
      - id: slime_05
        prefab: prefabs/slime.prefab.yaml
        x: 260
        "y": 278
`)

	writeFile("scenes/level_03.scene.yaml", `version: 2
name: level_03
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    objects:
      - id: old_slime_1
        prefab: prefabs/slime.prefab.yaml
        x: 100
        "y": 260
      - id: old_slime_2
        prefab: prefabs/slime.prefab.yaml
        x: 180
        "y": 260
      - id: old_slime_3
        prefab: prefabs/slime.prefab.yaml
        x: 260
        "y": 260
`)

	prefabBefore := writeFile("prefabs/slime.prefab.yaml", `version: 1
name: slime
components:
  patrol:
    speed: 32
    turn_pause: 0.4
`)

	prefabAfter := []byte(`version: 1
name: slime
components:
  patrol:
    speed: 24
    turn_pause: 0.4
`)

	dataBefore := writeFile("data/enemies.yaml", `version: 1
rows:
  - id: bat
    hp: 2
    speed: 40
    damage: 1
    xp: 3
  - id: slime
    hp: 3
    speed: 24
    damage: 1
    xp: 2
  - id: skeleton
    hp: 5
    speed: 28
    damage: 2
    xp: 6
`)

	dataAfter := []byte(`version: 1
rows:
  - id: bat
    hp: 2
    speed: 40
    damage: 1
    xp: 3
  - id: slime
    hp: 2
    speed: 24
    damage: 1
    xp: 2
  - id: skeleton
    hp: 5
    speed: 28
    damage: 2
    xp: 6
`)

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	ed.WaitForProblems()

	if err := ed.StartTurnReview(editor.ReviewTurnInput{
		Request: "Put slime patrols on the lower floors of level_01 and level_02, with a coin trail toward each door.",
		Files: []editor.ReviewFileInput{
			{
				Path:       "scenes/level_01.scene.yaml",
				BeforeYAML: level1Before,
				AfterYAML:  level1After,
				Reasons: map[string]string{
					"slime_01": "Two slimes share the 120 px floor span, half a cycle apart, so there is always a gap to pass.",
					"slime_02": "Two slimes share the 120 px floor span, half a cycle apart, so there is always a gap to pass.",
					"slime_03": "The plank is the only other floor wide enough for a patrol; this slime guards the coin trail from above.",
					"coin_01":  "Coins lead from under the plank to door_1, spaced one jump apart.",
					"knight":   "Slimes draw at z 11; without z 12 the Knight disappears behind slime_03 on the plank.",
					"crate_04": "The crate blocked the coin trail; nothing else references it.",
				},
			},
			{
				Path:       "scenes/level_02.scene.yaml",
				BeforeYAML: level2Before,
				AfterYAML:  level2After,
				Reasons: map[string]string{
					"slime_04": "Patrols the left lower floor.",
					"slime_05": "Patrols the center lower floor.",
					"spawn":    "At x 64 the Knight spawned inside slime_04's patrol and took a hit at once.",
				},
			},
			{
				Path:       "prefabs/slime.prefab.yaml",
				BeforeYAML: prefabBefore,
				AfterYAML:  prefabAfter,
				Reasons: map[string]string{
					"patrol.speed": "Slower patrols leave time to collect the trail between passes.",
				},
			},
			{
				Path:       "data/enemies.yaml",
				BeforeYAML: dataBefore,
				AfterYAML:  dataAfter,
				Reasons: map[string]string{
					"slime": "There are four times as many slimes on these floors; two hits each keeps clearing them quick.",
				},
			},
		},
	}); err != nil {
		t.Fatalf("StartTurnReview: %v", err)
	}

	// Mark a few changes seen and focus slime_03.
	snap := ed.ReviewStatus()
	ed.FocusReviewChange(snap.Changes[1].ID)
	ed.FocusReviewChange(snap.Changes[2].ID)
	ed.ToggleReviewChangeReverted(snap.Changes[5].ID) // revert crate_04 removal

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))

	// 1. Scene change view (Dark).
	app.Screenshot("review-scene-dark")

	// 2. Prefab impact card view (Dark).
	for _, ch := range ed.ReviewStatus().Changes {
		if ch.FileKind == editor.ReviewFilePrefab {
			app.Post(func() { ed.FocusReviewChange(ch.ID) })
			app.Settle()
			break
		}
	}
	app.Screenshot("review-prefab-dark")

	// 3. Small data table view (Dark).
	for _, ch := range ed.ReviewStatus().Changes {
		if ch.FileKind == editor.ReviewFileData {
			app.Post(func() { ed.FocusReviewChange(ch.ID) })
			app.Settle()
			break
		}
	}
	app.Screenshot("review-datatable-dark")

	// 4. Revert All confirmation dialog (Dark).
	app.Post(func() { ed.OpenReviewRevertAllDialog() })
	app.Settle()
	app.Screenshot("review-revert-dialog-dark")
	app.Post(func() { ed.CancelReviewRevertAllDialog() })
	app.Settle()

	// 5. Paused pending review in Editor (Agent panel Review waiting card + viewport overlays).
	app.Post(func() { ed.PauseReview() })
	app.Settle()
	app.Screenshot("review-paused-editor-dark")

	// 6. Spec Light appearance of the full-window Review screen.
	app.Post(func() {
		ed.SetAppearance(editor.AppearanceLight)
		_ = ed.ResumeReview()
		ed.FocusReviewChange(snap.Changes[2].ID)
	})
	app.Settle()
	app.Screenshot("review-scene-light")
}
