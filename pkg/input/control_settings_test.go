package input_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/nsgo/gamepad"
)

func TestApplyControlSettings(t *testing.T) {
	var actions input.Actions
	actions.BindGamepadAxis("look_x", gamepad.AxisLeftX, input.GamepadAxisOptions{Sensitivity: 1})
	actions.BindGamepadAxis("look_y", gamepad.AxisLeftY, input.GamepadAxisOptions{Sensitivity: 1})
	input.ApplyControlSettings(&actions, save.ControlSettings{StickSensitivity: 2, StickDeadZone: 0.3, InvertY: true})
	_, x, ok := actions.GamepadAxisBinding("look_x")
	if !ok || x.Sensitivity != 2 || x.DeadZone != 0.3 || x.Invert {
		t.Fatalf("x binding = %+v", x)
	}
	_, y, ok := actions.GamepadAxisBinding("look_y")
	if !ok || !y.Invert {
		t.Fatalf("y binding = %+v", y)
	}
}
