package input

import (
	"fmt"
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

// PointerAxis identifies one direction of the relative mouse motion a view hears while it holds the pointer lock.
type PointerAxis uint8

const (
	// PointerAxisNone is the zero PointerAxis, which always reads 0.
	PointerAxisNone PointerAxis = iota
	// PointerX is relative motion to the right, in the units NSGo's View.OnPointerDelta reports; motion to the left is negative.
	PointerX
	// PointerY is relative motion down, in the units NSGo's View.OnPointerDelta reports; motion up is negative.
	PointerY
)

// String returns a stable name for p.
func (p PointerAxis) String() string {
	switch p {
	case PointerAxisNone:
		return "none"
	case PointerX:
		return "pointer_x"
	case PointerY:
		return "pointer_y"
	default:
		return fmt.Sprintf("PointerAxis(%d)", p)
	}
}

func (p PointerAxis) valid() bool {
	return p == PointerX || p == PointerY
}

func encodePointerAxis(p PointerAxis) (string, error) {
	if !p.valid() && p != PointerAxisNone {
		return "", fmt.Errorf("unknown pointer axis %d", p)
	}
	return p.String(), nil
}

func decodePointerAxis(s string) (PointerAxis, error) {
	switch s {
	case "", "none":
		return PointerAxisNone, nil
	case "pointer_x":
		return PointerX, nil
	case "pointer_y":
		return PointerY, nil
	default:
		return PointerAxisNone, fmt.Errorf("unknown pointer axis %q", s)
	}
}

// PointerAxisOptions configures how one bound pointer axis scales relative mouse motion.
type PointerAxisOptions struct {
	// Sensitivity multiplies the motion, such as radians of camera turn per unit of motion (1 when zero, negative or not finite).
	Sensitivity float64
	// Invert negates the motion, as an inverted look axis does.
	Invert bool
}

type pointerAxisBinding struct {
	axis PointerAxis
	opts PointerAxisOptions
}

func (b pointerAxisBinding) scale(dx, dy float64) float64 {
	v := dx
	if b.axis == PointerY {
		v = dy
	}
	s := b.opts.Sensitivity
	if s <= 0 || math.IsNaN(s) || math.IsInf(s, 0) {
		s = 1
	}
	v *= s
	if b.opts.Invert {
		v = -v
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

// BindPointerAxis binds axis name on a to relative mouse motion along axis while the game holds the pointer lock, scaled by opts. Calling BindPointerAxis with PointerAxisNone unbinds the pointer axis for name.
//
// Read the motion with Delta for mouselook, where a fast flick must turn the camera further than a slow one, or with Axis, which adds it to any other axis bound to name and clamps the sum to [-1, 1].
func (a *Actions) BindPointerAxis(name string, axis PointerAxis, opts PointerAxisOptions) {
	if !axis.valid() {
		a.UnbindPointerAxis(name)
		return
	}
	if math.IsNaN(opts.Sensitivity) || math.IsInf(opts.Sensitivity, 0) {
		opts.Sensitivity = 0
	}
	if a.pointerAxes == nil {
		a.pointerAxes = make(map[string]pointerAxisBinding)
	}
	a.pointerAxes[name] = pointerAxisBinding{axis: axis, opts: opts}
}

// UnbindPointerAxis removes the pointer axis binding for name.
func (a *Actions) UnbindPointerAxis(name string) {
	delete(a.pointerAxes, name)
}

// PointerAxisBinding returns the pointer axis and options bound to name.
func (a *Actions) PointerAxisBinding(name string) (PointerAxis, PointerAxisOptions, bool) {
	b, ok := a.pointerAxes[name]
	return b.axis, b.opts, ok
}

// LockPointer asks the platform to hide the pointer and keep it in place while the view ref names hears relative motion, which Attach forwards to a's pointer axes. It reports whether the surface supports the lock, as cx.Env().CanLockPointer does, and the request went out; with a nil ref or a view that is not mounted it does nothing and reports false.
//
// Call it from a press on that view: browsers lock only during a user gesture. Locking again replaces the previous lock. Escape, the window losing focus and hiding or removing the view release the lock as the platform decides, and NSGo does not tell the app, so PointerLocked keeps reporting the request until UnlockPointer or Reset; lock again from the next press to resume.
func (a *Actions) LockPointer(cx *core.Scope, ref *core.Ref) bool {
	if cx == nil || ref == nil || !cx.Env().CanLockPointer() {
		return false
	}
	if _, mounted := ref.Bounds(); !mounted {
		return false
	}
	a.UnlockPointer()
	a.unlockPointer = ref.LockPointer()
	return true
}

// UnlockPointer releases a lock LockPointer took, so the platform shows the pointer again where it was. Motion heard before the release still reaches the next Step. It does nothing when a holds no lock.
func (a *Actions) UnlockPointer() {
	if unlock := a.unlockPointer; unlock != nil {
		a.unlockPointer = nil
		unlock()
	}
}

// PointerLocked reports whether a holds a lock from LockPointer that neither UnlockPointer nor Reset has released.
func (a *Actions) PointerLocked() bool {
	return a.unlockPointer != nil
}

// OnPointerDelta records relative motion, right and down, from a view's OnPointerDelta callback, which NSGo calls only while that view holds the pointer lock. Motion adds up until the next Step; a value that is not finite is ignored.
func (a *Actions) OnPointerDelta(dx, dy float64) {
	if math.IsNaN(dx) || math.IsInf(dx, 0) || math.IsNaN(dy) || math.IsInf(dy, 0) {
		return
	}
	x, y := a.pendingDX+dx, a.pendingDY+dy
	if math.IsInf(x, 0) || math.IsInf(y, 0) {
		return
	}
	a.pendingDX, a.pendingDY = x, y
}

// PointerDelta returns the relative mouse motion, right and down, heard between the previous Step and the current one, unscaled.
func (a *Actions) PointerDelta() (dx, dy float64) {
	return a.stepDX, a.stepDY
}

// Delta returns the relative motion of pointer axis name for the current step: the motion heard since the previous Step along its axis, scaled by Sensitivity and negated by Invert, without a clamp. It reads 0 when no pointer axis is bound to name.
func (a *Actions) Delta(name string) float64 {
	b, ok := a.pointerAxes[name]
	if !ok {
		return 0
	}
	return b.scale(a.stepDX, a.stepDY)
}

func (a *Actions) stepPointer() {
	a.stepDX, a.stepDY = a.pendingDX, a.pendingDY
	a.pendingDX, a.pendingDY = 0, 0
}

func (a *Actions) resetPointer() {
	a.UnlockPointer()
	a.pendingDX, a.pendingDY = 0, 0
	a.stepDX, a.stepDY = 0, 0
}
