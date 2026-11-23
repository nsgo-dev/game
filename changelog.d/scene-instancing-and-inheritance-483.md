### Added

- `game/sceneformat`: scene format version 2 lets an object instance another scene file (`scene`, with per-object `overrides`) and a scene inherit a base scene (`inherits`, `overrides`, `layerOverrides`); `Resolve` loads them through a `SceneResolver` such as `FSScenes`, refuses cycles and returns a self-contained copy, `Build` resolves scenes itself, and `SceneDoc.Clone` and `ObjectDoc.Clone` deep copy documents (#483).
- `game/editor`: `PlaceScene` places an instance of a scene file as an undoable command, and `PlaceObject` refuses an instance that does not resolve or, with the new `Editor.ScenePath` set, leads back to the open scene (#483).

### Changed

- `game/sceneformat`: `CurrentVersion` is 2; version 1 scenes still load and save unchanged, and `SpawnEntities` and `BuildNodes3D` refuse a scene that still instances or inherits others until it is resolved (#483).
