package input

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"time"

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
// Step reads the controllers' state as of the current display frame, so call Step from game.Loop.Update as usual. When Wake is set and at least one controller is connected in the foreground, a also watches connected controllers on display frames and calls Wake when a button press/release or bound stick axis changes so an idle OnDemand loop wakes to step. Reading stops once cx is disposed, and while cx's core.Access refuses core.AccessGamepad no controller is read. Passing nil stops reading and releases every controller input.
func (a *Actions) WatchGamepads(cx *core.Scope) {
	a.stopGamepadWatch()
	a.gpScope = cx
	if cx == nil {
		hadChange := a.gamepadWakeChanged(nil)
		a.gpPads = nil
		if a.syncRaw() || hadChange {
			a.wake()
		}
		return
	}
	a.gpWatchGen++
	gen := a.gpWatchGen
	stopEffect := cx.Effect(func() {
		bg := cx.Env().Lifecycle() == core.LifecycleBackground
		pads := gamepad.Pads(cx)
		cx.Untrack(func() {
			if a.gpWatchGen != gen {
				return
			}
			if bg || len(pads) == 0 {
				a.stopGamepadWakeRAF()
				if len(pads) == 0 && a.Wake != nil && a.gamepadWakeChanged(nil) {
					a.setGamepads(nil)
					a.syncRaw()
					a.wake()
				}
				return
			}
			if a.Wake != nil {
				a.unmuteReleasedButtonsFrom(pads)
				if a.gamepadWakeChanged(pads) {
					a.setGamepads(pads)
					a.syncRaw()
					a.wake()
				}
			}
			a.scheduleGamepadWakeRAF(cx, gen)
		})
	})
	a.gpWatchStop = stopEffect
	cx.Cleanup(func() {
		if a.gpWatchGen != gen {
			return
		}
		hadChange := a.gamepadWakeChanged(nil)
		a.stopGamepadWatch()
		a.gpScope = nil
		a.gpPads = a.gpPads[:0]
		if a.syncRaw() || hadChange {
			a.wake()
		}
	})
}

func (a *Actions) stopGamepadWatch() {
	a.gpWatchGen++
	a.stopGamepadWakeRAF()
	if stop := a.gpWatchStop; stop != nil {
		a.gpWatchStop = nil
		stop()
	}
}

func (a *Actions) stopGamepadWakeRAF() {
	if cancel := a.gpWakeCancel; cancel != nil {
		a.gpWakeCancel = nil
		cancel()
	}
}

func (a *Actions) scheduleGamepadWakeRAF(cx *core.Scope, gen uint64) {
	if a.gpWakeCancel != nil || cx == nil || cx.Disposed() || a.gpWatchGen != gen {
		return
	}
	cx.Run(func() {
		a.gpWakeCancel = cx.RequestAnimationFrame(func(time.Time) {
			a.pollGamepadWakeRAF(cx, gen)
		})
	})
}

func (a *Actions) pollGamepadWakeRAF(cx *core.Scope, gen uint64) {
	a.gpWakeCancel = nil
	if a.gpWatchGen != gen || a.gpScope != cx || cx == nil || cx.Disposed() {
		return
	}
	if cx.Env().Lifecycle() == core.LifecycleBackground {
		return
	}
	pads := core.Untracked(cx, func() []gamepad.Pad { return gamepad.Pads(cx) })
	if len(pads) == 0 {
		return
	}
	a.scheduleGamepadWakeRAF(cx, gen)
	if a.Wake == nil {
		return
	}
	a.unmuteReleasedButtonsFrom(pads)
	if a.gamepadWakeChanged(pads) {
		a.setGamepads(pads)
		a.syncRaw()
		a.wake()
	}
}

// OnGamepads replaces the controller state a reads with pads, such as recorded or synthetic controllers in a unit test or an input replay, and latches any button that became pressed so it registers on the next Step even if it is released before then.
//
// While WatchGamepads has a scope attached, the next Step replaces pads with the live controllers.
func (a *Actions) OnGamepads(pads []gamepad.Pad) {
	a.unmuteReleasedButtonsFrom(pads)
	changed := a.gamepadWakeChanged(pads)
	a.setGamepads(pads)
	if a.syncRaw() || changed {
		a.wake()
	}
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
	a.gpAxes[name] = gamepadAxisBinding{axis: axis, opts: finiteGamepadAxisOptions(opts)}
	delete(a.unboundGpAxes, name)
	delete(a.gpSnap, name)
}

// UnbindGamepadAxis removes the gamepad axis binding for name. Save records the unbinding so a later Load keeps name unbound rather than restoring a default.
func (a *Actions) UnbindGamepadAxis(name string) {
	delete(a.gpAxes, name)
	delete(a.gpSnap, name)
	if name != "" {
		if a.unboundGpAxes == nil {
			a.unboundGpAxes = make(map[string]bool)
		}
		a.unboundGpAxes[name] = true
	}
}

// GamepadAxisBinding returns the stick axis and options bound to name with BindGamepadAxis.
func (a *Actions) GamepadAxisBinding(name string) (gamepad.Axis, GamepadAxisOptions, bool) {
	b, ok := a.gpAxes[name]
	return b.axis, b.opts, ok
}

// finiteGamepadAxisOptions replaces non-finite options with the finite values gamepadAxisValue treats them as, so bindings in use can always be saved: a NaN dead zone is the default (0), +Inf reads 0 like any dead zone of 1 or more (1), -Inf is off (-1), and a non-finite sensitivity is 1 (0).
func finiteGamepadAxisOptions(opts GamepadAxisOptions) GamepadAxisOptions {
	switch {
	case math.IsNaN(opts.DeadZone):
		opts.DeadZone = 0
	case math.IsInf(opts.DeadZone, 1):
		opts.DeadZone = 1
	case math.IsInf(opts.DeadZone, -1):
		opts.DeadZone = -1
	}
	if math.IsNaN(opts.Sensitivity) || math.IsInf(opts.Sensitivity, 0) {
		opts.Sensitivity = 0
	}
	return opts
}

func (a *Actions) setGamepads(pads []gamepad.Pad) {
	if len(pads) == 0 {
		a.gpPads = a.gpPads[:0]
	} else {
		a.gpPads = append(a.gpPads[:0], pads...)
		slices.SortStableFunc(a.gpPads, func(x, y gamepad.Pad) int { return cmp.Compare(x.ID, y.ID) })
	}
	a.unmuteReleasedButtons()
}

// muteHeldButtons makes every button held on the controllers last read inactive until it is released, so a press held across Reset does not register as a new press, as a mouse button held across Reset does not.
func (a *Actions) muteHeldButtons() {
	for i := range a.gpPads {
		p := &a.gpPads[i]
		if m := pressedButtons(p); m != 0 {
			if a.gpMuted == nil {
				a.gpMuted = make(map[int]uint32)
			}
			a.gpMuted[p.ID] |= m
		}
	}
}

// unmuteReleasedButtons stops ignoring each muted button once it is released or its controller is disconnected.
func (a *Actions) unmuteReleasedButtons() {
	a.unmuteReleasedButtonsFrom(a.gpPads)
}

func (a *Actions) unmuteReleasedButtonsFrom(pads []gamepad.Pad) {
	for id, m := range a.gpMuted {
		var held uint32
		for i := range pads {
			if pads[i].ID == id {
				held = pressedButtons(&pads[i])
				break
			}
		}
		if m &= held; m == 0 {
			delete(a.gpMuted, id)
		} else {
			a.gpMuted[id] = m
		}
	}
}

func (a *Actions) gamepadWakeChanged(pads []gamepad.Pad) bool {
	if a.gamepadButtonsChanged(pads) {
		return true
	}
	for _, b := range a.gpAxes {
		if gamepadAxisForPads(pads, a.gpOne, a.gpID, b) != gamepadAxisForPads(a.gpPads, a.gpOne, a.gpID, b) {
			return true
		}
	}
	return false
}

func (a *Actions) gamepadButtonsChanged(pads []gamepad.Pad) bool {
	for i := range pads {
		p := &pads[i]
		if a.gpOne && p.ID != a.gpID {
			continue
		}
		now := pressedButtons(p) &^ a.gpMuted[p.ID]
		var prev uint32
		for j := range a.gpPads {
			if a.gpPads[j].ID == p.ID {
				prev = pressedButtons(&a.gpPads[j]) &^ a.gpMuted[p.ID]
				break
			}
		}
		if now != prev {
			return true
		}
	}
	for i := range a.gpPads {
		p := &a.gpPads[i]
		if a.gpOne && p.ID != a.gpID {
			continue
		}
		prev := pressedButtons(p) &^ a.gpMuted[p.ID]
		if prev == 0 {
			continue
		}
		found := false
		for j := range pads {
			if pads[j].ID == p.ID {
				found = true
				break
			}
		}
		if !found {
			return true
		}
	}
	return false
}

func gamepadAxisForPads(pads []gamepad.Pad, one bool, id int, b gamepadAxisBinding) float64 {
	var v float64
	for i := range pads {
		p := &pads[i]
		if one && p.ID != id {
			continue
		}
		if pv := gamepadAxisValue(p, b.axis, b.opts); math.Abs(pv) > math.Abs(v) {
			v = pv
		}
	}
	return v
}

// pressedButtons returns a bit mask of the buttons pressed on p, bit b for gamepad.Button b.
func pressedButtons(p *gamepad.Pad) uint32 {
	var m uint32
	for b := range gamepad.Button(gamepad.ButtonCount) {
		if p.Pressed(b) {
			m |= 1 << b
		}
	}
	return m
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
		if p.Pressed(b) && a.gpMuted[p.ID]&(1<<b) == 0 {
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
