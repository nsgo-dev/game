# The platformer2d template

`platformer2d` is a small side-scrolling platformer: a lantern bearer runs and jumps through the three rooms of Lantern Hills, collects sparks, lights a lamp that becomes the respawn point and reaches the beacon to win. Spikes and pits cost a life, and losing the last one loses the run. It is built on [`platformer`](platformer.md), [`physics`](physics.md), [`tilemap`](tilemap.md), [`rooms`](rooms.md), [`input`](input.md), [`sprite`](sprite.md) and [`screen`](screen.md), and its art is drawn by code.

```sh
go run github.com/nsgo-dev/game/cmd/game@latest new -template platformer2d mygame
cd mygame
go tool nsgo dev
go tool game playtest
```

`nsgo init -template github.com/nsgo-dev/game/template/platformer2d` writes the same project, as [Templates](templates.md) describes.

## What it shows

- A [`platformer.Controller`](platformer.md) over a `physics.CharacterBody` with variable-height jumps, coyote time, jump buffering, one-way ledges, wall slides and wall jumps, all tuned by `data/player.yaml`.
- A level drawn as text in `data/level.yaml`, built into a `physics.Grid` to collide with, a `tilemap.Tilemap` to draw and `physics.Trigger` sensors for the spikes, sparks, lamp and beacon.
- Rooms with their own camera bounds from `data/rooms.yaml`: a [`rooms.Director`](rooms.md) slides the camera from one room to the next, and the player waits until it lands.
- One input map in `game.yaml` for the keyboard and game controllers, bound with `Project.BindInput`, paired with `input.TouchControls` from `controls.touch.yaml` so on-screen touch controls sit inside the safe area in view coordinates.
- A 320 by 180 virtual screen scaled by whole multiples, with a native HUD and an end-of-run panel over the canvas, requesting landscape orientation on phones (`cx.RequestOrientation(core.OrientationLandscape)`).
- Tests that play the level through to a win with the keyboard, a controller and two-finger touch, run into the spikes until the run is lost, and check the rules, the controller's timing and that every data file is canonical.

## Controls

| | Keyboard | Controller | Touch |
| --- | --- | --- | --- |
| Run | Left and right arrows, A and D | Left stick, d-pad | The d-pad at the bottom left (slide between left and right) |
| Jump (hold to jump higher) | Space, up arrow, W | South button | The JUMP button at the bottom right |
| Play again after the run ends | R, Enter | Start | The Play again button |

The on-screen controls appear from the start on phones and tablets, stay hidden on desktop until the screen is touched, and hide again when a key or controller is used.

## How it is organised

| File | What it holds |
| --- | --- |
| `game.yaml` | The project file: the window, the virtual screen, the input map and the export presets. |
| `controls.touch.yaml` | The on-screen touch layout: a bottom-left d-pad and a bottom-right jump button, read by `input.TouchControls`. |
| `data/level.yaml` | The level: one character per tile, its tile size and the lives a run starts with. |
| `data/rooms.yaml` | The rooms in world points and the camera transition into them, in the format of [`rooms`](rooms.md#room-maps). |
| `data/player.yaml` | The controller's tuning, in the format of [`platformer`](platformer.md). |
| `level.go` | The level format: parsing, validation and canonical encoding. |
| `world.go` | One run of the level: the collision grid, the player, the sensors, the lives, the checkpoint, the goal and the rooms, stepped once per fixed step without knowing about devices or drawing. |
| `game.go` | The root component, the fixed-step loop, input sampling, the HUD and the end-of-run panel. |
| `draw.go`, `art.go` | Drawing a run, and the tileset and player frames drawn by code. |
| `playtest.yaml`, `playtest_test.go` | The [`platformer-runner`](playtest.md) playtest configuration and `TestPlaytest` adapter driven by `go tool game playtest`. |
| `*_test.go` | The playthroughs, the rules, the controller's timing and the data files. |

The level is plain text, one row per line:

```yaml
version: 1
name: Lantern Hills
tileSize: 16
lives: 3
tiles: |
  #....................#
  #.P....^^....o.......#
  ######################
```

| Glyph | Tile |
| --- | --- |
| `.` | Empty |
| `#` | Ground, solid from every side |
| `=` | A ledge you can jump up through |
| `^` | Spikes, which cost a life |
| `o` | A spark to collect |
| `C` | A checkpoint lamp |
| `G` | The goal beacon, exactly one |
| `P` | Where the player starts, exactly one |

`name` is shown on the panel at the end of a run. Every row has the same width, and every `P` and `C` has ground or a ledge under it. Unknown fields, unknown glyphs and a version other than 1 are refused, and the game writes the file back byte for byte, so a review shows only real edits.

## Where to change it

- **A new level**: edit the tiles in `data/level.yaml` and keep `data/rooms.yaml` covering every column; `parseLevel` refuses a start (`P`) or lamp (`C`) with nothing to stand on, and `TestDataFilesAreCanonical` and `TestRoomsCoverTheLevel` report what is wrong. The playthrough test jumps at spikes, pits and walls ahead of the player, so a route that needs more than that needs a change to `obstacleAhead` in `game_test.go`.
- **How the jump feels**: `data/player.yaml`. A longer `coyote_time` or `jump_buffer` forgives late and early presses, and `wall_jump_speed_y: 0` turns wall jumps off.
- **A new tile kind**: a glyph in `level.go`, a case in `newWorld` for what it does and one in `newTilemap` for how it looks.
- **Enemies and moving platforms**: give each a `physics.Body`, move it in `world.step`, pass moving platforms to `Controller.Step`, and add a sensor that sets `hurt`.
- **Controls**: the `input` section of `game.yaml` for keyboard and controller, and `controls.touch.yaml` for the on-screen touch controls.
- **Art**: load an atlas with `sprite.LoadAtlas` in `newPlayerAtlas` and a tileset image in `newTilemap`, or load the whole level from a Tiled map with `tilemap.LoadTiled`.
- **Sound**: a `sound.Mixer` configured from an `audio` section in `game.yaml`.
