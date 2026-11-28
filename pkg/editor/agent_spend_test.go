package editor_test

import (
	"bytes"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
)

const agentSpendSettingsYAML = `version: 1
language: system
appearance: system
agent:
  weeklySpendLimitUSD: 2.5
  requestSpendLimitUSD: 0.25
  toolCallLimit: 24
  usage:
    weekStart: "2026-11-23"
    inputTokens: 100
    cachedInputTokens: 25
    outputTokens: 40
    estimatedCostUSD: 1.25
`

func TestAgentSpendSettingsRoundTrip(t *testing.T) {
	s, err := editor.ParseEditorSettings([]byte(agentSpendSettingsYAML))
	if err != nil {
		t.Fatal(err)
	}
	a := s.Agent
	if a == nil || a.WeeklySpendLimitUSD == nil || *a.WeeklySpendLimitUSD != 2.5 || a.RequestSpendLimitUSD == nil || *a.RequestSpendLimitUSD != 0.25 || a.ToolCallLimit == nil || *a.ToolCallLimit != 24 {
		t.Fatalf("caps = %+v", a)
	}
	if u := a.Usage; u == nil || u.InputTokens != 100 || u.CachedInputTokens != 25 || u.OutputTokens != 40 || u.EstimatedCostUSD != 1.25 {
		t.Fatalf("usage = %+v", a.Usage)
	}
	out, err := editor.EncodeEditorSettings(s)
	if err != nil {
		t.Fatal(err)
	}
	again, err := editor.ParseEditorSettings(out)
	if err != nil {
		t.Fatal(err)
	}
	out2, err := editor.EncodeEditorSettings(again)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, out2) {
		t.Fatalf("round trip changed output:\n%s\n---\n%s", out, out2)
	}
	for _, field := range []string{"weeklySpendLimitUSD: 2.5", "requestSpendLimitUSD: 0.25", "toolCallLimit: 24", "inputTokens: 100", "cachedInputTokens: 25", "outputTokens: 40", "estimatedCostUSD: 1.25"} {
		if !bytes.Contains(out, []byte(field)) {
			t.Fatalf("encoded settings miss %q:\n%s", field, out)
		}
	}
}

func TestAgentSpendCapAloneSurvivesEncode(t *testing.T) {
	s, err := editor.ParseEditorSettings([]byte("version: 1\nlanguage: system\nappearance: system\nagent:\n  toolCallLimit: 8\n"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := editor.EncodeEditorSettings(s)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte("toolCallLimit: 8")) {
		t.Fatalf("encoded settings dropped toolCallLimit:\n%s", out)
	}
}

func TestAgentSpendSettingsRejectInvalidValues(t *testing.T) {
	for _, tc := range []struct{ yaml, field string }{
		{"  weeklySpendLimitUSD: -1\n", "agent.weeklySpendLimitUSD"},
		{"  weeklySpendLimitUSD: .nan\n", "agent.weeklySpendLimitUSD"},
		{"  requestSpendLimitUSD: .inf\n", "agent.requestSpendLimitUSD"},
		{"  toolCallLimit: -2\n", "agent.toolCallLimit"},
		{"  usage:\n    outputTokens: -5\n", "agent.usage.outputTokens"},
		{"  usage:\n    estimatedCostUSD: -0.5\n", "agent.usage.estimatedCostUSD"},
	} {
		_, err := editor.ParseEditorSettings([]byte("version: 1\nlanguage: system\nappearance: system\nagent:\n" + tc.yaml))
		if err == nil || !strings.Contains(err.Error(), tc.field) {
			t.Errorf("%q: err = %v, want mention of %s", tc.yaml, err, tc.field)
		}
	}
}

func TestRecordAgentSpendWeeklyCapAndRollover(t *testing.T) {
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC) // Thursday
	ed.Now = func() time.Time { return now }
	s, err := editor.ParseEditorSettings([]byte("version: 1\nlanguage: system\nappearance: system\nagent:\n  weeklySpendLimitUSD: 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.ApplySettings(*s); err != nil {
		t.Fatal(err)
	}

	ed.RecordAgentRequest(3)
	ed.RecordAgentSpend(10, 4, 6, 0.75)
	if got := ed.AgentUsage(); got.WeeklyRequests != 3 || got.InputTokens != 10 || got.CachedInputTokens != 4 || got.OutputTokens != 6 || got.EstimatedCostUSD != 0.75 {
		t.Fatalf("usage = %+v", got)
	}
	if ed.AgentSpendCapReached() {
		t.Fatal("cap reached at $0.75 of $1")
	}
	ed.RecordAgentSpend(-1, -1, -1, -1)
	if got := ed.AgentUsage(); got.InputTokens != 10 || got.EstimatedCostUSD != 0.75 {
		t.Fatalf("negative usage was recorded: %+v", got)
	}
	ed.RecordAgentSpend(0, 0, 0, 0.25)
	if !ed.AgentSpendCapReached() {
		t.Fatal("cap not reached at $1 of $1")
	}

	now = now.AddDate(0, 0, 7)
	if got := ed.AgentUsage(); got.WeeklyRequests != 0 || got.InputTokens != 0 || got.CachedInputTokens != 0 || got.OutputTokens != 0 || got.EstimatedCostUSD != 0 {
		t.Fatalf("usage after the weekly reset = %+v", got)
	}
	if ed.AgentSpendCapReached() {
		t.Fatal("cap still reached after the weekly reset")
	}
	ed.RecordAgentRequest(1)
	if got := ed.AgentUsage(); got.WeeklyRequests != 1 || got.EstimatedCostUSD != 0 || got.InputTokens != 0 {
		t.Fatalf("a request in the new week kept last week's spend: %+v", got)
	}
}

func TestAgentSpendInvalidCostAndDisabledCap(t *testing.T) {
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	ed.RecordAgentSpend(10, 5, 4, 0.5)
	ed.RecordAgentSpend(1, 1, 1, 0.25)
	if got := ed.AgentUsage(); got.InputTokens != 11 || got.CachedInputTokens != 6 || got.OutputTokens != 5 || got.EstimatedCostUSD != 0.75 {
		t.Fatalf("usage=%+v", got)
	}
	ed.RecordAgentSpend(99, 99, 99, math.NaN())
	if ed.AgentSpendCapReached() {
		t.Fatal("disabled weekly cap reported reached")
	}
}
