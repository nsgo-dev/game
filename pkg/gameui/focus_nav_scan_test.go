package gameui

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

func TestSpatialNavigatorScanAdvancesOnIntervalAndSelects(t *testing.T) {
	var selected int
	a := &Button{Text: "A", OnClick: func() { selected++ }}
	b := &Button{Text: "B", OnClick: func() { selected++ }}
	root := NewRoot(&VBox{Children: []Widget{a, b}})
	n := NewSpatialNavigator(root)
	n.SetScanOptions(ScanOptions{Enabled: true, Interval: time.Second, Select: NavConfirm})
	if n.Step(500*time.Millisecond) || n.Focused() != nil {
		t.Fatal("scan advanced before interval")
	}
	if !n.Step(500*time.Millisecond) || n.Focused() == nil {
		t.Fatal("scan did not focus first item at interval")
	}
	if !n.ScanSelect() || selected != 1 {
		t.Fatalf("select result=%v selected=%d", n.ScanSelect(), selected)
	}
}

func TestSpatialNavigatorScanLoopsPause(t *testing.T) {
	a := &Button{Text: "A"}
	b := &Button{Text: "B"}
	n := NewSpatialNavigator(NewRoot(&VBox{Children: []Widget{a, b}}))
	n.SetScanOptions(ScanOptions{Enabled: true, Interval: time.Millisecond, Loops: 1})
	n.Step(3 * time.Millisecond)
	if n.Scan.Enabled {
		t.Fatal("scan remained enabled after configured loop")
	}
}

func TestSpatialNavigatorScanFollowsFocusOrderInLaidOutRow(t *testing.T) {
	a := &Button{Text: "A"}
	b := &Button{Text: "B"}
	c := &Button{Text: "C"}
	root := NewRoot(&HBox{Children: []Widget{a, b, c}})
	root.Layout(core.Bounds{Size: core.Size{Width: 300, Height: 40}})
	n := NewSpatialNavigator(root)
	var changes int
	n.OnFocusChange = func(_, _ Widget) { changes++ }
	n.SetScanOptions(ScanOptions{Enabled: true, Interval: time.Second})
	want := []Widget{a, b, c, a}
	for i, w := range want {
		if !n.Step(time.Second) {
			t.Fatalf("step %d did not move focus", i)
		}
		if got := n.Focused(); got != w {
			t.Fatalf("step %d focused %v, want %v", i, got, w)
		}
	}
	if changes != len(want) {
		t.Fatalf("OnFocusChange calls = %d, want %d", changes, len(want))
	}
}
