package editor_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func holdTaskUntilCanceled(ctx context.Context, _ editor.AgentTask) error {
	<-ctx.Done()
	return ctx.Err()
}

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
	t.Cleanup(func() { _ = ed2.Close() })
	ed2.Now = func() time.Time { return now }
	ed2.AgentTaskRunner = holdTaskUntilCanceled
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
	t.Cleanup(func() { _ = ed.Close() })
	ed.AgentTaskRunner = holdTaskUntilCanceled
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
	t.Cleanup(func() { _ = ed.Close() })
	ed.Now = func() time.Time { return now }
	ed.AgentTaskRunner = holdTaskUntilCanceled

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
	t.Cleanup(func() { _ = ed.Close() })
	ed.AgentTaskRunner = holdTaskUntilCanceled
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

func TestTaskConcurrencyCapsAndPerThreadLimit(t *testing.T) {
	ed, err := editor.New(newTasksTestScene(), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	ed.AgentTaskRunner = holdTaskUntilCanceled
	for i := 1; i <= 4; i++ {
		if _, err := ed.SubmitTask(editor.TaskRequest{
			Title:     fmt.Sprintf("Parallel %d", i),
			StartMode: editor.TaskStartParallel,
		}); err != nil {
			t.Fatalf("SubmitTask parallel %d: %v", i, err)
		}
	}
	if _, err := ed.SubmitTask(editor.TaskRequest{
		Title:     "Parallel 5 should fail",
		StartMode: editor.TaskStartParallel,
	}); err == nil {
		t.Fatal("expected 5th parallel task to be rejected by concurrency cap")
	}

	// Fill up to 64 active queued tasks (4 running + 60 queued = 64).
	for i := 5; i <= 64; i++ {
		if _, err := ed.SubmitTask(editor.TaskRequest{
			Title:     fmt.Sprintf("Queued %d", i),
			StartMode: editor.TaskStartQueue,
		}); err != nil {
			t.Fatalf("SubmitTask queued %d: %v", i, err)
		}
	}
	if _, err := ed.SubmitTask(editor.TaskRequest{
		Title:     "65th active task should fail",
		StartMode: editor.TaskStartQueue,
	}); err == nil {
		t.Fatal("expected 65th active task to be rejected by per-thread cap")
	}
}

func TestTaskPathTraversalAndThreadIDValidation(t *testing.T) {
	dir := t.TempDir()
	projDir := filepath.Join(dir, "proj")
	if err := os.MkdirAll(filepath.Join(projDir, "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	outsideScene := filepath.Join(dir, "secret.scene.yaml")
	if err := os.WriteFile(outsideScene, []byte("version: 1\nname: secret\nvirtualWidth: 320\nvirtualHeight: 180\nlayers:\n  - name: main\n    space: world\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Bad thread IDs are refused by ParseAgentThread and EncodeAgentThread.
	for _, badID := range []string{"", "../evil", "a/b", "a\\b", "foo.bar", strings.Repeat("a", 65)} {
		raw := fmt.Sprintf("version: 1\nid: %q\n", badID)
		if _, err := editor.ParseAgentThread([]byte(raw)); err == nil {
			t.Fatalf("expected ParseAgentThread to refuse id %q", badID)
		}
		if _, err := editor.EncodeAgentThread(&editor.AgentThreadDoc{Version: 1, ID: badID}); err == nil && badID != "" {
			t.Fatalf("expected EncodeAgentThread to refuse id %q", badID)
		}
	}

	// Oversized thread file (>2 MiB) is refused by LoadAgentThread.
	threadsDir := filepath.Join(projDir, ".nsgo", "threads")
	if err := os.MkdirAll(threadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bigPath := filepath.Join(threadsDir, "big.thread.yaml")
	if err := os.WriteFile(bigPath, bytes.Repeat([]byte("a"), (2<<20)+16), 0o644); err != nil {
		t.Fatal(err)
	}
	ed, err := editor.New(newTasksTestScene(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.ProjectPath = filepath.Join(projDir, "game.yaml")
	ed.ThreadsDir = threadsDir
	if err := ed.LoadAgentThread(bigPath); err == nil {
		t.Fatal("expected LoadAgentThread to refuse oversized file")
	}

	// Symlink escaping ThreadsDir is refused by LoadAgentThread.
	outsideThread := filepath.Join(dir, "outside.thread.yaml")
	if err := os.WriteFile(outsideThread, []byte("version: 1\nid: outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	symlinkThread := filepath.Join(threadsDir, "link.thread.yaml")
	if err := os.Symlink(outsideThread, symlinkThread); err == nil {
		if err := ed.LoadAgentThread(symlinkThread); err == nil {
			t.Fatal("expected LoadAgentThread to refuse symlink escaping threads directory")
		}
	}

	// SubmitTask refuses path traversal scene paths.
	if _, err := ed.SubmitTask(editor.TaskRequest{
		ID:        "bad-scene",
		Title:     "Bad scene",
		ScenePath: "../secret.scene.yaml",
	}); err == nil {
		t.Fatal("expected SubmitTask to refuse traversal scene path")
	}

	// OpenTask refuses a scene symlink escaping project root.
	symlinkScene := filepath.Join(projDir, "scenes", "link.scene.yaml")
	if err := os.Symlink(outsideScene, symlinkScene); err == nil {
		if _, err := ed.SubmitTask(editor.TaskRequest{
			ID:           "link-task",
			Title:        "Symlink scene",
			ScenePath:    "scenes/link.scene.yaml",
			InitialState: editor.TaskReadyToReview,
		}); err != nil {
			t.Fatalf("SubmitTask link-task: %v", err)
		}
		if err := ed.OpenTask("link-task"); err == nil {
			t.Fatal("expected OpenTask to refuse scene symlink escaping project root")
		}
	}
}

func TestTaskProjectGenerationGuardAndLifecycle(t *testing.T) {
	ed, err := editor.New(newTasksTestScene(), nil)
	if err != nil {
		t.Fatal(err)
	}

	started := make(chan string, 4)
	canceled := make(chan struct{}, 2)
	releaseOld := make(chan struct{})
	ed.AgentTaskRunner = func(ctx context.Context, task editor.AgentTask) error {
		started <- task.ID
		<-ctx.Done()
		canceled <- struct{}{}
		<-releaseOld
		return ctx.Err()
	}

	var releasedHolds []string
	ed.SetTaskHoldReleaseHook(func(taskID string) {
		releasedHolds = append(releasedHolds, taskID)
	})

	t1, err := ed.SubmitTask(editor.TaskRequest{Title: "Old project task", StartMode: editor.TaskStartQueue})
	if err != nil {
		t.Fatal(err)
	}
	<-started

	// OpenDemo resets tasks, cancels old runner, releases holds, and does not reuse old task IDs.
	ed.OpenDemo()
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("OpenDemo did not cancel running task runner")
	}
	if len(releasedHolds) == 0 || releasedHolds[0] != t1.ID {
		t.Fatalf("OpenDemo did not release old task holds: %v", releasedHolds)
	}

	// Submit a brand new task in the new session — must get a fresh ID and survive stale old-runner exit.
	ed.AgentTaskRunner = holdTaskUntilCanceled
	tNew, err := ed.SubmitTask(editor.TaskRequest{Title: "Demo task", StartMode: editor.TaskStartQueue})
	if err != nil {
		t.Fatal(err)
	}
	if tNew.ID == t1.ID {
		t.Fatalf("nextSeq was reset across project switch: reused ID %q", tNew.ID)
	}
	close(releaseOld)
	time.Sleep(20 * time.Millisecond)

	cur, ok := ed.Task(tNew.ID)
	if !ok || cur.State != editor.TaskRunning {
		t.Fatalf("new session task corrupted by stale runner: ok=%v task=%+v", ok, cur)
	}

	// StopRunningTasks (wired to Mod+.) stops running tasks without disturbing Ready to review tasks.
	_, _ = ed.SubmitTask(editor.TaskRequest{
		ID:           "rev-keep",
		Title:        "Reviewable turn",
		Source:       editor.TaskSourceAutomation,
		InitialState: editor.TaskReadyToReview,
	})
	ed.StopRunningTasks()
	if cur, _ := ed.Task(tNew.ID); cur.State != editor.TaskStopped {
		t.Fatalf("StopRunningTasks state = %s, want stopped", cur.State)
	}
	if revTask, _ := ed.Task("rev-keep"); revTask.State != editor.TaskReadyToReview {
		t.Fatalf("StopRunningTasks disturbed reviewable task: state=%s", revTask.State)
	}

	// Editor.Close stops active runners cleanly.
	if err := ed.Close(); err != nil {
		t.Fatalf("Editor.Close: %v", err)
	}
}

func TestRevertTaskPreservesParallelKeptEditsAndHumanEdits(t *testing.T) {
	ed, err := editor.New(newTasksTestScene(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	ed.AgentTaskRunner = holdTaskUntilCanceled
	s0, _ := ed.SaveYAML()

	// Parallel Task 1 edits slime_01.
	if _, err := ed.SubmitTask(editor.TaskRequest{ID: "p1", Title: "Move slime_01", StartMode: editor.TaskStartParallel}); err != nil {
		t.Fatal(err)
	}
	if err := ed.MoveObject("slime_01", 120, 260); err != nil {
		t.Fatal(err)
	}
	s1, _ := ed.SaveYAML()
	if err := ed.CompleteTaskTurn("p1", editor.TaskReview{
		ID:              "rev-p1",
		TaskID:          "p1",
		BeforeSceneYAML: string(s0),
		AfterSceneYAML:  string(s1),
		Changes:         []editor.TaskReviewChange{{ObjectID: "slime_01", Kind: "changed", Summary: "move slime_01"}},
	}); err != nil {
		t.Fatal(err)
	}

	// Parallel Task 2 edits slime_03 (and captures BeforeSceneYAML after p1's edit).
	if _, err := ed.SubmitTask(editor.TaskRequest{ID: "p2", Title: "Move slime_03", StartMode: editor.TaskStartParallel}); err != nil {
		t.Fatal(err)
	}
	if err := ed.MoveObject("slime_03", 500, 180); err != nil {
		t.Fatal(err)
	}
	s2, _ := ed.SaveYAML()
	if err := ed.CompleteTaskTurn("p2", editor.TaskReview{
		ID:              "rev-p2",
		TaskID:          "p2",
		BeforeSceneYAML: string(s1),
		AfterSceneYAML:  string(s2),
		Changes:         []editor.TaskReviewChange{{ObjectID: "slime_03", Kind: "changed", Summary: "move slime_03"}},
	}); err != nil {
		t.Fatal(err)
	}

	// Human also moves player.
	if err := ed.MoveObject("player", 80, 220); err != nil {
		t.Fatal(err)
	}

	// Keep Task 2, then Revert Task 1.
	if err := ed.KeepTask("p2"); err != nil {
		t.Fatal(err)
	}
	if err := ed.RevertTask("p1"); err != nil {
		t.Fatal(err)
	}

	// slime_01 must be reverted to (196, 260), while Task 2's kept slime_03 (500, 180) and human's player (80, 220) are preserved!
	o1, _, _ := ed.FindObject("slime_01")
	o3, _, _ := ed.FindObject("slime_03")
	op, _, _ := ed.FindObject("player")
	if o1 == nil || o1.X != 196 || o1.Y != 260 {
		t.Fatalf("slime_01 = %+v, want reverted to (196, 260)", o1)
	}
	if o3 == nil || o3.X != 500 || o3.Y != 180 {
		t.Fatalf("Task 2 kept slime_03 was clobbered by RevertTask(p1): %+v", o3)
	}
	if op == nil || op.X != 80 || op.Y != 220 {
		t.Fatalf("Human player edit was clobbered by RevertTask(p1): %+v", op)
	}
}

func TestTasksComposerPreservesFocusAcrossBackgroundProgressAndUTF8Title(t *testing.T) {
	ed, err := editor.New(newTasksTestScene(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	ed.AgentTaskRunner = holdTaskUntilCanceled
	_, _ = ed.SubmitTask(editor.TaskRequest{
		ID:           "bg-1",
		Title:        "Background runner",
		StartMode:    editor.TaskStartQueue,
		InitialState: editor.TaskRunning,
	})
	ed.SetTasksPanelOpen(true)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Settle()

	// Real AgentStatusChipView is rendered inside TasksPanelView (no fake static MCP badge).
	if app.Exists("id:tasks-mcp-badge") {
		t.Fatal("expected static fake tasks-mcp-badge to be replaced by real AgentStatusChipView")
	}

	// Focus the composer input and trigger repeated background task updates.
	app.Focus("id:tasks-composer-input")
	app.Settle()
	if !ed.IsTyping() {
		t.Fatal("expected ed.IsTyping() == true while tasks-composer-input is focused")
	}
	app.Post(func() {
		_ = ed.UpdateTaskStep("bg-1", 2, 5, "placing jump arc")
		_ = ed.AddTaskStep("bg-1", "updated slime_02", false)
		ed.SetTaskComposerMode(editor.TaskStartParallel)
	})
	app.Settle()

	// Typing focus MUST NOT be dropped by background progress updates.
	if !ed.IsTyping() {
		t.Fatal("background task updates remounted tasks-composer-input and dropped typing focus!")
	}

	// Long multi-byte UTF-8 prompt truncates cleanly on rune boundaries without producing invalid UTF-8.
	longCN := strings.Repeat("给史莱姆加上跳跃动画并调整速度", 8)
	app.Post(func() {
		ed.SetTaskComposerDraft(longCN)
		task, serr := ed.SubmitComposerTask()
		if serr != nil {
			t.Errorf("SubmitComposerTask UTF-8: %v", serr)
			return
		}
		if !utf8.ValidString(task.Title) {
			t.Errorf("task.Title contains invalid UTF-8: %q", task.Title)
		}
		if len([]rune(task.Title)) > 72 {
			t.Errorf("task.Title rune length = %d, want <= 72", len([]rune(task.Title)))
		}
	})
	app.Settle()
}

func TestTasksOpenReviewBlocksAutoSave(t *testing.T) {
	ed, err := editor.New(newTasksTestScene(), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	ed.AgentTaskRunner = holdTaskUntilCanceled
	if ed.HasOpenReview() {
		t.Fatal("expected HasOpenReview == false before any task review")
	}
	task, err := ed.SubmitTask(editor.TaskRequest{
		ID:     "rev-1",
		Title:  "Tune player jump",
		Prompt: "Tune player jump",
	})
	if err != nil {
		t.Fatalf("SubmitTask: %v", err)
	}
	if ed.HasOpenReview() {
		t.Fatal("expected HasOpenReview == false while task is still running")
	}
	if err := ed.CompleteTaskTurn(task.ID, editor.TaskReview{
		Summary: "Adjusted jump velocity",
		Changes: []editor.TaskReviewChange{{ObjectID: "player", Summary: "jump +20%"}},
	}); err != nil {
		t.Fatalf("CompleteTaskTurn: %v", err)
	}
	if !ed.HasOpenTaskReview() || !ed.HasOpenReview() {
		t.Fatal("expected HasOpenReview == true while background task review is unkept")
	}
	if err := ed.KeepTask(task.ID); err != nil {
		t.Fatalf("KeepTask: %v", err)
	}
	if ed.HasOpenTaskReview() || ed.HasOpenReview() {
		t.Fatal("expected HasOpenReview == false after KeepTask")
	}
}

func waitForTaskState(t *testing.T, ed *editor.Editor, id string, want editor.TaskState) editor.AgentTask {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		cur, ok := ed.Task(id)
		if ok && cur.State == want {
			return cur
		}
		select {
		case <-deadline:
			t.Fatalf("task %q did not reach state %s (last=%+v, ok=%v)", id, want, cur, ok)
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// #1507 Acceptance 1 & 2: Without an injected AgentTaskRunner, submitting a parallel task with a
// scripted CLI agent runs it in its own session and reaches Ready to review with its own turn;
// a queued task starts automatically after the first is kept.
func TestDefaultTaskRunnerParallelScriptedCLIAgentAndQueuedTaskAfterKeep(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("scripted POSIX shell CLI agent test runs on Unix")
	}

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "cli-invocations.log")
	gatePath := filepath.Join(dir, "release-first")

	// Fake `claude` CLI on PATH: logs task ID + whether --resume was passed, gates task-1 until
	// the test creates gatePath, emits stream-json session/step/message/usage/result lines.
	fakeClaude := filepath.Join(binDir, "claude")
	script := `#!/bin/sh
set -eu
has_resume="no"
for arg in "$@"; do
  if [ "$arg" = "--resume" ]; then
    has_resume="yes"
  fi
done
task_id="${NSGO_EDITOR_TASK_ID:-}"
printf '%s|resume=%s\n' "$task_id" "$has_resume" >> "` + logPath + `"
if [ "$task_id" = "task-1" ]; then
  i=0
  while [ ! -f "` + gatePath + `" ] && [ "$i" -lt 400 ]; do
    sleep 0.01
    i=$((i + 1))
  done
fi
printf '{"type":"system","subtype":"init","session_id":"cli-sess-%s"}\n' "$task_id"
printf '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu-%s","name":"scene_edit"}]}}\n' "$task_id"
printf '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu-%s","is_error":false}]}}\n' "$task_id"
printf '{"type":"assistant","message":{"content":[{"type":"text","text":"Completed %s."}]}}\n' "$task_id"
printf '{"type":"result","subtype":"success","session_id":"cli-sess-%s","result":"Completed %s.","usage":{"input_tokens":120,"output_tokens":45}}\n' "$task_id" "$task_id"
`
	if err := os.WriteFile(fakeClaude, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ed, err := editor.New(newTasksTestScene(), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	// Deliberately leave BOTH ed.AgentTaskRunner == nil AND ed.AgentRunner == nil!
	if ed.AgentTaskRunner != nil || ed.AgentRunner != nil {
		t.Fatal("expected nil AgentTaskRunner and AgentRunner")
	}

	// Simulate Claude Code connected over MCP.
	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{
		ID:        "c1",
		Name:      "claude-code",
		Transport: "stdio",
	})

	// Submit task 1 in Run in parallel mode via the Tasks composer.
	ed.SetTaskComposerMode(editor.TaskStartParallel)
	ed.SetTaskComposerDraft("add three coins to level_01")
	t1, err := ed.SubmitComposerTask()
	if err != nil {
		t.Fatalf("SubmitComposerTask (parallel): %v", err)
	}
	if t1.ID != "task-1" || t1.State != editor.TaskRunning {
		t.Fatalf("t1 = %+v, want task-1 running", t1)
	}
	if ed.TaskComposerDraft() != "" {
		t.Fatalf("expected composer draft to clear on successful submit, got %q", ed.TaskComposerDraft())
	}

	// Queue task 2 behind task 1 while task 1 is still running.
	ed.SetTaskComposerMode(editor.TaskStartQueue)
	ed.SetTaskComposerDraft("adjust coin pickup sound")
	t2, err := ed.SubmitComposerTask()
	if err != nil {
		t.Fatalf("SubmitComposerTask (queue): %v", err)
	}
	if t2.State != editor.TaskQueued || t2.StartsAfterTaskID != t1.ID {
		t.Fatalf("t2 = %+v, want queued behind %s", t2, t1.ID)
	}

	// Let task 1 finish and reach Ready to review.
	if err := os.WriteFile(gatePath, []byte("go"), 0o644); err != nil {
		t.Fatal(err)
	}
	done1 := waitForTaskState(t, ed, t1.ID, editor.TaskReadyToReview)
	if done1.TurnID == "" {
		t.Fatalf("task 1 missing dedicated TurnID: %+v", done1)
	}
	if done1.SessionID != "cli-sess-task-1" {
		t.Fatalf("task 1 SessionID = %q, want cli-sess-task-1", done1.SessionID)
	}
	if ed.AgentSessionID() != "" {
		t.Fatalf("parallel task clobbered foreground AgentSessionID = %q", ed.AgentSessionID())
	}
	turn1, ok := ed.AgentTurn(done1.TurnID)
	if !ok || !turn1.Done || turn1.Source != "task" || !strings.Contains(turn1.Summary, "Completed task-1.") {
		t.Fatalf("task 1 AgentTurn = %+v (ok=%v)", turn1, ok)
	}
	if turn1.InputTokens != 120 || turn1.OutputTokens != 45 {
		t.Fatalf("task 1 turn tokens = %d/%d, want 120/45", turn1.InputTokens, turn1.OutputTokens)
	}
	if done1.Review == nil || !strings.Contains(done1.Review.Summary, "Completed task-1.") {
		t.Fatalf("task 1 Review = %+v", done1.Review)
	}

	// Task 2 MUST still be queued until task 1 is kept or reverted.
	if cur2, _ := ed.Task(t2.ID); cur2.State != editor.TaskQueued {
		t.Fatalf("task 2 started before task 1 was kept! state=%s", cur2.State)
	}

	// Keep task 1 -> queued task 2 starts automatically through the default runner and reaches Ready to review with its own turn!
	if err := ed.KeepTask(t1.ID); err != nil {
		t.Fatalf("KeepTask(t1): %v", err)
	}
	done2 := waitForTaskState(t, ed, t2.ID, editor.TaskReadyToReview)
	if done2.TurnID == "" || done2.TurnID == done1.TurnID {
		t.Fatalf("task 2 TurnID = %q, want distinct from %q", done2.TurnID, done1.TurnID)
	}
	if done2.SessionID != "cli-sess-task-2" {
		t.Fatalf("task 2 SessionID = %q, want cli-sess-task-2", done2.SessionID)
	}
	turn2, ok := ed.AgentTurn(done2.TurnID)
	if !ok || !turn2.Done || !strings.Contains(turn2.Summary, "Completed task-2.") {
		t.Fatalf("task 2 AgentTurn = %+v (ok=%v)", turn2, ok)
	}

	rawLog, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read CLI log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(rawLog)), "\n")
	if len(lines) != 2 || lines[0] != "task-1|resume=no" || lines[1] != "task-2|resume=no" {
		t.Fatalf("CLI invocations = %q, want two fresh sessions [task-1|resume=no, task-2|resume=no]", lines)
	}
}

// #1507 Acceptance 3: With no agent connected, submitting from SubmitTask or the Tasks panel
// composer opens the Connect dialog and creates no task.
func TestTaskSubmitWithoutConnectedAgentOpensConnectAndCreatesNoTask(t *testing.T) {
	ed, err := editor.New(newTasksTestScene(), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	t.Cleanup(func() { _ = ed.Close() })

	// 1. Direct SubmitTask in parallel mode without a connected agent.
	if _, err := ed.SubmitTask(editor.TaskRequest{
		Title:     "Add three coins",
		Prompt:    "add three coins to level_01",
		StartMode: editor.TaskStartParallel,
	}); err == nil {
		t.Fatal("expected SubmitTask to fail when no agent is connected")
	}
	if !ed.ConnectDialogOpen() {
		t.Fatal("expected Connect dialog to open when submitting with no agent connected")
	}
	if got := ed.Tasks(); len(got) != 0 {
		t.Fatalf("expected no task to be created, got %+v", got)
	}

	// 2. Direct SubmitTask in queue mode without a connected agent.
	ed.CloseConnectDialog()
	if _, err := ed.SubmitTask(editor.TaskRequest{
		Title:     "Queue task",
		Prompt:    "queue a request",
		StartMode: editor.TaskStartQueue,
	}); err == nil {
		t.Fatal("expected queued SubmitTask to fail when no agent is connected")
	}
	if !ed.ConnectDialogOpen() {
		t.Fatal("expected Connect dialog to open for queued SubmitTask with no agent connected")
	}
	if got := ed.Tasks(); len(got) != 0 {
		t.Fatalf("expected no task to be created, got %+v", got)
	}

	// 3. UI Tasks panel composer press with no agent connected -> opens Connect, creates no task, preserves draft.
	ed.CloseConnectDialog()
	ed.SetTasksPanelOpen(true)
	ed.SetTaskComposerMode(editor.TaskStartParallel)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Settle()

	app.Post(func() {
		ed.SetTaskComposerDraft("add three coins to level_01")
	})
	app.Settle()
	app.Press("id:tasks-composer-send")
	app.Settle()

	if !ed.ConnectDialogOpen() {
		t.Fatal("expected Tasks composer send with no agent connected to open Connect dialog")
	}
	if got := ed.Tasks(); len(got) != 0 {
		t.Fatalf("expected Tasks composer send with no agent connected to create no task, got %+v", got)
	}
	if ed.TaskComposerDraft() != "add three coins to level_01" {
		t.Fatalf("expected composer draft to be preserved after Connect refusal, got %q", ed.TaskComposerDraft())
	}

	// 4. API key mode without a stored API key -> opens Connect at API key step, creates no task.
	ed.CloseConnectDialog()
	ed.OpenConnectDialogForMode(editor.AgentModeAPIKey)
	ed.CompleteConnectDialog()
	if _, err := ed.SubmitTask(editor.TaskRequest{
		Prompt:    "add coins with missing key",
		StartMode: editor.TaskStartParallel,
	}); !errors.Is(err, editor.ErrAgentAPIKeyMissing) {
		t.Fatalf("expected ErrAgentAPIKeyMissing, got %v", err)
	}
	if !ed.ConnectDialogOpen() || ed.ConnectDialogStep() != editor.ConnectStepAPIKey {
		t.Fatalf("expected Connect dialog at ConnectStepAPIKey, open=%v step=%s", ed.ConnectDialogOpen(), ed.ConnectDialogStep())
	}
	if len(ed.Tasks()) != 0 {
		t.Fatalf("expected no task created when API key is missing, got %+v", ed.Tasks())
	}

	// 5. AgentModeNone -> refuses without creating a task.
	ed.OpenConnectDialogForMode(editor.AgentModeNone)
	ed.CompleteConnectDialog()
	if _, err := ed.SubmitTask(editor.TaskRequest{
		Prompt:    "should refuse when agent disabled",
		StartMode: editor.TaskStartParallel,
	}); err == nil {
		t.Fatal("expected SubmitTask to fail in AgentModeNone")
	}
	if len(ed.Tasks()) != 0 {
		t.Fatalf("expected no task created in AgentModeNone, got %+v", ed.Tasks())
	}
}

// #1507: Default task runner reports steps, Needs you, Ready to review, Failed, Retry, and Stop
// through existing task state transitions without any injected AgentTaskRunner.
func TestDefaultTaskRunnerStepsNeedsYouFailureRetryAndStop(t *testing.T) {
	ed, err := editor.New(newTasksTestScene(), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	t.Cleanup(func() { _ = ed.Close() })

	permResolved := make(chan struct{})
	ed.OnTaskPermissionDecision = func(taskID string, allow bool) {
		if allow {
			close(permResolved)
		}
	}

	var mu sync.Mutex
	attempts := map[string]int{}
	ed.AgentRunner = editor.AgentRunnerFunc(func(ctx context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		mu.Lock()
		attempts[req.TaskID]++
		n := attempts[req.TaskID]
		mu.Unlock()

		switch req.Prompt {
		case "needs permission":
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "s1", StepTitle: "Inspect scene", StepStatus: editor.AgentStepRunning, StepIndex: 1, StepTotal: 2})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "s1", StepTitle: "Inspect scene", StepStatus: editor.AgentStepDone, StepIndex: 1, StepTotal: 2})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "s2", StepTitle: "Allow playtest?", StepDetail: "Needs to run level_01 headless", StepStatus: editor.AgentStepWaiting, StepIndex: 2, StepTotal: 2})
			select {
			case <-permResolved:
			case <-ctx.Done():
				return ctx.Err()
			}
			emit(editor.AgentEvent{Kind: editor.AgentEventMessage, Text: "Playtest passed."})
			emit(editor.AgentEvent{Kind: editor.AgentEventDone})
			return nil
		case "fail then succeed":
			if n == 1 {
				return errors.New("tool exited with code 1")
			}
			emit(editor.AgentEvent{Kind: editor.AgentEventMessage, Text: "Succeeded on retry."})
			emit(editor.AgentEvent{Kind: editor.AgentEventDone})
			return nil
		default:
			<-ctx.Done()
			return ctx.Err()
		}
	})

	// 1. Steps + Needs you -> ResolveTaskPermission -> Ready to review.
	tPerm, err := ed.SubmitTask(editor.TaskRequest{
		Prompt:    "needs permission",
		StartMode: editor.TaskStartParallel,
	})
	if err != nil {
		t.Fatalf("SubmitTask perm: %v", err)
	}
	waiting := waitForTaskState(t, ed, tPerm.ID, editor.TaskNeedsYou)
	if waiting.PermissionPrompt != "Allow playtest?" || waiting.PermissionDetail != "Needs to run level_01 headless" {
		t.Fatalf("waiting permission = %+v", waiting)
	}
	if err := ed.ResolveTaskPermission(tPerm.ID, true); err != nil {
		t.Fatalf("ResolveTaskPermission: %v", err)
	}
	ready := waitForTaskState(t, ed, tPerm.ID, editor.TaskReadyToReview)
	if ready.Review == nil || !strings.Contains(ready.Review.Summary, "Playtest passed.") {
		t.Fatalf("ready review = %+v", ready.Review)
	}

	// 2. Failure -> TaskFailed -> RetryTask -> Ready to review.
	tFail, err := ed.SubmitTask(editor.TaskRequest{
		Prompt:    "fail then succeed",
		StartMode: editor.TaskStartParallel,
	})
	if err != nil {
		t.Fatalf("SubmitTask fail: %v", err)
	}
	failed := waitForTaskState(t, ed, tFail.ID, editor.TaskFailed)
	if !strings.Contains(failed.Error, "tool exited with code 1") {
		t.Fatalf("failed.Error = %q", failed.Error)
	}
	if err := ed.RetryTask(tFail.ID); err != nil {
		t.Fatalf("RetryTask: %v", err)
	}
	retried := waitForTaskState(t, ed, tFail.ID, editor.TaskReadyToReview)
	if retried.Review == nil || !strings.Contains(retried.Review.Summary, "Succeeded on retry.") {
		t.Fatalf("retried review = %+v", retried.Review)
	}

	// 3. StopTask cancels a running default-runner task cleanly.
	tStop, err := ed.SubmitTask(editor.TaskRequest{
		Prompt:    "wait forever",
		StartMode: editor.TaskStartParallel,
	})
	if err != nil {
		t.Fatalf("SubmitTask stop: %v", err)
	}
	if err := ed.StopTask(tStop.ID); err != nil {
		t.Fatalf("StopTask: %v", err)
	}
	stopped := waitForTaskState(t, ed, tStop.ID, editor.TaskStopped)
	if stopped.State != editor.TaskStopped {
		t.Fatalf("stopped state = %s", stopped.State)
	}
}
