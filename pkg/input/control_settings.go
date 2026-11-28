package input

import "github.com/nsgo-dev/game/pkg/save"

// LookDevice identifies a physical input device whose look sensitivity can be
// selected from ControlSettings.
type LookDevice uint8

const (
	LookMouse LookDevice = iota
	LookStick
	LookTouch
	LookGyro
)

// LookScale returns the validated sensitivity multiplier for device.
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

// ApplyControlSettings applies player look preferences to all gamepad axis
// bindings on actions while preserving their physical axis assignments.
func ApplyControlSettings(actions *Actions, settings save.ControlSettings) {
	if actions == nil {
		return
	}
	settings.Validate()
	for name, binding := range actions.gpAxes {
		binding.opts.Sensitivity = settings.StickSensitivity
		binding.opts.DeadZone = settings.StickDeadZone
		if binding.axis == 1 || binding.axis == 3 {
			binding.opts.Invert = settings.InvertY
		} else {
			binding.opts.Invert = settings.InvertX
		}
		actions.gpAxes[name] = binding
	}
	for name, binding := range actions.pointerAxes {
		binding.opts.Sensitivity = settings.MouseSensitivity
		if binding.axis == PointerY {
			binding.opts.Invert = settings.InvertY
		} else {
			binding.opts.Invert = settings.InvertX
		}
		actions.pointerAxes[name] = binding
	}
	for name, binding := range actions.motionAxes {
		binding.opts.Sensitivity = settings.GyroSensitivity
		switch binding.axis {
		case TiltY, GyroY:
			binding.opts.Invert = settings.InvertY
		default:
			binding.opts.Invert = settings.InvertX
		}
		actions.motionAxes[name] = binding
	}
}

// ApplyControlSettingsToHaptics applies the player's vibration strength to a
// haptics device. It is separate from ApplyControlSettings because Actions
// does not own a Haptics instance.
func ApplyControlSettingsToHaptics(h *Haptics, settings save.ControlSettings) {
	if h == nil {
		return
	}
	settings.Validate()
	h.SetVibrationScale(float32(settings.Vibration))
}
