package flow_test

import (
	"github.com/nsgo-dev/game/pkg/flow"
	"testing"
)

func TestEventPageSelectionUsesLastMatchingPage(t *testing.T) {
	e, err := flow.ParseEvent([]byte("version: 1\npages:\n  - trigger: action\n    sprite: closed\n  - when: {self: opened, is: true}\n    trigger: action\n    sprite: open\n"))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := e.SelectPage(map[string]bool{"opened": true})
	if p.Sprite != "open" {
		t.Fatalf("sprite=%q", p.Sprite)
	}
	p, _ = e.SelectPage(nil)
	if p.Sprite != "closed" {
		t.Fatalf("default sprite=%q", p.Sprite)
	}
}
func TestEventRejectsUnknownCommandAndTrigger(t *testing.T) {
	for _, raw := range []string{"version: 1\npages: [{trigger: action, commands: [{teleport: {map: town}}]}]", "version: 1\npages: [{trigger: mystery}]"} {
		if _, err := flow.ParseEvent([]byte(raw)); err == nil {
			t.Fatal("invalid event accepted")
		}
	}
}
func TestEventRoundTrip(t *testing.T) {
	raw := []byte("version: 1\npages:\n  - trigger: touch\n    commands:\n      - wait: {frames: 2}\n")
	e, err := flow.ParseEvent(raw)
	if err != nil {
		t.Fatal(err)
	}
	out, err := flow.EncodeEvent(e)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("empty encoding")
	}
}
