# worldmap

`game/worldmap` remembers which rooms of a game world the player has discovered and draws them as a minimap of the current area and a world map, with markers. What the player has discovered goes into a save slot and comes back on load.

```go
import "github.com/nsgo-dev/game/packages/game/worldmap"
```

## Map definitions

A map is a YAML file with a `version` field. Each room has an ID, world-space bounds and, optionally, a display name, an area (a floor or region drawn as its own map) and links to neighbouring rooms. Fixed markers, such as save points or bosses, sit inside a room and appear once that room is discovered.

```yaml
version: 1
rooms:
  - id: entrance
    name: Castle Gate
    x: 0
    "y": 0
    width: 320
    height: 180
    links: [hall, cellar]
  - id: hall
    x: 320
    "y": 0
    width: 320
    height: 180
  - id: cellar
    area: crypt
    x: 0
    "y": 0
    width: 320
    height: 180
markers:
  - id: save-gate
    kind: save
    room: entrance
    x: 40
    "y": 90
    label: Gate shrine
```

`Parse` refuses unknown fields, other versions, duplicate IDs, links to missing rooms (or the same pair listed twice), sizes that are not positive (or that float32 rounding erases at the room's position), areas too large for float32 and rooms that overlap inside one area; rooms in different areas may overlap. `Encode` writes canonical YAML with a fixed key order: it parses back to the same `Doc` and encodes again to the same bytes. Comments, flow style and other formatting of a hand-written file are not kept, so a file saved with `Encode` loses them. The encoder quotes the key `"y"` because YAML 1.1 reads a bare `y` as a boolean; both forms parse.

`Load(data)` parses a file and builds a `*Map`; `New(doc)` builds one from a `Doc` you made in code.

## Discovery

```go
m, err := worldmap.Load(data)

// Each fixed step, with the player's world position:
if id, first := m.Visit(math2d.Vec2{X: px, Y: py}); first {
	toast("Discovered " + id)
}

m.Enter("hall")          // a room transition moved the player into hall
m.Reveal("tower")        // a map item shows a room without visiting it
m.RevealArea("crypt")    // or a whole area
```

| Method | Result |
| --- | --- |
| `Visit(p)` | Marks the room under `p` visited and current; returns its ID and whether this is the first visit. Outside every room it keeps the current room. |
| `Enter(id)` | Same by ID; `ErrUnknownRoom` for an unknown ID. |
| `Reveal(ids...)`, `RevealArea(area)` | Unknown rooms become `Revealed`; visited rooms stay `Visited`. A bad ID changes nothing. |
| `Status(id)` | `Unknown`, `Revealed` or `Visited`. |
| `Visited()`, `Revealed()` | Sorted room IDs. |
| `Explored(area)` | Visited and total rooms of an area, for a completion percentage. |
| `RoomAt(p)` | The room containing `p`. Where areas overlap, the current room wins, then the current area, then definition order; with no current room, definition order alone decides. `Visit` picks its room the same way. |

## Markers

Players can place their own markers. A marker needs an unused ID, a kind and a position inside an existing room (edges count):

```go
err := m.AddMarker(worldmap.Marker{ID: "pin-1", Kind: "note", Room: "hall", X: 400, Y: 90, Label: "locked door"})
m.RemoveMarker("pin-1")
```

`Markers()` returns fixed and player markers sorted by ID, `VisibleMarkers()` only those whose room is revealed or visited, and `PlayerMarkers()` only the player's. Fixed markers cannot be removed. Invalid markers return `ErrInvalidMarker`.

## Saving

`m.State()` returns a `State`: the version, the current room, the sorted visited and revealed room IDs and the player's markers. Equal states encode to equal JSON. Store it in a `save.Store` slot with the rest of the game and hand it back with `Restore`:

```go
type Slot struct {
	Gold int            `json:"gold"`
	Map  worldmap.State `json:"map"`
}

store.Save("slot1", Slot{Gold: gold, Map: m.State()})

var s Slot
if err := store.Load("slot1", &s); err == nil {
	err = m.Restore(s.Map)
}
```

`Restore` checks the whole state first and returns `ErrInvalidState` without changing the map when the version differs, a room is unknown or listed twice, the current room was not visited or a marker is invalid. Decoding a `State` from JSON or YAML refuses unknown fields. A save that names a room the definition no longer has is refused, so when a game renames or removes rooms, add a `save.WithMigration` step that rewrites the saved IDs. `Reset` forgets everything discovered.

## Drawing

Both views draw into a `core.Drawing`, clipped to a destination rectangle, and show only revealed and visited rooms of one area; unknown rooms and their markers are never drawn.

```go
style := worldmap.DefaultStyle()

core.Canvas{
	Style: core.Style{Width: core.Pt(160), Height: core.Pt(160)},
	Draw: func(d *core.Drawing, size core.Size) {
		// The current room's area around the player, 0.2 map points per world unit.
		m.DrawMinimap(d, core.Bounds{Size: size}, player, 0.2, &style)
	},
}

// A pause-menu world map of one area, fitted to the canvas.
m.DrawWorldMap(d, core.Bounds{Size: size}, "", &player, &style)
```

- `DrawMinimap` centers the player in the rectangle at a fixed scale and draws the current room's area. A scale that is not positive and finite counts as 1; a NaN or infinite player position draws nothing.
- `DrawWorldMap` fits every room of the area, discovered or not, into the rectangle inset by `Style.Padding`, so the layout does not shift as the player explores. The player is drawn only when the current room is in that area and the position is finite.
- `Style` sets the background, the fills of visited, revealed and current rooms, borders, links between discovered rooms, marker colors (`MarkerKinds` per kind) and sizes. A transparent color leaves that part out, except `Current`: a transparent `Current` fills the current room with `Visited`. `nil` uses `DefaultStyle()`.
- `WorldMapProjection` and `MinimapProjection` return the `Projection` a view uses. `ToWorld` turns a tap on the map into a world point, for example to place a marker with `RoomAt` and `AddMarker`.

Drawing the same map twice records the same operations, and a map restored from a save draws exactly as the one that was saved.

## Tile minimap

`TileMinimap` renders a `tilemap.Tilemap`'s layers as a compact minimap image for RTS, strategy, roguelike and top-down RPG games, with optional `fog.Fog` compositing, unit and point-of-interest markers, the camera's visible rectangle, and click-to-pan.

```go
mm := worldmap.NewTileMinimap(tm, nil)
style := worldmap.DefaultTileMinimapStyle()

// Inside a Canvas Draw callback:
mm.Draw(d, core.Bounds{Size: size}, &worldmap.TileMinimapOverlay{
	Fog:    f,
	Team:   playerTeam,
	Camera: cam,
	Markers: []worldmap.TileMarker{
		{Position: heroPos, Kind: "ally"},
		{Position: enemyPos, Kind: "enemy", RequireVisible: true},
		{Position: shrinePos, Color: core.RGB(0xf2c14e)},
	},
}, &style)

// On pointer click at clickPos (in the same coordinates as dst):
mm.PanCamera(cam, core.Bounds{Size: size}, clickPos, &style)
```

### Tile colors and incremental updates

Each cell is composited from bottom to top across every visible tile layer (respecting layer opacity, or the subset listed in `TileMinimapOptions.Layers`). A tile's color comes from:

1. `TileMinimapOptions.TileColor(tileID)` when provided and returning `ok = true`.
2. The tile's custom color property (`TileMinimapOptions.ColorProperty`, defaulting to `DefaultTileColorProperty`, `"minimap_color"`), parsed via `tilemap.Properties.Color`.
3. Otherwise, the alpha-weighted average color of the tile's non-transparent pixels from its tileset image (`Image.Pixels`, embedded `Image.Data`, or `TileMinimapOptions.Images` matched by URI, URI without `"asset:"`, or tileset name). Ignoring fully transparent pixels preserves opaque terrain colors for diamond isometric tile sprites.

Edits made through `Tilemap.SetTile` (and small `Tilemap.FillRect` calls) are tracked by `Tilemap.TileRevision` / `Tilemap.TileChangesSince`, so `Sync` and `Draw` recompute only the changed cells rather than redrawing the whole map. Call `Invalidate` after mutating `layer.Data`, `layer.Visible`, `layer.Opacity` or tile properties directly.

### Fog, markers, camera and click-to-pan

- **Fog of war**: when `TileMinimapOverlay.Fog` is non-nil, `TileMinimap` composites `TileMinimapStyle.Fog` (`fog.DefaultStyle()` when both colors are transparent) over `Unseen` and `Explored` cells and caches the result against `Fog.Revision(Team)`. Unchanged frames skip recompositing completely, and single-tile `SetTile` edits patch only the changed cells in the composited image when the fog revision has not changed.
- **Markers**: each `TileMarker` draws a square at its world `Position` using `Color`, `TileMinimapStyle.MarkerKinds[Kind]`, or `TileMinimapStyle.Marker`. Set `RequireVisible: true` to hide enemy blips unless their map cell is currently `fog.Visible` to the drawn team.
- **Camera rectangle**: when `TileMinimapOverlay.Camera` is non-nil, `Draw` outlines the camera's visible region (axis-aligned for unrotated cameras, rotated quad when `Camera.Rotation != 0`).
- **Orthogonal and isometric maps**: orthogonal maps render 1 pixel per tile; `tilemap.OrientationIsometric` maps render as a solid `(W+H) x (W+H-1)` diamond aligned with `Tilemap.WorldBounds`, with transparent corners outside the diamond.
- **Click-to-pan**: `Projection`, `WorldToMinimap`, and `MinimapToWorld` convert between world and minimap coordinates for both orthogonal and isometric maps. `PanCamera(cam, dst, click, style)` centers `cam` on the clicked world point when `click` lies inside `dst` and calls `cam.Clamp()`.
