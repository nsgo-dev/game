// Package rooms splits a 2D world into rooms with their own camera bounds and moves a tilemap.Camera between them with a cut, slide or fade transition, as metroidvania platformers do.
//
// # Room Maps
//
// A Map is a versioned YAML file listing rectangular rooms in world points and the transition each one plays when the player enters it. Parse decodes and validates a map, refusing unknown fields, unsupported versions, duplicate IDs, empty or non-finite rectangles, unknown transition kinds or curves and malformed colors. Encode writes deterministic 2-space YAML that Parse reads back to the same Map. FromLDtk and FromTiledObjects build rooms from an LDtk project's levels or a Tiled object layer's rectangles, so a map can be laid out in either editor.
//
// # Director
//
// A Director owns the camera's position. Each fixed step, Update finds the room holding the target (usually the player), keeping the current room while the target is still inside it so overlapping edges do not flicker. When the target enters another room, Update sets Camera.Bounds to that room and plays its transition:
//
//   - cut snaps the camera to the target, clamped to the new room.
//   - slide pans the camera from where it was to the target's clamped position in the new room over the transition's duration, along its curve.
//   - fade covers the view with a color over the first half, cuts the camera to the new room at the midpoint and uncovers the view over the second half; DrawFade paints the cover.
//
// Outside transitions Update follows the target with tilemap.Camera.Follow, clamped to the current room. Transitioning reports when a transition is playing, so a game can pause its player until the camera lands.
package rooms
