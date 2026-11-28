package gameui

import (
	"slices"
	"strings"

	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/save"
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

var genericGamepadButtonDisplayNames = [gamepad.ButtonCount]string{
	gamepad.ButtonSouth:      "Pad South",
	gamepad.ButtonEast:       "Pad East",
	gamepad.ButtonWest:       "Pad West",
	gamepad.ButtonNorth:      "Pad North",
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

var xboxGamepadButtonDisplayNames = [gamepad.ButtonCount]string{
	gamepad.ButtonSouth:      "Xbox A",
	gamepad.ButtonEast:       "Xbox B",
	gamepad.ButtonWest:       "Xbox X",
	gamepad.ButtonNorth:      "Xbox Y",
	gamepad.ButtonLB:         "Xbox LB",
	gamepad.ButtonRB:         "Xbox RB",
	gamepad.ButtonLT:         "Xbox LT",
	gamepad.ButtonRT:         "Xbox RT",
	gamepad.ButtonBack:       "Xbox View",
	gamepad.ButtonStart:      "Xbox Menu",
	gamepad.ButtonLeftStick:  "Xbox L3",
	gamepad.ButtonRightStick: "Xbox R3",
	gamepad.ButtonDPadUp:     "Xbox D-pad Up",
	gamepad.ButtonDPadDown:   "Xbox D-pad Down",
	gamepad.ButtonDPadLeft:   "Xbox D-pad Left",
	gamepad.ButtonDPadRight:  "Xbox D-pad Right",
	gamepad.ButtonHome:       "Xbox Home",
}

var playStationGamepadButtonDisplayNames = [gamepad.ButtonCount]string{
	gamepad.ButtonSouth:      "PlayStation Cross",
	gamepad.ButtonEast:       "PlayStation Circle",
	gamepad.ButtonWest:       "PlayStation Square",
	gamepad.ButtonNorth:      "PlayStation Triangle",
	gamepad.ButtonLB:         "PlayStation L1",
	gamepad.ButtonRB:         "PlayStation R1",
	gamepad.ButtonLT:         "PlayStation L2",
	gamepad.ButtonRT:         "PlayStation R2",
	gamepad.ButtonBack:       "PlayStation Create",
	gamepad.ButtonStart:      "PlayStation Options",
	gamepad.ButtonLeftStick:  "PlayStation L3",
	gamepad.ButtonRightStick: "PlayStation R3",
	gamepad.ButtonDPadUp:     "PlayStation D-pad Up",
	gamepad.ButtonDPadDown:   "PlayStation D-pad Down",
	gamepad.ButtonDPadLeft:   "PlayStation D-pad Left",
	gamepad.ButtonDPadRight:  "PlayStation D-pad Right",
	gamepad.ButtonHome:       "PlayStation PS",
}

var nintendoGamepadButtonDisplayNames = [gamepad.ButtonCount]string{
	gamepad.ButtonSouth:      "Nintendo B",
	gamepad.ButtonEast:       "Nintendo A",
	gamepad.ButtonWest:       "Nintendo Y",
	gamepad.ButtonNorth:      "Nintendo X",
	gamepad.ButtonLB:         "Nintendo L",
	gamepad.ButtonRB:         "Nintendo R",
	gamepad.ButtonLT:         "Nintendo ZL",
	gamepad.ButtonRT:         "Nintendo ZR",
	gamepad.ButtonBack:       "Nintendo -",
	gamepad.ButtonStart:      "Nintendo +",
	gamepad.ButtonLeftStick:  "Nintendo L3",
	gamepad.ButtonRightStick: "Nintendo R3",
	gamepad.ButtonDPadUp:     "Nintendo D-pad Up",
	gamepad.ButtonDPadDown:   "Nintendo D-pad Down",
	gamepad.ButtonDPadLeft:   "Nintendo D-pad Left",
	gamepad.ButtonDPadRight:  "Nintendo D-pad Right",
	gamepad.ButtonHome:       "Nintendo Home",
}

func gamepadButtonDisplayNames(device input.DeviceKind) *[gamepad.ButtonCount]string {
	switch device {
	case input.DeviceGamepadXbox:
		return &xboxGamepadButtonDisplayNames
	case input.DeviceGamepadPlayStation:
		return &playStationGamepadButtonDisplayNames
	case input.DeviceGamepadNintendo:
		return &nintendoGamepadButtonDisplayNames
	default:
		return &genericGamepadButtonDisplayNames
	}
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

// FormatBindingLabel returns a concise human-readable label for a binding token using generic gamepad names and readable key-code fallbacks.
func FormatBindingLabel(token string) string {
	return FormatBindingLabelForDevice(nil, token, input.DeviceUnknown)
}

// FormatBindingLabelForDevice returns a binding label using device's controller family. When cx is non-nil, physical key codes use the active keyboard layout; otherwise readable names such as "W" and "Left" are used.
func FormatBindingLabelForDevice(cx *core.Scope, token string, device input.DeviceKind) string {
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
		if cx != nil {
			if label := strings.TrimSpace(core.KeyCodeLabel(cx, in.Code)); label != "" {
				return label
			}
		}
		return formatKeyCodeLabel(in.Code)
	case input.InputGamepadButton:
		names := gamepadButtonDisplayNames(device)
		if int(in.PadButton) < len(names) {
			return names[in.PadButton]
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

func formatKeyCodeLabel(code string) string {
	code = strings.TrimSpace(code)
	if len(code) == 4 && strings.HasPrefix(code, "Key") && code[3] >= 'A' && code[3] <= 'Z' {
		return code[3:]
	}
	if len(code) == 6 && strings.HasPrefix(code, "Digit") && code[5] >= '0' && code[5] <= '9' {
		return code[5:]
	}
	if len(code) == 7 && strings.HasPrefix(code, "Numpad") && code[6] >= '0' && code[6] <= '9' {
		return "Numpad " + code[6:]
	}
	switch code {
	case "Space":
		return "Space"
	case "Enter", "NumpadEnter":
		return "Enter"
	case "Escape":
		return "Esc"
	case "ArrowLeft":
		return "Left"
	case "ArrowRight":
		return "Right"
	case "ArrowUp":
		return "Up"
	case "ArrowDown":
		return "Down"
	case "ShiftLeft":
		return "Left Shift"
	case "ShiftRight":
		return "Right Shift"
	case "ControlLeft":
		return "Left Ctrl"
	case "ControlRight":
		return "Right Ctrl"
	case "AltLeft":
		return "Left Alt"
	case "AltRight":
		return "Right Alt"
	case "MetaLeft":
		return "Left Meta"
	case "MetaRight":
		return "Right Meta"
	case "Backquote":
		return "`"
	case "Minus":
		return "-"
	case "Equal":
		return "="
	case "BracketLeft":
		return "["
	case "BracketRight":
		return "]"
	case "Backslash", "IntlBackslash":
		return "\\"
	case "Semicolon":
		return ";"
	case "Quote":
		return "'"
	case "Comma":
		return ","
	case "Period":
		return "."
	case "Slash", "NumpadDivide":
		return "/"
	case "NumpadMultiply":
		return "Numpad *"
	case "NumpadAdd":
		return "Numpad +"
	case "NumpadSubtract":
		return "Numpad -"
	case "NumpadDecimal":
		return "Numpad ."
	default:
		return code
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

func bindingTokensEqual(a, b string) bool {
	parsedA, okA := ParseBindingToken(a)
	parsedB, okB := ParseBindingToken(b)
	if okA && okB {
		return FormatBindingToken(parsedA) == FormatBindingToken(parsedB)
	}
	return strings.TrimSpace(a) == strings.TrimSpace(b)
}

func replacedBindingForClass(existing []string, token string) string {
	cls := bindingDeviceClass(token)
	for _, item := range existing {
		if bindingDeviceClass(item) == cls && !bindingTokensEqual(item, token) {
			return strings.TrimSpace(item)
		}
	}
	return ""
}

func replaceConflictingBinding(existing []string, token, replacement string) []string {
	out := make([]string, 0, len(existing))
	hasReplacement := false
	for _, item := range existing {
		if !bindingTokensEqual(item, token) && bindingTokensEqual(item, replacement) {
			hasReplacement = true
			break
		}
	}
	inserted := false
	for _, item := range existing {
		if !bindingTokensEqual(item, token) {
			out = append(out, item)
			continue
		}
		if replacement != "" && !hasReplacement && !inserted {
			out = append(out, replacement)
			inserted = true
		}
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

// InputAssistsFromSettings converts s.InputAssists (and s.Accessibility.HoldToToggle,
// expanding toggleable actions when enabled) into an input.Assists value.
func InputAssistsFromSettings(s save.Settings, toggleable ...string) input.Assists {
	assists := input.Assists{
		Toggle:      slices.Clone(s.InputAssists.Toggle),
		MashToHold:  slices.Clone(s.InputAssists.MashToHold),
		MinPress:    s.InputAssists.MinPress(),
		AcceptDelay: s.InputAssists.AcceptDelay(),
	}
	if s.Accessibility.HoldToToggle {
		for _, name := range toggleable {
			if name != "" && !slices.Contains(assists.Toggle, name) {
				assists.Toggle = append(assists.Toggle, name)
			}
		}
	}
	return assists
}

// ApplyInputAssistsToActions applies s.InputAssists and s.Accessibility.HoldToToggle
// (expanding a.ToggleableActions() when HoldToToggle is true) onto a.
func ApplyInputAssistsToActions(a *input.Actions, s save.Settings) {
	if a == nil {
		return
	}
	a.SetAssists(InputAssistsFromSettings(s, a.ToggleableActions()...))
}
