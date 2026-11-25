# rooms

`game/rooms` splits a 2D world into rooms with their own camera bounds, as metroidvania platformers do. When the player walks from one room into the next, a `Director` clamps the `tilemap.Camera` to the new room and plays its transition: a cut, a slide or a fade.

```go
import "github.com/nsgo-dev/game/packages/game/rooms"
```

## Room maps

Rooms live in a versioned YAML file. Each room is a rectangle in world points; the map's `transition` plays when entering any room, and a room's own `transition` overrides it.

```yaml
version: 1
transition:
  kind: slide
  duration: 300ms
rooms:
  - id: entrance
    name: room.entrance
    x: 0
    "y": 0
    width: 320
    height: 180
  - id: hall
    x: 320
    "y": 0
    width: 640
    height: 180
  - id: crypt
    x: 320
    "y": 180
    width: 320
    height: 360
    transition:
      kind: fade
      duration: 500ms
      easing: easeIn
      color: '#102030'
```

| Field | Meaning |
| --- | --- |
| `version` | Schema version, `1` (`rooms.FormatVersion`) |
| `transition` | Played when entering a room that sets none; omitted means a cut |
| `rooms[].id` | Unique, non-empty ID used in code and saves |
| `rooms[].name` | Display name or localization key; not interpreted |
| `rooms[].x`, `y`, `width`, `height` | The room's rectangle in world points; the size must be positive |
| `rooms[].transition` | Played when entering this room |
| `transition.kind` | `cut`, `slide` or `fade` |
| `transition.duration` | A Go duration with a unit, such as `400ms`; omitted means 400 ms for a slide and 600 ms for a fade. A cut takes none |
| `transition.easing` | `linear`, `ease`, `easeIn`, `easeOut` or `easeInOut`; omitted means `easeInOut` for a slide and `linear` for a fade. A cut takes none |
| `transition.color` | A fade's cover as a CSS color; omitted means black. Only a fade takes one |

`rooms.Parse` and `rooms.LoadFS` refuse unknown fields, other versions, missing or duplicate IDs, empty or non-finite rectangles, bare numbers as durations, unknown kinds or easings and malformed colors, with errors wrapping `rooms.ErrInvalidMap`. `rooms.Encode` writes the same map back byte for byte: fields in a fixed order, 2-space indentation and nothing generated. It quotes the `y` key because YAML 1.1 reads a bare `y` as `true`; hand-written files may leave it bare. A negative zero is written as `0`, so saving a loaded map twice gives the same bytes.

A point belongs to a room when it is inside its left and top edges and before its right and bottom ones, so rooms sharing an edge never both hold it. `Map.RoomAt` returns the first room in list order holding a point.

## Laying rooms out in the editor, LDtk or Tiled

You can edit `*.rooms.yaml` files visually right over your scene in the [scene editor](editor.md): open a `*.rooms.yaml` beside the scene from the file browser or Scene Settings inspector, create/move/resize/delete rooms with handles snapped to the scene's tile grid, edit each room's `id`, `name` and `transition` in the inspector with full Undo/Redo, and save deterministic YAML via `rooms.Encode`.

Rooms can also come from an external level editor instead of being typed by hand. `rooms.FromLDtk` makes one room per level of an LDtk project in a Free or GridVania layout, named after the level, and `rooms.FromTiledLayer` makes one room per rectangle in a Tiled object layer, named after the object and moved by the layer's offset, including the offsets of the groups that hold it. Both refuse what cannot be a room, such as a Linear layout, an ellipse or an unnamed object, instead of skipping it.

```go
data, _ := fs.ReadFile(assets, "world.ldtk")
list, err := rooms.FromLDtk(data, "") // "" reads a single-world project
if err != nil {
	return err
}
m := &rooms.Map{
	Version:    rooms.FormatVersion,
	Transition: &rooms.Transition{Kind: rooms.KindSlide},
	Rooms:      list,
}
if err := rooms.Validate(m); err != nil {
	return err
}
```

## Moving the camera between rooms

A `Director` owns the camera's position and bounds. Call `Start` once where the player spawns, then `Update` on every fixed step with the player's position instead of `Camera.Follow`:

```go
dir := rooms.NewDirector(m, cam)
dir.Smoothing = 10 // Camera.Follow smoothing between transitions; 0 locks on the player
dir.Start(player.Pos)

// Inside game.Loop.Update:
if !dir.Transitioning() {
	player.Update(dt) // freeze the player while the camera moves, as most metroidvanias do
}
if dir.Update(player.Pos, dt) {
	hud.ShowRoomName(dir.Room().Name)
}
cam.Update(dt) // screen shake still runs

// Inside game.Loop.Draw:
d.Save()
cam.Apply(d)
drawWorld(d)
d.Restore()
dir.DrawFade(d) // view space: the fade's cover over the whole view
```

Each step, `Update` keeps the current room while the player is still inside it, so standing where two rooms overlap never flickers between them, and keeps it too when the player is in no room. When the player enters another room, `Update` sets `Camera.Bounds` to that room, returns `true` and starts the room's transition:

- **cut**: the camera snaps to the player, clamped to the new room.
- **slide**: the camera pans from where it was to the player's clamped position in the new room along the easing curve, following the player if they keep moving, and lands clamped exactly when the duration ends.
- **fade**: the view is covered with the fade's color over the first half while the camera holds, the camera cuts to the new room at the midpoint, and the view is uncovered over the second half. `FadeAlpha` returns the cover's opacity and `DrawFade` paints it over the `Camera.Width` by `Camera.Height` view.

When the player changes room again during a fade, the cover never jumps: a new fade covers on from the same opacity, even with a different easing, and a slide or cut leaves the cover to clear as the interrupted fade would have while the camera moves. A slide interrupted by another room change pans on from wherever the camera is. A target with a NaN or infinite coordinate is ignored for that step, so a bad position never reaches the camera. `Room`, `Previous`, `Transition` and `Progress` report where the camera is going, where it came from, what is playing and how far it has run. Everything advances by the fixed `dt` only, so a replay produces the same camera path bit for bit.
