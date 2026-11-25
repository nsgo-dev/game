# Agent Note: Scene 2D Navigation Regions, Viewport Overlay, and Path Preview

Status: implemented

## Problem

2D navigation meshes (`packages/game/ai`, `packages/game/navbake`) could previously only be built from polygons or tilemaps wired in Go code, so level designers could not author walkable areas or obstacle cutouts in `*.scene.yaml` files, subtract tilemap collision, inspect the built convex mesh in the 2D viewport, or test pathfinding visually in `game/editor` (#888).

## Decision

Extend `packages/game/sceneformat`, `packages/game/cmd/gameschema`, and `packages/game/editor` following the versioned built-in component and in-place 2D polygon handle architecture of [Scene 2D Lights, Occluders, Ambient Color, and Editor Lighting Preview](2026-11-25-editor-lights2d-preview-and-handles.md) and [The editor's 2D transform gizmo edits in place and records one step per drag](2026-11-25-editor-2d-transform-gizmo.md):

- **Built-in `navregion2d` component (`NavRegion2DComponentVersion = 1`)**:
  - Define `navregion2d` in `packages/game/sceneformat/navregion2d.go` (`mode`: `"walkable"` or `"obstacle"`, `points`: `FieldPointList`, `offsetX`, `offsetY`, `subtractTileCollision`, `tileLayer`, `tileLayers`, `agentRadius`, `enabled`), registered via `RegisterNavRegion2DComponent` / `NavRegion2DComponentRegistry`.
  - Canonicalize `navregion2d.points` in `sceneformat.Encode` (`canonicalComponentMap`) as compact deterministic flow YAML (`[[x, y], ...]`) so YAML and Go edits round-trip byte-for-byte unchanged.
  - Emit a `NavRegion2DComponent` definition under `ObjectDoc.components.properties.navregion2d` in `scene.v1.json`, `scene.v2.json`, and `scene.v3.json` via `packages/game/cmd/gameschema/main.go` (derived directly from `sceneformat.NavRegion2DComponent()` so schemas never drift from the Go component schema while keeping `additionalProperties: {}` for custom components).
- **Unified `BuildNavMesh2D` pipeline (`packages/game/sceneformat`)**:
  - `BuildNavMesh2D` walks visible `SpaceWorld` layers (composing parent-child `BuiltObject.World`), normalizes every polygon to positive signed area before feeding `navbake.Source` (so overlapping walkable regions union and overlapping obstacles union regardless of authored winding or mirrored `scaleX`/`scaleY`), optionally subtracts solid tile collision from scene tilemaps (merged rectangles via `tileshape.View.Boxes()` plus non-rectangular solid `TileShapes` on orthogonal maps, and `Origin`-adjusted solid cell polygons on isometric/staggered/hexagonal maps), attaches any authored 2D `nav_link` components, and bakes via `navbake.Bake`.
  - `sceneformat.Build` populates `BuiltScene.NavBake2D` (`*navbake.Mesh`) and `BuiltScene.NavMesh2D` (`*ai.NavMesh`) automatically (keeping both `nil` for scenes with no `navregion2d` components and returning an empty mesh without error when all regions are disabled or lack 3 vertices). `BuiltScene.SyncNavMesh2D()` refreshes both after runtime edits.
- **Editor handles, overlay, and Test Path tool (`packages/game/editor`)**:
  - Register `navregion2d` in the Inspector's `builtinEditorComponents` fallback with an undoable `InitNavRegionPolygon` ("Reset to Default Polygon") button, plus `PlaceNavRegion2D`, `SetNavRegionPoints`, `InsertNavRegionVertex`, and `RemoveNavRegionVertex`.
  - Extend `GizmoHandle2D` with `Handle2DNavRegionVertex` and `Handle2DNavRegionEdgeMid` (`IsNavRegion`, included in `IsComponentHandle`). Dragging a vertex or edge midpoint edits in place, calls `Rebuild()` live on every move so the built mesh overlay and any active test path update immediately, cancels cleanly on Escape, and commits one undoable `componentMapCmd` on `EndDrag2D` so one Undo reverts both the polygon and overlay byte-for-byte.
  - Add `Editor.ShowNavMesh2D` (default `true`, toolbar toggle `view-navmesh2d-overlay`, command `CmdToggleNavMesh2D`) to render the baked convex polygons, cached shared-edge portals (`navbake.Mesh.Portals`), and authored `navregion2d` outlines in `DrawViewport`.
  - Add `ToolNavTestPath` (toolbar button `tool-nav-test-path`, command `CmdToolNavTestPath`): pressing start and end points in the 2D viewport evaluates `ActiveScene().NavMesh2D.FindPath(start, end)` directly, draws the resulting waypoint polyline (or a dashed red line when unreachable), recomputes automatically on `Rebuild()` (vertex edits, Undo/Redo), and clears on Escape or `ClearNavPathPreview2D`.

## Alternatives considered

### Layer-level `navRegions` field on `LayerDoc` instead of a built-in component

A dedicated field on `LayerDoc` would keep navigation polygons separate from `ObjectDoc` entries, but it would require bumping or extending `LayerDoc`, `LayerOverride`, `merge.go`, and `resolve.go`, and would lose object transforms, parent-child parenting, scene instancing/overrides, selection in the Scene Tree, and reuse of the existing 2D gizmo and Inspector component pipeline. Making `navregion2d` a versioned built-in component works across scene versions 1–3 and composes naturally with `nav_link`, `occluder2d`, and prefabs.

### Separate editor-only triangulation or grid pathfinder instead of `BuiltScene.NavMesh2D`

Computing a lightweight editor preview mesh or grid A* path separately from `sceneformat.Build` would risk subtle geometric divergence (different polygon splits, corner clearance, or tile collision clipping) between the editor's test path and the game. Driving both `Editor.DrawViewport` / `ToolNavTestPath` and runtime gameplay from the exact same `BuiltScene.NavBake2D` and `BuiltScene.NavMesh2D` guarantees identical paths by construction.

## Consequences

- **Benefits**: Level designers can draw walkable and obstacle 2D navigation regions directly in `*.scene.yaml`, subtract tilemap collision, edit vertices and insert edge vertices with live mesh-overlay feedback and single-step Undo/Redo, and verify exact `ai.NavMesh` paths in the viewport with guaranteed parity with the game.
- **Trade-offs**: `sceneformat` now imports `game/navbake` (Experimental, matching `sceneformat` in `TestStabilityTable`). Live vertex drags invoke `navbake.Bake` during `Rebuild()`, which is sub-millisecond for typical rooms and fast on tilemaps because orthogonal solid cells are merged into maximal rectangles via `tileshape.View.Boxes()`; scenes with no `navregion2d` components incur zero overhead.
- **Verification**: Covered by `packages/game/sceneformat/navregion2d_test.go`, `packages/game/sceneformat/testdata/canonical/nav_room.scene.yaml` (`formattest.Run`), `packages/game/cmd/gameschema/main_test.go`, `packages/game/editor/navregion2d_test.go` (editor-vs-game path parity with tile collision subtraction, live vertex drag + single-step Undo/Redo + edge-midpoint insertion + Escape cancel, UI overlay toggle and Test Path tool), `cmd/gameschema -check`, `TestFormatsPage`, `TestStabilityTable`, `tools/changelog check`, `bun run verify-notes`, and multi-target `nsgo check`.
