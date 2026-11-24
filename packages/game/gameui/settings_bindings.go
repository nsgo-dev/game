package gameui

import (
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
)

var gamepadButtonTokenNames = [gamepad.ButtonCount]string{
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

var gamepadButtonDisplayNames = [gamepad.ButtonCount]string{
	gamepad.ButtonSouth:      "Pad A/South",
	gamepad.ButtonEast:       "Pad B/East",
	gamepad.ButtonWest:       "Pad X/West",
	gamepad.ButtonNorth:      "Pad Y/North",
	gamepad.ButtonLB:         "Pad LB",
	gamepad.ButtonRB:         "Pad RB",
	gamepad.ButtonLT:         "Pad LT",
	gamepad.ButtonRT:         "Pad RT",
	gamepad.ButtonBack:       "Pad Back",
	gamepad.ButtonStart:      "Pad Start",
	gamepad.ButtonLeftStick:  "Pad L3",
	gamepad.ButtonRightStick: "Pad R3",
	gamepad.ButtonDPadUp:     "Pad Up",
	gamepad.ButtonDPadDown:   "Pad Down",
	gamepad.ButtonDPadLeft:   "Pad Left",
	gamepad.ButtonDPadRight:  "Pad Right",
	gamepad.ButtonHome:       "Pad Home",
}

// GamepadButtonToken returns the canonical binding token stored in save.Settings.Bindings
// for b (such as "gamepad:south" or "gamepad:dpad_left"), or "" if b is out of range.
func GamepadButtonToken(b gamepad.Button) string {
	if int(b) >= len(gamepadButtonTokenNames) {
		return ""
	}
	return "gamepad:" + gamepadButtonTokenNames[b]
}

// FormatBindingToken encodes an input.Input into a portable string token suitable for save.Settings.Bindings.
// Returns "" for region inputs or zero inputs.
func FormatBindingToken(in input.Input) string {
	switch in.Kind {
	case input.InputKey:
		k := strings.ToLower(strings.TrimSpace(in.Key))
		if k == "" {
			return ""
		}
		return "key:" + k
	case input.InputKeyCode:
		c := strings.TrimSpace(in.Code)
		if c == "" {
			return ""
		}
		return "keycode:" + c
	case input.InputGamepadButton:
		return GamepadButtonToken(in.PadButton)
	case input.InputMouseButton:
		switch in.Button {
		case core.MouseButtonLeft:
			return "mouse:left"
		case core.MouseButtonRight:
			return "mouse:right"
		case core.MouseButtonMiddle:
			return "mouse:middle"
		case core.MouseButtonBack:
			return "mouse:back"
		case core.MouseButtonForward:
			return "mouse:forward"
		default:
			return ""
		}
	default:
		return ""
	}
}

// ParseBindingToken decodes a binding token from save.Settings.Bindings into an input.Input.
// Supported forms are "key:<name>", "keycode:<code>", "gamepad:<button>", "mouse:<button>",
// and bare key names (such as "space" or "Escape") treated as keyboard keys.
func ParseBindingToken(token string) (input.Input, bool) {
	t := strings.TrimSpace(token)
	if t == "" {
		return input.Input{}, false
	}
	prefix, rest, hasColon := strings.Cut(t, ":")
	if !hasColon {
		return input.Key(strings.ToLower(t)), true
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return input.Input{}, false
	}
	switch strings.ToLower(strings.TrimSpace(prefix)) {
	case "key":
		return input.Key(strings.ToLower(rest)), true
	case "keycode":
		return input.KeyCode(rest), true
	case "gamepad":
		norm := strings.ToLower(rest)
		if idx := slices.Index(gamepadButtonTokenNames[:], norm); idx >= 0 {
			return input.GamepadButton(gamepad.Button(idx)), true
		}
		return input.Input{}, false
	case "mouse":
		switch strings.ToLower(rest) {
		case "left":
			return input.MouseButton(core.MouseButtonLeft), true
		case "right":
			return input.MouseButton(core.MouseButtonRight), true
		case "middle":
			return input.MouseButton(core.MouseButtonMiddle), true
		case "back":
			return input.MouseButton(core.MouseButtonBack), true
		case "forward":
			return input.MouseButton(core.MouseButtonForward), true
		default:
			return input.Input{}, false
		}
	default:
		return input.Input{}, false
	}
}

// FormatBindingLabel returns a concise human-readable label for a binding token
// (for example "Space", "A", "Pad A/South", or "Mouse Left").
func FormatBindingLabel(token string) string {
	t := strings.TrimSpace(token)
	if t == "" {
		return ""
	}
	in, ok := ParseBindingToken(t)
	if !ok {
		return t
	}
	switch in.Kind {
	case input.InputKey:
		return formatKeyLabel(in.Key)
	case input.InputKeyCode:
		return in.Code
	case input.InputGamepadButton:
		if int(in.PadButton) < len(gamepadButtonDisplayNames) {
			return gamepadButtonDisplayNames[in.PadButton]
		}
		return t
	case input.InputMouseButton:
		switch in.Button {
		case core.MouseButtonLeft:
			return "Mouse Left"
		case core.MouseButtonRight:
			return "Mouse Right"
		case core.MouseButtonMiddle:
			return "Mouse Middle"
		case core.MouseButtonBack:
			return "Mouse Back"
		case core.MouseButtonForward:
			return "Mouse Forward"
		default:
			return t
		}
	default:
		return t
	}
}

func formatKeyLabel(key string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case " ":
		return "Space"
	case "space":
		return "Space"
	case "enter", "return":
		return "Enter"
	case "escape", "esc":
		return "Esc"
	case "left", "arrowleft":
		return "Left"
	case "right", "arrowright":
		return "Right"
	case "up", "arrowup":
		return "Up"
	case "down", "arrowdown":
		return "Down"
	case "tab":
		return "Tab"
	case "shift":
		return "Shift"
	case "ctrl", "control":
		return "Ctrl"
	case "alt":
		return "Alt"
	case "meta":
		return "Meta"
	default:
		k := strings.TrimSpace(key)
		if len(k) == 1 {
			return strings.ToUpper(k)
		}
		if len(k) > 1 {
			return strings.ToUpper(k[:1]) + k[1:]
		}
		return k
	}
}

func bindingDeviceClass(token string) string {
	in, ok := ParseBindingToken(token)
	if !ok {
		return "other"
	}
	switch in.Kind {
	case input.InputKey, input.InputKeyCode:
		return "keyboard"
	case input.InputGamepadButton:
		return "gamepad"
	case input.InputMouseButton:
		return "mouse"
	default:
		return "other"
	}
}

// replaceBindingForClass updates existing with token, replacing the first entry of the same
// device class (keyboard vs gamepad vs mouse) while preserving bindings from other device classes.
func replaceBindingForClass(existing []string, token string) []string {
	token = strings.TrimSpace(token)
	if token == "" {
		return slices.Clone(existing)
	}
	cls := bindingDeviceClass(token)
	out := make([]string, 0, len(existing)+1)
	replaced := false
	for _, item := range existing {
		norm := strings.TrimSpace(item)
		if norm == "" {
			continue
		}
		if bindingDeviceClass(norm) == cls {
			if !replaced {
				out = append(out, token)
				replaced = true
			}
			continue
		}
		if norm != token {
			out = append(out, norm)
		}
	}
	if !replaced {
		out = append(out, token)
	}
	return out
}

// ApplyBindingsToActions updates a's action bindings from save.Settings.Bindings.
// When preserveRegions is true, existing TouchRegion and MouseRegion inputs on each action
// are preserved alongside the rebound keyboard, mouse-button, and gamepad bindings.
func ApplyBindingsToActions(a *input.Actions, bindings map[string][]string, preserveRegions bool) {
	if a == nil || len(bindings) == 0 {
		return
	}
	for action, tokens := range bindings {
		var inputs []input.Input
		if preserveRegions {
			for _, cur := range a.Bindings(action) {
				if cur.Kind == input.InputTouchRegion || cur.Kind == input.InputMouseRegion {
					inputs = append(inputs, cur)
				}
			}
		}
		for _, tok := range tokens {
			if in, ok := ParseBindingToken(tok); ok {
				inputs = append(inputs, in)
			}
		}
		if len(inputs) > 0 {
			a.Bind(action, inputs...)
		} else if !preserveRegions {
			a.Unbind(action)
		}
	}
}
