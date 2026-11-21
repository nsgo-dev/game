# input

`game/input` turns touch regions and mouse buttons into named actions and axes, sampled once per `game.Loop` step and persisted across launches in `core.Storage`.

```go
import "github.com/nsgo-dev/game/packages/game/input"
```

All `Actions` methods run on the engine's goroutine, alongside `core.View` callbacks and `game.Loop.Update`.

## Defining actions and axes

An `Actions` instance maps logical names (such as `"jump"`, `"left"`, `"right"`, or `"move_x"`) to one or more physical `Input` triggers. Its zero value is ready to use, or construct one with `input.NewActions()`:

```go
actions := input.NewActions()

// Touch zones in virtual canvas coordinates (e.g. 320×180)
actions.Bind("left",
	input.TouchRegion(core.Bounds{X: 0, Y: 100, Width: 70, Height: 80}),
)
actions.Bind("right",
	input.TouchRegion(core.Bounds{X: 70, Y: 100, Width: 70, Height: 80}),
)
actions.Bind("jump",
	input.TouchRegion(core.Bounds{X: 220, Y: 100, Width: 100, Height: 80}),
	input.MouseButton(core.MouseButtonLeft),
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

You can also forward individual callbacks manually via `OnTouch`, `OnPressDown`, `OnPressUp`, `OnMouseMove`, `OnMouseLeave`, and `OnContextMenu`.

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
- **`Reset()`**: clears active touches, held buttons, pending latches and the current snapshot while keeping all bindings. Call it on focus loss, backgrounding, or scene changes so inputs never stick.

## Saving and loading bindings

Persist remapped bindings in `core.Storage` as JSON with `Save` and `Load`:

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

When `key` is absent, `Load` returns `(false, nil)` and leaves existing default bindings intact. If the stored JSON is invalid or uses an unknown format, `Load` leaves bindings untouched and returns `(false, err)`.
