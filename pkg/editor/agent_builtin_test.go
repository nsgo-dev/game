package editor_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// #1030 Acceptance 2: Reaching the weekly limit queues the next request and shows "Limit reached · resets 00:00";
// any attempt by the agent to raise the limit (or mutate You-only settings / read the API key) is refused with ErrAgentSettingsYouOnly.
func TestAgentWeeklyLimitQueuesAndRefusesAgentRaise(t *testing.T) {
	var apiHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "msg_limit_test",
			"model":       editor.DefaultBuiltInModel,
			"stop_reason": "end_turn",
			"content": []map[string]any{
				{"type": "text", "text": "Done."},
			},
			"usage": map[string]int{"input_tokens": 20, "output_tokens": 5},
		})
	}))
	defer srv.Close()

	settingsPath := filepath.Join(t.TempDir(), "editor.yaml")
	now := time.Date(2026, 11, 26, 14, 10, 0, 0, time.UTC)

	ed := newTestEditor(t)
	ed.Now = func() time.Time { return now }
	ed.SettingsPath = settingsPath
	ed.BuiltInAgentEndpoint = srv.URL
	ed.BuiltInHTTPClient = srv.Client()

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Secrets(map[string]string{
		editor.AgentAPIKeySecretAccount: "sk-ant-limit-test-secret",
	}))

	// Configure Built-in API key mode and open Usage and privacy in Settings.
	ed.OpenConnectDialogAt(editor.ConnectStepAPIKey)
	app.Settle()
	app.Press("id:connect-done")
	app.Settle()

	ed.OpenSettingsPage(editor.SettingsPageUsage)
	app.Settle()
	if !app.Exists("id:settings-usage-page") {
		t.Fatal("Usage and privacy page did not render")
	}

	// Person sets weekly limit to 2 requests via UI.
	app.Clear("id:settings-usage-limit-input")
	app.Type("id:settings-usage-limit-input", "2")
	app.Press("id:settings-usage-limit-apply")
	app.Settle()
	if ed.WeeklyRequestLimit() != 2 {
		t.Fatalf("weekly limit = %d, want 2", ed.WeeklyRequestLimit())
	}
	ed.CloseSettings()
	app.Settle()

	// Run 2 requests -> both hit the endpoint and succeed.
	for i := range 2 {
		if err := ed.SubmitAgentPrompt("Place coin"); err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
		app.WaitUntil(5*time.Second, func() bool { return !ed.AgentWorking() })
		app.Settle()
	}
	if apiHits.Load() != 2 {
		t.Fatalf("apiHits = %d, want 2", apiHits.Load())
	}
	if ed.AgentStatus() != editor.AgentStatusLimitReached {
		t.Fatalf("status after reaching limit = %q, want %q", ed.AgentStatus(), editor.AgentStatusLimitReached)
	}
	if got := agentViewText(t, app, "id:agent-status-text"); got != "Limit reached · resets 00:00" {
		t.Fatalf("status chip = %q, want Limit reached · resets 00:00", got)
	}
	if !app.Exists("id:agent-status-usage") {
		t.Fatal("missing Usage link beside Limit reached chip")
	}

	// Submitting a 3rd request queues it without hitting the model endpoint.
	if err := ed.SubmitAgentPrompt("Make the coins spin slowly"); err != nil {
		t.Fatalf("queued submit returned error: %v", err)
	}
	app.Settle()
	if apiHits.Load() != 2 {
		t.Fatalf("queued request should not have called endpoint: apiHits=%d", apiHits.Load())
	}
	queued := ed.AgentQueuedRequests()
	if len(queued) != 1 || queued[0].Prompt != "Make the coins spin slowly" {
		t.Fatalf("queued = %+v, want 1 prompt", queued)
	}
	if !app.Exists("id:agent-queued-section") || !strings.Contains(agentViewText(t, app, "id:agent-queued-heading"), "QUEUED · 1") {
		t.Fatal("Agent panel missing QUEUED · 1 section")
	}

	// Sending a queued request while still at limit must fail with ErrAgentLimitReached.
	if err := ed.SendQueuedAgentRequest(queued[0].ID); !errors.Is(err, editor.ErrAgentLimitReached) {
		t.Fatalf("SendQueuedAgentRequest at limit = %v, want ErrAgentLimitReached", err)
	}

	// Any attempt by an agent to raise the weekly limit, alter You-only settings, or read the API key MUST be refused.
	if err := ed.WithAgentActor(func() error {
		return ed.SetWeeklyRequestLimit(999)
	}); !errors.Is(err, editor.ErrAgentSettingsYouOnly) {
		t.Fatalf("agent SetWeeklyRequestLimit = %v, want ErrAgentSettingsYouOnly", err)
	}
	if err := ed.WithAgentActor(func() error {
		s := ed.Settings()
		higher := 999
		if s.Agent == nil {
			s.Agent = &editor.AgentSettings{}
		}
		s.Agent.WeeklyRequestLimit = &higher
		return ed.ApplySettings(s)
	}); !errors.Is(err, editor.ErrAgentSettingsYouOnly) {
		t.Fatalf("agent ApplySettings = %v, want ErrAgentSettingsYouOnly", err)
	}
	if err := ed.WithAgentActor(func() error {
		return ed.SetAgentTaskRoute(string(editor.AgentTaskPlaytest), "Claude Code · default")
	}); !errors.Is(err, editor.ErrAgentSettingsYouOnly) {
		t.Fatalf("agent SetAgentTaskRoute = %v, want ErrAgentSettingsYouOnly", err)
	}
	if err := ed.WithAgentActor(func() error {
		return ed.SetAgentPrivacy(editor.PrivacyGoSource, true)
	}); !errors.Is(err, editor.ErrAgentSettingsYouOnly) {
		t.Fatalf("agent SetAgentPrivacy = %v, want ErrAgentSettingsYouOnly", err)
	}
	if err := ed.WithAgentActor(func() error {
		return ed.StoreAgentAPIKey(editor.DefaultAgentAPIKeyAccount, "sk-agent-injected")
	}); !errors.Is(err, editor.ErrAgentSettingsYouOnly) {
		t.Fatalf("agent StoreAgentAPIKey = %v, want ErrAgentSettingsYouOnly", err)
	}
	if err := ed.WithAgentActor(func() error {
		_, err := ed.AgentAPIKey(editor.DefaultAgentAPIKeyAccount)
		return err
	}); !errors.Is(err, editor.ErrAgentSettingsYouOnly) {
		t.Fatalf("agent AgentAPIKey = %v, want ErrAgentSettingsYouOnly", err)
	}
	if ed.WeeklyRequestLimit() != 2 {
		t.Fatalf("agent mutated weekly limit to %d, want 2", ed.WeeklyRequestLimit())
	}

	// Clicking the Usage link in the header opens Usage and privacy so the person can raise the limit.
	app.Press("id:agent-status-usage")
	app.Settle()
	if !ed.SettingsOpen() || ed.SettingsPage() != editor.SettingsPageUsage {
		t.Fatalf("Usage link opened page=%q open=%v, want usage/true", ed.SettingsPage(), ed.SettingsOpen())
	}
	app.Clear("id:settings-usage-limit-input")
	app.Type("id:settings-usage-limit-input", "10")
	app.Press("id:settings-usage-limit-apply")
	app.Settle()
	ed.CloseSettings()
	app.Settle()

	if ed.WeeklyRequestLimit() != 10 || ed.AgentStatus() != editor.AgentStatusConnected {
		t.Fatalf("after person raises limit: limit=%d status=%q", ed.WeeklyRequestLimit(), ed.AgentStatus())
	}

	// Person sends the queued request from the queue list.
	app.Press("id:agent-queued-send-0")
	app.WaitUntil(5*time.Second, func() bool { return !ed.AgentWorking() && apiHits.Load() == 3 })
	app.Settle()
	if len(ed.AgentQueuedRequests()) != 0 {
		t.Fatalf("queue not drained after sending: %+v", ed.AgentQueuedRequests())
	}

	// Verify automatic reset when clock crosses weekly reset boundary (Monday 00:00).
	if err := ed.SetWeeklyRequestLimit(3); err != nil {
		t.Fatal(err)
	}
	if !ed.AgentWeeklyLimitReached() {
		t.Fatal("expected limit reached at 3/3")
	}
	now = ed.AgentUsageResetAt().Add(time.Minute)
	ed.CheckAgentUsageReset()
	if ed.AgentWeeklyLimitReached() || ed.AgentUsage().WeeklyRequests != 0 {
		t.Fatalf("usage did not reset after ResetAt: %+v", ed.AgentUsage())
	}

	// Verify secret key never leaked into editor.yaml.
	rawSettings, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read persisted editor.yaml: %v", err)
	}
	if strings.Contains(string(rawSettings), "sk-ant-limit-test-secret") {
		t.Fatal("API key leaked into editor.yaml")
	}
}

// #1030 Acceptance 3: A fake endpoint returning 529 shows the model error card with its retry count
// ("Anthropic API is overloaded (529)", "Retrying in 4 s · try 1 of 4", then "Retrying in 8 s · try 2 of 4"),
// supports Retry now, and switching the work to another connected agent updates WHO DOES WHAT and runs the request there.
func TestBuiltInAgent529RetryBackoffAndSwitchToConnectedAgent(t *testing.T) {
	var apiHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(529)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type": "error",
			"error": map[string]string{
				"type":    "overloaded_error",
				"message": "Overloaded",
			},
		})
	}))
	defer srv.Close()

	sched := &fakeRetryScheduler{}
	var claudeMu sync.Mutex
	var claudeReqs []editor.AgentRequest

	ed := newTestEditor(t)
	ed.AgentScheduleAfter = sched.schedule
	ed.BuiltInAgentEndpoint = srv.URL
	ed.BuiltInHTTPClient = srv.Client()
	ed.AgentRunner = editor.AgentRunnerFunc(func(_ context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		claudeMu.Lock()
		claudeReqs = append(claudeReqs, req)
		claudeMu.Unlock()
		emit(editor.AgentEvent{
			Kind:       editor.AgentEventStep,
			StepID:     "claude-step-1",
			StepTitle:  "Analyzed playtest replay via Claude Code",
			StepStatus: editor.AgentStepDone,
		})
		emit(editor.AgentEvent{Kind: editor.AgentEventMessage, Text: "Claude Code finished the playtest report."})
		emit(editor.AgentEvent{Kind: editor.AgentEventDone})
		return nil
	})
	ed.SetRightRailTab(editor.RightTabAgent)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Secrets(map[string]string{
		editor.AgentAPIKeySecretAccount: "sk-ant-529-test-secret",
	}))

	// Connect Claude Code as an external MCP client alongside the built-in Anthropic API.
	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{
		ID:        "claude-1",
		Name:      "claude-code",
		Transport: "stdio",
	})
	app.Settle()

	// Playtest analysis defaults to Anthropic API · fast in WHO DOES WHAT.
	if got := ed.ResolveAgentModelRoute(editor.AgentTaskPlaytest); got != editor.AgentRouteBuiltInFast {
		t.Fatalf("default playtest route = %q, want %q", got, editor.AgentRouteBuiltInFast)
	}

	if err := ed.SubmitAgentTaskPrompt(editor.AgentTaskPlaytest, "Analyze floor 3 deaths"); err != nil {
		t.Fatalf("SubmitAgentTaskPrompt: %v", err)
	}

	// Try 1 fails with 529 -> card shows "Anthropic API is overloaded (529)" and "Retrying in 4 s · try 1 of 4".
	app.WaitUntil(5*time.Second, func() bool {
		turns := ed.AgentTurns()
		return len(turns) == 1 && turns[0].ModelError != nil && turns[0].ModelError.RetryAttempt == 1
	})
	app.Settle()

	if title := agentViewText(t, app, "id:agent-turn-model-error-title-0"); title != "Anthropic API is overloaded (529)" {
		t.Fatalf("model error title = %q, want Anthropic API is overloaded (529)", title)
	}
	if body := agentViewText(t, app, "id:agent-turn-model-error-body-0"); !strings.Contains(body, "Retrying in 4 s · try 1 of 4") || !strings.Contains(body, "Claude Code") {
		t.Fatalf("model error body (try 1) = %q", body)
	}
	if !app.Exists("id:agent-turn-retry-0") || !app.Exists("id:agent-turn-switch-0") {
		t.Fatal("model error card missing Retry now or Use Claude Code button")
	}

	// Fire backoff timer -> try 2 fails with 529 -> "Retrying in 8 s · try 2 of 4".
	sched.fireLast()
	app.WaitUntil(5*time.Second, func() bool {
		turns := ed.AgentTurns()
		return len(turns) == 1 && turns[0].ModelError != nil && turns[0].ModelError.RetryAttempt == 2
	})
	app.Settle()
	if body := agentViewText(t, app, "id:agent-turn-model-error-body-0"); !strings.Contains(body, "Retrying in 8 s · try 2 of 4") {
		t.Fatalf("model error body (try 2) = %q, want Retrying in 8 s · try 2 of 4", body)
	}

	// Clicking "Retry now" immediately performs try 3 -> "Retrying in 16 s · try 3 of 4".
	app.Press("id:agent-turn-retry-0")
	app.WaitUntil(5*time.Second, func() bool {
		turns := ed.AgentTurns()
		return len(turns) == 1 && turns[0].ModelError != nil && turns[0].ModelError.RetryAttempt == 3
	})
	app.Settle()
	wantDelays := []time.Duration{4 * time.Second, 8 * time.Second, 16 * time.Second}
	if !slices.Equal(sched.delays(), wantDelays) {
		t.Fatalf("model retry delays = %v, want %v", sched.delays(), wantDelays)
	}

	// An agent actor cannot invoke SwitchAgentTurnToAlternative (You only).
	if err := ed.WithAgentActor(func() error {
		return ed.SwitchAgentTurnToAlternative(ed.AgentTurns()[0].ID)
	}); !errors.Is(err, editor.ErrAgentSettingsYouOnly) {
		t.Fatalf("agent SwitchAgentTurnToAlternative = %v, want ErrAgentSettingsYouOnly", err)
	}

	usageBeforeSwitch := ed.AgentUsage().WeeklyRequests

	// Clicking "Use Claude Code" switches Playtest analysis to Claude Code and runs the request there without double-charging usage.
	app.Press("id:agent-turn-switch-0")
	app.WaitUntil(5*time.Second, func() bool {
		claudeMu.Lock()
		n := len(claudeReqs)
		claudeMu.Unlock()
		return n == 1 && !ed.AgentWorking()
	})
	app.Settle()

	if got := ed.ResolveAgentModelRoute(editor.AgentTaskPlaytest); got != editor.AgentRouteLocal {
		t.Fatalf("playtest route after switch = %q, want %q", got, editor.AgentRouteLocal)
	}
	if got := ed.AgentTaskRoute(string(editor.AgentTaskPlaytest)); got != "Claude Code · default" {
		t.Fatalf("persisted playtest route label = %q, want Claude Code · default", got)
	}
	if got := ed.AgentUsage().WeeklyRequests; got != usageBeforeSwitch {
		t.Fatalf("switching agent double-charged weekly requests: got %d, want %d", got, usageBeforeSwitch)
	}
	claudeMu.Lock()
	gotReq := claudeReqs[0]
	claudeMu.Unlock()
	if gotReq.Prompt != "Analyze floor 3 deaths" || gotReq.Task != editor.AgentTaskPlaytest {
		t.Fatalf("Claude Code received req = %+v", gotReq)
	}
	turns := ed.AgentTurns()
	if len(turns) != 2 || !turns[1].Done || !strings.Contains(turns[1].Summary, "Claude Code finished the playtest report.") {
		t.Fatalf("switched turn = %+v", turns)
	}
	if apiHits.Load() < 3 {
		t.Fatalf("expected at least 3 529 attempts before switching, got %d", apiHits.Load())
	}
}

// Verifies endpoint URL validation (no plaintext http:// to remote hosts, no embedded credentials),
// secret redaction on error responses, and response size cap without pointless retries.
func TestBuiltInAgentHardeningEndpointRedactionAndLimits(t *testing.T) {
	const secret = "sk-ant-super-secret-redaction-check-999"

	// 1. Refuse insecure non-loopback http://, file://, and embedded-userinfo endpoints before sending key.
	for _, badURL := range []string{
		"http://api.anthropic.com/v1/messages",
		"file:///etc/passwd",
		"https://user:pass@api.anthropic.com/v1/messages",
	} {
		ed := newTestEditor(t)
		ed.AgentSecretStore = editor.NewMemorySecretStore(map[string]string{
			editor.AgentAPIKeySecretAccount: secret,
		})
		ed.BuiltInAgentEndpoint = badURL
		if err := ed.SubmitAgentTaskPrompt(editor.AgentTaskPlaytest, "test"); !errors.Is(err, editor.ErrBuiltInEndpointInsecure) {
			t.Fatalf("endpoint %q: got err=%v, want ErrBuiltInEndpointInsecure", badURL, err)
		}
	}

	// 2. Redact API key if an upstream error echoes it, and reject oversized responses without retrying.
	var oversizedHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/echo-secret" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]string{
					"type":    "invalid_request_error",
					"message": "bad header value " + r.Header.Get("x-api-key"),
				},
			})
			return
		}
		oversizedHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(strings.Repeat("a", (8<<20)+1024)))
	}))
	defer srv.Close()

	ed := newTestEditor(t)
	ed.AgentSecretStore = editor.NewMemorySecretStore(map[string]string{
		editor.AgentAPIKeySecretAccount: secret,
	})
	ed.BuiltInAgentEndpoint = srv.URL + "/echo-secret"
	ed.BuiltInHTTPClient = srv.Client()

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800))

	if err := ed.SubmitAgentTaskPrompt(editor.AgentTaskPlaytest, "Trigger 400"); err != nil {
		t.Fatalf("submit 400: %v", err)
	}
	app.WaitUntil(5*time.Second, func() bool { return !ed.AgentWorking() && len(ed.AgentTurns()) == 1 && ed.AgentTurns()[0].Done })
	turn := ed.AgentTurns()[0]
	if strings.Contains(turn.Error, secret) || (turn.ModelError != nil && (strings.Contains(turn.ModelError.Headline, secret) || strings.Contains(turn.ModelError.Message, secret))) {
		t.Fatalf("API key leaked into turn error state: err=%q modelErr=%+v", turn.Error, turn.ModelError)
	}
	if !strings.Contains(turn.Error, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] in error, got %q", turn.Error)
	}

	// Oversized response fails cleanly in 1 attempt without burning 4 retries.
	ed.BuiltInAgentEndpoint = srv.URL + "/oversized"
	if err := ed.SubmitAgentTaskPrompt(editor.AgentTaskPlaytest, "Trigger oversized"); err != nil {
		t.Fatalf("submit oversized: %v", err)
	}
	app.WaitUntil(5*time.Second, func() bool { return !ed.AgentWorking() && len(ed.AgentTurns()) == 2 && ed.AgentTurns()[1].Done })
	if got := oversizedHits.Load(); got != 1 {
		t.Fatalf("oversized response retried %d times, want 1 attempt only", got)
	}
	if !strings.Contains(ed.AgentTurns()[1].Error, "maximum size") {
		t.Fatalf("unexpected oversized error: %q", ed.AgentTurns()[1].Error)
	}

	// MemorySecretStore.String() must redact values.
	if s := ed.AgentSecretStore.(*editor.MemorySecretStore).String(); strings.Contains(s, secret) || !strings.Contains(s, "[REDACTED]") {
		t.Fatalf("MemorySecretStore.String leaked or did not redact: %q", s)
	}
}
