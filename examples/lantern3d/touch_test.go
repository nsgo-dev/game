package main

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func touchControlFrame(t testing.TB, tc *input.TouchControls, id string) core.Bounds {
	t.Helper()
	for _, info := range tc.Controls() {
		if info.ID == id {
			return info.Frame
		}
	}
	t.Fatalf("no touch control %q", id)
	return core.Bounds{}
}

func boundsCenter(b core.Bounds) core.Point {
	return core.Point{
		X: b.Origin.X + b.Size.Width/2,
		Y: b.Origin.Y + b.Size.Height/2,
	}
}

func boundsOverlap(a, b core.Bounds) bool {
	return a.Origin.X < b.Origin.X+b.Size.Width &&
		b.Origin.X < a.Origin.X+a.Size.Width &&
		a.Origin.Y < b.Origin.Y+b.Size.Height &&
		b.Origin.Y < a.Origin.Y+a.Size.Height
}

type multiTouch struct {
	app     *nsgotest.App
	fingers map[int]core.Point
}

func (m *multiTouch) report(phase core.GesturePhase, id int, at core.Point) {
	touches := []core.Touch{{ID: id, PagePosition: at, Changed: true}}
	for other, p := range m.fingers {
		if other != id {
			touches = append(touches, core.Touch{ID: other, PagePosition: p})
		}
	}
	m.app.Report(nsgotest.Event{Kind: nsgotest.EventTouch, Phase: phase, Touches: touches})
}

func (m *multiTouch) down(id int, at core.Point) {
	if m.fingers == nil {
		m.fingers = map[int]core.Point{}
	}
	m.report(core.GestureBegan, id, at)
	m.fingers[id] = at
}

func (m *multiTouch) move(id int, at core.Point) {
	m.fingers[id] = at
	m.report(core.GestureChanged, id, at)
}

func (m *multiTouch) up(id int) {
	at, ok := m.fingers[id]
	if !ok {
		return
	}
	m.report(core.GestureEnded, id, at)
	delete(m.fingers, id)
}

func startPhoneLantern(t *testing.T, mode SceneMode) (*nsgotest.App, *Controller) {
	t.Helper()
	var c *Controller
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var node core.Node
		c, node = newView(cx, mode)
		c.SwitchFade = 40 * time.Millisecond
		return node
	},
		nsgotest.Size(844, 390),
		nsgotest.ManualFrames(),
		nsgotest.NoGPU(),
		nsgotest.Environment(core.Environment{
			Idiom:    core.IdiomPhone,
			Pointer:  core.PointerCoarse,
			SafeArea: core.Edges{Left: 47, Right: 47, Bottom: 21},
		}),
	)
	app.Advance(2 * fixedStep)
	return app, c
}

func TestTouchControlsWalkAllEightDirectionsAndEnterPortal(t *testing.T) {
	app, c := startPhoneLantern(t, Mode2D)

	// On a touch device in 2D mode, the floating stick shows right away and USE stays hidden until near the portal.
	if !app.Exists("id:touch-move") {
		t.Fatal("expected id:touch-move to be visible on a phone in 2D mode")
	}
	if app.Exists("id:touch-use") {
		t.Fatal("expected id:touch-use to stay hidden while away from the portal")
	}

	moveFrame := touchControlFrame(t, c.moveTouch, "move")
	useFrame := touchControlFrame(t, c.useTouch, "use")

	// Both controls stay inside the safe area, meet the 44pt target minimum, sit in their respective halves, and clear the bottom-right HUD buttons.
	if moveFrame.Origin.X < 47 || moveFrame.Origin.X+moveFrame.Size.Width > 844/2 || moveFrame.Origin.Y+moveFrame.Size.Height > 390-21 {
		t.Fatalf("move control outside safe left half: %+v", moveFrame)
	}
	if useFrame.Origin.X < 844/2 || useFrame.Origin.X+useFrame.Size.Width > 844-47 || useFrame.Origin.Y+useFrame.Size.Height > 390-21 {
		t.Fatalf("use control outside safe right half: %+v", useFrame)
	}
	if useFrame.Size.Width < input.MinTouchTarget || useFrame.Size.Height < input.MinTouchTarget {
		t.Fatalf("use control smaller than MinTouchTarget: %+v", useFrame)
	}
	for _, hudID := range []string{"id:btn-switch-scene", "id:btn-pause", "id:btn-mute"} {
		hudBounds := app.Find(hudID).Bounds()
		if boundsOverlap(useFrame, hudBounds) {
			t.Fatalf("use control %+v overlaps HUD button %s %+v", useFrame, hudID, hudBounds)
		}
	}

	// Tapping where USE sits while away from the portal does not switch scenes.
	mt := &multiTouch{app: app}
	mt.down(2, boundsCenter(useFrame))
	app.Advance(2 * fixedStep)
	mt.up(2)
	app.Advance(2 * fixedStep)
	if c.Mode() != Mode2D || c.Manager.Transitioning() {
		t.Fatalf("tapping hidden USE away from portal switched mode: mode=%v transitioning=%v", c.Mode(), c.Manager.Transitioning())
	}

	// Drag the floating stick in all eight directions from different start points inside the left zone.
	c.Map.Traveler.X = 160
	c.Map.Traveler.Y = 120
	c.Map.PrevX = 160
	c.Map.PrevY = 120

	dirs := []struct {
		name           string
		startX, startY float64 // fractional position within moveFrame
		dx, dy         core.Points
		wantX, wantY   int // expected sign of traveler movement in Map2D coordinates (-1, 0, +1)
	}{
		{"north", 0.35, 0.60, 0, -45, 0, -1},
		{"northeast", 0.40, 0.55, 35, -35, 1, -1},
		{"east", 0.30, 0.50, 45, 0, 1, 0},
		{"southeast", 0.35, 0.45, 35, 35, 1, 1},
		{"south", 0.50, 0.40, 0, 45, 0, 1},
		{"southwest", 0.65, 0.45, -35, 35, -1, 1},
		{"west", 0.60, 0.50, -45, 0, -1, 0},
		{"northwest", 0.55, 0.60, -35, -35, -1, -1},
	}

	for _, dir := range dirs {
		c.Map.Traveler.X = 160
		c.Map.Traveler.Y = 120
		c.Map.PrevX = 160
		c.Map.PrevY = 120
		before := c.Map.TravelerPos()

		start := core.Point{
			X: moveFrame.Origin.X + moveFrame.Size.Width*core.Points(dir.startX),
			Y: moveFrame.Origin.Y + moveFrame.Size.Height*core.Points(dir.startY),
		}
		mt.down(1, start)
		mt.move(1, core.Point{X: start.X + dir.dx, Y: start.Y + dir.dy})
		app.Advance(180 * time.Millisecond)
		mt.up(1)
		app.Advance(2 * fixedStep)

		after := c.Map.TravelerPos()
		movedX := after.X - before.X
		movedY := after.Y - before.Y
		if dir.wantX > 0 && movedX < 6 || dir.wantX < 0 && movedX > -6 || dir.wantX == 0 && math.Abs(float64(movedX)) > 1 {
			t.Fatalf("%s: movedX=%.2f (want sign %d), before=%v after=%v", dir.name, movedX, dir.wantX, before, after)
		}
		if dir.wantY > 0 && movedY < 6 || dir.wantY < 0 && movedY > -6 || dir.wantY == 0 && math.Abs(float64(movedY)) > 1 {
			t.Fatalf("%s: movedY=%.2f (want sign %d), before=%v after=%v", dir.name, movedY, dir.wantY, before, after)
		}
	}

	// Reset to the courtyard entrance and walk north into the portal with one continuous stick drag.
	c.ResetCurrentView()
	app.Advance(2 * fixedStep)
	if app.Exists("id:touch-use") {
		t.Fatal("USE still visible after resetting away from portal")
	}

	start := boundsCenter(moveFrame)
	mt.down(1, start)
	mt.move(1, core.Point{X: start.X, Y: start.Y - 48})
	app.Advance(700 * time.Millisecond)

	if !c.Map.NearPortal() {
		t.Fatalf("stick drag north did not reach portal: traveler=%v", c.Map.TravelerPos())
	}
	if !app.Exists("id:touch-use") {
		t.Fatal("USE button did not appear when traveler entered the portal ring")
	}
	if !c.moveTouch.Stick("move").Active() {
		t.Fatal("appearing USE button dropped the active stick finger")
	}

	// Walking south out of the portal hides USE again.
	mt.move(1, core.Point{X: start.X, Y: start.Y + 48})
	app.Advance(700 * time.Millisecond)
	if c.Map.NearPortal() || app.Exists("id:touch-use") {
		t.Fatalf("USE stayed visible after walking away from portal: near=%v exists=%v", c.Map.NearPortal(), app.Exists("id:touch-use"))
	}

	// Walk north back into the portal and tap USE with a second finger while still holding the stick.
	mt.move(1, core.Point{X: start.X, Y: start.Y - 48})
	app.Advance(700 * time.Millisecond)
	if !c.Map.NearPortal() || !app.Exists("id:touch-use") {
		t.Fatalf("did not re-enter portal: near=%v exists=%v", c.Map.NearPortal(), app.Exists("id:touch-use"))
	}

	mt.down(2, boundsCenter(useFrame))
	app.Advance(2 * fixedStep)
	mt.up(2)
	mt.up(1)

	if c.Mode() != Mode3D {
		t.Fatalf("tapping USE at portal did not switch to 3D: mode=%v", c.Mode())
	}
	app.Advance(c.SwitchFade + 40*time.Millisecond)
	if c.Mode() != Mode3D || c.Manager.Transitioning() {
		t.Fatalf("after USE switch fade: mode=%v transitioning=%v", c.Mode(), c.Manager.Transitioning())
	}
	if app.Exists("id:touch-move") || app.Exists("id:touch-use") {
		t.Fatal("touch controls remained visible after switching to 3D")
	}
	if c.Actions.Axis("move_x") != 0 || c.Actions.Axis("move_y") != 0 || c.moveTouch.Stick("move").Active() {
		t.Fatalf("held stick leaked into 3D: move_x=%v move_y=%v active=%v",
			c.Actions.Axis("move_x"), c.Actions.Axis("move_y"), c.moveTouch.Stick("move").Active())
	}
}

func TestTouchControlsHiddenIn3DAndOnDesktopUntilTouched(t *testing.T) {
	t.Run("hidden in 3D on phone and 3D drag untouched", func(t *testing.T) {
		app, c := startPhoneLantern(t, Mode3D)
		app.Press("id:btn-orbit")
		app.Advance(2 * fixedStep)

		if app.Exists("id:touch-move") || app.Exists("id:touch-use") {
			t.Fatal("2D touch controls appeared in 3D mode on a phone")
		}

		// 1-finger drag in the left half still orbits the 3D camera and never drives 2D movement axes.
		yawBefore := c.Scene.Orbit.Yaw
		finger := app.Touch("id:lantern-root", core.Point{X: 180, Y: 220})
		finger.Move(core.Point{X: 100, Y: 220})
		app.Advance(4 * fixedStep)
		if c.Actions.Axis("move_x") != 0 || c.Actions.Axis("move_y") != 0 {
			t.Fatalf("3D touch drag drove 2D stick axes: move_x=%v move_y=%v", c.Actions.Axis("move_x"), c.Actions.Axis("move_y"))
		}
		finger.Lift()
		app.Advance(200 * time.Millisecond)
		if math.Abs(float64(c.Scene.Orbit.Yaw-yawBefore)) < 0.2 {
			t.Fatalf("3D 1-finger touch drag did not orbit: before=%.3f after=%.3f", yawBefore, c.Scene.Orbit.Yaw)
		}

		// Switching to 2D shows the floating stick immediately on a phone.
		app.Press("id:btn-switch-scene")
		app.Advance(c.SwitchFade + 40*time.Millisecond)
		if c.Mode() != Mode2D || !app.Exists("id:touch-move") || app.Exists("id:touch-use") {
			t.Fatalf("after switching to 2D on phone: mode=%v move=%v use=%v", c.Mode(), app.Exists("id:touch-move"), app.Exists("id:touch-use"))
		}
	})

	t.Run("hidden on desktop until touched", func(t *testing.T) {
		var c *Controller
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			var node core.Node
			c, node = newView(cx, Mode2D)
			c.SwitchFade = 40 * time.Millisecond
			return node
		},
			nsgotest.Size(960, 540),
			nsgotest.ManualFrames(),
			nsgotest.NoGPU(),
			nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Pointer: core.PointerFine}),
		)
		app.Advance(2 * fixedStep)

		if app.Exists("id:touch-move") || app.Exists("id:touch-use") {
			t.Fatal("touch controls showed on desktop before any touch")
		}

		// A touch anywhere on the 2D map reveals the floating stick (while USE stays hidden away from the portal).
		finger := app.Touch("id:lantern-map2d", core.Point{X: 200, Y: 300})
		app.Advance(2 * fixedStep)
		finger.Lift()
		app.Advance(2 * fixedStep)
		if !app.Exists("id:touch-move") || app.Exists("id:touch-use") {
			t.Fatalf("after desktop touch away from portal: move=%v use=%v", app.Exists("id:touch-move"), app.Exists("id:touch-use"))
		}

		// Walking north to the portal with W hides the controls again (keyboard took over).
		app.KeyDown("w")
		app.Advance(700 * time.Millisecond)
		app.KeyUp("w")
		app.Advance(2 * fixedStep)
		if !c.Map.NearPortal() {
			t.Fatalf("W did not walk traveler to portal: pos=%v", c.Map.TravelerPos())
		}
		if app.Exists("id:touch-move") || app.Exists("id:touch-use") {
			t.Fatalf("keyboard input did not hide touch controls on desktop: move=%v use=%v", app.Exists("id:touch-move"), app.Exists("id:touch-use"))
		}

		// Touching the 2D map again while at the portal shows both the stick and USE.
		finger = app.Touch("id:lantern-map2d", core.Point{X: 200, Y: 300})
		app.Advance(2 * fixedStep)
		finger.Lift()
		app.Advance(2 * fixedStep)
		if !app.Exists("id:touch-move") || !app.Exists("id:touch-use") {
			t.Fatalf("touch on desktop at portal did not show move and use: move=%v use=%v", app.Exists("id:touch-move"), app.Exists("id:touch-use"))
		}
	})
}

func TestSwitchTo3DMidStickDragLeavesNoHeldDirection(t *testing.T) {
	app, c := startPhoneLantern(t, Mode2D)
	c.Scene.AutoOrbit = false

	moveFrame := touchControlFrame(t, c.moveTouch, "move")
	start := boundsCenter(moveFrame)

	// Hold the floating stick right so the traveler is actively walking east.
	mt := &multiTouch{app: app}
	mt.down(1, start)
	mt.move(1, core.Point{X: start.X + 48, Y: start.Y})
	app.Advance(150 * time.Millisecond)
	if c.Map.TravelerPos().X <= travelerStartX+5 || c.Actions.Axis("move_x") <= 0.5 {
		t.Fatalf("expected active east stick drag before switch: pos=%v move_x=%v", c.Map.TravelerPos(), c.Actions.Axis("move_x"))
	}

	// Switch to 3D mid-drag while finger 1 is still held down.
	yawBefore := c.Scene.Orbit.Yaw
	app.Press("id:btn-switch-scene")
	app.Advance(c.SwitchFade + 40*time.Millisecond)

	if c.Mode() != Mode3D || c.Manager.Transitioning() {
		t.Fatalf("did not settle in 3D after mid-drag switch: mode=%v transitioning=%v", c.Mode(), c.Manager.Transitioning())
	}
	if c.Actions.Axis("move_x") != 0 || c.Actions.Axis("move_y") != 0 || c.Actions.Pressed("move_right") || c.moveTouch.Stick("move").Active() {
		t.Fatalf("mid-drag switch leaked direction into 3D: move_x=%v move_y=%v right=%v active=%v",
			c.Actions.Axis("move_x"), c.Actions.Axis("move_y"), c.Actions.Pressed("move_right"), c.moveTouch.Stick("move").Active())
	}

	// Trailing motion/release from the old finger and extra frames in 3D must not drift the 3D camera or leave stick state behind.
	mt.move(1, core.Point{X: start.X + 60, Y: start.Y})
	mt.up(1)
	app.Advance(200 * time.Millisecond)
	if math.Abs(float64(c.Scene.Orbit.Yaw-yawBefore)) > 1e-3 {
		t.Fatalf("3D orbit yaw drifted after mid-drag switch: before=%.4f after=%.4f", yawBefore, c.Scene.Orbit.Yaw)
	}

	// Returning to 2D with no finger down keeps the traveler stationary.
	app.Press("id:btn-switch-scene")
	app.Advance(c.SwitchFade + 40*time.Millisecond)
	posAfterReturn := c.Map.TravelerPos()
	app.Advance(200 * time.Millisecond)
	if c.Map.TravelerPos() != posAfterReturn {
		t.Fatalf("traveler drifted after returning to 2D: before=%v after=%v", posAfterReturn, c.Map.TravelerPos())
	}
}

func TestTouchControlsHideAndResetWhilePausedIn2D(t *testing.T) {
	app, c := startPhoneLantern(t, Mode2D)

	moveFrame := touchControlFrame(t, c.moveTouch, "move")
	start := boundsCenter(moveFrame)

	mt := &multiTouch{app: app}
	mt.down(1, start)
	mt.move(1, core.Point{X: start.X + 44, Y: start.Y})
	app.Advance(100 * time.Millisecond)
	if !c.moveTouch.Stick("move").Active() {
		t.Fatal("stick was not active before pause")
	}

	app.Press("id:btn-pause")
	app.Advance(2 * fixedStep)
	if !c.Paused() || app.Exists("id:touch-move") || c.moveTouch.Stick("move").Active() {
		t.Fatalf("pause did not hide and reset touch controls: paused=%v exists=%v active=%v",
			c.Paused(), app.Exists("id:touch-move"), c.moveTouch.Stick("move").Active())
	}
	mt.up(1)

	app.Press("id:btn-resume")
	app.Advance(2 * fixedStep)
	if c.Paused() || !app.Exists("id:touch-move") || c.moveTouch.Alpha() != 1 {
		t.Fatalf("resume did not restore awake touch controls: paused=%v exists=%v alpha=%v",
			c.Paused(), app.Exists("id:touch-move"), c.moveTouch.Alpha())
	}
}
