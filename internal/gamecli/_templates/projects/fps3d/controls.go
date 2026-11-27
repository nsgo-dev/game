package main

import (
	"time"

	"github.com/nsgo-dev/game/pkg/input"
)

// Action and axis names, as game.yaml's input map and controls.touch.yaml declare them.
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

	// Locked mouse motion is bound in code because game.yaml has no pointer axes.
	axisMouseX = "mouseX"
	axisMouseY = "mouseY"
)

// newActions returns the game's actions: game.yaml's input map and mouselook while the pointer is locked.
func newActions() *input.Actions {
	a := input.NewActions()
	Project.BindInput(a)
	a.BindPointerAxis(axisMouseX, input.PointerX, input.PointerAxisOptions{Sensitivity: float64(arena.Player.MouseSensitivity)})
	a.BindPointerAxis(axisMouseY, input.PointerY, input.PointerAxisOptions{Sensitivity: float64(arena.Player.MouseSensitivity)})
	return a
}

// readControls turns this step's actions into the match's controls: the keys, the left stick and the touch move stick share moveX/moveY; locked mouse motion, the look keys, the right stick and the touch look stick share lookX/lookY.
func readControls(a *input.Actions, dt time.Duration) controls {
	turnStep := arena.Player.TurnSpeed * float32(dt.Seconds())
	c := controls{
		moveX:  float32(a.Axis(axisMoveX)),
		moveY:  float32(a.Axis(axisMoveY)),
		turn:   -float32(a.Axis(axisLookX))*turnStep - float32(a.Delta(axisMouseX)),
		tilt:   float32(a.Axis(axisLookY))*turnStep - float32(a.Delta(axisMouseY)),
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
