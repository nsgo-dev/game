# Agent Note: Scene file objects draw glTF models and built-in primitives

Status: implemented

## Problem

A scene file could place objects in 3D (`posZ`, `rotX`/`rotY`/`rotZ`, `scaleZ`) and `sceneformat.BuildNodes3D` turned it into a `scene3d` tree, but an object could not say what it looks like: `mesh` was free text that no loader read, `game check` and `game mv` ignored it, and the tree carried no geometry. A 3D level therefore had to be assembled in Go, against the repository rule that every file a person writes is reviewable text.

## Decision

- **Format** (`sceneformat.ObjectDoc`, `ObjectOverride`): `mesh` names one of five built-in primitives (`box`, `sphere`, `plane`, `capsule`, `cylinder`; `sceneformat.MeshPrimitives`) and is refused otherwise. Version 2 adds `model`, a `.gltf` or `.glb` path from the project root, narrowed optionally by `modelNode` or `modelMesh` (first match by name, never both). `mesh` and `model` exclude each other. An object's `material` file replaces every material of its model or is the surface of its primitive. Overrides can set each field; `modelNode` and `modelMesh` without `model` are refused only on whole objects, since an override may narrow a model the base object names. An override that swaps an inherited primitive for a model writes `mesh: ""`, which the override schema's enum allows, because `Resolve` validates the merged object. A version 1 file hears about the version before any model rule, and its unknown-primitive hint says to move to version 2 rather than to use `model`, which version 1 refuses. `CheckObjectModel` exposes the same checks for one object, and the editor's `PlaceObject` runs it.
- **Building lives in a new package, `game/scenemodel`**, not in `sceneformat`. `Build(doc, parent, models ModelResolver)` runs `BuildNodes3D`, then attaches each object's geometry as a `*Drawable` (attachment `scenemodel.AttachmentKey`) with `LocalBounds` from the geometry. A whole model becomes child nodes named after its glTF nodes, so code can animate or hide model parts through `scene3d`. Parts share `MeshID` and `MaterialID` values for identical geometry and materials, matching `render3d.DrawItem`. Resolvers follow the `MaterialResolver` pattern: `ModelResolver` is required, `sceneformat.MaterialResolver` and `ImageResolver` are discovered by type assertion on the same value, and `Assets` implements all three over an `fs.FS`.
- **Drawing**: `Built.AppendDrawItems` feeds a caller's `render3d.Queue`; `Renderer` is a complete forward renderer (lit PBR with base color texture, cutout, double-sided and blended pipelines) whose output is pinned by a golden image under `gpu.NewSoftwareRenderer`.
- **References**: `refs` treats `model` in objects and overrides as a root-relative reference, and reads `.gltf` files' `buffers[*].uri` and `images[*].uri` as file-relative references, skipping data URIs and percent-decoding the rest; a rewrite re-encodes the new name and keeps the old spelling when the decoded path is unchanged. `assetcheck` loads each named model once with `gltf.LoadFS`, reports a missing `modelNode` or `modelMesh` at the scene line, checks every override that sets `model`, `modelNode` or `modelMesh` (an instance's or a scene-level one of an inheriting scene) against the resolved object at the override's line, leaving the file of a model the override does not name to the scene that names it, and reports a model's load failure against the model file, except when a buffer or image it names is missing, which the generic reference check reports at the `.gltf` line.
- **Example**: Lantern 3D's level is `assets/shrine.scene.yaml`, built with `scenemodel.Build`; it animates the `scene3d` nodes made for the diorama's `LightWisp` and `ShrineCore` and keeps its own fog shader, drawing `Built` parts.

## Alternatives considered

- **Extend `sceneformat.BuildNodes3D` to attach geometry**: one entry point, as the issue allowed. Rejected because `sceneformat` would import `gltf`, `render3d` and image decoders, which every 2D game that parses scenes would then link; a separate package keeps the 2D dependency graph unchanged.
- **Keep `mesh` as free text and add a separate `primitive` field**: no existing file breaks. Rejected because no loader ever read `mesh`, no file in the repository used a value other than a primitive name, and two fields for one concept would leave the old one meaning nothing; the schema enum and validation now tell an author the accepted values.
- **Flatten a whole model onto the object's node (one drawable, baked world matrices)**: fewer nodes. Rejected because games need to animate, hide or pick model parts by name (Lantern 3D moves two of them), which needs a node per glTF node.
- **Make `Renderer` the only way to draw**: simpler surface. Rejected because games with their own shaders (fog, outlines) need the parts; `AppendDrawItems` and the exported `Part` data serve them, as Lantern 3D shows.

## Consequences

- Benefits: a 3D level is a reviewable YAML file that `game check` validates and `game mv` keeps consistent; model parts are ordinary `scene3d` nodes with bounds, so culling, picking and the editor see them; the golden image catches regressions in geometry, lighting, texturing and blending.
- Costs: a version 1 or 2 scene whose `mesh` held anything but a primitive name no longer parses. Skins, animations, cameras, lights, vertex colors, points and lines in a model are ignored; models draw in bind pose with default morph weights. `Build` reads material files when it runs, so an edited material needs a rebuild. A `.gltf` model that no scene names is checked only for its references, not loaded.

## Testing

- `packages/game/scenemodel`: build tests for whole models, `modelNode`, `modelMesh`, overrides, bounds, shared IDs, error atomicity and resolver requirements; primitive geometry tests; a strip/fan and flat-shading unit test; the golden render `testdata/golden/level.png` with pixel checks for texture, lighting and visibility; a fixture test that keeps `models/crate.glb` in sync with its generator.
- `packages/game/sceneformat`: the override coverage test sets each 3D look combination; `model_test.go` covers each validation rule with its field path and line, and an override that clears `mesh` to set `model`.
- `packages/game/refs`: `model_test.go` covers scanning, moving a model, moving a model's directory without rewriting its `.gltf`, and percent-encoding a moved buffer.
- `packages/game/tooling/assetcheck`: `model_test.go` covers missing model, node and mesh, a model that does not load, a non-glTF path, an unknown primitive, an override's model, a scene-level override's node, overrides that set only a node or mesh, one that sets a model lacking the node it keeps, and a missing buffer.
- `packages/game/editor`: `TestMoveFileRewritesModels` covers the open document and files on disk; `model_test.go` covers `PlaceObject`'s refusals, prefab defaults and the version raise; the 3D inspector test checks the Mesh field is disabled on a model object.
- `packages/game/scenemodel`: `Prepare` and `Draw` after `Close` return an error and record nothing.
- `examples/lantern3d`: its tests run against the level built from the scene file.
