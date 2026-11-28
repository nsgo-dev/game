package gameui

import (
	"testing"
	"time"
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
