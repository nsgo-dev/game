package main

import (
	"cmp"
	"slices"

	"github.com/nsgo-dev/game/pkg/fighting"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
)

// noGamepad is a controller ID no controller has, for an Actions that should read none.
const noGamepad = -1

// prefixed reads one player's actions from an input.Actions whose action names start with the player's prefix, such as "p1_", so a move set names "light" and "left" for whichever player it plays.
type prefixed struct {
	a      *input.Actions
	prefix string
}

// Pressed implements fighting.Source.
func (s prefixed) Pressed(name string) bool { return s.a.Pressed(s.prefix + name) }

// Axis implements fighting.Source.
func (s prefixed) Axis(name string) float64 { return s.a.Axis(s.prefix + name) }

// either reads two sources as one, for a player alone against the CPU, who may use either side of the keyboard: an action is pressed when it is on either and an axis reads the one farther from center.
type either [2]fighting.Source

// Pressed implements fighting.Source.
func (e either) Pressed(name string) bool { return e[0].Pressed(name) || e[1].Pressed(name) }

// Axis implements fighting.Source.
func (e either) Axis(name string) float64 {
	a, b := e[0].Axis(name), e[1].Axis(name)
	if abs32(float32(b)) > abs32(float32(a)) {
		return b
	}
	return a
}

// controls are the game's input devices. Each player has an input.Actions with every binding of game.yaml's input map, so both hear the keyboard, and each reads its own game controller once two are connected. The first player's also hears touch and the mouse: an on-screen stick and the touch regions game.yaml gives its buttons.
type controls struct {
	// cx is the scope the controllers are read in.
	cx      *core.Scope
	actions [2]*input.Actions
	pad     *input.VirtualPad
	stick   *input.VirtualStick
	// touched is set by the first touch, which shows the touch controls on a device with a mouse too.
	touched bool
}

func newControls(cx *core.Scope, mapPoint func(core.Point) (core.Point, bool)) *controls {
	c := &controls{cx: cx}
	for i := range c.actions {
		a := input.NewActions()
		Project.BindInput(a)
		a.WatchKeys(cx)
		a.WatchGamepads(cx)
		c.actions[i] = a
	}
	c.actions[0].MapPoint = mapPoint
	c.pad = input.NewVirtualPad()
	c.pad.MapPoint = mapPoint
	c.stick = c.pad.AddStick(&input.VirtualStick{
		AxisXName: "p1_move_x",
		AxisYName: "p1_move_y",
		Zone:      core.Bounds{Origin: core.Point{Y: 110}, Size: core.Size{Width: 240, Height: 160}},
		Center:    core.Point{X: 70, Y: 206},
		Radius:    34,
		DeadZone:  0.25,
		Floating:  true,
	})
	c.actions[0].BindVirtualPad(c.pad)
	return c
}

// attach wires touch, the mouse and focused keys on v into the first player's actions and the on-screen stick.
func (c *controls) attach(v *core.View) {
	v.OnTouch = func(core.TouchEvent) { c.touched = true }
	c.actions[0].Attach(v)
	c.pad.Attach(v)
}

// step samples both players' input for this fixed step. With two sides of the keyboard in play (versus, and the title screen where a second player joins) the connected controllers go to the players in ID order; against the CPU the player reads every controller.
//
// The controllers are read here rather than from Actions.Gamepads, which holds the previous step's poll and is empty after reset, so the first step on a new screen already gives each player its own controller.
func (c *controls) step(split bool) {
	pads := c.gamepads()
	switch {
	case !split:
		c.actions[0].SelectAnyGamepad()
		c.actions[1].SelectGamepad(noGamepad)
	case len(pads) >= 2:
		c.actions[0].SelectGamepad(pads[0].ID)
		c.actions[1].SelectGamepad(pads[1].ID)
	case len(pads) == 1:
		c.actions[0].SelectGamepad(pads[0].ID)
		c.actions[1].SelectGamepad(noGamepad)
	default:
		c.actions[0].SelectAnyGamepad()
		c.actions[1].SelectGamepad(noGamepad)
	}
	for _, a := range c.actions {
		a.Step()
	}
}

// gamepads returns the connected controllers sorted by ID, read untracked as Actions.Step reads them so the caller is not subscribed to connections.
func (c *controls) gamepads() []gamepad.Pad {
	if c.cx == nil || c.cx.Disposed() {
		return nil
	}
	pads := core.Untracked(c.cx, func() []gamepad.Pad { return gamepad.Pads(c.cx) })
	return slices.SortedStableFunc(slices.Values(pads), func(x, y gamepad.Pad) int { return cmp.Compare(x.ID, y.ID) })
}

// source returns player i's input as a fighting.Source: its own prefix of its own actions in versus, and both sides of the keyboard for a player alone.
func (c *controls) source(i int, split bool) fighting.Source {
	own := prefixed{c.actions[i], playerPrefix(i)}
	if split {
		return own
	}
	return either{own, prefixed{c.actions[i], playerPrefix(1 - i)}}
}

// justPressed reports whether either player's actions pressed name on this step.
func (c *controls) justPressed(name string) bool {
	return c.actions[0].JustPressed(name) || c.actions[1].JustPressed(name)
}

// playerJustPressed reports whether player i pressed its action name, such as "start", on this step.
func (c *controls) playerJustPressed(i int, name string) bool {
	return c.actions[i].JustPressed(playerPrefix(i) + name)
}

// reset releases everything held, as the screen changes, so a key held into a new round does not act until it is pressed again.
func (c *controls) reset() {
	for _, a := range c.actions {
		a.Reset()
	}
	c.pad.Reset()
}

func playerPrefix(i int) string {
	if i == 0 {
		return "p1_"
	}
	return "p2_"
}

// read samples a source as a control for p.
func read(p *player, src fighting.Source) control {
	return control{dir: p.rec.Direction(src), held: p.rec.Held(src)}
}
