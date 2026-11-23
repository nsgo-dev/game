package input

import (
	"fmt"
	"math"
	"slices"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
)

// DefaultGamepadDeadZone is the stick dead zone GamepadAxisOptions uses when its DeadZone is 0: stick deflections up to this magnitude read 0.
const DefaultGamepadDeadZone = 0.15

// GamepadButton returns an input that is active while button b is pressed (value at least 0.5, as gamepad.Pad.Pressed tests) on a connected game controller that a uses (see SelectGamepad). A button outside the W3C Standard Gamepad layout is never active.
//
// Bind it with Actions.Bind like any other Input, and feed controller state to the Actions with WatchGamepads or OnGamepads.
func GamepadButton(b gamepad.Button) Input {
	return Input{Kind: InputGamepadButton, PadButton: b}
}

// GamepadAxisOptions configures how one bound stick axis is mapped into [-1, 1].
type GamepadAxisOptions struct {
	// DeadZone is the stick deflection in [0, 1) below which the axis reads 0; readings beyond it are rescaled so the axis still rises smoothly from 0 to 1. Zero uses DefaultGamepadDeadZone, and a negative value turns the dead zone off.
	DeadZone float64
	// Axial applies DeadZone to this axis alone instead of to the whole stick. By default the dead zone is radial: it measures the stick's distance from center over both of its axes, so a diagonal push is not snapped to a straight line.
	Axial bool
	// Sensitivity multiplies the dead-zoned value before clamping to [-1, 1] (1 when <= 0).
	Sensitivity float64
	// Invert negates the resulting axis value, such as to make pushing the stick up read positive on AxisLeftY.
	Invert bool
}

type gamepadAxisBinding struct {
	axis gamepad.Axis
	opts GamepadAxisOptions
}

// WatchGamepads makes a read the game controllers connected to cx's app on every Step with gamepad.Pads, so GamepadButton inputs and BindGamepadAxis axes follow the physical controllers on every target that supports them.
//
// Step reads the controllers' state as of the current display frame, so call Step from game.Loop.Update as usual. A press and release that both happen between two Step calls are not seen. Reading stops once cx is disposed, and while cx's core.Access refuses core.AccessGamepad no controller is read. Passing nil stops reading and releases every controller input.
func (a *Actions) WatchGamepads(cx *core.Scope) {
	a.gpScope = cx
	if cx == nil {
		a.gpPads = nil
		a.syncRaw()
	}
}

// OnGamepads replaces the controller state a reads with pads, such as recorded or synthetic controllers in a unit test or an input replay, and latches any button that became pressed so it registers on the next Step even if it is released before then.
//
// While WatchGamepads has a scope attached, the next Step replaces pads with the live controllers.
func (a *Actions) OnGamepads(pads []gamepad.Pad) {
	a.setGamepads(pads)
	a.syncRaw()
}

// Gamepads returns a copy of the controller state a last read, sorted by controller ID.
func (a *Actions) Gamepads() []gamepad.Pad {
	return slices.Clone(a.gpPads)
}

// SelectGamepad makes a read only the controller with ID id, such as one player's controller in a local multiplayer game with one Actions per player. By default a reads every connected controller.
func (a *Actions) SelectGamepad(id int) {
	a.gpID = id
	a.gpOne = true
	a.syncRaw()
}

// SelectAnyGamepad makes a read every connected controller again: a button is pressed when it is pressed on any of them, and a stick axis takes the reading farthest from center.
func (a *Actions) SelectAnyGamepad() {
	a.gpID = 0
	a.gpOne = false
	a.syncRaw()
}

// SelectedGamepad returns the controller ID chosen with SelectGamepad, and false when a reads every controller.
func (a *Actions) SelectedGamepad() (id int, ok bool) {
	return a.gpID, a.gpOne
}

// BindGamepadAxis binds axis name to a stick axis of the game controllers a reads, mapped into [-1, 1] with opts' dead zone and sensitivity. An axis outside the W3C Standard Gamepad layout unbinds the gamepad axis for name.
//
// A gamepad axis may share a name with an axis bound with BindAxis, BindMotionAxis or a VirtualPad stick; Actions.Axis sums all of them and clamps the result to [-1, 1].
func (a *Actions) BindGamepadAxis(name string, axis gamepad.Axis, opts GamepadAxisOptions) {
	if int(axis) >= gamepad.AxisCount {
		a.UnbindGamepadAxis(name)
		return
	}
	if a.gpAxes == nil {
		a.gpAxes = make(map[string]gamepadAxisBinding)
	}
	a.gpAxes[name] = gamepadAxisBinding{axis: axis, opts: opts}
	delete(a.gpSnap, name)
}

// UnbindGamepadAxis removes the gamepad axis binding for name.
func (a *Actions) UnbindGamepadAxis(name string) {
	delete(a.gpAxes, name)
	delete(a.gpSnap, name)
}

// GamepadAxisBinding returns the stick axis and options bound to name with BindGamepadAxis.
func (a *Actions) GamepadAxisBinding(name string) (gamepad.Axis, GamepadAxisOptions, bool) {
	b, ok := a.gpAxes[name]
	return b.axis, b.opts, ok
}

func (a *Actions) setGamepads(pads []gamepad.Pad) {
	if len(pads) == 0 {
		a.gpPads = a.gpPads[:0]
		return
	}
	a.gpPads = append(a.gpPads[:0], pads...)
	slices.SortStableFunc(a.gpPads, func(x, y gamepad.Pad) int { return x.ID - y.ID })
}

// pollGamepads reads the live controllers from the attached scope at the start of Step.
func (a *Actions) pollGamepads() {
	cx := a.gpScope
	if cx == nil {
		return
	}
	if cx.Disposed() {
		a.gpScope = nil
		a.gpPads = a.gpPads[:0]
		return
	}
	// Pads is a tracked read; Step must not subscribe whatever computation calls it to controller connections.
	a.setGamepads(core.Untracked(cx, func() []gamepad.Pad { return gamepad.Pads(cx) }))
}

func (a *Actions) gamepadButtonDown(b gamepad.Button) bool {
	if int(b) >= gamepad.ButtonCount {
		return false
	}
	for i := range a.gpPads {
		p := &a.gpPads[i]
		if a.gpOne && p.ID != a.gpID {
			continue
		}
		if p.Pressed(b) {
			return true
		}
	}
	return false
}

func (a *Actions) stepGamepadAxes() {
	if len(a.gpAxes) == 0 {
		clear(a.gpSnap)
		return
	}
	if a.gpSnap == nil {
		a.gpSnap = make(map[string]float64, len(a.gpAxes))
	}
	for name, b := range a.gpAxes {
		var v float64
		for i := range a.gpPads {
			p := &a.gpPads[i]
			if a.gpOne && p.ID != a.gpID {
				continue
			}
			if pv := gamepadAxisValue(p, b.axis, b.opts); math.Abs(pv) > math.Abs(v) {
				v = pv
			}
		}
		a.gpSnap[name] = v
	}
}

// gamepadAxisValue maps one stick axis of p into [-1, 1] with opts' dead zone, sensitivity and inversion.
func gamepadAxisValue(p *gamepad.Pad, axis gamepad.Axis, opts GamepadAxisOptions) float64 {
	if int(axis) >= gamepad.AxisCount {
		return 0
	}
	dz := opts.DeadZone
	switch {
	case math.IsNaN(dz):
		dz = DefaultGamepadDeadZone
	case dz == 0:
		dz = DefaultGamepadDeadZone
	case dz < 0:
		dz = 0
	}
	if dz >= 1 {
		return 0
	}
	v := finiteAxis(p.Axes[axis])
	if opts.Axial {
		v = axialDeadZone(v, dz)
	} else {
		// Standard layout pairs axes 0/1 (left stick) and 2/3 (right stick).
		base := axis &^ 1
		x, y := finiteAxis(p.Axes[base]), finiteAxis(p.Axes[base+1])
		v = radialDeadZone(v, math.Hypot(x, y), dz)
	}
	sens := opts.Sensitivity
	if sens <= 0 || math.IsNaN(sens) || math.IsInf(sens, 0) {
		sens = 1
	}
	v *= sens
	if opts.Invert {
		v = -v
	}
	return clampUnit(v)
}

func finiteAxis(v float32) float64 {
	f := float64(v)
	if math.IsNaN(f) {
		return 0
	}
	return clampUnit(f)
}

func axialDeadZone(v, dz float64) float64 {
	abs := math.Abs(v)
	if abs <= dz {
		return 0
	}
	scaled := (abs - dz) / (1 - dz)
	if v < 0 {
		return -scaled
	}
	return scaled
}

// radialDeadZone rescales component v of a stick whose distance from center is mag so the stick's magnitude rises from 0 at dz to 1 at full deflection while its direction is kept.
func radialDeadZone(v, mag, dz float64) float64 {
	if mag <= dz || mag == 0 {
		return 0
	}
	m := mag
	if m > 1 {
		m = 1
	}
	return v / mag * (m - dz) / (1 - dz)
}

var gamepadButtonNames = [gamepad.ButtonCount]string{
	gamepad.ButtonSouth:      "south",
	gamepad.ButtonEast:       "east",
	gamepad.ButtonWest:       "west",
	gamepad.ButtonNorth:      "north",
	gamepad.ButtonLB:         "left_shoulder",
	gamepad.ButtonRB:         "right_shoulder",
	gamepad.ButtonLT:         "left_trigger",
	gamepad.ButtonRT:         "right_trigger",
	gamepad.ButtonBack:       "back",
	gamepad.ButtonStart:      "start",
	gamepad.ButtonLeftStick:  "left_stick",
	gamepad.ButtonRightStick: "right_stick",
	gamepad.ButtonDPadUp:     "dpad_up",
	gamepad.ButtonDPadDown:   "dpad_down",
	gamepad.ButtonDPadLeft:   "dpad_left",
	gamepad.ButtonDPadRight:  "dpad_right",
	gamepad.ButtonHome:       "home",
}

var gamepadAxisNames = [gamepad.AxisCount]string{
	gamepad.AxisLeftX:  "left_x",
	gamepad.AxisLeftY:  "left_y",
	gamepad.AxisRightX: "right_x",
	gamepad.AxisRightY: "right_y",
}

func encodeGamepadButton(b gamepad.Button) (string, error) {
	if int(b) >= gamepad.ButtonCount {
		return "", fmt.Errorf("unknown gamepad button %d", b)
	}
	return gamepadButtonNames[b], nil
}

func decodeGamepadButton(s string) (gamepad.Button, error) {
	if i := slices.Index(gamepadButtonNames[:], s); i >= 0 {
		return gamepad.Button(i), nil
	}
	return 0, fmt.Errorf("unknown gamepad button %q", s)
}

func encodeGamepadAxis(ax gamepad.Axis) (string, error) {
	if int(ax) >= gamepad.AxisCount {
		return "", fmt.Errorf("unknown gamepad axis %d", ax)
	}
	return gamepadAxisNames[ax], nil
}

func decodeGamepadAxis(s string) (gamepad.Axis, error) {
	if i := slices.Index(gamepadAxisNames[:], s); i >= 0 {
		return gamepad.Axis(i), nil
	}
	return 0, fmt.Errorf("unknown gamepad axis %q", s)
}
