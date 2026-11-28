package input_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/nsgo/gamepad"
)

func TestApplyControlSettingsScalesAndInvertsLookAxes(t *testing.T) {
	actions := input.NewActions()
	actions.BindGamepadAxis("look_x", gamepad.AxisRightX, input.GamepadAxisOptions{DeadZone: 0.1, Sensitivity: 0.5})
	actions.BindGamepadAxis("look_y", gamepad.AxisRightY, input.GamepadAxisOptions{DeadZone: 0.1, Invert: true})
	actions.BindPointerAxis("mouse_x", input.PointerX, input.PointerAxisOptions{Sensitivity: 0.01})
	actions.BindPointerAxis("mouse_y", input.PointerY, input.PointerAxisOptions{Sensitivity: 0.01})

	read := func() (lookX, lookY, mouseX, mouseY float64) {
		actions.OnGamepads([]gamepad.Pad{pad(0, nil, [4]float32{0, 0, 0.5, 0.25})})
		actions.OnPointerDelta(100, 100)
		actions.Step()
		return actions.Axis("look_x"), actions.Axis("look_y"), actions.Delta("mouse_x"), actions.Delta("mouse_y")
	}
	baseX, baseY, baseMX, baseMY := read()

	settings := save.DefaultSettings().Controls
	settings.StickSensitivity, settings.MouseSensitivity, settings.InvertY, settings.StickDeadZone = 2, 3, true, 0
	input.ApplyControlSettings(actions, settings)
	lookX, lookY, mouseX, mouseY := read()

	// A zero dead zone passes the stick through, so look_x reads 0.5 * 0.5 * 2, and look_y reads 0.25 * 2 with the declared inversion flipped back to positive.
	if math.Abs(lookX-0.5) > 1e-6 || math.Abs(lookY-0.5) > 1e-6 {
		t.Fatalf("stick axes = (%v, %v), want (0.5, 0.5); before settings (%v, %v)", lookX, lookY, baseX, baseY)
	}
	if baseY >= 0 {
		t.Fatalf("declared inverted look_y read %v before settings, want negative", baseY)
	}
	if math.Abs(mouseX-3*baseMX) > 1e-9 || math.Abs(mouseY+3*baseMY) > 1e-9 {
		t.Fatalf("pointer deltas = (%v, %v), want (%v, %v)", mouseX, mouseY, 3*baseMX, -3*baseMY)
	}

	// Applying again replaces the preferences rather than compounding them, and leaves the declared binding options unchanged.
	input.ApplyControlSettings(actions, settings)
	if x, _, _, _ := read(); math.Abs(x-lookX) > 1e-9 {
		t.Fatalf("look_x after second apply = %v, want %v", x, lookX)
	}
	if _, opts, ok := actions.GamepadAxisBinding("look_x"); !ok || opts.Sensitivity != 0.5 || opts.DeadZone != 0.1 || opts.Invert {
		t.Fatalf("declared look_x options = %+v, ok=%v", opts, ok)
	}
}

func TestLookScaleValidates(t *testing.T) {
	s := save.ControlSettings{MouseSensitivity: 2, TouchSensitivity: 99, GyroSensitivity: math.NaN()}
	if got := input.LookScale(s, input.LookMouse); got != 2 {
		t.Fatalf("LookScale(mouse) = %v, want 2", got)
	}
	if got := input.LookScale(s, input.LookTouch); got != save.MaxLookSensitivity {
		t.Fatalf("LookScale(touch) = %v, want %v", got, save.MaxLookSensitivity)
	}
	if got := input.LookScale(s, input.LookGyro); got != 1 {
		t.Fatalf("LookScale(gyro) = %v, want 1", got)
	}
}

func TestApplyControlSettingsToHapticsScalesRumble(t *testing.T) {
	h := input.NewHaptics(nil)
	var low, high float32
	h.GamepadRumble = func(l, hi float32, _ time.Duration) { low, high = l, hi }
	if h.VibrationScale() != 1 {
		t.Fatalf("default VibrationScale = %v, want 1", h.VibrationScale())
	}

	input.ApplyControlSettingsToHaptics(h, save.ControlSettings{Vibration: 0.5})
	h.Rumble(1, 0.5, time.Second)
	if low != 0.5 || high != 0.25 {
		t.Fatalf("rumble at half strength = (%v, %v), want (0.5, 0.25)", low, high)
	}

	input.ApplyControlSettingsToHaptics(h, save.ControlSettings{Vibration: 0})
	if h.Rumbling() || low != 0 || high != 0 {
		t.Fatalf("vibration 0 left rumble running: rumbling=%v motors=(%v, %v)", h.Rumbling(), low, high)
	}
	h.Rumble(1, 1, time.Second)
	if h.Rumbling() {
		t.Fatal("Rumble played with vibration 0")
	}
}
