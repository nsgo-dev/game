package main

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// boxAround returns the corners of a world rectangle around the units es, grown by pad points on every side.
func boxAround(g *rootSkirmish, es []*entity, pad float32) (from, to math2d.Vec2) {
	from, to = g.w.pos(es[0]), g.w.pos(es[0])
	for _, e := range es[1:] {
		from, to = from.Min(g.w.pos(e)), to.Max(g.w.pos(e))
	}
	return from.Sub(math2d.V2(pad, pad)), to.Add(math2d.V2(pad, pad))
}

// selected returns the player's selected entities.
func selected(g *rootSkirmish) []*entity {
	var out []*entity
	for _, id := range g.sel.IDs() {
		out = append(out, g.w.byBody[id])
	}
	return out
}

// openGround is a point on the player's side of the map with no unit, building, rock or crystal on it.
func openGround(g *rootSkirmish) math2d.Vec2 {
	c, _ := g.w.nearestWalkable(ai.Cell{Col: g.w.cols/2 - 6, Row: g.w.rows - 6})
	return g.w.cellCenter(c)
}

func TestMouseSelectsAndCommands(t *testing.T) {
	app, g := startGame(t)
	soldiers := unitsOf(g.w, player, "soldier")

	// A drag selects every one of the player's units its box touches.
	from, to := boxAround(g, soldiers, 3)
	boxSelect(app, g, from, to)
	for _, e := range soldiers {
		if !g.sel.Contains(e.body) {
			t.Fatalf("the box from %v to %v missed a soldier at %v", from, to, g.w.pos(e))
		}
	}
	for _, e := range selected(g) {
		if e.team != player {
			t.Fatal("a box selected an opponent's unit")
		}
	}

	// A click picks one unit, and a Shift click adds another. The pause keeps the click from counting as a double click with the drag's.
	app.Advance(time.Second)
	at := g.toView(g.w.pos(soldiers[0]))
	app.MouseDownAt("id:battlefield", at, core.MouseButtonLeft)
	app.MouseUpAt("id:battlefield", at, core.MouseButtonLeft)
	if g.sel.Len() != 1 || !g.sel.Contains(soldiers[0].body) {
		t.Fatalf("a click selected %d units", g.sel.Len())
	}
	app.Find("text:1 selected")
	at = g.toView(g.w.pos(soldiers[1]))
	app.MouseDownAtWith("id:battlefield", at, core.MouseButtonLeft, core.Modifiers{Shift: true})
	app.MouseUpAtWith("id:battlefield", at, core.MouseButtonLeft, core.Modifiers{Shift: true})
	if g.sel.Len() != 2 {
		t.Fatalf("a Shift click left %d units selected, want 2", g.sel.Len())
	}

	// A right click on open ground sends the selection there, fighting on the way.
	ground := openGround(g)
	rightClick(app, g, ground)
	for _, e := range soldiers {
		if e.order != orderAttackMove || e.dest != ground {
			t.Fatalf("a right click did not send a soldier: order %d dest %v, want %v", e.order, e.dest, ground)
		}
	}

	// A click on empty ground clears the selection.
	at = g.toView(ground.Add(math2d.V2(0, -30)))
	app.MouseDownAt("id:battlefield", at, core.MouseButtonLeft)
	app.MouseUpAt("id:battlefield", at, core.MouseButtonLeft)
	if g.sel.Len() != 0 {
		t.Fatalf("a click on empty ground left %d units selected", g.sel.Len())
	}

	// Right clicking a crystal cell with workers selected sets them mining it.
	press(app, "s")
	node := g.w.nodes[len(g.w.nodes)-1]
	rightClick(app, g, g.w.cellCenter(node.cell))
	for _, e := range unitsOf(g.w, player, "worker") {
		if e.order != orderGather || e.node != len(g.w.nodes)-1 {
			t.Fatalf("a right click on a crystal cell did not send a worker to it: order %d node %d", e.order, e.node)
		}
	}
}

func TestKeyboardHotkeysAndControlGroups(t *testing.T) {
	app, g := startGame(t)
	press(app, "a")
	if g.sel.Len() != 2 {
		t.Fatalf("A selected %d units, want the 2 soldiers", g.sel.Len())
	}
	chord(app, "ctrl", "2")
	press(app, "s")
	if g.sel.Len() != 4 || selected(g)[0].unit.Name != "worker" {
		t.Fatalf("S selected %d units, want the 4 workers", g.sel.Len())
	}
	press(app, "2")
	if g.sel.Len() != 2 || selected(g)[0].unit.Name != "soldier" {
		t.Fatalf("2 recalled %d units, want the 2 soldiers", g.sel.Len())
	}
	// Recalling the same group again at once highlights it, where a game with a camera would center on it.
	press(app, "2")
	if g.focus == 0 || g.focusGroup != 2 {
		t.Fatal("2 twice in a row did not focus group 2")
	}
	// Shift with a digit adds the selection to the group and selects the whole group.
	press(app, "s")
	chord(app, "shift", "2")
	if g.sel.Len() != 6 || len(g.groups.Group(2)) != 6 {
		t.Fatalf("Shift+2 left %d selected and %d in the group, want 6 and 6", g.sel.Len(), len(g.groups.Group(2)))
	}
	press(app, "escape")
	if g.sel.Len() != 0 {
		t.Fatal("Escape did not clear the selection")
	}

	// Q trains a worker, which the HUD shows.
	press(app, "q")
	if len(g.w.sides[player].hq.queue) != 1 {
		t.Fatal("Q did not queue a worker")
	}
	app.Find("text:Crystals 50")
	app.Find("id:train-worker")
	if got := app.Find("id:train-worker").Props.Label; got != "Worker 50 ×1" {
		t.Fatalf("the worker button reads %q, want the queued count", got)
	}

	// P pauses: the overlay shows and the world stops; P again resumes.
	press(app, "p")
	app.Find("text:Paused")
	steps := g.w.steps
	app.Advance(time.Second)
	if g.w.steps != steps {
		t.Fatal("the world kept stepping while paused")
	}
	press(app, "p")
	app.Advance(step)
	if app.Exists("id:overlay") || g.w.steps == steps {
		t.Fatal("P did not resume the skirmish")
	}

	// The arrow keys move the cursor and Space and Enter pick and command at it, so the keyboard alone plays too.
	start := g.cursor
	app.KeyDown("right")
	app.Advance(250 * time.Millisecond)
	app.KeyUp("right")
	if g.cursor.X <= start.X+20 || !g.cursorShown {
		t.Fatalf("holding Right moved the cursor from %v to %v", start, g.cursor)
	}
}

// steerCursor holds the left stick of controller 1 toward p until the cursor is on it.
func steerCursor(t *testing.T, app *nsgotest.App, g *rootSkirmish, p math2d.Vec2) {
	t.Helper()
	for range 10 * stepRate {
		d := p.Sub(g.cursor)
		if d.Len() < 1.5 {
			break
		}
		// Ease off near the target so a fixed step does not overshoot it.
		v := d.Normalize().Scale(min(1, d.Len()/8))
		app.SetGamepadAxis(1, gamepad.AxisLeftX, v.X)
		app.SetGamepadAxis(1, gamepad.AxisLeftY, v.Y)
		app.Advance(step)
	}
	app.SetGamepadAxis(1, gamepad.AxisLeftX, 0)
	app.SetGamepadAxis(1, gamepad.AxisLeftY, 0)
	app.Advance(step)
	if p.Dist(g.cursor) >= 3 {
		t.Fatalf("the stick did not bring the cursor to %v: it is at %v", p, g.cursor)
	}
}

// tapPad presses and releases button b on controller 1.
func tapPad(app *nsgotest.App, b gamepad.Button) {
	app.SetGamepadButton(1, b, 1)
	app.Advance(2 * step)
	app.SetGamepadButton(1, b, 0)
	app.Advance(2 * step)
}

func TestGamepadPlays(t *testing.T) {
	app, g := startGame(t)
	app.ConnectGamepad(1, "Standard Gamepad")
	soldiers := unitsOf(g.w, player, "soldier")
	steerCursor(t, app, g, g.w.pos(soldiers[0]))
	// South on a unit picks it.
	tapPad(app, gamepad.ButtonSouth)
	if g.sel.Len() != 1 || !g.sel.Contains(soldiers[0].body) {
		t.Fatalf("South on a soldier selected %d units", g.sel.Len())
	}
	// East orders the selection to the cursor.
	ground := openGround(g)
	steerCursor(t, app, g, ground)
	tapPad(app, gamepad.ButtonEast)
	if soldiers[0].order != orderAttackMove || soldiers[0].dest.Dist(ground) > 3 {
		t.Fatalf("East did not send the soldier to the cursor: order %d dest %v", soldiers[0].order, soldiers[0].dest)
	}
	// Holding South while the stick moves drags a box, once the soldier has arrived.
	app.Advance(8 * time.Second)
	from, to := boxAround(g, soldiers, 4)
	steerCursor(t, app, g, from)
	app.SetGamepadButton(1, gamepad.ButtonSouth, 1)
	app.Advance(2 * step)
	steerCursor(t, app, g, to)
	app.SetGamepadButton(1, gamepad.ButtonSouth, 0)
	app.Advance(2 * step)
	for _, e := range soldiers {
		if !g.sel.Contains(e.body) {
			t.Fatal("a box dragged with the stick missed a soldier")
		}
	}
	// West trains a worker, the left trigger selects the army, Back clears the selection, and Start pauses and resumes.
	tapPad(app, gamepad.ButtonWest)
	if len(g.w.sides[player].hq.queue) != 1 {
		t.Fatal("West did not queue a worker")
	}
	tapPad(app, gamepad.ButtonBack)
	if g.sel.Len() != 0 {
		t.Fatal("Back did not clear the selection")
	}
	tapPad(app, gamepad.ButtonLT)
	if g.sel.Len() != 2 {
		t.Fatalf("the left trigger selected %d units, want 2", g.sel.Len())
	}
	tapPad(app, gamepad.ButtonStart)
	if !g.paused || !app.Exists("text:Paused") {
		t.Fatal("Start did not pause")
	}
	tapPad(app, gamepad.ButtonStart)
	if g.paused {
		t.Fatal("Start did not resume")
	}
}

func TestTouchPlays(t *testing.T) {
	app, g := startGame(t)
	soldiers := unitsOf(g.w, player, "soldier")
	// A tap on a unit selects it.
	f := app.Touch("id:battlefield", g.toView(g.w.pos(soldiers[0])))
	f.Lift()
	if g.sel.Len() != 1 || !g.sel.Contains(soldiers[0].body) {
		t.Fatalf("a tap on a soldier selected %d units", g.sel.Len())
	}
	// A tap on open ground sends the selection there.
	ground := openGround(g)
	f = app.Touch("id:battlefield", g.toView(ground))
	f.Lift()
	if soldiers[0].order != orderAttackMove || soldiers[0].dest.Dist(ground) > 1 {
		t.Fatalf("a tap on open ground did not send the soldier: order %d dest %v", soldiers[0].order, soldiers[0].dest)
	}
	// A browser's mouse events made up from that tap are ignored.
	app.MouseDownAt("id:battlefield", g.toView(ground.Add(math2d.V2(0, -40))), core.MouseButtonRight)
	app.MouseUpAt("id:battlefield", g.toView(ground.Add(math2d.V2(0, -40))), core.MouseButtonRight)
	if soldiers[0].dest.Dist(ground) > 1 {
		t.Fatal("a mouse click right after a touch gave a command")
	}
	// A drag selects the units in its box, once the soldier has arrived.
	app.Advance(8 * time.Second)
	from, to := boxAround(g, soldiers, 4)
	f = app.Touch("id:battlefield", g.toView(from))
	f.Move(g.toView(from.Lerp(to, 0.5)))
	f.Move(g.toView(to))
	f.Lift()
	for _, e := range soldiers {
		if !g.sel.Contains(e.body) {
			t.Fatal("a drag missed a soldier")
		}
	}
	// The command bar's buttons give a finger every hotkey but the control groups, which are keyboard-only.
	app.Press("id:train-worker")
	if len(g.w.sides[player].hq.queue) != 1 {
		t.Fatal("the worker button did not queue a worker")
	}
	app.Press("id:select-workers")
	if g.sel.Len() != 4 {
		t.Fatalf("the workers button selected %d units, want 4", g.sel.Len())
	}
	app.Press("id:select-army")
	if g.sel.Len() != 2 {
		t.Fatalf("the army button selected %d units, want 2", g.sel.Len())
	}
	app.Press("id:deselect")
	if g.sel.Len() != 0 {
		t.Fatalf("the clear button left %d units selected", g.sel.Len())
	}
	if !app.Find("id:deselect").Props.Disabled {
		t.Fatal("the clear button is enabled with nothing selected")
	}
	if !app.Find("id:build-barracks").Props.Disabled {
		t.Fatal("the barracks button is enabled without the crystals")
	}
	app.Press("id:pause")
	app.Find("text:Paused")
	app.Press("id:overlay-action")
	if g.paused {
		t.Fatal("Resume did not resume")
	}
}
