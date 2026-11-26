package main

import (
	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/nsgo/core"
)

// Action and axis names, as game.yaml's input map declares them.
const (
	actionFire    = "fire"
	actionFocus   = "focus"
	actionBomb    = "bomb"
	actionPause   = "pause"
	actionConfirm = "confirm"
	axisMoveX     = "moveX"
	axisMoveY     = "moveY"
)

// bombRegion is where the touch bomb button is, in playfield points, at the bottom right. It is not a touchRegion of game.yaml's input map, because a touchRegion is held by any finger inside it, and the finger that steers the ship would set off a bomb as it slid over the button.
var bombRegion = core.Bounds{Origin: core.Point{X: 206, Y: 416}, Size: core.Size{Width: 56, Height: 56}}

// drag steers the ship by dragging anywhere on the playfield, with a finger or with the mouse's left button: the ship moves as far as the pointer does, so a finger never covers it, and its guns fire while the pointer is down. A finger that goes down on the bomb button sets off a bomb instead, and one that slides onto it while dragging does not.
type drag struct {
	// toGame maps a point in the game view to the playfield, and false outside it.
	toGame func(core.Point) (core.Point, bool)

	finger   int
	touching bool
	mouse    bool
	last     core.Point
	// dx and dy are how far the pointer moved since the last step.
	dx, dy float32
	// bomb is whether a finger went down on the bomb button since the last step.
	bomb bool
}

// held reports whether a finger or the mouse is steering the ship.
func (g *drag) held() bool { return g.touching || g.mouse }

// take returns how far the pointer moved since the last call.
func (g *drag) take() (dx, dy float32) {
	dx, dy = g.dx, g.dy
	g.dx, g.dy = 0, 0
	return dx, dy
}

// takeBomb reports whether a finger went down on the bomb button since the last call.
func (g *drag) takeBomb() bool {
	bomb := g.bomb
	g.bomb = false
	return bomb
}

// reset lets go of the pointer, as a scene change or pause does.
func (g *drag) reset() {
	g.touching, g.mouse = false, false
	g.dx, g.dy = 0, 0
	g.bomb = false
}

// moveTo moves the pointer to view point p, adding how far it went on the playfield.
func (g *drag) moveTo(p core.Point) {
	q, ok := g.toGame(p)
	if !ok {
		return
	}
	g.dx += float32(q.X - g.last.X)
	g.dy += float32(q.Y - g.last.Y)
	g.last = q
}

// start begins a drag at view point p, unless p is off the playfield. The mouse drags from anywhere, as the bomb button is only for touch and shows only on a touch screen.
func (g *drag) start(p core.Point) bool {
	q, ok := g.toGame(p)
	if !ok {
		return false
	}
	g.last = q
	return true
}

// onTouch follows the first finger that goes down on the playfield until it lifts, and sets off a bomb for each finger that goes down on the bomb button.
func (g *drag) onTouch(ev core.TouchEvent) {
	switch ev.Phase {
	case core.TouchStart:
		for _, t := range ev.Touches {
			if !t.Changed {
				continue
			}
			if q, ok := g.toGame(t.Position); ok && bombRegion.Contains(q) {
				g.bomb = true
				continue
			}
			if !g.touching && g.start(t.Position) {
				g.touching, g.finger, g.mouse = true, t.ID, false
			}
		}
	case core.TouchMove:
		for _, t := range ev.Touches {
			if g.touching && t.ID == g.finger {
				g.moveTo(t.Position)
			}
		}
	case core.TouchEnd, core.TouchCancel:
		for _, t := range ev.Touches {
			if t.ID == g.finger && (t.Changed || ev.Phase == core.TouchCancel) {
				g.touching = false
			}
		}
		if ev.Phase == core.TouchCancel && len(ev.Touches) == 0 {
			g.touching = false
		}
	}
}

// onMouseDown starts a mouse drag with the left button.
func (g *drag) onMouseDown(ev core.MouseButtonEvent) {
	if ev.Button != core.MouseButtonLeft || g.touching {
		return
	}
	g.mouse = g.start(ev.Position)
}

// onMouseUp ends a mouse drag, as the left button comes up or the window loses the focus while it is held.
func (g *drag) onMouseUp(ev core.MouseButtonEvent) {
	if ev.Button == core.MouseButtonLeft {
		g.mouse = false
	}
}

// onMouseMove follows the mouse while its left button drags the ship.
func (g *drag) onMouseMove(ev core.MouseEvent) {
	if !g.mouse {
		return
	}
	if ev.PressedButton != core.MouseButtonLeft {
		g.mouse = false
		return
	}
	g.moveTo(ev.Position)
}

// attach wires the drag onto the game view. Call it before Actions.Attach, which chains the input map's handlers onto these.
func (g *drag) attach(v *core.View) {
	v.OnTouch = g.onTouch
	v.OnMouseDown = g.onMouseDown
	v.OnMouseUp = g.onMouseUp
	v.OnMouseMove = g.onMouseMove
	v.OnMouseLeave = func(core.MouseEvent) { g.mouse = false }
}

// sample reads one fixed step's controls from the input map and the drag. Call it once a step, after Actions.Step.
func sample(a *input.Actions, g *drag) controls {
	dx, dy := g.take()
	return controls{
		MoveX: float32(a.Axis(axisMoveX)),
		MoveY: float32(a.Axis(axisMoveY)),
		DragX: dx,
		DragY: dy,
		Fire:  a.Pressed(actionFire) || g.held(),
		Focus: a.Pressed(actionFocus),
		Bomb:  a.JustPressed(actionBomb) || g.takeBomb(),
	}
}
