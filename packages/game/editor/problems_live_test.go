package editor_test

import (
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
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
