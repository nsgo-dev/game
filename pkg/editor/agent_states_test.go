package editor_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/theme"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func postAndWait(ed *editor.Editor, fn func()) {
	done := make(chan struct{})
	ed.Post(func() {
		fn()
		close(done)
	})
	<-done
}

func TestAgentStatePartialKeepPointEscAndTryAnotherWay(t *testing.T) {
	ed := newTestEditor(t)
	ed.SetRightRailTab(editor.RightTabAgent)
	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{
		ID:        "c1",
		Name:      "claude-code",
		Transport: "stdio",
	})

	var gotFollowup editor.AgentRequest
	var pointedTarget string
	var pointedX, pointedY float32
	ed.OnAgentPointPicked = func(_, target string, wx, wy float32) {
		pointedTarget = target
		pointedX, pointedY = wx, wy
	}

	callCount := 0
	ed.AgentRunner = editor.AgentRunnerFunc(func(_ context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		callCount++
		if callCount == 1 {
			postAndWait(ed, func() {
				_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "slime_01", X: 220, Y: 180})
				_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "slime_02", X: 310, Y: 180})
			})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "s1", StepTitle: "Placed slime_01 and slime_02 · patrol x 220–340", StepStatus: editor.AgentStepDone})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "s2", StepTitle: "slime_03: no free floor 48 px wide; the widest left is 32 px at x 590", StepStatus: editor.AgentStepWarning})
			emit(editor.AgentEvent{
				Kind: editor.AgentEventDone,
				Partial: &editor.AgentPartialState{
					DoneCount:   2,
					TotalCount:  3,
					Target:      "slime_03",
					Reason:      "No floor is wide enough for a third patrol. The plank at x 400–576 has room, but the Knight starts on it.",
					PointPrompt: "Click where slime_03 should patrol",
				},
			})
			return nil
		}
		gotFollowup = req
		emit(editor.AgentEvent{Kind: editor.AgentEventDone})
		return nil
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 820))

	if err := ed.SubmitAgentPrompt("Put three slimes patrolling the lower floor."); err != nil {
		t.Fatalf("SubmitAgentPrompt: %v", err)
	}
	app.WaitUntil(5*time.Second, func() bool {
		return app.Exists("id:agent-partial-card-0")
	})

	if title := agentViewText(t, app, "id:agent-partial-title-0"); title != "Done 2 of 3 · couldn't place slime_03" {
		t.Fatalf("partial title = %q, want Done 2 of 3 · couldn't place slime_03", title)
	}
	if !app.Exists("id:agent-review-bar-0") {
		t.Fatal("partial review bar missing")
	}

	// "Let me point" enters pointing mode; Esc cancels cleanly without calling OnAgentPointPicked.
	app.Press("id:agent-partial-point-0")
	app.Settle()
	if !ed.AgentPointingActive() || !app.Exists("id:agent-partial-point-banner-0") || !app.Exists("id:viewport-agent-pointing-pill") {
		t.Fatal("Let me point did not activate pointing banner and viewport pill")
	}
	app.Key("escape")
	app.Settle()
	if ed.AgentPointingActive() || app.Exists("id:agent-partial-point-banner-0") {
		t.Fatal("Esc did not cancel Let me point")
	}
	if pointedTarget != "" {
		t.Fatalf("Esc triggered point pick unexpectedly: %q", pointedTarget)
	}

	// Re-enter pointing mode and click in the 2D viewport -> picks world coordinates.
	app.Press("id:agent-partial-point-0")
	app.Settle()
	ed.HandleCanvasPress(240, 160)
	app.WaitUntil(5*time.Second, func() bool {
		return !ed.AgentPointingActive() && callCount == 2 && !ed.AgentWorking()
	})
	if pointedTarget != "slime_03" {
		t.Fatalf("pointedTarget = %q, want slime_03", pointedTarget)
	}
	if gotFollowup.PointedWorld == nil || gotFollowup.PointedTarget != "slime_03" {
		t.Fatalf("follow-up PointedWorld = %v, PointedTarget = %q", gotFollowup.PointedWorld, gotFollowup.PointedTarget)
	}
	_ = pointedX
	_ = pointedY

	// "Keep these" dismisses the partial card, marks review kept, and shows the kept confirmation.
	app.Press("id:agent-partial-keep-0")
	app.Settle()
	if app.Exists("id:agent-partial-card-0") || !app.Exists("id:agent-partial-kept-0") {
		t.Fatal("Keep these did not replace partial card with kept confirmation")
	}
	turns := ed.AgentTurns()
	if turns[0].Review == nil || !turns[0].Review.Kept {
		t.Fatalf("Keep these did not mark turn review kept: %+v", turns[0].Review)
	}

	// Manual tools keep working after Keep these (including Undo).
	ed.SetTool(editor.ToolMove)
	if ed.Tool != editor.ToolMove {
		t.Fatal("manual Move tool failed in Partial state")
	}

	// Verify "Try another way" on a second partial turn.
	ed.MarkAgentTurnPartial("", editor.AgentPartialState{
		DoneCount:  2,
		TotalCount: 3,
		Target:     "slime_03",
		Reason:     "No floor is wide enough.",
	})
	app.Settle()
	app.Press("id:agent-partial-try-0")
	app.WaitUntil(5*time.Second, func() bool {
		return callCount == 3 && !ed.AgentWorking()
	})
	if !gotFollowup.TryAnotherWay {
		t.Fatal("Try another way did not set AgentRequest.TryAnotherWay")
	}
}

func TestAgentStateStuckThreeFailuresHintAndStop(t *testing.T) {
	ed := newTestEditor(t)
	ed.SetRightRailTab(editor.RightTabAgent)
	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{
		ID:        "c1",
		Name:      "claude-code",
		Transport: "stdio",
	})

	runCanceled := make(chan struct{})
	var hintReq editor.AgentRequest
	runNum := 0
	ed.AgentRunner = editor.AgentRunnerFunc(func(ctx context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		runNum++
		if runNum == 1 {
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "r1", StepTitle: "Read door.prefab.yaml", ToolName: "scene_read", StepStatus: editor.AgentStepDone})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "f1", StepTitle: "Edit scene", ToolName: "scene_edit", CallSummary: "add Door.lock", StepDetail: `unknown component "lock"`, StepStatus: editor.AgentStepFailed})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "f2", StepTitle: "Edit scene", ToolName: "scene_edit", CallSummary: "set Door.trigger.locked: true", StepDetail: `unknown field "locked"`, StepStatus: editor.AgentStepFailed})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "f3", StepTitle: "Edit scene", ToolName: "scene_edit", CallSummary: "set Door.trigger.requires: lever_01", StepDetail: `no node "lever_01" in level_01`, StepStatus: editor.AgentStepFailed})
			<-ctx.Done()
			close(runCanceled)
			return ctx.Err()
		}
		hintReq = req
		emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "ok1", StepTitle: "Linked Door.trigger.requires to lever_door", ToolName: "scene_edit", StepStatus: editor.AgentStepDone})
		emit(editor.AgentEvent{Kind: editor.AgentEventDone})
		return nil
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 820))

	if err := ed.SubmitAgentPrompt("Make the door open only after lever_01 is pulled."); err != nil {
		t.Fatal(err)
	}
	// Pump the UI event loop so the 3 emitted failure events are processed and cancel the active run.
	app.WaitUntil(5*time.Second, func() bool {
		return app.Exists("id:agent-stuck-card-0")
	})
	select {
	case <-runCanceled:
	case <-time.After(5 * time.Second):
		t.Fatal("3 consecutive tool failures did not pause/cancel the active agent run")
	}

	if ed.AgentStatus() != editor.AgentStatusWaitingForYou {
		t.Fatalf("AgentStatus = %q, want waiting", ed.AgentStatus())
	}
	if title := agentViewText(t, app, "id:agent-stuck-title-0"); title != "Paused instead of guessing again" {
		t.Fatalf("stuck title = %q", title)
	}
	if got := agentViewText(t, app, "id:agent-stuck-call-0-0"); got != "1 add Door.lock" {
		t.Fatalf("failure 1 call = %q", got)
	}
	if got := agentViewText(t, app, "id:agent-stuck-err-0-2"); got != `no node "lever_01" in level_01` {
		t.Fatalf("failure 3 err = %q", got)
	}

	// Continue with hint passes the hint to the runner and resumes work.
	ed.SetAgentStuckHint("", "The lever is lever_door, under Actors")
	app.Settle()
	app.Press("id:agent-stuck-continue-0")
	app.WaitUntil(5*time.Second, func() bool {
		return runNum == 2 && ed.AgentStatus() == editor.AgentStatusConnected
	})
	if hintReq.Hint != "The lever is lever_door, under Actors" {
		t.Fatalf("Hint = %q, want The lever is lever_door, under Actors", hintReq.Hint)
	}

	// Verify Stop here on a second stuck turn leaves nothing changed.
	for range 3 {
		ed.RecordAgentToolCall(editor.AgentToolCallRecord{
			ClientName:  "Claude Code",
			ToolName:    "scene_edit",
			CallSummary: "add Door.lock",
			ErrorText:   `unknown component "lock"`,
			IsError:     true,
		})
	}
	app.Settle()
	app.Press("id:agent-stuck-stop-1")
	app.Settle()
	if got := agentViewText(t, app, "id:agent-stuck-stopped-1"); !strings.Contains(got, "Nothing was changed") {
		t.Fatalf("stuck stopped message = %q", got)
	}
}

func TestAgentStateStoppedResumeSkipsDoneStepsAndKeepsUnkeptReview(t *testing.T) {
	ed := newTestEditor(t)
	ed.SetRightRailTab(editor.RightTabAgent)
	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{
		ID:        "c1",
		Name:      "claude-code",
		Transport: "stdio",
	})

	var resumedReq editor.AgentRequest
	runs := 0
	ed.AgentRunner = editor.AgentRunnerFunc(func(ctx context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		runs++
		if runs == 1 {
			postAndWait(ed, func() {
				_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "slime_01", X: 220, Y: 180})
				_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "coin_01", X: 300, Y: 180})
			})
			emit(editor.AgentEvent{
				Kind:         editor.AgentEventStep,
				StepID:       "s1",
				StepTitle:    "Read the scene and the slime prefab",
				StepStatus:   editor.AgentStepDone,
				StepIndex:    1,
				StepTotal:    5,
				PlannedSteps: []string{"Read the scene and the slime prefab", "Placed slime_01 to slime_03", "Placed coin_01 to coin_03", "Run game check", "Play 10 s headless"},
			})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "s2", StepTitle: "Placed slime_01 to slime_03", StepStatus: editor.AgentStepDone, StepIndex: 2, StepTotal: 5})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "s3", StepTitle: "Placed coin_01 to coin_03", StepStatus: editor.AgentStepDone, StepIndex: 3, StepTotal: 5})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "s4", StepTitle: "Run game check", StepStatus: editor.AgentStepRunning, StepIndex: 4, StepTotal: 5})
			<-ctx.Done()
			return ctx.Err()
		}
		resumedReq = req
		emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "s4", StepTitle: "Run game check", StepStatus: editor.AgentStepDone, StepIndex: 4, StepTotal: 5})
		emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "s5", StepTitle: "Play 10 s headless", StepStatus: editor.AgentStepDone, StepIndex: 5, StepTotal: 5})
		emit(editor.AgentEvent{Kind: editor.AgentEventDone})
		return nil
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 820))

	if err := ed.SubmitAgentPrompt("Put three slimes patrolling the lower floor and a short coin trail."); err != nil {
		t.Fatal(err)
	}
	app.WaitUntil(5*time.Second, func() bool {
		turns := ed.AgentTurns()
		return len(turns) == 1 && len(turns[0].Steps) == 4 && app.Exists("id:agent-composer-stop")
	})

	// Stop the active run.
	app.Press("id:agent-composer-stop")
	app.Settle()

	if !app.Exists("id:agent-stopped-card-0") || !app.Exists("id:agent-stopped-done-0") || !app.Exists("id:agent-stopped-todo-0") {
		t.Fatal("Stopped card missing done or not-done sections")
	}
	if title := agentViewText(t, app, "id:agent-stopped-title-0"); title != "Stopped at step 3 of 5" {
		t.Fatalf("stopped title = %q, want Stopped at step 3 of 5", title)
	}
	turns := ed.AgentTurns()
	if turns[0].Review == nil || turns[0].Review.Count() != 2 || turns[0].Review.Kept {
		t.Fatalf("stopped turn review = %+v, want 2 unkept changes", turns[0].Review)
	}

	// Resume skips steps 1..3 and starts at step 4 ("Run game check").
	app.Press("id:agent-stopped-resume-0")
	app.WaitUntil(5*time.Second, func() bool {
		return runs == 2 && ed.AgentStatus() == editor.AgentStatusConnected
	})
	if resumedReq.ResumeFromStep != 4 {
		t.Fatalf("ResumeFromStep = %d, want 4", resumedReq.ResumeFromStep)
	}
	if len(resumedReq.CompletedSteps) != 3 {
		t.Fatalf("CompletedSteps = %v, want 3 done steps skipped", resumedReq.CompletedSteps)
	}
	if len(resumedReq.RemainingSteps) != 2 || resumedReq.RemainingSteps[0] != "Run game check" {
		t.Fatalf("RemainingSteps = %v, want [Run game check, Play 10 s headless]", resumedReq.RemainingSteps)
	}
	// Changes still remain unkept until explicitly reviewed and kept.
	turns = ed.AgentTurns()
	if turns[0].Review == nil || turns[0].Review.Kept {
		t.Fatalf("changes became kept automatically without review: %+v", turns[0].Review)
	}
	app.Settle()
	// Opening review and pressing Keep marks them kept.
	ed.OpenAgentTurnReview(turns[0].ID)
	app.Settle()
	if !app.Exists("id:agent-review-drawer") {
		t.Fatal("OpenAgentTurnReview did not open review drawer")
	}
	app.Press("id:agent-review-keep")
	app.Settle()
	if !ed.AgentTurns()[0].Review.Kept {
		t.Fatal("Keep in review drawer did not keep changes")
	}
}

func TestAgentStateLostConnectionModelErrorAndPostReconnectAsk(t *testing.T) {
	ed := newTestEditor(t)
	now := time.Date(2026, 11, 26, 14, 2, 0, 0, time.UTC)
	ed.Now = func() time.Time { return now }
	sched := &fakeRetryScheduler{}
	ed.AgentScheduleAfter = sched.schedule
	ed.SetRightRailTab(editor.RightTabAgent)
	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{
		ID:        "c1",
		Name:      "claude-code",
		Transport: "stdio",
	})

	resumedCalls := 0
	ed.AgentRunner = editor.AgentRunnerFunc(func(ctx context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		if req.ResumeFromStep > 0 {
			resumedCalls++
			emit(editor.AgentEvent{Kind: editor.AgentEventDone})
			return nil
		}
		postAndWait(ed, func() {
			_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "coin_04", X: 200, Y: 180})
			_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "coin_05", X: 230, Y: 180})
		})
		emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "s1", StepTitle: "Read level_01.scene.yaml", StepStatus: editor.AgentStepDone})
		emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "s2", StepTitle: "Placed coin_04 to coin_05", StepStatus: editor.AgentStepDone})
		emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "s3", StepTitle: "Placing coin_06", StepStatus: editor.AgentStepRunning})
		<-ctx.Done()
		return ctx.Err()
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 820))

	if err := ed.SubmitAgentPrompt("Add a coin trail from the lever to the door."); err != nil {
		t.Fatal(err)
	}
	app.WaitUntil(5*time.Second, func() bool {
		return len(ed.AgentTurns()) == 1 && len(ed.AgentTurns()[0].Steps) == 3
	})

	// Connection drops mid-request at 14:02 during step 3.
	ed.NotifyAgentMCPDisconnected("c1", "")
	for range 5 {
		sched.fireLast()
	}
	app.Settle()

	if !app.Exists("id:agent-lost-card-0") {
		t.Fatal("Lost card did not appear after mid-request disconnect")
	}
	if title := agentViewText(t, app, "id:agent-lost-title-0"); title != "Connection lost after step 2" {
		t.Fatalf("lost title = %q, want Connection lost after step 2", title)
	}
	if body := agentViewText(t, app, "id:agent-lost-body-0"); !strings.Contains(body, "14:02") || !strings.Contains(body, "step 3") {
		t.Fatalf("lost body = %q, want timestamp 14:02 and step 3", body)
	}
	if !app.Exists("id:agent-lost-reconnect-0") || !app.Exists("id:agent-lost-review-0") || !app.Exists("id:agent-lost-troubleshoot-0") {
		t.Fatal("Lost card missing Reconnect, Review, or Troubleshoot button")
	}

	// Reconnecting must NOT auto-resume; instead the agent asks before picking up at step 3.
	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{
		ID:        "c2",
		Name:      "claude-code",
		Transport: "stdio",
	})
	app.Settle()
	if resumedCalls != 0 {
		t.Fatalf("reconnecting auto-resumed without asking (resumedCalls=%d)", resumedCalls)
	}
	if !app.Exists("id:agent-lost-ask-resume-0") {
		t.Fatal("post-reconnect ask-before-pickup card did not appear")
	}
	if ed.AgentStatus() != editor.AgentStatusWaitingForYou {
		t.Fatalf("AgentStatus after reconnect = %q, want waiting", ed.AgentStatus())
	}

	// Confirming picks up at step 3.
	app.Press("id:agent-lost-resume-confirm-0")
	app.WaitUntil(5*time.Second, func() bool {
		return resumedCalls == 1 && !ed.AgentWorking()
	})

	// Model error variant (529 overloaded) with Retry now and Use Claude Code.
	ed.ReportAgentModelError("", editor.AgentLostState{
		Kind:          editor.AgentLostModelError,
		Title:         "Anthropic API is overloaded (529)",
		RetryAttempt:  2,
		RetryTotal:    4,
		RetryIn:       8 * time.Second,
		FallbackAgent: "Claude Code",
	})
	app.Settle()
	if title := agentViewText(t, app, "id:agent-lost-title-0"); title != "Anthropic API is overloaded (529)" {
		t.Fatalf("model error title = %q", title)
	}
	if !app.Exists("id:agent-lost-retry-0") || !app.Exists("id:agent-lost-fallback-0") {
		t.Fatal("model error card missing Retry now or Use Claude Code button")
	}
}

func TestAgentStateMismatchSummaryVsDiffAndKeepAppliesReview(t *testing.T) {
	ed := newTestEditor(t)
	ed.SetRightRailTab(editor.RightTabAgent)
	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{
		ID:        "c1",
		Name:      "claude-code",
		Transport: "stdio",
	})

	askedWhy := make(chan string, 1)
	runs := 0
	ed.AgentRunner = editor.AgentRunnerFunc(func(_ context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		runs++
		if runs > 1 {
			askedWhy <- req.Prompt
			emit(editor.AgentEvent{Kind: editor.AgentEventDone})
			return nil
		}
		postAndWait(ed, func() {
			_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "slime_01", X: 220, Y: 180})
			_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "slime_02", X: 280, Y: 180})
		})
		emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "s1", StepTitle: "Placed slime_01 and slime_02", StepStatus: editor.AgentStepDone, StepIndex: 1})
		postAndWait(ed, func() {
			_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "slime_03", X: 340, Y: 180})
		})
		emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "s3", StepTitle: "Placed slime_03", StepStatus: editor.AgentStepDone, StepIndex: 3})
		postAndWait(ed, func() {
			_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "coin_01", X: 360, Y: 160})
			_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "coin_02", X: 390, Y: 160})
			_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "coin_03", X: 420, Y: 160})
		})
		emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "s4", StepTitle: "Placed 3 coins", StepStatus: editor.AgentStepDone, StepIndex: 4})
		postAndWait(ed, func() {
			_ = ed.DeleteObject("slime_03")
		})
		emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "s5", StepTitle: "Retried placement", StepStatus: editor.AgentStepDone, StepIndex: 5})
		emit(editor.AgentEvent{
			Kind: editor.AgentEventMessage,
			Text: "Done. Three slimes patrol the lower floor and 3 coins lead to the door. I left the door untouched.",
		})
		emit(editor.AgentEvent{Kind: editor.AgentEventDone})
		return nil
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 820))

	if err := ed.SubmitAgentPrompt("Put three slimes patrolling the lower floor and 3 coins toward the door."); err != nil {
		t.Fatal(err)
	}
	app.WaitUntil(5*time.Second, func() bool {
		return app.Exists("id:agent-mismatch-card-0")
	})

	if title := agentViewText(t, app, "id:agent-mismatch-title-0"); title != "Summary says 3, review has 2" {
		t.Fatalf("mismatch title = %q, want Summary says 3, review has 2", title)
	}
	if reason := agentViewText(t, app, "id:agent-mismatch-reason-0"); !strings.Contains(reason, "slime_03") || !strings.Contains(reason, "step 3") || !strings.Contains(reason, "step 5") {
		t.Fatalf("mismatch reason = %q, want slime_03 added at step 3 and removed at step 5", reason)
	}

	// Open review -> Keep applies the review (2 slimes), never the summary (3 slimes).
	app.Press("id:agent-mismatch-open-review-0")
	app.Settle()
	if !app.Exists("id:agent-review-drawer") {
		t.Fatal("Open review did not open the review drawer")
	}
	app.Press("id:agent-review-keep")
	app.Settle()
	if obj, _, _ := ed.FindObject("slime_03"); obj != nil {
		t.Fatal("Keep fabricated slime_03 from summary instead of applying the review diff")
	}
	if obj, _, _ := ed.FindObject("slime_01"); obj == nil {
		t.Fatal("slime_01 missing after Keep")
	}
	if obj, _, _ := ed.FindObject("slime_02"); obj == nil {
		t.Fatal("slime_02 missing after Keep")
	}

	// Ask why sends a follow-up prompt naming the discrepancy.
	app.Press("id:agent-mismatch-ask-why-0")
	app.WaitUntil(5*time.Second, func() bool {
		return runs == 2
	})
	select {
	case prompt := <-askedWhy:
		if !strings.Contains(prompt, "Slimes added") || !strings.Contains(prompt, "3") || !strings.Contains(prompt, "2") {
			t.Fatalf("Ask why prompt = %q", prompt)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Ask why did not submit follow-up prompt")
	}
}

func TestAgentStateOfflineQueueManualToolsAndManualSendBackOnline(t *testing.T) {
	ed := newTestEditor(t)
	now := time.Date(2026, 11, 26, 14, 10, 0, 0, time.UTC)
	ed.Now = func() time.Time { return now }
	ed.SetRightRailTab(editor.RightTabAgent)
	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{
		ID:        "c1",
		Name:      "claude-code",
		Transport: "stdio",
	})

	sentPrompts := make(chan string, 4)
	ed.AgentRunner = editor.AgentRunnerFunc(func(_ context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		sentPrompts <- req.Prompt
		emit(editor.AgentEvent{Kind: editor.AgentEventDone})
		return nil
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 820))

	ed.SetAgentOffline(true)
	app.Settle()

	if got := agentViewText(t, app, "id:agent-status-text"); got != "Disconnected · offline" {
		t.Fatalf("offline status chip = %q, want Disconnected · offline", got)
	}
	if !app.Exists("id:agent-offline-card") || !app.Exists("id:agent-composer-queue") {
		t.Fatal("Offline card or Queue button missing")
	}

	// Manual tools keep working while offline: Select, Move, Inspector, game check, and Play.
	ed.SetTool(editor.ToolSelect)
	ed.SetTool(editor.ToolMove)
	if ed.Tool != editor.ToolMove {
		t.Fatal("Move tool blocked offline")
	}
	id, err := ed.PlaceObject("", sceneformat.ObjectDoc{ID: "hero_offline", X: 40, Y: 40})
	if err != nil {
		t.Fatalf("PlaceObject offline: %v", err)
	}
	ed.SelectObject(id)
	if err := ed.MoveObject(id, 80, 60); err != nil {
		t.Fatalf("MoveObject offline: %v", err)
	}
	ed.CheckProblems()
	if err := ed.Play(); err != nil {
		t.Fatalf("Play offline: %v", err)
	}
	if err := ed.Stop(); err != nil {
		t.Fatalf("Stop offline: %v", err)
	}

	// Queue two requests with timestamps while offline.
	if err := ed.SubmitAgentPrompt("Make the coins spin slowly"); err != nil {
		t.Fatalf("SubmitAgentPrompt while offline: %v", err)
	}
	now = now.Add(2 * time.Minute)
	if err := ed.SubmitAgentPrompt("Add a lever beside the door"); err != nil {
		t.Fatalf("SubmitAgentPrompt 2 while offline: %v", err)
	}
	app.Settle()

	if len(sentPrompts) != 0 {
		t.Fatal("requests ran while offline instead of queueing")
	}
	if hdr := agentViewText(t, app, "id:agent-queue-header"); hdr != "QUEUED · 2" {
		t.Fatalf("queue header = %q, want QUEUED · 2", hdr)
	}
	if meta := agentViewText(t, app, "id:agent-queue-meta-0"); meta != "queued 14:10" {
		t.Fatalf("queue meta 0 = %q, want queued 14:10", meta)
	}
	if meta := agentViewText(t, app, "id:agent-queue-meta-1"); meta != "queued 14:12" {
		t.Fatalf("queue meta 1 = %q, want queued 14:12", meta)
	}

	// Remove the first queued request.
	app.Press("id:agent-queue-remove-0")
	app.Settle()
	if hdr := agentViewText(t, app, "id:agent-queue-header"); hdr != "QUEUED · 1" {
		t.Fatalf("queue header after remove = %q, want QUEUED · 1", hdr)
	}

	// Go back online: NOTHING runs on its own; queued request waits for explicit Send.
	ed.SetAgentOffline(false)
	app.Settle()
	if len(sentPrompts) != 0 {
		t.Fatal("queued request ran automatically upon coming back online")
	}
	if !app.Exists("id:agent-queue-send-0") {
		t.Fatal("Send button not shown for waiting queued request once back online")
	}

	app.Press("id:agent-queue-send-0")
	app.WaitUntil(5*time.Second, func() bool {
		return len(sentPrompts) == 1
	})
	got := <-sentPrompts
	if got != "Add a lever beside the door" {
		t.Fatalf("sent queued prompt = %q, want Add a lever beside the door", got)
	}
}

func TestAgentFailureStatesUseOnlySpecTokensInLightAndDark(t *testing.T) {
	ed := newTestEditor(t)
	ed.SetRightRailTab(editor.RightTabAgent)
	ed.SetAgentOffline(true)
	ed.QueueAgentRequest("Make the coins spin slowly")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 820), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   false,
		Locale: "en-US",
	}))

	light := theme.Light()
	dark := theme.Dark()

	if got := app.Find("id:agent-offline-card").Style.Background; got != light.Raised {
		t.Fatalf("light offline card bg = %#x, want %#x", uint32(got), uint32(light.Raised))
	}
	if got := app.Find("id:agent-status-chip").Style.Background; got != light.Removed.Fill {
		t.Fatalf("light offline chip bg = %#x, want %#x", uint32(got), uint32(light.Removed.Fill))
	}

	app.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	})
	if got := app.Find("id:agent-offline-card").Style.Background; got != dark.Raised {
		t.Fatalf("dark offline card bg = %#x, want %#x", uint32(got), uint32(dark.Raised))
	}
	if got := app.Find("id:agent-status-chip").Style.Background; got != dark.Removed.Fill {
		t.Fatalf("dark offline chip bg = %#x, want %#x", uint32(got), uint32(dark.Removed.Fill))
	}
}

func TestAgentStatesHardeningDraftQueuePreconditionsAutosaveLifecycleUTF8AndEsc(t *testing.T) {
	ed := newTestEditor(t)
	ed.SetRightRailTab(editor.RightTabAgent)
	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{
		ID:        "c1",
		Name:      "claude-code",
		Transport: "stdio",
	})

	blockRun := make(chan struct{})
	runStarted := make(chan struct{}, 2)
	ed.AgentRunner = editor.AgentRunnerFunc(func(ctx context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		runStarted <- struct{}{}
		emit(editor.AgentEvent{
			Kind:         editor.AgentEventStep,
			StepID:       "s1",
			StepTitle:    "放置史莱姆",
			StepStatus:   editor.AgentStepRunning,
			PlannedSteps: []string{"放置史莱姆", "检查场景"},
		})
		select {
		case <-blockRun:
			emit(editor.AgentEvent{Kind: editor.AgentEventDone})
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 820), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   false,
		Locale: "en-US",
	}))

	// 1. Offline queueing with explicit prompt preserves unrelated unsent composer draft.
	ed.SetAgentComposerText("unsent draft")
	ed.SetAgentOffline(true)
	q1 := ed.QueueAgentRequest("queued prompt 1")
	q2 := ed.QueueAgentRequest("queued prompt 2")
	if ed.AgentComposerText() != "unsent draft" {
		t.Fatalf("composer draft wiped by explicit QueueAgentRequest: %q", ed.AgentComposerText())
	}
	ed.SetAgentOffline(false)

	// 2. SendQueuedAgentRequest preserves the item in-place if SubmitAgentPrompt fails (ErrPlaying).
	if err := ed.Play(); err != nil {
		t.Fatalf("Play: %v", err)
	}
	if err := ed.SendQueuedAgentRequest(q1.ID); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("SendQueuedAgentRequest during Play = %v, want ErrPlaying", err)
	}
	gotQueue := ed.AgentQueuedRequests()
	if len(gotQueue) != 2 || gotQueue[0].ID != q1.ID || gotQueue[1].ID != q2.ID {
		t.Fatalf("queue corrupted after failed SendQueuedAgentRequest: %+v", gotQueue)
	}
	if err := ed.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// 3. Esc while a modal (CommandPalette) is open MUST NOT stop a running agent turn!
	if err := ed.SendQueuedAgentRequest(q1.ID); err != nil {
		t.Fatalf("SendQueuedAgentRequest: %v", err)
	}
	<-runStarted
	app.Settle()
	if !ed.AgentWorking() {
		t.Fatal("expected active agent run")
	}
	if !ed.HasOpenReview() {
		t.Fatal("HasOpenReview should be true while an agent run is active")
	}
	ed.OpenCommandPalette()
	app.Settle()
	app.Key("escape")
	app.Settle()
	if !ed.AgentWorking() {
		t.Fatal("Esc while CommandPalette was open unexpectedly stopped the running agent turn")
	}

	// 4. Going offline mid-run cleanly interrupts the active run into Lost (no leaked goroutine).
	ed.SetAgentOffline(true)
	app.Settle()
	if ed.AgentWorking() {
		t.Fatal("SetAgentOffline(true) left active agent run working")
	}
	turns := ed.AgentTurns()
	if len(turns) == 0 || turns[len(turns)-1].Lost == nil {
		t.Fatalf("expected Lost state after going offline mid-run, got %+v", turns)
	}

	// 5. Reconnecting once prompts AskResumePending; dismissing it is NOT resurrected by duplicate connect notifications.
	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{ID: "c2", Name: "claude-code", Transport: "stdio"})
	app.Settle()
	if !ed.AgentTurns()[len(ed.AgentTurns())-1].Lost.AskResumePending {
		t.Fatal("first reconnect should prompt AskResumePending")
	}
	ed.DismissResumeLostAgentTurn("")
	app.Settle()
	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{ID: "c2", Name: "claude-code", Transport: "stdio"})
	app.Settle()
	if ed.AgentTurns()[len(ed.AgentTurns())-1].Lost.AskResumePending {
		t.Fatal("duplicate NotifyAgentMCPConnected resurrected dismissed AskResumePending")
	}

	// 6. UTF-8 multi-byte step title in StoppedState must remain valid UTF-8.
	st := editor.AgentStoppedState{Resumed: true, ResumedAtTitle: "放置史莱姆"}
	if title := st.EffectiveTitle(); !utf8.ValidString(title) || !strings.Contains(title, "放置史莱姆") {
		t.Fatalf("UTF-8 EffectiveTitle corrupted: %q", title)
	}

	// 7. Partial state: preconditions guard TryAnotherWay while offline; Play() cancels pointing;
	//    pointing pill reacts live to Spec Light/Dark switch; unkept review pauses autosave.
	if _, err := ed.PlaceObject("", sceneformat.ObjectDoc{ID: "slime_guard", X: 100, Y: 100}); err != nil {
		t.Fatal(err)
	}
	ed.MarkAgentTurnPartial("", editor.AgentPartialState{
		DoneCount:  1,
		TotalCount: 2,
		Target:     "slime_02",
		Reason:     "blocked",
	})
	ed.SetAgentOffline(true)
	if err := ed.TryAnotherWayAgentTurn(""); err == nil {
		t.Fatal("TryAnotherWayAgentTurn should fail while offline")
	}
	if ed.AgentTurns()[len(ed.AgentTurns())-1].Partial.TryingAnother {
		t.Fatal("failed TryAnotherWayAgentTurn mutated TryingAnother=true")
	}
	ed.SetAgentOffline(false)
	if !ed.HasOpenReview() {
		t.Fatal("HasOpenReview should be true while Partial turn has an unkept review")
	}

	// Pointing overlay updates colors live on Light -> Dark switch and is canceled by Play().
	ed.ToggleAgentPointing("")
	app.Settle()
	pill := app.Find("id:viewport-agent-pointing-pill")
	if pill == nil || len(pill.Children) == 0 {
		t.Fatal("viewport pointing pill missing")
	}
	if got := pill.Children[0].Style.Background; got != theme.Light().PillBg {
		t.Fatalf("light pointing pill bg = %#x, want %#x", uint32(got), uint32(theme.Light().PillBg))
	}
	app.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	})
	pill = app.Find("id:viewport-agent-pointing-pill")
	if got := pill.Children[0].Style.Background; got != theme.Dark().PillBg {
		t.Fatalf("dark pointing pill bg = %#x, want %#x", uint32(got), uint32(theme.Dark().PillBg))
	}
	if err := ed.Play(); err != nil {
		t.Fatalf("Play while pointing: %v", err)
	}
	if ed.AgentPointingActive() {
		t.Fatal("Play() did not cancel active viewport pointing")
	}
	if ed.DiscardAgentTurnReview("") {
		t.Fatal("DiscardAgentTurnReview should be refused during Play()")
	}
	if err := ed.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Keeping the review clears HasOpenReview().
	if !ed.KeepAgentTurnDiffReview("") {
		t.Fatal("KeepAgentTurnDiffReview should return true")
	}
	if ed.HasOpenReview() {
		t.Fatal("HasOpenReview should be false after keeping the agent turn review")
	}

	// 8. Transient add+remove leaving 0 objects (review == nil, mutations > 0) still catches summary mismatch!
	ed.RecordAgentToolCall(editor.AgentToolCallRecord{
		ClientName: "Claude Code",
		ToolName:   "scene_read",
		StepTitle:  "Read scene",
	})
	ed.PrepareExternalAgentBaseline()
	if _, err := ed.PlaceObject("", sceneformat.ObjectDoc{ID: "temp_slime_01", X: 50, Y: 50}); err != nil {
		t.Fatal(err)
	}
	ed.RecordAgentToolCall(editor.AgentToolCallRecord{
		ClientName: "Claude Code",
		ToolName:   "scene_edit",
		StepTitle:  "Added temp_slime_01",
	})
	if err := ed.DeleteObject("temp_slime_01"); err != nil {
		t.Fatal(err)
	}
	ed.RecordAgentToolCall(editor.AgentToolCallRecord{
		ClientName: "Claude Code",
		ToolName:   "scene_edit",
		StepTitle:  "Removed temp_slime_01",
	})
	mm := ed.RecordAgentTurnSummary("", "Added 1 new patrolling temp_slime.")
	if mm == nil {
		t.Fatal("expected mismatch report when added object was later removed leaving empty diff")
	}
	if !strings.Contains(mm.Reason, "temp_slime_01") {
		t.Fatalf("mismatch reason = %q, want temp_slime_01 explanation", mm.Reason)
	}
}
