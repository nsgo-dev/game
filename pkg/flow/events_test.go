package flow_test

import (
	"github.com/nsgo-dev/game/pkg/flow"
	"testing"
	"time"
)

func TestMapEventWaitResumeSnapshotAndUnknownCommand(t *testing.T) {
	doc, err := flow.ParseMapEvent([]byte("version: 1\npages:\n  - trigger: action\n    commands:\n      - say: {text: hello}\n      - wait: {seconds: 1}\n      - set: {name: score, value: 2}\n"))
	if err != nil {
		t.Fatal(err)
	}
	var says []string
	r := flow.NewEventRunner(doc)
	r.Say = func(s string) { says = append(says, s) }
	if r.Update(0) {
		t.Fatal("event completed before wait")
	}
	snap := r.Snapshot()
	if r.Update(500 * time.Millisecond) {
		t.Fatal("wait completed early")
	}
	if err := r.Restore(snap); err != nil {
		t.Fatal(err)
	}
	if !r.Update(time.Second) {
		t.Fatal("event did not resume")
	}
	if len(says) != 1 || r.Variables["score"] != 2 {
		t.Fatalf("says=%v vars=%v", says, r.Variables)
	}
	if _, err := flow.ParseMapEvent([]byte("version: 1\npages:\n - commands:\n   - nope: true\n")); err == nil {
		t.Fatal("unknown command accepted")
	}
}
