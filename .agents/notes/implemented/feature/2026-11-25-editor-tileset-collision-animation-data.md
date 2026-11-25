# Agent Note: Editor tileset collision, animation, custom data and live tile preview

Status: implemented

## Problem

`*.tileset.yaml` (`TilesetDoc`) already stored collision flags, shapes, animations and custom properties (#847), and `game/editor` had a terrain-only mask editor (#879), but authors still could not visually author per-tile collision flags/shapes, animations, names, variant weights, or typed custom properties inside the editor (#878). In addition, single-grid inline tilemaps (`data`) could only reference a sprite `tileset` atlas (not `tilesetFile`), `*tilemap.Tilemap` did not directly satisfy `physics.TileGrid`, animated tiles stood still in the editor's 2D edit viewport, and edits made in the Tileset Editor did not affect `sceneformat.Build` or Play mode until saved to disk.

## Decision

### Extend `TilesetDoc` and runtime `Tilemap` in place

- `packages/game/tilemap/tileset_doc.go` adds optional `Name` (`name`) and `Weight` (`weight`, finite and `>= 0`) on `TileDoc`, reflected in `apps/website/public/schemas/tileset.v1.json` via `cmd/gameschema`. `AddTilesetDoc` populates `Tileset.TileProbabilities[local]` when `Weight` is set.
- `packages/game/tilemap/physics.go` implements `physics.TileGrid` and `physics.TileGridOrigin` directly on `*Tilemap` (`TileSize`, `GridSize`, `GridOrigin`, `TileFlagsAt`), delegating to `FlagsAt` so any tile marked `solid` or `oneWay` in a tileset file collides through `physics.MoveAndSlide` with no game code naming the tile (in addition to `tileshape.New` for custom rect/polygon shapes).

### Single-grid inline tilemaps with `tilesetFile`

- `packages/game/sceneformat/format.go` and `build.go` allow version 2+ inline single-grid tilemaps to name `tilesetFile` + `data` in place of `tileset` + `data` (`TilemapDoc.HasTilesetFile()`), still refusing `tileset` + `tilesetFile` and `data` + `tileLayers`. `Build` builds `tilesetFile` + `data` into `BuiltLayer.Map` (`*tilemap.Tilemap`) with 1-based tile IDs (`n` -> local tile `n - 1`) and all tileset flags, shapes, animations and properties.
- `packages/game/tooling/assetcheck/scene.go` validates `HasTilesetFile()` for both layered grids (`tileLayers`) and flat grids (`data`), reporting broken `tilesetFile` paths and out-of-range tile IDs with file and line; `packages/game/refs/scan.go` records unreadable `*.tileset.yaml` files alongside valid `KindTileset` scans.

### Tabbed Tileset Editor, right-side Tile Inspector, live animation loop, and MCP parity

- `packages/game/editor/tileset_editor.go` and `tileset_editor_ui.go` extend `TilesetEditorPanelView` with four tabs (**Terrain**, **Collision**, **Animation**, **Data**) over one shared atlas grid with visual badges (solid, one-way, shapes, animation, terrain mask, default). Every edit flows through `Editor.History` (`tilesetDocCmd`), validates via `tilemap.ValidateTileset`, and saves canonical YAML.
- `Editor.liveResolver()` wraps `Editor.Resolver` while a tileset is open so `Rebuild()` and `PlayIn()` see live unsaved tileset edits immediately before saving to disk.
- `Editor.syncTileAnimLoop()` drives a `game.Loop` on `Editor.scope` whenever the built scene has animated tiles or the Tileset Editor's animation preview is playing, calling `StepTileAnimations(dt)` to advance both `e.Built.Update(dt)` (live animated tiles in the 2D edit viewport) and the panel preview deterministically without background goroutines.
- When a 2D tile tool is active on a YAML tileset layer with no object selected (`inspectedTilesetTile`), `InspectorView` renders the right-side Tile Inspector (swatch, tile name, source cell, `COLLISION` segmented control, `RULES`, and jump to the Tileset Editor). Switching to a tile tool (`SetTool`) or picking a tile (`SetActiveTileID`) clears `inspectLayer` so the Tile Inspector appears reliably.
- `packages/game/editor/ui.go` converts `tool-fill` and `tool-pick` to compact icon toggle buttons (`lucide.PaintBucket`, `lucide.Pipette`) and sets `Side: 28` on the 2D view framing buttons so the 2D `editor-toolbar` stays at most 2 rows at `1120x680` and keeps the file browser scroll view visible.
- `packages/game/editor/editormcp/tileset.go` adds `tileset_read` and `tileset_edit` (atomic via `History.Group`) and teaches `scene_read` to serve the open tileset's unsaved edits, keeping agent and UI editing on the exact same `Editor` methods.

This extends [YAML tilesets and layered tilemaps](2026-11-25-yaml-tilesets-and-layered-scene-tilemaps.md) and [Editor terrain painting with autotile rules and tileset editor](2026-11-25-editor-terrain-autotile.md).

## Alternatives considered

- **Define a second `*.tileset.yaml` schema or version 2 for collision/animation/data**: Rejected because version 1 `TilesetDoc` (#847) already had `solid`, `oneWay`, `flags`, `shapes`, `animation` and `properties`; adding optional `name` and `weight` preserves full backward compatibility and one canonical format.
- **Keep `Editor.Rebuild()` reading tilesets only from disk until saved**: Simpler resolver wiring, but animated tiles and collision/name edits made in the Tileset Editor would not preview live in the 2D viewport or Play mode until written to disk. Wrapping `Resolver` with `liveResolver()` keeps `sceneformat.Build` pure while reflecting live edits immediately.
- **Require game code to wrap `BuiltLayer.Map` in a custom `physics.TileGrid` adapter**: Rejected because #878 requires a tile marked solid in the tileset editor to collide through `physics.MoveAndSlide` with no Go code naming the tile; implementing `physics.TileGrid` directly on `*tilemap.Tilemap` works alongside `tileshape.New` with zero extra allocations.
- **Add a new Tileset button to the 2D top toolbar**: Rejected because the 2D `editor-toolbar` already had 8 tool buttons at `1120x680` and wrapping to a third row squeezed the file browser to zero scroll height; opening the Tileset Editor from the Tile Palette, right-side Tile Inspector, and File Browser keeps the top toolbar within two rows.

## Consequences

- **Benefits**: One versioned YAML tileset format drives tile collision (`physics.MoveAndSlide` and `tileshape`), live viewport/game animations, custom data, terrains, editor UI, right-side Tile Inspector, `game check`, `game mv`, and MCP agent tools with deterministic round-trip YAML and single-step undo/redo.
- **Costs**: `liveResolver()` re-encodes the open tileset document on `Rebuild()` while the Tileset Editor is open (negligible for tileset YAML sizes, and zero cost when closed). `tool-fill` and `tool-pick` render as icon buttons with accessibility `Label`s instead of text labels to preserve the two-row `1120x680` toolbar budget.

## Verification

- `env -u GOROOT go run ./cmd/gameschema -check`
- `env -u GOROOT go tool nsgo test ./tilemap` (`TestTilesetFormatConformance`, `TestTilesetTileNameWeightAndMoveAndSlide`), `./sceneformat` (`TestInlineTilemapWithTilesetFileTakesFlagsAndAnimations`, `TestMergeCoversAllDocumentFields`), `./tooling/assetcheck` (`TestInlineTilesetFileChecksBrokenRefAndOutOfRangeTile`), `./refs`, `./editor/editormcp` (`TestTilesetReadAndEditViaMCP`), and `TMPDIR=/tmp env -u GOROOT go tool nsgo test ./editor` (`TestTilesetEditorSolidTileCollidesInGameWithMoveAndSlide`, `TestTilesetEditorAnimatedTileAnimatesInViewportAndGame`, `TestTilesetEditorCollisionShapesCustomDataAndUndo`, `TestOpenFromTheFileBrowserAsksFirst`, `TestDiscardFollowsTheProjectBack`, `TestLightingHandlesAlignOnParallaxLayer`).
