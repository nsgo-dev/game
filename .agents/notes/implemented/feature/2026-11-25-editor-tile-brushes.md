# Agent Note: Editor rectangle, line, stamp and random tile brushes

Status: implemented

## Problem

The editor's tile tools supported only single-cell freehand paint/erase strokes ([tile strokes](2026-11-25-editor-tile-strokes.md)) and flood fill, with a single-tile radio palette ([tile palette](2026-11-25-editor-tile-palette.md)). Authoring larger tile rooms, diagonal lines, repeating patterns, copied map regions, or natural floor variation required repetitive cell-by-cell work (#876).

## Decision

`packages/game/editor/tilebrush.go` and extensions to `tilestroke.go`, `tilepalette.go`, `viewport.go`, `commands.go`, `menubar.go`, and `ui.go` add four tile brushes (`ToolRectTile`, `ToolLineTile`, `ToolStampTile`, `ToolRandomTile`) and a map-region clipboard (`CopyTileRegion`, `CutTileRegion`, `PasteTileRegion`), keeping every edit a single `tileEditCmd` on `Editor.History`:

- **Deferred preview strokes for multi-cell brushes.** Unlike freehand `TileStrokePaint`/`TileStrokeErase` (which mutate the grid live while dragging), `TileStrokeRect`, `TileStrokeRectErase`, `TileStrokeLine`, `TileStrokeLineErase`, `TileStrokeStamp`, `TileStrokeRandom`, and `TileStrokeSelectRegion` accumulate a deterministic row-major preview slice (`Editor.TilePreview`) without mutating `Editor.Doc` until `EndTileStroke`. Shrinking a rectangle or pivoting a line never leaves stale edits, `DrawViewport` renders the live preview with Spec accent (`#3B7BFF`) and erase (`#FF6B6B`) overlays plus ghost tiles, and `CancelTileStroke` (including pressing Escape in `ViewportView`, which also sets `strokeTookPress` so releasing the button cannot fire a stray `OnPress` paint) discards the preview cleanly without touching the document or history.
- **Bresenham line vs 4-connected freehand walk.** `TileStrokeLine` / `DrawTileLine` uses classic 8-connected Bresenham (`bresenhamLineClipped`, clipping extreme off-map coordinates to `[-1, w] x [-1, h]` first so runtime stays `O(max(w, h))`), whereas `TileStrokeRandom` uses the existing 4-connected `cellsAlong` walk so fast freehand random strokes leave no gaps.
- **Stamp patterns preserve empty cells and layered flips.** `TileStamp` stores row-major `Tiles []int` (where `0` is empty) and optional `Flips []uint32`. Copying or cutting a map region captures both tile IDs and layered-grid flip bits verbatim so pasting elsewhere reproduces empty cells and flips exactly. Dragging `ToolStampTile` aligns every cell modulo `(stamp.Width, stamp.Height)` relative to the stroke anchor `(startCol, startRow)` so multi-tile patterns tile seamlessly without smearing. Right-dragging with `ToolStampTile` starts `TileStrokeSelectRegion` to set `TileSelection` for `Mod+C`/`Mod+X`/`Mod+V`.
- **Palette rectangle selection and brush bar keep single-tile accessibility and toolbar height intact.** `TilePalette` exposes `Columns` (from YAML tileset `Columns` or wrapped panel columns) for `SelectPaletteStamp` and `SelectPaletteStampRange`. `TilePaletteView` hosts the `Rect`, `Line`, `Stamp`, and `Random` brush toggles (`tile-brush-bar`) above the tileset so the top editor toolbar stays on two rows at the default 1120×680 window size (avoiding squeezing the left file browser). `TilePaletteView` keeps `base.RadioGroup` for single-tile tools (Paint, Erase, Fill, Pick, Rect, Line) and switches to multi-select buttons with Stamp/Random controls only when `ToolStampTile` or `ToolRandomTile` is active.
- **Coordinate-hashed deterministic random brush.** `ToolRandomTile` picks from `WeightedTile` entries via `randomPickTile(seed, col, row, pool)` using a pure Go `splitmix64` hash of `(seed, col, row)` mapped to `[0, totalWeight)`. For a fixed seed (`RandomBrushSeed`, default `1`), every cell's pick is deterministic across runs, architectures, and drag orders, and the hover ghost, live preview, and committed cells always agree.
- **Collision-free command bindings.** The unified command registry binds `u` (Rect), `l` (Line), `c` (Stamp, avoiding Terrain's `t`), `n` (Random), and `mod+c`/`mod+x`/`mod+v` (Copy/Cut/Paste Tile Region), all guarded with `core.KeyNotEditing` and exposed in the desktop menu bar and Command Palette.

## Alternatives considered

### Mutate `Editor.Doc` live on every pointer move for Rect and Line

Live mutation matches `TileStrokePaint`, where cells are only added. Rejected for Rect and Line because moving the opposite corner shrinks or pivots the shape, which would require restoring previous cells and calling `Rebuild()` on every move; deferred preview keeps `Editor.Doc` untouched until release, avoids repeated rebuilds during drag, and makes Escape cancellation trivial.

### Stateful PRNG stream (`math/rand/v2`) stepped per painted cell for Random brush

Stepping a PRNG in cell-visit order is common in paint tools. Rejected because visit order depends on pointer drag direction and speed, and hover previews could not match what a stroke will place; hashing `(seed, col, row)` with `splitmix64` makes fixed-seed output reproducible regardless of stroke traversal order and keeps preview and commit identical.

### Skip `0` (empty) cells when pasting a `TileStamp`

Treating `0` as transparent can be convenient for irregular stamps. Rejected because copying a map region that contains empty tiles and pasting it over existing tiles must reproduce the exact copied tile IDs rather than leaving stale tiles behind in the empty slots.

## Consequences

- **Benefit:** Rectangles, Bresenham lines, palette/map stamps, map region copy/cut/paste, and deterministic weighted random brushes all work across single-grid and layered tilemaps with live previews, Escape cancel, and one-step Undo/Redo.
- **Benefit:** Existing freehand tile strokes, hover colors, single-tile radio-group palette semantics, and keyboard shortcuts remain unchanged.
- **Cost:** Deferred preview strokes draw ghost tiles per previewed cell in `DrawViewport` instead of through the scene's compiled tile batch; very large rectangle previews draw one overlay op per cell.
- **Testing:** `tilebrush_internal_test.go` verifies `bresenhamLine`, `bresenhamLineClipped` with extreme coordinates, and `randomPickTile` determinism and 9:1 weighted distribution; `tilestroke_internal_test.go` covers the updated `toolStrokeMode` table; `tilebrush_test.go` proves rectangle fill/erase, Bresenham line fill/erase, palette and map-region stamp/copy/cut/paste, fixed-seed random brush determinism, mounted UI shortcuts/Escape cancel/palette controls, layered-grid flip preservation with YAML round-trip, and locked-layer/validation guards.
