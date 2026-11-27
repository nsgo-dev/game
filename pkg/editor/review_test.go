package editor_test

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type reviewFixture struct {
	dir          string
	ed           *editor.Editor
	level1Before []byte
	level1After  []byte
	level2Before []byte
	level2After  []byte
	level3Before []byte
	prefabBefore []byte
	prefabAfter  []byte
	dataBefore   []byte
	dataAfter    []byte
}

func newReviewFixture(t *testing.T) *reviewFixture {
	t.Helper()
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

	level3Before := writeFile("scenes/level_03.scene.yaml", `version: 2
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

	return &reviewFixture{
		dir:          dir,
		ed:           ed,
		level1Before: level1Before,
		level1After:  level1After,
		level2Before: level2Before,
		level2After:  level2After,
		level3Before: level3Before,
		prefabBefore: prefabBefore,
		prefabAfter:  prefabAfter,
		dataBefore:   dataBefore,
		dataAfter:    dataAfter,
	}
}

func (f *reviewFixture) startTurn(t *testing.T, includeData bool) {
	t.Helper()
	files := []editor.ReviewFileInput{
		{
			Path:       "scenes/level_01.scene.yaml",
			BeforeYAML: f.level1Before,
			AfterYAML:  f.level1After,
			Reasons: map[string]string{
				"slime_01": "Two slimes share the floor span, half a cycle apart, so there is always a gap to pass.",
				"slime_02": "Two slimes share the floor span, half a cycle apart, so there is always a gap to pass.",
				"slime_03": "Guards the coin trail from the upper plank.",
				"coin_01":  "Starts the coin trail leading toward door_1.",
				"knight":   "Slimes draw at z 11; without z 12 the Knight disappears behind slime_03.",
				"crate_04": "The crate blocked the coin trail and nothing references it.",
			},
		},
		{
			Path:       "scenes/level_02.scene.yaml",
			BeforeYAML: f.level2Before,
			AfterYAML:  f.level2After,
			Reasons: map[string]string{
				"slime_04": "Patrols the left lower floor.",
				"slime_05": "Patrols the center lower floor.",
				"spawn":    "Moved clear of slime_04's patrol range.",
			},
		},
		{
			Path:       "prefabs/slime.prefab.yaml",
			BeforeYAML: f.prefabBefore,
			AfterYAML:  f.prefabAfter,
			Reasons: map[string]string{
				"patrol.speed": "Slower patrols leave time to collect the trail between passes.",
			},
		},
	}
	if includeData {
		files = append(files, editor.ReviewFileInput{
			Path:       "data/enemies.yaml",
			BeforeYAML: f.dataBefore,
			AfterYAML:  f.dataAfter,
			Reasons: map[string]string{
				"slime": "With more slimes on the floor, two hits keeps clearing them snappy.",
			},
		})
	}
	if err := f.ed.StartTurnReview(editor.ReviewTurnInput{
		Request: "Put slime patrols on the lower floors of level_01 and level_02, with a coin trail toward each door.",
		Files:   files,
	}); err != nil {
		t.Fatalf("StartTurnReview: %v", err)
	}
}

// #1014 Acceptance 1: Opens the review of a turn with changes in two scenes and a prefab,
// filters to Unseen, walks it with J and K, reverts a file group with X on its card,
// peeks with B, keeps, and checks the saved YAML and that one Undo restores everything.
func TestReviewChangeByChangeWorkflowAndOneUndo(t *testing.T) {
	f := newReviewFixture(t)
	f.startTurn(t, true)

	macEnv := nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true, Locale: "en-US"})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, f.ed)
	}, nsgotest.Size(1440, 900), macEnv)

	if !app.Exists("id:review-screen") {
		t.Fatal("review-screen did not mount when StartTurnReview opened")
	}

	snap := f.ed.ReviewStatus()
	// level_01: 4 added + 1 changed + 1 removed = 6; level_02: 2 added + 1 changed = 3; prefab: 1; data: 1 => 11 total
	if snap.TotalChanges != 11 || snap.FileCount != 4 || snap.SeenCount != 1 {
		t.Fatalf("initial snap = total:%d files:%d seen:%d, want 11/4/1", snap.TotalChanges, snap.FileCount, snap.SeenCount)
	}

	// Verify focused change overlay uses its own semantic color at 2.5 px (never accent).
	ovs := f.ed.ReviewOverlays()
	foundFocused := false
	for _, ov := range ovs {
		if ov.Focused {
			foundFocused = true
			if ov.ColorRole == "accent" {
				t.Fatalf("focused review overlay used accent color role: %+v", ov)
			}
			if ov.StrokeWidth != 2.5 {
				t.Fatalf("focused review overlay stroke width = %v, want 2.5", ov.StrokeWidth)
			}
		}
	}
	if !foundFocused {
		t.Fatalf("expected focused change overlay in %+v", ovs)
	}

	// Verify prefab impact cites both review scenes AND existing level_03 instances!
	var prefabChange *editor.ReviewChange
	for i := range snap.Changes {
		if snap.Changes[i].File == "prefabs/slime.prefab.yaml" {
			prefabChange = &snap.Changes[i]
			break
		}
	}
	if prefabChange == nil {
		t.Fatal("missing prefab change")
	}
	if prefabChange.DeltaPct != "−25%" {
		t.Fatalf("prefab DeltaPct = %q, want −25%%", prefabChange.DeltaPct)
	}
	if !strings.Contains(prefabChange.PrefabImpact, "level_01") ||
		!strings.Contains(prefabChange.PrefabImpact, "level_02") ||
		!strings.Contains(prefabChange.PrefabImpact, "level_03") {
		t.Fatalf("prefab impact missing expected scenes: %q", prefabChange.PrefabImpact)
	}

	// Filter to Unseen and walk with J and K, checking framing updates and seen count increases.
	app.Press("id:review-chip-unseen")
	if got := f.ed.ReviewStatus().Filter; got != editor.ReviewFilterUnseen {
		t.Fatalf("filter = %q, want unseen", got)
	}

	viewBefore := f.ed.ReviewViewportView2D()
	app.Key("j")
	snapAfterJ1 := f.ed.ReviewStatus()
	if snapAfterJ1.SeenCount != 2 {
		t.Fatalf("after first J, seen = %d, want 2", snapAfterJ1.SeenCount)
	}
	viewAfterJ1 := f.ed.ReviewViewportView2D()
	if viewAfterJ1.X == viewBefore.X && viewAfterJ1.Y == viewBefore.Y {
		t.Fatalf("J did not re-frame viewport: before=%+v after=%+v", viewBefore, viewAfterJ1)
	}

	app.Key("j")
	if got := f.ed.ReviewStatus().SeenCount; got != 3 {
		t.Fatalf("after second J, seen = %d, want 3", got)
	}
	focusedAfterJ2 := f.ed.ReviewStatus().FocusedID
	app.Key("k")
	if got := f.ed.ReviewStatus().FocusedID; got == focusedAfterJ2 {
		t.Fatalf("K did not move to previous change; still %q", got)
	}

	// Return to All filter, focus level_02 file row, and revert the entire level_02 file group with X.
	app.Press("id:review-chip-all")
	app.Post(func() {
		f.ed.FocusReviewFileRow("scenes/level_02.scene.yaml")
	})
	app.Settle()
	app.Key("x")

	snapReverted := f.ed.ReviewStatus()
	if snapReverted.RevertedCount != 3 || snapReverted.KeptCount != 8 {
		t.Fatalf("after reverting level_02 group with X: reverted=%d kept=%d, want 3/8", snapReverted.RevertedCount, snapReverted.KeptCount)
	}

	// Focus a level_01 change and hold B to peek Before; overlays clear and banner appears.
	app.Post(func() {
		f.ed.FocusReviewChange(snapReverted.Changes[0].ID)
	})
	app.Settle()
	app.KeyDown("b")
	if !f.ed.ReviewPeekingBefore() || !app.Exists("id:review-peek-banner") {
		t.Fatal("holding B did not enable Before peek mode")
	}
	if len(f.ed.ReviewOverlays()) != 0 {
		t.Fatalf("overlays remained visible while peeking Before: %+v", f.ed.ReviewOverlays())
	}
	app.KeyUp("b")
	if f.ed.ReviewPeekingBefore() {
		t.Fatal("releasing B did not exit Before peek mode")
	}

	// Keep with ⌘↵ (meta+enter).
	app.Key("meta+enter")
	if f.ed.ReviewOpen() || f.ed.HasPendingReview() {
		t.Fatal("review remained open/pending after Keep")
	}
	if got := f.ed.ReviewOutcome(); got != editor.ReviewOutcomeKept {
		t.Fatalf("outcome = %q, want kept", got)
	}
	if !app.Exists("id:editor-toast") || !app.Exists("id:agent-review-kept-card") {
		t.Fatal("expected toast with Undo and Agent panel kept summary after Keep")
	}
	if msg := app.Find("id:agent-review-outcome-text").Text; !strings.Contains(msg, "Kept 8") || !strings.Contains(msg, "reverted 3") || !strings.Contains(msg, "one undo step") {
		t.Fatalf("kept summary = %q", msg)
	}

	// Check saved YAML on disk: level_01 kept, level_02 reverted byte-for-byte, prefab kept, data kept!
	gotL1, err := os.ReadFile(filepath.Join(f.dir, "scenes", "level_01.scene.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gotL1), "id: slime_01") || !strings.Contains(string(gotL1), "z: 12") || strings.Contains(string(gotL1), "id: crate_04") {
		t.Fatalf("saved level_01 YAML unexpected:\n%s", gotL1)
	}
	gotL2, err := os.ReadFile(filepath.Join(f.dir, "scenes", "level_02.scene.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotL2, f.level2Before) {
		t.Fatalf("reverted level_02 was modified on disk:\ngot:\n%s\nwant:\n%s", gotL2, f.level2Before)
	}
	gotPrefab, err := os.ReadFile(filepath.Join(f.dir, "prefabs", "slime.prefab.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gotPrefab), "speed: 24") {
		t.Fatalf("saved prefab YAML unexpected:\n%s", gotPrefab)
	}
	gotData, err := os.ReadFile(filepath.Join(f.dir, "data", "enemies.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gotData), "hp: 2") {
		t.Fatalf("saved enemies.yaml unexpected:\n%s", gotData)
	}

	// ONE Undo restores every touched file byte for byte!
	app.Post(func() {
		f.ed.Undo()
	})
	app.Settle()

	for rel, want := range map[string][]byte{
		"scenes/level_01.scene.yaml": f.level1Before,
		"scenes/level_02.scene.yaml": f.level2Before,
		"prefabs/slime.prefab.yaml":  f.prefabBefore,
		"data/enemies.yaml":          f.dataBefore,
	} {
		got, err := os.ReadFile(filepath.Join(f.dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s after undo: %v", rel, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s after one Undo did not restore byte-for-byte:\ngot:\n%s\nwant:\n%s", rel, got, want)
		}
	}
}

// #1014 Acceptance 2: Esc leaves the review pending (editor shows overlays & Agent panel card)
// and Resume returns to the exact same focused change and seen state.
func TestReviewEscLeavesPendingAndResumeRestoresFocusAndSeen(t *testing.T) {
	f := newReviewFixture(t)
	f.startTurn(t, false)

	macEnv := nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true, Locale: "en-US"})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, f.ed)
	}, nsgotest.Size(1440, 900), macEnv)

	app.Key("j")
	app.Key("j")
	beforePause := f.ed.ReviewStatus()
	if beforePause.SeenCount != 3 {
		t.Fatalf("seen before Esc = %d, want 3", beforePause.SeenCount)
	}

	// Press Esc to leave Review while keeping it pending.
	app.Key("escape")
	if f.ed.ReviewOpen() {
		t.Fatal("Esc did not close full-window review screen")
	}
	if !f.ed.HasPendingReview() {
		t.Fatal("Esc dropped pending review instead of keeping it pending")
	}
	if !app.Exists("id:agent-review-waiting-card") {
		t.Fatal("Agent panel did not show Review waiting card after Esc")
	}
	if title := app.Find("id:agent-review-waiting-title").Text; !strings.Contains(title, "Review waiting") {
		t.Fatalf("waiting title = %q", title)
	}
	if meta := app.Find("id:agent-review-waiting-meta").Text; !strings.Contains(meta, "3 of 10 seen") {
		t.Fatalf("waiting meta = %q, want 3 of 10 seen", meta)
	}
	// Main editor viewport must still expose active overlays for the open scene (level_01).
	if len(f.ed.ReviewOverlays()) == 0 {
		t.Fatal("main editor viewport lost pending review overlays after Esc")
	}

	// Press Resume review and verify exact focused change and seen count return.
	app.Press("id:agent-resume-review")
	if !f.ed.ReviewOpen() || !app.Exists("id:review-screen") {
		t.Fatal("Resume review did not reopen ReviewScreenView")
	}
	afterResume := f.ed.ReviewStatus()
	if afterResume.FocusedID != beforePause.FocusedID || afterResume.SeenCount != beforePause.SeenCount {
		t.Fatalf("after Resume: focused=%q seen=%d, want focused=%q seen=%d",
			afterResume.FocusedID, afterResume.SeenCount, beforePause.FocusedID, beforePause.SeenCount)
	}
}

// Blocker disables Keep and pressing Keep jumps directly to the blocking change;
// Revert All opens a confirmation dialog saying what goes back and that ⌘Z brings it all back.
func TestReviewBlockerAndRevertAllDialog(t *testing.T) {
	f := newReviewFixture(t)
	f.startTurn(t, false)

	snap := f.ed.ReviewStatus()
	blockerID := snap.Changes[len(snap.Changes)-1].ID
	f.ed.SetReviewChangeBlocker(blockerID, "Resolve conflict on prefab")

	macEnv := nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true, Locale: "en-US"})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, f.ed)
	}, nsgotest.Size(1440, 900), macEnv)

	if !app.Exists("id:review-blocker-pill") {
		t.Fatal("blocker pill missing in review header")
	}
	// Pressing Keep while blocked must NOT keep, and must jump directly to blockerID.
	app.Press("id:review-keep")
	if !f.ed.ReviewOpen() {
		t.Fatal("Keep succeeded despite active blocker")
	}
	if got := f.ed.ReviewStatus().FocusedID; got != blockerID {
		t.Fatalf("Keep on blocked review focused %q, want %q", got, blockerID)
	}

	// Clear blocker and test ⌘⌫ Revert All confirmation dialog.
	app.Post(func() {
		f.ed.SetReviewChangeBlocker(blockerID, "")
	})
	app.Settle()

	app.Key("meta+backspace")
	if !app.Exists("id:review-revert-all-dialog") {
		t.Fatal("meta+backspace did not open Revert All confirmation dialog")
	}
	body := app.Find("id:review-revert-all-body").Text
	if !strings.Contains(body, "level_01") || !strings.Contains(body, "⌘Z brings everything back") {
		t.Fatalf("Revert All dialog body = %q", body)
	}

	// Cancel with Esc keeps review open.
	app.Key("escape")
	if app.Exists("id:review-revert-all-dialog") || !f.ed.ReviewOpen() {
		t.Fatal("Esc did not cancel Revert All dialog cleanly")
	}

	// Open again and confirm Revert all -> toast with Undo -> Undo restores pending review!
	app.Key("meta+backspace")
	app.Press("id:review-revert-all-confirm")
	if f.ed.ReviewOpen() || f.ed.ReviewOutcome() != editor.ReviewOutcomeReverted {
		t.Fatalf("after confirm revert all: open=%v outcome=%q", f.ed.ReviewOpen(), f.ed.ReviewOutcome())
	}
	if !app.Exists("id:editor-toast") {
		t.Fatal("missing Undo toast after Revert all")
	}
	app.Press("id:editor-toast-action")
	if !f.ed.ReviewOpen() || !f.ed.HasPendingReview() {
		t.Fatal("clicking Undo on Revert all toast did not bring the review back")
	}
}

// #1014 Acceptance 3: Every control is reachable with Tab, F6 or arrows and shows the focus ring;
// each action has its shortcut in the Review scope; typing in filter input never fires J/K/X/B.
func TestReviewKeyboardAccessibilityFocusRingsAndShortcuts(t *testing.T) {
	f := newReviewFixture(t)
	f.startTurn(t, true)

	// Verify all Review shortcuts are registered in ScopeReview.
	wantShortcuts := map[string]string{
		editor.CmdReviewNext:         "j",
		editor.CmdReviewPrev:         "k",
		editor.CmdReviewToggleRevert: "x",
		editor.CmdReviewPeekBefore:   "b",
		editor.CmdReviewKeep:         "mod+enter",
		editor.CmdReviewRevertAll:    "mod+backspace",
	}
	found := map[string]string{}
	for _, cmd := range f.ed.Commands() {
		if cmd.Scope == editor.ScopeReview {
			found[cmd.ID] = cmd.Shortcut
		}
	}
	for id, wantKey := range wantShortcuts {
		if got, ok := found[id]; !ok || got != wantKey {
			t.Fatalf("Review scope command %s = (%q, %v), want %q", id, got, ok, wantKey)
		}
	}

	macEnv := nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true, Locale: "en-US"})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, f.ed)
	}, nsgotest.Size(1440, 900), macEnv)

	// Tab through review controls and verify keyboard focus reaches controls and draws the 2 px Spec focus ring.
	tabFocusCount := 0
	for range 12 {
		app.Key("tab")
		if fv := focusedView(app); fv != nil && fv.Style.Outline.Width == 2 {
			tabFocusCount++
		}
	}
	if tabFocusCount < 4 {
		t.Fatalf("Tab traversal drew 2 px Spec focus ring on only %d controls, want >= 4", tabFocusCount)
	}

	// Cycle through Review's 4 regions with F6.
	var visited []string
	for range 4 {
		app.Key("f6")
		for _, rid := range []string{"review-header", "review-sidebar", "review-center", "review-right-pane"} {
			if n := app.Find("id:" + rid); n.Style.Outline.Width == 2 {
				if !slices.Contains(visited, rid) {
					visited = append(visited, rid)
				}
			}
		}
	}
	if len(visited) != 4 {
		t.Fatalf("F6 visited %v, want all 4 review regions", visited)
	}

	// Typing in filter input must filter by name/field/file and NEVER fire single-letter J/K/X/B shortcuts.
	revertedBefore := f.ed.ReviewStatus().RevertedCount
	app.Type("id:review-filter-input", "patrol")
	if f.ed.ReviewStatus().RevertedCount != revertedBefore {
		t.Fatal("typing in review-filter-input triggered revert shortcut")
	}
	rows := f.ed.ReviewVisibleRows()
	if len(rows) == 0 {
		t.Fatal("filter query 'patrol' returned 0 rows")
	}
	for _, r := range rows {
		if r.RowKind == editor.ReviewRowCard && r.File != "prefabs/slime.prefab.yaml" {
			t.Fatalf("unexpected row %+v for query 'patrol'", r)
		}
	}
}

// #1014 Acceptance 5: Captures real macOS nsgotest screenshots of the Review screen states
// (scene change, prefab impact card, small data table, Revert All dialog, pending Agent card)
// in Dark and Light Spec appearances.
func TestReviewScreenScreenshots(t *testing.T) {
	f := newReviewFixture(t)
	f.startTurn(t, true)

	// Mark a couple of changes seen and revert one so the screenshot shows realistic mid-review state.
	snap := f.ed.ReviewStatus()
	f.ed.FocusReviewChange(snap.Changes[1].ID)
	f.ed.FocusReviewChange(snap.Changes[2].ID)
	f.ed.ToggleReviewChangeReverted(snap.Changes[5].ID) // revert crate_04 removal

	macDark := nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true, Locale: "en-US"})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, f.ed)
	}, nsgotest.Size(1440, 900), macDark)

	// 1. Scene change view (slime_03 focused, 2.5 px dashed added overlay + outlined pill + Why + YAML diff).
	app.Screenshot("review-scene-dark")

	// 2. Prefab impact card view (patrol.speed 32 -> 24 -25% + applies to level_01, level_02, level_03).
	for _, ch := range f.ed.ReviewStatus().Changes {
		if ch.FileKind == editor.ReviewFilePrefab {
			app.Post(func() { f.ed.FocusReviewChange(ch.ID) })
			app.Settle()
			break
		}
	}
	app.Screenshot("review-prefab-dark")

	// 3. Small data table view (data/enemies.yaml with changed hp cell).
	for _, ch := range f.ed.ReviewStatus().Changes {
		if ch.FileKind == editor.ReviewFileData {
			app.Post(func() { f.ed.FocusReviewChange(ch.ID) })
			app.Settle()
			break
		}
	}
	app.Screenshot("review-datatable-dark")

	// 4. Revert All confirmation dialog.
	app.Key("meta+backspace")
	app.Screenshot("review-revert-dialog-dark")
	app.Key("escape")

	// 5. Paused pending state in Editor (Esc -> Agent panel Review waiting card + viewport overlays).
	app.Key("escape")
	app.Screenshot("review-paused-editor-dark")
}

func TestReviewSecurityPathConfinementAndYouOnly(t *testing.T) {
	f := newReviewFixture(t)

	// 1. Refuse traversal, .git metadata, and You-only files at StartTurnReview.
	for _, badPath := range []string{
		"..",
		"../outside.yaml",
		"scenes/../../outside.yaml",
		"/etc/passwd",
		".git/config",
		"permissions.yaml",
		".nsgo/permissions.yaml",
		"tools.yaml",
		".nsgo/models.yaml",
		"usage.yaml",
	} {
		err := f.ed.StartTurnReview(editor.ReviewTurnInput{
			Request: "bad path test",
			Files: []editor.ReviewFileInput{
				{
					Path:       badPath,
					BeforeYAML: []byte("a: 1\n"),
					AfterYAML:  []byte("a: 2\n"),
				},
			},
		})
		if err == nil {
			t.Fatalf("StartTurnReview accepted forbidden path %q", badPath)
		}
	}

	// 2. Refuse oversized files (>8 MiB).
	huge := bytes.Repeat([]byte("a"), (8<<20)+1)
	if err := f.ed.StartTurnReview(editor.ReviewTurnInput{
		Request: "oversized test",
		Files: []editor.ReviewFileInput{
			{Path: "notes.txt", BeforeYAML: []byte("ok\n"), AfterYAML: huge},
		},
	}); err == nil {
		t.Fatal("StartTurnReview accepted file exceeding 8 MiB cap")
	}

	// 3. Refuse symlink escape on KeepTurnReview and ConfirmReviewRevertAll without touching outside target.
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("original-secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(f.dir, "escaped.txt")
	if err := os.Symlink(outsideFile, linkPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := f.ed.StartTurnReview(editor.ReviewTurnInput{
		Request: "symlink escape test",
		Files: []editor.ReviewFileInput{
			{
				Path:       "escaped.txt",
				BeforeYAML: []byte("original-secret\n"),
				AfterYAML:  []byte("pwned\n"),
			},
		},
	}); err != nil {
		t.Fatalf("StartTurnReview(escaped.txt): %v", err)
	}
	if err := f.ed.KeepTurnReview(); err == nil {
		t.Fatal("KeepTurnReview overwrote symlink escaping project root")
	}
	if err := f.ed.ConfirmReviewRevertAll(); err == nil {
		t.Fatal("ConfirmReviewRevertAll wrote through symlink escaping project root")
	}
	gotOutside, err := os.ReadFile(outsideFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotOutside) != "original-secret\n" {
		t.Fatalf("outside symlink target was modified: %q", gotOutside)
	}
}

func TestReviewLayerMovePartialRevertAndMultiTabSync(t *testing.T) {
	f := newReviewFixture(t)

	// Open BOTH level_01 and level_02 as open tabs before starting the review.
	if err := f.ed.OpenScene("scenes/level_02.scene.yaml"); err != nil {
		t.Fatalf("OpenScene level_02: %v", err)
	}
	if err := f.ed.OpenScene("scenes/level_01.scene.yaml"); err != nil {
		t.Fatalf("OpenScene level_01: %v", err)
	}

	// level_01 moves knight to a new layer "foreground" AND adds an extra coin we will revert.
	l1AfterMove := []byte(`version: 2
name: level_01
virtualWidth: 640
virtualHeight: 360
backgroundColor: "#181C24"
layers:
  - name: actors
    objects:
      - id: door_1
        x: 574
        "y": 248
      - id: crate_04
        x: 504
        "y": 270
      - id: temp_coin
        x: 100
        "y": 100
  - name: foreground
    objects:
      - id: knight
        x: 352
        "y": 192
        z: 15
`)
	if err := f.ed.StartTurnReview(editor.ReviewTurnInput{
		Request: "Move knight to foreground and add slimes to level_02",
		Files: []editor.ReviewFileInput{
			{Path: "scenes/level_01.scene.yaml", BeforeYAML: f.level1Before, AfterYAML: l1AfterMove},
			{Path: "scenes/level_02.scene.yaml", BeforeYAML: f.level2Before, AfterYAML: f.level2After},
		},
	}); err != nil {
		t.Fatalf("StartTurnReview: %v", err)
	}

	// Revert temp_coin so level_01 must reconstruct via effectiveReviewSceneDoc.
	for _, ch := range f.ed.ReviewStatus().Changes {
		if ch.ObjectID == "temp_coin" {
			f.ed.ToggleReviewChangeReverted(ch.ID)
		}
	}
	if err := f.ed.KeepTurnReview(); err != nil {
		t.Fatalf("KeepTurnReview: %v", err)
	}

	// Verify knight moved to foreground layer and temp_coin was excluded.
	gotL1, err := os.ReadFile(filepath.Join(f.dir, "scenes", "level_01.scene.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gotL1), "id: temp_coin") {
		t.Fatalf("reverted temp_coin leaked into saved level_01:\n%s", gotL1)
	}
	if !strings.Contains(string(gotL1), "name: foreground") || !strings.Contains(string(gotL1), "z: 15") {
		t.Fatalf("knight layer move lost during partial revert reconstruction:\n%s", gotL1)
	}
	// Verify knight is on foreground in active Editor.Doc (not duplicated on actors).
	fgFound, actorsFound := false, false
	for _, l := range f.ed.Doc.Layers {
		for _, o := range l.Objects {
			if o.ID == "knight" {
				if l.Name == "foreground" {
					fgFound = true
				}
				if l.Name == "actors" {
					actorsFound = true
				}
			}
		}
	}
	if !fgFound || actorsFound {
		t.Fatalf("knight layer placement = (foreground:%v, actors:%v), want (true, false)", fgFound, actorsFound)
	}

	// Switch to previously-open inactive tab level_02 and confirm it was synced in memory!
	if err := f.ed.OpenScene("scenes/level_02.scene.yaml"); err != nil {
		t.Fatalf("switch to level_02 tab: %v", err)
	}
	foundSlime04 := false
	for _, l := range f.ed.Doc.Layers {
		for _, o := range l.Objects {
			if o.ID == "slime_04" {
				foundSlime04 = true
			}
		}
	}
	if !foundSlime04 {
		t.Fatal("inactive open tab level_02 was not synced after KeepTurnReview")
	}
	if f.ed.HasUnsavedChanges() {
		t.Fatal("synced inactive tab level_02 was left marked dirty")
	}

	// Switch back to level_01 and Undo -> both tabs and disk files restore cleanly!
	if err := f.ed.OpenScene("scenes/level_01.scene.yaml"); err != nil {
		t.Fatal(err)
	}
	f.ed.Undo()
	if err := f.ed.OpenScene("scenes/level_02.scene.yaml"); err != nil {
		t.Fatal(err)
	}
	for _, l := range f.ed.Doc.Layers {
		for _, o := range l.Objects {
			if o.ID == "slime_04" {
				t.Fatal("inactive tab level_02 still had slime_04 after Undo")
			}
		}
	}
}

func TestReviewUnifiedHistoryIntegrationAndModalShortcutGuards(t *testing.T) {
	f := newReviewFixture(t)
	f.startTurn(t, false)

	macEnv := nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: false, Locale: "en-US"})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, f.ed)
	}, nsgotest.Size(1440, 900), macEnv)

	// 1. Opening Command Palette over ReviewScreenView must block Review shortcuts and Esc must NOT pause Review.
	app.Post(func() { f.ed.SetCommandPaletteOpen(true) })
	app.Settle()
	focusedBefore := f.ed.ReviewStatus().FocusedID
	app.Key("j")
	if f.ed.ReviewStatus().FocusedID != focusedBefore {
		t.Fatal("J stepped review while Command Palette was open")
	}
	app.Key("escape") // clears the "j" typed into Command Palette search
	app.Key("escape") // closes the empty Command Palette
	app.Advance(250 * time.Millisecond)
	app.Settle()
	if !f.ed.ReviewOpen() {
		t.Fatal("Esc while Command Palette was open paused Review underneath")
	}
	if f.ed.CommandPaletteOpen() {
		t.Fatal("Esc did not close Command Palette")
	}

	// 2. While Review is paused and a You-only Permissions draft card is open, ⌘↵ must NOT keep the review.
	app.Key("escape")
	if f.ed.ReviewOpen() || !f.ed.HasPendingReview() {
		t.Fatalf("expected Review paused after Esc (open=%v pending=%v palette=%v typing=%v)",
			f.ed.ReviewOpen(), f.ed.HasPendingReview(), f.ed.CommandPaletteOpen(), f.ed.IsTyping())
	}
	app.Post(func() {
		_, err := f.ed.SubmitPermissionsDraft(editor.PermissionsDraftRequest{
			Request: "need allow",
			Rules: []editor.PermissionsDraftRuleSpec{
				{Action: editor.PermissionActionEdit, Patterns: []string{"scenes/**"}, Decision: editor.PermissionAllow},
			},
		})
		if err != nil {
			t.Errorf("SubmitPermissionsDraft: %v", err)
		}
	})
	app.Settle()
	app.Key("meta+enter")
	if !f.ed.HasPendingReview() {
		t.Fatal("meta+enter kept paused Review while PermissionsDraft dialog was open")
	}
	if !f.ed.PermissionsDraftOpen() {
		t.Fatal("meta+enter closed or applied PermissionsDraft dialog")
	}
	app.Post(func() { f.ed.DiscardPermissionsDraft() })
	app.Advance(250 * time.Millisecond)
	app.Settle()

	// 3. Resume review, then PlayIn must pause ReviewOpen so Play mode is not trapped behind ReviewScreenView.
	app.Post(func() {
		if err := f.ed.ResumeReview(); err != nil {
			t.Errorf("ResumeReview: %v", err)
		}
		if err := f.ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
	})
	app.Settle()
	if f.ed.ReviewOpen() {
		t.Fatal("Play did not pause full-window Review screen")
	}
	if !f.ed.HasPendingReview() {
		t.Fatal("Play dropped pending review instead of pausing it")
	}
	app.Post(func() {
		_ = f.ed.Stop()
		_ = f.ed.ResumeReview()
	})
	app.Settle()

	// 4. KeepTurnReview records an Agent entry in the unified History panel (#1017).
	app.Key("meta+enter")
	if f.ed.ReviewOutcome() != editor.ReviewOutcomeKept {
		t.Fatalf("outcome = %q, want kept", f.ed.ReviewOutcome())
	}
	agentEntries := f.ed.FilteredHistoryEntries(editor.HistoryFilterAgent)
	if len(agentEntries) == 0 {
		t.Fatal("KeepTurnReview did not record an HistoryAuthorAgent entry on unified History timeline")
	}
	latest := agentEntries[0]
	if latest.Author != editor.HistoryAuthorAgent || latest.ChangeCount != 10 || latest.KeptCount != 10 {
		t.Fatalf("history agent entry = %+v, want author=agent changes=10 kept=10", latest)
	}
	if !slices.Contains(latest.Files, "prefabs/slime.prefab.yaml") {
		t.Fatalf("history agent entry files = %v, missing prefabs/slime.prefab.yaml", latest.Files)
	}
}

// Regression test: ScopeReview single-key shortcuts ('x', 'j', 'k', 'b', 'mod+enter')
// must only intercept keys while ReviewScreenView is open, and must never steal 'x' from
// 3D GridMap rotation before opening Review or after pausing/closing Review.
func TestReviewShortcutsDoNotStealKeysWhenClosed(t *testing.T) {
	dir, ed := setupGridMapProject(t)
	macEnv := nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true, Locale: "en-US"})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800), macEnv)

	// 1. Before Review ever opens, 'x' rotates GridMap orientation.
	beforeRot := ed.GridMapOrientation()
	app.Key("x")
	afterRot := ed.GridMapOrientation()
	if afterRot == beforeRot {
		t.Fatal("pressing 'x' with Review closed did not rotate GridMap orientation")
	}

	// 2. Open a turn Review: 'x' now toggles revert inside ReviewScreenView and does NOT rotate GridMap.
	notePath := filepath.Join(dir, "scenes", "note.txt")
	if err := os.WriteFile(notePath, []byte("before\n"), 0o644); err != nil {
		t.Fatalf("write note before: %v", err)
	}
	app.Post(func() {
		if err := ed.StartTurnReview(editor.ReviewTurnInput{
			Request: "Update note",
			Files: []editor.ReviewFileInput{
				{
					Path:       "scenes/note.txt",
					BeforeYAML: []byte("before\n"),
					AfterYAML:  []byte("after\n"),
				},
			},
		}); err != nil {
			t.Errorf("StartTurnReview: %v", err)
		}
	})
	app.Settle()
	if !ed.ReviewOpen() {
		t.Fatal("Review did not open")
	}
	rotWhileOpen := ed.GridMapOrientation()
	app.Key("x")
	if ed.GridMapOrientation() != rotWhileOpen {
		t.Fatal("pressing 'x' while Review was open rotated GridMap underneath")
	}
	if ed.ReviewStatus().RevertedCount != 1 {
		t.Fatalf("pressing 'x' inside ReviewScreenView reverted %d changes, want 1", ed.ReviewStatus().RevertedCount)
	}

	// 3. Pause Review (Esc): Review bindings release immediately, so 'x' rotates GridMap again
	// and does NOT toggle revert on the paused review.
	app.Key("escape")
	if ed.ReviewOpen() || !ed.HasPendingReview() {
		t.Fatalf("expected Review paused after Esc (open=%v pending=%v)", ed.ReviewOpen(), ed.HasPendingReview())
	}
	rotBeforePausedX := ed.GridMapOrientation()
	app.Key("x")
	if ed.GridMapOrientation() == rotBeforePausedX {
		t.Fatal("pressing 'x' after pausing Review did not rotate GridMap orientation")
	}
	if ed.ReviewStatus().RevertedCount != 1 {
		t.Fatalf("pressing 'x' while Review paused changed RevertedCount to %d, want 1", ed.ReviewStatus().RevertedCount)
	}
}
