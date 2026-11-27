package main

import (
	"math"

	"github.com/nsgo-dev/game/pkg/input"
)

// The actions and axes of game.yaml's input map. Each action is bound there to keys, gamepad buttons and an on-screen touch pad, so every device plays the same way; to rebind a control, edit game.yaml.
const (
	actionUp      = "up"
	actionDown    = "down"
	actionLeft    = "left"
	actionRight   = "right"
	actionConfirm = "confirm"
	actionCancel  = "cancel"
	actionMenu    = "menu"
	axisX         = "moveX"
	axisY         = "moveY"
)

// padActions are the actions with an on-screen pad, in the order they are drawn.
var padActions = []string{actionUp, actionDown, actionLeft, actionRight, actionConfirm, actionCancel}

// stickThreshold is how far an axis must lean for a direction to count.
const stickThreshold = 0.5

// readIntent reads one step's intent from a, which Step has just sampled; prev is the direction read the step before.
func readIntent(a *input.Actions, prev dir) intent {
	in := intent{
		confirm: a.JustPressed(actionConfirm),
		cancel:  a.JustPressed(actionCancel),
		menu:    a.JustPressed(actionMenu),
	}
	// moveX and moveY add the arrow keys, the D-pad and the touch pad, through their actions, to the gamepad's left stick.
	x, y := a.Axis(axisX), a.Axis(axisY)
	switch {
	case math.Abs(x) < stickThreshold && math.Abs(y) < stickThreshold:
	case math.Abs(y) >= math.Abs(x) && y < 0:
		in.dir = dirUp
	case math.Abs(y) >= math.Abs(x):
		in.dir = dirDown
	case x < 0:
		in.dir = dirLeft
	default:
		in.dir = dirRight
	}
	in.turned = in.dir != dirNone && in.dir != prev
	return in
}
