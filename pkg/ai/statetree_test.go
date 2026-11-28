package ai_test

import (
	"bytes"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/pkg/ai"
	"strings"
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

func TestStateTreeRoundTripAndValidation(t *testing.T) {
	doc := ai.StateTree{
		Version:    1,
		Parameters: map[string]string{"phase": "combat"},
		States: []ai.State{{
			ID:           "idle",
			Enter:        []ai.StateCondition{{Key: "enemy_visible", Op: "eq", Value: true}},
			Tasks:        []ai.Task{{Name: "watch", Params: map[string]any{"radius": 8}}},
			CompleteWhen: "any",
			Transitions:  []ai.Transition{{On: "event", To: "combat", Priority: 3, Delay: 2, Restart: true}},
			Children:     []ai.State{{ID: "patrol", Tasks: []ai.Task{{Name: "walk"}}}},
		}},
	}
	encoded, err := ai.EncodeStateTree(&doc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(encoded, []byte(ai.StateTreeSchemaComment+"\n")) {
		t.Fatalf("missing schema comment: %s", encoded)
	}
	parsed, err := ai.ParseStateTree(encoded)
	if err != nil {
		t.Fatal(err)
	}
	encodedAgain, err := ai.EncodeStateTree(&parsed)
	if err != nil || !bytes.Equal(encoded, encodedAgain) {
		t.Fatalf("round trip changed bytes: %v\n%s\n%s", err, encoded, encodedAgain)
	}
	loaded, err := ai.LoadStateTreeFS(fstest.MapFS{"ai/guard.statetree.yaml": &fstest.MapFile{Data: encoded}}, "ai/guard.statetree.yaml")
	if err != nil || len(loaded.States) != 1 {
		t.Fatalf("LoadStateTreeFS = %+v, %v", loaded, err)
	}
}

func TestStateTreeRejectsInvalidDocuments(t *testing.T) {
	valid := "version: 1\nstates:\n  - id: idle\n"
	for name, data := range map[string]string{
		"unknown":        valid + "    nope: true\n",
		"trailing":       valid + "---\nversion: 1\nstates:\n  - id: other\n",
		"duplicate id":   "version: 1\nstates:\n  - id: idle\n  - id: idle\n",
		"bad operator":   "version: 1\nstates:\n  - id: idle\n    enter:\n      - {key: hp, op: between, value: 1}\n",
		"missing value":  "version: 1\nstates:\n  - id: idle\n    enter:\n      - {key: hp, op: eq}\n",
		"negative delay": "version: 1\nstates:\n  - id: idle\n    transitions:\n      - {on: event, to: next, delay: -1}\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ai.ParseStateTree([]byte(data)); err == nil {
				t.Fatal("ParseStateTree accepted invalid document")
			}
		})
	}
	if _, err := ai.ParseStateTree([]byte(strings.Replace(valid, "version: 1", "version: 2", 1))); err == nil {
		t.Fatal("unsupported version accepted")
	}
}

func FuzzParseStateTree(f *testing.F) {
	f.Add([]byte("version: 1\nstates:\n  - id: idle\n"))
	f.Add([]byte("not yaml"))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ai.ParseStateTree(data)
	})
}
