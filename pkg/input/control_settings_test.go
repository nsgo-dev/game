package input_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/nsgo/gamepad"
)

func TestApplyControlSettingsUpdatesLookBindings(t *testing.T) {
	var actions input.Actions
	actions.BindGamepadAxis("look_x", gamepad.AxisRightX, input.GamepadAxisOptions{DeadZone: 0.1})
	actions.BindPointerAxis("look_y", input.PointerY, input.PointerAxisOptions{Sensitivity: 4})
	actions.BindMotionAxis("gyro_x", input.GyroX, input.MotionAxisOptions{Sensitivity: 3})
	settings := save.ControlSettings{MouseSensitivity: 2, StickSensitivity: 1.5, GyroSensitivity: 0.75, StickDeadZone: 0.25, InvertX: true, InvertY: false}

	input.ApplyControlSettings(&actions, settings)
	if _, got, ok := actions.GamepadAxisBinding("look_x"); !ok || got.Sensitivity != 1.5 || got.DeadZone != 0.25 || !got.Invert {
		t.Fatalf("gamepad binding = %+v, ok=%v", got, ok)
	}
	if _, got, ok := actions.PointerAxisBinding("look_y"); !ok || got.Sensitivity != 2 || got.Invert {
		t.Fatalf("pointer binding = %+v, ok=%v", got, ok)
	}
	if _, got, ok := actions.MotionAxisBinding("gyro_x"); !ok || got.Sensitivity != 0.75 || !got.Invert {
		t.Fatalf("motion binding = %+v, ok=%v", got, ok)
	}
}

func TestApplyControlSettingsToHaptics(t *testing.T) {
	h := &input.Haptics{Enabled: true, VibrationScale: 1}
	input.ApplyControlSettingsToHaptics(h, save.ControlSettings{Vibration: 0})
	if h.VibrationScale != 0 {
		t.Fatalf("vibration scale = %v, want 0", h.VibrationScale)
	}
}
