package cards_test

import (
	"testing"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

// mountTable mounts the table's board on a view the size of the board, as a game wraps its Canvas, and returns the running app.
func mountTable(t *testing.T, tb *table) *nsgotest.App {
	t.Helper()
	return nsgotest.Start(t, func(cx *core.Scope) core.Node {
		view := core.View{
			TestID: "table",
			Style:  core.Style{Width: core.Pt(400), Height: core.Pt(400)},
			Children: []core.Node{core.Canvas{
				Label: "Table",
				Style: core.Style{Width: core.Pt(400), Height: core.Pt(400)},
				Draw:  func(*core.Drawing, core.Size) {},
			}},
		}
		tb.b.Attach(&view)
		return view
	})
}

func TestViewMouseDragSnapsIntoSlot(t *testing.T) {
	tb := newTable(t)
	app := mountTable(t, tb)
	// Drag reports a mouse's primary button as finger 0, as the desktops do: from card a up into the play area.
	app.DragAt("id:table", core.Point{X: 150, Y: 350}, core.Point{X: 50, Y: -240})
	if tb.a.Slot() != tb.play || len(tb.drops) != 1 || len(tb.returns) != 0 {
		t.Fatalf("mouse drag: a in %s, drops %+v, returns %+v", tb.a.Slot().ID, tb.drops, tb.returns)
	}
	tb.b.Update(tb.b.SnapDuration)
	if !nearVec(tb.a.Pose().Pos, v(130, 90)) {
		t.Fatalf("a snapped to %v, want its cell at (130, 90)", tb.a.Pose().Pos)
	}
}

func TestViewTouchDropOnRefusingSlotReturns(t *testing.T) {
	tb := newTable(t)
	app := mountTable(t, tb)
	start := tb.c.Pose()
	finger := app.Touch("id:table", core.Point{X: 240, Y: 350})
	finger.Move(core.Point{X: 300, Y: 200})
	finger.Move(core.Point{X: 350, Y: 90})
	if !tb.c.Held() {
		t.Fatal("touch did not pick card c up")
	}
	if s, ok := tb.b.DropTarget(tb.c); s != tb.deck || ok {
		t.Fatalf("DropTarget = %v %v, want the refusing deck", s, ok)
	}
	finger.Lift()
	if len(tb.returns) != 1 || tb.returns[0].Rejected != tb.deck || tb.c.Slot() != tb.hand {
		t.Fatalf("returns %+v, c in %s", tb.returns, tb.c.Slot().ID)
	}
	tb.b.Update(tb.b.ReturnDuration)
	if tb.c.Pose() != start {
		t.Fatalf("c returned to %+v, want %+v", tb.c.Pose(), start)
	}

	// A second finger then drags card a into the play area, where it snaps.
	finger = app.Touch("id:table", core.Point{X: 150, Y: 350})
	finger.Move(core.Point{X: 180, Y: 200})
	finger.Move(core.Point{X: 200, Y: 110})
	finger.Lift()
	if tb.a.Slot() != tb.play || len(tb.drops) != 1 {
		t.Fatalf("touch drag: a in %s, drops %+v", tb.a.Slot().ID, tb.drops)
	}
}

func TestViewTouchCancelReturns(t *testing.T) {
	tb := newTable(t)
	app := mountTable(t, tb)
	finger := app.Touch("id:table", core.Point{X: 200, Y: 350})
	finger.Move(core.Point{X: 200, Y: 100})
	finger.Cancel()
	if len(tb.returns) != 1 || tb.returns[0].Rejected != nil || tb.bc.Held() {
		t.Fatalf("cancel: returns %+v", tb.returns)
	}
}
