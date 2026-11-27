# Spark Slide

A 2D grid puzzle built with [NSGo](https://github.com/nsgo-dev/nsgo) and [`github.com/nsgo-dev/game`](https://github.com/nsgo-dev/game), made from the `puzzle2d` template.

A spark slides across the board until a wall stops it, lighting every lamp it passes. Light all of a level's lamps before its moves run out to win it; a slide into a void, or running out of moves with a lamp still dark, loses it. A win in the level's par earns three stars, a win a little over par two, and any other win one; each win unlocks the next level, and the level select keeps the best stars of each.

It demonstrates:

- Levels defined in a versioned YAML file, `levels.yaml`, read strictly (unknown fields refused) and kept in the canonical form its encoder writes.
- Undo and redo of every move through `undo.History`, with each move a `Move` that records what it changed.
- A fixed-step `game.Loop` that reads `input.Actions`, bound from the input map in `game.yaml`, and animates moves with `tween`.
- Progress saved with `save.Store` in the app's storage on every platform.
- Native NSGo controls (`core.Text` and `base.Button`) for the level select, the counters, the buttons and the result, over a board drawn on a `core.Canvas` with no image files.
- Headless tests that play the levels through `nsgotest` to a win and to a loss with the keyboard, a game controller, touch and the mouse.

## Controls

| | Keyboard | Game controller | Touch and mouse |
| --- | --- | --- | --- |
| Slide | Arrow keys or WASD | D-pad or left stick | Swipe, or tap or click toward where to go |
| Undo | Z or Backspace | West or left shoulder | Undo |
| Redo | Y | Right shoulder | Redo |
| Restart | R | North | Restart |
| Confirm (play the highlighted level, next level, retry) | Enter or Space | South | The level's button, Next, Retry |
| Back to the level select | Escape | East | Levels |

The keyboard and game controller bindings are the input map in `game.yaml`; edit them there. The swipes are bound in `newSparkGame`.

The game's settings live in `game.yaml`, its project file. Edit the name and identifier there before shipping it, then write them into `nsgo.yaml` and check the project:

```sh
go run github.com/nsgo-dev/game/cmd/gameproject sync
go run github.com/nsgo-dev/game/cmd/gameproject check
```

## Running and testing

Run with hot reload on your desktop (macOS, Windows or Linux):

```sh
go tool nsgo dev
```

Run on other targets:

```sh
go tool nsgo dev -target web
go tool nsgo dev -target ios_simulator
go tool nsgo dev -target android
```

Run the tests, which play every level and check every level can be won in its par:

```sh
go tool nsgo test ./...
go tool game playtest
```

Vet and check across platforms:

```sh
go tool nsgo check -target macos,windows,linux,android,web
go tool nsgo check -target ios,ios_simulator   # on macOS with Xcode
```

Build release artifacts into `dist/` (`go tool game release` sets `GOFLAGS=-tags=release` automatically to strip debug code):

```sh
GOFLAGS=-tags=release go tool nsgo build -target macos_arm64,windows_x64,linux_x64,web
GOFLAGS=-tags=release go tool nsgo build -target ios,android_arm64
```

## Adding a level

Each level in `levels.yaml` has an `id`, which the save file keys its stars by, so keep it when renaming or moving the level; a `name`; the `moves` allowed; `stars`, the most moves that earn three (`three`, the level's par) and two (`two`) stars; and its `rows`, top row first, one character per tile:

| Character | Tile |
| --- | --- |
| `#` | Wall: the spark stops in the cell before it. Outside the grid counts as wall. |
| `.` | Floor. |
| `S` | The spark's start, on floor. Exactly one. |
| `o` | A lamp, on floor, which lights when the spark passes over or stops on it. At least one. |
| `x` | A void: the spark falls in and the level is lost. |

The levels are played in the order the file lists them. `TestLevelsAreSolvableAtPar` fails until `stars.three` is the fewest moves that win the level and `moves` leaves at least two more, printing the best solution it finds; `TestLevelsFileIsCanonical` prints the canonical form of the file when it is not in it.

## Layout

- `game.yaml`: the project file, with the game's name, id and version, its window, the input map and the export presets.
- `levels.yaml`: the levels. `levels.go` defines its format: `ParseLevels`, `EncodeLevels` and the checks a level must pass.
- `board.go`: the rules. `Board` is a level being played; `slideMove` is one move, which `undo.History` applies, undoes and redoes.
- `game.go`: the game: the loop, the input, moving between the level select and a level, and the animations.
- `draw.go`: how the board is laid out and drawn on the Canvas.
- `ui.go`: the native views: the level select, the counters and buttons, and the result.
- `progress.go`: the saved stars and which levels are unlocked.
- `project.go`, `main.go`: the parsed `game.yaml` and the entry point.
- `playtest.yaml`, `playtest_test.go`: the `puzzle-solver` playtest configuration and `TestPlaytest` adapter driven by `go tool game playtest`.
- `*_test.go`: the tests. `game_test.go` plays the game through `nsgotest`; `levels_test.go` checks the levels and fuzzes their format; `board_test.go` tests the rules and benchmarks a move.
- `nsgo.yaml`: NSGo app metadata and dev settings; its name, id, version and build output and targets follow `game.yaml`.

## Git setup

Each clone enables the object-by-object scene merge driver once (`.gitattributes` names it, while Git keeps driver config local to `.git/config`):

```sh
go tool game merge -setup
```
