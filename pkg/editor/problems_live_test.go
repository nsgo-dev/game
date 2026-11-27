package editor_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/tooling/assetcheck"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const liveDoorsScene = `version: 2
name: Doors
layers:
  - name: actors
    objects:
      - id: door_3
        name: door_3
        x: 420
        "y": 220
        components:
          door: {}
`

const liveBossRoomScene = `version: 2
name: Boss Room
inherits: scenes/doors.scene.yaml
layers:
  - name: actors
    objects:
      - id: lever
        name: lever
        x: 160
        "y": 240
        components:
          lever: {}
connections:
  - from: lever
    event: pulled
    to: door_3
    handler: open
`

func liveProblemRegistry(t *testing.T) *sceneformat.ComponentRegistry {
	t.Helper()
	reg := sceneformat.NewComponentRegistry()
	if err := reg.Register(sceneformat.ComponentType{
		Name:   "lever",
		Events: []sceneformat.Event{{Name: "pulled"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(sceneformat.ComponentType{
		Name:     "door",
		Handlers: []sceneformat.Handler{{Name: "open"}},
	}); err != nil {
		t.Fatal(err)
	}
	return reg
}

func setupLiveProblemFixture(t *testing.T) (*editor.Editor, string) {
	t.Helper()
	dir := t.TempDir()
	writeFixtureFiles(t, dir, map[string]string{
		project.FileName: `version: 1
name: DungeonRun
id: dev.example.dungeonrun
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: boss_room
  scenes:
    - name: boss_room
      path: scenes/boss_room.scene.yaml
`,
		project.NSGoFileName:          "name: DungeonRun\nid: dev.example.dungeonrun\nversion: 1.0.0\n",
		"art/actors.json":             fixtureAtlasJSON,
		"scenes/doors.scene.yaml":     liveDoorsScene,
		"scenes/boss_room.scene.yaml": liveBossRoomScene,
		"scenes/other.scene.yaml":     fixtureCleanOtherScene,
	}, map[string][]byte{
		"art/actors.png": encodeTestPNG(t, 32, 16),
	})

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.ProjectAssets = true
	ed.ProblemCheckDebounce = 15 * time.Millisecond
	t.Cleanup(ed.WaitForProblems)
	if err := ed.SetComponentRegistry(liveProblemRegistry(t)); err != nil {
		t.Fatal(err)
	}
	return ed, dir
}

// TestLiveProblemsIgnoreAndReappearOnNextEdit verifies that Ignore hides a live
// problem across the Problems panel, status bar, scene tree badge, and viewport
// overlay, stays hidden when a different file is edited, and comes right back
// on a new edit to the problem's file if the problem still holds.
func TestLiveProblemsIgnoreAndReappearOnNextEdit(t *testing.T) {
	ed, dir := setupLiveProblemFixture(t)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	if ed.ProblemCount() != 0 {
		t.Fatalf("initial ProblemCount = %d, want 0", ed.ProblemCount())
	}

	// Rename door_3 -> gate in scenes/doors.scene.yaml without saving.
	if err := ed.OpenScene("scenes/doors.scene.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetObjectID("door_3", "gate"); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	probs := ed.Problems()
	if len(probs) != 1 || probs[0].File != "scenes/boss_room.scene.yaml" || probs[0].Line <= 0 || probs[0].Column <= 0 {
		t.Fatalf("live problem before save = %+v, want 1 in boss_room with line & col", probs)
	}
	prob := probs[0]

	// Switch to boss_room: lever is attributed the problem and viewport draws Problem overlay.
	if err := ed.OpenScene("scenes/boss_room.scene.yaml"); err != nil {
		t.Fatal(err)
	}
	if ed.ObjectProblemCount("lever") != 1 {
		t.Fatalf("ObjectProblemCount(lever) = %d, want 1", ed.ObjectProblemCount("lever"))
	}
	var withProb core.Drawing
	ed.DrawViewport(&withProb, core.Size{Width: 640, Height: 360})
	if !hasFillTextContaining(withProb.Ops, "door_3") {
		var texts []string
		for _, op := range withProb.Ops {
			if op.Kind == core.DrawFillText {
				texts = append(texts, op.Text)
			}
		}
		t.Fatalf("expected viewport Problem overlay pill naming door_3 on live lever object (activeScene=%v, buildErr=%v, objProbs=%+v, ops=%d, texts=%q)",
			ed.ActiveScene() != nil, ed.BuildErr, ed.ObjectProblems("lever"), len(withProb.Ops), texts)
	}

	// Ignore hides the problem everywhere.
	ed.IgnoreProblem(prob)
	if !ed.IsProblemIgnored(prob) {
		t.Fatal("IsProblemIgnored returned false after IgnoreProblem")
	}
	if ed.ProblemCount() != 0 || len(ed.Problems()) != 0 {
		t.Fatalf("problems after Ignore = %+v, want empty", ed.Problems())
	}
	if ed.ObjectProblemCount("lever") != 0 {
		t.Fatalf("ObjectProblemCount(lever) after Ignore = %d, want 0", ed.ObjectProblemCount("lever"))
	}
	var afterIgnore core.Drawing
	ed.DrawViewport(&afterIgnore, core.Size{Width: 640, Height: 360})
	if hasFillTextContaining(afterIgnore.Ops, "door_3") {
		t.Fatal("viewport Problem overlay still drew after IgnoreProblem")
	}

	// Editing a DIFFERENT file keeps the problem ignored.
	if err := ed.OpenScene("scenes/other.scene.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := ed.MoveObject("slime", 42, 24); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	if ed.ProblemCount() != 0 {
		t.Fatalf("problem reappeared after editing unrelated file: %+v", ed.Problems())
	}

	// A new edit to scenes/boss_room.scene.yaml brings the ignored problem right back!
	if err := ed.OpenScene("scenes/boss_room.scene.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := ed.MoveObject("lever", 180, 240); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	again := ed.Problems()
	if len(again) != 1 || again[0].File != "scenes/boss_room.scene.yaml" || again[0].Line <= 0 || again[0].Column <= 0 || !strings.Contains(again[0].Message, "door_3") {
		t.Fatalf("problems after editing boss_room.scene.yaml = %+v, want problem back with line & col", again)
	}
	if ed.ObjectProblemCount("lever") != 1 {
		t.Fatalf("ObjectProblemCount(lever) after re-edit = %d, want 1", ed.ObjectProblemCount("lever"))
	}
}

// TestProblemAgentPanelShowMeApplyFixUndoAndAskUI drives the full EditorView UI:
// scene tree problem badge, Agent problem card with location/explanation/diff,
// Show me, Ask about this problem…, Apply fix with toast, and Undo on the toast.
func TestProblemAgentPanelShowMeApplyFixUndoAndAskUI(t *testing.T) {
	ed, dir := setupLiveProblemFixture(t)
	var asked []editor.ProblemQuestionRequest
	ed.OnAskAboutProblem = func(req editor.ProblemQuestionRequest) {
		asked = append(asked, req)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	app.Post(func() {
		if err := ed.OpenProject(dir); err != nil {
			t.Error(err)
		}
	})
	app.WaitUntil(10*time.Second, func() bool { return !ed.CheckingProblems() })
	app.Settle()

	// Rename door_3 -> gate in doors.scene.yaml without saving, then view boss_room.
	app.Post(func() {
		if err := ed.OpenScene("scenes/doors.scene.yaml"); err != nil {
			t.Error(err)
			return
		}
		if err := ed.SetObjectID("door_3", "gate"); err != nil {
			t.Error(err)
			return
		}
	})
	app.WaitUntil(10*time.Second, func() bool {
		return !ed.CheckingProblems() && ed.ProblemCount() == 1
	})
	app.Settle()

	probs := ed.Problems()
	if len(probs) != 1 || probs[0].Column <= 0 {
		t.Fatalf("expected 1 live problem with column, got %+v", probs)
	}
	prob := probs[0]

	app.Post(func() {
		if err := ed.OpenScene("scenes/boss_room.scene.yaml"); err != nil {
			t.Error(err)
		}
	})
	app.Settle()

	// Scene tree shows the problem badge on lever, and status bar shows Problems: 1.
	if !app.Exists("id:scene-tree-problem-lever") {
		t.Fatal("expected scene-tree-problem-lever badge on lever row")
	}
	if got := strings.TrimSpace(app.Find("id:status-problems").Text); got != "Problems: 1" {
		t.Fatalf("status-problems = %q, want Problems: 1", got)
	}

	// Stage an agent-proposed fix (as MCP problem_propose_fix does).
	app.Post(func() {
		_, err := ed.ProposeProblemFix(prob,
			"The lever was wired to door_3, which was renamed to gate in your last edit.",
			editor.ProblemFixEdit{
				Kind:            editor.FixKindUpdateConnection,
				ConnectionIndex: 0,
				To:              "gate",
			},
		)
		if err != nil {
			t.Error(err)
		}
	})
	app.Settle()

	if !app.Exists("id:problem-agent-panel") {
		t.Fatal("expected problem-agent-panel after proposing fix")
	}
	if got := app.Find("id:problem-card-location").Text; got != prob.Location() {
		t.Fatalf("problem-card-location = %q, want %q", got, prob.Location())
	}

	// Press Show me -> selects lever and shows the YAML snippet line.
	app.Press("id:problem-fix-show")
	app.Settle()
	if ed.SelectedID != "lever" {
		t.Fatalf("SelectedID after Show me = %q, want lever", ed.SelectedID)
	}
	if !app.Exists("id:problem-card-snippet") {
		t.Fatal("expected problem-card-snippet after pressing Show me")
	}

	// Ask about this problem… attaches the problem.
	app.Post(func() {
		if err := ed.AskAboutProblem(prob, "Does gate have an open handler?"); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	if len(asked) != 1 || asked[0].Problem != prob || asked[0].ObjectID != "lever" || asked[0].Fix == nil {
		t.Fatalf("OnAskAboutProblem = %+v, want problem, lever, and fix attached", asked)
	}
	if !app.Exists("id:problem-question-0") {
		t.Fatal("expected submitted question to render in Agent panel")
	}

	// Press Apply fix -> applies as one undoable step, shows Undo toast, clears problem.
	app.Press("id:problem-fix-apply")
	app.WaitUntil(10*time.Second, func() bool {
		return !ed.CheckingProblems() && ed.ProblemCount() == 0
	})
	app.Settle()

	if !app.Exists("id:editor-toast") || !app.Exists("id:editor-toast-action") {
		t.Fatal("expected Spec toast with Undo action after Apply fix")
	}
	if app.Exists("id:scene-tree-problem-lever") {
		t.Fatal("scene-tree-problem-lever badge still shown after fix applied")
	}

	// Press Undo on the toast -> brings the problem back in one step!
	app.Press("id:editor-toast-action")
	app.WaitUntil(10*time.Second, func() bool {
		return !ed.CheckingProblems() && ed.ProblemCount() == 1
	})
	app.Settle()

	if !app.Exists("id:scene-tree-problem-lever") {
		t.Fatal("expected scene-tree-problem-lever badge back after Undo")
	}
}

func hasFillTextContaining(ops []core.DrawOp, substr string) bool {
	for _, op := range ops {
		if op.Kind == core.DrawFillText && strings.Contains(op.Text, substr) {
			return true
		}
	}
	return false
}

// TestLiveProblemCheckCancelAndCloseRace hammers debounced live checks,
// concurrent readers, and Editor.Close() under -race to ensure no double-close
// channel panic, no deadlock in WaitForProblems, and no data races.
func TestLiveProblemCheckCancelAndCloseRace(t *testing.T) {
	ed, dir := setupLiveProblemFixture(t)
	ed.ProblemCheckDebounce = time.Millisecond
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	stopReaders := make(chan struct{})
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stopReaders:
					return
				default:
					_ = ed.Problems()
					_ = ed.ProblemCount()
					_ = ed.CheckingProblems()
				}
			}
		}()
	}

	for i := range 30 {
		if err := ed.MoveObject("lever", float32(160+i), 240); err != nil {
			t.Fatal(err)
		}
		if i%3 == 0 {
			ed.CheckProblems()
		}
		if i%5 == 0 {
			time.Sleep(time.Millisecond)
			if err := ed.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := ed.Close(); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	close(stopReaders)
	wg.Wait()
}

// TestLiveProblemPathTraversalAndSymlinkContainment verifies that path traversal
// and symlinks escaping the project root are refused by ProposeProblemFix,
// IgnoreProblem, and OpenProblem.
func TestLiveProblemPathTraversalAndSymlinkContainment(t *testing.T) {
	ed, dir := setupLiveProblemFixture(t)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	// Create a live problem first so ProposeProblemFix reaches targetFile validation.
	if err := ed.OpenScene("scenes/doors.scene.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetObjectID("door_3", "gate"); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	probs := ed.Problems()
	if len(probs) != 1 {
		t.Fatalf("expected 1 live problem, got %+v", probs)
	}
	prob := probs[0]

	outsideDir := t.TempDir()
	outsideScene := filepath.Join(outsideDir, "outside.scene.yaml")
	if err := os.WriteFile(outsideScene, []byte(liveBossRoomScene), 0o644); err != nil {
		t.Fatal(err)
	}
	symlinkPath := filepath.Join(dir, "scenes", "escape.scene.yaml")
	if err := os.Symlink(outsideScene, symlinkPath); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}

	// Traversal in edit.File must be refused.
	if _, err := ed.ProposeProblemFix(prob, "escape", editor.ProblemFixEdit{
		Kind:            editor.FixKindUpdateConnection,
		File:            "../outside.scene.yaml",
		ConnectionIndex: 0,
		To:              "gate",
	}); err == nil {
		t.Fatal("expected ProposeProblemFix to reject path traversal in edit.File")
	}

	// Symlink escaping project root must be refused.
	if _, err := ed.ProposeProblemFix(prob, "symlink escape", editor.ProblemFixEdit{
		Kind:            editor.FixKindUpdateConnection,
		File:            "scenes/escape.scene.yaml",
		ConnectionIndex: 0,
		To:              "gate",
	}); err == nil {
		t.Fatal("expected ProposeProblemFix to reject symlink escaping project root")
	}

	// OpenProblem must refuse traversal and symlink escape.
	if err := ed.OpenProblem(assetcheck.Problem{File: "../outside.scene.yaml", Line: 1, Message: "bad"}); err == nil {
		t.Fatal("expected OpenProblem to reject path traversal")
	}
	if err := ed.OpenProblem(assetcheck.Problem{File: "scenes/escape.scene.yaml", Line: 1, Message: "bad"}); err == nil {
		t.Fatal("expected OpenProblem to reject symlink escape")
	}

	// IgnoreProblem must refuse traversal and symlink escape without recording them.
	travProb := assetcheck.Problem{File: "../outside.scene.yaml", Line: 1, Message: "bad"}
	ed.IgnoreProblem(travProb)
	if ed.IsProblemIgnored(travProb) {
		t.Fatal("IgnoreProblem recorded a path-traversal problem")
	}
	symProb := assetcheck.Problem{File: "scenes/escape.scene.yaml", Line: 1, Message: "bad"}
	ed.IgnoreProblem(symProb)
	if ed.IsProblemIgnored(symProb) {
		t.Fatal("IgnoreProblem recorded a symlink-escape problem")
	}
}

// TestLiveProblemStaleProposalHashAndCrossProjectReset verifies stale proposal
// base-hash rejection, Toast Undo tab isolation, OpenDemo reset, and same-project
// reopen reset.
func TestLiveProblemStaleProposalHashAndCrossProjectReset(t *testing.T) {
	ed, dir := setupLiveProblemFixture(t)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	if err := ed.OpenScene("scenes/doors.scene.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetObjectID("door_3", "gate"); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	probs := ed.Problems()
	if len(probs) != 1 {
		t.Fatalf("expected 1 live problem, got %+v", probs)
	}
	prob := probs[0]

	fix, err := ed.ProposeProblemFix(prob, "point lever at gate", editor.ProblemFixEdit{
		Kind:            editor.FixKindUpdateConnection,
		ConnectionIndex: 0,
		To:              "gate",
	})
	if err != nil {
		t.Fatalf("ProposeProblemFix: %v", err)
	}

	// Mutate boss_room.scene.yaml after proposing -> ApplyProposedFix must reject stale fix
	// via its baseHash check even before the background re-check finishes!
	if err := ed.OpenScene("scenes/boss_room.scene.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := ed.MoveObject("lever", 200, 240); err != nil {
		t.Fatal(err)
	}
	if err := ed.ApplyProposedFix(fix.ID); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("ApplyProposedFix immediately after file edit = %v, want stale error", err)
	}
	ed.WaitForProblems()

	// Re-propose on the updated scene, apply it, then switch tabs before triggering Toast Undo.
	probs = ed.Problems()
	if len(probs) != 1 {
		t.Fatalf("expected 1 live problem after move, got %+v", probs)
	}
	fix2, err := ed.ProposeProblemFix(probs[0], "point lever at gate", editor.ProblemFixEdit{
		Kind:            editor.FixKindUpdateConnection,
		ConnectionIndex: 0,
		To:              "gate",
	})
	if err != nil {
		t.Fatalf("re-propose: %v", err)
	}
	if err := ed.ApplyProposedFix(fix2.ID); err != nil {
		t.Fatalf("ApplyProposedFix: %v", err)
	}
	ed.WaitForProblems()
	toast, ok := ed.ActiveToast()
	if !ok || toast.OnAction == nil {
		t.Fatal("expected active Undo toast")
	}

	// Switch to another tab and make an edit there; invoking the old toast's Undo
	// must NOT undo the other tab!
	if err := ed.OpenScene("scenes/other.scene.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := ed.MoveObject("slime", 77, 88); err != nil {
		t.Fatal(err)
	}
	toast.OnAction()
	slime, _, _ := ed.FindObject("slime")
	if slime == nil || slime.X != 77 || slime.Y != 88 {
		t.Fatalf("cross-tab toast Undo unexpectedly mutated other.scene.yaml: %+v", slime)
	}

	// Ask a question and ignore a problem, then verify OpenProject(sameDir) clears all state.
	if err := ed.AskAboutProblem(probs[0], "why?"); err != nil {
		t.Fatalf("AskAboutProblem: %v", err)
	}
	ed.IgnoreProblem(probs[0])
	ed.SetProblemAgentPanelOpen(true)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("reopen same project: %v", err)
	}
	ed.WaitForProblems()
	if ed.ProblemAgentPanelOpen() || len(ed.ProposedFixes()) != 0 || len(ed.ProblemQuestions()) != 0 || ed.IsProblemIgnored(probs[0]) {
		t.Fatalf("reopening same project left stale problem state: panel=%v fixes=%d qs=%d ignored=%v",
			ed.ProblemAgentPanelOpen(), len(ed.ProposedFixes()), len(ed.ProblemQuestions()), ed.IsProblemIgnored(probs[0]))
	}

	// Create a problem + proposal + question, then verify OpenDemo() resets everything cleanly.
	if err := ed.OpenScene("scenes/doors.scene.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetObjectID("door_3", "gate"); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	probs = ed.Problems()
	if len(probs) != 1 {
		t.Fatalf("expected 1 live problem before OpenDemo, got %+v", probs)
	}
	if _, err := ed.ProposeProblemFix(probs[0], "fix before demo", editor.ProblemFixEdit{
		Kind:            editor.FixKindUpdateConnection,
		ConnectionIndex: 0,
		To:              "gate",
	}); err != nil {
		t.Fatal(err)
	}
	if err := ed.AskAboutProblem(probs[0], "demo leak?"); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenDemo(); err != nil {
		t.Fatalf("OpenDemo: %v", err)
	}
	if ed.ProblemCount() != 0 || ed.ProblemAgentPanelOpen() || len(ed.ProposedFixes()) != 0 || len(ed.ProblemQuestions()) != 0 {
		t.Fatalf("OpenDemo left stale problem state: count=%d panel=%v fixes=%d qs=%d",
			ed.ProblemCount(), ed.ProblemAgentPanelOpen(), len(ed.ProposedFixes()), len(ed.ProblemQuestions()))
	}
}

// TestLiveProblemPlayAndStartScreenGuards verifies Play-mode and StartScreen
// guards block proposal, apply, show, open, ask, ignore, and panel open.
func TestLiveProblemPlayAndStartScreenGuards(t *testing.T) {
	ed, dir := setupLiveProblemFixture(t)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	if err := ed.OpenScene("scenes/doors.scene.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetObjectID("door_3", "gate"); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	prob := ed.Problems()[0]

	// StartScreen guards.
	ed.SetStartScreenOpen(true)
	if _, err := ed.ProposeProblemFix(prob, "blocked", editor.ProblemFixEdit{Kind: editor.FixKindUpdateConnection, ConnectionIndex: 0, To: "gate"}); err == nil {
		t.Fatal("expected StartScreen to block ProposeProblemFix")
	}
	if err := ed.ApplyProposedFix(""); err == nil {
		t.Fatal("expected StartScreen to block ApplyProposedFix")
	}
	if err := ed.ShowProblem(prob); err == nil {
		t.Fatal("expected StartScreen to block ShowProblem")
	}
	if err := ed.OpenProblem(prob); err == nil {
		t.Fatal("expected StartScreen to block OpenProblem")
	}
	if err := ed.AskAboutProblem(prob, "blocked?"); err == nil {
		t.Fatal("expected StartScreen to block AskAboutProblem")
	}
	ed.IgnoreProblem(prob)
	if ed.IsProblemIgnored(prob) {
		t.Fatal("expected StartScreen to block IgnoreProblem")
	}
	ed.SetProblemAgentPanelOpen(true)
	if ed.ProblemAgentPanelOpen() {
		t.Fatal("expected StartScreen to block SetProblemAgentPanelOpen(true)")
	}
	ed.SetStartScreenOpen(false)

	// Propose while editing, then enter Play mode on a clean scene tab and verify guards + hidden panel.
	fix, err := ed.ProposeProblemFix(prob, "fix", editor.ProblemFixEdit{Kind: editor.FixKindUpdateConnection, ConnectionIndex: 0, To: "gate"})
	if err != nil {
		t.Fatalf("ProposeProblemFix: %v", err)
	}
	if err := ed.OpenScene("scenes/other.scene.yaml"); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Settle()

	if !app.Exists("id:problem-agent-panel") {
		t.Fatal("expected problem-agent-panel before Play")
	}
	app.Post(func() {
		if err := ed.Play(); err != nil {
			t.Error(err)
		}
	})
	app.Settle()

	if app.Exists("id:problem-agent-panel") {
		t.Fatal("problem-agent-panel remained visible during Play mode")
	}
	app.Post(func() {
		if _, err := ed.ProposeProblemFix(prob, "play", editor.ProblemFixEdit{Kind: editor.FixKindUpdateConnection, ConnectionIndex: 0, To: "gate"}); !errors.Is(err, editor.ErrPlaying) {
			t.Errorf("ProposeProblemFix in Play = %v, want ErrPlaying", err)
		}
		if err := ed.ApplyProposedFix(fix.ID); !errors.Is(err, editor.ErrPlaying) {
			t.Errorf("ApplyProposedFix in Play = %v, want ErrPlaying", err)
		}
		if err := ed.ShowProblem(prob); !errors.Is(err, editor.ErrPlaying) {
			t.Errorf("ShowProblem in Play = %v, want ErrPlaying", err)
		}
		if err := ed.OpenProblem(prob); !errors.Is(err, editor.ErrPlaying) {
			t.Errorf("OpenProblem in Play = %v, want ErrPlaying", err)
		}
		if err := ed.AskAboutProblem(prob, "play?"); !errors.Is(err, editor.ErrPlaying) {
			t.Errorf("AskAboutProblem in Play = %v, want ErrPlaying", err)
		}
		if err := ed.Stop(); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
}

// TestProblemAgentPanelInputFocusStabilityAndLiveThemeSwitch verifies that
// problem-ask-input keeps focus and draft text across rev updates and live
// Dark <-> Light appearance switches without remounting.
func TestProblemAgentPanelInputFocusStabilityAndLiveThemeSwitch(t *testing.T) {
	ed, dir := setupLiveProblemFixture(t)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	if err := ed.OpenScene("scenes/doors.scene.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetObjectID("door_3", "gate"); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	prob := ed.Problems()[0]
	if err := ed.OpenScene("scenes/boss_room.scene.yaml"); err != nil {
		t.Fatal(err)
	}
	if _, err := ed.ProposeProblemFix(prob, "update target to gate", editor.ProblemFixEdit{
		Kind:            editor.FixKindUpdateConnection,
		ConnectionIndex: 0,
		To:              "gate",
	}); err != nil {
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
	app.Settle()

	app.Focus("id:problem-ask-input")
	app.Settle()
	if !app.Find("id:problem-ask-input").Focused {
		t.Fatal("problem-ask-input did not receive focus")
	}
	app.Type("id:problem-ask-input", "Is gate safe?")
	app.Settle()

	// Trigger multiple rev updates (status notifications, scene selection, background re-check).
	app.Post(func() {
		ed.SelectObject("lever")
		ed.CheckProblems()
	})
	app.WaitUntil(10*time.Second, func() bool { return !ed.CheckingProblems() })
	app.Settle()

	if !app.Find("id:problem-ask-input").Focused {
		t.Fatal("problem-ask-input lost focus after rev updates / background check")
	}

	// Switch Dark -> Light -> Dark live in EditorView and verify all problem views render cleanly.
	app.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   false,
		Locale: "en-US",
	})
	app.Settle()
	if !app.Exists("id:problems-panel") || !app.Exists("id:problem-agent-panel") || !app.Exists("id:scene-tree-problem-lever") || !app.Exists("id:status-problems-pill") {
		t.Fatal("problem UI missing after live switch to Light appearance")
	}
	app.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	})
	app.Settle()
	if !app.Exists("id:problems-panel") || !app.Exists("id:problem-agent-panel") || !app.Exists("id:scene-tree-problem-lever") || !app.Exists("id:status-problems-pill") {
		t.Fatal("problem UI missing after live switch back to Dark appearance")
	}

	// Standalone ProblemAgentPanelView + ProblemsView mount: verify live Dark <-> Light switches
	// repaint reactively via StyleFunc without remounting problem-ask-input or dropping its focus.
	standalone := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetTheme(editor.SpecTheme())
		rev := core.NewSignal(cx, uint64(0))
		unsub := ed.Subscribe(func() { rev.Set(rev.GetUntracked() + 1) })
		cx.Cleanup(unsub)
		return core.View{
			Style: core.Style{Direction: core.Row, Width: core.Pct(100), Height: core.Pct(100)},
			Children: []core.Node{
				core.View{
					Style:    core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), MinWidth: core.Pt(0), Overflow: core.Hidden},
					Children: []core.Node{editor.ProblemsView(cx, ed, rev)},
				},
				editor.ProblemAgentPanelView(cx, ed, rev),
			},
		}
	}, nsgotest.Size(900, 600), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))
	standalone.Settle()
	standalone.Focus("id:problem-ask-input")
	standalone.Settle()
	if !standalone.Find("id:problem-ask-input").Focused {
		t.Fatal("standalone problem-ask-input did not receive focus")
	}
	standalone.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   false,
		Locale: "en-US",
	})
	standalone.Settle()
	if !standalone.Find("id:problem-ask-input").Focused {
		t.Fatal("standalone problem-ask-input lost focus on live Dark -> Light switch")
	}
	standalone.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	})
	standalone.Settle()
	if !standalone.Find("id:problem-ask-input").Focused {
		t.Fatal("standalone problem-ask-input lost focus on live Light -> Dark switch")
	}
}

// TestProposedFixSetObjectIDSimulationParity verifies that FixKindSetObjectID
// matches Editor.SetObjectID: it rejects duplicate IDs at propose time and
// includes rewritten local connection targets in the previewed diff matching
// the applied YAML 1:1.
func TestProposedFixSetObjectIDSimulationParity(t *testing.T) {
	dir := t.TempDir()
	const sceneWithDuplicateCheck = `version: 2
name: RenameParity
layers:
  - name: actors
    objects:
      - id: lever
        name: lever
        x: 100
        "y": 100
        sprite:
          atlas: actors
          frame: missing_frame
        components:
          lever: {}
      - id: door_3
        name: door_3
        x: 300
        "y": 100
        components:
          door: {}
connections:
  - from: lever
    event: pulled
    to: door_3
    handler: open
`
	writeFixtureFiles(t, dir, map[string]string{
		project.FileName: `version: 1
name: RenameParity
id: dev.example.renameparity
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: main
  scenes:
    - name: main
      path: scenes/main.scene.yaml
`,
		project.NSGoFileName:     "name: RenameParity\nid: dev.example.renameparity\nversion: 1.0.0\n",
		"art/actors.json":        fixtureAtlasJSON,
		"scenes/main.scene.yaml": sceneWithDuplicateCheck,
	}, map[string][]byte{
		"art/actors.png": encodeTestPNG(t, 32, 16),
	})

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.ProjectAssets = true
	ed.ProblemCheckDebounce = 15 * time.Millisecond
	t.Cleanup(func() { _ = ed.Close() })
	if err := ed.SetComponentRegistry(liveProblemRegistry(t)); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	probs := ed.Problems()
	if len(probs) == 0 {
		t.Fatal("expected missing_frame problem in main.scene.yaml")
	}

	// Duplicate ID must be rejected during simulation, matching SetObjectID.
	if _, err := ed.ProposeProblemFix(probs[0], "duplicate rename", editor.ProblemFixEdit{
		Kind:     editor.FixKindSetObjectID,
		ObjectID: "lever",
		NewID:    "door_3",
	}); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("ProposeProblemFix duplicate ID = %v, want duplicate error", err)
	}

	// Renaming lever -> switch_1 must rewrite the connection's `from: switch_1` in the previewed diff too!
	fix, err := ed.ProposeProblemFix(probs[0], "rename lever to switch_1", editor.ProblemFixEdit{
		Kind:     editor.FixKindSetObjectID,
		ObjectID: "lever",
		NewID:    "switch_1",
	})
	if err != nil {
		t.Fatalf("ProposeProblemFix rename: %v", err)
	}
	sawConnRewrite := false
	for _, dl := range fix.Diff {
		if dl.Kind == "added" && strings.Contains(dl.Text, "from: switch_1") {
			sawConnRewrite = true
		}
	}
	if !sawConnRewrite {
		t.Fatalf("FixKindSetObjectID diff omitted connection rewrite: %+v", fix.Diff)
	}

	if err := ed.ApplyProposedFix(fix.ID); err != nil {
		t.Fatalf("ApplyProposedFix: %v", err)
	}
	gotYAML, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	if !strings.Contains(string(gotYAML), "id: switch_1") || !strings.Contains(string(gotYAML), "from: switch_1") {
		t.Fatalf("applied YAML mismatch:\n%s", gotYAML)
	}
}
