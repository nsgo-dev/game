# Agent Note: Scene GridMap references and 3D editor GridMap painting

Status: implemented

## Problem

[`*.gridmap.yaml`](./2026-11-25-world3d-yaml-formats.md) gave `game/world3d` a canonical, schema-validated YAML format for modular 3D levels, and [`game/scenemodel`](./2026-11-25-scene-objects-draw-gltf-models.md) enabled scene files to draw primitives and glTF models in 3D, but scene files still could not reference a `.gridmap.yaml` file directly, `scenemodel.Build` could not instance a `GridMap` from a scene object, `refs` and `game check` did not track scene-to-GridMap links or binary `.glb` mesh libraries, and the 3D scene editor had no visual GridMap painting workflow.

## Decision

Wire `.gridmap.yaml` end-to-end across `world3d`, `sceneformat`, `scenemodel`, `refs`, `tooling/assetcheck`, and the 3D `editor`:

- **World3D helpers (`packages/game/world3d/gridmap.go`)**:
  - Extend `LoadMeshLibraryFromGLTF` to unpack binary `.glb` containers (validating the 12-byte GLB header and extracting the JSON chunk via `encoding/binary`) alongside `.gltf` JSON so mesh libraries work with both glTF containers without adding new dependencies to `world3d`.
  - Add `GridAxis` (`GridAxisX`, `GridAxisY`, `GridAxisZ`), `FindGridOrientation(m math3d.Mat4) (uint8, bool)`, and `RotateGridOrientation(orient uint8, axis GridAxis, quarterTurns int) uint8` to compose exact 90-degree rotations across the 24-element cube rotation group.
  - Add `MeshLibrary.Items() []MeshTileDef` (sorted by ascending tile ID with cloned `ColliderBoxes`) and `GridMap.EachCell(fn func(GridCoord, GridCell) bool)` so tools and renderers can enumerate libraries and occupied cells without inspecting internal chunks.
- **Scene format and JSON Schema (`packages/game/sceneformat/`, `cmd/gameschema`, `apps/website/public/schemas/scene.v2.json`)**:
  - Add `ObjectDoc.GridMap` and `ObjectOverride.GridMap` (`yaml:"gridMap,omitempty"`, version 2 only) and `IsGridMapPath(p string) bool` accepting `.gridmap.yaml` and `.gridmap.yml`.
  - Enforce mutual exclusivity with `Mesh` and `Model` (and with `ModelNode` / `ModelMesh`) in `validateObjects`, `validateModel`, and `CheckObjectModel`, and register `"GridMap": true` in `sceneV2Fields` so `scene.v2.json` stays generated and checked by `cmd/gameschema -check`.
- **Scene model instancing (`packages/game/scenemodel/assets.go`, `build.go`)**:
  - Add `GridMapResolver` (`GridMap(path string) (*world3d.GridMapDoc, error)`) and implement it on `Assets` with per-path caching; add `Built.GridMaps map[string]*world3d.GridMap`.
  - In `builder.gridMap`, load the `GridMapDoc` and its glTF `meshLibrary` via `ModelResolver`, build `*world3d.GridMap`, invoke `gm.BuildInstances(nil, nil)` to obtain per-tile instance transforms, resolve each tile's glTF mesh parts once (sharing `MeshID` and `MaterialID` across every instance and honoring any object-level `material` override), sort transforms deterministically, attach child `scene3d.Node`s using `math3d.QuatFromMat4`, and set the parent object node's `LocalBounds` to `gm.Bounds()`.
- **Reference tracking and `game check` (`packages/game/refs/`, `packages/game/tooling/assetcheck/`)**:
  - Add `refs.KindGridMap`, `refs.FieldGridMap`, and `refs.FieldMeshLibrary` so `game mv`, `game index`, and `Editor.MoveFile` rewrite scene `gridMap` fields, overrides, and `.gridmap.yaml` `meshLibrary` paths.
  - Extend `assetcheck` (`scene.go`, `world3d.go`) to validate `o.GridMap` and override `ov.GridMap` at the exact scene file and line, check `.gridmap.yaml` files once via `c.checked[p]`, validate both `.gltf` and `.glb` mesh libraries, and flag non-empty grid maps with an empty `meshLibrary`.
- **3D editor GridMap painting (`packages/game/editor/gridmap.go`, `gridmap_palette.go`, `viewport3d.go`, `viewport.go`, `ui.go`, `inspector.go`, `scenefile.go`, `refs.go`)**:
  - Add `GridMapTool` (`None`, `Paint`, `Erase`, `Pick`, `BoxFill`) in the 3D toolbar, inspector `GridMap` selector (`SetObjectGridMap`), and right-side `GridMapPaletteView` (`Editor.GridMapPalette()`) that renders isometric shaded 3D thumbnails for every glTF mesh library tile (falling back to cell bounding boxes when mesh triangles are unavailable).
  - Support interactive floor-plane ray projection (`ScreenToGridMapCell`), hovered cell and box-fill previews (`drawGridMap3D`), stroke coalescing (`BeginGridMapStroke` / `ContinueGridMapStroke` / `EndGridMapStroke` / `CancelGridMapStroke`), floor stepping (`]` / `[` / `PageUp` / `PageDown`), 24-orientation stepping (`X` / `Y` / `Z` / `Q` with `Shift` variants, bound with `core.KeyNotEditing` only while in 3D GridMap mode so existing shortcuts stay collision-free), and single-step Undo/Redo for paint, erase, box fill, and cell rotation (`PaintGridMapCell`, `EraseGridMapCell`, `FillGridMapBox`, `RotateGridMapCell`).
  - Integrate dirty tracking (`GridMapDirty` in `HasUnsavedChanges`) and canonical saving (`Save` -> `SaveGridMap` -> `world3d.GridMap.ToDoc` + `world3d.EncodeGridMap`) plus `MoveFile` path tracking (`followGridMapMove`).

## Alternatives considered

- **Reusing `ObjectDoc.Model` for `.gridmap.yaml` files by extension sniffing**: Saves one schema field, but blurs the contract between glTF models (which support `modelNode` and `modelMesh`) and modular grid maps (which have their own YAML format, resolver, and editor tool). A dedicated `gridMap` field keeps `scene.v2.json`, `CheckObjectModel`, `refs`, and the inspector explicit and unambiguous.
- **Building a separate `scene3d` renderer path for `GridMap` instances instead of emitting child `scene3d.Node`s**: Could avoid per-instance child nodes in `scenemodel.Built`, but would require every downstream consumer (`Built.AppendDrawItems`, `scenemodel.Renderer`, picking, visibility/transform inheritance) to special-case `GridMap`. Creating child nodes per `BuildInstances` entry with shared `MeshID` and `MaterialID` reuses the entire existing `scenemodel` pipeline and GPU mesh/material deduplication.
- **Embedding GridMap cells inline inside `*.scene.yaml` like 2D inline tilemaps**: Eliminates external file management, but bloats scene diffs whenever a 3D level is painted and prevents sharing or independently validating a modular level layout. Referencing standalone `*.gridmap.yaml` files matches Godot's modular resource workflow and keeps both scene and GridMap YAML diffs small and canonical.

## Consequences

- **Benefits**:
  - 3D modular levels can be referenced declaratively from `*.scene.yaml`, instanced identically in `scenemodel.Build`, checked by `game check`, tracked by `game mv` / `MoveFile`, and painted visually in the 3D editor with thumbnails, floor heights, 24 orientations, previews, and full undo/redo.
  - Binary `.glb` mesh libraries work seamlessly in `world3d.LoadMeshLibraryFromGLTF`, `assetcheck`, `scenemodel`, and the editor palette.
  - Saving an edited GridMap writes canonical `.gridmap.yaml` (greedy merged `boxes` + sorted `cells`) that round-trips byte-for-byte.
- **Costs**:
  - `scenemodel.Build` requires callers whose scenes use `gridMap` to supply a resolver implementing `GridMapResolver` in addition to `ModelResolver` (`scenemodel.Assets` implements all resolver interfaces automatically).
  - Editor 3D viewport GridMap previews render in software-projected 2D canvas geometry matching the rest of `viewport3d.go` (wireframes, shaded cell boxes, and isometric mesh thumbnails) rather than a GPU render pass, keeping headless tests deterministic across platforms.
