package editormcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editormcp"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func holdTaskUntilCanceled(ctx context.Context, _ editor.AgentTask) error {
	<-ctx.Done()
	return ctx.Err()
}

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
	t.Cleanup(func() { _ = ed.Close() })
	ed.AgentTaskRunner = holdTaskUntilCanceled
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

	// Finish t1 -> Ready to review; the agent's keep is refused, and the person's keep starts t2.
	if _, err := s.CompleteTaskTurn(context.Background(), t1.ID, "done"); err != nil {
		t.Fatalf("CompleteTaskTurn: %v", err)
	}
	if res := call(t, cs, editormcp.ToolTasksControl, map[string]any{"action": "keep", "task_id": t1.ID}); !res.IsError {
		t.Fatal("tasks_control keep succeeded for an agent, want refusal")
	}
	if err := ed.KeepTask(t1.ID); err != nil {
		t.Fatalf("person KeepTask: %v", err)
	}
	listAfter := call(t, cs, editormcp.ToolTasksList, map[string]any{})
	mustOK(t, listAfter)
	var listedAfter editormcp.TasksListResult
	structured(t, listAfter, &listedAfter)
	if after := listedAfter.Summary; after.Running != 1 || after.Queued != 0 {
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

	// t2's own session cannot keep it; the person keeps t2, then t1's session reverts its own task over MCP.
	if res := call(t, c2, editormcp.ToolTasksControl, map[string]any{"action": "keep", "task_id": "t2"}); !res.IsError {
		t.Fatal("t2 session kept its own task over MCP, want refusal")
	}
	if err := ed.KeepTask("t2"); err != nil {
		t.Fatalf("person KeepTask(t2): %v", err)
	}
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

// taskRunnerWait bounds each wait on the default task runner's goroutines, which only guards against a hang.
const taskRunnerWait = 10 * time.Second

// #1507: Built-in agent tasks run through the default AgentTaskRunner (AgentTaskRunner == nil)
// with editormcp per-object holds, Needs you permission prompts, and separate TaskReviews/AgentTurns.
func TestDefaultTaskRunnerBuiltInAgentHoldsPermissionsAndSeparateReviews(t *testing.T) {
	const secretKey = "sk-ant-test-task-runner-secret"

	ed := newSlimesEditor(t)
	ed.AgentTaskRunner = nil // Use Editor's default AgentTaskRunner!

	task1Held := make(chan struct{})
	releaseTask1 := make(chan struct{})
	task2Blocked := make(chan struct{}, 1)

	var mu sync.Mutex
	callsByTask := map[string]int{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		firstMsg := string(payload.Messages[0].Content)
		taskKey := "task-other"
		switch {
		case strings.Contains(firstMsg, "PROMPT_TASK_1"):
			taskKey = "task-1"
		case strings.Contains(firstMsg, "PROMPT_TASK_2"):
			taskKey = "task-2"
		case strings.Contains(firstMsg, "PROMPT_TASK_PERM"):
			taskKey = "task-perm"
		}
		mu.Lock()
		callsByTask[taskKey]++
		callNum := callsByTask[taskKey]
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch taskKey {
		case "task-1":
			if callNum == 1 {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id": "m1-1", "model": payload.Model, "stop_reason": "tool_use",
					"content": []map[string]any{{
						"type": "tool_use", "id": "tu-1", "name": editormcp.ToolSceneEdit,
						"input": map[string]any{
							"edits": []map[string]any{{
								"op": "set", "id": "slime_02", "component": "patrol",
								"fields": map[string]any{"jump": true},
							}},
						},
					}},
				})
				return
			}
			// After scene_edit succeeds, hold the turn open until task-2 blocks on slime_02.
			close(task1Held)
			<-releaseTask1
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "m1-2", "model": payload.Model, "stop_reason": "end_turn",
				"content": []map[string]any{{"type": "text", "text": "Enabled jump on slime_02."}},
			})
			return

		case "task-2":
			if callNum == 1 {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id": "m2-1", "model": payload.Model, "stop_reason": "tool_use",
					"content": []map[string]any{{
						"type": "tool_use", "id": "tu-2", "name": editormcp.ToolSceneEdit,
						"input": map[string]any{
							"edits": []map[string]any{{
								"op": "set", "id": "slime_02", "component": "patrol",
								"fields": map[string]any{"speed": 48.0},
							}},
						},
					}},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "m2-2", "model": payload.Model, "stop_reason": "end_turn",
				"content": []map[string]any{{"type": "text", "text": "Raised slime_02 speed to 48."}},
			})
			return

		case "task-perm":
			if callNum == 1 {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id": "mp-1", "model": payload.Model, "stop_reason": "tool_use",
					"content": []map[string]any{{
						"type": "tool_use", "id": "tu-p", "name": editormcp.ToolSceneEdit,
						"input": map[string]any{
							"edits": []map[string]any{{
								"op": "set", "id": "slime_01", "component": "patrol",
								"fields": map[string]any{"speed": 60.0},
							}},
						},
					}},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "mp-2", "model": payload.Model, "stop_reason": "end_turn",
				"content": []map[string]any{{"type": "text", "text": "Updated slime_01 after approval."}},
			})
			return
		}
		http.Error(w, "unexpected task", http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	// Registered after srv.Close so it runs first: a failure before the release would otherwise leave task-1's handler blocked and srv.Close waiting on it until the test binary's timeout.
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseTask1) }) }
	t.Cleanup(release)

	ed.BuiltInAgentEndpoint = srv.URL
	ed.BuiltInHTTPClient = srv.Client()

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800), nsgotest.Secrets(map[string]string{
		editor.AgentAPIKeySecretAccount: secretKey,
	}))
	s := editormcp.New(ed, editormcp.Options{Run: app.Post})
	t.Cleanup(func() { _ = s.Close() })

	s.SetHoldBlockedHook(func(taskID, objectID, holderTaskID string) {
		if taskID == "b2" && objectID == "slime_02" && holderTaskID == "b1" {
			select {
			case task2Blocked <- struct{}{}:
			default:
			}
		}
	})

	// Configure built-in API key mode via Connect dialog.
	app.Post(func() {
		ed.OpenConnectDialogForMode(editor.AgentModeAPIKey)
		ed.CompleteConnectDialog()
	})
	app.Settle()

	// Submit parallel task 1; wait until it holds slime_02.
	var submitErr error
	app.Post(func() {
		_, submitErr = ed.SubmitTask(editor.TaskRequest{
			ID:        "b1",
			Title:     "Task 1 jump",
			Prompt:    "PROMPT_TASK_1",
			StartMode: editor.TaskStartParallel,
		})
	})
	app.Settle()
	if submitErr != nil {
		t.Fatalf("SubmitTask b1: %v", submitErr)
	}
	awaitSettling(t, app, task1Held, taskRunnerWait, "b1 to acquire slime_02")

	// Submit parallel task 2; it must hold waiting on slime_02 held by b1.
	app.Post(func() {
		_, submitErr = ed.SubmitTask(editor.TaskRequest{
			ID:        "b2",
			Title:     "Task 2 speed",
			Prompt:    "PROMPT_TASK_2",
			StartMode: editor.TaskStartParallel,
		})
	})
	app.Settle()
	if submitErr != nil {
		t.Fatalf("SubmitTask b2: %v", submitErr)
	}
	awaitSettling(t, app, task2Blocked, taskRunnerWait, "b2 to block on slime_02")

	// Release task 1 -> both finish as separate Ready to review tasks with distinct reviews and turns.
	release()
	app.WaitUntil(taskRunnerWait, func() bool {
		t1, _ := ed.Task("b1")
		t2, _ := ed.Task("b2")
		return t1.State == editor.TaskReadyToReview && t2.State == editor.TaskReadyToReview
	})

	var b1, b2 editor.AgentTask
	app.Post(func() {
		b1, _ = ed.Task("b1")
		b2, _ = ed.Task("b2")
	})
	app.Settle()
	if b1.Review == nil || b2.Review == nil || b1.Review.EffectiveChangeCount() != 1 || b2.Review.EffectiveChangeCount() != 1 {
		t.Fatalf("expected separate 1-change reviews: b1=%+v b2=%+v", b1.Review, b2.Review)
	}
	if b1.TurnID == "" || b2.TurnID == "" || b1.TurnID == b2.TurnID {
		t.Fatalf("expected distinct TurnIDs: %q vs %q", b1.TurnID, b2.TurnID)
	}

	// Verify Needs you permission flow through editormcp Cautious profile (asks before scene edits).
	app.Post(func() {
		_ = ed.SelectPermissionProfile(editor.PermissionProfileCautious)
		_, submitErr = ed.SubmitTask(editor.TaskRequest{
			ID:        "b-perm",
			Title:     "Task needing permission",
			Prompt:    "PROMPT_TASK_PERM",
			StartMode: editor.TaskStartParallel,
		})
	})
	app.Settle()
	if submitErr != nil {
		t.Fatalf("SubmitTask b-perm: %v", submitErr)
	}
	app.WaitUntil(taskRunnerWait, func() bool {
		tp, _ := ed.Task("b-perm")
		return tp.State == editor.TaskNeedsYou
	})
	app.Post(func() {
		_ = ed.ResolveTaskPermission("b-perm", true)
	})
	app.WaitUntil(taskRunnerWait, func() bool {
		tp, _ := ed.Task("b-perm")
		return tp.State == editor.TaskReadyToReview
	})
}

// #1507: tasks_submit runs through the default AgentTaskRunner (no injected AgentTaskRunner)
// and queued tasks start after the person keeps the first.
func TestMCPTasksSubmitRunsDefaultRunnerAndAdvancesQueue(t *testing.T) {
	ed := newSlimesEditor(t)
	ed.AgentTaskRunner = nil // Use Editor's default AgentTaskRunner!

	releaseFirst := make(chan struct{})
	started := make(chan string, 4)
	ed.AgentRunner = editor.AgentRunnerFunc(func(ctx context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		started <- req.TaskID
		if strings.Contains(req.Prompt, "FIRST") {
			select {
			case <-releaseFirst:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		emit(editor.AgentEvent{Kind: editor.AgentEventMessage, Text: "Done: " + req.Prompt})
		emit(editor.AgentEvent{Kind: editor.AgentEventDone})
		return nil
	})

	s := editormcp.New(ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	res1 := call(t, cs, editormcp.ToolTasksSubmit, map[string]any{
		"title":  "First task",
		"prompt": "FIRST task",
		"mode":   "queue",
	})
	mustOK(t, res1)
	var t1 editor.AgentTask
	structured(t, res1, &t1)
	if got := <-started; got != t1.ID {
		t.Fatalf("first started = %q, want %q", got, t1.ID)
	}

	res2 := call(t, cs, editormcp.ToolTasksSubmit, map[string]any{
		"title":  "Second task",
		"prompt": "SECOND task",
		"mode":   "queue",
	})
	mustOK(t, res2)
	var t2 editor.AgentTask
	structured(t, res2, &t2)
	if t2.State != editor.TaskQueued {
		t.Fatalf("t2.State = %s, want queued", t2.State)
	}

	close(releaseFirst)
	deadline := time.After(3 * time.Second)
	for {
		cur, _ := ed.Task(t1.ID)
		if cur.State == editor.TaskReadyToReview {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("t1 did not reach ready_to_review, state=%s", cur.State)
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	// The person keeps t1 -> starts t2 automatically through the default runner.
	if err := ed.KeepTask(t1.ID); err != nil {
		t.Fatalf("person KeepTask: %v", err)
	}
	select {
	case got := <-started:
		if got != t2.ID {
			t.Fatalf("second started = %q, want %q", got, t2.ID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("t2 did not start after the person kept t1")
	}
	deadline = time.After(3 * time.Second)
	for {
		cur, _ := ed.Task(t2.ID)
		if cur.State == editor.TaskReadyToReview {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("t2 did not reach ready_to_review, state=%s", cur.State)
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// #1507: Proxy forwards NSGO_EDITOR_TASK_ID so a spawned CLI MCP client automatically binds to
// its background task session for holds and per-task TaskReview snapshots.
func TestProxyBindsSessionToTaskIDFromEnv(t *testing.T) {
	endpointDir(t)
	f := openFixture(t)
	root := mustRoot(t, f.dir)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Open(); err != nil {
		t.Fatalf("Server.Open: %v", err)
	}

	f.ed.AgentTaskRunner = holdTaskUntilCanceled
	task, err := f.ed.SubmitTask(editor.TaskRequest{
		ID:        "proxy-task-1",
		Title:     "Proxy task",
		StartMode: editor.TaskStartParallel,
	})
	if err != nil {
		t.Fatalf("SubmitTask: %v", err)
	}

	t.Setenv("NSGO_EDITOR_TASK_ID", task.ID)
	cs := startProxy(t, root, nil, "")

	mustOK(t, call(t, cs, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{map[string]any{"op": "move", "id": "hero", "x": 150, "y": 80}},
	}))

	held := s.HeldObjects()
	if held["hero"] != task.ID {
		t.Fatalf("HeldObjects[hero] = %q, want %q (all=%+v)", held["hero"], task.ID, held)
	}
	beforeYAML, changes, ok := s.TaskReviewSnapshot(task.ID)
	if !ok || beforeYAML == "" || len(changes) != 1 || changes[0].ObjectID != "hero" {
		t.Fatalf("TaskReviewSnapshot = (ok=%v, beforeLen=%d, changes=%+v)", ok, len(beforeYAML), changes)
	}
}

// Keeping a task's review and answering its permission prompt are the person's decisions: tasks_control refuses keep, allow and deny from every agent session (the task's own and any other), refuses revert and Ready to review dismissal of a task the session does not own, and the person's direct calls still work.
func TestTasksControlRefusesAgentReviewAndPermissionDecisions(t *testing.T) {
	ed := newSlimesEditor(t)
	s := editormcp.New(ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })

	_, _ = ed.SubmitTask(editor.TaskRequest{ID: "t1", Number: 1, Title: "Edit slime_01", StartMode: editor.TaskStartParallel})
	_, _ = ed.SubmitTask(editor.TaskRequest{ID: "t2", Number: 2, Title: "Edit slime_03", StartMode: editor.TaskStartParallel})
	c1 := connectTaskClient(t, s, "t1")
	c2 := connectTaskClient(t, s, "t2")
	untethered := connect(t, s, nil)

	mustOK(t, call(t, c1, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{map[string]any{"op": "set", "id": "slime_01", "component": "patrol", "fields": map[string]any{"speed": 99.0}}},
	}))
	if _, err := s.CompleteTaskTurn(context.Background(), "t1", "slime_01 faster"); err != nil {
		t.Fatal(err)
	}
	if err := ed.RequestTaskPermission("t2", "Edit game.yaml?", "permissions.yaml asks before editing game.yaml"); err != nil {
		t.Fatal(err)
	}

	refused := func(cs *mcp.ClientSession, who string, args map[string]any) {
		t.Helper()
		res := call(t, cs, editormcp.ToolTasksControl, args)
		if !res.IsError {
			t.Fatalf("%s tasks_control %v succeeded, want refusal", who, args)
		}
	}
	for _, cs := range []struct {
		name string
		cs   *mcp.ClientSession
	}{{"t1 session", c1}, {"t2 session", c2}, {"untethered session", untethered}} {
		refused(cs.cs, cs.name, map[string]any{"action": "keep", "task_id": "t1"})
		refused(cs.cs, cs.name, map[string]any{"action": "allow", "task_id": "t2"})
		refused(cs.cs, cs.name, map[string]any{"action": "deny", "task_id": "t2"})
	}
	refused(c2, "t2 session", map[string]any{"action": "revert", "task_id": "t1"})
	refused(untethered, "untethered session", map[string]any{"action": "revert", "task_id": "t1"})
	refused(c1, "t1 session", map[string]any{"action": "dismiss", "task_id": "t1"})
	refused(c2, "t2 session", map[string]any{"action": "dismiss", "task_ids": []any{"t1"}})
	refused(c1, "t1 session", map[string]any{"action": "stop", "task_id": "t1"})
	refused(c2, "t2 session", map[string]any{"action": "stop", "task_id": "t1"})
	refused(untethered, "untethered session", map[string]any{"action": "stop", "task_id": "t2"})

	if t1, _ := ed.Task("t1"); t1.State != editor.TaskReadyToReview || t1.Dismissed || (t1.Review != nil && t1.Review.Kept) {
		t.Fatalf("t1 after agent attempts = state %s dismissed %v review %+v, want unkept Ready to review", t1.State, t1.Dismissed, t1.Review)
	}
	if !ed.HasOpenTaskReview() {
		t.Fatal("t1's review closed after the agents' attempts, want it open")
	}
	if t2, _ := ed.Task("t2"); t2.State != editor.TaskNeedsYou {
		t.Fatalf("t2 after agent attempts = %s, want needs_you", t2.State)
	}
	if err := ed.RunAsAgent(func() error { return ed.KeepTask("t1") }); !errors.Is(err, editor.ErrTaskDecisionYouOnly) {
		t.Fatalf("agent KeepTask = %v, want ErrTaskDecisionYouOnly", err)
	}
	if err := ed.RunAsAgent(func() error { return ed.ResolveTaskPermission("t2", true) }); !errors.Is(err, editor.ErrTaskDecisionYouOnly) {
		t.Fatalf("agent ResolveTaskPermission = %v, want ErrTaskDecisionYouOnly", err)
	}

	// The person's decisions go through.
	if err := ed.ResolveTaskPermission("t2", true); err != nil {
		t.Fatalf("person ResolveTaskPermission: %v", err)
	}
	if err := ed.KeepTask("t1"); err != nil {
		t.Fatalf("person KeepTask: %v", err)
	}
	if t1, _ := ed.Task("t1"); t1.State != editor.TaskDone {
		t.Fatalf("t1 after person keep = %s, want done", t1.State)
	}
}

// No registered tool may let an agent keep a review or answer a permission prompt: tasks_control is the only tool whose actions reach KeepTask or ResolveTaskPermission, and every tool whose name suggests a decision is listed here with the reason it is not a person-only gate.
func TestNoToolLetsAgentKeepReviewOrResolveAsk(t *testing.T) {
	ed := newSlimesEditor(t)
	s := editormcp.New(ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	notPersonGates := map[string]string{
		// The tile review is an opt-in grouping the agent asks for with review: true; keeping it folds the agent's own tile edits into one undo step, and the turn or task review that contains them is still the person's.
		editormcp.ToolTileReviewKeep: "agent-owned tile edit grouping",
		// Picks a side in a Git conflict the person started; merge_finish still runs game check and a playtest.
		editormcp.ToolMergeResolve: "Git conflict resolution",
	}
	decision := []string{"keep", "approve", "accept", "allow", "confirm", "resolve", "answer"}
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		lower := strings.ToLower(tool.Name)
		for _, word := range decision {
			if strings.Contains(lower, word) {
				if _, ok := notPersonGates[tool.Name]; !ok {
					t.Errorf("tool %s looks like a review or permission decision; refuse it for agents or list why it is not a person-only gate", tool.Name)
				}
			}
		}
	}

	_, _ = ed.SubmitTask(editor.TaskRequest{ID: "t1", Number: 1, Title: "Task", StartMode: editor.TaskStartParallel})
	if err := ed.RequestTaskPermission("t1", "Edit game.yaml?", ""); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"keep", "allow", "deny"} {
		if res := call(t, cs, editormcp.ToolTasksControl, map[string]any{"action": action, "task_id": "t1"}); !res.IsError {
			t.Errorf("tasks_control %s succeeded for an agent", action)
		}
	}
}
