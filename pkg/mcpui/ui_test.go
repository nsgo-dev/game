package mcpui_test

import (
	"github.com/nsgo-dev/game/pkg/mcpui"
	"testing"
)

type fake struct {
	snap mcpui.Snapshot
	got  string
}

func (f *fake) Snapshot() mcpui.Snapshot                   { return f.snap }
func (f *fake) Activate(path string, a mcpui.Action) error { f.got = path; return nil }
func TestActivateByLabel(t *testing.T) {
	f := &fake{snap: mcpui.Snapshot{Widgets: []mcpui.Widget{{Path: "root/play", Label: "Play", Enabled: true}}}}
	if err := mcpui.Activate(f, "play", mcpui.Action{Kind: "press"}); err != nil {
		t.Fatal(err)
	}
	if f.got != "root/play" {
		t.Fatal(f.got)
	}
}
