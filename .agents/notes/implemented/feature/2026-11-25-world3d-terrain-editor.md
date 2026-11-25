# Agent Note: Terrain 16-bit heightmaps, per-layer weight and foliage maps, and editor sculpt/paint tools

Status: implemented

## Problem

`world3d` previously defined `TerrainDoc` (`*.terrain.yaml`) with packed RGBA `splatMaps`, `Heightmap`, and `TerrainLOD` ([versioned world3d YAML formats](./2026-11-25-world3d-yaml-formats.md), [splat-map terrain materials](./2026-11-25-world3d-terrain-splat-materials.md)), while `FoliageDoc` lived only in a separate `*.foliage.yaml` file. Authors still had no way to declare terrain grid dimensions (`size`), per-layer material resources and grayscale weight-map PNGs, or embedded foliage density maps inside `.terrain.yaml`, no 16-bit PNG encoder to write edited heightmaps back without 8-bit slope banding, no one-call bridge in `render3d` to construct a `TerrainMaterial` directly from a `TerrainDoc`, and no interactive terrain sculpting or painting tools in the 3D editor (#900).

## Decision

Extend `world3d`, `render3d`, `refs`, `tooling/assetcheck`, and `editor` while keeping full backward compatibility with existing v1 `*.terrain.yaml` documents:

- **16-bit heightmaps and extended `TerrainDoc` (`packages/game/world3d`)**:
  - `LoadHeightmapGray` takes a direct `*image.Gray16` fast path (avoiding per-pixel interface boxing) and `Heightmap.ToGray16`, `Heightmap.EncodePNG`, `EncodeHeightmapPNG`, and `DecodeHeightmapPNG` round-trip true 16-bit grayscale PNGs with full `uint16` precision (65,536 levels, eliminating 8-bit staircase banding on gentle slopes).
  - `TerrainDoc` (still `TerrainFormatVersion = 1`) adds optional `size` (`[cols, rows]`, each `>= 2`), optional `foliage` (`[]FoliageLayerDoc`, sharing `validateFoliageLayers` with `FoliageDoc`), and per-layer `material` (resource path) and `weightMap` (grayscale PNG path) on `TerrainLayerDoc`. `ValidateTerrain` refuses mixing packed `splatMaps` with per-layer `weightMap` paths in the same document so weight ownership is unambiguous.
  - `PackLayerWeightMaps` and `LoadTerrainSplatMapFromWeights` pack up to 8 per-layer grayscale weight PNGs into the same 1–2 RGBA `*image.NRGBA` splat grids used by `TerrainSplatMap` and GPU shaders. `Heightmap` exposes `FoliageDensity` / `FoliageDensityAt`, `TerrainDoc.LoadHeightmap` loads per-layer weights and foliage density maps automatically, `TerrainDoc.ScatterFoliage` reuses attached density maps, and `DensityMap` gains `NewDensityMap`, `At`, `Set`, `Clone`, `ToGray`, `ToGray16`, and `EncodePNG`.
- **`render3d` document loader (`packages/game/render3d/terrain_loader.go`)**:
  - `TerrainMaterialFromDoc(fsys, doc)` loads a validated `render3d.TerrainMaterial` directly from a `TerrainDoc`, resolving `resource.Material` defaults (`BaseColor`, `Texture`, `Roughness`, `Metallic`), explicit layer overrides, and either packed `splatMaps` or packed per-layer `weightMap` PNGs. `MeshFromTerrainMesh` bridges `world3d.TerrainMesh` to `render3d.Mesh`.
- **Editor sculpt & paint tools (`packages/game/editor/terrain.go`, `terrain_ui.go`)**:
  - Supports `TerrainBrushRaise`, `TerrainBrushLower`, `TerrainBrushSmooth` (Jacobi 3×3 Gaussian kernel without scan-order bias), `TerrainBrushFlatten` (sampling stroke-start elevation or explicit `TargetHeight`), `TerrainBrushSetHeight`, `TerrainBrushPaintLayer` (renormalizing all layers to sum to 1 and syncing `hm.Splat` live), and `TerrainBrushPaintFoliage` (blending toward `TargetDensity` in `[0, 1]`), with configurable `Radius`, `Strength`, and `Falloff` (`Smooth`, `Linear`, `Flat`).
  - Each drag (`BeginTerrainStroke` / `ContinueTerrainStroke` / `EndTerrainStroke` or 3D raycast `BeginTerrainStrokeAt` / `ContinueTerrainStrokeAt`) interpolates dabs smoothly, captures each touched sample's pre-stroke value once, and records the entire stroke as **one undoable command** (`terrainStrokeCmd`) in `Editor.History` with exact state-ID dirty tracking (`TerrainDirty`).
  - `SaveTerrain` / `SaveTerrainTo` atomically writes the 16-bit grayscale PNG heightmap, per-layer weight PNGs (or RGBA splat maps for terrains opened with `splatMaps`), foliage density PNGs, and deterministic `*.terrain.yaml`.
  - `TerrainPanelView` docks below the viewport in 3D mode, `DrawViewport3D` renders the terrain heightfield preview and terrain-conforming brush ring, `refs.Scan` classifies `*.terrain.yaml` as `KindTerrain`, and `assetcheck` validates `size`, layer `material`/`weightMap`, and embedded `foliage` references.

## Alternatives considered

- **Bumping `TerrainFormatVersion` to 2 for `size`, `weightMap`, `material`, and `foliage`**: Would cleanly separate old and new schemas, but forces every existing `*.terrain.yaml` fixture and game to migrate even though all new fields are strictly optional with `omitempty` and existing v1 files remain valid with unchanged semantics. Keeping `version: 1` with additive optional fields preserves backward compatibility and single-schema tooling.
- **Allowing both `splatMaps` and per-layer `weightMap` in the same terrain file with precedence rules**: Avoids an error when switching authoring styles, but silently ignores one set of PNG files on disk and creates subtle editor-vs-game drift when saving. Refusing both simultaneously keeps one authoritative representation per terrain.
- **Storing full heightmap/weightmap array snapshots on every undo command**: Simpler than sparse per-sample diffs, but a 1025×1025 terrain with 8 weight layers would copy ~36 MB per brush dab stroke and exhaust memory after a few dozen strokes. Capturing sparse pre/post sample diffs per stroke keeps memory proportional to the brushed footprint while restoring exact values on Undo and Redo.

## Consequences

- **Benefits**:
  - Authors can sculpt smooth, band-free 16-bit terrain slopes, paint multi-layer materials, and paint foliage density masks in the 3D editor with one-step Undo/Redo per stroke, and save files that `world3d.LoadTerrainFS` and `render3d.TerrainMaterialFromDoc` load and render identically in the game.
  - Both packed RGBA `splatMaps` (for compact runtime assets) and per-layer grayscale `weightMap` PNGs (for straightforward authoring and diffing) are first-class in `world3d`, `render3d`, `refs`, `assetcheck`, and `editor`.
- **Costs**:
  - `render3d` now imports `world3d` and `resource` (all three are `Experimental`, with no import cycles and `world3d` remaining CPU-only).
  - Weight maps and foliage density maps are saved as 8-bit grayscale PNGs (256 levels), matching GPU RGBA8 splat textures, while heightmaps use 16-bit grayscale PNGs (65,536 levels) where vertical banding is visible on slopes.
- **Verification**:
  - `packages/game/world3d/terrain_editor_format_test.go` verifies bit-exact 16-bit PNG heightmap round-trip without 8-bit banding, per-layer weight packing/unpacking, embedded foliage density loading and scattering, and validation rules.
  - `packages/game/render3d/terrain_loader_test.go` verifies `TerrainMaterialFromDoc` with material resources and per-layer weight maps through full GPU rendering.
  - `packages/game/editor/terrain_test.go` verifies every sculpt/paint brush, falloff curves, single-step Undo/Redo, mounted 3D UI interaction, and end-to-end Editor Save -> `world3d.LoadTerrainFS` + `render3d.TerrainMaterialFromDoc` + `assetcheck.Check` parity.
  - `packages/game/tooling/assetcheck/world3d_test.go` and `packages/game/refs/formats_test.go` verify static checks and reference scanning for the new terrain fields.
