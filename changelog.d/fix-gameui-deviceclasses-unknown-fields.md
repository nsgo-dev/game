### Fixed

- `game/gameui`: reject disjoint nested `deviceClasses` and keep empty intersections hidden across every device class and `SetDeviceClass`, and reject screen node fields unsupported by the node's `type` in `ParseScreen` and `ValidateScreen` (#1581).
