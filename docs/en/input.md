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
- **`Reset()`**: clears active touches, held buttons, held keys, the last controller state read, pending latches, relative mouse motion and the current snapshot, while keeping all bindings and any pointer lock, so mouselook survives a scene change. A controller button held across `Reset` stays inactive until it is released, as a held mouse button does. Call it on focus loss, backgrounding, or scene changes so inputs never stick.

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

## Saving and loading bindings

Persist remapped bindings, including controller buttons and stick axes, in `core.Storage` as JSON with `Save` and `Load`:

```go
// Save current action and axis bindings
if err := actions.Save(cx.Storage(), "controls.v1"); err != nil {
	// Handles *core.StorageFullError
	log.Println("save controls:", err)
}

// Restore on startup (leaves defaults untouched when key is absent)
found, err := actions.Load(cx.Storage(), "controls.v1")
if err != nil {
	log.Println("corrupt saved controls, keeping defaults:", err)
}
_ = found
```

When `key` is absent, `Load` returns `(false, nil)` and leaves existing default bindings intact. If the stored JSON is invalid, has a field `Load` does not know or uses an unknown format, `Load` leaves bindings untouched and returns `(false, err)`.
