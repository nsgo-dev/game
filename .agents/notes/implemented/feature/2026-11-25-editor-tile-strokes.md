# Agent Note: Editor tile strokes, hover outline and tile picking

Status: implemented

## Problem

Painting in the editor was one press per cell, and each cell was its own undo step (#875): a floor of a few hundred tiles took a few hundred clicks and a few hundred Undos. Nothing showed which cell a press would hit, and the only way to choose the tile to paint was the palette ([tile palette](2026-11-25-editor-tile-palette.md)), with no way to take a tile from the map.

## Decision

`packages/game/editor/tilestroke.go` adds tile strokes, the hover outline and picking; `viewport.go` drives them from the pointer.

- **A stroke edits live and records once.** `BeginTileStroke(mode, x, y)` starts a stroke (`TileStrokePaint`, `TileStrokeErase` or `TileStrokePick`) on the active layer's inline tilemap, `ContinueTileStroke` carries it to each point the pointer reaches and `EndTileStroke` records every cell it changed as one `tileEditCmd` ("Paint 12 tiles"). Cells change in the document while the stroke runs, with a `Rebuild` after each move that changed one, so the map shows the stroke as it is drawn; each cell is recorded once with the ID it held before the stroke, so the step undoes exactly. `History.Execute` then runs `Do`, which writes the values already there. A stroke that changed nothing records nothing. `CancelTileStroke` puts the cells back and records nothing.
- **Interpolation walks every crossed cell.** `cellsAlong` clips the segment between the last and the new pointer point to the map (Liang-Barsky) and walks it with Amanatides-Woo in tile units, so a fast drag leaves no gaps and the walk is bounded by the map's size, not the pointer's distance. Each step goes to a side neighbor: a line through a grid corner takes one of the two cells beside it, so a diagonal stroke is 4-connected and a stroke drawn as a wall has no diagonal gaps. A segment end the clip leaves in place is used exactly, since `x0+1*(x1-x0)` need not round back to `x1`.
- **A stroke never outlives the context it started in.** Choosing a tool or a plugin tool, changing the active layer or the view mode, Undo, Redo and Play end a stroke under way first (`endTileStrokeForChange`), so Undo during a drag takes the whole stroke back; opening another scene drops it with the scene's history.
- **Pointer mapping is one table.** `toolStrokeMode(secondary, alt)` decides what a press starts: Alt picks with any tile tool, the right button erases with Paint and Erase, otherwise Paint paints, Erase erases and Pick picks; Fill starts no stroke and keeps filling on a press. A Space-drag never starts one.
- **Mouse and touch feed the same stroke.** The viewport frame's `OnMouseDown`/`OnMouseMove`/`OnMouseUp` and one-finger `OnTouch` begin, continue and end it. A begin while a stroke is under way is refused, which matters on the web, where a left mouse button reports both pointer touches and mouse events. Alt is read from the event's modifiers or `core.KeyHeld(cx, "alt")`, since touches carry no modifiers. A second finger cancels the stroke, so a two-finger pan that lands one finger first leaves the map as it was.
- **The press after a stroke edits nothing more.** The press a mouse button or finger completes may arrive before or after its up. The canvas's `OnPress` skips while a stroke is under way or right after one ended (`strokeTookPress`); every pointer-down clears that flag, because a stroke that no press follows (a drag the frame's pan took, a right-button drag, a tap whose press came before its up) would otherwise swallow the next click of Select, Fill or Place. A press no pointer drew a stroke for (`PressEvent.Keyboard`, an accessibility action) makes a stroke of one cell.
- **The hover outline is drawn from the resting point.** `SetTileHover` stores the canvas point and redraws only when the cell under it, or whether it would pick, changes; `HoveredTile` and the overlay map the point to a cell at draw time, so panning under a resting mouse keeps the outline on the right cell. Paint and Fill show the active tile faded in the cell, Erase tints it red, picking outlines it in amber.
- **Picking is view state.** `PickTile` sets `ActiveTileID` through `SetActiveTileID` and never goes into `History`; an empty cell leaves the active tile, and it refuses while the scene plays, as the tile edits return `ErrPlaying`. The Pick tool hands over to Paint after a pick, so on a touch screen, where there is no Alt, pick-then-paint is one tool press and one tap.

## Alternatives considered

### Record each cell as its own command and merge on end

`History` could have gained command merging, as editors that coalesce typing do. Rejected: it widens a shared contract every command would have to consider, and a stroke already knows its own cells; one command built at the end needs nothing new from `History`.

### Bresenham lines between pointer cells

Bresenham between the cells of the two pointer points is the usual line for pixel art and is cheaper. Rejected because it steps diagonally, so a fast drag leaves corner-to-corner gaps that a tile wall or a floor collider would leak through, and it ignores where in the cells the pointer was; the issue asks for every cell the stroke crosses.

### Drive the stroke from `OnPan`

The frame already has `OnPan`, which gives a translation from the press point. Rejected because a pan activates only past the slop, so the first cell would paint late or not at all for a short drag, and a pan event does not say which mouse button drags; the touch's own position is what both the mouse and the finger report directly.

## Consequences

- **Benefit:** a drag paints a row, a floor or a wall in one gesture and one Undo, with no gaps however fast it moves.
- **Benefit:** the outline shows the cell a press will hit and what it will do there, and Alt or the Pick tool takes a tile from the map into the palette.
- **Cost:** each move that changes a cell rebuilds the whole scene, as one `PaintTile` did; a very large scene could make strokes stutter, and a cheaper path would update the built tilemap's batch in place.
- **Cost:** the brush is one cell; larger brushes and shapes are not part of this change.
- **Testing:** `tilestroke_internal_test.go` checks `cellsAlong` by table, fuzz (`FuzzCellsAlong`: on the grid, no repeats, side-neighbor steps, exact start and end cells) and a benchmark, the tool table and the touch path; `tilestroke_test.go` checks the API and the mounted editor through nsgotest mouse drags, finger drags, right-drags, Alt presses, the Pick tool, Space-drags, the hover outline's draw ops, and Select and Fill clicks after a right-drag or a paint tap.
