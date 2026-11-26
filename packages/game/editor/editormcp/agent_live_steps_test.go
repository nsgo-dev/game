package editormcp_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
	"github.com/nsgo-dev/game/packages/game/playtest"
)

// editorLoop serializes all Editor calls onto a single goroutine, matching how a live UI runs
// editormcp.Server with Options.Run = cx.Post, so concurrent MCP calls are 100% race-free under -race.
type editorLoop struct {
	ch   chan func()
	stop chan struct{}
	done chan struct{}
}

func newEditorLoop(t *testing.T) *editorLoop {
	t.Helper()
	l := &editorLoop{
		ch:   make(chan func(), 64),
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	go func() {
		defer close(l.done)
		for {
			select {
			case fn := <-l.ch:
				fn()
			case <-l.stop:
				return
			}
		}
	}()
	t.Cleanup(func() {
		close(l.stop)
		<-l.done
	})
	return l
}

func (l *editorLoop) Post(fn func()) {
	select {
	case l.ch <- fn:
	case <-l.stop:
	}
}

func (l *editorLoop) Do(fn func()) {
	done := make(chan struct{})
	l.Post(func() {
		defer close(done)
		fn()
	})
	select {
	case <-done:
	case <-l.stop:
	}
}

func startTestMCPServer(t *testing.T, ed *editor.Editor, opts editormcp.Options) (*editorLoop, *editormcp.Server) {
	t.Helper()
	loop := newEditorLoop(t)
	opts.Run = loop.Post
	var s *editormcp.Server
	loop.Do(func() {
		s = editormcp.New(ed, opts)
	})
	t.Cleanup(func() {
		loop.Do(func() {
			_ = s.Close()
		})
	})
	return loop, s
}

func writePlaytestFixture(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, playtest.FileName), []byte(fixturePlaytestYAML), 0o644); err != nil {
		t.Fatalf("write playtest.yaml: %v", err)
	}
}

func fakePlaytestRunner(_ context.Context, req editormcp.PlaytestRunRequest) (*playtest.Report, error) {
	return &playtest.Report{
		Version:   playtest.ReportVersion,
		Scope:     req.Config.Scope,
		Bot:       req.Config.Bot,
		Rate:      60,
		Speed:     1,
		SeedCount: 1,
		Passed:    true,
		Metrics: []playtest.MetricReport{
			{
				Name:      "finished",
				Label:     "Finished",
				Aggregate: playtest.AggCount,
				Value:     4,
				Goal:      &playtest.Goal{Summary: "clear the room", Source: "brief.yaml", Min: playtest.Float64(3)},
				Status:    playtest.StatusMet,
			},
		},
		Runs: []playtest.RunReport{
			{Index: 1, Seed: 1, Outcome: playtest.OutcomeFinished, Steps: 60, DurationSeconds: 1},
		},
	}, nil
}

func waitForPendingAsk(t *testing.T, loop *editorLoop, ed *editor.Editor) *editor.AgentPermissionPrompt {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var ask *editor.AgentPermissionPrompt
		loop.Do(func() {
			ask = ed.AgentSession().PendingAsk
		})
		if ask != nil {
			return ask
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for permission card to appear")
	return nil
}

// Acceptance 1: A scripted MCP client drives a request: steps appear in order, an Ask shows a card,
// Allow once runs the tool, Deny skips it, shows "You denied …" with Allow now, and the agent's next
// identical call in the same request proceeds without retrying/showing a card; a new request may ask again.
func TestScriptedMCPClientLiveStepsAskAllowOnceAndDeny(t *testing.T) {
	f := openFixture(t)
	writePlaytestFixture(t, f.dir)
	if err := f.ed.SelectPermissionProfile(editor.PermissionProfileCautious); err != nil {
		t.Fatalf("SelectPermissionProfile: %v", err)
	}

	var playtestRuns atomic.Int32
	loop, s := startTestMCPServer(t, f.ed, editormcp.Options{
		RunPlaytest: func(ctx context.Context, req editormcp.PlaytestRunRequest) (*playtest.Report, error) {
			playtestRuns.Add(1)
			return fakePlaytestRunner(ctx, req)
		},
	})
	cs := connect(t, s, nil)

	loop.Do(func() {
		f.ed.StartAgentRequest("Put a slime on the floor and check it.")
	})

	// Step 1: Read (allowed even under Cautious).
	mustOK(t, call(t, cs, editormcp.ToolSceneRead, map[string]any{}))

	// Step 2: Agent reports a custom step with touched objects.
	repRes := call(t, cs, editormcp.ToolAgentReportStep, map[string]any{
		"text":    "Placed 3 coins toward the door",
		"objects": []string{"hero"},
	})
	mustOK(t, repRes)
	var repOut editormcp.AgentReportStepResult
	structured(t, repRes, &repOut)
	if repOut.Text != "Placed 3 coins toward the door" || !slices.Equal(repOut.Objects, []string{"hero"}) {
		t.Fatalf("agent_report_step structured = %+v", repOut)
	}

	// Step 3: game_check under Cautious shows Ask card -> Allow once runs it.
	checkDone := make(chan *mcp.CallToolResult, 1)
	go func() {
		checkDone <- call(t, cs, editormcp.ToolGameCheck, map[string]any{})
	}()
	ask := waitForPendingAsk(t, loop, f.ed)
	if ask.Tool != editormcp.ToolGameCheck || !strings.Contains(ask.Effect, "Nothing is saved") {
		t.Fatalf("unexpected game_check ask: %+v", ask)
	}
	var allowed bool
	loop.Do(func() { allowed = f.ed.AllowPermissionOnce() })
	if !allowed {
		t.Fatal("AllowPermissionOnce returned false")
	}
	mustOK(t, <-checkDone)

	// Step 4: game_playtest under Cautious shows Ask card -> Deny skips it.
	ptDone := make(chan *mcp.CallToolResult, 1)
	go func() {
		ptDone <- call(t, cs, editormcp.ToolGamePlaytest, map[string]any{})
	}()
	ask2 := waitForPendingAsk(t, loop, f.ed)
	if ask2.Title != "Allow the agent to run the game?" || !strings.Contains(ask2.RuleSource, "Cautious") {
		t.Fatalf("unexpected playtest ask: %+v", ask2)
	}
	var denied bool
	loop.Do(func() { denied = f.ed.DenyPermission() })
	if !denied {
		t.Fatal("DenyPermission returned false")
	}
	ptRes := <-ptDone
	if ptRes.IsError {
		t.Fatalf("denial should return a non-fatal structured response so the agent continues: %s", text(ptRes))
	}
	var denyInfo editormcp.PermissionDeniedResult
	structured(t, ptRes, &denyInfo)
	if !denyInfo.Denied || !denyInfo.ContinuedWithoutIt {
		t.Fatalf("deny structured = %+v", denyInfo)
	}
	if playtestRuns.Load() != 0 {
		t.Fatalf("denied playtest still ran %d times", playtestRuns.Load())
	}

	// Verify step rows appear in order and step 3 shows "You denied the playtest" with CanAllowNow=true.
	var snap editor.AgentSessionSnapshot
	loop.Do(func() { snap = f.ed.AgentSession() })
	if len(snap.Steps) != 4 {
		t.Fatalf("expected 4 steps in order, got %d: %+v", len(snap.Steps), snap.Steps)
	}
	if snap.Steps[0].ToolName != editormcp.ToolSceneRead || snap.Steps[0].Status != editor.AgentStepDone {
		t.Fatalf("step 0 = %+v", snap.Steps[0])
	}
	if snap.Steps[1].ToolName != editormcp.ToolAgentReportStep || snap.Steps[1].Title != "Placed 3 coins toward the door" {
		t.Fatalf("step 1 = %+v", snap.Steps[1])
	}
	if snap.Steps[2].ToolName != editormcp.ToolGameCheck || snap.Steps[2].Status != editor.AgentStepDone {
		t.Fatalf("step 2 = %+v", snap.Steps[2])
	}
	if snap.Steps[3].Status != editor.AgentStepDenied || !strings.Contains(snap.Steps[3].Title, "You denied the playtest") || !snap.Steps[3].CanAllowNow {
		t.Fatalf("step 3 = %+v, want denied with Allow now", snap.Steps[3])
	}

	// Calling game_playtest AGAIN in the same request MUST NOT show a card and MUST NOT run it!
	ptRetry := call(t, cs, editormcp.ToolGamePlaytest, map[string]any{})
	var retryInfo editormcp.PermissionDeniedResult
	structured(t, ptRetry, &retryInfo)
	if !retryInfo.Denied || !retryInfo.AlreadyDenied {
		t.Fatalf("retry after denial = %+v, want alreadyDenied=true", retryInfo)
	}
	loop.Do(func() {
		if f.ed.AgentSession().PendingAsk != nil {
			t.Error("retried denied tool opened a second permission card in the same request")
		}
	})

	// Agent's next call proceeds cleanly without being blocked by the earlier denial.
	mustOK(t, call(t, cs, editormcp.ToolSelectionGet, map[string]any{}))

	// Completing the request records "Not playtested" in the summary.
	var doneSnap editor.AgentSessionSnapshot
	loop.Do(func() {
		f.ed.CompleteAgentRequest("")
		doneSnap = f.ed.AgentSession()
	})
	if doneSnap.Review == nil || !slices.Contains(doneSnap.Review.NotChecked, "Not playtested") {
		t.Fatalf("summary review after denial = %+v, want Not playtested", doneSnap.Review)
	}

	// Clicking "Allow now" on the denied step runs the playtest on demand and clears "Not playtested".
	deniedStepID := snap.Steps[3].ID
	loop.Do(func() {
		if !f.ed.AllowDeniedStepNow(deniedStepID) {
			t.Errorf("AllowDeniedStepNow(%q) returned false", deniedStepID)
		}
	})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && playtestRuns.Load() < 1 {
		time.Sleep(5 * time.Millisecond)
	}
	if playtestRuns.Load() != 1 {
		t.Fatalf("Allow now playtest runs = %d, want 1", playtestRuns.Load())
	}

	// A NEW request clears per-request denials so the agent MAY ask again.
	loop.Do(func() {
		f.ed.StartAgentRequest("Now run the playtest.")
	})
	newReqDone := make(chan *mcp.CallToolResult, 1)
	go func() {
		newReqDone <- call(t, cs, editormcp.ToolGamePlaytest, map[string]any{})
	}()
	waitForPendingAsk(t, loop, f.ed)
	loop.Do(func() { f.ed.AllowPermissionOnce() })
	mustOK(t, <-newReqDone)
	if playtestRuns.Load() != 2 {
		t.Fatalf("expected playtest to run twice total, got %d", playtestRuns.Load())
	}
}

// Acceptance 2: Always adds a rule to permissions.yaml and the next identical call runs without a card.
func TestAlwaysRulePersistsAndSkipsNextCard(t *testing.T) {
	f := openFixture(t)
	writePlaytestFixture(t, f.dir)
	if err := f.ed.SelectPermissionProfile(editor.PermissionProfileCautious); err != nil {
		t.Fatalf("SelectPermissionProfile: %v", err)
	}

	var runs atomic.Int32
	loop, s := startTestMCPServer(t, f.ed, editormcp.Options{
		RunPlaytest: func(ctx context.Context, req editormcp.PlaytestRunRequest) (*playtest.Report, error) {
			runs.Add(1)
			return fakePlaytestRunner(ctx, req)
		},
	})
	cs := connect(t, s, nil)

	loop.Do(func() {
		f.ed.StartAgentRequest("Playtest level.")
	})
	firstDone := make(chan *mcp.CallToolResult, 1)
	go func() {
		firstDone <- call(t, cs, editormcp.ToolGamePlaytest, map[string]any{})
	}()
	waitForPendingAsk(t, loop, f.ed)
	var approved bool
	loop.Do(func() { approved = f.ed.AllowPermissionAlways() })
	if !approved {
		t.Fatal("AllowPermissionAlways returned false")
	}
	mustOK(t, <-firstDone)

	// Verify permissions.yaml on disk holds the new Allow rule for playtests.
	permPath := filepath.Join(f.dir, editor.PermissionsFileName)
	data, err := os.ReadFile(permPath)
	if err != nil {
		t.Fatalf("read permissions.yaml: %v", err)
	}
	loaded, err := editor.ParsePermissions(data)
	if err != nil {
		t.Fatalf("ParsePermissions: %v", err)
	}
	eval := loaded.Evaluate(editor.PermissionRequest{
		Tool:   editormcp.ToolGamePlaytest,
		Action: editor.PermissionActionRun,
		Target: editor.PermissionRunPlaytests,
	})
	if eval.Decision != editor.PermissionAllow {
		t.Fatalf("persisted permissions.yaml eval for playtests = %+v, want allow", eval)
	}

	// Next identical call runs immediately WITHOUT showing any permission card!
	mustOK(t, call(t, cs, editormcp.ToolGamePlaytest, map[string]any{}))
	loop.Do(func() {
		if f.ed.AgentSession().PendingAsk != nil {
			t.Error("second identical call unexpectedly showed a permission card")
		}
	})
	if runs.Load() != 2 {
		t.Fatalf("playtest runs = %d, want 2", runs.Load())
	}

	// Undo reverts the persisted rule so permissions.yaml asks again.
	var undoErr error
	loop.Do(func() {
		toast, ok := f.ed.PermissionToast()
		if !ok || toast.Message == "" {
			t.Errorf("expected PermissionToast after Always, got %+v (%v)", toast, ok)
		}
		undoErr = f.ed.UndoPermissionToast()
	})
	if undoErr != nil {
		t.Fatalf("UndoPermissionToast: %v", undoErr)
	}
	var afterEval editor.PermissionEvaluation
	loop.Do(func() {
		afterEval = f.ed.EvaluatePermission(editor.PermissionRequest{
			Tool:   editormcp.ToolGamePlaytest,
			Action: editor.PermissionActionRun,
			Target: editor.PermissionRunPlaytests,
		})
	})
	if afterEval.Decision != editor.PermissionAsk {
		t.Fatalf("after Undo, playtest eval = %+v, want ask", afterEval)
	}
}

func TestAddToRequestDeliversFollowUpsAndStopCancelsAsk(t *testing.T) {
	f := openFixture(t)
	writePlaytestFixture(t, f.dir)
	if err := f.ed.SelectPermissionProfile(editor.PermissionProfileCautious); err != nil {
		t.Fatalf("SelectPermissionProfile: %v", err)
	}
	loop, s := startTestMCPServer(t, f.ed, editormcp.Options{
		RunPlaytest: fakePlaytestRunner,
	})
	cs := connect(t, s, nil)

	loop.Do(func() {
		f.ed.StartAgentRequest("Inspect and edit.")
		if err := f.ed.AddToAgentRequest("Also make the coins gold."); err != nil {
			t.Errorf("AddToAgentRequest: %v", err)
		}
	})
	res := call(t, cs, editormcp.ToolAgentReportStep, map[string]any{"text": "Checked scene"})
	mustOK(t, res)
	var out editormcp.AgentReportStepResult
	structured(t, res, &out)
	if !slices.Equal(out.FollowUps, []string{"Also make the coins gold."}) {
		t.Fatalf("followUps = %v, want [Also make the coins gold.]", out.FollowUps)
	}

	// Trigger a waiting Ask card and interrupt it with StopAgent().
	ptDone := make(chan *mcp.CallToolResult, 1)
	go func() {
		ptDone <- call(t, cs, editormcp.ToolGamePlaytest, map[string]any{})
	}()
	waitForPendingAsk(t, loop, f.ed)
	var stopped bool
	loop.Do(func() { stopped = f.ed.StopAgent() })
	if !stopped {
		t.Fatal("StopAgent returned false")
	}
	stoppedRes := <-ptDone
	if !stoppedRes.IsError || !strings.Contains(text(stoppedRes), "stopped") {
		t.Fatalf("expected stopped error after StopAgent, got isError=%v text=%q", stoppedRes.IsError, text(stoppedRes))
	}
	var snap editor.AgentSessionSnapshot
	loop.Do(func() { snap = f.ed.AgentSession() })
	if !snap.Stopped || snap.Review == nil || !snap.Review.Stopped || snap.Review.StepsDone != 1 {
		t.Fatalf("session after Stop = %+v", snap)
	}
}

func TestLiveRequestStillEnforcesYouOnlyAndValidatesReportStep(t *testing.T) {
	f := openFixture(t)
	loop, s := startTestMCPServer(t, f.ed, editormcp.Options{})
	if err := s.AddTool(editormcp.Tool{
		Name:   "file_edit_test",
		Action: editormcp.ActionEdit,
		Handler: func(ctx context.Context, c *editormcp.Call) (*editormcp.Result, error) {
			return &editormcp.Result{Text: "should not run"}, nil
		},
	}); err != nil {
		t.Fatalf("AddTool: %v", err)
	}
	cs := connect(t, s, nil)

	loop.Do(func() {
		f.ed.StartAgentRequest("Attempt restricted edits.")
	})

	// You-only boundary (permissions.yaml) must STILL be refused even during an active live request.
	res := call(t, cs, "file_edit_test", map[string]any{"file": editor.PermissionsFileName})
	if !res.IsError || !strings.Contains(strings.ToLower(text(res)), "you only") {
		t.Fatalf("expected You-only refusal on permissions.yaml, got err=%v text=%q", res.IsError, text(res))
	}

	// agent_report_step rejects NUL bytes and oversized text.
	nulRes := call(t, cs, editormcp.ToolAgentReportStep, map[string]any{"text": "bad\x00step"})
	if !nulRes.IsError {
		t.Fatal("agent_report_step unexpectedly accepted NUL byte")
	}
	bigRes := call(t, cs, editormcp.ToolAgentReportStep, map[string]any{"text": strings.Repeat("a", 5000)})
	if !bigRes.IsError {
		t.Fatal("agent_report_step unexpectedly accepted oversized text")
	}
}
