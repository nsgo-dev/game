package flow_test

import (
	"errors"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/flow"
	"go.yaml.in/yaml/v3"
)

func mustParseMapEvent(t *testing.T, src string) flow.MapEventDoc {
	t.Helper()
	doc, err := flow.ParseMapEvent([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestMapEventWaitResumeAndSnapshotMidList(t *testing.T) {
	doc := mustParseMapEvent(t, "version: 1\npages:\n  - commands:\n      - say: {text: hello}\n      - wait: {seconds: 1}\n      - set: {name: score, value: 2}\n      - self: {name: done, value: true}\n")
	var says []string
	r := flow.NewEventRunner(doc)
	r.Say = func(s string) { says = append(says, s) }
	if !r.Update(time.Second) || r.Running() {
		t.Fatal("a runner that was never started is not idle")
	}
	if err := r.Start(0); err != nil {
		t.Fatal(err)
	}
	if r.Update(0) {
		t.Fatal("event completed before its wait")
	}
	if len(says) != 1 || says[0] != "hello" {
		t.Fatalf("says = %v, want [hello]", says)
	}
	if r.Update(400 * time.Millisecond) {
		t.Fatal("wait completed early")
	}

	data, err := yaml.Marshal(r.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	var snap flow.EventSnapshot
	if err := yaml.Unmarshal(data, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Page != 0 || snap.Command != 2 || snap.Waiting != 600*time.Millisecond {
		t.Fatalf("snapshot = %+v, want page 0, command 2, 600ms left", snap)
	}

	restored := flow.NewEventRunner(doc)
	restored.Say = func(s string) { says = append(says, s) }
	if err := restored.Restore(snap); err != nil {
		t.Fatal(err)
	}
	if restored.Update(599 * time.Millisecond) {
		t.Fatal("restored wait completed early")
	}
	if !restored.Update(time.Millisecond) {
		t.Fatal("restored event did not finish once its wait ran out")
	}
	if len(says) != 1 || restored.Variables["score"] != 2 || !restored.Self["done"] || restored.Running() {
		t.Fatalf("says=%v vars=%v self=%v running=%v", says, restored.Variables, restored.Self, restored.Running())
	}
}

func TestMapEventWaitCarriesLeftoverTime(t *testing.T) {
	doc := mustParseMapEvent(t, "version: 1\npages:\n  - commands:\n      - wait: {seconds: 1}\n      - wait: {seconds: 1}\n      - set: {name: n, value: 1}\n")
	r := flow.NewEventRunner(doc)
	if err := r.Start(0); err != nil {
		t.Fatal(err)
	}
	if r.Update(1500 * time.Millisecond) {
		t.Fatal("finished after 1.5s of 2s of waits")
	}
	if !r.Update(500*time.Millisecond) || r.Variables["n"] != 1 {
		t.Fatalf("not finished after 2s of 2s of waits: vars=%v", r.Variables)
	}
}

func TestMapEventActivePageIsLastThatHolds(t *testing.T) {
	doc := mustParseMapEvent(t, `version: 1
pages:
  - trigger: action
  - when: {switches: {lit: true}}
    trigger: touch
  - when: {self: {opened: true}, variables: {gold: 10}}
    trigger: autorun
`)
	tests := []struct {
		name     string
		self     map[string]bool
		switches map[string]bool
		vars     map[string]int
		want     int
	}{
		{"nothing set", nil, nil, nil, 0},
		{"switch on", nil, map[string]bool{"lit": true}, nil, 1},
		{"self without enough gold", map[string]bool{"opened": true}, map[string]bool{"lit": true}, map[string]int{"gold": 9}, 1},
		{"self and gold at least the value", map[string]bool{"opened": true}, nil, map[string]int{"gold": 10}, 2},
	}
	for _, tc := range tests {
		if got := doc.ActivePage(tc.self, tc.switches, tc.vars); got != tc.want {
			t.Errorf("%s: ActivePage = %d, want %d", tc.name, got, tc.want)
		}
	}
	if got := doc.Pages[0].Trigger; got != flow.TriggerAction {
		t.Errorf("empty trigger read as %q, want %q", got, flow.TriggerAction)
	}

	none := mustParseMapEvent(t, "version: 1\npages:\n  - when: {switches: {lit: true}}\n    trigger: parallel\n")
	if got := none.ActivePage(nil, nil, nil); got != -1 {
		t.Errorf("ActivePage with no page holding = %d, want -1", got)
	}
}

func TestParseMapEventRefusesInvalidDocuments(t *testing.T) {
	tests := map[string]string{
		"unknown command":     "version: 1\npages:\n  - commands:\n      - nope: true\n",
		"two commands":        "version: 1\npages:\n  - commands:\n      - say: {text: a}\n        wait: {seconds: 1}\n",
		"empty command":       "version: 1\npages:\n  - commands:\n      - {}\n",
		"unknown trigger":     "version: 1\npages:\n  - trigger: shout\n",
		"negative wait":       "version: 1\npages:\n  - commands:\n      - wait: {seconds: -1}\n",
		"nan wait":            "version: 1\npages:\n  - commands:\n      - wait: {seconds: .nan}\n",
		"infinite wait":       "version: 1\npages:\n  - commands:\n      - wait: {seconds: .inf}\n",
		"unnamed set":         "version: 1\npages:\n  - commands:\n      - set: {value: 1}\n",
		"unnamed self":        "version: 1\npages:\n  - commands:\n      - self: {value: true}\n",
		"no pages":            "version: 1\n",
		"no version":          "pages:\n  - trigger: action\n",
		"future version":      "version: 2\npages:\n  - trigger: action\n",
		"trailing document":   "version: 1\npages:\n  - trigger: action\n---\nversion: 1\n",
		"empty":               "",
		"unknown page field":  "version: 1\npages:\n  - sprite: chest\n",
		"unknown condition":   "version: 1\npages:\n  - when: {items: {potion: 1}}\n",
		"duplicate page keys": "version: 1\npages:\n  - trigger: action\n    trigger: touch\n",
	}
	for name, src := range tests {
		_, err := flow.ParseMapEvent([]byte(src))
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	_, err := flow.ParseMapEvent([]byte(tests["unknown trigger"]))
	if !errors.Is(err, flow.ErrInvalidMapEvent) {
		t.Errorf("unknown trigger error %v does not wrap ErrInvalidMapEvent", err)
	}
}

func TestEventRunnerRefusesSnapshotsThatDoNotFit(t *testing.T) {
	doc := mustParseMapEvent(t, "version: 1\npages:\n  - commands:\n      - say: {text: a}\n      - wait: {seconds: 1}\n")
	r := flow.NewEventRunner(doc)
	r.Variables["keep"] = 1
	for name, s := range map[string]flow.EventSnapshot{
		"version":          {Version: 2},
		"page past end":    {Version: 1, Page: 1},
		"page below idle":  {Version: 1, Page: -2},
		"idle with wait":   {Version: 1, Page: -1, Waiting: time.Second},
		"command past end": {Version: 1, Page: 0, Command: 3},
		"negative command": {Version: 1, Page: 0, Command: -1},
		"negative wait":    {Version: 1, Page: 0, Command: 2, Waiting: -1},
		"wait after say":   {Version: 1, Page: 0, Command: 1, Waiting: time.Second},
	} {
		if err := r.Restore(s); !errors.Is(err, flow.ErrInvalidEventSnapshot) {
			t.Errorf("%s: Restore = %v, want ErrInvalidEventSnapshot", name, err)
		}
	}
	if r.Running() || r.Variables["keep"] != 1 {
		t.Fatal("a refused snapshot changed the runner")
	}
	if err := r.Start(1); err == nil {
		t.Error("Start(1) of a one-page event succeeded")
	}
	if err := r.Restore(flow.EventSnapshot{Version: 1, Page: 0, Command: 2, Waiting: time.Second}); err != nil {
		t.Fatalf("Restore of a wait in progress: %v", err)
	}
}
