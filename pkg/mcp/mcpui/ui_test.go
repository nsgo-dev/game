package mcpui_test

import (
	"errors"
	"testing"

	"github.com/nsgo-dev/game/pkg/mcp/mcpui"
)

type fake struct {
	snap mcpui.Snapshot
	got  string
	act  mcpui.Action
}

func (f *fake) Snapshot() mcpui.Snapshot { return f.snap }
func (f *fake) Activate(path string, a mcpui.Action) error {
	f.got, f.act = path, a
	return nil
}

func TestActivateByLabel(t *testing.T) {
	f := &fake{snap: mcpui.Snapshot{Widgets: []mcpui.Widget{{Path: "root/play", Label: "Play", Enabled: true}}}}
	if err := mcpui.Activate(f, "play", mcpui.Action{Kind: "press"}); err != nil {
		t.Fatal(err)
	}
	if f.got != "root/play" || f.act.Kind != "press" {
		t.Fatalf("activated %q with %+v, want root/play with press", f.got, f.act)
	}
}

func TestFindPrefersPathOverLabel(t *testing.T) {
	snap := mcpui.Snapshot{Widgets: []mcpui.Widget{
		{Path: "menu/a", Label: "menu/b"},
		{Path: "menu/b", Label: "Back"},
	}}
	w, err := mcpui.Find(snap, "menu/b")
	if err != nil || w.Path != "menu/b" {
		t.Fatalf("Find = %+v, %v; want menu/b", w, err)
	}
}

func TestFindErrors(t *testing.T) {
	snap := mcpui.Snapshot{Widgets: []mcpui.Widget{
		{Path: "a/ok", Label: "OK"},
		{Path: "b/ok", Label: "ok"},
		{Path: "c/none"},
	}}
	if _, err := mcpui.Find(snap, "OK"); !errors.Is(err, mcpui.ErrAmbiguous) {
		t.Errorf("shared label: err = %v, want ErrAmbiguous", err)
	}
	if _, err := mcpui.Find(snap, "missing"); !errors.Is(err, mcpui.ErrNotFound) {
		t.Errorf("missing: err = %v, want ErrNotFound", err)
	}
	if _, err := mcpui.Find(snap, ""); !errors.Is(err, mcpui.ErrNotFound) {
		t.Errorf("empty query matched a widget without a label: err = %v", err)
	}
}

func TestActivateRefusesDisabledAndNil(t *testing.T) {
	f := &fake{snap: mcpui.Snapshot{Widgets: []mcpui.Widget{{Path: "root/play", Label: "Play"}}}}
	if err := mcpui.Activate(f, "Play", mcpui.Action{Kind: "press"}); !errors.Is(err, mcpui.ErrDisabled) {
		t.Fatalf("err = %v, want ErrDisabled", err)
	}
	if f.got != "" {
		t.Fatalf("disabled widget %q reached the provider", f.got)
	}
	if err := mcpui.Activate(nil, "Play", mcpui.Action{}); !errors.Is(err, mcpui.ErrNotFound) {
		t.Fatalf("nil provider: err = %v, want ErrNotFound", err)
	}
}
