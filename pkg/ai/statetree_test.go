package ai_test

import (
	"github.com/nsgo-dev/game/pkg/ai"
	"testing"
)

func TestParseStateTreeStrictAndNested(t *testing.T) {
	t1, err := ai.ParseStateTree([]byte("version: 1\nstates:\n  - id: idle\n    children:\n      - id: patrol\n        transitions:\n          - {on: event, to: combat, priority: 2}\n"))
	if err != nil || len(t1.States) != 1 {
		t.Fatal(err)
	}
	if _, err := ai.ParseStateTree([]byte("version: 1\nstates:\n  - id: idle\n    nope: true\n")); err == nil {
		t.Fatal("unknown field accepted")
	}
}
