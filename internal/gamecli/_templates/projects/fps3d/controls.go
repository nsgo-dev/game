package main

import (
	"time"

	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/nsgo/core"
)

// Action and axis names, as game.yaml's input map declares them.
const (
	actionJump    = "jump"
	actionFire    = "fire"
	actionReload  = "reload"
	actionNext    = "next"
	actionWeapon1 = "weapon1"
	actionWeapon2 = "weapon2"
	actionPause   = "pause"

	axisMoveX = "moveX"
	axisMoveY = "moveY"
	axisLookX = "lookX"
	axisLookY = "lookY"

	// The mouse's locked motion and the touch sticks' vertical axes are bound in code: game.yaml has no pointer axes or on-screen sticks, and a stick's vertical axis reads downward.
	axisMouseX     = "mouseX"
	axisMouseY     = "mouseY"
	axisTouchMoveY = "touchMoveY"
	axisTouchLookY = "touchLookY"
)

// newActions returns the game's actions: game.yaml's input map, mouselook while the pointer is locked and the touch pad.
func newActions(pad *input.VirtualPad) *input.Actions {
	a := input.NewActions()
	Project.BindInput(a)
	a.BindPointerAxis(axisMouseX, input.PointerX, input.PointerAxisOptions{Sensitivity: float64(arena.Player.MouseSensitivity)})
	a.BindPointerAxis(axisMouseY, input.PointerY, input.PointerAxisOptions{Sensitivity: float64(arena.Player.MouseSensitivity)})
	pad.BindActions(a)
	return a
}

// newTouchPad returns the on-screen controls: a floating move stick on the left, a floating look stick beside it, and fire, jump, reload and next-weapon buttons on the right. layoutTouchPad places them.
func newTouchPad() *input.VirtualPad {
	pad := input.NewVirtualPad()
	pad.AddStick(&input.VirtualStick{Name: "touchMove", AxisXName: axisMoveX, AxisYName: axisTouchMoveY, Radius: 56, DeadZone: 0.15, Floating: true})
	pad.AddStick(&input.VirtualStick{Name: "touchLook", AxisXName: axisLookX, AxisYName: axisTouchLookY, Radius: 56, DeadZone: 0.1, Floating: true})
	for _, b := range []struct{ action, label string }{{actionFire, "Fire"}, {actionJump, "Jump"}, {actionReload, "Reload"}, {actionNext, "Swap"}} {
		pad.AddButton(&input.VirtualButton{Action: b.action, Label: b.label})
	}
	return pad
}

// layoutTouchPad fits the touch controls to a game view of size: the sticks take the lower left and the lower middle, the buttons a column on the right.
func layoutTouchPad(pad *input.VirtualPad, size core.Size) {
	w, h := float32(size.Width), float32(size.Height)
	move, look := pad.Sticks[0], pad.Sticks[1]
	move.Zone = bounds(0, h*0.35, w*0.4, h*0.65)
	move.Center = core.Point{X: core.Points(w * 0.15), Y: core.Points(h * 0.75)}
	look.Zone = bounds(w*0.4, h*0.35, w*0.42, h*0.65)
	look.Center = core.Point{X: core.Points(w * 0.62), Y: core.Points(h * 0.75)}
	x := w * 0.91
	radii := []float32{44, 32, 28, 28}
	y := h - 70
	for i, b := range pad.Buttons {
		b.Radius = radii[i]
		b.Center = core.Point{X: core.Points(x), Y: core.Points(y)}
		y -= 2*radii[i] + 16
	}
	move.Reset()
	look.Reset()
}

func bounds(x, y, w, h float32) core.Bounds {
	return core.Bounds{Origin: core.Point{X: core.Points(x), Y: core.Points(y)}, Size: core.Size{Width: core.Points(w), Height: core.Points(h)}}
}

// readControls turns this step's actions into the match's controls: the keys, the left stick and the move stick walk; locked mouse motion, the look keys, the right stick and the look stick turn the view, the analog ones at the player's turn speed.
func readControls(a *input.Actions, dt time.Duration) controls {
	turnStep := arena.Player.TurnSpeed * float32(dt.Seconds())
	lookY := clamp(float32(a.Axis(axisLookY)-a.Axis(axisTouchLookY)), -1, 1)
	c := controls{
		moveX:  float32(a.Axis(axisMoveX)),
		moveY:  clamp(float32(a.Axis(axisMoveY)-a.Axis(axisTouchMoveY)), -1, 1),
		turn:   -float32(a.Axis(axisLookX))*turnStep - float32(a.Delta(axisMouseX)),
		tilt:   lookY*turnStep - float32(a.Delta(axisMouseY)),
		jump:   a.Pressed(actionJump),
		fire:   a.Pressed(actionFire),
		reload: a.JustPressed(actionReload),
		next:   a.JustPressed(actionNext),
	}
	switch {
	case a.JustPressed(actionWeapon1):
		c.slot = 1
	case a.JustPressed(actionWeapon2):
		c.slot = 2
	}
	return c
}
