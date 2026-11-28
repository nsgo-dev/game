package editor_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/keychain"
	"github.com/nsgo-dev/game/pkg/editor/theme"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type scheduledRetry struct {
	delay    time.Duration
	fn       func()
	canceled bool
}

type fakeRetryScheduler struct {
	items []*scheduledRetry
}

func (f *fakeRetryScheduler) schedule(d time.Duration, fn func()) func() {
	item := &scheduledRetry{delay: d, fn: fn}
	f.items = append(f.items, item)
	return func() { item.canceled = true }
}

func (f *fakeRetryScheduler) delays() []time.Duration {
	out := make([]time.Duration, len(f.items))
	for i, it := range f.items {
		out[i] = it.delay
	}
	return out
}

func (f *fakeRetryScheduler) fireLast() {
	if len(f.items) == 0 {
		return
	}
	it := f.items[len(f.items)-1]
	if !it.canceled && it.fn != nil {
		it.fn()
	}
}

func agentViewText(t *testing.T, app *nsgotest.App, sel string) string {
	t.Helper()
	v := app.Find(sel)
	if v == nil {
		t.Fatalf("selector %q not found", sel)
	}
	if v.Text != "" {
		return v.Text
	}
	var parts []string
	var walk func(n *nsgotest.View)
	walk = func(n *nsgotest.View) {
		if n.Text != "" {
			parts = append(parts, n.Text)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(v)
	return strings.Join(parts, " ")
}

func TestConnectDialogChoicesCopyWaitingPermissionsAndNoAgent(t *testing.T) {
	ed := newTestEditor(t)
	var copied string
	ed.WriteClipboard = func(text string) { copied = text }
	sched := &fakeRetryScheduler{}
	ed.AgentScheduleAfter = sched.schedule

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 820))

	// Open Connect dialog via command registry.
	if err := ed.ExecuteCommand(editor.CmdConnectAgent); err != nil {
		t.Fatalf("ExecuteCommand(CmdConnectAgent): %v", err)
	}
	app.Settle()

	if !app.Exists("id:connect-dialog-title") {
		t.Fatal("Connect dialog did not appear")
	}
	if !app.Exists("id:connect-choice-local") || !app.Exists("id:connect-choice-key") || !app.Exists("id:connect-choice-none") {
		t.Fatal("Connect dialog missing one of the three choices")
	}
	if cmd := agentViewText(t, app, "id:connect-install-command"); cmd != editor.AgentInstallCommand {
		t.Fatalf("install command = %q, want %q", cmd, editor.AgentInstallCommand)
	}
	if waitText := agentViewText(t, app, "id:connect-waiting-text"); !strings.Contains(waitText, "Waiting for an agent to connect") {
		t.Fatalf("waiting text = %q, want waiting message", waitText)
	}

	// Copy button copies `go tool game mcp install`.
	app.Press("id:connect-copy-command")
	app.Settle()
	if copied != editor.AgentInstallCommand {
		t.Fatalf("copied = %q, want %q", copied, editor.AgentInstallCommand)
	}

	// Built-in API key choice reveals the API key card.
	app.Press("id:connect-choice-key")
	app.Settle()
	if !app.Exists("id:connect-api-key-input") {
		t.Fatal("API key input did not appear for Built in choice")
	}

	// Switch back to local and attach an MCP client -> Waiting turns to Connected.
	app.Press("id:connect-choice-local")
	app.Settle()
	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{
		ID:        "c1",
		Name:      "claude-code",
		Transport: "stdio",
	})
	app.Settle()
	if got := agentViewText(t, app, "id:connect-waiting-text"); !strings.Contains(got, "Connected") || !strings.Contains(got, "Claude Code over stdio") {
		t.Fatalf("connected indicator = %q, want Connected · Claude Code over stdio", got)
	}

	// Toggle permission checkboxes and verify starting permission rules.
	app.Press("id:connect-perm-playtest")
	app.Press("id:connect-perm-go")
	app.Settle()

	// Later closes without committing draft permission toggles.
	app.Press("id:connect-later")
	app.Settle()
	if ed.ConnectDialogOpen() {
		t.Fatal("Later did not close Connect dialog")
	}
	if ed.AgentPermissions().RunHeadlessPlaytests || ed.AgentPermissions().EditGoCode {
		t.Fatalf("Later committed draft permissions: %+v", ed.AgentPermissions())
	}

	// Reopen, toggle playtest permission, and press Done -> commits rules and opens Agent panel.
	ed.OpenConnectDialog()
	app.Settle()
	app.Press("id:connect-perm-playtest")
	app.Press("id:connect-done")
	app.Settle()
	if !ed.AgentPermissions().RunHeadlessPlaytests {
		t.Fatal("Done did not commit RunHeadlessPlaytests permission")
	}
	rules := ed.AgentPermissionRules()
	foundPlaytestAllow := false
	for _, r := range rules {
		if r.Files == "playtests" && r.Decision == editor.AgentPermissionAllow {
			foundPlaytestAllow = true
		}
	}
	if !foundPlaytestAllow {
		t.Fatalf("starting permission rules missing playtests=Allow: %+v", rules)
	}
	if !ed.AgentPanelOpen() || !app.Exists("id:agent-panel") {
		t.Fatal("Done with local agent did not open Agent panel")
	}

	// Choosing "No agent" keeps the Agent panel closed.
	ed.OpenConnectDialog()
	app.Settle()
	app.Press("id:connect-choice-none")
	app.Press("id:connect-done")
	app.Settle()
	if ed.AgentMode() != editor.AgentModeNone {
		t.Fatalf("AgentMode = %q, want none", ed.AgentMode())
	}
	if ed.AgentPanelOpen() || app.Exists("id:agent-panel") {
		t.Fatal("No agent did not keep the Agent panel closed")
	}
	ed.SetRightRailTab(editor.RightTabAgent)
	if ed.AgentPanelOpen() {
		t.Fatal("Agent panel opened while AgentModeNone is active")
	}
}

func TestAgentReconnectScheduleAndTroubleshootFailedStep(t *testing.T) {
	ed := newTestEditor(t)
	// Give the editor a project directory without .mcp.json first to verify failed-step diagnosis,
	// then also test with .mcp.json present (ConnectStepWaiting).
	projDir := t.TempDir()
	ed.ProjectPath = filepath.Join(projDir, "game.yaml")
	if err := os.WriteFile(filepath.Join(projDir, ".mcp.json"), []byte(`{"mcpServers":{"nsgo-game-editor":{"command":"go","args":["tool","game","mcp"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	sched := &fakeRetryScheduler{}
	ed.AgentScheduleAfter = sched.schedule
	ed.AgentReconnectProbe = func(context.Context) (editor.AgentClientInfo, error) {
		return editor.AgentClientInfo{}, errors.New("still offline")
	}
	ed.SetRightRailTab(editor.RightTabAgent)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 820))

	// Connect MCP client -> Connected · MCP.
	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{
		ID:        "sess-1",
		Name:      "claude-code",
		Transport: "stdio",
	})
	app.Settle()
	if got := agentViewText(t, app, "id:agent-status-text"); got != "Connected · MCP" {
		t.Fatalf("status chip = %q, want Connected · MCP", got)
	}

	// Drop connection -> Reconnecting · try 1 of 5, then 2..5 with exact 1, 2, 4, 8, 16 s schedule.
	ed.NotifyAgentMCPDisconnected("sess-1", "Claude Code stopped answering over MCP.")
	app.Settle()

	for wantAttempt := 1; wantAttempt <= 5; wantAttempt++ {
		if ed.AgentStatus() != editor.AgentStatusReconnecting {
			t.Fatalf("attempt %d: AgentStatus = %q, want reconnecting", wantAttempt, ed.AgentStatus())
		}
		gotText := agentViewText(t, app, "id:agent-status-text")
		wantText := "Reconnecting · try " + string(rune('0'+wantAttempt)) + " of 5"
		if gotText != wantText {
			t.Fatalf("attempt %d: chip text = %q, want %q", wantAttempt, gotText, wantText)
		}
		// Manual tools keep working during Reconnecting.
		ed.SetTool(editor.ToolMove)
		if ed.Tool != editor.ToolMove {
			t.Fatal("manual Move tool failed during Reconnecting")
		}
		sched.fireLast()
		app.Settle()
	}

	wantDelays := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
	}
	if !slices.Equal(sched.delays(), wantDelays) {
		t.Fatalf("retry delays = %v, want %v", sched.delays(), wantDelays)
	}

	// After 5 failures -> Disconnected with Reconnect and Troubleshoot buttons.
	if ed.AgentStatus() != editor.AgentStatusDisconnected {
		t.Fatalf("after 5 failures AgentStatus = %q, want disconnected", ed.AgentStatus())
	}
	if got := agentViewText(t, app, "id:agent-status-text"); got != "Disconnected" {
		t.Fatalf("disconnected chip = %q, want Disconnected", got)
	}
	if !app.Exists("id:agent-status-reconnect") || !app.Exists("id:agent-status-troubleshoot") {
		t.Fatal("Disconnected header missing Reconnect or Troubleshoot button")
	}

	// Manual tools keep working while Disconnected.
	ed.SetTool(editor.ToolSelect)
	if ed.Tool != editor.ToolSelect {
		t.Fatal("manual Select tool failed during Disconnected")
	}

	// Clicking Troubleshoot reopens Connect at the failed step (ConnectStepWaiting), NOT at ConnectStepChoice.
	app.Press("id:agent-status-troubleshoot")
	app.Settle()
	if !ed.ConnectDialogOpen() {
		t.Fatal("Troubleshoot did not open Connect dialog")
	}
	if ed.ConnectDialogStep() != editor.ConnectStepWaiting {
		t.Fatalf("Troubleshoot step = %q, want %q", ed.ConnectDialogStep(), editor.ConnectStepWaiting)
	}
	if !ed.ConnectDialogTroubleshooting() || !app.Exists("id:connect-troubleshoot-banner") {
		t.Fatal("Connect dialog missing troubleshoot banner")
	}

	// If project MCP config files are missing, Troubleshoot lands at ConnectStepInstall.
	_ = os.Remove(filepath.Join(projDir, ".mcp.json"))
	ed.CloseConnectDialog()
	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{ID: "sess-2", Name: "claude-code", Transport: "stdio"})
	ed.NotifyAgentMCPDisconnected("sess-2", "")
	if ed.AgentFailedStep() != editor.ConnectStepInstall {
		t.Fatalf("failed step without .mcp.json = %q, want %q", ed.AgentFailedStep(), editor.ConnectStepInstall)
	}
}

func TestAgentStatusChipWaitingWorkingLiveRegionAndManualTools(t *testing.T) {
	ed := newTestEditor(t)
	now := time.Date(2026, 11, 26, 14, 0, 0, 0, time.UTC)
	ed.Now = func() time.Time { return now }
	ed.SetRightRailTab(editor.RightTabAgent)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 820))

	// Verify live region uses LiveRegionPolite.
	liveView := app.Find("id:agent-status-live")
	if liveView == nil {
		t.Fatal("agent-status-live not mounted")
	}
	if liveView.Props.LiveRegion != core.LiveRegionPolite {
		t.Fatalf("LiveRegion = %v, want LiveRegionPolite", liveView.Props.LiveRegion)
	}

	// Waiting for you · 0:32 -> clicking focuses the open request.
	ed.SetAgentWaitingForYou(editor.AgentOpenRequest{
		ID:       "perm-1",
		Title:    "Allow the agent to run the game?",
		Body:     "It wants to play level_01 headless for 10 seconds.",
		OpenedAt: now.Add(-32 * time.Second),
	})
	app.Settle()
	if got := agentViewText(t, app, "id:agent-status-text"); got != "Waiting for you · 0:32" {
		t.Fatalf("waiting chip = %q, want Waiting for you · 0:32", got)
	}
	if !strings.Contains(ed.AgentLiveAnnouncement(), "waiting for you") {
		t.Fatalf("live announcement = %q", ed.AgentLiveAnnouncement())
	}
	app.Press("id:agent-status-chip")
	app.Settle()
	if ed.AgentFocusRequestCount() != 1 {
		t.Fatalf("FocusRequestCount = %d, want 1", ed.AgentFocusRequestCount())
	}
	if !app.Exists("id:agent-open-request") {
		t.Fatal("open request card not shown")
	}

	// Manual tools (placing/moving/undoing an object) keep working in Waiting state.
	ed.SetTool(editor.ToolMove)
	if ed.Tool != editor.ToolMove {
		t.Fatal("manual tool blocked while Waiting")
	}
}

func TestAgentComposerSendsAndStreamsSteps(t *testing.T) {
	ed := newTestEditor(t)
	ed.SetRightRailTab(editor.RightTabAgent)
	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{
		ID:        "c1",
		Name:      "claude-code",
		Transport: "stdio",
	})

	stepGate := make(chan struct{})
	finishGate := make(chan struct{})
	gotReq := make(chan editor.AgentRequest, 1)

	ed.AgentRunner = editor.AgentRunnerFunc(func(ctx context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		gotReq <- req
		emit(editor.AgentEvent{Kind: editor.AgentEventSession, SessionID: "sess-abc"})
		emit(editor.AgentEvent{
			Kind:       editor.AgentEventStep,
			StepID:     "s1",
			StepTitle:  "Read level_01.scene.yaml",
			StepStatus: editor.AgentStepRunning,
			StepIndex:  1,
			StepTotal:  3,
		})
		<-stepGate
		emit(editor.AgentEvent{
			Kind:       editor.AgentEventStep,
			StepID:     "s1",
			StepStatus: editor.AgentStepDone,
			StepIndex:  1,
			StepTotal:  3,
		})
		emit(editor.AgentEvent{
			Kind:       editor.AgentEventStep,
			StepID:     "s2",
			StepTitle:  "Placed slime_01 on layer main",
			StepStatus: editor.AgentStepRunning,
			StepIndex:  2,
			StepTotal:  3,
		})
		<-finishGate
		emit(editor.AgentEvent{
			Kind:       editor.AgentEventStep,
			StepID:     "s2",
			StepStatus: editor.AgentStepDone,
			StepIndex:  2,
			StepTotal:  3,
		})
		emit(editor.AgentEvent{
			Kind:       editor.AgentEventStep,
			StepID:     "s3",
			StepTitle:  "Ran game check · no problems",
			StepStatus: editor.AgentStepDone,
			StepIndex:  3,
			StepTotal:  3,
		})
		emit(editor.AgentEvent{
			Kind: editor.AgentEventMessage,
			Text: "Done. Placed slime_01 and verified with game check.",
		})
		emit(editor.AgentEvent{Kind: editor.AgentEventDone})
		return nil
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 820))

	// Verify multiline newline insertion (Shift+Enter) and Enter submission.
	ed.SetAgentComposerText("Line one")
	ed.InsertAgentComposerNewline()
	ed.SetAgentComposerText(ed.AgentComposerText() + "Add slime_01")
	app.Settle()
	if ed.AgentComposerText() != "Line one\nAdd slime_01" {
		t.Fatalf("composer multiline draft = %q", ed.AgentComposerText())
	}

	app.Press("id:agent-composer-send")
	app.Settle()

	req := <-gotReq
	if req.Prompt != "Line one\nAdd slime_01" {
		t.Fatalf("received prompt = %q", req.Prompt)
	}
	if req.AgentKind != editor.ExternalAgentClaudeCode {
		t.Fatalf("AgentKind = %q, want claude", req.AgentKind)
	}

	app.WaitUntil(5*time.Second, func() bool {
		return ed.AgentStatus() == editor.AgentStatusWorking && app.Exists("id:agent-step-0-0")
	})
	if got := agentViewText(t, app, "id:agent-status-text"); got != "Working · step 1 of 3" {
		t.Fatalf("working chip = %q, want Working · step 1 of 3", got)
	}
	if !app.Exists("id:agent-composer-stop") {
		t.Fatal("Stop button not shown while working")
	}

	close(stepGate)
	app.WaitUntil(5*time.Second, func() bool {
		return app.Exists("id:agent-step-0-1")
	})
	if got := agentViewText(t, app, "id:agent-status-text"); got != "Working · step 2 of 3" {
		t.Fatalf("working chip step 2 = %q, want Working · step 2 of 3", got)
	}

	close(finishGate)
	app.WaitUntil(5*time.Second, func() bool {
		return ed.AgentStatus() == editor.AgentStatusConnected && app.Exists("id:agent-turn-summary-0")
	})
	if got := agentViewText(t, app, "id:agent-turn-summary-0"); !strings.Contains(got, "Placed slime_01") {
		t.Fatalf("turn summary = %q", got)
	}
	if ed.AgentSessionID() != "sess-abc" {
		t.Fatalf("AgentSessionID = %q, want sess-abc", ed.AgentSessionID())
	}
}

func TestParseClaudeAndCodexStreamLines(t *testing.T) {
	claudeInit := `{"type":"system","subtype":"init","session_id":"claude-sess-1"}`
	evs, ok := editor.ParseClaudeStreamLine([]byte(claudeInit))
	if !ok || len(evs) != 1 || evs[0].Kind != editor.AgentEventSession || evs[0].SessionID != "claude-sess-1" {
		t.Fatalf("claude init = %+v (ok=%v)", evs, ok)
	}
	claudeTool := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu_1","name":"mcp__nsgo-game-editor__scene_edit"}]}}`
	evs, ok = editor.ParseClaudeStreamLine([]byte(claudeTool))
	if !ok || len(evs) != 1 || evs[0].Kind != editor.AgentEventStep || evs[0].StepID != "tu_1" || evs[0].StepTitle != "Edit scene" {
		t.Fatalf("claude tool = %+v (ok=%v)", evs, ok)
	}
	claudeResult := `{"type":"result","result":"All set","usage":{"input_tokens":120,"output_tokens":45}}`
	evs, ok = editor.ParseClaudeStreamLine([]byte(claudeResult))
	if !ok || len(evs) != 2 || evs[0].Kind != editor.AgentEventUsage || evs[1].Kind != editor.AgentEventDone {
		t.Fatalf("claude result = %+v (ok=%v)", evs, ok)
	}

	codexStart := `{"type":"thread.started","thread_id":"codex-thr-9"}`
	evs, ok = editor.ParseCodexStreamLine([]byte(codexStart))
	if !ok || len(evs) != 1 || evs[0].SessionID != "codex-thr-9" {
		t.Fatalf("codex start = %+v (ok=%v)", evs, ok)
	}
	codexTool := `{"type":"item.started","item":{"id":"i1","type":"mcp_tool_call","tool":"game_check"}}`
	evs, ok = editor.ParseCodexStreamLine([]byte(codexTool))
	if !ok || len(evs) != 1 || evs[0].StepTitle != "Run game check" || evs[0].StepStatus != editor.AgentStepRunning {
		t.Fatalf("codex tool = %+v (ok=%v)", evs, ok)
	}
	codexDone := `{"type":"turn.completed","usage":{"input_tokens":200,"output_tokens":80}}`
	evs, ok = editor.ParseCodexStreamLine([]byte(codexDone))
	if !ok || len(evs) != 2 || evs[0].InputTokens != 200 || evs[1].Kind != editor.AgentEventDone {
		t.Fatalf("codex done = %+v (ok=%v)", evs, ok)
	}
}

// TestAgentConnectAndStatesScreenshots captures real macOS screenshots of the Connect dialog
// and the Agent panel across all status chip states for side-by-side inspection against
// design/editor/Connect.dc.html and design/editor/AgentStates.dc.html.
func TestAgentConnectAndStatesScreenshots(t *testing.T) {
	ed := newTestEditor(t)
	now := time.Date(2026, 11, 26, 14, 0, 0, 0, time.UTC)
	ed.Now = func() time.Time { return now }
	ed.SetAppearance(editor.AppearanceDark)
	sched := &fakeRetryScheduler{}
	ed.AgentScheduleAfter = sched.schedule

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	// 1. Connect dialog (local choice, Waiting state).
	ed.OpenConnectDialog()
	app.Settle()
	app.Screenshot("agent/connect-waiting")

	// 2. Connect dialog after first MCP client connects -> green Connected indicator.
	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{
		ID:        "c1",
		Name:      "claude-code",
		Transport: "stdio",
	})
	app.Settle()
	app.Screenshot("agent/connect-connected")

	// 3. Agent panel (Connected · MCP + streamed thread & composer).
	ed.CompleteConnectDialog()
	ed.AgentRunner = editor.AgentRunnerFunc(func(_ context.Context, _ editor.AgentRequest, emit func(editor.AgentEvent)) error {
		emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "1", StepTitle: "Read level_01.scene.yaml", StepStatus: editor.AgentStepDone, StepIndex: 1, StepTotal: 3})
		emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "2", StepTitle: "Placed slime_01 on layer main", StepStatus: editor.AgentStepDone, StepIndex: 2, StepTotal: 3})
		emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "3", StepTitle: "Ran game check · no problems", StepStatus: editor.AgentStepDone, StepIndex: 3, StepTotal: 3})
		emit(editor.AgentEvent{Kind: editor.AgentEventMessage, Text: "Done. Placed slime_01 and verified with game check."})
		emit(editor.AgentEvent{Kind: editor.AgentEventDone})
		return nil
	})
	if err := ed.SubmitAgentPrompt("Place slime_01 patrolling the lower floor."); err != nil {
		t.Fatal(err)
	}
	app.WaitUntil(5*time.Second, func() bool {
		return ed.AgentStatus() == editor.AgentStatusConnected && app.Exists("id:agent-turn-summary-0")
	})
	app.Screenshot("agent/panel-connected")

	// 4. Waiting for you · 0:32 state.
	ed.SetAgentWaitingForYou(editor.AgentOpenRequest{
		ID:       "req-playtest",
		Title:    "Allow the agent to run the game?",
		Body:     "It wants to play level_01 headless for 10 seconds to check the slimes stay on the floor. Nothing is saved.",
		OpenedAt: now.Add(-32 * time.Second),
	})
	app.Settle()
	app.Screenshot("agent/panel-waiting")

	// 5. Disconnected with Reconnect and Troubleshoot buttons.
	ed.ClearAgentWaitingForYou()
	ed.AgentReconnectProbe = func(context.Context) (editor.AgentClientInfo, error) {
		return editor.AgentClientInfo{}, errors.New("offline")
	}
	ed.NotifyAgentMCPDisconnected("c1", "Claude Code stopped answering over MCP.")
	for range 5 {
		sched.fireLast()
	}
	app.Settle()
	app.Screenshot("agent/panel-disconnected")
}

func TestAgentReconnectAsyncNonBlockingWithStalledProbe(t *testing.T) {
	ed := newTestEditor(t)
	probeStarted := make(chan struct{})
	probeCancelled := make(chan struct{})
	ed.AgentReconnectProbe = func(ctx context.Context) (editor.AgentClientInfo, error) {
		close(probeStarted)
		<-ctx.Done()
		close(probeCancelled)
		return editor.AgentClientInfo{}, ctx.Err()
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1200, 760))
	_ = app

	ed.ReconnectAgent()
	select {
	case <-probeStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("async reconnect probe did not start")
	}
	if ed.AgentStatus() != editor.AgentStatusReconnecting {
		t.Fatalf("AgentStatus = %q, want reconnecting", ed.AgentStatus())
	}

	// Cancelling reconnect (or closing editor) cancels the in-flight probe context.
	ed.CancelAgentReconnect()
	select {
	case <-probeCancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("CancelAgentReconnect did not cancel in-flight probe context")
	}
}

func TestAgentLifecycleResetAPIKeyClearingAndPlayGuard(t *testing.T) {
	ed := newTestEditor(t)
	ed.Keychain = keychain.NewMemory()
	ed.AgentScheduleAfter = func(_ time.Duration, _ func()) func() { return func() {} }
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1200, 760))
	_ = app

	// Closing or completing the connect dialog must zero the transient API key buffer.
	ed.OpenConnectDialog()
	ed.SetConnectChoice(editor.AgentModeAPIKey)
	ed.SetConnectAPIKey("sk-test-secret-1")
	ed.CloseConnectDialog()
	if got := ed.ConnectDialogAPIKey(); got != "" {
		t.Fatalf("after CloseConnectDialog API key = %q, want empty", got)
	}

	ed.OpenConnectDialog()
	ed.SetConnectChoice(editor.AgentModeAPIKey)
	ed.SetConnectAPIKey("sk-test-secret-2")
	ed.CompleteConnectDialog()
	if got := ed.ConnectDialogAPIKey(); got != "" {
		t.Fatalf("after CompleteConnectDialog API key = %q, want empty", got)
	}

	// Submitting a prompt during Play mode must be refused with ErrPlaying.
	ed.SetConnectChoice(editor.AgentModeLocal)
	ed.CompleteConnectDialog()
	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{ID: "c1", Name: "claude-code", Transport: "stdio"})
	ed.RecordExternalAgentToolCall("Claude Code", "scene_edit", "Edit scene", false)
	if err := ed.Play(); err != nil {
		t.Fatalf("Play: %v", err)
	}
	if err := ed.SubmitAgentPrompt("move player"); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("SubmitAgentPrompt during Play = %v, want ErrPlaying", err)
	}
	if err := ed.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Switching projects (OpenDemo) resets connected clients, turns, and status cleanly.
	if err := ed.OpenDemo(); err != nil {
		t.Fatalf("OpenDemo: %v", err)
	}
	if ed.AgentStatus() != editor.AgentStatusNotConnected {
		t.Fatalf("after OpenDemo AgentStatus = %q, want not_connected", ed.AgentStatus())
	}
	if clients := ed.AgentClients(); len(clients) != 0 {
		t.Fatalf("after OpenDemo AgentClients = %+v, want empty", clients)
	}
	if turns := ed.AgentTurns(); len(turns) != 0 {
		t.Fatalf("after OpenDemo AgentTurns len = %d, want 0", len(turns))
	}
	ed.Close()
}

func TestAgentSpecLightAppearanceSwitchesColors(t *testing.T) {
	ed := newTestEditor(t)
	ed.SetAgentPanelOpen(true)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   false,
		Locale: "en-US",
	}))

	light := theme.Light()
	dark := theme.Dark()

	if got := app.Find("id:agent-panel").Style.Background; got != light.Panel {
		t.Fatalf("light mode agent-panel bg = %#x, want %#x", uint32(got), uint32(light.Panel))
	}
	ed.OpenConnectDialog()
	app.Settle()
	if got := app.Find("id:connect-dialog-body").Style.Background; got != light.Panel {
		t.Fatalf("light mode connect-dialog-body bg = %#x, want %#x", uint32(got), uint32(light.Panel))
	}
	ed.CloseConnectDialog()
	ed.OpenSettingsPage(editor.SettingsPageModels)
	app.Settle()
	if got := app.Find("id:settings-models-local-card").Style.Background; got != light.Raised {
		t.Fatalf("light mode settings-models-local-card bg = %#x, want %#x", uint32(got), uint32(light.Raised))
	}

	app.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	})
	if got := app.Find("id:settings-models-local-card").Style.Background; got != dark.Raised {
		t.Fatalf("dark mode settings-models-local-card bg = %#x, want %#x", uint32(got), uint32(dark.Raised))
	}
	ed.CloseSettings()
	ed.OpenConnectDialog()
	app.Settle()
	if got := app.Find("id:agent-panel").Style.Background; got != dark.Panel {
		t.Fatalf("dark mode agent-panel bg = %#x, want %#x", uint32(got), uint32(dark.Panel))
	}
	if got := app.Find("id:connect-dialog-body").Style.Background; got != dark.Panel {
		t.Fatalf("dark mode connect-dialog-body bg = %#x, want %#x", uint32(got), uint32(dark.Panel))
	}
}
