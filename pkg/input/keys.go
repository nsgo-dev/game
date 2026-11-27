package input

import (
	"slices"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
)

// Key returns an input that is active while the key named name is held, named as core.KeyEvent.Key names it: "a", "1", "space", "enter", "left", "f5", or a modifier alone, "shift", "ctrl", "alt" or "meta". Only those names are held: the aliases core.KeyHeld also accepts, such as "mod", "cmd" or "option", never are, so bind "meta" where Env.AppleKeys reports Command as the platform modifier and "ctrl" elsewhere. A letter or a digit is the one the keyboard layout types unshifted, so Key follows the layout; KeyCode follows the physical position instead. The name is matched without regard to case, and an empty name is never active.
func Key(name string) Input {
	return Input{Kind: InputKey, Key: strings.ToLower(name)}
}

// normalizeInput lowercases an InputKey's name, as Key does, so an Input built as a struct literal matches and saves the same way.
func normalizeInput(in Input) Input {
	if in.Kind == InputKey {
		in.Key = strings.ToLower(in.Key)
	}
	return in
}

// KeyCode returns an input that is active while the key at physical position code is held, named as core.KeyEvent.Code names it, after the web's KeyboardEvent.code: "KeyW", "ArrowLeft", "Space", "ShiftLeft". A position stays where it is on every layout, so KeyCode("KeyW") is the key above S on QWERTY and AZERTY alike. An empty code is never active.
func KeyCode(code string) Input {
	return Input{Kind: InputKeyCode, Code: code}
}

// heldKey is one key a focused view heard go down: its name and, when the platform reports it, its physical position.
type heldKey struct {
	key  string
	code string
}

// OnKeyDown records a key going down from a view's OnKeyDown callback and reports whether an action is bound to it, which consumes the key so the keymap's bindings and the view's own Return and Space do not take it as well. A held key's auto-repeat changes nothing, and neither does the repeat of a key that went down before the view had the focus, whose coming up the view does not hear.
func (a *Actions) OnKeyDown(ev core.KeyEvent) bool {
	bound := a.keyBound(ev)
	if ev.Repeat || (ev.Key == "" && ev.Code == "") {
		return bound
	}
	if a.keysDown == nil {
		a.keysDown = make(map[string]heldKey)
	}
	a.keysDown[keyIdentity(ev)] = heldKey{key: ev.Key, code: ev.Code}
	a.syncRaw()
	a.wake()
	return bound
}

// OnKeyUp records a key coming up from a view's OnKeyUp callback. NSGo sends it for a key that went down while the view had the keyboard focus, wherever the focus has gone since, and for every key still held as the window loses the keyboard focus, so no key stays held. It does not send it once the view has left the tree, so a key held as the attached view is removed stays held until Reset; call Reset when the game view is replaced, or use WatchKeys, which has no such view.
func (a *Actions) OnKeyUp(ev core.KeyEvent) {
	id := keyIdentity(ev)
	removed := false
	if _, ok := a.keysDown[id]; ok {
		delete(a.keysDown, id)
		removed = true
	} else {
		// The key's down reported no position or another one; release the key by name.
		for k, h := range a.keysDown {
			if h.key == ev.Key {
				delete(a.keysDown, k)
				removed = true
				break
			}
		}
	}
	if a.syncRaw() || removed {
		a.wake()
	}
}

// WatchKeys has a hear the keys held anywhere on cx's window, whichever view has the keyboard focus, until cx is disposed or stop is called, which releases the keys it heard. A key held as Reset is called counts as released until it comes up and goes down again, as it does for a focused view. It follows core.KeysDown and core.KeyCodesDown, so keys typed into a text input or composed by an input method are never held, and every held key comes up as the window loses the keyboard focus. Call it once, from the scope that runs the game.Loop.
func (a *Actions) WatchKeys(cx *core.Scope) (stop func()) {
	if cx == nil {
		return func() {}
	}
	stopEffect := cx.Effect(func() {
		keys, codes := core.KeysDown(cx), core.KeyCodesDown(cx)
		cx.Untrack(func() { a.setWatchedKeys(keys, codes) })
	})
	return func() {
		stopEffect()
		a.setWatchedKeys(nil, nil)
	}
}

// setWatchedKeys replaces the keys held on the window with keys and codes, less those held since Reset, and latches any action they press.
func (a *Actions) setWatchedKeys(keys, codes []string) {
	var keysChanged, codesChanged bool
	a.ignoredKeys, a.watchedKeys, keysChanged = splitHeld(keys, a.ignoredKeys, a.watchedKeys)
	a.ignoredCodes, a.watchedCodes, codesChanged = splitHeld(codes, a.ignoredCodes, a.watchedCodes)
	if a.syncRaw() || keysChanged || codesChanged {
		a.wake()
	}
}

// ignoreWatchedKeys has the keys WatchKeys last read count as released until they come up, for Reset.
func (a *Actions) ignoreWatchedKeys() {
	a.ignoredKeys = append(a.ignoredKeys, a.watchedKeys...)
	a.ignoredCodes = append(a.ignoredCodes, a.watchedCodes...)
	a.watchedKeys = a.watchedKeys[:0]
	a.watchedCodes = a.watchedCodes[:0]
}

// splitHeld splits the held keys into those still ignored, kept from ignored, and the rest, reusing the two slices' storage, and reports whether rest changed.
func splitHeld(held, ignored, watched []string) (stillIgnored, rest []string, changed bool) {
	stillIgnored = slices.DeleteFunc(ignored, func(k string) bool { return !slices.Contains(held, k) })
	prev := watched
	rest = watched[:0]
	for _, k := range held {
		if !slices.Contains(stillIgnored, k) {
			i := len(rest)
			if i >= len(prev) || prev[i] != k {
				changed = true
			}
			rest = append(rest, k)
		}
	}
	if len(rest) != len(prev) {
		changed = true
	}
	return stillIgnored, rest, changed
}

// keyHeld reports whether the key named name is held, as a focused view heard it or WatchKeys reads it.
func (a *Actions) keyHeld(name string) bool {
	if name == "" {
		return false
	}
	if slices.Contains(a.watchedKeys, name) {
		return true
	}
	for _, h := range a.keysDown {
		if h.key == name {
			return true
		}
	}
	return false
}

// keyCodeHeld reports whether the key at physical position code is held, as a focused view heard it or WatchKeys reads it.
func (a *Actions) keyCodeHeld(code string) bool {
	if code == "" {
		return false
	}
	if slices.Contains(a.watchedCodes, code) {
		return true
	}
	for _, h := range a.keysDown {
		if h.code == code {
			return true
		}
	}
	return false
}

// keyBound reports whether an action is bound to ev's key by name or by position.
func (a *Actions) keyBound(ev core.KeyEvent) bool {
	for _, inputs := range a.bindings {
		for _, in := range inputs {
			switch in.Kind {
			case InputKey:
				if in.Key != "" && in.Key == ev.Key {
					return true
				}
			case InputKeyCode:
				if in.Code != "" && in.Code == ev.Code {
					return true
				}
			}
		}
	}
	return false
}

// keyIdentity names ev's key as one held key: by its physical position when the platform reports one, so a key held on both sides counts twice, and by its name otherwise.
func keyIdentity(ev core.KeyEvent) string {
	if ev.Code != "" {
		return "code:" + ev.Code
	}
	return "key:" + ev.Key
}
