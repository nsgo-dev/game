### Added

- Scene objects now support a version 2 `gridMap` field referencing `*.gridmap.yaml` files (including scene overrides and `scene.v2.json` schema), instanced in `scenemodel.Build` via `world3d.GridMap.BuildInstances` with shared `MeshID`/`MaterialID` and validated/tracked by `game check`, `refs`, and `MoveFile`.
- `world3d.LoadMeshLibraryFromGLTF` now reads binary `.glb` mesh libraries in addition to `.gltf`, and `world3d` adds `RotateGridOrientation`, `FindGridOrientation`, `MeshLibrary.Items`, and `GridMap.EachCell`.
- The 3D scene editor adds interactive GridMap painting: a mesh-library palette with 3D thumbnails, Paint/Erase/Pick/Box Fill tools on the active floor height (`]`/`[`/`PageUp`/`PageDown`), 24-orientation rotation keys (`X`/`Y`/`Z`/`Q` + Shift), live hovered cell and box-fill previews, full undo/redo, and canonical `.gridmap.yaml` saving.
