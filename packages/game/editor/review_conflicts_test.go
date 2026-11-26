package editor_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const reviewConflictBaseScene = `version: 3
name: level_01
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    space: world
    order: 0
    objects:
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

const reviewConflictAgentScene = `version: 3
name: level_01
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    space: world
    order: 0
    objects:
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
      - id: slime_03
        prefab: prefabs/slime.prefab.yaml
        x: 452
        y: 208
`

func writeReviewConflictFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

func newReviewConflictFixture(t *testing.T) (*editor.Editor, string) {
	t.Helper()
	dir := t.TempDir()
	writeReviewConflictFile(t, filepath.Join(dir, "game.yaml"), `version: 1
name: conflict-demo
id: dev.nsgo.conflict-demo
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: level_01
  scenes:
    - name: level_01
      path: scenes/level_01.scene.yaml
`)
	writeReviewConflictFile(t, filepath.Join(dir, "scenes", "level_01.scene.yaml"), reviewConflictBaseScene)
	writeReviewConflictFile(t, filepath.Join(dir, "prefabs", "slime.prefab.yaml"), "version: 1\nid: slime\nname: Slime\n")

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	ed.WaitForProblems()
	return ed, dir
}

func startReviewConflictTurn(t *testing.T, ed *editor.Editor) {
	t.Helper()
	if err := ed.StartTurnReview(editor.ReviewTurnInput{
		Request: "Put slime patrols on the lower floors with a coin trail toward door_1.",
		Files: []editor.ReviewFileInput{
			{
				Path:       "scenes/level_01.scene.yaml",
				BeforeYAML: []byte(reviewConflictBaseScene),
				AfterYAML:  []byte(reviewConflictAgentScene),
				Reasons: map[string]string{
					"door_1":   "The trail stays hidden until the door opens.",
					"slime_03": "Guards the coin trail from the upper plank.",
					"knight":   "Slimes draw at z 11; keep Knight above them.",
					"crate_04": "Blocked the coin trail.",
				},
			},
		},
	}); err != nil {
		t.Fatalf("StartTurnReview: %v", err)
	}
}

func findSnapChange(snap editor.ReviewSnapshot, objID string) *editor.ReviewChange {
	for i := range snap.Changes {
		if snap.Changes[i].ObjectID == objID {
			return &snap.Changes[i]
		}
	}
	return nil
}

// #1032 Acceptance 1: edit door_1 by hand while a turn changes the same list, check that Keep is refused
// with the blocker, pick Both, keep, and find both entries in the saved YAML, yours first, and verify
// one Undo restores everything.
func TestReviewConflictDoorListMergeBothYoursFirst(t *testing.T) {
	ed, dir := newReviewConflictFixture(t)
	startReviewConflictTurn(t, ed)

	// Person edits door_1's on_open list by hand while the turn is pending.
	if err := ed.SetProperty("door_1", "on_open", []any{
		map[string]any{"open": true},
		map[string]any{"sound": "sfx/door_creak.ogg"},
	}); err != nil {
		t.Fatalf("SetProperty door_1 on_open: %v", err)
	}

	snap := ed.ReviewStatus()
	doorChange := findSnapChange(snap, "door_1")
	if doorChange == nil {
		t.Fatal("missing review change for door_1")
	}
	if len(doorChange.Conflicts) != 1 {
		t.Fatalf("door_1 conflicts = %d, want 1", len(doorChange.Conflicts))
	}
	c := doorChange.Conflicts[0]
	if !c.IsList || c.Field != "properties.on_open" {
		t.Fatalf("unexpected conflict metadata: %+v", c)
	}
	if len(c.MineLines) == 0 || c.MineLines[0].Author != editor.ReviewAuthorYou || !strings.Contains(c.MineLines[0].Text, "sfx/door_creak.ogg") {
		t.Fatalf("MineLines = %+v, want you + sfx/door_creak.ogg", c.MineLines)
	}
	if len(c.AgentLines) == 0 || c.AgentLines[0].Author != editor.ReviewAuthorAgent || !strings.Contains(c.AgentLines[0].Text, "coin_trail") {
		t.Fatalf("AgentLines = %+v, want agent + coin_trail", c.AgentLines)
	}

	// Focus a different change so we verify Keep jumps directly to door_1.
	if !ed.FocusReviewChange("slime_03") {
		t.Fatal("FocusReviewChange(slime_03) failed")
	}

	// Keep MUST be refused with "Choose how door_1 merges" and jump focus to door_1.
	err := ed.KeepTurnReview()
	if err == nil {
		t.Fatal("KeepTurnReview succeeded despite unresolved conflict on door_1")
	}
	var blocked *editor.ReviewBlockedError
	if !errors.As(err, &blocked) || !errors.Is(err, editor.ErrReviewBlocked) {
		t.Fatalf("KeepTurnReview error = %T (%v), want *ReviewBlockedError wrapping ErrReviewBlocked", err, err)
	}
	if blocked.Blocker.Reason != "Choose how door_1 merges" {
		t.Fatalf("blocker reason = %q, want %q", blocked.Blocker.Reason, "Choose how door_1 merges")
	}
	if got := ed.ReviewStatus().FocusedID; got != doorChange.ID {
		t.Fatalf("focused change after blocked Keep = %q, want %q", got, doorChange.ID)
	}

	// Pick Both -> blocker clears.
	if err := ed.ResolveReviewConflict("door_1", editor.ReviewConflictBoth); err != nil {
		t.Fatalf("ResolveReviewConflict Both: %v", err)
	}
	if blockers := ed.ReviewBlockers(); len(blockers) != 0 {
		t.Fatalf("expected blockers cleared after choosing Both, got %+v", blockers)
	}

	// Verify YAML lines tag both sides accurately ("you" first, then "agent").
	doorChange = findSnapChange(ed.ReviewStatus(), "door_1")
	youIdx, agentIdx := -1, -1
	for i, ln := range doorChange.YAMLLines {
		if strings.Contains(ln.Text, "sfx/door_creak.ogg") && ln.Author == editor.ReviewAuthorYou {
			youIdx = i
		}
		if strings.Contains(ln.Text, "coin_trail") && ln.Author == editor.ReviewAuthorAgent {
			agentIdx = i
		}
	}
	if youIdx < 0 || agentIdx < 0 || youIdx >= agentIdx {
		t.Fatalf("expected you line before agent line in YAML pane, got youIdx=%d agentIdx=%d lines=%+v", youIdx, agentIdx, doorChange.YAMLLines)
	}

	// Keep the review -> saved YAML on disk has both entries, yours first.
	if err := ed.KeepTurnReview(); err != nil {
		t.Fatalf("KeepTurnReview after Both: %v", err)
	}
	savedBytes, err := os.ReadFile(filepath.Join(dir, "scenes", "level_01.scene.yaml"))
	if err != nil {
		t.Fatalf("read saved scene: %v", err)
	}
	savedStr := string(savedBytes)
	posYours := strings.Index(savedStr, "sfx/door_creak.ogg")
	posAgent := strings.Index(savedStr, "coin_trail")
	if posYours < 0 || posAgent < 0 {
		t.Fatalf("saved YAML missing merged entries:\n%s", savedStr)
	}
	if posYours >= posAgent {
		t.Fatalf("expected yours (%d) before agent's (%d) in saved YAML:\n%s", posYours, posAgent, savedStr)
	}
	if obj, _, _ := ed.FindObject("slime_03"); obj == nil {
		t.Fatal("expected slime_03 present after Keep")
	}

	// One Undo reverts the kept turn (agent additions + merge result) in a single step,
	// preserving the person's own manual edit.
	if !ed.Undo() {
		t.Fatal("expected Undo to revert kept review turn")
	}
	if obj, _, _ := ed.FindObject("slime_03"); obj != nil {
		t.Fatal("expected slime_03 removed after one Undo")
	}
	afterUndoBytes, err := os.ReadFile(filepath.Join(dir, "scenes", "level_01.scene.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(afterUndoBytes), "coin_trail") {
		t.Fatalf("expected agent's coin_trail undone in one step:\n%s", string(afterUndoBytes))
	}
	if !strings.Contains(string(afterUndoBytes), "sfx/door_creak.ogg") {
		t.Fatalf("expected person's manual door_creak edit preserved after undoing turn:\n%s", string(afterUndoBytes))
	}
}

func TestReviewConflictMineAndAgentChoices(t *testing.T) {
	for _, tc := range []struct {
		name      string
		choice    editor.ReviewConflictChoice
		wantHas   string
		wantLacks string
	}{
		{"mine", editor.ReviewConflictMine, "sfx/door_creak.ogg", "coin_trail"},
		{"agent", editor.ReviewConflictAgent, "coin_trail", "sfx/door_creak.ogg"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ed, dir := newReviewConflictFixture(t)
			startReviewConflictTurn(t, ed)
			if err := ed.SetProperty("door_1", "on_open", []any{
				map[string]any{"open": true},
				map[string]any{"sound": "sfx/door_creak.ogg"},
			}); err != nil {
				t.Fatal(err)
			}
			if err := ed.ResolveReviewConflict("door_1", tc.choice); err != nil {
				t.Fatal(err)
			}
			if err := ed.KeepTurnReview(); err != nil {
				t.Fatal(err)
			}
			saved, err := os.ReadFile(filepath.Join(dir, "scenes", "level_01.scene.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			s := string(saved)
			if !strings.Contains(s, tc.wantHas) {
				t.Fatalf("expected %q in saved YAML:\n%s", tc.wantHas, s)
			}
			if strings.Contains(s, tc.wantLacks) {
				t.Fatalf("did not expect %q in saved YAML:\n%s", tc.wantLacks, s)
			}
		})
	}
}

func TestReviewDifferentFieldsAutoMergeWithoutConflict(t *testing.T) {
	ed, dir := newReviewConflictFixture(t)
	startReviewConflictTurn(t, ed)

	// Agent edited door_1.properties.on_open; person moves door_1's x coordinate (a different field).
	if err := ed.MoveObject("door_1", 590, 248); err != nil {
		t.Fatal(err)
	}

	doorChange := findSnapChange(ed.ReviewStatus(), "door_1")
	if len(doorChange.Conflicts) != 0 {
		t.Fatalf("expected 0 conflicts when editing different fields, got %+v", doorChange.Conflicts)
	}
	if len(ed.ReviewBlockers()) != 0 {
		t.Fatalf("expected no Keep blocker, got %+v", ed.ReviewBlockers())
	}
	if err := ed.KeepTurnReview(); err != nil {
		t.Fatalf("KeepTurnReview failed: %v", err)
	}
	door, _, _ := ed.FindObject("door_1")
	if door == nil || door.X != 590 {
		t.Fatalf("door_1.X = %v, want 590", door)
	}
	saved, err := os.ReadFile(filepath.Join(dir, "scenes", "level_01.scene.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "coin_trail") {
		t.Fatalf("expected agent's on_open edit preserved alongside person's move:\n%s", string(saved))
	}
}

func TestReviewHandEditYAMLParseErrorFixAndUndo(t *testing.T) {
	ed, dir := newReviewConflictFixture(t)
	startReviewConflictTurn(t, ed)

	if err := ed.BeginReviewYAMLEdit("slime_03"); err != nil {
		t.Fatalf("BeginReviewYAMLEdit: %v", err)
	}
	slime := findSnapChange(ed.ReviewStatus(), "slime_03")
	if slime == nil || !slime.EditingYAML {
		t.Fatal("expected slime_03 in EditingYAML mode")
	}

	// Type invalid YAML: replace "x: 452" with "x 444" (missing colon).
	brokenYAML := strings.Replace(slime.HandEditYAML, "x: 452", "x 444", 1)
	if brokenYAML == slime.HandEditYAML {
		t.Fatalf("fixture did not contain x: 452:\n%s", slime.HandEditYAML)
	}
	if err := ed.SetReviewYAMLDraft("slime_03", brokenYAML); err == nil {
		t.Fatal("expected SetReviewYAMLDraft to fail on missing colon")
	}

	slime = findSnapChange(ed.ReviewStatus(), "slime_03")
	if slime.YAMLError == nil {
		t.Fatal("expected YAMLError set on slime_03")
	}
	if !strings.Contains(slime.YAMLError.Title, "isn't valid YAML") {
		t.Fatalf("unexpected error title: %q", slime.YAMLError.Title)
	}
	if slime.YAMLError.Fix == nil || slime.YAMLError.Fix.Label != "Add the colon" {
		t.Fatalf("expected one-step fix 'Add the colon', got %+v", slime.YAMLError.Fix)
	}

	// Header blocker names the failing line.
	blockers := ed.ReviewBlockers()
	if len(blockers) != 1 || blockers[0].Kind != "yaml_error" || blockers[0].Line != slime.YAMLError.Line {
		t.Fatalf("unexpected blockers: %+v", blockers)
	}
	if !strings.HasPrefix(blockers[0].Reason, "Fix YAML on line ") {
		t.Fatalf("blocker reason = %q, want prefix 'Fix YAML on line '", blockers[0].Reason)
	}

	// Done and Keep are both refused while YAML is invalid.
	if err := ed.FinishReviewYAMLEdit("slime_03"); !errors.Is(err, editor.ErrReviewYAMLInvalid) {
		t.Fatalf("FinishReviewYAMLEdit err = %v, want ErrReviewYAMLInvalid", err)
	}
	if err := ed.KeepTurnReview(); !errors.Is(err, editor.ErrReviewBlocked) {
		t.Fatalf("KeepTurnReview err = %v, want ErrReviewBlocked", err)
	}

	// Apply the offered one-step fix.
	if err := ed.ApplyReviewYAMLFix("slime_03"); err != nil {
		t.Fatalf("ApplyReviewYAMLFix: %v", err)
	}
	slime = findSnapChange(ed.ReviewStatus(), "slime_03")
	if slime.YAMLError != nil {
		t.Fatalf("expected YAMLError cleared after fix, got %+v", slime.YAMLError)
	}
	if len(ed.ReviewBlockers()) != 0 {
		t.Fatalf("expected blockers cleared after fix, got %+v", ed.ReviewBlockers())
	}

	// Live effective scene doc updates immediately as soon as YAML parses.
	effDoc := ed.ReviewEffectiveSceneDoc("scenes/level_01.scene.yaml")
	if effDoc == nil {
		t.Fatal("ReviewEffectiveSceneDoc returned nil")
	}
	var effSlime *sceneformat.ObjectDoc
	for i := range effDoc.Layers[0].Objects {
		if effDoc.Layers[0].Objects[i].ID == "slime_03" {
			effSlime = &effDoc.Layers[0].Objects[i]
		}
	}
	if effSlime == nil || effSlime.X != 444 {
		t.Fatalf("live viewport slime_03 = %+v, want X=444", effSlime)
	}

	// Edited line is tagged "you".
	foundYou := false
	for _, ln := range slime.YAMLLines {
		if strings.Contains(ln.Text, "x: 444") && ln.Author == editor.ReviewAuthorYou {
			foundYou = true
			break
		}
	}
	if !foundYou {
		t.Fatalf("expected x: 444 line tagged 'you' in YAMLLines: %+v", slime.YAMLLines)
	}

	// Done succeeds now, Keep lands x: 444 on disk, and one Undo reverts the whole turn.
	if err := ed.FinishReviewYAMLEdit("slime_03"); err != nil {
		t.Fatalf("FinishReviewYAMLEdit after fix: %v", err)
	}
	if err := ed.KeepTurnReview(); err != nil {
		t.Fatalf("KeepTurnReview after fix: %v", err)
	}
	keptSlime, _, _ := ed.FindObject("slime_03")
	if keptSlime == nil || keptSlime.X != 444 {
		t.Fatalf("kept slime_03 = %+v, want X=444", keptSlime)
	}
	savedBytes, err := os.ReadFile(filepath.Join(dir, "scenes", "level_01.scene.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(savedBytes), "x: 444") {
		t.Fatalf("saved scene YAML missing x: 444:\n%s", string(savedBytes))
	}
	if !ed.Undo() {
		t.Fatal("expected single Undo to revert turn including hand edit")
	}
	if obj, _, _ := ed.FindObject("slime_03"); obj != nil {
		t.Fatal("expected slime_03 removed after single Undo")
	}
}

func TestReviewYAMLValidationRefusesUnknownFieldsDuplicateKeysOversizedAndYouOnly(t *testing.T) {
	ed, _ := newReviewConflictFixture(t)
	startReviewConflictTurn(t, ed)

	if err := ed.BeginReviewYAMLEdit("slime_03"); err != nil {
		t.Fatal(err)
	}
	baseYAML := findSnapChange(ed.ReviewStatus(), "slime_03").HandEditYAML

	// Unknown field must be refused.
	if err := ed.SetReviewYAMLDraft("slime_03", baseYAML+"\n    unknownBogusField: 123"); err == nil {
		t.Fatal("expected unknown field to be refused")
	}
	// Duplicate key must be refused.
	if err := ed.SetReviewYAMLDraft("slime_03", baseYAML+"\n    x: 100"); err == nil {
		t.Fatal("expected duplicate key to be refused")
	}
	// Oversized YAML draft (>256 KiB) must be refused.
	if err := ed.SetReviewYAMLDraft("slime_03", baseYAML+"\n    name: "+strings.Repeat("a", 300<<10)); err == nil {
		t.Fatal("expected oversized YAML draft to be refused")
	}
	// NUL byte must be refused.
	if err := ed.SetReviewYAMLDraft("slime_03", baseYAML+"\x00"); err == nil {
		t.Fatal("expected NUL byte in YAML draft to be refused")
	}
	// Undo my typing restores clean state.
	if err := ed.UndoReviewYAMLTyping("slime_03"); err != nil {
		t.Fatalf("UndoReviewYAMLTyping: %v", err)
	}
	slime := findSnapChange(ed.ReviewStatus(), "slime_03")
	if slime.YAMLError != nil || slime.EditingYAML || slime.HandEdited {
		t.Fatalf("expected clean state after Undo my typing: err=%v editing=%v handEdited=%v", slime.YAMLError, slime.EditingYAML, slime.HandEdited)
	}
	if len(ed.ReviewBlockers()) != 0 {
		t.Fatalf("expected blockers cleared after Undo my typing, got %+v", ed.ReviewBlockers())
	}
}

func TestReviewUIConflictAndYAMLEditInteractionAndFocusStability(t *testing.T) {
	ed, _ := newReviewConflictFixture(t)
	startReviewConflictTurn(t, ed)

	if err := ed.SetProperty("door_1", "on_open", []any{
		map[string]any{"open": true},
		map[string]any{"sound": "sfx/door_creak.ogg"},
	}); err != nil {
		t.Fatal(err)
	}

	macEnv := nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true, Locale: "en-US"})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), macEnv)

	// Blocker pill jumps to door_1; pressing Both resolves the conflict.
	app.Press("id:review-blocker-pill")
	doorChange := findSnapChange(ed.ReviewStatus(), "door_1")
	if ed.ReviewStatus().FocusedID != doorChange.ID {
		t.Fatalf("expected blocker press to focus door_1 (%s), got %s", doorChange.ID, ed.ReviewStatus().FocusedID)
	}
	app.Press("id:review-conflict-both-door_1")
	if len(ed.ReviewBlockers()) != 0 {
		t.Fatalf("expected blockers cleared after pressing Both, got %+v", ed.ReviewBlockers())
	}

	// Focus slime_03, click Edit, verify YAML input mounts stably and does not let single-letter Review shortcuts fire while typing.
	slimeChange := findSnapChange(ed.ReviewStatus(), "slime_03")
	app.Press("id:review-card-focus-" + slimeChange.ID)
	app.Press("id:review-yaml-edit")
	app.Settle()
	if !app.Exists("id:review-yaml-input") {
		t.Fatal("expected review-yaml-input mounted after pressing Edit")
	}
	origSlimeYAML := findSnapChange(ed.ReviewStatus(), "slime_03").HandEditYAML
	app.Press("id:review-yaml-input")
	app.Settle()
	app.Key("x")
	app.Key("j")
	app.Key("k")
	app.Key("b")
	app.Settle()
	if cur := findSnapChange(ed.ReviewStatus(), "slime_03"); cur == nil || cur.Reverted {
		t.Fatalf("typing 'x' in review-yaml-input must not revert slime_03: %+v", cur)
	}
	if ed.ReviewStatus().FocusedID != slimeChange.ID {
		t.Fatalf("typing 'j'/'k' in review-yaml-input must not move focus away from slime_03")
	}
	if ed.ReviewPeekingBefore() {
		t.Fatal("typing 'b' in review-yaml-input must not activate peek-before")
	}

	// Inject broken YAML ("x 444" missing colon), press Fix, press Done, press Keep.
	app.Post(func() {
		_ = ed.SetReviewYAMLDraft("slime_03", strings.Replace(origSlimeYAML, "x: 452", "x 444", 1))
	})
	app.Settle()
	if !app.Exists("id:review-yaml-error") {
		t.Fatal("expected review-yaml-error alert card mounted")
	}
	app.Press("id:review-yaml-fix")
	app.Settle()
	app.Press("id:review-yaml-done")
	app.Settle()
	app.Press("id:review-keep")
	app.Settle()

	if ed.ReviewOpen() || ed.HasPendingReview() {
		t.Fatal("expected review closed and kept")
	}
	if obj, _, _ := ed.FindObject("slime_03"); obj == nil || obj.X != 444 {
		t.Fatalf("slime_03 after UI Keep = %+v, want X=444", obj)
	}
}
