# Agent Note: 3D navigation mesh baking from scenes in the editor

Status: implemented

## Problem

`game/navbake3d` already provided Recast-style 3D voxelization, polygon generation, deterministic `.navmesh.yaml` serialization, and the `game navmesh` CLI command, and `sceneformat.SceneDoc` already carried a `NavMesh` path field. However, two critical gaps blocked authoring 3D navigation meshes inside the editor (issue #901):

1. `navbake3d.Geometry.AddSceneFile` only read whole glTF models from `ObjectDoc.Model`, ignoring `ObjectDoc.ModelNode` / `ObjectDoc.ModelMesh`, built-in 3D primitives (`ObjectDoc.Mesh`), static `collider3d` components, and dynamic/kinematic `body3d` filtering, and lacked direct YAML file loaders for `*.terrain.yaml` and `*.gridmap.yaml`.
2. `game/editor` had no 3D navigation mesh bake settings panel, no `BakeNavMesh3D` API, no undoable `SceneDoc.NavMesh` editing, and no 3D viewport overlay rendering the baked walkable polygons.

## Decision

Extend `packages/game/navbake3d` with complete static scene geometry extraction and add first-class 3D NavMesh baking, undoable linking, and toggleable viewport visualization to `packages/game/editor`. Both packages are Experimental, keeping the dependency graph cycle-free and preserving `TestStabilityTable` (no Stable package imports either).

### Scene geometry extraction in `game/navbake3d`

- `AddPrimitive(name, world, layer)` generates deterministic triangles for all five `sceneformat.MeshPrimitives` (`box`, `plane`, `sphere`, `capsule`, `cylinder`) sized to match `scenemodel.PrimitiveMesh`, using `navbake3d`'s lookup-table `stepSin`/`stepCos` rather than `math.Sin` so baked `.navmesh.yaml` bytes stay identical across CPU architectures. Closed primitives (`box`, `sphere`, `capsule`, `cylinder`) are wrapped in `beginSolid`/`endSolid` so tall walls and pillars bake as filled solids rather than hollow rooms; `plane` is baked as a surface.
- `AddTerrainFile(fsys, name, layer)` and `AddGridMapFile(fsys, name, layer)` load versioned `*.terrain.yaml` and `*.gridmap.yaml` assets via `world3d.LoadTerrainFS` / `world3d.LoadGridMapFS` and forward to `AddHeightmap` / `AddGridMap`.
- `AddSceneFile(doc, fsys)` and `SceneMeshes(fsys)` skip screen-space layers (`SpaceScreen`) and objects with dynamic (non-zero mass) or kinematic `body3d` components, respect `ModelNode` and `ModelMesh` when extracting glTF models, add built-in `Mesh` primitives, and extract static non-trigger `collider3d` shapes via `sceneformat.ExtractPhysicsDebug3DAt`.

### Editor baking API, panel, and 3D viewport overlay in `game/editor`

- `NavMeshBakeConfig` and `Editor.BakeNavMesh3D()` collect static geometry from the open scene (plus optional programmatic terrain/GridMap/physics geometry via `Editor.OnBakeNavMesh3DGeometry`), bake via `navbake3d.Bake`, encode deterministically via `navbake3d.Encode`, write atomically when an output path resolves inside the open project (or beside `FilePath`, or as an absolute path), link `SceneDoc.NavMesh` through `History` (`SetSceneNavMesh`), and cache the baked `*navbake3d.File` for immediate preview. Opening a project or scene autoloads an existing `SceneDoc.NavMesh` file and seeds defaults from `game.yaml`'s `navigation` section.
- `NavMeshPanelView` docks below the viewport when toggled via the 3D toolbar **NavMesh** button (`toolbar-navmesh-panel`), **View > 3D NavMesh Bake...**, or `CmdNavMeshPanel`, exposing agent name, radius, height, step height (`MaxClimb`), max slope, cell size, cell height, output path, overlay switch, and **Bake** button (`navmesh-bake`). `SceneDoc.NavMesh` is also editable in the Scene Settings inspector (`inspector-scene-navmesh`).
- `drawNavMeshOverlay3D` runs inside `Editor.DrawViewport3D` whenever `ShowNavMesh3D` is enabled (default `true`, toggled via `SetShowNavMesh3D`, `toolbar-navmesh-overlay`, `navmesh-overlay-toggle`, or `CmdNavMeshOverlay`), rendering near-plane-clipped translucent cyan fills for each detail triangle plus detail and boundary outline strokes via `navbake3d.Mesh.DebugLines` and `Camera3D.projectSegment`.

## Alternatives considered

### Reusing `scenemodel.PrimitiveMesh` inside `navbake3d`

Importing `game/scenemodel` in `navbake3d` would avoid duplicating primitive tessellation. Rejected because `scenemodel` pulls in `render3d` and image decoding dependencies and uses `math.Sin`/`math.Cos`, which can differ by 1 ULP across architectures and break `navbake3d`'s cross-architecture byte-identical golden guarantee.

### Putting NavMesh bake settings only inside the Scene Settings inspector

Placing all bake controls inside `inspector_scene.go` would avoid adding a docked panel. Rejected because `inspector_scene.go` only appears when no object is selected, forcing authors to deselect level geometry every time they tweak agent radius or re-bake; a docked `NavMeshPanelView` (matching `ExportPanelView` and `AudioMixerPanelView`) stays accessible while editing 3D objects, while Scene Settings still exposes the `NavMesh` path field.

### Calling `navbake3d.DrawDebug` directly from `DrawViewport3D`

`navbake3d.DrawDebug` is gated behind `//go:build !release` (so game release builds strip debug drawing) and draws strokes only through a `[0, 1]` `math3d.Mat4`. Drawing the overlay directly in `editor` with `Camera3D.projectTriangle` and `Camera3D.projectSegment` works identically in all build tags, adds translucent walkable polygon fills alongside `Mesh.DebugLines` outlines, and matches the exact projection and near-plane clipping of `drawSceneObjects3D` and `drawPhysicsDebug3D`.

## Consequences

- **Benefits**: 3D levels authored with glTF models, built-in primitives, static `collider3d` walls, or programmatic terrain/GridMaps can be baked and visually inspected directly inside the editor; `.navmesh.yaml` outputs remain deterministic and byte-identical across runs; `SceneDoc.NavMesh` updates integrate cleanly with Undo/Redo and scene reloading.
- **Costs**: `navbake3d` maintains its own lightweight deterministic primitive tessellation (12 segments for round primitives) separate from `scenemodel`'s higher-poly render meshes (32 segments), which is accurate to within voxel cell size but slightly coarser visually if someone inspected raw bake input triangles.

## Verification

- `packages/game/navbake3d/scene_bake_test.go`: verifies baking a 3D scene with a ramp, second floor, static `collider3d` wall, dynamic `body3d` crate, trigger, and screen-space layer; confirms `world3d.NavMesh.FindPath` climbs the ramp, keeps `agent.Radius` off the wall, ignores dynamic/trigger objects, and round-trips `.navmesh.yaml` byte for byte; also verifies all 5 primitives, `ModelNode`/`ModelMesh`, `AddTerrainFile`, and `AddGridMapFile`.
- `packages/game/editor/navmesh3d_test.go`: verifies `BakeNavMesh3D`, deterministic file output, byte-for-byte round-trip, `FindPath` ramp climb and wall clearance, Undo/Redo of `SceneDoc.NavMesh`, autoload on reopen, `OnBakeNavMesh3DGeometry`, error/play-mode guards, and full `nsgotest` UI tests toggling the panel, editing settings, pressing Bake, and verifying 3D viewport overlay `DrawOps` appear and toggle cleanly off/on with zero non-finite coordinates.
- Verified with `go tool nsgo test ./navbake3d`, `TMPDIR=/tmp go tool nsgo test ./editor`, `go tool nsgo test .`, `go tool nsgo check` across all targets, `go run ./cmd/gameschema -check`, `go run ./tools/changelog check`, and `bun run verify-notes`.
