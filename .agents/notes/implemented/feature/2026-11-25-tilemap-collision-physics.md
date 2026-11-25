# Agent Note: Direct Tilemap physics grid, Tiled collision preservation, merged static shapes, and LDtk IntGrid project settings

Status: implemented

## Problem

Issue #814 identified four remaining gaps between `game/tilemap`, `game/tileshape`, `game/project`, and `game/physics` after [per-tile collision shapes](2026-11-25-tilemap-per-tile-collision-shapes.md) landed:

1. `*tilemap.Tilemap` and `*tilemap.WorldStream` did not satisfy `physics.TileGrid` directly, forcing games such as `examples/caverun` and tests in `tilemap` to carry hand-written `TileGrid` adapters and duplicate `TileFlags` conversions.
2. `tilemap.LoadTiled` flattened each tile's Tiled collision `objectgroup` into `Tileset.Shapes` (`[]TileShape`), dropping raw object metadata (`ID`, original `Width`/`Height`/`Rotation`, `Visible`, points, and group properties), and `tilemap` had no `.tmj` encoder to round-trip per-tile collision data.
3. `tileshape.View.Boxes` did not merge adjacent rectangular partial tiles across neighbors and had no `StaticShapes` output covering non-rectangular partial shapes (slopes, bevels, ellipses, concave polygons) as `physics.Shape` colliders.
4. `tilemap.LoadLDtk` mapped LDtk IntGrid values only via a fixed hardcoded name/value heuristic, ignoring LDtk's `intGridValuesGroups` and offering no `game.yaml` project settings to configure IntGrid-to-`TileFlags` mappings.

## Decision

- **Direct `physics.TileGrid` + `physics.TileGridOrigin` on `*tilemap.Tilemap` and `*tilemap.WorldStream`**: `tilemap` already imports `physics` (`packages/game/tilemap/physics.go`), so `*Tilemap` implements `TileSize`, `GridSize`, `GridOrigin`, and `TileFlagsAt`, and `*WorldStream` adds `TileFlagsAt`. Both and `tileshape.View` share one canonical conversion, `tilemap.TileFlags.PhysicsFlags()`, which preserves `TileSolid` (bit 0), `TileOneWay` (bit 1), and custom bits 2..31 verbatim. `examples/caverun` and `tilemap/world_test.go` drop their manual adapters.
- **Per-tile collision object groups and `.tmj` round-tripping**: `Tileset.TileCollisions` (`map[uint32]*ObjectLayer`, queried via `Tilemap.TileCollisionGroup`) preserves each tile's Tiled collision object group verbatim across `.tmj`, `.tsj`, `.tmx`, and `.tsx` while keeping `Tileset.Shapes` populated for `tileshape`. `tilemap.EncodeTiled` writes deterministic, indented Tiled JSON (`.tmj`) preserving tilesets, layers, properties, animations, Wang sets, and per-tile `objectgroup` entries (falling back to synthesizing an `objectgroup` from `Tileset.Shapes` when `TileCollisions` is absent). `testdata/sample.tmj`, `sample.tmx`, `props.tsj`, and `props.tsx` include per-tile collision object groups.
- **Merged `Boxes` and `StaticShapes` in `game/tileshape`**: `View.Boxes` merges touching axis-aligned solid boxes (both whole cells and partial rectangular tiles) horizontally and vertically across neighbors and chunk borders. `View.StaticShapes` and `View.AppendStaticShapesIn` emit those merged `physics.Box` shapes plus non-rectangular solid partial shapes: adjacent convex polygons sharing an opposite edge are greedily merged whenever their simplified union remains convex with at most `physics.MaxPolygonVertices` (8) vertices (`physics.PolygonShape`), and concave or >8-vertex shapes (such as 16-segment ellipses) emit closed `physics.ChainShape`.
- **LDtk IntGrid flags from project settings**: `tilemap` adds `LDtkOptions` (`ResolveImage`, `IntGridFlags`, `IntGridValueFlags`), `LoadLDtkWith`, `LoadLDtkAllWith`, and `LoadLDtkWorldWith`, plus `intGridValuesGroups` parsing in the default heuristic. Precedence is: numeric `IntGridValueFlags[v]`, then case-insensitive identifier in `IntGridFlags`, then case-insensitive group identifier in `IntGridFlags`, then the default heuristic; mapping an entry to `0` explicitly clears collision. `project.Physics` adds `LDtkIntGrid []LDtkIntGridRule` (`yaml:"ldtkIntGrid,omitempty"`) in `game.yaml` with `File.LDtkOptions` / `Physics.LDtkOptions`, validated for exclusivity (`name` vs positive `value`) and uniqueness. `sceneformat.LDtkOptionsResolver` lets `project.Resolver` supply `game.yaml`'s IntGrid rules automatically to `sceneformat.Build`.

## Alternatives considered

### Keeping `TileGrid` only on `tileshape.View`

Requiring `tileshape.New(m, tileshape.Options{})` for every map keeps `Tilemap`'s method set smaller and handles partial shapes uniformly. However, issue #814 explicitly requires `physics.MoveAndSlide(body, tm, dt)` to compile and work with a loaded `*tilemap.Tilemap`, and simple whole-tile maps (like `examples/caverun` and LDtk IntGrid maps) should not need a second package or cached chunk view just to collide with solid and one-way tiles. Both `*Tilemap` and `tileshape.View` now satisfy `physics.TileGrid` + `physics.TileGridOrigin`.

### Replacing `Tileset.Shapes` with `Tileset.TileCollisions`

Storing only `ObjectLayer` on tilesets would avoid two fields, but `tileshape`, `TilesetDoc`, and existing callers rely on `Tileset.Shapes` (`[]TileShape` with pre-baked rotations and ellipse polygons). Keeping both lets `LoadTiled`/`EncodeTiled` preserve lossless Tiled object groups while keeping `tileshape`'s fast path untouched.

### Putting slope and static-shape merging inside `game/tilemap`

Moving `tileshape`'s chunked geometry derivation into `game/tilemap` would duplicate or relocate `lights2d` occluders and chunked polygon clipping into `tilemap`. Keeping derived geometry in `game/tileshape` preserves the package boundary established in [per-tile collision shapes](2026-11-25-tilemap-per-tile-collision-shapes.md) while exposing `StaticShapes` alongside `Slopes`, `Boxes`, `Outline`, and `Occluders`.

## Consequences

- Benefits: `*tilemap.Tilemap` and `*tilemap.WorldStream` plug directly into `physics.MoveAndSlide`, `World`, and `RigidWorld` with zero boilerplate; `examples/caverun` drops `tileGridAdapter`; `.tmj` round-trips preserve per-tile collision data deterministically; `tileshape.View.StaticShapes` provides neighbor-merged `Box`, `Polygon`, and `Chain` colliders for `RigidWorld` and `World`; LDtk IntGrid collision flags are configurable declaratively in `game.yaml`.
- Costs: `Tileset` stores both `TileCollisions` and `Shapes` for Tiled maps with per-tile collision groups; `game.v1.json` grows by the optional `physics.ldtkIntGrid` array; `project.Resolver` lazily parses `game.yaml` once (cached until `Refresh`) when building scenes with `.ldtk` sources.
- Verification: `packages/game/tilemap/physics_test.go` verifies direct `physics.MoveAndSlide(body, tm, dt)` and `World` queries; `packages/game/tilemap/shapes_test.go` verifies `sample.tmj` embedded and external `.tsj` `TileCollisions` and idempotent `EncodeTiled` -> `LoadTiled` round-trips; `packages/game/tilemap/ldtk_test.go` and `packages/game/project/physics_test.go` verify `intGridValuesGroups`, `LDtkOptions`, `game.yaml` `physics.ldtkIntGrid` round-trip/validation, and `project.Resolver` + `sceneformat.Build`; `packages/game/tileshape/tileshape_test.go` verifies merged half-tile `Boxes`, merged 2:1 slope `Polygon`s, ellipse `Chain`s, and `RigidWorld` collision against `StaticShapes`; `examples/caverun` tests verify gameplay with the adapter removed.
