package input

import (
	"strings"

	"github.com/nsgo-dev/nsgo/core"
)

// DeviceKind identifies the input device class the player last used.
type DeviceKind uint8

const (
	DeviceUnknown DeviceKind = iota
	DeviceTouch
	DeviceMouse
	DeviceKeyboard
	DeviceGamepadXbox
	DeviceGamepadPlayStation
	DeviceGamepadNintendo
	DeviceGamepadGeneric
	// DeviceGamepadSteamDeck identifies Steam Deck's built-in controls.
	DeviceGamepadSteamDeck
)

// String returns a short canonical name for d.
func (d DeviceKind) String() string {
	switch d {
	case DeviceUnknown:
		return "unknown"
	case DeviceTouch:
		return "touch"
	case DeviceMouse:
		return "mouse"
	case DeviceKeyboard:
		return "keyboard"
	case DeviceGamepadXbox:
		return "xbox"
	case DeviceGamepadPlayStation:
		return "playstation"
	case DeviceGamepadNintendo:
		return "nintendo"
	case DeviceGamepadGeneric:
		return "gamepad"
	case DeviceGamepadSteamDeck:
		return "steam_deck"
	default:
		return "unknown"
	}
}

// IsGamepad reports whether d is one of the gamepad device kinds.
func (d DeviceKind) IsGamepad() bool {
	switch d {
	case DeviceGamepadXbox, DeviceGamepadPlayStation, DeviceGamepadNintendo, DeviceGamepadGeneric, DeviceGamepadSteamDeck:
		return true
	default:
		return false
	}
}

// DetectGamepadKind infers the gamepad brand from a controller name string, falling back to DeviceGamepadGeneric.
func DetectGamepadKind(name string) DeviceKind {
	s := strings.ToLower(name)
	switch {
	case strings.Contains(s, "steam deck"), strings.Contains(s, "steamdeck"), strings.Contains(s, "valve handheld"):
		return DeviceGamepadSteamDeck
	case strings.Contains(s, "dualsense"),
		strings.Contains(s, "dualshock"),
		strings.Contains(s, "playstation"),
		strings.Contains(s, "sony"),
		strings.Contains(s, "ps4"),
		strings.Contains(s, "ps5"):
		return DeviceGamepadPlayStation
	case strings.Contains(s, "nintendo"),
		strings.Contains(s, "switch"),
		strings.Contains(s, "joy-con"),
		strings.Contains(s, "joycon"),
		strings.Contains(s, "pro controller"):
		return DeviceGamepadNintendo
	case strings.Contains(s, "xbox"),
		strings.Contains(s, "xinput"),
		strings.Contains(s, "microsoft"):
		return DeviceGamepadXbox
	default:
		return DeviceGamepadGeneric
	}
}

// DeviceTracker tracks the player's last-used input device across touch, mouse, keyboard, and gamepad activity.
//
// When constructed with NewDeviceTracker(cx, initial) and a non-nil core.Scope, it backs LastDevice with a core.Signal[DeviceKind] so reactive HUD views automatically re-render whenever the player switches devices.
//
// The zero value is ready to use in non-reactive mode starting at DeviceUnknown.
type DeviceTracker struct {
	device        DeviceKind
	signal        *core.Signal[DeviceKind]
	activeTouches map[int]struct{}
	onChange      []func(DeviceKind)
}

// NewDeviceTracker returns a DeviceTracker initialized to initial. When cx is non-nil, it creates a reactive core.Signal[DeviceKind] owned by cx.
func NewDeviceTracker(cx *core.Scope, initial DeviceKind) *DeviceTracker {
	t := &DeviceTracker{device: initial}
	if cx != nil {
		t.signal = core.NewSignal(cx, initial)
	}
	return t
}

// LastDevice returns the most recently used input device. When t was created with a core.Scope, reading LastDevice subscribes the current reactive computation to device changes.
func (t *DeviceTracker) LastDevice() DeviceKind {
	if t.signal != nil {
		return t.signal.Get()
	}
	return t.device
}

// LastDeviceUntracked returns the most recently used input device without subscribing to t's signal.
func (t *DeviceTracker) LastDeviceUntracked() DeviceKind {
	if t.signal != nil {
		return t.signal.GetUntracked()
	}
	return t.device
}

// Signal returns the reactive signal backing t, or nil if t was created without a core.Scope.
func (t *DeviceTracker) Signal() *core.Signal[DeviceKind] {
	return t.signal
}

// OnChange registers fn to run whenever the tracked device changes.
func (t *DeviceTracker) OnChange(fn func(DeviceKind)) {
	if fn != nil {
		t.onChange = append(t.onChange, fn)
	}
}

// SetDevice explicitly sets the last-used device and notifies subscribers if it changed.
func (t *DeviceTracker) SetDevice(d DeviceKind) {
	if t.device == d {
		return
	}
	t.device = d
	if t.signal != nil {
		t.signal.Set(d)
	}
	for _, fn := range t.onChange {
		fn(d)
	}
}

// Reset clears active touch tracking while keeping the last-used device.
func (t *DeviceTracker) Reset() {
	clear(t.activeTouches)
}

// OnTouch records touch activity and switches the active device to DeviceTouch.
func (t *DeviceTracker) OnTouch(ev core.TouchEvent) {
	switch ev.Phase {
	case core.TouchStart, core.TouchMove:
		if len(ev.Touches) > 0 {
			if t.activeTouches == nil {
				t.activeTouches = make(map[int]struct{}, len(ev.Touches))
			}
			for _, f := range ev.Touches {
				t.activeTouches[f.ID] = struct{}{}
			}
		}
		t.SetDevice(DeviceTouch)
	case core.TouchEnd, core.TouchCancel:
		if len(ev.Touches) == 0 {
			if ev.Phase == core.TouchCancel {
				clear(t.activeTouches)
			}
			return
		}
		hasChanged := touchesHaveChanged(ev.Touches)
		for _, f := range ev.Touches {
			if f.Changed || !hasChanged {
				delete(t.activeTouches, f.ID)
			} else {
				if t.activeTouches == nil {
					t.activeTouches = make(map[int]struct{}, len(ev.Touches))
				}
				t.activeTouches[f.ID] = struct{}{}
			}
		}
		if ev.Phase == core.TouchEnd {
			t.SetDevice(DeviceTouch)
		}
	}
}

// OnMouseMove records mouse pointer movement and switches the active device to DeviceMouse.
func (t *DeviceTracker) OnMouseMove(core.MouseEvent) {
	t.SetDevice(DeviceMouse)
}

// OnPressDown records a press event. Keyboard presses switch to DeviceKeyboard; pointer presses switch to DeviceMouse unless synthesized while a touch finger is active.
func (t *DeviceTracker) OnPressDown(ev core.PressEvent) {
	if ev.Keyboard {
		t.SetDevice(DeviceKeyboard)
		return
	}
	if len(t.activeTouches) > 0 {
		return
	}
	t.SetDevice(DeviceMouse)
}

// OnContextMenu records a secondary mouse click and switches to DeviceMouse.
func (t *DeviceTracker) OnContextMenu(core.ContextMenuEvent) {
	t.SetDevice(DeviceMouse)
}

// OnWheel records mouse wheel activity and switches to DeviceMouse.
func (t *DeviceTracker) OnWheel(core.WheelEvent) {
	t.SetDevice(DeviceMouse)
}

// OnKey records hardware keyboard activity and switches to DeviceKeyboard.
func (t *DeviceTracker) OnKey(core.KeyEvent) {
	t.SetDevice(DeviceKeyboard)
}

// OnGamepad records gamepad activity for kind. Non-gamepad kinds default to DeviceGamepadGeneric.
func (t *DeviceTracker) OnGamepad(kind DeviceKind) {
	if !kind.IsGamepad() {
		kind = DeviceGamepadGeneric
	}
	t.SetDevice(kind)
}

// OnGamepadActivity records gamepad activity using a controller name string to infer its brand.
func (t *DeviceTracker) OnGamepadActivity(controllerName string) {
	t.SetDevice(DetectGamepadKind(controllerName))
}

// Attach wires t's touch, mouse, press, wheel, and keyboard handlers onto v, chaining any existing callbacks without consuming key events.
func (t *DeviceTracker) Attach(v *core.View) {
	if v == nil {
		return
	}
	if prev := v.OnTouch; prev != nil {
		v.OnTouch = func(ev core.TouchEvent) { t.OnTouch(ev); prev(ev) }
	} else {
		v.OnTouch = t.OnTouch
	}
	if prev := v.OnPressDown; prev != nil {
		v.OnPressDown = func(ev core.PressEvent) { t.OnPressDown(ev); prev(ev) }
	} else {
		v.OnPressDown = t.OnPressDown
	}
	if prev := v.OnMouseMove; prev != nil {
		v.OnMouseMove = func(ev core.MouseEvent) { t.OnMouseMove(ev); prev(ev) }
	} else {
		v.OnMouseMove = t.OnMouseMove
	}
	if prev := v.OnContextMenu; prev != nil {
		v.OnContextMenu = func(ev core.ContextMenuEvent) { t.OnContextMenu(ev); prev(ev) }
	} else {
		v.OnContextMenu = t.OnContextMenu
	}
	if prev := v.OnWheel; prev != nil {
		v.OnWheel = func(ev core.WheelEvent) { t.OnWheel(ev); prev(ev) }
	} else {
		v.OnWheel = t.OnWheel
	}
	if prev := v.OnKeyDown; prev != nil {
		v.OnKeyDown = func(ev core.KeyEvent) bool { t.OnKey(ev); return prev(ev) }
	} else {
		v.OnKeyDown = func(ev core.KeyEvent) bool { t.OnKey(ev); return false }
	}
}

// Prompt describes how an action should be shown in a HUD for a specific input device.
type Prompt struct {
	// Label is the human-readable prompt text, such as "Tap Jump", "Click", "[Space]", "[A]", or "[Cross]".
	Label string
	// Icon is an optional icon asset or sprite key, such as "key_space" or "gamepad_ps_cross".
	Icon string
	// Glyph is an optional compact button or key symbol, such as "Space", "A", or "Cross".
	Glyph string
}

// PromptTable maps (action, DeviceKind) pairs to HUD Prompts with a device fallback chain.
//
// The zero value is ready to use.
type PromptTable struct {
	entries map[string]map[DeviceKind]Prompt
}

// NewPromptTable returns an empty PromptTable.
func NewPromptTable() *PromptTable {
	return &PromptTable{}
}

// DefaultPromptTable returns a PromptTable pre-populated with prompts for common game actions across touch, mouse, keyboard, and Xbox/PlayStation/Nintendo/generic gamepads.
func DefaultPromptTable() *PromptTable {
	pt := NewPromptTable()
	pt.SetMany("jump", map[DeviceKind]Prompt{
		DeviceTouch:              {Label: "Tap Jump", Icon: "touch_tap", Glyph: "Tap"},
		DeviceMouse:              {Label: "Click", Icon: "mouse_left", Glyph: "LMB"},
		DeviceKeyboard:           {Label: "[Space]", Icon: "key_space", Glyph: "Space"},
		DeviceGamepadXbox:        {Label: "[A]", Icon: "gamepad_xbox_a", Glyph: "A"},
		DeviceGamepadPlayStation: {Label: "[Cross]", Icon: "gamepad_ps_cross", Glyph: "Cross"},
		DeviceGamepadNintendo:    {Label: "[B]", Icon: "gamepad_nintendo_b", Glyph: "B"},
		DeviceGamepadGeneric:     {Label: "[South]", Icon: "gamepad_south", Glyph: "South"},
	})
	pt.SetMany("confirm", map[DeviceKind]Prompt{
		DeviceTouch:              {Label: "Tap", Icon: "touch_tap", Glyph: "Tap"},
		DeviceMouse:              {Label: "Click", Icon: "mouse_left", Glyph: "LMB"},
		DeviceKeyboard:           {Label: "[Enter]", Icon: "key_enter", Glyph: "Enter"},
		DeviceGamepadXbox:        {Label: "[A]", Icon: "gamepad_xbox_a", Glyph: "A"},
		DeviceGamepadPlayStation: {Label: "[Cross]", Icon: "gamepad_ps_cross", Glyph: "Cross"},
		DeviceGamepadNintendo:    {Label: "[A]", Icon: "gamepad_nintendo_a", Glyph: "A"},
		DeviceGamepadGeneric:     {Label: "[Confirm]", Icon: "gamepad_south", Glyph: "OK"},
	})
	pt.SetMany("cancel", map[DeviceKind]Prompt{
		DeviceTouch:              {Label: "Tap Back", Icon: "touch_back", Glyph: "Back"},
		DeviceMouse:              {Label: "Right Click", Icon: "mouse_right", Glyph: "RMB"},
		DeviceKeyboard:           {Label: "[Esc]", Icon: "key_escape", Glyph: "Esc"},
		DeviceGamepadXbox:        {Label: "[B]", Icon: "gamepad_xbox_b", Glyph: "B"},
		DeviceGamepadPlayStation: {Label: "[Circle]", Icon: "gamepad_ps_circle", Glyph: "Circle"},
		DeviceGamepadNintendo:    {Label: "[B]", Icon: "gamepad_nintendo_b", Glyph: "B"},
		DeviceGamepadGeneric:     {Label: "[East]", Icon: "gamepad_east", Glyph: "Back"},
	})
	pt.SetMany("attack", map[DeviceKind]Prompt{
		DeviceTouch:              {Label: "Tap Attack", Icon: "touch_attack", Glyph: "Tap"},
		DeviceMouse:              {Label: "Click", Icon: "mouse_left", Glyph: "LMB"},
		DeviceKeyboard:           {Label: "[J]", Icon: "key_j", Glyph: "J"},
		DeviceGamepadXbox:        {Label: "[X]", Icon: "gamepad_xbox_x", Glyph: "X"},
		DeviceGamepadPlayStation: {Label: "[Square]", Icon: "gamepad_ps_square", Glyph: "Square"},
		DeviceGamepadNintendo:    {Label: "[Y]", Icon: "gamepad_nintendo_y", Glyph: "Y"},
		DeviceGamepadGeneric:     {Label: "[West]", Icon: "gamepad_west", Glyph: "West"},
	})
	pt.SetMany("interact", map[DeviceKind]Prompt{
		DeviceTouch:              {Label: "Tap Interact", Icon: "touch_tap", Glyph: "Tap"},
		DeviceMouse:              {Label: "Click", Icon: "mouse_left", Glyph: "LMB"},
		DeviceKeyboard:           {Label: "[E]", Icon: "key_e", Glyph: "E"},
		DeviceGamepadXbox:        {Label: "[Y]", Icon: "gamepad_xbox_y", Glyph: "Y"},
		DeviceGamepadPlayStation: {Label: "[Triangle]", Icon: "gamepad_ps_triangle", Glyph: "Triangle"},
		DeviceGamepadNintendo:    {Label: "[X]", Icon: "gamepad_nintendo_x", Glyph: "X"},
		DeviceGamepadGeneric:     {Label: "[North]", Icon: "gamepad_north", Glyph: "North"},
	})
	pt.SetMany("dash", map[DeviceKind]Prompt{
		DeviceTouch:              {Label: "Double Tap", Icon: "touch_double_tap", Glyph: "2x"},
		DeviceMouse:              {Label: "Right Click", Icon: "mouse_right", Glyph: "RMB"},
		DeviceKeyboard:           {Label: "[Shift]", Icon: "key_shift", Glyph: "Shift"},
		DeviceGamepadXbox:        {Label: "[RB]", Icon: "gamepad_xbox_rb", Glyph: "RB"},
		DeviceGamepadPlayStation: {Label: "[R1]", Icon: "gamepad_ps_r1", Glyph: "R1"},
		DeviceGamepadNintendo:    {Label: "[R]", Icon: "gamepad_nintendo_r", Glyph: "R"},
		DeviceGamepadGeneric:     {Label: "[R1]", Icon: "gamepad_r1", Glyph: "R1"},
	})
	pt.SetMany("pause", map[DeviceKind]Prompt{
		DeviceTouch:              {Label: "Tap Pause", Icon: "touch_pause", Glyph: "||"},
		DeviceMouse:              {Label: "Click Pause", Icon: "mouse_left", Glyph: "||"},
		DeviceKeyboard:           {Label: "[Esc]", Icon: "key_escape", Glyph: "Esc"},
		DeviceGamepadXbox:        {Label: "[Menu]", Icon: "gamepad_xbox_menu", Glyph: "Menu"},
		DeviceGamepadPlayStation: {Label: "[Options]", Icon: "gamepad_ps_options", Glyph: "Options"},
		DeviceGamepadNintendo:    {Label: "[+]", Icon: "gamepad_nintendo_plus", Glyph: "+"},
		DeviceGamepadGeneric:     {Label: "[Start]", Icon: "gamepad_start", Glyph: "Start"},
	})
	return pt
}

// Set stores prompt for (action, device).
func (t *PromptTable) Set(action string, device DeviceKind, prompt Prompt) {
	if action == "" {
		return
	}
	if t.entries == nil {
		t.entries = make(map[string]map[DeviceKind]Prompt)
	}
	byDevice := t.entries[action]
	if byDevice == nil {
		byDevice = make(map[DeviceKind]Prompt)
		t.entries[action] = byDevice
	}
	byDevice[device] = prompt
}

// SetMany stores multiple per-device prompts for action.
func (t *PromptTable) SetMany(action string, prompts map[DeviceKind]Prompt) {
	for device, p := range prompts {
		t.Set(action, device, p)
	}
}

// Delete removes the prompt registered for (action, device).
func (t *PromptTable) Delete(action string, device DeviceKind) {
	if byDevice := t.entries[action]; byDevice != nil {
		delete(byDevice, device)
		if len(byDevice) == 0 {
			delete(t.entries, action)
		}
	}
}

// Lookup returns the prompt explicitly registered for (action, device) without consulting the fallback chain.
func (t *PromptTable) Lookup(action string, device DeviceKind) (Prompt, bool) {
	if byDevice := t.entries[action]; byDevice != nil {
		p, ok := byDevice[device]
		return p, ok
	}
	return Prompt{}, false
}

// Prompt resolves the prompt for (action, device) using the device fallback chain (e.g. GamepadPlayStation -> GamepadGeneric -> Keyboard -> action name).
func (t *PromptTable) Prompt(action string, device DeviceKind) Prompt {
	if byDevice := t.entries[action]; byDevice != nil {
		for _, candidate := range deviceFallbackChain(device) {
			if p, ok := byDevice[candidate]; ok {
				return p
			}
		}
	}
	return Prompt{Label: action}
}

// PromptFor resolves the prompt for action using tracker.LastDevice(), subscribing any active reactive computation to device changes.
func (t *PromptTable) PromptFor(action string, tracker *DeviceTracker) Prompt {
	dev := DeviceUnknown
	if tracker != nil {
		dev = tracker.LastDevice()
	}
	return t.Prompt(action, dev)
}

func deviceFallbackChain(d DeviceKind) []DeviceKind {
	switch d {
	case DeviceGamepadSteamDeck:
		return []DeviceKind{DeviceGamepadSteamDeck, DeviceGamepadGeneric, DeviceKeyboard}
	case DeviceGamepadPlayStation:
		return []DeviceKind{DeviceGamepadPlayStation, DeviceGamepadGeneric, DeviceKeyboard}
	case DeviceGamepadXbox:
		return []DeviceKind{DeviceGamepadXbox, DeviceGamepadGeneric, DeviceKeyboard}
	case DeviceGamepadNintendo:
		return []DeviceKind{DeviceGamepadNintendo, DeviceGamepadGeneric, DeviceKeyboard}
	case DeviceGamepadGeneric:
		return []DeviceKind{DeviceGamepadGeneric, DeviceKeyboard}
	case DeviceMouse:
		return []DeviceKind{DeviceMouse, DeviceKeyboard}
	case DeviceTouch:
		return []DeviceKind{DeviceTouch, DeviceKeyboard}
	case DeviceKeyboard:
		return []DeviceKind{DeviceKeyboard}
	default:
		return []DeviceKind{DeviceUnknown, DeviceKeyboard, DeviceMouse, DeviceTouch, DeviceGamepadGeneric}
	}
}
