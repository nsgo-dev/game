package editor

import "testing"

func TestRecordAgentUsageAndWeeklySpendCap(t *testing.T) {
	e := &Editor{settings: EditorSettings{Agent: &AgentSettings{WeeklySpendLimitUSD: 1}}}
	e.RecordAgentUsage(10, 4, 6, 0.75)
	if got := e.AgentUsage(); got.WeeklyInputTokens != 10 || got.CachedInputTokens != 4 || got.WeeklyOutputTokens != 6 || got.WeeklyCostUSD != 0.75 {
		t.Fatalf("usage = %+v", got)
	}
	if e.AgentWeeklySpendLimitReached() {
		t.Fatal("cap reached too early")
	}
	e.RecordAgentUsage(0, 0, 0, 0.25)
	if !e.AgentWeeklySpendLimitReached() {
		t.Fatal("cap not reached")
	}
}
