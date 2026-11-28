# input

`game/input` turns touch regions, mouse buttons, relative mouse motion under a pointer lock, keyboard keys and game controllers into named actions and axes, sampled once per `game.Loop` step and persisted across launches in `core.Storage`.

```go
import "github.com/nsgo-dev/game/pkg/input"
```

All `Actions` methods run on the engine's goroutine, alongside `core.View` callbacks and `game.Loop.Update`.

## Defining actions and axes

An `Actions` instance maps logical names (such as `"jump"`, `"left"`, `"right"`, or `"move_x"`) to one or more physical `Input` triggers. Its zero value is ready to use, or construct one with `input.NewActions()`:

```go
actions := input.NewActions()

// Touch zones in virtual canvas coordinates (e.g. 320×180)
actions.Bind("left",
	input.TouchRegion(core.Bounds{X: 0, Y: 100, Width: 70, Height: 80}),
	input.Key("left"),     // by name, following the keyboard layout
	input.KeyCode("KeyA"), // by physical position
)
actions.Bind("right",
	input.TouchRegion(core.Bounds{X: 70, Y: 100, Width: 70, Height: 80}),
	input.Key("right"),
	input.KeyCode("KeyD"),
)
actions.Bind("jump",
	input.TouchRegion(core.Bounds{X: 220, Y: 100, Width: 100, Height: 80}),
	input.MouseButton(core.MouseButtonLeft),
	input.Key("space"),
)
actions.Bind("interact",
	input.MouseRegion(core.Bounds{X: 260, Y: 10, Width: 50, Height: 30}, core.MouseButtonLeft),
)

// Combine two opposing actions into a [-1, 1] axis
actions.BindAxis("move_x", "left", "right")
```

Physical `Input` constructors:

| Constructor | Active when |
| --- | --- |
| `TouchRegion(rect)` | Any active touch finger lies inside `rect` (`core.Bounds.Contains`). |
| `MouseButton(btn)` | `btn` is held down anywhere on the attached view. |
| `MouseRegion(rect, btn)` | `btn` is held down and the pointer lies inside `rect`. |
| `Key(name)` | The key `name` is held, named as `core.KeyEvent.Key` names it (`"a"`, `"space"`, `"left"`, `"shift"`), matched without regard to case. A letter follows the keyboard layout. |
| `KeyCode(code)` | The key at physical position `code` is held, named as `core.KeyEvent.Code` names it (`"KeyW"`, `"ArrowLeft"`), the same key on every layout. |
| `GamepadButton(btn)` | `btn` is pressed on a controller the `Actions` reads (see Game controllers below). |

Calling `Bind(name, ...)` replaces previous bindings for `name`; calling `Bind(name)` with no inputs or `Unbind(name)` clears it. Inspect bindings with `Bindings(name)` and `AxisBinding(name)`, and remove an axis with `UnbindAxis(name)`.

## Attaching to a view and coordinate mapping

Wire `Actions` onto your game's `core.View` with `Attach`, which chains onto any existing event handlers without overwriting them:

```go
view := core.View{
	Style: core.Style{FlexGrow: 1, FlexShrink: 1},
	Children: []core.Node{gameCanvas},
}
actions.Attach(&view)
```

You can also forward individual callbacks manually via `OnTouch`, `OnPressDown`, `OnPressUp`, `OnMouseMove`, `OnMouseLeave`, `OnContextMenu`, `OnPointerDelta`, `OnKeyDown`, and `OnKeyUp`.

- `OnPressDown` ignores keyboard presses and synthetic presses fired while a touch finger is active so touches only trigger `TouchRegion` bindings.
- `OnContextMenu` records a right click as a single-step pulse of `core.MouseButtonRight` at `ev.Position`.
- `OnMouseLeave` releases held mouse buttons when the pointer exits the view.
- Set `actions.Mixer` to your [`sound.Mixer`](sound.md#web-autoplay-and-gesture-unlock) so any press, touch or key press calls `Mixer.Unblock()` to retry music tracks blocked by browser autoplay policies on the web.

### Mapping view coordinates to a virtual canvas

When your game renders through a letterboxed [`screen.Viewport`](screen.md), set `MapPoint` so `TouchRegion` and `MouseRegion` bounds are evaluated in virtual game coordinates:

```go
actions.MapPoint = func(p core.Point) (core.Point, bool) {
	return viewport.ViewToGame(windowSize, p)
}
```

Returning `false` from `MapPoint` (for example, a tap on a letterbox bar) ignores that point for region tests.

## Hearing keys

A view hears keys only while it has the keyboard focus, and a click presses a view without focusing it, so a game usually hears them on the whole window instead. Call `WatchKeys` once, from the scope that runs the loop:

```go
actions.WatchKeys(cx)
loop.Run(cx)
```

`WatchKeys` follows NSGo's `core.KeysDown` and `core.KeyCodesDown` until the scope is disposed or the returned `stop` is called. Keys typed into a text input or composed by an input method are never held.

`Attach` also wires `OnKeyDown` and `OnKeyUp`, which hear keys while the view itself has the keyboard focus, such as a `Focusable` game view on a desktop. `OnKeyDown` consumes a key bound to an action, so the keymap's bindings and the view's own Return and Space do not take it too; an unbound key passes on. A held key's auto-repeat changes nothing.

Either way:

- A held key keeps its action `Pressed` on every step until it comes up, and a key that goes down and up between two steps is latched for one step, as a tap is.
- Two actions bound to opposite keys drive an axis with `BindAxis`: `Axis` is `-1` with the negative key held, `+1` with the positive one, and `0` with both or neither.
- Every held key comes up as the window loses the keyboard focus, firing `JustReleased` on the next step, so no action stays held while the player is in another window.
- `Reset` releases every held key, and a key still held then stays released until it comes up and goes down again.

A key held as the attached view leaves the tree stays held, because NSGo no longer sends that view its key-up: call `Reset` when you replace the game view, or use `WatchKeys`, which does not depend on a view.

`Key` takes the names `core.KeyEvent.Key` reports, matched without regard to case. The aliases `core.KeyHeld` also accepts, such as `"mod"`, `"cmd"` or `"option"`, are never held; bind `"meta"` where `Env.AppleKeys` reports Command as the platform modifier and `"ctrl"` elsewhere.

## Game controllers

Bind controller buttons with `GamepadButton` like any other `Input`, bind stick axes with `BindGamepadAxis`, and call `WatchGamepads` once so every `Step` reads the controllers connected to the app through NSGo's [`gamepad`](https://github.com/nsgo-dev/nsgo/tree/main/gamepad) package, on every target that has them (macOS, iOS, Windows, Linux, Android and the web):

```go
actions.Bind("jump", input.GamepadButton(gamepad.ButtonSouth), input.TouchRegion(jumpRect))
actions.Bind("left", input.GamepadButton(gamepad.ButtonDPadLeft))
actions.Bind("right", input.GamepadButton(gamepad.ButtonDPadRight))
actions.BindAxis("move_x", "left", "right")

// The left stick drives the same axis as the D-pad, with the default radial dead zone.
actions.BindGamepadAxis("move_x", gamepad.AxisLeftX, input.GamepadAxisOptions{})
// Pushing the right stick up reads positive.
actions.BindGamepadAxis("look_y", gamepad.AxisRightY, input.GamepadAxisOptions{DeadZone: 0.2, Invert: true})

actions.WatchGamepads(cx)
```

Buttons use the W3C Standard Gamepad layout; a button counts as pressed from 0.5, so the analog triggers `ButtonLT` and `ButtonRT` press halfway down. `Axis` adds a stick axis to anything else bound to the same name (`BindAxis`, `BindMotionAxis`, a `VirtualPad` stick) and clamps the sum to `[-1, 1]`.

| `GamepadAxisOptions` field | Effect |
| --- | --- |
| `DeadZone` | Deflection below which the axis reads 0; the rest is rescaled to rise smoothly from 0 to 1. `0` uses `DefaultGamepadDeadZone` (0.15), a negative value turns it off. |
| `Axial` | Applies the dead zone to this axis alone. By default it is radial over the whole stick, so a diagonal push keeps its direction. |
| `Sensitivity` | Multiplies the dead-zoned value before clamping (1 when `<= 0`). |
| `Invert` | Negates the value. |

By default an `Actions` reads every connected controller: a button is pressed when it is pressed on any of them, and a stick axis takes the reading farthest from center. Call `SelectGamepad(id)` with an ID from `gamepad.Pads` to restrict one `Actions` to a single controller, or `SelectAnyGamepad` to go back to every controller; for multi-player setups, use `Players` below. Disconnecting a controller releases what it held, and reading stops once the scope passed to `WatchGamepads` is disposed.

Controllers are polled once per step, so a press and release that both happen between two steps are not seen. Tests and replays can skip the live controllers and feed `[]gamepad.Pad` snapshots with `OnGamepads`, which latches presses between steps like touches; `Gamepads()` returns the state last read.

## On-screen touch controls

On a phone the player needs controls under their thumbs. `TouchControls` draws a layer of them from a `TouchLayout`, a YAML document, and drives the same named actions and axes the keyboard and game controllers do, so the game reads `Pressed("jump")` and `Axis("move")` whatever the player holds:

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/touchlayout.v1.json
version: 1
reference: {width: 844, height: 390}
maxScale: 1.5
idleAfter: 4s
idleAlpha: 0.45
controls:
  - id: move
    kind: dpad
    anchor: bottomLeft
    offset: {x: 10, "y": 8}
    size: {width: 120, height: 120}
    label: Move
    ways: 8
    left: left
    right: right
  - id: jump
    kind: button
    anchor: bottomRight
    offset: {x: 36, "y": 32}
    size: {width: 110, height: 110}
    label: Jump
    action: jump
    text: JUMP
```

```go
//go:embed controls.touch.yaml
var touchYAML []byte

layout, err := input.ParseTouchLayout(touchYAML)
if err != nil {
	return err
}
touch, err := input.NewTouchControls(cx, layout)
if err != nil {
	return err
}
touch.BindActions(actions)
touch.Haptics = input.NewHaptics(cx)

actions.Attach(&gameView)
touch.Attach(&gameView) // after Actions.Attach

// In Loop.Update, after actions.Step():
touch.Update(dt)

// In the game Canvas's Draw, which covers the view:
touch.Resize(size, cx.Env().SafeArea())
touch.Draw(d)

// When play starts or resumes after a menu the controls were not drawn under:
touch.Wake()
```

What the layer does:

- **Placement.** Each control sits against one of nine anchors of the safe area (`topLeft`, `top`, `topRight`, `left`, `center`, `right`, `bottomLeft`, `bottom`, `bottomRight`), moved inward by `offset`. Offsets and sizes are points on a screen of `reference` size and scale by the smaller of the safe area's width and height over the reference's, capped by `maxScale` when set, so the layout keeps its proportions on any screen. No control is ever smaller than `MinTouchTarget` (44 points), and none leaves the safe area. Pass `Env.SafeArea`, or `core.Insets` of the view's scope where a screen or modal keeps part of it clear, to `Resize`; the controls work in the view's points, so they ignore `Actions.MapPoint`.
- **Visibility.** The controls start visible when `Env.Pointer` is `core.PointerCoarse` (phones and tablets) and hidden elsewhere. The first touch on the attached view, or a press on a control, shows them; a key or a game controller hides them again, heard through the view's key events and, on `Update`, the keys `WatchKeys` reads and the controllers `WatchGamepads` reads. `Tracker` returns the `DeviceTracker` behind this, and `SetVisible` overrides it; after either, the next key or controller still hides the controls and the next touch still shows them.
- **Idle fade.** With `idleAfter` set, the controls fade to `idleAlpha` once no finger has touched them for that long, and return to full opacity on the next press. An `idleAlpha` of 0, the default, fades them out of sight while they still take touches and still name themselves for screen readers. The idle time runs whether or not the game draws the controls, so call `Wake` when they come back into view, such as when play resumes from a menu, or they first appear already faded.
- **Left-handed play.** `mirrored: true`, or `SetMirrored(true)` at run time, swaps every control between the left and right of the screen; the directions keep their meaning.
- **Haptics.** With `Haptics` set, each press plays a selection tick: a touch-down on a button, a finger sliding onto another button, or a d-pad rocking into a new direction.
- **Accessibility.** `Draw` names each control as a canvas node with its `label`, the role of a button for buttons, and the test ID `touch-` followed by its `id`, so screen readers and `nsgotest` find them, at any opacity. `Controls` lists each control's ID, kind, label and frame.
- **Player edits.** `Layout` returns the layout, `SetLayout` applies an edited one, and `Save` and `Load` keep it in `core.Storage` as the same YAML; `Load` leaves the layout as it is and returns a `*loaderr.Error` naming the key when the saved one does not parse.

### Control kinds

| Kind | Builds | Fields |
| --- | --- | --- |
| `button` | `VirtualButton` | `action` (required), `text` or `icon`, `mode` (`hold`, `toggle`, `tap`), `shape` (`circle`, `rect`) |
| `dpad` | `VirtualDPad` | `up`, `down`, `left`, `right` actions, `axisX` and `axisY` axes, `ways` (4 or 8), `deadZone` |
| `stick` | `VirtualStick` with `UpPositive` | `axisX` and `axisY` axes, `deadZone`, `floating`, `radius` |

Every kind takes `id`, `kind`, `anchor`, `offset`, `size` and `label`; the three kinds above refuse a field they do not take. Button modes:

- `hold` presses the action while a finger is on the button.
- `toggle` flips it on each touch-down and keeps it after the finger lifts, until the next touch-down or `Reset`.
- `tap` presses it for exactly one `Actions.Step` per touch-down, however long the finger stays.

A d-pad belongs to one finger as a whole: the thumb rocks or slides between directions without lifting, and leaving the circle keeps steering. With `ways: 8` a diagonal presses both of its directions. `axisY` reads `+1` up, as a controller's d-pad does. Inside the layer a finger that slides off one button onto another presses the second one, so a thumb rocks between neighbouring buttons; a toggle button is never entered that way.

### Plugin control kinds

A package adds a kind of its own with `RegisterTouchControlKind`, from an `init` function. Its factory gets the control's `TouchControlSpec`, reads the common fields it uses and its own settings from `options`, and returns a `TouchControl`: a `VirtualControl` that `TouchControls` also places.

```go
func init() {
	input.RegisterTouchControlKind("acme.wheel", func(spec input.TouchControlSpec) (input.TouchControl, error) {
		steer := spec.Options["axis"]
		if steer == "" {
			return nil, loaderr.Errorf("options.axis", "name the axis the wheel drives", "a wheel needs options.axis")
		}
		return &wheel{axis: steer, action: spec.Action}, nil
	})
}
```

```yaml
  - id: wheel
    kind: acme.wheel
    anchor: bottomLeft
    size: {width: 160, height: 160}
    label: Steering wheel
    options: {axis: steer, gain: "1.5"}
```

`options` values are strings. Quote a value YAML would otherwise read as a number, a boolean or null, as `gain` above; `ParseTouchLayout` refuses an unquoted one at its line, since it would read back as a string and save quoted, changing the file.

`VirtualControl` has `Claim` (take a finger that touches down at a point), `Move` and `Release` (follow it until it lifts), `Reset`, `ActionNames`, `ActionPressed` and `AxisValues` (what it drives), and `Draw`; `TouchControl` adds `Place`, which receives the frame and scale `TouchControls` computes. `ParseTouchLayout` accepts any kind, so a layout parses before the plugin that defines its kind is imported; `NewTouchControls` refuses a kind no one registered, and reports a factory's `*loaderr.Error` against the control's own field. `RegisterTouchIcon` adds an icon a button's `icon` can name, beside the built-in `arrowUp`, `arrowDown`, `arrowLeft` and `arrowRight`. Both panic on an empty or repeated name.

Two optional interfaces give a plugin's control what the built-in kinds get:

- `ButtonControl` adds `Button() *VirtualButton`, for a control that is a `VirtualButton` underneath, such as a custom-drawn or cooldown button. The pad then drives that button itself: a finger touching down on it is claimed before sticks and other controls, slides onto and off it with its neighbours, and a `ButtonTap` press ends after one step; `TouchControls.Button` returns it, and `Draw` names it as a button. The pad does not call the control's `Claim`, `Move` and `Release` for its fingers.
- `AccessibleControl` adds `Accessibility() (core.Role, bool)`, the role and selected state `Draw` gives the control's canvas node in place of a button's or a group's, such as `core.RoleSwitch` and whether it is on.

### Virtual controls without a layout

The pieces `TouchControls` builds work alone on a `VirtualPad`, which hands each finger to the control it touched down on and keeps it there until it lifts:

```go
pad := input.NewVirtualPad()
pad.SlideBetweenButtons = true
pad.AddStick(&input.VirtualStick{AxisXName: "move_x", AxisYName: "move_y", UpPositive: true, Zone: leftHalf, Radius: 56, Floating: true})
pad.AddControl(&input.VirtualDPad{Center: dpadCenter, Radius: 70, Ways: 8, Left: "left", Right: "right"})
pad.AddButton(&input.VirtualButton{Center: jumpCenter, Radius: 48, Label: "JUMP", Action: "jump"})
pad.AddButton(&input.VirtualButton{Center: crouchCenter, Radius: 36, Label: "C", Action: "crouch", Mode: input.ButtonToggle})
pad.BindActions(actions)
```

- `VirtualStick` reads `AxisY` positive downward, in screen coordinates, unless `UpPositive` is set; set it so a touch stick and a game controller's inverted stick agree on one axis name.
- `SlideBetweenButtons` is off by default on a plain `VirtualPad` and on in `TouchControls`.
- `OnPress` is called on each press, for a haptic tick.
- A `ButtonTap` button's press ends at the next `Actions.Step`, so it needs the pad bound to an `Actions`.

## Local multiplayer

`Players` manages several player slots (`0` to `Len()-1`), each with its own `Actions`, routing every connected game controller to at most one player and forwarding keyboard or pointer events only to the players assigned to them:

```go
players := input.NewPlayers(2)
for _, p := range players.All() {
	a := p.Actions()
	a.Bind("jump", input.GamepadButton(gamepad.ButtonSouth))
	a.Bind("left", input.GamepadButton(gamepad.ButtonDPadLeft))
	a.Bind("right", input.GamepadButton(gamepad.ButtonDPadRight))
	a.BindAxis("move_x", "left", "right")
	a.BindGamepadAxis("move_x", gamepad.AxisLeftX, input.GamepadAxisOptions{})
}

players.AssignGamepad(0, pad1ID)
players.AssignGamepad(1, pad2ID)
players.WatchGamepads(cx)

// In Loop.Update:
players.Step()
p0Move := players.Player(0).Axis("move_x")
p1Jump := players.Player(1).JustPressed("jump")
```

- **Device isolation and exclusivity**: `AssignGamepad(index, padID)` assigns controller `padID` exclusively to player `index` (if another player owned `padID`, that player loses it and releases any held buttons). Assigning a controller ID before it connects remembers the slot; `p.GamepadConnected()` and `p.Gamepad()` report whether the assigned controller is currently present.
- **Split keyboard and mixed devices**: Call `AssignKeyboard(index)` on one or more players and `WatchKeys(cx)` (or `Attach(&view)`) on `Players`. Several players can share one keyboard with disjoint key sets (such as WASD + Space for player 0 and arrow keys + Enter for player 1), or one player can use keyboard while another uses a gamepad — a gamepad-only player never reacts to keyboard events even when both players share the same binding template. Call `AssignPointer(index)` to route touch, mouse, and pointer-lock events to specific players (such as split-screen touch regions or player 0 on keyboard + mouse).
- **Auto-assignment and drop-in lobbies**: Call `AssignUnassignedGamepads()` to assign unassigned connected controllers (in ascending ID order) to open slots with no device (`!p.HasDevice()`), or inspect `UnassignedGamepads()` and `NextUnassigned()` to implement press-to-join lobbies. Add slots dynamically with `Add()`, and clear assignments with `UnassignGamepad`, `UnassignKeyboard`, `UnassignPointer`, or `Unassign`.
- **Single-step polling, reset, and persistence**: `players.Step()` polls `gamepad.Pads` once and steps every player's `Actions`; `players.Reset()` resets every player while preserving slots, bindings, and device assignments; tests and replays can feed snapshots with `players.OnGamepads(pads)`. Save and load each player's bindings independently through `p.Actions().Save` and `p.Actions().Load`.

### Controller disconnect, reconnect, and fallback

Configure `Players.SetDisconnectPolicy` to handle controllers that disconnect mid-game cleanly for both single-player (`NewPlayers(1)`) and local multiplayer games:

```go
players.SetDisconnectPolicy(input.DisconnectPolicy{
	PauseOnDisconnect:    true,
	AllowReassignOnPress: true,
	OnPause:              loop.SetPaused,
	OnDisconnect: func(ev input.DisconnectEvent) {
		showReconnectPrompt(ev.PlayerIndex)
	},
	OnReconnect: func(ev input.ReconnectEvent) {
		if !ev.ShouldPause {
			hideReconnectPrompt()
		}
	},
})
```

- **Clean release and pause**: When a player's previously connected controller disappears from the snapshot, `Players` releases that player's held buttons and stick axes immediately on the step, remembers the lost controller ID, emits `OnDisconnect` (`DisconnectEvent`), and calls `OnPause(true)` when `PauseOnDisconnect` is set (and fallback is not active). While `WatchGamepads` has a live scope and any player is awaiting reconnect, `Players` polls controllers on display frames even while `game.Loop` is paused, so reconnecting or pressing a button on another pad resumes the loop automatically. Call `PollGamepads()` directly if you drive polling outside `game.Loop`.
- **Reconnect and press-to-reassign**: Reconnecting the same controller automatically reclaims the player's slot and emits `OnReconnect` (`Reassigned: false`). When `AllowReassignOnPress` is enabled, newly pressing any button on an unassigned connected controller assigns it to the lowest-index disconnected player slot and emits `OnReconnect` (`Reassigned: true`). Once every disconnected required player has recovered (or been manually reassigned/unassigned), `Players` calls `OnPause(false)`.
- **Single-player keyboard/touch fallback**: Set `FallbackToKeyboardTouch: true` so a controller disconnect continues without pausing (`DisconnectEvent.FallbackActive == true`, `ShouldPause == false`). `Players` temporarily enables keyboard and pointer/touch routing on that player (preserving any pre-existing keyboard or pointer assignments) and switches back cleanly when the original controller reconnects or another controller claims the slot.
- **State queries**: Inspect `p.GamepadDisconnected()`, `p.FallbackActive()`, `players.DisconnectedPlayers()`, and `players.PausedByDisconnect()` to drive HUD prompts or pause overlays.

## Sampling once per fixed step

Call `actions.Step()` once at the start of `game.Loop.Update`, then query `Pressed`, `JustPressed`, `JustReleased`, and `Axis` for that step's snapshot:

```go
loop := &game.Loop{
	Rate: 60,
	Update: func(dt time.Duration) {
		actions.Step()

		vx := float32(actions.Axis("move_x")) * 140
		if actions.JustPressed("jump") {
			startJump()
		}
		if actions.JustReleased("jump") {
			cutJumpShort()
		}
		movePlayer(vx, dt)
	},
	Draw: func(alpha float64) {
		frame.Set(alpha)
	},
}
```

- **Tap latching**: a brief tap that both starts and ends between two fixed steps is latched so `Pressed` and `JustPressed` still register for one step, followed by `JustReleased` on the next step.
- **`Axis(name)`**: returns `-1` when only the negative action is pressed, `+1` when only the positive action is pressed, and `0` when neither or both are pressed. For an unbound axis name, it falls back to `1` if an action of that name is pressed, else `0`.
- **`Reset()`**: clears active touches, held buttons, held keys, the last controller state read, pending latches, relative mouse motion, latched motor-assist toggles and the current snapshot, while keeping all bindings, assist configuration and any pointer lock, so mouselook survives a scene change. A controller button held across `Reset` stays inactive until it is released, as a held mouse button does. Call it on focus loss, backgrounding, or scene changes so inputs never stick.

## Player motor assists

Configure player-chosen motor accessibility assists on `Actions` (or per `Player` in local multiplayer) with `SetAssists`:

```go
// Declare which actions may be toggled or mashed (or declare assist: in game.yaml).
actions.SetActionAssist("sprint", input.ActionAssist{Toggle: true})
actions.SetActionAssist("struggle", input.ActionAssist{Toggle: true, Mash: true, MashRate: 12})
actions.SetActionAssist("pause", input.ActionAssist{}) // menu action is never latched

actions.SetStepRate(loop.StepRate())
actions.SetAssists(input.Assists{
	Toggle:      []string{"sprint"},
	MashToHold:  []string{"struggle"},
	MinPress:    100 * time.Millisecond,
	AcceptDelay: 500 * time.Millisecond,
})

// Release latched toggles when the game pauses.
loop.OnPause = actions.SetPaused
```

| `Assists` field | Behavior |
| --- | --- |
| `Toggle` | Actions that latch pressed on the first accepted press and release on the next accepted press (hold-to-toggle). |
| `MashToHold` | Rapid-press actions that emit repeated 1-step presses at the action's declared `MashRate` (`DefaultMashRate`, 10 presses/sec when unset) while held, or continuously after a single press when also listed in `Toggle`. |
| `MinPress` | Minimum continuous hold duration before a press counts (slow keys); brief accidental taps shorter than `MinPress` are ignored. |
| `AcceptDelay` | Cooldown after an accepted press of an action during which further presses of that action are ignored (bounce keys), without interrupting an ongoing hold. |

- **Allowlist via `ActionAssist` and `game.yaml`**: Actions declare `assist: {toggle: true, mash: true, mashRate: 12}` in `game.yaml` (applied by `project.File.BindInput`) or via `SetActionAssist`. Once any action declares assist metadata, `Toggle` and `MashToHold` apply only to actions that opt in, so menu or pause actions are never latched. Inspect declared actions with `ToggleableActions()` and `MashableActions()`.
- **Fixed-step evaluation and replays**: All four assists run inside `Step()` using the fixed step rate (`SetStepRate`, defaulting to `DefaultStepRate`, 60 Hz). Synthetic `MashToHold` pulses always alternate through a released step between presses so `JustPressed` fires on every pulse and `replay.Recorder` / `replay.Player` reproduce identical snapshots.
- **Pause, reset, and persistence**: Wire `loop.OnPause = actions.SetPaused` (or `players.SetPaused`) or call `ReleaseToggles()` to release latched toggles on pause; `Reset()` clears all latched toggles and assist timers on focus loss or scene change. `ToggleActive(name)` reports whether an action is currently latched on. `Actions.Save` and `Actions.Load` persist `Assists` alongside bindings, and `save.Settings.InputAssists` persists them with player settings (see [a11y](a11y.md#player-motor-assists)).

### Waking an idle `OnDemand` loop

When driving a menu-heavy, turn-based or narrative game with an idle [`game.Loop`](loop.md#on-demand-drawing-and-idling-ondemand-idle-markdirty) (`OnDemand: true` and `Idle`), set `actions.Wake = loop.MarkDirty` so touches, mouse presses, keys, connected game controller changes and injected triggers wake the loop for a `Step` and redraw without waking on idle pointer hovers or stick jitter inside the dead zone. `TouchControls` and `GestureRecognizer` wake through a bound `Actions` automatically, or through `TouchControls.OnWake` and `GestureRecognizer.Wake` when used standalone.

## Mouselook through pointer lock

For a first-person camera, bind pointer axes to relative mouse motion, lock the pointer from a press on the game view, and read each step's motion with `Delta`:

```go
var gameRef core.Ref

actions.BindPointerAxis("yaw", input.PointerX, input.PointerAxisOptions{Sensitivity: 0.003})
actions.BindPointerAxis("pitch", input.PointerY, input.PointerAxisOptions{Sensitivity: 0.003, Invert: true})

view := core.View{
	Ref: &gameRef,
	OnPress: func(core.PressEvent) {
		actions.LockPointer(cx, &gameRef)
	},
}
actions.Attach(&view)

// In Loop.Update, after actions.Step():
camera.Yaw += actions.Delta("yaw")
camera.Pitch += actions.Delta("pitch")
```

- `PointerX` reads motion to the right and `PointerY` motion down, in the units the view's `OnPointerDelta` reports. `Sensitivity` scales them (1 when zero) and `Invert` negates them.
- `Delta(name)` returns the scaled motion heard since the previous `Step`, without a clamp, so a fast flick turns the camera further than a slow one. `PointerDelta()` returns the raw `(dx, dy)` of the step. `Axis(name)` adds the scaled motion to any other axis bound to `name` and clamps the sum to `[-1, 1]`, which suits steering with a small `Sensitivity`.
- The view hears motion only while it holds the lock: `Attach` forwards its `OnPointerDelta` to `actions.OnPointerDelta`.
- `LockPointer(cx, &ref)` returns `false` and does nothing when `cx.Env().CanLockPointer()` is false or the view has been removed. Call it from every press, since browsers grant the lock only during a user gesture; locking again replaces the previous lock. Linux on Wayland needs a compositor with pointer constraints and relative motion.
- `UnlockPointer()` releases the lock, and the platform shows the pointer again where it was. Motion heard before the release still reaches the next step. `Reset()` keeps the lock.
- Escape, the window losing focus and hiding or removing the view also release the lock, but NSGo does not report that to the app. `PointerLockRequested()` therefore reports only whether `actions` holds a request that `UnlockPointer` has not released, not whether the platform still holds the lock, so do not use it to decide whether to lock again or to show a resume prompt. The next press locks again.

Pointer axes are saved and loaded with the other bindings.

## Motion sensors (tilt and gyroscope)

Bind accelerometer/gravity tilt (`TiltX`, `TiltY`, `TiltZ`) and gyroscope rotation rate (`GyroX`, `GyroY`, `GyroZ`) to named axes with `BindMotionAxis`, then subscribe to NSGo's motion stream with `WatchMotion`:

```go
actions.BindMotionAxis("steer", input.TiltX, input.MotionAxisOptions{DeadZone: 0.08, MaxRange: 0.6})
actions.BindMotionAxis("pitch", input.TiltY, input.MotionAxisOptions{MaxRange: 0.5})
actions.BindMotionAxis("yaw", input.GyroZ, input.MotionAxisOptions{MaxRange: math.Pi})

motion, err := actions.WatchMotion(cx, input.MotionOptions{
	Orientation: input.MotionOrientationLandscapeLeft,
	Smoothing:   0.25,
	Interval:    16 * time.Millisecond,
})
if err != nil {
	return err
}

// Stop sensors while the game is paused and resume them with calibration intact.
loop.OnPause = motion.SetPaused
```

- **Screen orientation**: NSGo delivers motion vectors in the device's right-handed natural frame (`+X` right, `+Y` up, `+Z` out of the screen in portrait). Set `MotionOptions.Orientation` to rotate both tilt and gyro vectors into screen axes before smoothing and calibration:
  - `MotionOrientationPortrait` (zero value, default): natural portrait frame (`+X`, `+Y`, `+Z`).
  - `MotionOrientationPortraitUpsideDown`: rotated 180° (`-X`, `-Y`, `+Z`).
  - `MotionOrientationLandscapeLeft`: rotated 90° counter-clockwise, device top on the screen's left (`-Y`, `+X`, `+Z`), matching NSGo's `core.OrientationLandscapeLeft` on Android, HarmonyOS and Web.
  - `MotionOrientationLandscapeRight`: rotated 90° clockwise, device top on the screen's right (`+Y`, `-X`, `+Z`).
  - `MotionOrientationAuto`: infers portrait vs. landscape from `cx.Env().Window()`'s aspect ratio (or `SetWindowSize` when used without a scope). Because NSGo does not yet report which landscape side is active, a wider-than-tall window uses `MotionOptions.LandscapeSide` (defaulting to `MotionOrientationLandscapeLeft`, or set to `MotionOrientationLandscapeRight`). Call `SetOrientation` to change orientation at runtime and `Orientation()` to inspect the resolved orientation; changing the resolved orientation resets smoothing history cleanly.
- **Calibration**: Call `motion.Calibrate()` (or `actions.CalibrateMotion()`) to record the player's current neutral hold angle in screen axes so `TiltX`, `TiltY`, and `TiltZ` read `0` at that hold. If no tilt sample has arrived yet, the next tilt sample calibrates automatically. `ResetCalibration` clears the neutral offset.
- **Pausing and backgrounding**: NSGo pauses `WatchMotion` automatically while the app is in `core.LifecycleBackground` and stops it when `cx` is disposed. Wire `loop.OnPause = motion.SetPaused` so sensors also stop while a pause menu is open (clearing live and snapped readings while preserving calibration and options) and restart automatically when play resumes. Calling `Stop()` ends watching permanently until `Start(cx)` is called again.
- **Combining and testing**: Motion axes sum with `BindAxis`, `BindGamepadAxis`, `BindPointerAxis`, and `VirtualPad` sticks sharing the same name (clamped to `[-1, 1]`), and persist through `Save` / `Load`. In unit tests or replays without live sensors, construct `NewMotionInput` (or call `actions.OnMotion`) and feed `core.MotionSample` values directly.

## Saving and loading bindings

Persist remapped bindings, including controller buttons and stick axes, in `core.Storage` as JSON with `Save` and `Load`:

```go
// Save current action and axis bindings
if err := actions.Save(cx.Storage(), "controls.v1"); err != nil {
	// Handles *core.StorageFullError
	log.Println("save controls:", err)
}

// Bind defaults first, then merge the player's saved bindings on startup
found, err := actions.Load(cx.Storage(), "controls.v1")
if err != nil {
	log.Println("corrupt saved controls, keeping defaults:", err)
}
_ = found
```

`Load` merges per name across actions, opposite-action axes, motion axes, pointer axes and gamepad axes: any name present in the saved value replaces that name's binding, while names absent from the saved value keep their current (default) bindings. That way, controls added in a newer build stay bound for players loading an older save.

Explicit unbindings made with `Unbind`, `UnbindAxis`, `UnbindMotionAxis`, `UnbindPointerAxis` or `UnbindGamepadAxis` (or `Bind` with no inputs) are written by `Save` (`[{"kind":"none"}]` for an action, `{}` for an opposite-action axis, and `{"axis":"none"}` for a motion, pointer or gamepad axis), so `Load` keeps them unbound rather than restoring the default. Call `LoadReplace` instead when you want to replace all bindings wholesale with the saved snapshot and drop any binding absent from it.

When `key` is absent, both `Load` and `LoadReplace` return `(false, nil)` and leave existing default bindings intact. If the stored JSON is invalid, has a field they do not know or uses an unknown format, they leave bindings untouched and return `(false, err)`.
## Player control preferences

Persisted `save.ControlSettings` values describe look sensitivity per device, axis inversion, stick dead zones, trigger thresholds, and vibration strength. Call `input.ApplyControlSettings` after loading settings to update existing gamepad axis bindings. `input.Haptics.SetVibrationScale` applies the vibration preference to rumble output.
