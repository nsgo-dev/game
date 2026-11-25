# Agent Note: 3D Physics Collider Shapes, Viewport Handles, and Fit-to-Bounds

Status: implemented

## Problem

Built-in 3D scene physics components from [Scene Physics Bodies, Colliders, Triggers, and Named Layers](./2026-11-25-scene-physics-components.md) covered `box`, `sphere`, and `capsule` primitives without local collider rotation, lacked `cylinder`, model-derived `convex` / `trimesh`, and terrain `heightfield` shapes, did not accept `trigger` kind, `layer`/`mask`, or `physicsMaterial` on `body3d`, and offered no 3D viewport size handles or one-click fit-to-model-bounds action in `game/editor` (#902). Authors therefore still had to write Go setup code for non-primitive 3D colliders and hand-type dimensions to align colliders with glTF models.

## Decision

Extend the existing `body3d`, `collider3d`, and `trigger3d` built-in components (keeping `PhysicsComponentVersion = 1` and full backwards compatibility with #817 scenes) and integrate collider size handles directly into the editor's existing 3D gizmo API:

- **`sceneformat` 3D component schemas and `BuildPhysics`** (`packages/game/sceneformat/physics.go`, `physics_3d_shapes.go`):
  - Extend `body3d` with `kind: "trigger"`, `layer`, `mask`, `physicsMaterial` (alongside `material`; conflicting distinct paths are refused), and `enter`/`stay`/`exit` events; include `body3d` in `isLayeredPhysicsComponent` so `ParseWith` validates `body3d.layer` and `body3d.mask` with exact file, line, and column.
  - Extend `collider3d` with `shape` enum `["box", "sphere", "capsule", "cylinder", "convex", "trimesh", "heightfield"]`, local Euler XYZ rotation (`rotX`, `rotY`, `rotZ` using the same `Rz * Ry * Rx` order as `ObjectLocalTransform3D`), `terrain` (`*.terrain.yaml` path), and `maxVertices` (`[0, 256]`); extend `trigger3d` with `cylinder`, `convex`, `rotX`/`rotY`/`rotZ`, and `maxVertices`.
  - Add optional `ModelResolver` (structurally identical to `scenemodel.ModelResolver`) and `TerrainResolver` interfaces with automatic fallback through `FileResolver` (`fileResolverFS` adapter for `gltf.LoadFS` and `world3d.LoadTerrainFS`), so `project.Resolver` and `sceneformat.FSAssets` work without extra wiring.
  - Record static triangle-mesh and heightfield colliders in `PhysicsScene.TriMeshes3D` and `PhysicsScene.Heightfields3D`, refuse non-static or trigger `trimesh`/`heightfield` colliders, extract glTF meshes matching `scenemodel.Build` (`modelMesh`, `modelNode`, or default scene roots with morph weights), and collide kinematic 3D bodies against `TriMeshes` and `Heightfields` in `stepKinematic3D`.
  - Expose `ExtractPhysicsDebug3DAtWithResolver` returning `PhysicsDebugShape3D` with `Shape`, `TriMesh`, or `Heightfield` computed from the exact same local/world transform pipeline as `BuildPhysics`, guaranteeing the editor's drawn wireframe matches runtime collision identically.
- **`editor` 3D collider wireframes, gizmo handles, and fit-to-bounds** (`packages/game/editor/collider3d_gizmo.go`, `physics_debug.go`, `viewport3d.go`, `state.go`, `inspector.go`, `ui.go`, `commands.go`, `menubar.go`, `play.go`):
  - Add `Gizmo3DCollider` to `GizmoMode3D` (`CmdGizmoCollider`, toolbar button `gizmo-collider`, menu item, shortcut `shift+c`) and route `PickGizmo3D`, `BeginGizmoDrag3D`, `BeginGizmoDragAxis3D`, `UpdateGizmoDrag3D`, `EndGizmoDrag3D`, `CancelGizmoDrag3D`, and `drawGizmo3D` to `ColliderHandles3D` when active.
  - Surface handles on `box` (`width`/`height`/`depth`), `sphere` (`radius`), `capsule` (`radius`/`height`), and `cylinder` (`radius`/`height`) sit directly on the collider's world-space oriented faces/caps; dragging updates the field live and commits as a single `setComponentFieldCmd` undo step on `EndGizmoDrag3D` (restoring exact pre-drag YAML on `Undo` or `CancelGizmoDrag3D`).
  - Add `Editor.FitCollider3DToBounds(objectID)` (`CmdFitCollider3D` and Inspector button `inspector-comp-collider3d-fit-bounds` / `inspector-comp-trigger3d-fit-bounds`) reading local geometry bounds from `Editor.scene3D()` and updating the collider's dimensions and offset in one undo step (`setComponentMapCmd`).
  - Extend `drawPhysicsShape3D` to render wireframes for `ShapeCylinder`, `ShapeConvexHull`, `TriMesh` (capped at 4096 edges), and `Heightfield` (strided XZ grid), backed by `Editor.physicsResolver()` in both edit-mode debug drawing and `PlayIn`.

Related notes: [Scene Physics Bodies, Colliders, Triggers, and Named Layers](./2026-11-25-scene-physics-components.md), [3D colliders from glTF scenes, GridMaps, and heightmap images](./2026-11-25-physics3d-gltf-gridmap-colliders.md).

## Alternatives considered

### Separate `physics3d` collider gizmo API parallel to `GizmoMode3D`

Adding a dedicated `PickColliderHandle3D`/`BeginColliderDrag3D` interaction path alongside `PickGizmo3D`/`BeginGizmoDrag3D` would force callers and viewport pointer routing to arbitrate between two independent 3D gizmo systems and duplicate drag/cancel lifecycle handling. Extending `GizmoMode3D` with `Gizmo3DCollider` keeps one unified 3D gizmo API, reuses `BeginViewport3DDrag`/`UpdateViewport3DDrag`/`EndViewport3DDrag` and the toolbar/menu/shortcut plumbing, and still exposes `ColliderHandles3D` for direct handle inspection.

### One-sided face dragging that mutates both size and offset

Dragging a single box face while holding the opposite face fixed changes two fields (`width` and `offsetX`) on every drag and shifts centered model colliders off-center whenever a user widens them. Symmetric dragging around the collider's center matches Godot's `BoxShape3D` default handle behavior, keeps centered colliders aligned with their models, and commits a clean single-field `setComponentFieldCmd` undo step.

### Moving model/terrain collider construction into `scenemodel` instead of `sceneformat`

Building `convex`/`trimesh`/`heightfield` colliders only in `scenemodel` would split 3D physics construction across two packages when `sceneformat` already imports `physics3d` and `world3d` and already owns `BuildPhysics` and `ExtractPhysicsDebug3DAt`. Keeping all 3D collider builders in `sceneformat` behind optional `ModelResolver`/`TerrainResolver` interfaces (with `FileResolver` fallback) gives one entry point (`BuildPhysics`) for both games and `editor.PlayIn`.

## Consequences

- **Benefits**: Every `physics3d` shape (box, sphere, capsule, cylinder, convex hull, triangle mesh, heightfield) can be authored in plain-text YAML scenes, validated with exact file and line diagnostics via `ParseWith`, visualized as wireframes in the 3D viewport, resized interactively via `Gizmo3DCollider` handles with single-step undo, fitted to glTF/primitive bounds in one click, and simulated at exact matching world poses in `BuildPhysics`.
- **Trade-offs**: `trimesh` and `heightfield` colliders are static-only (matching `physics3d.World`'s `TriMeshCollider` and `HeightfieldCollider` model), and `heightfield` colliders remain axis-aligned in XZ (ignoring 3D collider rotation).
- **Verification**: Covered by `packages/game/sceneformat/physics3d_collider_test.go` and `packages/game/editor/collider3d_gizmo_test.go`, plus `nsgo check` (default and `ios`), `nsgo test` on `./sceneformat`, `./editor`, and `TestFormatsPage`, `gameschema -check`, `tools/changelog check`, and `bun run verify-notes`.
