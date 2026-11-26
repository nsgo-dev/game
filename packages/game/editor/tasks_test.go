package editor_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func newTasksTestScene() *sceneformat.SceneDoc {
	return &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "level_01",
		VirtualWidth:  640,
		VirtualHeight: 360,
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "Actors",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{ID: "player", Name: "Player", X: 100, Y: 240},
					{ID: "slime_01", Name: "slime_01", X: 196, Y: 260},
					{ID: "slime_02", Name: "slime_02", X: 292, Y: 260},
					{ID: "slime_03", Name: "slime_03", X: 440, Y: 210},
				},
			},
		},
	}
}

// #1022 Acceptance: A queued request starts only after the running one is kept or reverted.
func TestQueuedTaskStartsOnlyAfterRunningIsKeptOrReverted(t *testing.T) {
	now := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	ed, err := editor.New(newTasksTestScene(), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.Now = func() time.Time { return now }

	started := make(chan string, 4)
	release := make(chan struct{})
	ed.AgentTaskRunner = func(ctx context.Context, task editor.AgentTask) error {
		started <- task.ID
		if task.ID == "t1" {
			<-release
		}
		return nil
	}

	// Start task 1 (running).
	t1, err := ed.SubmitTask(editor.TaskRequest{
		ID:        "t1",
		Title:     "slime_02 jumps the gap",
		StartMode: editor.TaskStartQueue,
	})
	if err != nil {
		t.Fatalf("SubmitTask t1: %v", err)
	}
	if t1.State != editor.TaskRunning {
		t.Fatalf("t1.State = %s, want running", t1.State)
	}
	if got := <-started; got != "t1" {
		t.Fatalf("first started = %q, want t1", got)
	}

	// Queue task 2 behind task 1.
	t2, err := ed.SubmitTask(editor.TaskRequest{
		ID:        "t2",
		Title:     "Coin pickup sound a little higher",
		StartMode: editor.TaskStartQueue,
	})
	if err != nil {
		t.Fatalf("SubmitTask t2: %v", err)
	}
	if t2.State != editor.TaskQueued || t2.StartsAfterTaskID != "t1" {
		t.Fatalf("t2 = %+v, want queued after t1", t2)
	}

	// Let task 1 finish its run -> moves to Ready to review.
	// Task 2 MUST NOT start yet because task 1 has neither been kept nor reverted!
	close(release)
	deadline := time.After(2 * time.Second)
	for {
		cur, _ := ed.Task("t1")
		if cur.State == editor.TaskReadyToReview {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("t1 did not reach ready_to_review, state=%s", cur.State)
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if cur2, _ := ed.Task("t2"); cur2.State != editor.TaskQueued {
		t.Fatalf("t2 started before t1 was kept or reverted! state=%s", cur2.State)
	}
	select {
	case id := <-started:
		t.Fatalf("unexpected task started while t1 unreviewed: %q", id)
	default:
	}

	// Keeping t1 immediately starts t2.
	if err := ed.KeepTask("t1"); err != nil {
		t.Fatalf("KeepTask t1: %v", err)
	}
	select {
	case got := <-started:
		if got != "t2" {
			t.Fatalf("started = %q, want t2", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("t2 did not start after KeepTask(t1)")
	}
	if cur1, _ := ed.Task("t1"); cur1.State != editor.TaskDone || cur1.Review == nil || !cur1.Review.Kept {
		t.Fatalf("t1 after keep = %+v", cur1)
	}

	// Verify the Revert path also starts a queued task (and reverts unkept scene edits).
	ed2, err := editor.New(newTasksTestScene(), nil)
	if err != nil {
		t.Fatalf("editor.New(2): %v", err)
	}
	ed2.Now = func() time.Time { return now }
	beforeYAML, err := ed2.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	a1, err := ed2.SubmitTask(editor.TaskRequest{ID: "a1", Title: "Move slime_02", StartMode: editor.TaskStartQueue})
	if err != nil || a1.State != editor.TaskRunning {
		t.Fatalf("a1 = %+v, err=%v", a1, err)
	}
	if err := ed2.MoveObject("slime_02", 500, 120); err != nil {
		t.Fatalf("MoveObject: %v", err)
	}
	a2, err := ed2.SubmitTask(editor.TaskRequest{ID: "a2", Title: "Follow-up", StartMode: editor.TaskStartQueue})
	if err != nil || a2.State != editor.TaskQueued {
		t.Fatalf("a2 = %+v, err=%v", a2, err)
	}
	if err := ed2.CompleteTaskTurn("a1", editor.TaskReview{
		ID:              "rev-a1",
		TaskID:          "a1",
		BeforeSceneYAML: string(beforeYAML),
		Changes:         []editor.TaskReviewChange{{ObjectID: "slime_02", Kind: "changed", Summary: "move slime_02"}},
	}); err != nil {
		t.Fatalf("CompleteTaskTurn a1: %v", err)
	}
	if cur2, _ := ed2.Task("a2"); cur2.State != editor.TaskQueued {
		t.Fatalf("a2 should still be queued before revert, got %s", cur2.State)
	}
	if err := ed2.RevertTask("a1"); err != nil {
		t.Fatalf("RevertTask a1: %v", err)
	}
	obj, _, _ := ed2.FindObject("slime_02")
	if obj == nil || obj.X != 292 || obj.Y != 260 {
		t.Fatalf("RevertTask did not restore slime_02: %+v", obj)
	}
	if cur2, _ := ed2.Task("a2"); cur2.State != editor.TaskRunning {
		t.Fatalf("a2 should start running after RevertTask(a1), got %s", cur2.State)
	}
}

// #1022 Acceptance: Dismissing several Ready to review tasks reverts nothing that was kept.
func TestDismissingSeveralReadyToReviewTasksRevertsNothingKept(t *testing.T) {
	ed, err := editor.New(newTasksTestScene(), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	beforeYAML, _ := ed.SaveYAML()

	// Task 1 edits slime_02 and is KEPT.
	if _, err := ed.SubmitTask(editor.TaskRequest{ID: "t1", Title: "Keep slime_02 move", StartMode: editor.TaskStartQueue}); err != nil {
		t.Fatalf("SubmitTask t1: %v", err)
	}
	if err := ed.MoveObject("slime_02", 340, 260); err != nil {
		t.Fatalf("MoveObject: %v", err)
	}
	if err := ed.CompleteTaskTurn("t1", editor.TaskReview{
		ID:              "rev-1",
		TaskID:          "t1",
		BeforeSceneYAML: string(beforeYAML),
		Changes:         []editor.TaskReviewChange{{ObjectID: "slime_02", Kind: "changed"}},
	}); err != nil {
		t.Fatalf("CompleteTaskTurn t1: %v", err)
	}
	if err := ed.KeepTask("t1"); err != nil {
		t.Fatalf("KeepTask t1: %v", err)
	}

	histIdxAfterKeep := ed.History.Index()
	keptSceneYAML, _ := ed.SaveYAML()

	// Seed two Ready to review automation tasks and one Ready to review user task.
	for _, req := range []editor.TaskRequest{
		{
			ID:           "t4",
			Title:        "Nightly: fix game check warnings",
			Source:       editor.TaskSourceAutomation,
			StartMode:    editor.TaskStartQueue,
			ScenePath:    "level_02",
			InitialState: editor.TaskReadyToReview,
			Review:       &editor.TaskReview{ID: "rev-4", TaskID: "t4", ChangeCount: 3},
		},
		{
			ID:           "t5",
			Title:        "Translate HUD strings to Japanese",
			Source:       editor.TaskSourceAutomation,
			StartMode:    editor.TaskStartQueue,
			ScenePath:    "locale/ja.yaml",
			InitialState: editor.TaskReadyToReview,
			Review:       &editor.TaskReview{ID: "rev-5", TaskID: "t5", ChangeCount: 12},
		},
	} {
		if _, err := ed.SubmitTask(req); err != nil {
			t.Fatalf("SubmitTask %s: %v", req.ID, err)
		}
	}

	ed.ToggleTaskDismissSelection("t4")
	ed.ToggleTaskDismissSelection("t5")
	if got := ed.SelectedDismissTaskIDs(); len(got) != 2 {
		t.Fatalf("SelectedDismissTaskIDs = %v, want 2", got)
	}
	if n := ed.DismissSelectedTasks(); n != 2 {
		t.Fatalf("DismissSelectedTasks = %d, want 2", n)
	}

	// Both Ready to review tasks are dismissed, and nothing kept was reverted.
	if s := ed.TaskSummary(); s.ReadyToReview != 0 {
		t.Fatalf("ReadyToReview after bulk dismiss = %d, want 0", s.ReadyToReview)
	}
	if ed.History.Index() != histIdxAfterKeep {
		t.Fatalf("History.Index changed from %d to %d on DismissSelectedTasks", histIdxAfterKeep, ed.History.Index())
	}
	afterYAML, _ := ed.SaveYAML()
	if !bytes.Equal(afterYAML, keptSceneYAML) {
		t.Fatalf("DismissSelectedTasks altered kept scene:\ngot:\n%s\nwant:\n%s", afterYAML, keptSceneYAML)
	}
	obj, _, _ := ed.FindObject("slime_02")
	if obj == nil || obj.X != 340 || obj.Y != 260 {
		t.Fatalf("kept slime_02 edit was reverted: %+v", obj)
	}
	if cur1, _ := ed.Task("t1"); cur1.Review == nil || !cur1.Review.Kept || cur1.Review.Reverted {
		t.Fatalf("t1 review state corrupted: %+v", cur1.Review)
	}
}

// #1022: Thread YAML persistence round-trips deterministically and refuses unknown fields.
func TestAgentThreadYAMLPersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)

	ed, err := editor.New(newTasksTestScene(), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.Now = func() time.Time { return now }
	ed.ThreadsDir = dir

	_, err = ed.SubmitTask(editor.TaskRequest{
		ID:               "t3",
		Title:            "Torches around the boss arena",
		Source:           editor.TaskSourcePlan,
		PlanStep:         2,
		PlanTotal:        5,
		StartMode:        editor.TaskStartQueue,
		ScenePath:        "boss_room",
		PermissionPrompt: "wants to run the game",
		InitialState:     editor.TaskNeedsYou,
		TimeLabel:        "1:12",
	})
	if err != nil {
		t.Fatalf("SubmitTask t3: %v", err)
	}
	_, err = ed.SubmitTask(editor.TaskRequest{
		ID:             "t1",
		Number:         1,
		Title:          "slime_02 jumps the gap at x 360",
		Source:         editor.TaskSourceUser,
		StartMode:      editor.TaskStartQueue,
		ScenePath:      "level_01",
		StepIndex:      2,
		StepTotal:      4,
		StepSummary:    "placing the jump arc",
		EditingObjects: []string{"slime_02"},
		InitialState:   editor.TaskRunning,
		ElapsedSeconds: 38,
	})
	if err != nil {
		t.Fatalf("SubmitTask t1: %v", err)
	}

	threadPath := ed.AgentThreadFilePath()
	raw1, err := os.ReadFile(threadPath)
	if err != nil {
		t.Fatalf("ReadFile thread yaml: %v", err)
	}
	if !strings.Contains(string(raw1), "version: 1") {
		t.Fatalf("thread yaml missing version: 1:\n%s", raw1)
	}

	// Reload into a fresh editor and verify deterministic byte-identical round trip.
	ed2, err := editor.New(newTasksTestScene(), nil)
	if err != nil {
		t.Fatalf("editor.New(2): %v", err)
	}
	ed2.ThreadsDir = dir
	if err := ed2.LoadAgentThread(threadPath); err != nil {
		t.Fatalf("LoadAgentThread: %v", err)
	}
	raw2, err := editor.EncodeAgentThread(ed2.AgentThreadDoc())
	if err != nil {
		t.Fatalf("EncodeAgentThread: %v", err)
	}
	if !bytes.Equal(raw1, raw2) {
		t.Fatalf("round-trip YAML mismatch:\n--- first ---\n%s\n--- second ---\n%s", raw1, raw2)
	}

	// Unknown fields and wrong versions are refused.
	if _, err := editor.ParseAgentThread([]byte("version: 1\nid: default\nunknownField: true\n")); err == nil {
		t.Fatal("expected unknown field in thread yaml to be refused")
	}
	if _, err := editor.ParseAgentThread([]byte("version: 99\nid: default\n")); err == nil {
		t.Fatal("expected unsupported version in thread yaml to be refused")
	}
	_ = filepath.Base(threadPath)
}

// #1022: Status bar indicator states, DockBadge counts (for #1023), and deterministic elapsed clock.
func TestTaskSummaryDockBadgeAndStatusBarParts(t *testing.T) {
	now := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	ed, err := editor.New(newTasksTestScene(), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.Now = func() time.Time { return now }

	// 1. Idle -> "No tasks"
	if parts := ed.TaskStatusParts(); len(parts) != 1 || parts[0].Label != "No tasks" {
		t.Fatalf("idle parts = %+v, want [No tasks]", parts)
	}
	if ed.TaskDockBadgeCount() != 0 {
		t.Fatalf("idle dock badge = %d, want 0", ed.TaskDockBadgeCount())
	}

	// 2. Single running task -> "1 running · 0:38"
	if _, err := ed.SubmitTask(editor.TaskRequest{
		ID:        "t1",
		Title:     "slime_02 jumps the gap",
		StartMode: editor.TaskStartQueue,
	}); err != nil {
		t.Fatalf("SubmitTask t1: %v", err)
	}
	now = now.Add(38 * time.Second)
	if parts := ed.TaskStatusParts(); len(parts) != 1 || parts[0].Label != "1 running · 0:38" {
		t.Fatalf("single running parts = %+v, want [1 running · 0:38]", parts)
	}
	if ed.TaskDockBadgeCount() != 0 {
		t.Fatalf("running task must not add to dock badge, got %d", ed.TaskDockBadgeCount())
	}

	// 3. Mixed: 1 needs you, 2 running, 2 ready to review, 1 queued, 1 failed -> DockBadge == 4 (1+2+1).
	_, _ = ed.SubmitTask(editor.TaskRequest{ID: "t2", Title: "All slimes faster", StartMode: editor.TaskStartParallel})
	_, _ = ed.SubmitTask(editor.TaskRequest{ID: "t3", Title: "Boss torches", InitialState: editor.TaskNeedsYou})
	_, _ = ed.SubmitTask(editor.TaskRequest{ID: "t4", Title: "Nightly check", Source: editor.TaskSourceAutomation, InitialState: editor.TaskReadyToReview})
	_, _ = ed.SubmitTask(editor.TaskRequest{ID: "t5", Title: "Translate HUD", Source: editor.TaskSourceAutomation, InitialState: editor.TaskReadyToReview})
	_, _ = ed.SubmitTask(editor.TaskRequest{ID: "t6", Title: "Coin sound", StartMode: editor.TaskStartQueue})
	_, _ = ed.SubmitTask(editor.TaskRequest{ID: "t7", Title: "Bake normals", Source: editor.TaskSourceAutomation, InitialState: editor.TaskFailed, Error: "exit code 1"})

	sum := ed.TaskSummary()
	if sum.NeedsYou != 1 || sum.Running != 2 || sum.ReadyToReview != 2 || sum.Queued != 1 || sum.Failed != 1 {
		t.Fatalf("summary = %+v", sum)
	}
	if sum.DockBadge != 4 {
		t.Fatalf("DockBadge = %d, want 4 (1 needs you + 2 ready to review + 1 failed)", sum.DockBadge)
	}
	if sum.MostUrgent != editor.TaskFailed {
		t.Fatalf("MostUrgent = %s, want failed", sum.MostUrgent)
	}
	parts := ed.TaskStatusParts()
	if len(parts) != 4 || parts[0].Label != "1 failed" || parts[1].Label != "1 needs you" || parts[2].Label != "2 running" || parts[3].Label != "2 to review" {
		t.Fatalf("mixed parts = %+v", parts)
	}

	// 4. Stopping running + resolving needs_you leaves Failed + Ready to review ("Failed stays until opened or dismissed").
	_ = ed.StopTask("t1")
	_ = ed.StopTask("t2")
	_ = ed.StopTask("t6")
	_ = ed.StopTask("t3")
	parts = ed.TaskStatusParts()
	if len(parts) != 2 || parts[0].Label != "1 failed" || parts[1].Label != "2 to review" {
		t.Fatalf("failed+review parts = %+v", parts)
	}
}

// #1022 UI test: status bar toggles Tasks panel, scene tree shows task badges, bulk dismiss works,
// Queue / Run in parallel composer works, and 2D editor-toolbar stays compact at 1440x900.
func TestTasksPanelUIAndCompactToolbar(t *testing.T) {
	ed, err := editor.New(newTasksTestScene(), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	_, _ = ed.SubmitTask(editor.TaskRequest{
		ID:             "t1",
		Number:         1,
		Title:          "slime_02 jumps the gap",
		StepSummary:    "placing the jump arc",
		EditingObjects: []string{"slime_02"},
		InitialState:   editor.TaskRunning,
		ElapsedSeconds: 38,
	})
	_, _ = ed.SubmitTask(editor.TaskRequest{
		ID:              "t2",
		Number:          2,
		Title:           "All slimes 20% faster",
		StartMode:       editor.TaskStartParallel,
		HoldingObjects:  []string{"slime_01", "slime_03"},
		BlockedOn:       "slime_02",
		BlockedByTaskID: "t1",
		InitialState:    editor.TaskRunning,
		ElapsedSeconds:  12,
	})
	_, _ = ed.SubmitTask(editor.TaskRequest{
		ID:           "t4",
		Title:        "Nightly: fix game check warnings",
		Source:       editor.TaskSourceAutomation,
		InitialState: editor.TaskReadyToReview,
		Review:       &editor.TaskReview{ID: "rev-4", TaskID: "t4", ChangeCount: 3},
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Settle()

	// 2D editor-toolbar must remain compact (<= 2 rows, <= 104 pt at 1440x900).
	if h := app.Find("id:editor-toolbar").Bounds().Size.Height; h > 104 {
		t.Fatalf("2D editor-toolbar height at 1440x900 = %v pt, want <= 104 pt (<= 2 rows)", h)
	}

	// Scene tree badges show numbered editing / holding overlays.
	if got := app.Find("id:scene-tree-task-slime_02").Text; got != "1 · editing" {
		t.Fatalf("scene-tree-task-slime_02 = %q, want %q", got, "1 · editing")
	}
	if got := app.Find("id:scene-tree-task-slime_01").Text; got != "2 · holding" {
		t.Fatalf("scene-tree-task-slime_01 = %q, want %q", got, "2 · holding")
	}

	// Viewport task legend is visible for active tasks.
	if app.Find("id:viewport-task-legend") == nil {
		t.Fatal("expected viewport-task-legend to be visible")
	}

	// Click status bar tasks indicator to open the Agent/Tasks panel.
	app.Press("id:status-tasks")
	app.Settle()
	if !ed.TasksPanelOpen() {
		t.Fatal("expected pressing status-tasks to open TasksPanelView")
	}
	if app.Find("id:tasks-panel") == nil {
		t.Fatal("expected tasks-panel node to be mounted")
	}

	// Check and dismiss Ready to review task t4 via UI buttons.
	app.Press("id:task-check-t4")
	app.Settle()
	app.Press("id:tasks-dismiss-selected")
	app.Settle()
	if ed.TaskSummary().ReadyToReview != 0 {
		t.Fatalf("ReadyToReview = %d after UI dismiss, want 0", ed.TaskSummary().ReadyToReview)
	}

	// Switch composer to Run in parallel and submit a prompt.
	app.Press("id:tasks-mode-parallel")
	app.Settle()
	if ed.TaskComposerMode() != editor.TaskStartParallel {
		t.Fatalf("composer mode = %s, want parallel", ed.TaskComposerMode())
	}
	app.Post(func() {
		ed.SetTaskComposerDraft("Add a lever beside the door")
	})
	app.Settle()
	app.Press("id:tasks-composer-send")
	app.Settle()
	if ed.TaskSummary().Running != 3 {
		t.Fatalf("Running count after parallel submit = %d, want 3", ed.TaskSummary().Running)
	}
}
