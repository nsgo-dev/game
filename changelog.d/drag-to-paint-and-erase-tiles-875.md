### Added

- `game/editor`: drag with Paint or Erase to paint or erase every cell the stroke crosses, recorded as one undo step, with a right-button drag erasing while Paint is active; an outline of the cell under the mouse that shows what a press would do; and picking the tile in a cell as the active tile with Alt+click or the new Pick tool. `Editor.BeginTileStroke`, `ContinueTileStroke`, `EndTileStroke`, `CancelTileStroke`, `PickTile`, `SetTileHover` and `HoveredTile` do the same in Go (#875).
