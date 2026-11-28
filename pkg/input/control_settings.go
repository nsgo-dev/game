package input

import "github.com/nsgo-dev/game/pkg/save"

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
}
