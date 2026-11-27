package editormcp_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editormcp"
	"github.com/nsgo-dev/game/pkg/sceneformat"
)

func connectTaskClient(t *testing.T, s *editormcp.Server, taskID string) *mcp.ClientSession {
	t.Helper()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := s.ConnectTask(context.Background(), st, taskID); err != nil {
		t.Fatalf("ConnectTask(%s): %v", taskID, err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "agent-" + taskID}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatalf("client Connect(%s): %v", taskID, err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func newSlimesEditor(t *testing.T) *editor.Editor {
	t.Helper()
	reg := sceneformat.NewComponentRegistry()
	if err := reg.Register(sceneformat.ComponentType{
		Name: "patrol",
		Fields: []sceneformat.Field{
			{Name: "speed", Kind: sceneformat.FieldFloat, Default: 40.0},
			{Name: "jump", Kind: sceneformat.FieldBool, Default: false},
		},
	}); err != nil {
		t.Fatalf("register patrol: %v", err)
	}
	doc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "level_01",
		VirtualWidth:  640,
		VirtualHeight: 360,
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "Actors",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{ID: "slime_01", Name: "slime_01", X: 196, Y: 260, Components: map[string]any{"patrol": map[string]any{"speed": 40.0, "jump": false}}},
					{ID: "slime_02", Name: "slime_02", X: 292, Y: 260, Components: map[string]any{"patrol": map[string]any{"speed": 40.0, "jump": false}}},
					{ID: "slime_03", Name: "slime_03", X: 440, Y: 210, Components: map[string]any{"patrol": map[string]any{"speed": 40.0, "jump": false}}},
				},
			},
		},
	}
	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.SetComponentRegistry(reg); err != nil {
		t.Fatalf("SetComponentRegistry: %v", err)
	}
	return ed
}

// #1022 Acceptance: A test starts two parallel scripted agents that both edit slime_02:
// the second holds, shows "waiting on slime_02" and finishes after the first; both turns are separate reviews.
func TestParallelScriptedAgentsHoldOnSlime02AndProduceSeparateReviews(t *testing.T) {
	ed := newSlimesEditor(t)
	s := editormcp.New(ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })

	// Submit two parallel tasks so each runs in its own MCP session.
	t1, err := ed.SubmitTask(editor.TaskRequest{
		ID:          "task-1",
		Number:      1,
		Title:       "slime_02 jumps the gap at x 360",
		StartMode:   editor.TaskStartParallel,
		StepSummary: "placing the jump arc",
	})
	if err != nil || t1.State != editor.TaskRunning {
		t.Fatalf("SubmitTask task-1: %+v, err=%v", t1, err)
	}
	t2, err := ed.SubmitTask(editor.TaskRequest{
		ID:        "task-2",
		Number:    2,
		Title:     "All slimes 20% faster",
		StartMode: editor.TaskStartParallel,
	})
	if err != nil || t2.State != editor.TaskRunning {
		t.Fatalf("SubmitTask task-2: %+v, err=%v", t2, err)
	}

	agent1 := connectTaskClient(t, s, "task-1")
	agent2 := connectTaskClient(t, s, "task-2")

	// Deterministic synchronization channels (zero sleeps!).
	agent1Held := make(chan struct{})
	releaseAgent1 := make(chan struct{})
	agent2Blocked := make(chan struct{}, 1)
	finishOrder := make(chan string, 2)

	s.SetHoldBlockedHook(func(taskID, objectID, holderTaskID string) {
		if taskID == "task-2" && objectID == "slime_02" && holderTaskID == "task-1" {
			select {
			case agent2Blocked <- struct{}{}:
			default:
			}
		}
	})

	// Scripted Agent 1: edits slime_02 (enabling jump), holds slime_02 until released, then finishes its turn.
	errCh1 := make(chan error, 1)
	go func() {
		res, err := agent1.CallTool(context.Background(), &mcp.CallToolParams{
			Name: editormcp.ToolSceneEdit,
			Arguments: map[string]any{
				"label": "Agent 1: slime_02 jump",
				"edits": []any{
					map[string]any{"op": "set", "id": "slime_02", "component": "patrol", "fields": map[string]any{"jump": true}},
				},
			},
		})
		if err != nil {
			errCh1 <- err
			return
		}
		if res.IsError {
			errCh1 <- context.Canceled
			return
		}
		close(agent1Held)
		<-releaseAgent1

		if _, err := s.CompleteTaskTurn(context.Background(), "task-1", "Added jump arc to slime_02"); err != nil {
			errCh1 <- err
			return
		}
		finishOrder <- "task-1"
		errCh1 <- nil
	}()

	// Wait until Agent 1 holds slime_02.
	select {
	case <-agent1Held:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Agent 1 to acquire slime_02")
	}

	// Scripted Agent 2 (in parallel): tries to speed up slime_01, slime_02, slime_03.
	errCh2 := make(chan error, 1)
	go func() {
		res, err := agent2.CallTool(context.Background(), &mcp.CallToolParams{
			Name: editormcp.ToolSceneEdit,
			Arguments: map[string]any{
				"label": "Agent 2: all slimes 20% faster",
				"edits": []any{
					map[string]any{"op": "set", "id": "slime_01", "component": "patrol", "fields": map[string]any{"speed": 48.0}},
					map[string]any{"op": "set", "id": "slime_02", "component": "patrol", "fields": map[string]any{"speed": 48.0}},
					map[string]any{"op": "set", "id": "slime_03", "component": "patrol", "fields": map[string]any{"speed": 48.0}},
				},
			},
		})
		if err != nil {
			errCh2 <- err
			return
		}
		if res.IsError {
			errCh2 <- context.Canceled
			return
		}
		if _, err := s.CompleteTaskTurn(context.Background(), "task-2", "Increased all slime speeds by 20%"); err != nil {
			errCh2 <- err
			return
		}
		finishOrder <- "task-2"
		errCh2 <- nil
	}()

	// Wait deterministically until Agent 2 blocks on slime_02.
	select {
	case <-agent2Blocked:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Agent 2 to hold on slime_02")
	}

	// Inspect live editor state while Agent 2 is holding:
	// - task-2 shows "waiting on slime_02" and BlockedByTaskNumber == 1
	// - scene tree & viewport overlays report task 1 editing slime_02 and task 2 holding slime_01/slime_03
	var gotT1, gotT2 editor.AgentTask
	var ov1, ov2 editor.ObjectTaskOverlay
	var meta2 string
	if err := s.Editor(context.Background(), func(ed *editor.Editor) error {
		gotT1, _ = ed.Task("task-1")
		gotT2, _ = ed.Task("task-2")
		ov1, _ = ed.ObjectTaskBadge("slime_02")
		ov2, _ = ed.ObjectTaskBadge("slime_01")
		meta2 = ed.FormatTaskMeta(gotT2)
		return nil
	}); err != nil {
		t.Fatalf("inspect state: %v", err)
	}
	if !gotT2.Holding() || gotT2.BlockedOn != "slime_02" || gotT2.BlockedByTaskID != "task-1" || gotT2.BlockedByTaskNumber != 1 {
		t.Fatalf("task-2 holding state = %+v", gotT2)
	}
	if !strings.Contains(meta2, "waiting on slime_02") {
		t.Fatalf("task-2 meta = %q, want to contain %q", meta2, "waiting on slime_02")
	}
	if ov1.TaskNumber != 1 || ov1.Holding {
		t.Fatalf("slime_02 overlay = %+v, want task 1 editing", ov1)
	}
	if ov2.TaskNumber != 2 || !ov2.Holding || ov2.Label != "waiting on slime_02" {
		t.Fatalf("slime_01 overlay = %+v, want task 2 holding waiting on slime_02", ov2)
	}
	if !slices.Contains(gotT1.EditingObjects, "slime_02") {
		t.Fatalf("task-1 EditingObjects = %v, want slime_02", gotT1.EditingObjects)
	}

	// Now release Agent 1 -> finishes first, releases slime_02 -> Agent 2 automatically continues and finishes second!
	close(releaseAgent1)

	if err := <-errCh1; err != nil {
		t.Fatalf("Agent 1 error: %v", err)
	}
	if err := <-errCh2; err != nil {
		t.Fatalf("Agent 2 error: %v", err)
	}

	first := <-finishOrder
	second := <-finishOrder
	if first != "task-1" || second != "task-2" {
		t.Fatalf("finish order = [%s, %s], want [task-1, task-2]", first, second)
	}

	// Verify both edits applied to slime_02 AND both turns are separate Ready to review entries.
	if err := s.Editor(context.Background(), func(ed *editor.Editor) error {
		obj, _, _ := ed.FindObject("slime_02")
		if obj == nil {
			t.Fatal("slime_02 missing")
		}
		patrol := obj.Components["patrol"].(map[string]any)
		if patrol["jump"] != true || patrol["speed"] != 48.0 {
			t.Fatalf("slime_02 patrol = %+v, want jump=true speed=48", patrol)
		}
		final1, _ := ed.Task("task-1")
		final2, _ := ed.Task("task-2")
		if final1.State != editor.TaskReadyToReview || final2.State != editor.TaskReadyToReview {
			t.Fatalf("final states = %s, %s, want both ready_to_review", final1.State, final2.State)
		}
		if final1.Review == nil || final2.Review == nil {
			t.Fatalf("expected separate TaskReview on both turns: %+v / %+v", final1.Review, final2.Review)
		}
		if final1.Review.ID == "" || final2.Review.ID == "" || final1.Review.ID == final2.Review.ID {
			t.Fatalf("reviews must have distinct IDs: %q vs %q", final1.Review.ID, final2.Review.ID)
		}
		if final1.Review.EffectiveChangeCount() != 1 || final2.Review.EffectiveChangeCount() != 3 {
			t.Fatalf("review change counts = %d and %d, want 1 and 3", final1.Review.EffectiveChangeCount(), final2.Review.EffectiveChangeCount())
		}
		if s := ed.TaskSummary(); s.ReadyToReview != 2 || s.Running != 0 || s.DockBadge != 2 {
			t.Fatalf("final summary = %+v", s)
		}
		return nil
	}); err != nil {
		t.Fatalf("final inspect: %v", err)
	}
}

// TestMCPTasksTools verifies tasks_submit, tasks_list, and tasks_control over MCP.
func TestMCPTasksTools(t *testing.T) {
	ed := newSlimesEditor(t)
	s := editormcp.New(ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	res := call(t, cs, editormcp.ToolTasksSubmit, map[string]any{
		"title":  "First task",
		"prompt": "Move slime_01 right",
		"mode":   "queue",
	})
	mustOK(t, res)
	var t1 editor.AgentTask
	structured(t, res, &t1)
	if t1.State != editor.TaskRunning {
		t.Fatalf("t1 = %+v, want running", t1)
	}

	res2 := call(t, cs, editormcp.ToolTasksSubmit, map[string]any{
		"title":  "Queued follow-up",
		"prompt": "Adjust coin sound",
		"mode":   "queue",
	})
	mustOK(t, res2)
	var t2 editor.AgentTask
	structured(t, res2, &t2)
	if t2.State != editor.TaskQueued {
		t.Fatalf("t2 = %+v, want queued", t2)
	}

	listRes := call(t, cs, editormcp.ToolTasksList, map[string]any{})
	mustOK(t, listRes)
	var listed editormcp.TasksListResult
	structured(t, listRes, &listed)
	if listed.Summary.Running != 1 || listed.Summary.Queued != 1 || len(listed.Tasks) != 2 {
		t.Fatalf("tasks_list = %+v", listed)
	}

	// Finish t1 -> Ready to review, then keep via tasks_control -> starts t2.
	if _, err := s.CompleteTaskTurn(context.Background(), t1.ID, "done"); err != nil {
		t.Fatalf("CompleteTaskTurn: %v", err)
	}
	ctrlRes := call(t, cs, editormcp.ToolTasksControl, map[string]any{
		"action":  "keep",
		"task_id": t1.ID,
	})
	mustOK(t, ctrlRes)
	var after editor.TaskSummary
	structured(t, ctrlRes, &after)
	if after.Running != 1 || after.Queued != 0 {
		t.Fatalf("summary after keep = %+v, want running=1 queued=0", after)
	}
}

func TestParallelABBADeadlockAvoidedAndHoldWaitTimeout(t *testing.T) {
	ed := newSlimesEditor(t)
	s := editormcp.New(ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })

	_, _ = ed.SubmitTask(editor.TaskRequest{ID: "t1", Number: 1, Title: "Task 1", StartMode: editor.TaskStartParallel})
	_, _ = ed.SubmitTask(editor.TaskRequest{ID: "t2", Number: 2, Title: "Task 2", StartMode: editor.TaskStartParallel})

	c1 := connectTaskClient(t, s, "t1")
	c2 := connectTaskClient(t, s, "t2")

	// ConnectTask sessions must appear in Server.Clients().
	if clients := s.Clients(); len(clients) < 2 {
		t.Fatalf("Server.Clients() = %d, want >= 2", len(clients))
	}

	// Task 1 acquires slime_01; Task 2 acquires slime_02.
	mustOK(t, call(t, c1, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{map[string]any{"op": "set", "id": "slime_01", "component": "patrol", "fields": map[string]any{"speed": 50.0}}},
	}))
	mustOK(t, call(t, c2, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{map[string]any{"op": "set", "id": "slime_02", "component": "patrol", "fields": map[string]any{"speed": 55.0}}},
	}))

	// Task 1 starts waiting on slime_02 (held by Task 2).
	t1Blocked := make(chan struct{}, 1)
	s.SetHoldBlockedHook(func(taskID, objectID, holderTaskID string) {
		if taskID == "t1" && objectID == "slime_02" && holderTaskID == "t2" {
			select {
			case t1Blocked <- struct{}{}:
			default:
			}
		}
	})
	t1Done := make(chan *mcp.CallToolResult, 1)
	go func() {
		res, _ := c1.CallTool(context.Background(), &mcp.CallToolParams{
			Name: editormcp.ToolSceneEdit,
			Arguments: map[string]any{
				"edits": []any{map[string]any{"op": "set", "id": "slime_02", "component": "patrol", "fields": map[string]any{"jump": true}}},
			},
		})
		t1Done <- res
	}()
	select {
	case <-t1Blocked:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for t1 to block on slime_02")
	}

	// Task 2 now requests slime_01 (held by Task 1) -> AB-BA cycle MUST be rejected immediately without hanging!
	res2 := call(t, c2, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{map[string]any{"op": "set", "id": "slime_01", "component": "patrol", "fields": map[string]any{"jump": true}}},
	})
	if !res2.IsError || !strings.Contains(text(res2), "deadlock avoided") {
		t.Fatalf("expected AB-BA deadlock error, got isError=%v text=%q", res2.IsError, text(res2))
	}

	// Finish Task 2 -> Task 1 unblocks and succeeds cleanly.
	if _, err := s.CompleteTaskTurn(context.Background(), "t2", "Task 2 done"); err != nil {
		t.Fatalf("CompleteTaskTurn t2: %v", err)
	}
	select {
	case res1 := <-t1Done:
		mustOK(t, res1)
	case <-time.After(3 * time.Second):
		t.Fatal("t1 did not resume after t2 released holds")
	}
	if _, err := s.CompleteTaskTurn(context.Background(), "t1", "Task 1 done"); err != nil {
		t.Fatalf("CompleteTaskTurn t1: %v", err)
	}

	// Verify bounded hold-wait timeout also fails cleanly instead of hanging forever.
	s.SetHoldWaitTimeout(50 * time.Millisecond)
	_, _ = ed.SubmitTask(editor.TaskRequest{ID: "t3", Number: 3, Title: "Task 3", StartMode: editor.TaskStartParallel})
	_, _ = ed.SubmitTask(editor.TaskRequest{ID: "t4", Number: 4, Title: "Task 4", StartMode: editor.TaskStartParallel})
	c3 := connectTaskClient(t, s, "t3")
	c4 := connectTaskClient(t, s, "t4")
	mustOK(t, call(t, c3, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{map[string]any{"op": "set", "id": "slime_03", "component": "patrol", "fields": map[string]any{"speed": 60.0}}},
	}))
	res4 := call(t, c4, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{map[string]any{"op": "set", "id": "slime_03", "component": "patrol", "fields": map[string]any{"speed": 70.0}}},
	})
	if !res4.IsError || !strings.Contains(text(res4), "timed out waiting") {
		t.Fatalf("expected hold wait timeout error, got isError=%v text=%q", res4.IsError, text(res4))
	}
	s.ReleaseTaskHolds("t3")
}

func TestFailedEditRollsBackHoldsAndObjectToolsRespectHolds(t *testing.T) {
	ed := newSlimesEditor(t)
	s := editormcp.New(ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	s.SetHoldWaitTimeout(60 * time.Millisecond)

	_, _ = ed.SubmitTask(editor.TaskRequest{ID: "t1", Number: 1, Title: "Task 1", StartMode: editor.TaskStartParallel})
	_, _ = ed.SubmitTask(editor.TaskRequest{ID: "t2", Number: 2, Title: "Task 2", StartMode: editor.TaskStartParallel})
	c1 := connectTaskClient(t, s, "t1")
	c2 := connectTaskClient(t, s, "t2")

	// Failed scene_edit on slime_02 MUST roll back the newly acquired hold on slime_02.
	bad := call(t, c1, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{map[string]any{"op": "set", "id": "slime_02", "component": "patrol", "fields": map[string]any{"noSuchField": 1}}},
	})
	if !bad.IsError {
		t.Fatal("expected invalid field edit to fail")
	}
	if held := s.HeldObjects(); held["slime_02"] != "" {
		t.Fatalf("failed edit leaked hold on slime_02: %+v", held)
	}

	// Now Task 1 holds slime_02 via a valid edit; Task 2 calling object_visibility / object_duplicate on slime_02 MUST hold (and hit our 60ms timeout rather than mutating!).
	mustOK(t, call(t, c1, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{map[string]any{"op": "set", "id": "slime_02", "component": "patrol", "fields": map[string]any{"jump": true}}},
	}))
	visRes := call(t, c2, editormcp.ToolObjectVisibility, map[string]any{"id": "slime_02", "hidden": true})
	if !visRes.IsError || !strings.Contains(text(visRes), "timed out waiting") {
		t.Fatalf("object_visibility bypassed hold on slime_02: isError=%v text=%q", visRes.IsError, text(visRes))
	}
	dupRes := call(t, c2, editormcp.ToolObjectDuplicate, map[string]any{"id": "slime_02"})
	if !dupRes.IsError || !strings.Contains(text(dupRes), "timed out waiting") {
		t.Fatalf("object_duplicate bypassed hold on slime_02: isError=%v text=%q", dupRes.IsError, text(dupRes))
	}

	// Releasing Task 1's holds lets Task 2 succeed cleanly.
	s.ReleaseTaskHolds("t1")
	mustOK(t, call(t, c2, editormcp.ToolObjectVisibility, map[string]any{"id": "slime_02", "hidden": true}))
	s.ReleaseTaskHolds("t2")
}

func TestParallelRevertTaskPreservesOtherTasksKeptEditsOverMCP(t *testing.T) {
	ed := newSlimesEditor(t)
	s := editormcp.New(ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })

	_, _ = ed.SubmitTask(editor.TaskRequest{ID: "t1", Number: 1, Title: "Edit slime_01", StartMode: editor.TaskStartParallel})
	_, _ = ed.SubmitTask(editor.TaskRequest{ID: "t2", Number: 2, Title: "Edit slime_03", StartMode: editor.TaskStartParallel})
	c1 := connectTaskClient(t, s, "t1")
	c2 := connectTaskClient(t, s, "t2")

	mustOK(t, call(t, c1, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{map[string]any{"op": "set", "id": "slime_01", "component": "patrol", "fields": map[string]any{"speed": 99.0}}},
	}))
	mustOK(t, call(t, c2, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{map[string]any{"op": "set", "id": "slime_03", "component": "patrol", "fields": map[string]any{"speed": 77.0}}},
	}))

	if _, err := s.CompleteTaskTurn(context.Background(), "t1", "slime_01 faster"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompleteTaskTurn(context.Background(), "t2", "slime_03 faster"); err != nil {
		t.Fatal(err)
	}

	// Keep t2, then revert t1 over MCP.
	mustOK(t, call(t, c2, editormcp.ToolTasksControl, map[string]any{"action": "keep", "task_id": "t2"}))
	mustOK(t, call(t, c1, editormcp.ToolTasksControl, map[string]any{"action": "revert", "task_id": "t1"}))

	if err := s.Editor(context.Background(), func(ed *editor.Editor) error {
		o1, _, _ := ed.FindObject("slime_01")
		o3, _, _ := ed.FindObject("slime_03")
		asFloat := func(v any) float64 {
			switch n := v.(type) {
			case float64:
				return n
			case int:
				return float64(n)
			default:
				return -1
			}
		}
		if got := asFloat(o1.Components["patrol"].(map[string]any)["speed"]); got != 40.0 {
			t.Fatalf("slime_01 speed = %v (%T), want reverted 40", o1.Components["patrol"], o1.Components["patrol"].(map[string]any)["speed"])
		}
		if got := asFloat(o3.Components["patrol"].(map[string]any)["speed"]); got != 77.0 {
			t.Fatalf("t2 kept slime_03 speed = %v, want preserved 77", o3.Components["patrol"])
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
