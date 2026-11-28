package editor_test

import (
	"github.com/nsgo-dev/game/pkg/editor"
	"testing"
)

func TestAgentSpendUsageAndCapsRoundTrip(t *testing.T) {
	_, err := editor.ParseEditorSettings([]byte("version: 1\nlanguage: system\nappearance: system\nagent:\n  weeklySpendLimitUSD: 2.5\n  requestSpendLimitUSD: 0.25\n  toolCallLimit: 24\n  usage:\n    inputTokens: 100\n    cachedInputTokens: 25\n    outputTokens: 40\n    estimatedCostUSD: 1.25\n"))
	if err != nil {
		t.Fatal(err)
	}
}
