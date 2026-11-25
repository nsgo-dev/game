// Package worldmap keeps track of which rooms of a game world the player has discovered and draws them as a minimap of the current area and a world map, with markers.
//
// # Map Definitions
//
// A map is defined in a versioned YAML document (Doc): rooms with an ID, an optional name and area, world-space bounds and links to neighbouring rooms, plus fixed markers such as save points or bosses. Parse refuses unknown fields, unsupported versions, duplicate IDs, links to missing rooms and rooms that overlap inside one area; Encode writes deterministic 2-space YAML that parses back to an equal Doc and encodes again to the same bytes; comments and hand-written formatting are not preserved.
//
// # Discovery
//
// New builds a Map from a Doc. Visit finds the room under the player's position and marks it visited, Enter marks a room by ID (for room transitions), and Reveal marks rooms as known without visiting them (for a map item). Players can also place their own markers with AddMarker.
//
// State returns the discovered rooms, the current room and the player's markers as a value with sorted lists that encodes to the same JSON every time, so it can be stored in a save.Store slot; Restore validates such a value against the map and applies it, or changes nothing.
//
// # Drawing
//
// DrawMinimap draws the discovered rooms of the current room's area around the player at a fixed scale, clipped to a rectangle. DrawWorldMap fits a whole area into a rectangle and draws its discovered rooms. Rooms that are neither visited nor revealed, and the markers in them, are never drawn. Both views take a Style and use a Projection, which also converts a point on the drawn map back to world coordinates (to place a marker where the player taps).
//
// # Tile Minimap
//
// TileMinimap renders a tilemap.Tilemap's layers into a compact cached RGBA image with one color per tile (from a tile custom property, or else the average color of its tileset image), updating only cells changed via Tilemap.SetTile. It overlays a team's fog.Fog cached by fog.Revision so unchanged frames skip recompositing, draws unit and point-of-interest markers and a tilemap.Camera's visible rectangle, supports both orthogonal maps and diamond isometric maps, and converts clicks back to world coordinates with MinimapToWorld and PanCamera.
package worldmap
