### Added

- `pkg/importsettings`, `pkg/tooling/pack`, `pkg/tooling/assetbuild`, `pkg/sprite`, `pkg/sceneformat`, `pkg/editor`: trace per-frame 2D convex collision polygons from sprite alpha and bake named image points (`muzzle`, `hand`, `feet`) into packed atlas metadata, read Aseprite slice pivots, add `Sprite.PointWorld` / `Sprite.ShapesWorld`, add `collider2d` / `trigger2d` `shape: sprite` following frame changes, flips, scale, and rotation, and overlay traced outlines and draggable points in the editor's image import inspector (#1758).
