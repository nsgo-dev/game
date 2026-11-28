package gameui

import (
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func newConflictTestMenu(policy SettingsConflictPolicy, text SettingsText) *SettingsMenu {
	return NewSettingsMenu(SettingsMenuConfig{
		Actions: []SettingsAction{
			{Name: "left", Label: "Move Left", Defaults: []string{"key:a", "key:left", "gamepad:dpad_left"}},
			{Name: "jump", Label: "Jump", Defaults: []string{"key:space", "gamepad:south"}},
		},
		OnConflict: policy,
		Text:       text,
	})
}

func TestSettingsMenuCaptureBindingConflictPolicies(t *testing.T) {
	t.Run("swap is the default and preserves other bindings", func(t *testing.T) {
		m := newConflictTestMenu(ConflictSwap, SettingsText{})
		m.BeginCapture("jump")
		if !m.CaptureBinding("key:a") {
			t.Fatal("CaptureBinding refused a swap")
		}
		if got, want := m.settings.Bindings["jump"], []string{"key:a", "gamepad:south"}; !slices.Equal(got, want) {
			t.Fatalf("jump bindings = %v, want %v", got, want)
		}
		if got, want := m.settings.Bindings["left"], []string{"key:space", "key:left", "gamepad:dpad_left"}; !slices.Equal(got, want) {
			t.Fatalf("left bindings = %v, want %v", got, want)
		}
		if got, want := m.statusLabel.Text, "Binding conflict with Move Left"; got != want {
			t.Fatalf("status = %q, want %q", got, want)
		}
		if m.CapturingAction() != "" {
			t.Fatal("swap did not finish capture")
		}
	})

	t.Run("clear removes only the conflicting token", func(t *testing.T) {
		m := newConflictTestMenu(ConflictClear, SettingsText{})
		m.BeginCapture("jump")
		if !m.CaptureBinding("key:a") {
			t.Fatal("CaptureBinding refused a clear")
		}
		if got, want := m.settings.Bindings["left"], []string{"key:left", "gamepad:dpad_left"}; !slices.Equal(got, want) {
			t.Fatalf("left bindings = %v, want %v", got, want)
		}
		if !strings.Contains(m.statusLabel.Text, "Move Left") {
			t.Fatalf("status = %q, want the other action label", m.statusLabel.Text)
		}
	})

	t.Run("refuse keeps capture and all settings unchanged", func(t *testing.T) {
		m := newConflictTestMenu(ConflictRefuse, SettingsText{ConflictStatus: "已由 %s 使用"})
		before := cloneSaveSettings(m.settings)
		m.BeginCapture("jump")
		if m.CaptureBinding("key:a") {
			t.Fatal("CaptureBinding accepted a refused conflict")
		}
		if !slices.Equal(m.settings.Bindings["jump"], before.Bindings["jump"]) || !slices.Equal(m.settings.Bindings["left"], before.Bindings["left"]) || m.dirty {
			t.Fatalf("refused capture changed settings: %+v", m.settings)
		}
		if got := m.CapturingAction(); got != "jump" {
			t.Fatalf("capturing action = %q, want jump", got)
		}
		if got, want := m.statusLabel.Text, "已由 Move Left 使用"; got != want {
			t.Fatalf("localized status = %q, want %q", got, want)
		}
	})
}

func TestSettingsMenuCaptureBindingAllowsConflictsAcrossGroups(t *testing.T) {
	m := NewSettingsMenu(SettingsMenuConfig{Actions: []SettingsAction{
		{Name: "left", Label: "Move Left", Group: "gameplay", Defaults: []string{"key:a"}},
		{Name: "menu_left", Label: "Menu Left", Group: "menu", Defaults: []string{"key:left"}},
	}})
	m.BeginCapture("menu_left")
	if !m.CaptureBinding("key:a") {
		t.Fatal("CaptureBinding refused actions in different groups")
	}
	if got, want := m.settings.Bindings["left"], []string{"key:a"}; !slices.Equal(got, want) {
		t.Fatalf("gameplay binding = %v, want %v", got, want)
	}
	if got, want := m.settings.Bindings["menu_left"], []string{"key:a"}; !slices.Equal(got, want) {
		t.Fatalf("menu binding = %v, want %v", got, want)
	}
	if m.statusLabel.Text != "" {
		t.Fatalf("cross-group capture status = %q, want empty", m.statusLabel.Text)
	}
}

func TestSettingsMenuSwapWithoutReplacedBindingClearsConflict(t *testing.T) {
	m := NewSettingsMenu(SettingsMenuConfig{Actions: []SettingsAction{
		{Name: "left", Label: "Move Left", Defaults: []string{"key:a", "gamepad:dpad_left"}},
		{Name: "jump", Label: "Jump", Defaults: []string{"gamepad:south"}},
	}})
	m.BeginCapture("jump")
	if !m.CaptureBinding("key:a") {
		t.Fatal("CaptureBinding refused a swap")
	}
	if got, want := m.settings.Bindings["left"], []string{"gamepad:dpad_left"}; !slices.Equal(got, want) {
		t.Fatalf("left bindings = %v, want %v", got, want)
	}
}

func TestFormatBindingLabelForControllerFamilies(t *testing.T) {
	cases := []struct {
		device input.DeviceKind
		want   []string
	}{
		{input.DeviceGamepadXbox, []string{"Xbox A", "Xbox B", "Xbox X", "Xbox Y", "Xbox LB", "Xbox RB", "Xbox LT", "Xbox RT"}},
		{input.DeviceGamepadPlayStation, []string{"PlayStation Cross", "PlayStation Circle", "PlayStation Square", "PlayStation Triangle", "PlayStation L1", "PlayStation R1", "PlayStation L2", "PlayStation R2"}},
		{input.DeviceGamepadNintendo, []string{"Nintendo B", "Nintendo A", "Nintendo Y", "Nintendo X", "Nintendo L", "Nintendo R", "Nintendo ZL", "Nintendo ZR"}},
		{input.DeviceGamepadGeneric, []string{"Pad South", "Pad East", "Pad West", "Pad North", "Pad LB", "Pad RB", "Pad LT", "Pad RT"}},
	}
	tokens := []string{"gamepad:south", "gamepad:east", "gamepad:west", "gamepad:north", "gamepad:left_shoulder", "gamepad:right_shoulder", "gamepad:left_trigger", "gamepad:right_trigger"}
	for _, tc := range cases {
		t.Run(tc.device.String(), func(t *testing.T) {
			for i, token := range tokens {
				if got := FormatBindingLabelForDevice(nil, token, tc.device); got != tc.want[i] {
					t.Errorf("FormatBindingLabelForDevice(%q) = %q, want %q", token, got, tc.want[i])
				}
			}
		})
	}
}

func TestSettingsMenuDeviceTrackerRefreshesControllerLabels(t *testing.T) {
	tracker := input.NewDeviceTracker(nil, input.DeviceGamepadXbox)
	m := NewSettingsMenu(SettingsMenuConfig{
		DeviceTracker: tracker,
		Actions:       []SettingsAction{{Name: "jump", Defaults: []string{"gamepad:south"}}},
	})
	if got := m.actionButtons[0].Text; !strings.Contains(got, "Xbox A") {
		t.Fatalf("initial label = %q, want Xbox A", got)
	}
	for _, tc := range []struct {
		device input.DeviceKind
		want   string
	}{
		{input.DeviceGamepadPlayStation, "PlayStation Cross"},
		{input.DeviceGamepadNintendo, "Nintendo B"},
		{input.DeviceGamepadGeneric, "Pad South"},
	} {
		tracker.SetDevice(tc.device)
		if got := m.actionButtons[0].Text; !strings.Contains(got, tc.want) {
			t.Errorf("label after SetDevice(%s) = %q, want %s", tc.device, got, tc.want)
		}
	}
}

func TestSettingsMenuStepGamepadTracksControllerFamily(t *testing.T) {
	tracker := input.NewDeviceTracker(nil, input.DeviceUnknown)
	m := NewSettingsMenu(SettingsMenuConfig{
		DeviceTracker: tracker,
		Actions:       []SettingsAction{{Name: "jump", Defaults: []string{"gamepad:south"}}},
	})
	pad := gamepad.Pad{ID: 1, Name: "DualSense Wireless Controller", Family: gamepad.FamilyPlayStation}
	pad.Buttons[gamepad.ButtonDPadUp] = 1
	m.StepGamepad([]gamepad.Pad{pad})
	if got := tracker.LastDevice(); got != input.DeviceGamepadPlayStation {
		t.Fatalf("last device = %s, want PlayStation", got)
	}
	if got := m.actionButtons[0].Text; !strings.Contains(got, "PlayStation Cross") {
		t.Fatalf("label = %q, want PlayStation Cross", got)
	}
}

func TestFormatBindingLabelForKeyCodes(t *testing.T) {
	for token, want := range map[string]string{
		"keycode:KeyW":       "W",
		"keycode:Digit7":     "7",
		"keycode:ArrowLeft":  "Left",
		"keycode:ShiftRight": "Right Shift",
	} {
		if got := FormatBindingLabelForDevice(nil, token, input.DeviceUnknown); got != want {
			t.Errorf("FormatBindingLabelForDevice(%q) = %q, want %q", token, got, want)
		}
	}

	var m *SettingsMenu
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		m = NewSettingsMenu(SettingsMenuConfig{
			Scope:   cx,
			Actions: []SettingsAction{{Name: "jump", Defaults: []string{"keycode:KeyW"}}},
		})
		return core.View{}
	}, nsgotest.Environment(core.Environment{KeyCodeNames: map[string]string{"KeyW": "z"}}))
	if got, want := m.actionButtons[0].Text, "Jump: Z"; got != want {
		t.Fatalf("layout-aware key-code label = %q, want %q", got, want)
	}
}
