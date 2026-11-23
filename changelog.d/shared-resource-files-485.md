### Added

- `game/resource`: shared resource files in YAML with a common `type`/`version` header (materials, curves, gradients, animation clips, UI themes, sound bus layouts and physics materials), a `Library` that gives every user of a path one value and reloads it in place, `material` references on `sceneformat` objects, and `hotreload.Watcher.WatchResources` (#485).
