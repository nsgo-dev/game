### Added

- `game/gltf`, `game/tooling/importsettings`, `game/sceneformat`, `game/scenemodel`, `game/editor`: add 3D model LOD levels (declared in `*.import.yaml` or via `_LODn` node/mesh names), scene object occluders (`mode: model` and `mode: box`), automatic `DrawItem.LODs` and `OcclusionBuffer` wiring in `scenemodel`, and 3D viewport LOD/occluder visualization with `RenderStats` overlay in the editor (#904).
