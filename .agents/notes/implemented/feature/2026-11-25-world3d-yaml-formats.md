# Agent Note: Versioned YAML formats for 3D grid maps, terrain, and foliage

Status: implemented

## Problem

`game/world3d` previously had a versioned YAML definition only for terrain splat-map layers (`TerrainDoc` from [splat-map terrain materials](./2026-11-25-world3d-terrain-splat-materials.md)), without a `chunkSize` field or runtime builders to assemble a `*Heightmap` / `*TerrainLOD` directly from YAML. Modular 3D `GridMap` levels and procedural `FoliageLayer` scatter definitions could only be constructed in Go code, and `game check` (`tooling/assetcheck`) did not validate `*.gridmap.yaml`, `*.terrain.yaml`, or `*.foliage.yaml` files or their referenced assets.

## Decision

Complete plain-text YAML authoring across all three `world3d` world-building primitives while preserving full backward compatibility with `TerrainDoc` and `alpine.terrain.yaml`:

- **Grid map YAML (`*.gridmap.yaml`, `GridMapDoc`, `gridmap_format.go`)**:
  - Declares `version: 1`, optional `name`, `origin` (`[x, y, z]`), `cellSize` (`[x, y, z]`, defaulting to `[1, 1, 1]`), `meshLibrary` (glTF 2.0 path), non-overlapping filled axis-aligned `boxes` (`min`, `max`, `tile`, `orientation`), and individual `cells` (`at`, `tile`, `orientation`).
  - `ValidateGridMap` refuses unknown fields, trailing documents, inverted boxes (`min > max`), overlapping `boxes`, duplicate coordinates in `cells`, orientations outside `0..23`, and non-positive cell sizes.
  - `EncodeGridMap` emits deterministic 2-space YAML prefixed with `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/gridmap.v1.json`, sorting `boxes` and `cells` into canonical `(y, z, x)` order on a shallow copy without mutating the caller's document.
  - `GridMap.ToDoc` exports occupied cells deterministically using greedy 3D maximal axis-aligned box merging (`+X`, then `+Z`, then `+Y`) so filled regions become compact `boxes` runs and isolated tiles become `cells`.
  - `GridMapDoc.Build(lib)`, `GridMapDoc.Load(fsys)`, and `LoadGridMapFS(fsys, name)` reconstruct a live `*GridMap` by resolving tile names against a `*MeshLibrary`.
- **Terrain YAML completeness (`*.terrain.yaml`, `TerrainDoc`, `terrain_format.go`)**:
  - Extends `TerrainDoc` with optional `chunkSize` (`yaml:"chunkSize,omitempty" schema:"minimum=1"`), `EffectiveChunkSize()` (defaulting to `64`), and `LODConfig()` / `BuildLOD(hm)`.
  - Adds `TerrainImages`, `TerrainDoc.BuildHeightmap`, `TerrainDoc.LoadHeightmap(fsys)`, and `LoadTerrainFS(fsys, name)` to construct a `*Heightmap` with `Origin`, `CellSize`, `HeightScale`, and attached `*TerrainSplatMap` (including `HeightBlendContrast` and per-layer height maps) directly from YAML and decoded images.
- **Foliage YAML (`*.foliage.yaml`, `FoliageDoc`, `foliage_format.go`)**:
  - Declares `version: 1`, optional `name` and `terrain` path, and `layers` (`FoliageLayerDoc`) with `name`, `mesh`, optional `material` and `densityMap`, mutually exclusive `density` (instances/m²) or `spacing` (average distance in meters, where `EffectiveDensity() = 1 / spacing^2`), `minScale`/`maxScale`, `alignToNormal`, `minSlopeDeg`/`maxSlopeDeg`, pointer-valued `minHeight`/`maxHeight` (distinguishing omitted from sea-level `0`), `baseMaxDistance`, `boundingRadius`, and `seed`.
  - `FoliageDoc.BuildLayers`, `FoliageDoc.ScatterAll`, `LoadFoliageFS`, and `FoliageLayerToDoc` bridge YAML definitions and runtime `FoliageField` scattering.
- **Schemas, conformance, and `game check`**:
  - Registers `GridMapDoc` (`gridmap.v1.json`) and `FoliageDoc` (`foliage.v1.json`) alongside `TerrainDoc` (`terrain.v1.json`) in `cmd/gameschema`.
  - Wires all three formats through `formattest.Run` in `packages/game/world3d/conformance_test.go`, `formatVersions` in `packages/game/formats_test.go`, and canonical examples in `docs/en/formats.md` and `docs/zh/formats.md`.
  - Adds `packages/game/tooling/assetcheck/world3d.go` so `game check` validates `*.gridmap.yaml`, `*.terrain.yaml`, and `*.foliage.yaml` files and verifies referenced glTF mesh libraries (including every tile name), heightmaps (requiring at least 2×2 decoded samples), splat maps, layer textures, terrain paths, foliage meshes, materials, and density maps at exact file lines. `Report.Summary()` appends 3D counts only when non-zero to preserve existing 2D summary output.

## Alternatives considered

- **Combining grid map boxes and cells into a single polymorphic slice (optional `to` field on cells)**: Simpler top-level schema, but weakens JSON Schema validation (requiring `min`/`max` vs `at` cleanly without `oneOf`) and complicates canonical sorting and overlap checks. Keeping disjoint `boxes` runs and per-coordinate `cells` (with `cells` applied after `boxes` so authors can override tiles inside a filled box) matches `GridMap.FillBox` + `GridMap.SetCell` directly and yields strict draft-07 schemas.
- **Accepting both `density` and `spacing` simultaneously on a foliage layer (with one taking precedence)**: Convenient when editing hastily, but silently ignores one author-written field and breaks mental models during review. Refusing both-set and neither-set at parse time makes every foliage layer unambiguous while supporting both authoring units.
- **Always printing `0 grid maps, 0 terrains, 0 foliage files` in `assetcheck.Report.Summary()`**: Uniform output across all projects, but adds noise to 2D games and breaks existing `assetcheck` summary assertions. Appending non-zero 3D counts keeps 2D output unchanged while surfacing 3D counts whenever present.

## Consequences

- **Benefits**:
  - Grid maps, terrains (including LOD chunk sizing), and foliage scatter layers are plain-text, diff-friendly, deterministic YAML files with published JSON Schemas and automated `formattest` + `gameschema` conformance.
  - Loading the sample YAML fixtures builds the exact same runtime `GridMap`, `Heightmap` + `TerrainLOD`, and `FoliageField` instances as the Go code examples.
  - `game check` catches schema mistakes, missing assets, undersized heightmaps, and unknown glTF tile names in CI with exact file and line numbers.
- **Costs**:
  - `world3d` registers standard-library `image/png`, `image/jpeg`, and `image/gif` decoders via blank imports so `LoadTerrainFS` and `FoliageBuildOptions.FS` can decode referenced images without extra caller wiring.
  - `GridMap.ToDoc` requires every occupied cell's tile ID to have a non-empty `MeshTileDef.Name` in `GridMap.Library`, since YAML stores portable tile names rather than transient numeric IDs.
