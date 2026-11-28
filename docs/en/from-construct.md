# Coming from Construct

Construct's event sheets, layouts and behaviors map to explicit Go systems and text scene files in NSGo.

## Layouts and event sheets

Store layouts as scenes and prefabs. Use `sceneformat` connections and rules for declarative conditions and actions, and use `pkg/flow` for dialogue, maps and resumable command sequences. Move simulation that needs type safety or performance into Go systems.

## Behaviors and input

Use the reusable motion behaviors in `sceneformat` for common movement, and `pkg/input.Actions` for named actions and axes across keyboard, touch, mouse and gamepad. `pkg/gameui` provides navigable widgets and settings screens.

## Runtime and export

`game.Loop` provides deterministic fixed-step updates. `pkg/assets` keeps source assets text-addressable and supports hot reload. Declare export presets in `game.yaml`, and validate with `go tool nsgo check` before shipping.

