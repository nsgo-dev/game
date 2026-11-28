package editor_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
)

// A running automation holds WithAgentActor for its whole run on a background goroutine. The person's own task decisions must still go through meanwhile: answering the automation's Ask step, keeping a separate parallel task, and dismissing a Ready to review task.
func TestTaskDecisionsStayThePersonsWhileAutomationRuns(t *testing.T) {
	dir := writeAutomationsProjectFixture(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC)
	ed.Now = func() time.Time { return now }
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ed.Close() })

	// Two parallel tasks whose runner holds until canceled, finished by hand so they wait in Ready to review.
	ed.AgentTaskRunner = holdTaskUntilCanceled
	ed.RunHeadless(func() {
		for _, id := range []string{"p1", "p2"} {
			if _, err := ed.SubmitTask(editor.TaskRequest{ID: id, Title: "Parallel " + id, StartMode: editor.TaskStartParallel}); err != nil {
				t.Fatalf("SubmitTask %s: %v", id, err)
			}
			if err := ed.CompleteTaskTurn(id, editor.TaskReview{ID: "review-" + id, TaskID: id, Summary: "done"}); err != nil {
				t.Fatalf("CompleteTaskTurn %s: %v", id, err)
			}
		}
	})

	permAnswered := make(chan struct{})
	ed.OnTaskPermissionDecision = func(string, bool) {
		select {
		case <-permAnswered:
		default:
			close(permAnswered)
		}
	}
	release := make(chan struct{})
	ed.AgentRunner = editor.AgentRunnerFunc(func(ctx context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "s1", StepTitle: "Allow playtest?", StepDetail: "Needs to run level_01 headless", StepStatus: editor.AgentStepWaiting})
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		emit(editor.AgentEvent{Kind: editor.AgentEventDone})
		return nil
	})
	defer close(release)

	ed.RunHeadless(func() {
		if err := ed.SetSceneName("Level 01 Updated"); err != nil {
			t.Fatal(err)
		}
		if err := ed.Save(); err != nil {
			t.Fatal(err)
		}
	})

	var autoID string
	deadline := time.Now().Add(5 * time.Second)
	for autoID == "" {
		if time.Now().After(deadline) {
			t.Fatal("automation task never reached Needs you")
		}
		ed.RunHeadless(func() {
			for _, task := range ed.Tasks() {
				if task.Source == editor.TaskSourceAutomation && task.State == editor.TaskNeedsYou {
					autoID = task.ID
				}
			}
		})
		if autoID == "" {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if !ed.AgentActorActive() {
		t.Fatal("precondition: the automation's runner should hold WithAgentActor while it waits")
	}

	var resolveErr, keepErr error
	var dismissed int
	ed.RunHeadless(func() {
		resolveErr = ed.ResolveTaskPermission(autoID, true)
		keepErr = ed.KeepTask("p1")
		dismissed = ed.DismissTasks([]string{"p2"})
	})
	if resolveErr != nil {
		t.Fatalf("person ResolveTaskPermission while an automation runs = %v, want nil", resolveErr)
	}
	select {
	case <-permAnswered:
	case <-time.After(5 * time.Second):
		t.Fatal("the person's answer never reached the automation")
	}
	if keepErr != nil {
		t.Fatalf("person KeepTask of a parallel task while an automation runs = %v, want nil", keepErr)
	}
	if dismissed != 1 {
		t.Fatalf("person DismissTasks of a Ready to review task while an automation runs dismissed %d, want 1", dismissed)
	}
	ed.RunHeadless(func() {
		if p1, _ := ed.Task("p1"); p1.State != editor.TaskDone {
			t.Errorf("p1 after the person's keep = %s, want done", p1.State)
		}
	})

	// An agent's MCP tool call on the editor goroutine is still refused the same decisions.
	ed.RunHeadless(func() {
		if _, err := ed.SubmitTask(editor.TaskRequest{ID: "p3", Title: "Parallel p3", StartMode: editor.TaskStartParallel}); err != nil {
			t.Fatalf("SubmitTask p3: %v", err)
		}
		if err := ed.CompleteTaskTurn("p3", editor.TaskReview{ID: "review-p3", TaskID: "p3", Summary: "done"}); err != nil {
			t.Fatalf("CompleteTaskTurn p3: %v", err)
		}
		if err := ed.RunAsAgent(func() error { return ed.KeepTask("p3") }); !errors.Is(err, editor.ErrTaskDecisionYouOnly) {
			t.Errorf("agent KeepTask = %v, want ErrTaskDecisionYouOnly", err)
		}
		n := 0
		_ = ed.RunAsAgent(func() error {
			n = ed.DismissTasks([]string{"p3"})
			return nil
		})
		if n != 0 {
			t.Errorf("agent DismissTasks of a Ready to review task dismissed %d, want 0", n)
		}
	})
}

// Stopping a Ready to review task would end its review without reverting its edits, a keep under another name, so StopTask refuses it for the person and an agent alike and the review stays open.
func TestStopTaskRefusesReadyToReview(t *testing.T) {
	dir := writeAutomationsProjectFixture(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	ed.AgentTaskRunner = holdTaskUntilCanceled

	ed.RunHeadless(func() {
		if _, err := ed.SubmitTask(editor.TaskRequest{ID: "t1", Title: "Task", StartMode: editor.TaskStartParallel}); err != nil {
			t.Fatal(err)
		}
		if err := ed.CompleteTaskTurn("t1", editor.TaskReview{ID: "review-t1", TaskID: "t1", Summary: "done"}); err != nil {
			t.Fatal(err)
		}
		if err := ed.WithAgentActor(func() error {
			return ed.RunAsAgent(func() error { return ed.StopTask("t1") })
		}); !errors.Is(err, editor.ErrTaskDecisionYouOnly) {
			t.Fatalf("agent StopTask of a Ready to review task = %v, want ErrTaskDecisionYouOnly", err)
		}
		if err := ed.StopTask("t1"); err == nil {
			t.Fatal("person StopTask of a Ready to review task succeeded, want refusal")
		}
		if task, _ := ed.Task("t1"); task.State != editor.TaskReadyToReview || !ed.HasOpenTaskReview() {
			t.Fatalf("t1 after refused stops = %s, open review %v; want ready_to_review with an open review", task.State, ed.HasOpenTaskReview())
		}
		if err := ed.KeepTask("t1"); err != nil {
			t.Fatalf("person KeepTask after refused stops: %v", err)
		}
	})
}
