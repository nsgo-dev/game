package rts_test

import (
	"slices"
	"testing"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/pathfind"
	"github.com/nsgo-dev/game/pkg/rts"
	"github.com/nsgo-dev/nsgo/core"
)

func TestSelectionSetOperations(t *testing.T) {
	var s rts.Selection
	s.Add(3, 1, 3, 0, 2)
	if !slices.Equal(s.IDs(), []rts.UnitID{3, 1, 2}) {
		t.Fatalf("Add = %v", s.IDs())
	}
	s.Toggle(1, 4)
	if !slices.Equal(s.IDs(), []rts.UnitID{3, 1, 2, 4}) {
		t.Fatalf("Toggle with an unselected unit = %v, want it added", s.IDs())
	}
	s.Toggle(1, 4)
	if !slices.Equal(s.IDs(), []rts.UnitID{3, 2}) {
		t.Fatalf("Toggle of selected units = %v, want them removed", s.IDs())
	}
	s.Set(s.IDs())
	if !slices.Equal(s.IDs(), []rts.UnitID{3, 2}) {
		t.Fatalf("Set to its own IDs = %v", s.IDs())
	}
	s.Retain(func(id rts.UnitID) bool { return id != 3 })
	if !slices.Equal(s.IDs(), []rts.UnitID{2}) || s.Contains(3) || !s.Contains(2) || s.Len() != 1 {
		t.Fatalf("Retain = %v", s.IDs())
	}
	s.Remove(2)
	s.Remove(9)
	if s.Len() != 0 {
		t.Fatalf("Remove = %v", s.IDs())
	}
	s.Apply(rts.Replace, []rts.UnitID{5})
	s.Apply(rts.Extend, []rts.UnitID{6})
	s.Apply(rts.Invert, []rts.UnitID{5})
	if !slices.Equal(s.IDs(), []rts.UnitID{6}) {
		t.Fatalf("Apply = %v", s.IDs())
	}
	s.Clear()
	if s.Len() != 0 || s.Contains(6) {
		t.Fatal("Clear kept units")
	}
}

func TestModeFor(t *testing.T) {
	for _, tc := range []struct {
		m    core.Modifiers
		want rts.SelectMode
	}{
		{core.Modifiers{}, rts.Replace},
		{core.Modifiers{Shift: true}, rts.Extend},
		{core.Modifiers{Control: true}, rts.Invert},
		{core.Modifiers{Platform: true}, rts.Invert},
		{core.Modifiers{Shift: true, Control: true}, rts.Extend},
	} {
		if got := rts.ModeFor(tc.m); got != tc.want {
			t.Errorf("ModeFor(%+v) = %v, want %v", tc.m, got, tc.want)
		}
	}
}

func newSelectorWorld() (*rts.Crowd, *rts.BoxSelector, []rts.UnitID) {
	c := rts.NewCrowd(pathfind.NewBoolGrid(100, 100), rts.CrowdOptions{})
	var ids []rts.UnitID
	for i := range 10 {
		ids = append(ids, c.Add(rts.UnitSpec{Pos: math2d.Vec2{X: 10 + float32(i)*5, Y: 10}, Radius: 1, Kind: i % 2}))
	}
	sel := &rts.Selection{}
	return c, &rts.BoxSelector{Picker: c, Selection: sel, DragThreshold: 2}, ids
}

func TestBoxSelectorClickAndDrag(t *testing.T) {
	c, b, ids := newSelectorWorld()
	_ = c
	// A click picks the unit under the pointer.
	b.Press(math2d.Vec2{X: 15.5, Y: 10.2}, core.Modifiers{}, 1)
	b.Drag(math2d.Vec2{X: 15.7, Y: 10.2})
	if b.Dragging() {
		t.Fatal("a small move started a drag")
	}
	if got := b.Release(math2d.Vec2{X: 15.7, Y: 10.2}); !slices.Equal(got, []rts.UnitID{ids[1]}) {
		t.Fatalf("click picked %v", got)
	}
	// A drag picks every unit its box touches, in any direction.
	b.Press(math2d.Vec2{X: 31, Y: 20}, core.Modifiers{}, 1)
	b.Drag(math2d.Vec2{X: 19, Y: 0})
	box, ok := b.Box()
	if !ok || box != (math2d.Rect{X: 19, Y: 0, W: 12, H: 20}) {
		t.Fatalf("Box = %v, %v", box, ok)
	}
	b.Release(math2d.Vec2{X: 19, Y: 0})
	if !slices.Equal(b.Selection.IDs(), ids[2:5]) {
		t.Fatalf("drag selected %v, want %v", b.Selection.IDs(), ids[2:5])
	}
	if _, ok := b.Box(); ok {
		t.Fatal("Box after release")
	}
	// Shift extends, Control toggles.
	b.Press(math2d.Vec2{X: 10, Y: 10}, core.Modifiers{Shift: true}, 1)
	b.Release(math2d.Vec2{X: 10, Y: 10})
	if !slices.Equal(b.Selection.IDs(), []rts.UnitID{ids[2], ids[3], ids[4], ids[0]}) {
		t.Fatalf("shift-click = %v", b.Selection.IDs())
	}
	b.Press(math2d.Vec2{X: 20, Y: 10}, core.Modifiers{Control: true}, 1)
	b.Release(math2d.Vec2{X: 20, Y: 10})
	if b.Selection.Contains(ids[2]) || b.Selection.Len() != 3 {
		t.Fatalf("ctrl-click = %v", b.Selection.IDs())
	}
	// A click on empty ground clears; a cancelled pick changes nothing.
	b.Press(math2d.Vec2{X: 80, Y: 80}, core.Modifiers{Control: true}, 1)
	b.Cancel()
	if got := b.Release(math2d.Vec2{X: 80, Y: 80}); got != nil || b.Selection.Len() != 3 {
		t.Fatal("cancelled pick changed the selection")
	}
	b.Press(math2d.Vec2{X: 80, Y: 80}, core.Modifiers{}, 1)
	b.Release(math2d.Vec2{X: 80, Y: 80})
	if b.Selection.Len() != 0 {
		t.Fatal("click on empty ground kept the selection")
	}
}

func TestBoxSelectorDoubleClickAndFilter(t *testing.T) {
	_, b, ids := newSelectorWorld()
	b.View = math2d.Rect{X: 0, Y: 0, W: 36, H: 20}
	b.Press(math2d.Vec2{X: 20, Y: 10}, core.Modifiers{}, 2)
	b.Release(math2d.Vec2{X: 20, Y: 10})
	// ids[2] is kind 0; ids[0], ids[4] are the other kind-0 units on screen.
	if !slices.Equal(b.Selection.IDs(), []rts.UnitID{ids[2], ids[0], ids[4]}) {
		t.Fatalf("double click = %v", b.Selection.IDs())
	}
	b.View = math2d.Rect{}
	b.Filter = func(id rts.UnitID) bool { return id != ids[6] }
	b.Press(math2d.Vec2{X: 20, Y: 10}, core.Modifiers{}, 2)
	b.Release(math2d.Vec2{X: 20, Y: 10})
	if !slices.Equal(b.Selection.IDs(), []rts.UnitID{ids[2], ids[0], ids[4], ids[8]}) {
		t.Fatalf("double click with whole-world view and filter = %v", b.Selection.IDs())
	}
	b.Press(math2d.Vec2{X: 40, Y: 10}, core.Modifiers{}, 1)
	if got := b.Release(math2d.Vec2{X: 40, Y: 10}); len(got) != 0 {
		t.Fatalf("filtered unit was picked: %v", got)
	}
}

func TestControlGroups(t *testing.T) {
	var g rts.ControlGroups
	var sel rts.Selection
	key := func(k string, m core.Modifiers) rts.GroupEvent {
		return g.HandleKey(core.KeyEvent{Key: k, Modifiers: m}, &sel, 0)
	}
	sel.Set([]rts.UnitID{1, 2})
	if ev := key("1", core.Modifiers{Control: true}); ev != (rts.GroupEvent{Action: rts.GroupAssigned, Group: 1}) {
		t.Fatalf("ctrl+1 = %+v", ev)
	}
	sel.Set([]rts.UnitID{3})
	if ev := key("1", core.Modifiers{Shift: true}); ev.Action != rts.GroupAdded || !slices.Equal(sel.IDs(), []rts.UnitID{1, 2, 3}) {
		t.Fatalf("shift+1 = %+v, selection %v", ev, sel.IDs())
	}
	sel.Set([]rts.UnitID{9})
	key("2", core.Modifiers{Platform: true})
	if ev := g.HandleKey(core.KeyEvent{Key: "1"}, &sel, 100); ev.Action != rts.GroupRecalled || !slices.Equal(sel.IDs(), []rts.UnitID{1, 2, 3}) {
		t.Fatalf("1 = %+v, selection %v", ev, sel.IDs())
	}
	if ev := g.HandleKey(core.KeyEvent{Key: "1"}, &sel, 110); ev.Action != rts.GroupFocused {
		t.Fatalf("second 1 within the window = %+v", ev)
	}
	// A third tap starts a new double tap rather than focusing again.
	if ev := g.HandleKey(core.KeyEvent{Key: "1"}, &sel, 115); ev.Action != rts.GroupRecalled {
		t.Fatalf("third 1 = %+v", ev)
	}
	if ev := g.HandleKey(core.KeyEvent{Key: "1"}, &sel, 115+rts.DefaultDoubleTapSteps+1); ev.Action != rts.GroupRecalled {
		t.Fatalf("1 after the window = %+v", ev)
	}
	if ev := g.HandleKey(core.KeyEvent{Key: "2"}, &sel, 200); ev.Action != rts.GroupRecalled || !slices.Equal(sel.IDs(), []rts.UnitID{9}) {
		t.Fatalf("2 = %+v", ev)
	}
	for _, ev := range []core.KeyEvent{{Key: "3"}, {Key: "a"}, {Key: "f1"}, {Key: "1", Repeat: true}, {Key: "1", Modifiers: core.Modifiers{Alt: true}}} {
		if got := g.HandleKey(ev, &sel, 300); got.Action != rts.NoGroupAction {
			t.Fatalf("HandleKey(%+v) = %+v", ev, got)
		}
	}
	g.Forget(2)
	g.Retain(func(id rts.UnitID) bool { return id != 3 })
	if !slices.Equal(g.Group(1), []rts.UnitID{1}) || g.Group(10) != nil || g.Group(-1) != nil {
		t.Fatalf("group 1 = %v", g.Group(1))
	}
	g.Assign(0, []rts.UnitID{4, 4, 0, 5})
	if !slices.Equal(g.Group(0), []rts.UnitID{4, 5}) {
		t.Fatalf("Assign = %v", g.Group(0))
	}
	if rts.GroupFocused.String() != "focused" || rts.Arrived.String() != "arrived" {
		t.Fatal("String names changed")
	}
}
