# Agent Note: Editor MCP tile tools and region-based tile edit review

Status: implemented

## Problem

The editor supports interactive tile painting, terrain autotiling ([editor-terrain-autotile](./2026-11-25-editor-terrain-autotile.md)), brush tools ([editor-tile-brushes](./2026-11-25-editor-tile-brushes.md)), and the tile palette ([editor-tile-palette](./2026-11-25-editor-tile-palette.md)), while `editormcp` exposes object edits (`scene_edit`) and tileset edits (`tileset_read`/`tileset_edit`). Coding agents had no MCP tools to query empty tile pockets, paint weighted tile variations, run terrain autotiling, or erase tile regions on a scene layer, and authors had no way to review hundreds of agent-edited cells grouped into a handful of independently revertible regions as designed in `docs/design/editor/Tiles.dc.html`.

Three invariants needed explicit contracts:

1. **Per-step undoability plus single-step Keep**: each agent tile tool call (`tile_paint`, `tile_autotile`, `tile_erase`) must be a valid undoable edit on its own, yet reverting a subset of regions in the review and pressing **Keep N of M** must collapse all kept regions into **one** undoable `History` step whose single `Undo` restores the exact pre-review tilemap.
2. **Coherent region grouping and exact outlines**: painted, replaced, and erased cells must group into distinct connected regions with stable IDs and exact perimeter outlines (rather than overlapping bounding boxes), without splitting diagonal autotile corners across unrelated regions.
3. **Shortcut and toolbar compatibility**: holding `B` during an active tile review must peek Before without hijacking the `b` shortcut (`CmdToolPaintTile`) when no review is open, and all review controls must stay out of the 2D `editor-toolbar` so it remains within two rows at `1120×680`.

## Decision

### Tile query, batch agent edits, and region-based review (`packages/game/editor/tile_review.go`)

- `Editor.QueryTiles` scans an inline single-grid or layered tilemap (filtered by `EmptyOnly`, `TileIDs`, `TileNames` resolved via the YAML tileset, and optional `Rect`) using 4-connected BFS by default (`Diagonal: true` for 8-connected), returning regions sorted largest-first with bounding rect, cell count, `Rectangular` flag, explicit coordinates, and human-readable summaries (e.g. `"an 11 × 5 pocket at columns 2–12, rows 12–16"`).
- `Editor.AgentPaintTiles`, `Editor.AgentAutotile`, and `Editor.AgentEraseTiles` validate bounds, layer lock/visibility, play mode, tile IDs/names, weighted pools, and terrain sets, and execute each call through `History` as one `tileEditCmd`. When `TrackReview` is true (default for MCP calls), they open or extend `Editor.tileReview` (mirrored per-tab in `sceneTab`).
- `recomputeTileReviewRegions` diffs `base` vs `proposed` cell-by-cell into `TileReviewPainted` (`0 -> >0`), `TileReviewReplaced` (`a -> b`), and `TileReviewErased` (`>0 -> 0`), groups connected components (8-connected per kind so diagonal autotile corners stay together while disjoint edits and different kinds stay separate), assigns deterministic IDs (`<kind>_<col>_<row>`) to preserve revert toggles across subsequent agent steps, and generates region titles and details (such as `"from 4 cave tiles"` or `"stone → cave autotile"`).
- **History ownership and single-step Keep**: `tileReviewState` records `historyStart` and `historyOwned`. While every command pushed since `historyStart` belongs to the active review, `KeepTileReview` collapses `[historyStart..cursor)` and executes one net `tileEditCmd` (`base -> composedKept`), and `RevertAllTileReview` restores `base` and rolls `History` back cleanly to `historyStart`. If a non-review command is interleaved (`syncTileReviewAfterMutate`), `historyOwned` clears and Keep/RevertAll fall back to a single delta command so interleaved user edits are never dropped.

### Viewport overlays, review panel, hold-B peek, and palette polish (`tile_review_ui.go`, `tilepalette.go`, `viewport.go`, `ui.go`)

- `drawTileReviewOverlay` draws translucent cell fills, exact cell-perimeter outlines in Spec line styles (green `#4FD38A` dashed for `painted`, amber `#E9A84A` dashed for `replaced`, red `#FF6B6B` solid for `erased`), and a `#3B7BFF` focus halo in `DrawViewport`.
- `viewportTileOverlays` adds a non-interactive (`PointerEventsNone`) top-left layer dimension caption (`viewport-tile-caption`, e.g. `World · 24 × 20 cells · 16 px`) whenever a tile layer is selected in 2D, plus the top-center Reviewing `Before / After` + `hold B` pill and bottom-left line-style legend while a tile review is open.
- `TileReviewPanelView` replaces the right-side `InspectorView` only while a 2D tile review is open (leaving the 2D `editor-toolbar` untouched) and displays the agent step trace, region cards with per-region `Revert` / `Restore`, `Keep N of M`, and `Revert all`, styled strictly with Spec tokens.
- Hold `B` is wired through both `TileReviewHandleKeyEvent` (ignoring modifier chords and Play mode) and a `core.KeyHeld(cx, "b")` effect mounted only while `TileReviewOpen()` is true; `bindShortcuts` in `commands.go` skips `CmdToolPaintTile` for `"b"` only while a 2D tile review is open.
- `TilePalette` enriches `PaletteTile` with `Name`, `Col`, `Row`, `Collision`, and `Terrain`, displays the `TILESET` / `<file>` header and a bottom selected-tile readout card, and stays visible during an active 2D tile review as well as tile tools.

### MCP server tools (`packages/game/editor/editormcp/tiles.go`)

Registers nine tools in `builtinTools()`: `tile_query` (`read`), `tile_paint`, `tile_autotile`, `tile_erase`, `tile_review_status` (`read`), `tile_review_revert_region`, `tile_review_restore_region`, `tile_review_keep`, and `tile_review_revert_all` (`edit`), supporting both 1-based tile IDs (`PaintTile` convention where `0` is unambiguously empty) and YAML tileset `tileName` lookups.

Related notes: [editor-cutscene-timeline-review](./2026-11-25-editor-cutscene-timeline-review.md), [editor-terrain-autotile](./2026-11-25-editor-terrain-autotile.md), [editor-tile-brushes](./2026-11-25-editor-tile-brushes.md), [editor-tile-palette](./2026-11-25-editor-tile-palette.md).

## Alternatives considered

1. **Stage pending tile review edits purely outside `Editor.History` (as `CutsceneReview` does for files on disk)**: rejected because scene tile layers live in the live `SceneDoc`, and individual agent steps (`tile_paint`, `tile_autotile`, `tile_erase`) are explicitly required to be undoable edits on their own if undone before Keep, while still collapsing into one undo step on Keep.
2. **Draw region outlines as simple bounding rectangles**: rejected because L-shaped walls and cave borders (wrapping three sides of a pocket) would draw boxes across untouched interior cells; tracing outer cell perimeter edges outlines arbitrary region shapes accurately.
3. **Show `TilePaletteView` unconditionally whenever a tile layer is active, even with `ToolSelect`**: rejected because existing editor behavior and `TestTilePaletteChoosesTheTilePaintSaves` expect the tile palette to hide when switching back to `ToolSelect` outside a tile review; showing the palette for tile tools and active tile reviews while showing `viewport-tile-caption` for any selected tile layer satisfies both contracts.

## Consequences

- **Benefits**:
  - Agents can query empty pockets, paint weighted variations, re-edge with terrain autotiles, erase regions, and drive reviews end-to-end through MCP.
  - Authors review large tile edits as a small set of visual regions with Before/After + hold `B`, per-region Revert/Restore, and a clean single-step Undo after Keep.
  - Zero buttons added to the 2D toolbar and zero regressions to existing tile palette or shortcut tests.
- **Costs**:
  - `tileReviewState` stores full `base` and `proposed` `[]int64` grid snapshots while a review is open; these are freed immediately on Keep, Revert all, or scene close.
  - Switching layers for a subsequent agent tile edit auto-commits an open review on the previous layer rather than tracking multi-layer reviews simultaneously.
