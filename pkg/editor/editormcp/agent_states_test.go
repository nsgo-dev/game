package editormcp_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editormcp"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestScriptedMCPThreeFailedSceneEditsPauseRequest verifies the #1024 Stuck acceptance criterion:
// three consecutive refused scene_edit calls from a scripted MCP client pause the request,
// list all three calls with their errors, and touch neither the scene nor the file on disk.
func TestScriptedMCPThreeFailedSceneEditsPauseRequest(t *testing.T) {
	f := openFixture(t)
	f.ed.SetRightRailTab(editor.RightTabAgent)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	diskBefore, err := os.ReadFile(f.scenePath())
	if err != nil {
		t.Fatal(err)
	}
	histBefore := f.ed.History.Index()

	// 1. Unknown component "lock"
	res1 := call(t, cs, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{map[string]any{"op": "add_component", "id": "hero", "component": "lock"}},
	})
	if !res1.IsError {
		t.Fatalf("call 1 unexpectedly succeeded: %s", text(res1))
	}
	// 2. Unknown field "locked"
	res2 := call(t, cs, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{map[string]any{"op": "set", "id": "hero", "component": "body", "fields": map[string]any{"locked": true}}},
	})
	if !res2.IsError {
		t.Fatalf("call 2 unexpectedly succeeded: %s", text(res2))
	}
	// 3. Missing object "lever_01"
	res3 := call(t, cs, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{map[string]any{"op": "set", "id": "lever_01", "fields": map[string]any{"x": 100}}},
	})
	if !res3.IsError {
		t.Fatalf("call 3 unexpectedly succeeded: %s", text(res3))
	}

	// Request is paused in Stuck state with all 3 refused calls & errors.
	if f.ed.AgentStatus() != editor.AgentStatusWaitingForYou {
		t.Fatalf("AgentStatus = %q, want waiting (stuck pause)", f.ed.AgentStatus())
	}
	turns := f.ed.AgentTurns()
	if len(turns) == 0 || turns[len(turns)-1].Stuck == nil {
		t.Fatalf("expected Stuck state on turn, got %+v", turns)
	}
	stuck := turns[len(turns)-1].Stuck
	if stuck.ToolName != editormcp.ToolSceneEdit || len(stuck.Failures) != 3 || !stuck.NothingChanged {
		t.Fatalf("stuck = %+v, want 3 scene_edit failures with NothingChanged=true", stuck)
	}
	if !strings.Contains(stuck.Failures[0].Call, "add hero.lock") {
		t.Fatalf("failure 0 call = %q", stuck.Failures[0].Call)
	}
	if !strings.Contains(stuck.Failures[1].Call, "set hero.body.locked") {
		t.Fatalf("failure 1 call = %q", stuck.Failures[1].Call)
	}
	if !strings.Contains(stuck.Failures[2].Call, "set lever_01.x") {
		t.Fatalf("failure 2 call = %q", stuck.Failures[2].Call)
	}

	// Nothing changed in editor history or on disk.
	if f.ed.History.Index() != histBefore {
		t.Fatalf("history index = %d, want %d", f.ed.History.Index(), histBefore)
	}
	diskAfter, err := os.ReadFile(f.scenePath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(diskBefore, diskAfter) {
		t.Fatal("refused edits touched the scene file on disk")
	}
}

// TestScriptedMCPDroppedConnectionLeavesReviewAndNoHalfWrittenFile verifies the #1024 Lost
// acceptance criterion: a dropped MCP connection mid-request leaves completed work in a review,
// leaves no partly written file on disk, and asks before picking up after reconnecting.
func TestScriptedMCPDroppedConnectionLeavesReviewAndNoHalfWrittenFile(t *testing.T) {
	f := openFixture(t)
	now := time.Date(2026, 11, 26, 14, 2, 0, 0, time.UTC)
	f.ed.Now = func() time.Time { return now }
	f.ed.AgentScheduleAfter = func(_ time.Duration, _ func()) func() { return func() {} }
	f.ed.SetRightRailTab(editor.RightTabAgent)

	diskBefore, err := os.ReadFile(f.scenePath())
	if err != nil {
		t.Fatal(err)
	}

	var s *editormcp.Server
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = editormcp.New(f.ed, editormcp.Options{Run: cx.Post})
		cx.Cleanup(func() { _ = s.Close() })
		return editor.EditorView(cx, f.ed)
	}, nsgotest.Size(1280, 820))

	runMCP := func(fn func()) {
		t.Helper()
		done := make(chan struct{})
		go func() {
			defer close(done)
			fn()
		}()
		for {
			app.Settle()
			select {
			case <-done:
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
	}

	// Connect scripted MCP client #1.
	var cs *mcp.ClientSession
	runMCP(func() {
		ct, st := mcp.NewInMemoryTransports()
		if _, err := s.Connect(context.Background(), st); err != nil {
			t.Errorf("Connect: %v", err)
			return
		}
		client := mcp.NewClient(&mcp.Implementation{Name: "claude-code"}, nil)
		var err error
		cs, err = client.Connect(context.Background(), ct, nil)
		if err != nil {
			t.Errorf("client.Connect: %v", err)
		}
	})
	if cs == nil {
		t.Fatal("client session nil")
	}

	// Scripted MCP client performs two completed steps (read + edit adding 2 coins).
	runMCP(func() {
		mustOK(t, call(t, cs, editormcp.ToolSceneRead, map[string]any{}))
		mustOK(t, call(t, cs, editormcp.ToolSceneEdit, map[string]any{
			"edits": []any{
				map[string]any{"op": "add", "layer": "actors", "object": map[string]any{"id": "coin_04", "x": 120, "y": 40}},
				map[string]any{"op": "add", "layer": "actors", "object": map[string]any{"id": "coin_05", "x": 150, "y": 40}},
			},
		}))
	})

	// Drop the MCP client connection abruptly mid-request!
	_ = cs.Close()

	app.WaitUntil(3*time.Second, func() bool {
		turns := f.ed.AgentTurns()
		return len(turns) > 0 && turns[len(turns)-1].Lost != nil
	})

	var turns []editor.AgentTurn
	var hasOpenReview bool
	app.Post(func() {
		turns = f.ed.AgentTurns()
		hasOpenReview = f.ed.HasOpenReview()
	})
	app.Settle()
	if len(turns) == 0 || turns[len(turns)-1].Lost == nil {
		t.Fatalf("expected Lost state after dropped MCP connection, got %+v", turns)
	}
	last := turns[len(turns)-1]
	if last.Lost.AfterStep != 2 || last.Lost.DuringStep != 3 {
		t.Fatalf("Lost steps = after %d during %d, want after 2 during 3", last.Lost.AfterStep, last.Lost.DuringStep)
	}
	if last.Review == nil || len(last.Review.Added) != 2 || last.Review.Kept {
		t.Fatalf("Lost turn review = %+v, want 2 unkept added objects", last.Review)
	}
	if !hasOpenReview {
		t.Fatal("HasOpenReview should be true while Lost review is unkept so autosave pauses")
	}

	// Disk file is completely intact (no partly written file on disk).
	diskAfter, err := os.ReadFile(f.scenePath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(diskBefore, diskAfter) {
		t.Fatal("dropped connection left a partly written file on disk")
	}

	// Reconnecting with client #2 asks before picking up instead of auto-running.
	runMCP(func() {
		_ = connect(t, s, nil)
	})
	var status editor.AgentStatus
	app.Post(func() {
		turns = f.ed.AgentTurns()
		status = f.ed.AgentStatus()
	})
	app.Settle()
	if !turns[len(turns)-1].Lost.AskResumePending {
		t.Fatal("reconnecting did not enter ask-before-pickup state")
	}
	if status != editor.AgentStatusWaitingForYou {
		t.Fatalf("AgentStatus after reconnect = %q, want waiting", status)
	}
}

// TestScriptedMCPMismatchSummaryThreeVsDiffTwo verifies the #1024 Mismatch acceptance criterion:
// a summary claiming three added objects against an actual scene_edit diff with two shows
// the mismatch table with the exact reason, and Keep applies only the two objects in the review.
func TestScriptedMCPMismatchSummaryThreeVsDiffTwo(t *testing.T) {
	f := openFixture(t)
	f.ed.SetRightRailTab(editor.RightTabAgent)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// Step 1: add slime_01 and slime_02.
	mustOK(t, call(t, cs, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{
			map[string]any{"op": "add", "layer": "actors", "object": map[string]any{"id": "slime_01", "x": 100, "y": 40}},
			map[string]any{"op": "add", "layer": "actors", "object": map[string]any{"id": "slime_02", "x": 140, "y": 40}},
		},
	}))
	// Step 2: add slime_03.
	mustOK(t, call(t, cs, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{
			map[string]any{"op": "add", "layer": "actors", "object": map[string]any{"id": "slime_03", "x": 180, "y": 40}},
		},
	}))
	// Step 3: agent's own retry removes slime_03.
	mustOK(t, call(t, cs, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{
			map[string]any{"op": "remove", "id": "slime_03"},
		},
	}))

	// Agent claims three slimes were added.
	mm := f.ed.RecordAgentTurnSummary("", "Done. Three slimes patrol the lower floor. I left the hero untouched.")
	if mm == nil {
		t.Fatal("expected mismatch report for summary claiming 3 slimes vs 2 in review")
	}
	if mm.Title != "Summary says 3, review has 2" {
		t.Fatalf("mismatch title = %q, want Summary says 3, review has 2", mm.Title)
	}
	if len(mm.Rows) < 2 || !mm.Rows[0].Disagrees || mm.Rows[0].Summary != "3" || mm.Rows[0].Review != "2" {
		t.Fatalf("mismatch rows = %+v", mm.Rows)
	}
	if !strings.Contains(mm.Reason, "slime_03") || !strings.Contains(mm.Reason, "step 2") || !strings.Contains(mm.Reason, "step 3") {
		t.Fatalf("mismatch reason = %q, want explanation of slime_03 add/remove steps", mm.Reason)
	}

	// Keep applies the review (2 slimes), not the summary (3 slimes).
	if !f.ed.KeepAgentTurnDiffReview("") {
		t.Fatal("KeepAgentTurnDiffReview failed")
	}
	if obj, _, _ := f.ed.FindObject("slime_03"); obj != nil {
		t.Fatal("slime_03 should not exist after keeping review of 2 slimes")
	}
	if obj, _, _ := f.ed.FindObject("slime_01"); obj == nil {
		t.Fatal("slime_01 missing after Keep")
	}
	if obj, _, _ := f.ed.FindObject("slime_02"); obj == nil {
		t.Fatal("slime_02 missing after Keep")
	}
}

// TestScriptedMCPResumeAfterStopSkipsDoneSteps verifies the #1024 Stopped/Resume acceptance criterion:
// stopping a request that executes MCP calls mid-way and pressing Resume skips the already-done
// steps and only runs the remaining MCP calls.
func TestScriptedMCPResumeAfterStopSkipsDoneSteps(t *testing.T) {
	f := openFixture(t)
	f.ed.SetRightRailTab(editor.RightTabAgent)

	var s *editormcp.Server
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = editormcp.New(f.ed, editormcp.Options{Run: cx.Post})
		cx.Cleanup(func() { _ = s.Close() })
		return editor.EditorView(cx, f.ed)
	}, nsgotest.Size(1280, 820))

	runMCP := func(fn func()) {
		t.Helper()
		done := make(chan struct{})
		go func() {
			defer close(done)
			fn()
		}()
		for {
			app.Settle()
			select {
			case <-done:
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
	}

	var cs *mcp.ClientSession
	runMCP(func() {
		cs = connect(t, s, nil)
	})

	stoppedGate := make(chan struct{})
	var stepMu sync.Mutex
	var executedSteps []string
	var runnerDone sync.WaitGroup
	f.ed.AgentRunner = editor.AgentRunnerFunc(func(ctx context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		runnerDone.Add(1)
		defer runnerDone.Done()
		if req.ResumeFromStep <= 1 {
			stepMu.Lock()
			executedSteps = append(executedSteps, "place-slimes")
			stepMu.Unlock()
			mustOK(t, call(t, cs, editormcp.ToolSceneEdit, map[string]any{
				"edits": []any{
					map[string]any{"op": "add", "layer": "actors", "object": map[string]any{"id": "slime_01", "x": 100, "y": 40}},
					map[string]any{"op": "add", "layer": "actors", "object": map[string]any{"id": "slime_02", "x": 140, "y": 40}},
				},
			}))
			emit(editor.AgentEvent{
				Kind:         editor.AgentEventStep,
				StepID:       "s1",
				StepTitle:    "Place slimes",
				StepStatus:   editor.AgentStepDone,
				StepIndex:    1,
				StepTotal:    3,
				PlannedSteps: []string{"Place slimes", "Run game check", "Place coins"},
			})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "s2", StepTitle: "Run game check", StepStatus: editor.AgentStepRunning, StepIndex: 2, StepTotal: 3})
			close(stoppedGate)
			<-ctx.Done()
			return ctx.Err()
		}
		// Resume starts at step 2 ("Run game check"), never re-running "Place slimes".
		for _, rem := range req.RemainingSteps {
			stepMu.Lock()
			executedSteps = append(executedSteps, rem)
			stepMu.Unlock()
			if rem == "Run game check" {
				mustOK(t, call(t, cs, editormcp.ToolGameCheck, map[string]any{}))
				emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "s2", StepTitle: "Run game check", StepStatus: editor.AgentStepDone, StepIndex: 2, StepTotal: 3})
			}
			if rem == "Place coins" {
				mustOK(t, call(t, cs, editormcp.ToolSceneEdit, map[string]any{
					"edits": []any{
						map[string]any{"op": "add", "layer": "actors", "object": map[string]any{"id": "coin_01", "x": 200, "y": 40}},
					},
				}))
				emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "s3", StepTitle: "Place coins", StepStatus: editor.AgentStepDone, StepIndex: 3, StepTotal: 3})
			}
		}
		emit(editor.AgentEvent{Kind: editor.AgentEventDone})
		return nil
	})

	app.Post(func() {
		if err := f.ed.SubmitAgentPrompt("Place slimes, check, and place coins"); err != nil {
			t.Errorf("SubmitAgentPrompt: %v", err)
		}
	})
	app.WaitUntil(5*time.Second, func() bool {
		select {
		case <-stoppedGate:
			turns := f.ed.AgentTurns()
			return len(turns) == 1 && len(turns[0].Steps) >= 2
		default:
			return false
		}
	})
	app.Post(func() {
		f.ed.StopAgentRun()
	})
	app.Post(func() {
		if err := f.ed.ResumeAgentTurn(""); err != nil {
			t.Errorf("ResumeAgentTurn: %v", err)
		}
	})
	app.WaitUntil(5*time.Second, func() bool {
		stepMu.Lock()
		n := len(executedSteps)
		stepMu.Unlock()
		return !f.ed.AgentWorking() && n >= 3
	})
	runnerDone.Wait()
	app.Settle()

	stepMu.Lock()
	gotSteps := append([]string(nil), executedSteps...)
	stepMu.Unlock()
	want := []string{"place-slimes", "Run game check", "Place coins"}
	if strings.Join(gotSteps, ",") != strings.Join(want, ",") {
		t.Fatalf("executedSteps = %v, want %v (done step must not be redone)", gotSteps, want)
	}
	var found bool
	app.Post(func() {
		obj, _, _ := f.ed.FindObject("coin_01")
		found = obj != nil
	})
	app.Settle()
	if !found {
		t.Fatal("coin_01 not placed on Resume")
	}
}
