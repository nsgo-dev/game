package editor

import (
	"fmt"
	"math"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// Navigation3DInput names a 3D viewport mouse gesture resolved by Navigation3D.Resolve.
type Navigation3DInput int

const (
	// Navigation3DInputPrimaryDrag is a primary (left) mouse button drag on the 3D viewport.
	Navigation3DInputPrimaryDrag Navigation3DInput = iota
	// Navigation3DInputMiddleDrag is a middle mouse button drag on the 3D viewport.
	Navigation3DInputMiddleDrag
	// Navigation3DInputRightDrag is a secondary (right) mouse button drag on the 3D viewport.
	Navigation3DInputRightDrag
	// Navigation3DInputWheel is a mouse wheel turn (distinct from a touchpad scroll).
	Navigation3DInputWheel
)

// Navigation3DAction is the camera action chosen for a 3D viewport mouse gesture.
type Navigation3DAction int

const (
	// Navigation3DActionNone leaves the gesture to viewport selection, tools, and gizmos.
	Navigation3DActionNone Navigation3DAction = iota
	// Navigation3DActionOrbit orbits the 3D camera around its target.
	Navigation3DActionOrbit
	// Navigation3DActionPan pans the 3D camera parallel to the view plane.
	Navigation3DActionPan
	// Navigation3DActionFly enters first-person fly mode.
	Navigation3DActionFly
	// Navigation3DActionDolly zooms the 3D camera toward or away from its target.
	Navigation3DActionDolly
)

// String returns the action's name.
func (a Navigation3DAction) String() string {
	switch a {
	case Navigation3DActionNone:
		return "none"
	case Navigation3DActionOrbit:
		return "orbit"
	case Navigation3DActionPan:
		return "pan"
	case Navigation3DActionFly:
		return "fly"
	case Navigation3DActionDolly:
		return "dolly"
	default:
		return "unknown"
	}
}

// Resolve maps a 3D viewport mouse gesture and its held modifiers to the camera action
// for navigation mode n (an empty or unrecognized value behaves as Navigation3DDefault):
//   - Default: left-drag orbits, Shift+left-drag or middle-drag pans, right-drag flies, wheel dollies.
//   - Blender: middle-drag orbits, Shift+middle-drag pans, right-drag flies, wheel dollies; left-drag returns Navigation3DActionNone (gizmo/select).
//   - Maya: Alt+left-drag orbits, middle-drag (with or without Alt) pans, Alt+right-drag or wheel dollies, plain right-drag flies; plain left-drag returns Navigation3DActionNone (gizmo/select).
func (n Navigation3D) Resolve(input Navigation3DInput, mods core.Modifiers) Navigation3DAction {
	switch input {
	case Navigation3DInputWheel:
		return Navigation3DActionDolly
	case Navigation3DInputRightDrag:
		if n == Navigation3DMaya && mods.Alt {
			return Navigation3DActionDolly
		}
		return Navigation3DActionFly
	case Navigation3DInputMiddleDrag:
		if n == Navigation3DBlender {
			if mods.Shift {
				return Navigation3DActionPan
			}
			return Navigation3DActionOrbit
		}
		return Navigation3DActionPan
	case Navigation3DInputPrimaryDrag:
		switch n {
		case Navigation3DBlender:
			return Navigation3DActionNone
		case Navigation3DMaya:
			if mods.Alt {
				return Navigation3DActionOrbit
			}
			return Navigation3DActionNone
		default:
			if mods.Shift {
				return Navigation3DActionPan
			}
			return Navigation3DActionOrbit
		}
	default:
		return Navigation3DActionNone
	}
}

// View3D names one of the six axis-aligned views of the 3D viewport, each looking at Target from one end of a world axis.
type View3D int

const (
	// View3DFront looks from +Z toward -Z.
	View3DFront View3D = iota
	// View3DBack looks from -Z toward +Z.
	View3DBack
	// View3DRight looks from +X toward -X.
	View3DRight
	// View3DLeft looks from -X toward +X.
	View3DLeft
	// View3DTop looks straight down from +Y, with -Z at the top of the view.
	View3DTop
	// View3DBottom looks straight up from -Y, with +Z at the top of the view.
	View3DBottom
)

// views3D are the axis views in the order View3D numbers them.
var views3D = [...]View3D{View3DFront, View3DBack, View3DRight, View3DLeft, View3DTop, View3DBottom}

// String returns the view's name: Front, Back, Right, Left, Top or Bottom.
func (v View3D) String() string {
	switch v {
	case View3DFront:
		return "Front"
	case View3DBack:
		return "Back"
	case View3DRight:
		return "Right"
	case View3DLeft:
		return "Left"
	case View3DTop:
		return "Top"
	case View3DBottom:
		return "Bottom"
	default:
		return "Unknown"
	}
}

// angles returns the yaw and pitch of the view, and false for a value that names no view.
func (v View3D) angles() (yaw, pitch float32, ok bool) {
	switch v {
	case View3DFront:
		return 0, 0, true
	case View3DBack:
		return math.Pi, 0, true
	case View3DRight:
		return math.Pi / 2, 0, true
	case View3DLeft:
		return -math.Pi / 2, 0, true
	case View3DTop:
		return 0, math.Pi / 2, true
	case View3DBottom:
		return 0, -math.Pi / 2, true
	default:
		return 0, 0, false
	}
}

// SetView turns the camera to look at Target along the axis view v, keeping Target, Distance and the projection. A value that names no view changes nothing.
func (c *OrbitCamera3D) SetView(v View3D) {
	if c == nil {
		return
	}
	if yaw, pitch, ok := v.angles(); ok {
		c.Yaw, c.Pitch = yaw, pitch
	}
}

// AxisView returns the axis view the camera looks along, and false when it looks along none, as after an orbit.
func (c OrbitCamera3D) AxisView() (View3D, bool) {
	const eps = 1e-4
	yaw := math.Mod(float64(c.Yaw), 2*math.Pi)
	if yaw < 0 {
		yaw += 2 * math.Pi
	}
	pitch := float64(c.Pitch)
	for _, v := range views3D {
		vy, vp, _ := v.angles()
		want := math.Mod(float64(vy)+2*math.Pi, 2*math.Pi)
		dy := math.Abs(yaw - want)
		dy = math.Min(dy, 2*math.Pi-dy)
		if math.Abs(pitch-float64(vp)) > eps {
			continue
		}
		// Looking straight down or up, the yaw only turns the view about its center, so the top and bottom views are those at a yaw of 0.
		if dy <= eps {
			return v, true
		}
	}
	return 0, false
}

// Look turns the camera about its eye, as a first-person camera turns its head: dyaw turns it left (positive) or right and dpitch tilts it down (positive) or up, in radians; a turn that is not finite changes nothing. Target moves to stay Distance in front of the eye, and the tilt stops short of straight up or down, as Orbit's does.
func (c *OrbitCamera3D) Look(dyaw, dpitch float32) {
	if c == nil || !isFiniteFloat32(dyaw) || !isFiniteFloat32(dpitch) {
		return
	}
	eye := c.eye64()
	c.Yaw += dyaw
	c.Pitch += dpitch
	c.Pitch = float32(clamp64(float64(c.Pitch), -float64(maxOrbitPitch), float64(maxOrbitPitch)))
	_, _, forward := c.basis64()
	c.Target = vec32(add64(eye, scale64(forward, c.eyeDistance64())))
}

// Fly moves the camera, eye and Target together, by right along the view's right direction, up along the world's up axis and forward along the direction the view looks. A move that is not finite changes nothing.
func (c *OrbitCamera3D) Fly(right, up, forward float32) {
	if c == nil || !isFiniteFloat32(right) || !isFiniteFloat32(up) || !isFiniteFloat32(forward) {
		return
	}
	r, _, f := c.basis64()
	move := add64(add64(scale64(r, float64(right)), [3]float64{0, float64(up), 0}), scale64(f, float64(forward)))
	c.Target = vec32(add64(vec64(c.Target), move))
}

const (
	// orbitStep3D is how far the Orbit buttons beside the axis widget turn the view: 15 degrees.
	orbitStep3D float32 = math.Pi / 12
	// lookPerPixel3D is how far a pointer's movement of one point turns the view while flying, in radians.
	lookPerPixel3D float32 = 0.004
	// minFlySpeed3D and maxFlySpeed3D bound the fly speed (FlySpeed3D) in world units a second.
	minFlySpeed3D float32 = 0.1
	maxFlySpeed3D float32 = 100000
	// flyBoost3D is how many times faster the camera flies with Shift held.
	flyBoost3D float32 = 4
)

// SetView3D turns the 3D viewport's camera to the axis view v, as the axis widget, the number-key shortcuts and the View commands do, keeping what it looks at and how far away. It ends a flight and cancels a gizmo or camera drag, whose rays came from the old camera. It does not change the projection; with Orthographic3D the view is then a true elevation or plan. The camera jumps there without animating.
func (e *Editor) SetView3D(v View3D) {
	if e == nil {
		return
	}
	if _, _, ok := v.angles(); !ok {
		return
	}
	e.endCameraGestures3D()
	e.lookThroughCamera3D = false
	e.Camera3D.SetView(v)
	e.Status = "View: " + e.ViewName3D()
	e.notify()
}

// SetOrthographic3D switches the 3D viewport's camera between perspective and orthographic (OrbitCamera3D.Orthographic), keeping what lies at its Target the same size. It ends a flight and cancels a gizmo or camera drag.
func (e *Editor) SetOrthographic3D(on bool) {
	if e == nil {
		return
	}
	if e.fly3D && !on {
		// A flight already looks in perspective; asking for it keeps it after the flight ends.
		e.fly3DOrtho = false
	}
	if e.Camera3D.Orthographic == on && !e.lookThroughCamera3D {
		return
	}
	e.endCameraGestures3D()
	e.lookThroughCamera3D = false
	e.Camera3D.Orthographic = on
	e.Status = "View: " + e.ViewName3D()
	e.notify()
}

// Orthographic3D reports whether the 3D viewport's camera projects in orthographic.
func (e *Editor) Orthographic3D() bool {
	return e != nil && e.Camera3D.Orthographic
}

// OrbitView3D orbits the 3D viewport's camera about its Target by dyaw and dpitch radians, as a drag does; the viewport's Orbit buttons turn it 15 degrees left or right. Looking straight down or up from an axis view, a tilt first brings the view back within Orbit's range. A turn that is not finite changes nothing.
func (e *Editor) OrbitView3D(dyaw, dpitch float32) {
	if e == nil || !isFiniteFloat32(dyaw) || !isFiniteFloat32(dpitch) || (dyaw == 0 && dpitch == 0) {
		return
	}
	e.lookThroughCamera3D = false
	e.Camera3D.Orbit(dyaw, dpitch)
	e.notify()
}

// ResetView3D puts the 3D viewport's camera back to DefaultOrbitCamera3D, looking at the world origin, keeping the projection. It ends a flight and cancels a gizmo or camera drag.
func (e *Editor) ResetView3D() {
	if e == nil {
		return
	}
	e.endCameraGestures3D()
	e.lookThroughCamera3D = false
	ortho := e.Camera3D.Orthographic
	e.Camera3D = DefaultOrbitCamera3D()
	e.Camera3D.Orthographic = ortho
	e.Status = "View: " + e.ViewName3D()
	e.notify()
}

// PanView3D moves the 3D viewport's camera parallel to the view so the scene follows the pointer: dx and dy are how far it moved, in points, right and down, on a viewport h points high. What lies at Target moves by exactly that much, in perspective and in orthographic. A move that is not finite changes nothing.
func (e *Editor) PanView3D(dx, dy, h float32) {
	if e == nil || !isFiniteFloat32(dx) || !isFiniteFloat32(dy) || (dx == 0 && dy == 0) {
		return
	}
	e.lookThroughCamera3D = false
	per := float32(e.Camera3D.worldPerPixel(h))
	e.Camera3D.Pan(-dx*per, dy*per)
	e.notify()
}

// ZoomView3D zooms the 3D viewport's camera by factor toward its Target (OrbitCamera3D.ZoomBy): above 1 zooms in, below 1 out, as a pinch does.
func (e *Editor) ZoomView3D(factor float32) {
	if e == nil || factor == 1 {
		return
	}
	e.lookThroughCamera3D = false
	e.Camera3D.ZoomBy(factor)
	e.notify()
}

// ViewName3D names the 3D viewport's view as its bar and the status bar show it: the active scene camera when looking through it, the axis view's name (Front, Top, ...) or Perspective, with " · ortho" in orthographic, where a view along no axis is "User · ortho".
func (e *Editor) ViewName3D() string {
	if e == nil {
		return ""
	}
	if name, ok := e.cameraViewName3D(); ok {
		return name
	}
	v, axis := e.Camera3D.AxisView()
	switch {
	case axis && e.Camera3D.Orthographic:
		return v.String() + " · ortho"
	case axis:
		return v.String()
	case e.Camera3D.Orthographic:
		return "User · ortho"
	default:
		return "Perspective"
	}
}

// viewAngles3D returns the camera's yaw, from 0 to 359, and pitch in whole degrees, as the status bar shows them.
func (e *Editor) viewAngles3D() (yaw, pitch int) {
	y := int(math.Round(float64(e.Camera3D.Yaw) * 180 / math.Pi))
	y %= 360
	if y < 0 {
		y += 360
	}
	return y, int(math.Round(float64(e.Camera3D.Pitch) * 180 / math.Pi))
}

// viewStatus3D is the status bar's description of the 3D view: its name, yaw and pitch.
func (e *Editor) viewStatus3D() string {
	yaw, pitch := e.viewAngles3D()
	return fmt.Sprintf("%s · yaw %d° · pitch %d°", e.ViewName3D(), yaw, pitch)
}

// BeginFly3D starts flying the 3D viewport's camera, as holding the right mouse button does: FlyLook3D then turns it about its eye and FlyMove3D moves it, until EndFly3D. Flying looks in perspective, so an orthographic camera switches to perspective, and back when the flight ends without having turned or moved it; and the first flight without a FlySpeed3D sets it to the camera's Distance a second. It reports whether it started: not while playing, and not during a gizmo or camera drag.
func (e *Editor) BeginFly3D() bool {
	if e == nil || e.playing || e.drag3D.kind != drag3DNone {
		return false
	}
	if e.fly3D {
		return true
	}
	e.lookThroughCamera3D = false
	e.fly3D = true
	e.fly3DOrtho, e.fly3DMoved = e.Camera3D.Orthographic, false
	e.Camera3D.Orthographic = false
	if e.FlySpeed3D <= 0 {
		e.FlySpeed3D = clampFlySpeed3D(max(e.Camera3D.Distance, 1))
	}
	e.notify()
	return true
}

// Flying3D reports whether the 3D viewport's camera is flying (BeginFly3D).
func (e *Editor) Flying3D() bool {
	return e != nil && e.fly3D
}

// FlyLook3D turns the flying camera about its eye by a pointer's movement of dx and dy points right and down (OrbitCamera3D.Look). It does nothing unless flying, or for a movement that is not finite.
func (e *Editor) FlyLook3D(dx, dy float32) {
	if e == nil || !e.fly3D || !isFiniteFloat32(dx) || !isFiniteFloat32(dy) || (dx == 0 && dy == 0) {
		return
	}
	e.fly3DMoved = true
	e.Camera3D.Look(-dx*lookPerPixel3D, dy*lookPerPixel3D)
	e.notify()
}

// FlyMove3D moves the flying camera for dt at FlySpeed3D, flyBoost3D times as fast when fast is set: right, up and forward are each -1, 0 or 1, as the held D and A, E and Q, and W and S keys say, and a diagonal moves no faster than a straight line. It does nothing unless flying, or for a direction that is not finite.
func (e *Editor) FlyMove3D(right, up, forward float32, fast bool, dt time.Duration) {
	if e == nil || !e.fly3D || dt <= 0 || !isFiniteFloat32(right) || !isFiniteFloat32(up) || !isFiniteFloat32(forward) {
		return
	}
	n := float32(math.Sqrt(float64(right*right + up*up + forward*forward)))
	if n == 0 {
		return
	}
	step := clampFlySpeed3D(e.FlySpeed3D) * float32(dt.Seconds()) / n
	if fast {
		step *= flyBoost3D
	}
	e.fly3DMoved = true
	e.Camera3D.Fly(right*step, up*step, forward*step)
	e.notify()
}

// AdjustFlySpeed3D changes the fly speed by a wheel's turn while flying: each 200 points of deltaY, down, halves it and, up, doubles it, within 0.1 to 100000 world units a second.
func (e *Editor) AdjustFlySpeed3D(deltaY float32) {
	if e == nil || deltaY == 0 || !isFiniteFloat32(deltaY) {
		return
	}
	speed := e.FlySpeed3D
	if speed <= 0 {
		speed = max(e.Camera3D.Distance, 1)
	}
	e.FlySpeed3D = clampFlySpeed3D(speed * float32(math.Exp2(float64(-deltaY)/200)))
	e.Status = fmt.Sprintf("Fly speed: %.3g", e.FlySpeed3D)
	e.notify()
}

// EndFly3D stops flying (BeginFly3D); the camera stays where it flew to. A flight that neither turned nor moved the camera, such as a plain right-click, puts back the orthographic projection it started from.
func (e *Editor) EndFly3D() {
	if e == nil || !e.fly3D {
		return
	}
	e.endFly3D()
	e.notify()
}

// endFly3D stops a flight as EndFly3D does without telling the listeners, for a change that tells them itself.
func (e *Editor) endFly3D() {
	if e == nil || !e.fly3D {
		return
	}
	e.fly3D = false
	if e.fly3DOrtho && !e.fly3DMoved {
		e.Camera3D.Orthographic = true
	}
	e.fly3DOrtho, e.fly3DMoved = false, false
}

// endCameraGestures3D ends a flight and cancels a gizmo or camera drag before the camera jumps to another view, so no gesture mixes rays from two cameras.
func (e *Editor) endCameraGestures3D() {
	e.endFly3D()
	e.CancelGizmoDrag3D()
	e.CancelComponentGizmoDrag3D()
	e.CancelPluginDrag3D()
	e.drag3D = drag3DState{}
}

func clampFlySpeed3D(v float32) float32 {
	if math.IsNaN(float64(v)) || v < minFlySpeed3D {
		return minFlySpeed3D
	}
	return min(v, maxFlySpeed3D)
}
