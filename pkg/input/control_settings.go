package input

import (
	"math"

	"github.com/nsgo-dev/game/pkg/save"
)

// LookDevice identifies a physical input device whose look sensitivity can be selected from save.ControlSettings.
type LookDevice uint8

const (
	// LookMouse selects ControlSettings.MouseSensitivity.
	LookMouse LookDevice = iota
	// LookStick selects ControlSettings.StickSensitivity.
	LookStick
	// LookTouch selects ControlSettings.TouchSensitivity.
	LookTouch
	// LookGyro selects ControlSettings.GyroSensitivity.
	LookGyro
)

// LookScale returns the validated sensitivity multiplier for device, for camera code that reads raw deltas (such as touch drags) instead of a bound axis.
func LookScale(s save.ControlSettings, device LookDevice) float64 {
	s.Validate()
	switch device {
	case LookStick:
		return s.StickSensitivity
	case LookTouch:
		return s.TouchSensitivity
	case LookGyro:
		return s.GyroSensitivity
	default:
		return s.MouseSensitivity
	}
}

// lookPrefs is the player's look preferences applied on top of every stick, pointer and motion axis binding when the axis is read. The zero value leaves bindings as declared.
type lookPrefs struct {
	set      bool
	mouse    float64
	stick    float64
	gyro     float64
	deadZone float64
	invertX  bool
	invertY  bool
}

// ApplyControlSettings applies the player's look preferences in settings to every gamepad stick, pointer and motion axis bound on actions, now and later: each binding's declared Sensitivity is multiplied by the device's look sensitivity, the player's InvertX or InvertY flips the binding's declared Invert on horizontal or vertical axes, and the player's StickDeadZone replaces stick bindings' DeadZone (0 turns the dead zone off).
//
// The bindings' own options are left unchanged, so Actions.Save keeps the game's declared values and calling ApplyControlSettings again replaces the previous preferences instead of compounding them. Vibration is applied separately with ApplyControlSettingsToHaptics; TouchSensitivity has no binding and is read with LookScale(settings, LookTouch).
func ApplyControlSettings(actions *Actions, settings save.ControlSettings) {
	if actions == nil {
		return
	}
	settings.Validate()
	actions.look = lookPrefs{
		set:      true,
		mouse:    settings.MouseSensitivity,
		stick:    settings.StickSensitivity,
		gyro:     settings.GyroSensitivity,
		deadZone: settings.StickDeadZone,
		invertX:  settings.InvertX,
		invertY:  settings.InvertY,
	}
}

// ApplyControlSettingsToHaptics applies the player's vibration strength to a haptics device. It is separate from ApplyControlSettings because Actions does not own a Haptics instance.
func ApplyControlSettingsToHaptics(h *Haptics, settings save.ControlSettings) {
	if h == nil {
		return
	}
	settings.Validate()
	h.SetVibrationScale(float32(settings.Vibration))
}

func (p lookPrefs) gamepad(b gamepadAxisBinding) GamepadAxisOptions {
	opts := b.opts
	if !p.set {
		return opts
	}
	opts.Sensitivity = declaredSensitivity(opts.Sensitivity) * p.stick
	opts.DeadZone = p.deadZone
	if opts.DeadZone == 0 {
		// gamepadAxisValue reads a zero DeadZone as DefaultGamepadDeadZone and a negative one as none.
		opts.DeadZone = -1
	}
	vertical := b.axis == 1 || b.axis == 3 // AxisLeftY, AxisRightY
	opts.Invert = opts.Invert != p.invert(vertical)
	return opts
}

func (p lookPrefs) pointer(b pointerAxisBinding) pointerAxisBinding {
	if !p.set {
		return b
	}
	b.opts.Sensitivity = declaredSensitivity(b.opts.Sensitivity) * p.mouse
	b.opts.Invert = b.opts.Invert != p.invert(b.axis == PointerY)
	return b
}

// motion returns opts with the player's preferences applied, where inherited is the sensitivity a zero opts.Sensitivity inherits from the attached MotionInput.
func (p lookPrefs) motion(axis MotionAxis, opts MotionAxisOptions, inherited float64) MotionAxisOptions {
	if !p.set {
		return opts
	}
	sens := opts.Sensitivity
	if sens <= 0 {
		sens = inherited
	}
	opts.Sensitivity = declaredSensitivity(sens) * p.gyro
	switch axis {
	case TiltY, GyroX: // vertical look: tilting forward or pitching around the screen X axis
		opts.Invert = opts.Invert != p.invertY
	case TiltX, GyroY, GyroZ:
		opts.Invert = opts.Invert != p.invertX
	}
	return opts
}

func (p lookPrefs) invert(vertical bool) bool {
	if vertical {
		return p.invertY
	}
	return p.invertX
}

// declaredSensitivity returns the multiplier a binding's Sensitivity stands for: 1 when it is zero, negative or not finite.
func declaredSensitivity(s float64) float64 {
	if s <= 0 || math.IsNaN(s) || math.IsInf(s, 0) {
		return 1
	}
	return s
}
