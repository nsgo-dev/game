### Added

- `game/scenemodel`: scene file objects draw in 3D. `Build` builds a scene into a `scene3d` tree as `BuildNodes3D` does and attaches to each object's node the geometry and materials of its built-in primitive or glTF model, with `LocalBounds` from the geometry; `Assets` loads models, material files and textures from a project's files, `Built.AppendDrawItems` feeds `render3d.Queue`, and `Renderer` draws the parts lit and textured (#889).
- `game/sceneformat`: version 2 objects and overrides name a glTF 2.0 model in `model`, by its path from the project root, optionally narrowed to one node (`modelNode`) or mesh (`modelMesh`); an object's `material` replaces the model's materials (#889).
- `game check` reports a missing or broken model, and a `modelNode` or `modelMesh` the model does not have, with the scene file and line; `game mv`, `game index` and the editor's `MoveFile` rewrite the scenes and overrides that name a moved model, and the `.gltf` files whose separate buffers and images move, keeping their URIs percent-encoded (#889).
- Lantern 3D loads its level from a scene file, `assets/shrine.scene.yaml`, built with `scenemodel.Build` (#889).

### Changed

- `game/sceneformat`: an object's `mesh` must name a built-in primitive, `box`, `sphere`, `plane`, `capsule` or `cylinder` (`MeshPrimitives`), and cannot be combined with `model`; it was free text no loader read (#889).
- `game/editor`: the 3D inspector's Mesh field picks a built-in primitive from a list, and `SetObjectMesh` refuses any other name and a primitive on an object that draws a model (#889).
