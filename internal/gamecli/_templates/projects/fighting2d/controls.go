package main

import (
	"github.com/nsgo-dev/game/pkg/fighting"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/nsgo/core"
)

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

// controls are the game's input devices, coordinated through input.Players so each connected game controller stays bound to its player slot across steps and screen changes, while both players share the keyboard on disjoint keys. The first player's slot also hears touch and the mouse: an on-screen stick and the touch regions game.yaml gives its buttons.
type controls struct {
	players *input.Players
	pad     *input.VirtualPad
	stick   *input.VirtualStick
	// touched is set by the first touch, which shows the touch controls on a device with a mouse too.
	touched bool
}

func newControls(cx *core.Scope, mapPoint func(core.Point) (core.Point, bool), policy input.DisconnectPolicy) *controls {
	c := &controls{players: input.NewPlayers(2)}
	for i := range 2 {
		Project.BindInput(c.players.Player(i).Actions())
		c.players.AssignKeyboard(i)
	}
	c.players.AssignPointer(0)
	c.players.Player(0).Actions().MapPoint = mapPoint
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
	c.players.Player(0).Actions().BindVirtualPad(c.pad)
	c.players.WatchKeys(cx)
	c.players.WatchGamepads(cx)
	c.players.SetDisconnectPolicy(policy)
	return c
}

// attach wires touch, the mouse and focused keys on v into Players and the on-screen stick.
func (c *controls) attach(v *core.View) {
	v.OnTouch = func(core.TouchEvent) { c.touched = true }
	c.players.Attach(v)
	c.pad.Attach(v)
}

// setDisconnectPolicy updates the controller disconnect and reconnect rules on Players.
func (c *controls) setDisconnectPolicy(policy input.DisconnectPolicy) {
	c.players.SetDisconnectPolicy(policy)
}

// assignFreePads polls live controllers, drops disconnected assignments when dropDisconnected is set, and assigns each unassigned connected controller in ID order to the first player slot that does not already have one.
func (c *controls) assignFreePads(dropDisconnected bool) {
	c.players.PollGamepads()
	if dropDisconnected {
		for i := range 2 {
			if c.players.Player(i).GamepadDisconnected() {
				c.players.UnassignGamepad(i)
			}
		}
	}
	for _, pad := range c.players.UnassignedGamepads() {
		for i := range 2 {
			if _, ok := c.players.Player(i).GamepadID(); !ok {
				c.players.AssignGamepad(i, pad.ID)
				break
			}
		}
	}
}

// step samples both players' input for this fixed step. When assignFree is true (the title screen), open slots claim newly connected controllers while existing assignments stay put; during a match each fighter keeps its assigned controller and DisconnectPolicy handles disconnects and recovery.
func (c *controls) step(assignFree bool) {
	if assignFree {
		c.assignFreePads(true)
	}
	c.players.Step()
}

// source returns player i's input as a fighting.Source: its own prefix of its own actions in versus, and both sides of the keyboard for a player alone.
func (c *controls) source(i int, split bool) fighting.Source {
	a := c.players.Player(i).Actions()
	own := prefixed{a, playerPrefix(i)}
	if split {
		return own
	}
	return either{own, prefixed{a, playerPrefix(1 - i)}}
}

// pressed reports whether player i holds action name on this step.
func (c *controls) pressed(i int, name string) bool {
	return c.players.Player(i).Pressed(name)
}

// justPressed reports whether either player's actions pressed name on this step.
func (c *controls) justPressed(name string) bool {
	return c.players.Player(0).JustPressed(name) || c.players.Player(1).JustPressed(name)
}

// playerJustPressed reports whether player i pressed its action name, such as "start", on this step.
func (c *controls) playerJustPressed(i int, name string) bool {
	return c.players.Player(i).JustPressed(playerPrefix(i) + name)
}

// playerDisconnected reports whether player i's assigned controller disconnected and is awaiting recovery.
func (c *controls) playerDisconnected(i int) bool {
	return c.players.Player(i).GamepadDisconnected()
}

// disconnected returns which players currently have a disconnected assigned controller.
func (c *controls) disconnected() [2]bool {
	return [2]bool{c.playerDisconnected(0), c.playerDisconnected(1)}
}

// pausedByDisconnect reports whether Players is currently requesting a disconnect pause.
func (c *controls) pausedByDisconnect() bool {
	return c.players.PausedByDisconnect()
}

// unassignGamepad detaches player i's controller assignment so player i continues on the keyboard.
func (c *controls) unassignGamepad(i int) {
	c.players.UnassignGamepad(i)
}

// unassignDisconnected detaches every currently disconnected controller assignment.
func (c *controls) unassignDisconnected() {
	for _, p := range c.players.DisconnectedPlayers() {
		c.players.UnassignGamepad(p.Index())
	}
}

// reset releases everything held, as the screen changes, so a key or controller button held into a new round does not act until it is pressed again, while keeping each player's assigned controller.
func (c *controls) reset() {
	c.players.Reset()
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
