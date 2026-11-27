### Added

- `sceneformat.Signals.Listen` subscribes a Go function to a scene object's events at run time — one object, or every object that declares the event — with once and deferred options as Godot's `CONNECT_ONE_SHOT` and `CONNECT_DEFERRED`, an idempotent `stop` that is safe inside a handler, and the same destroyed-entity rules as connections (#1362).
- `sceneformat`: the built-in `timer` component (`RegisterTimerComponent`, in the editor's inspector and the scene JSON schemas) emits `timeout` on the game clock through `Signals.Update`, with `seconds`, `oneShot` and `autostart` fields and `start` and `stop` handlers, so a scene wires a timer to any handler with no Go code (#1362).
