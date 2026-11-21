// Package physics provides 2D collision shapes, exact overlap contacts, axis-aligned tile resolution and spatial queries for games.
//
// Coordinates follow NSGo's screen space: X increases to the right and Y increases downward.
//
// Shapes (Box and Circle, unified by Shape) test overlaps with TestBoxBox, TestCircleCircle, TestBoxCircle, TestCircleBox and TestShapes, returning a Contact whose unit normal points from B toward A by the penetration Depth needed to separate them.
//
// MoveAndSlide advances a Body over a fixed game.Loop step against any TileGrid (or the concrete Grid), resolving solid walls, floors, ceilings and one-way platforms without tunneling at high speeds.
//
// World groups bodies and an optional TileGrid for deterministic overlap, point and raycast queries.
//
// DrawShape, DrawBody, DrawContact, DrawTileContacts, DrawTileGrid, DrawWorld and DrawRaycast record zoom-compensated debug overlays into a core.Drawing; builds tagged release compile them to no-ops.
package physics
