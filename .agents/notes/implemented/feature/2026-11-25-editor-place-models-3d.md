# Agent Note: Placing models into the editor's 3D viewport from the asset browser

Status: implemented

## Problem

In 3D a press on the editor's viewport only selected. The Place tool worked only in 2D, the asset browser listed only atlas frames, and an object's model could be named only by editing the scene file, so a 3D level could not be assembled from models in the editor (#893).

## Decision

- **What the browser offers** (`editor/models.go`): `Models` lists the built-in primitives in `sceneformat.MeshPrimitives` order, then `ProjectModels`: every `.gltf`/`.glb` file `ProjectFiles` lists plus every model target's `Out` in `game.yaml`, sorted and deduplicated, so a target shows before `game assets` has built it (`ModelEntry.Output`). The disk walk is cached by project path, `filesRev` and `assetsRev`, because the asset browser's memo runs on every editor revision; the targets are read from `Project` on each call, which is cheap and follows project edits.
- **Where a model lands**: `PlacementAt3D` casts `Camera3D.ScreenRay` into the 3D build and takes the nearest geometry hit, through the same loop picking uses (`rayGeometry3D`, which `pickGeometry3D` now calls, so hidden and locked objects are passed over in both); with no hit it intersects the horizontal plane at `PlaceHeight3D`. `PlaceSnap3D` rounds X and Z to `gridStep3D`, the spacing of the grid the viewport draws. `PlaceModel3D` builds the model alone (`buildModel3D`, through the same `scene3DAssets` and cached `scenemodel.Assets` the scene's build uses), takes its bounds in object space, optionally turns +Y onto the normal with rotations about X and Z only (`alignUp3D`: `rotX = asin(nz)`, `rotZ = atan2(-nx, ny)` for the objects' Rx-then-Rz order), and lifts the origin along the normal by how far the turned bounds reach below it, so the geometry rests on the surface. The object goes through `PlaceObject`, which validates it, names its ID, raises a version 1 scene and makes it one undoable command.
- **Input paths**: `HandleViewport3DPress` places the `SelectedModel` when the tool is Place, before the gizmo check, so pressing beside the new object's gizmo still places. A drag uses `base/dragdrop`: `EditorView` wraps the main row in one `dragdrop.Area` and provides `dragAreaKey`, the model rows are `dragdrop.Source`s, and the viewport frame is a `dragdrop.Target` accepting `ModelEntry` in 3D; `AssetBrowserView` and `ViewportView` mounted on their own wrap themselves in an area so `Source`/`Target` never panic for want of one. A drag places whatever the active tool, since dragging a model onto the viewport says what to do. Files from another app arrive through `core.View.OnFileDrop`, are resolved with `core.FileFromURL`, must lie inside the project root after resolving symlinks, and are placed together in one `History.Group`.
- **Thumbnails** (`editor/model_thumbs.go`): each model is built on the UI goroutine, since the editor's `Resolver` is not promised to be safe on others, and the build, which belongs to that job alone, is rendered by one worker goroutine with `scenemodel.Renderer` on a fresh `gpu.SoftwareRenderer` 64 pixels square, transparent background, framed by an `OrbitCamera3D` fitted to the bounds. Results are cached per entry until the project path or `assetsRev` changes; a stale generation's result is dropped. The worker posts `thumbs.onReady` through the editor's scope, which the browser points at a signal its thumbnail canvases read, so a finished thumbnail redraws the list and not the 3D viewport.
- **Inspector**: the 3D section's Model field (`modelField`) lists `ProjectModels` and calls `SetObjectModel`, which refuses a model that does not load and one on an object drawing a primitive (mirroring `SetObjectMesh`), clears `modelNode`/`modelMesh` with the model they belonged to, and restores all three and the scene version on undo.

## Alternatives considered

### Place the model's origin on the surface

Godot's default drops the origin where the ray hits. It is simpler and needs no bounds, but most models and every built-in primitive are centred on their origin, so a box or crate would sink half into the floor and every placement would need a gizmo drag; resting the bounds on the surface is what Unreal does and what a level author expects.

### Render thumbnails on the 3D viewport's GPU device or synchronously

The viewport's `gpu.View` device exists only while 3D is shown and belongs to its frame callback, so the browser in 2D would have no thumbnails. Rendering on the UI goroutine is simpler and deterministic but stalls the editor for a project with many models; the worker keeps the list responsive and tests wait on it (`waitModelThumbnails`).

### Track the pointer to place file drops

`OnFileDrop` carries only URLs, and desktop platforms deliver no pointer moves to the app during an OS drag, so a tracked position would be stale. Dropped files land under the view's middle instead.

## Consequences

- Placing loads the model on the UI goroutine the first time, through the same cache the scene's build uses, so the build that follows the placement loads nothing again.
- A file drop cannot choose its spot; this lifts once NSGo reports the drop's position.
- The software GPU renders each thumbnail with a renderer of its own, which costs a few milliseconds per model off the UI goroutine and isolates a failing model from the rest.

## Verification

- `cd packages/game && go tool nsgo test ./editor`: `models_test.go` places a crate on the floor by a press, checks its bounds rest at height 0, and that Undo restores the scene byte for byte and Redo places it again; places on another object's top and aligned on a turned slab; covers the ground plane, snapping, a level view, refusals, `Models`/`ProjectModels` with a model target, `SetObjectModel` undo and refusals, and `DropModelFiles` inside, outside and of the wrong kind; and drives the editor window with nsgotest: press a model then the viewport, toolbar Undo and Redo, drag a primitive onto the viewport, drop a file, and pick a model in the inspector. `models_internal_test.go` checks `alignUp3D` and that thumbnails render with the model in the middle and a transparent corner.
