# fog

`game/fog` tracks fog of war for strategy games on an orthogonal tile grid: which cells each team can see now, which it has explored before, and which it has never seen. It draws the fog over a tilemap and saves it with the rest of a game.

```go
import "github.com/nsgo-dev/game/packages/game/fog"
```

## Teams, sources and states

A `Fog` covers a grid of cells. Each team (`fog.Team`, any `int`) sees through its vision sources: units, buildings or wards, each with a cell and a radius in cells. To each team a cell is one of three `State`s:

| State | Meaning | Drawn as |
| --- | --- | --- |
| `Unseen` | The team has never seen the cell. | `Style.Unseen`, opaque black by default |
| `Explored` | The team saw the cell before but cannot see it now. | `Style.Explored`, half-transparent black by default |
| `Visible` | One of the team's sources sees the cell now. | Uncovered |

```go
f := fog.NewFromTilemap(level, tilemap.TileSolid) // walls block sight
scout := f.AddSource(fog.Source{Team: player, Col: 4, Row: 6, Radius: 7})
f.Update()
```

- `NewFromTilemap(m, mask)` takes the map's size and tile size, and makes every cell whose tilemap flags include a bit of `mask` opaque. `New(width, height, tileWidth, tileHeight)` starts from an empty grid, and `SetOpaque` changes one cell later, such as when a door opens.
- A cell `dx` columns and `dy` rows from a source is in range when `dx*dx+dy*dy <= Radius*Radius+Radius`, which rounds the disc. Radius 0 sees only the source's own cell, and a source outside the grid sees nothing.
- Opaque cells block sight by symmetric shadowcasting with exact integer slopes: a floor cell A sees a floor cell B exactly when B sees A, and the result never depends on floating-point rounding, so every client in a lockstep match computes the same fog. An opaque cell is itself seen when sight reaches it, so walls show at the edge of vision.
- Sources of the same team share vision; other teams' sources reveal nothing to it.

## Updating as units move

Move sources during a fixed step and call `Update` once before reading or drawing the fog:

```go
loop.Update = func(dt time.Duration) {
	moveUnits(dt)
	for _, u := range units {
		f.MoveTo(u.Vision, u.Pos) // or f.Move(id, col, row)
	}
	f.Update()
}
```

- `Update` recomputes only the teams whose sources or opaque cells changed since the last call, adds what they see to what they have explored, and reports whether any team's fog changed.
- `SetSource` changes a source's team, cell or radius at once; `RemoveSource` drops it when a unit dies, and the team keeps what it explored.
- `State`, `Visible` and `Explored` answer for a cell, and `VisibleAt(team, p)` for a world point, such as to hide enemy units in the fog. `CellAt(p)` turns a world point into a cell using `TileWidth` and `TileHeight`.
- `Explore`, `ExploreRect` and `ExploreAll` mark cells explored without making them visible (a scouting report or a revealed landmark), `Forget` clears everything a team explored except what it sees now, and `ExploredCount` counts its explored cells.
- `Revision(team)` grows whenever a team's visible or explored cells change, so a minimap or a cached fog image redraws only when it differs.

## Drawing over the map

Draw the fog after the tilemap and the units, under the same camera transform:

```go
d.Save()
cam.Apply(d)
level.Draw(d, cam)
drawUnits(d)
f.Draw(d, player, cam, fog.DefaultStyle())
d.Restore()
```

`Draw` fills one path over the unseen cells and one over the explored cells, each built from horizontal runs of cells, and leaves visible cells uncovered. Like `Tilemap.Draw`, it uses the camera only to skip cells outside `cam.VisibleBounds()`, and a nil camera draws the whole grid. A `Style` color with zero alpha leaves those cells uncovered, such as `fog.Style{Unseen: core.RGB(0x000000)}` for a game without remembered terrain.

## Saving

A `Fog` marshals to versioned JSON text, so it goes into a `save.Store` slot inside the game's own state:

```go
type saved struct {
	Units []unitSave `json:"units"` // each unit keeps its fog.SourceID
	Fog   *fog.Fog   `json:"fog"`
}

store.Save("slot1", saved{Units: units, Fog: f})

var s saved
store.Load("slot1", &s) // s.Fog sees and has explored exactly what f did
```

```json
{"version":1,"width":3,"height":2,"tileWidth":8,"tileHeight":8,"opaque":[".#.","..."],"nextSource":2,"sources":[{"id":1,"team":2,"col":2,"row":1,"radius":1},{"id":2,"team":1,"col":0,"row":0,"radius":0}],"teams":[{"team":1,"explored":["#..","..."]},{"team":2,"explored":[".##",".##"]}]}
```

- The snapshot holds the grid and tile size, the opaque cells, the sources with their IDs, and each team's explored cells as rows of `.` and `#`, so a diff of two saves shows where a team explored. Visible cells are not stored: loading recomputes them from the sources and opaque cells.
- The same fog always writes the same bytes: fields in a fixed order, sources by ID, teams in increasing order, and no timestamps. Loading and saving again changes nothing.
- Loading keeps source IDs and the next ID to hand out, so a unit's saved `fog.SourceID` still moves its source.
- Loading refuses unknown fields, a missing version, a newer version (`ErrFutureVersion`), rows that disagree with the grid size, sources or teams out of order or repeated, and grids larger than `MaxCells`; every other error wraps `ErrInvalidSnapshot`, and a refused snapshot leaves the fog unchanged.
