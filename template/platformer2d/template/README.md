# NSGo Game Platformer Template

A small 2D side-scrolling platformer built with [NSGo](https://github.com/nsgo-dev/nsgo) and [`github.com/nsgo-dev/game/packages/game`](https://github.com/nsgo-dev/game). A lantern bearer runs and jumps through three rooms of Lantern Hills, collects sparks, lights a lamp that becomes the respawn point and reaches the beacon to win. Spikes and pits cost a life; losing the last one loses the run.

It shows:

- A `platformer.Controller` over a `physics.CharacterBody`: run, variable-height jumps, coyote time, jump buffering, one-way ledges, wall slides and wall jumps, tuned by `data/player.yaml`.
- A level drawn as text in `data/level.yaml`, built into a `physics.Grid` to collide with, a `tilemap.Tilemap` to draw and `physics.Trigger` sensors for spikes, sparks, the lamp and the beacon.
- Rooms with their own camera bounds from `data/rooms.yaml`: a `rooms.Director` slides a `tilemap.Camera` between them and the player waits while it does.
- One input map in `game.yaml` for the keyboard, game controllers, touch and the mouse, bound into an `input.Actions` with `Project.BindInput`; on-screen controls are drawn where the touch regions are.
- A 320 by 180 virtual screen scaled by whole multiples, a native HUD and end-of-run panel over the canvas, and art drawn by code.
- Tests that play the level through with the keyboard, a controller and touch to a win, run into the spikes to a loss, and check the data files, the rules and the controller's timing.

## Controls

| | Keyboard | Controller | Touch |
| --- | --- | --- | --- |
| Run | Left and right arrows, A and D | Left stick, d-pad | The left and right arrows at the bottom left |
| Jump (hold to jump higher) | Space, up arrow, W | South button (A on Xbox, Cross on PlayStation) | The arrow at the bottom right |
| Play again after the run ends | R, Enter | Start | The Play again button |

The mouse plays like touch: hold the left button over an on-screen control. The controls are drawn on phones and tablets, and anywhere once the game view has been touched or clicked.

## Running and testing

```sh
go tool nsgo dev                      # the desktop, with hot reload
go tool nsgo dev -target web          # the browser
go tool nsgo dev -target ios_simulator
go tool nsgo dev -target android
go tool nsgo test ./...               # headless tests
go tool nsgo check -target macos,windows,linux,android,web
go tool nsgo build -target macos_arm64,windows_x64,linux_x64,web
```

The game's settings live in `game.yaml`, its project file. Edit the name and identifier there before shipping it, then write them into `nsgo.yaml` and check the project:

```sh
go run github.com/nsgo-dev/game/packages/game/cmd/gameproject sync
go run github.com/nsgo-dev/game/packages/game/cmd/gameproject check
```

## Layout

- `game.yaml`: the project file, with the window, the virtual screen, the input map and the export presets.
- `data/level.yaml`: the level, one character per tile (the legend is on `Level` in `level.go`), its tile size and the lives a run starts with.
- `data/rooms.yaml`: the rooms, in world points, and the camera transition into them.
- `data/player.yaml`: the controller's tuning.
- `data.go`: embeds and parses the data files.
- `level.go`: the level format: parsing, validation and canonical encoding.
- `world.go`: a run of the level: collision grid, player, sensors, lives, checkpoint, goal and rooms, stepped once per fixed step and independent of devices and drawing.
- `game.go`: the root component, the fixed-step loop, input sampling, the HUD and the end-of-run panel.
- `draw.go` and `art.go`: drawing the run, and the tileset and player frames drawn by code.
- `main.go` and `project.go`: the entry point and the embedded `game.yaml`.
- `*_test.go`: playthroughs, rules, controller timing and the data files.

## Where to change it

- Make a level: edit the tiles in `data/level.yaml`, keeping every row the same width with one `P` and one `G`, and ground (`#`) or a ledge (`=`) under every `P` and `C`, and keep `data/rooms.yaml` covering every column. `TestDataFilesAreCanonical` and `TestRoomsCoverTheLevel` say what is wrong. If you change the route, change the playthrough's `obstacleAhead` in `game_test.go` too, or give it a list of jumps.
- Change how the player feels: `data/player.yaml`. Raise `coyote_time` and `jump_buffer` for a more forgiving jump, set `wall_jump_speed_y: 0` to turn wall jumps off.
- Add a tile kind: a glyph in `level.go`, a case in `newWorld` for what it does and one in `newTilemap` for how it looks.
- Add enemies or moving platforms: give each a `physics.Body`, move it in `world.step`, pass moving platforms to `Controller.Step` and add a sensor that sets `w.hurt`.
- Rebind a control: the `input` section of `game.yaml`; the touch regions are in virtual screen points.
- Replace the art: load an atlas with `sprite.LoadAtlas` in `newPlayerAtlas` and a tileset image in `newTilemap`, or load the whole level from a Tiled map with `tilemap.LoadTiled`.
- Add sound with `sound.Mixer`, configured from an `audio` section in `game.yaml`.
