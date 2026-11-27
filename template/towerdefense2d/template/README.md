# Bramble Gate: a tower defense template

A small, complete 2D tower defense game built with [NSGo](https://github.com/nsgo-dev/nsgo) and [`github.com/nsgo-dev/game`](https://github.com/nsgo-dev/game). Enemies walk a road from the spawn to the gate; you build towers on the grass beside it, upgrade them with the money the enemies drop, and call each wave when you are ready. Stop all five waves to win; let ten lives' worth of enemies through the gate and you lose.

It shows:

- A level, its waves and its enemies in `data/level.yaml`, the towers and their upgrades in `data/towers.yaml`, and the projectiles in `data/projectiles.yaml`, all loaded through `game.yaml`'s autoloads and checked against each other at startup.
- The road found with `pathfind.AStar` over the map's road tiles, and the map drawn by `tilemap` from tiles painted in code.
- Tower and enemy stats on `stats.Sheet`s: upgrades are permanent stat effects, and a frost tower's slow is a timed modifier on the enemy's speed.
- Towers firing `bullets` patterns that lead their target, with splash damage and slows read from the tower's stats.
- A fixed-step `game.Loop`, a `screen.Viewport` that fits the map to any window, and a native HUD of NSGo text and buttons.
- Keyboard, gamepad and touch controls from `game.yaml`'s input map, and headless `nsgotest` playthroughs that win and lose the game with each of them.

Edit the name and identifier in `game.yaml` before shipping it, then write them into `nsgo.yaml` and check the project:

```sh
go run github.com/nsgo-dev/game/cmd/game project sync
go run github.com/nsgo-dev/game/cmd/game project check
```

## Running and testing

```sh
go tool nsgo dev                     # this machine, with hot reload
go tool nsgo dev -target web         # the browser
go tool nsgo dev -target ios_simulator
go tool nsgo dev -target android
go tool nsgo test ./...              # headless tests
go tool nsgo check -target macos,windows,linux,android,web
GOFLAGS=-tags=release go tool nsgo build -target macos_arm64,windows_x64,linux_x64,web
```

## Controls

| Action | Keyboard | Gamepad | Touch and mouse |
| --- | --- | --- | --- |
| Move the cursor | Arrow keys or WASD | D-pad or left stick | Tap a tile |
| Choose a tower | 1, 2, 3, or Q and E | Shoulder buttons | Tower buttons |
| Build | Enter or Space | A (south) | Tap the cursor's tile again |
| Upgrade | U | Y (north) | Upgrade |
| Sell | X or Backspace | X (west) | Sell |
| Call the next wave | N | Start | Next wave |
| Speed 1x, 2x, 3x | F | Right trigger | Speed |
| Pause | P or Escape | Back | Pause |
| Restart | R | Start or A on the end screen only | Play again |

The bindings are the `input` section of `game.yaml`: add a key or a button to an action there.

## Layout

- `game.yaml`: the project file: name, id and version, window and virtual resolution, the input map, the data files to autoload and the export presets.
- `data/level.yaml`: the level: tile size, starting money and lives, the map, the enemy kinds and the waves.
- `data/towers.yaml`: the tower kinds: cost, color, stats, upgrades and the pattern each fires, and the share of the cost selling returns.
- `data/projectiles.yaml`: the projectiles, a `bullets` pattern file.
- `data.go`: the level and tower formats, their validation, and `LoadData`, which reads the three files through `game.yaml`.
- `world.go`: the simulation: the road, spawning, walking, targeting, firing, hits, money, lives and the outcome, on fixed steps with no clock or randomness.
- `game.go`: the controller: the loop, input actions, the cursor, and the HUD's state.
- `view.go`: the NSGo views (status bar, battlefield canvas, toolbar, end banner) and the drawing of the battlefield.
- `art.go`: the ground tiles and projectiles, drawn in code.
- `project.go`, `main.go`: `game.yaml` embedded as `Project`, and the entry point.
- `*_test.go`: format, simulation and playthrough tests.
- `game.index.yaml`: the stable IDs of the project's files, kept by `game index`.

## Where to change it

- **The map**: edit `map` in `data/level.yaml`. Each character is a tile: `.` grass you can build on, `=` road, `S` the spawn, `G` the gate, `^` rock and `~` water. Every row has the same length, there is exactly one `S` and one `G`, and road must join them; enemies take the shortest road. The map's size in game points, `tileSize` (at most 256) times its columns by `tileSize` times its rows, must equal `width` and `height` under `display` in `game.yaml`, so change both together; `LoadData` refuses a map that does not match.
- **Waves and enemies**: each wave in `data/level.yaml` is groups that spawn side by side, each a `count` of one enemy every `interval` after a `delay`, and a `bonus` paid when the wave is cleared. Enemies have `health`, `speed` and an optional `armor` stat, a `reward` and how many lives a `leak` costs.
- **Towers**: in `data/towers.yaml`, a tower needs `damage`, `range` and `cooldown` stats; `splash` makes its hits damage everything around the target, and `slow` with `chill` slows what it hits. Each upgrade is a cost and stat modifiers (`add`, `mul` or `override`). Its projectile is the `pattern` it names in `data/projectiles.yaml`, which fires one shot.
- **Art**: `art.go` paints every tile and projectile. Replace `makeAtlas` with `sprite.LoadAtlas` and an atlas of your own to use image files.
- **Rules**: `world.go` holds them all, so a new kind of tower behavior is a new stat read in `World.hit` or `World.fire`.

After changing a data file, run `go tool nsgo test ./...`: `TestDataFilesAreCanonical` checks the files load and are in the form the game writes, and the playthrough tests show whether `winningPlan` in `plan_test.go` still wins. Run `go run github.com/nsgo-dev/game/cmd/game index` to update `game.index.yaml`.

## Git setup

Each clone enables the object-by-object scene merge driver once (`.gitattributes` names it, while Git keeps driver config local to `.git/config`):

```sh
go tool game merge -setup
```
