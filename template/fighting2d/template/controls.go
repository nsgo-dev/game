package main

import (
	"fmt"
	"strings"
	"time"

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

// mirrorVersusLayout turns player 1's compacted versus layout on the left half into player 2's mirrored layout on the right half, bound to p2_'s actions and axes.
func mirrorVersusLayout(l input.TouchLayout) input.TouchLayout {
	out := l
	out.Mirrored = true
	out.Controls = make([]input.TouchControlSpec, len(l.Controls))
	for i, c := range l.Controls {
		c.ID = strings.Replace(c.ID, "p1-", "p2-", 1)
		c.Label = strings.Replace(c.Label, "P1", "P2", 1)
		c.Action = strings.Replace(c.Action, "p1_", "p2_", 1)
		c.AxisX = strings.Replace(c.AxisX, "p1_", "p2_", 1)
		c.AxisY = strings.Replace(c.AxisY, "p1_", "p2_", 1)
		out.Controls[i] = c
	}
	return out
}

// controls are the game's input devices, coordinated through input.Players so each connected game controller stays bound to its player slot across steps and screen changes, while both players share the keyboard on disjoint keys. Touch is driven by input.TouchControls inside the safe area: one full-width layout for player 1 against the CPU, or player 1 compacted to the left half and player 2 mirrored on the right half in local versus.
type controls struct {
	players *input.Players
	touch   [2]*input.TouchControls
	versus  bool
}

func newControls(cx *core.Scope, policy input.DisconnectPolicy) *controls {
	c := &controls{players: input.NewPlayers(2)}
	for i := range 2 {
		Project.BindInput(c.players.Player(i).Actions())
		c.players.AssignKeyboard(i)
	}
	c.players.AssignPointer(0)
	p1, err := input.NewTouchControls(cx, touchLayout)
	if err != nil {
		panic(fmt.Sprintf("fighting2d: controls.touch.yaml: %v", err))
	}
	p2, err := input.NewTouchControls(cx, mirrorVersusLayout(versusTouchLayout))
	if err != nil {
		panic(fmt.Sprintf("fighting2d: versus.touch.yaml: %v", err))
	}
	haptics := input.NewHaptics(cx)
	p1.Haptics = haptics
	p2.Haptics = haptics
	p1.BindActions(c.players.Player(0).Actions())
	c.touch = [2]*input.TouchControls{p1, p2}
	c.players.WatchKeys(cx)
	c.players.WatchGamepads(cx)
	c.players.SetDisconnectPolicy(policy)
	return c
}

// setVersus switches the touch controls between single-player layout (full width for player 1) and two-player local versus (player 1 compacted on the left half, player 2 mirrored on the right half and routed touches).
func (c *controls) setVersus(versus bool) {
	if c.versus == versus {
		return
	}
	c.versus = versus
	if versus {
		_ = c.touch[0].SetLayout(versusTouchLayout)
		c.players.AssignPointer(1)
		c.touch[1].BindActions(c.players.Player(1).Actions())
		c.touch[1].SetVisible(c.touch[0].Visible())
		c.touch[1].Wake()
	} else {
		_ = c.touch[0].SetLayout(touchLayout)
		c.touch[1].BindActions(nil)
		c.touch[1].Reset()
		c.players.UnassignPointer(1)
	}
	c.touch[0].Wake()
}

// attach wires touch, the mouse and focused keys on v into Players and both touch control layers.
func (c *controls) attach(v *core.View) {
	c.players.Attach(v)
	// After Players, so a touch shows the on-screen controls before they hear it.
	c.touch[0].Attach(v)
	c.touch[1].Attach(v)
}

// resize updates both touch control layers to the view's size and safe area.
func (c *controls) resize(size core.Size, safe core.Edges) {
	c.touch[0].Resize(size, safe)
	c.touch[1].Resize(size, safe)
}

// draw draws the active touch control layer(s) into d.
func (c *controls) draw(d *core.Drawing) {
	c.touch[0].Draw(d)
	if c.versus {
		c.touch[1].Draw(d)
	}
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

// step samples both players' input for this fixed step and advances the touch controls' idle fade. When assignFree is true (the title screen), open slots claim newly connected controllers while existing assignments stay put; during a match each fighter keeps its assigned controller and DisconnectPolicy handles disconnects and recovery.
func (c *controls) step(dt time.Duration, assignFree bool) {
	if assignFree {
		c.assignFreePads(true)
	}
	c.players.Step()
	c.touch[0].Update(dt)
	if c.versus {
		c.touch[1].Update(dt)
	}
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
	c.touch[0].Wake()
	if c.versus {
		c.touch[1].Wake()
	}
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
