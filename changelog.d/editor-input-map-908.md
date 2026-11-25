### Added

- `game/project`: add `key`, `keyCode`, and `gamepadButton` action bindings plus `gamepadAxes` analog stick axes in `game.yaml`, validated, applied by `BindInput`, and published in `game.v1.json` (#908).
- `game/editor`: add an Input Map panel (`InputMapPanelView`) over `game.yaml`'s `input` section with undoable `EditProject` editing, action/binding CRUD, press-a-key capture, interactive touch/mouse region drawing and dragging over the virtual display size, axis/motion/gamepad stick editing, and action rename propagation to axes (#908).
