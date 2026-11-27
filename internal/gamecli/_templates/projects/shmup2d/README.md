# NSGo Vertical Shooter Template

A vertical scrolling shooter built with [NSGo](https://github.com/nsgo-dev/nsgo) and [`github.com/nsgo-dev/game`](https://github.com/nsgo-dev/game): one stage of enemy waves, bullet patterns, power-ups, a boss with three phases, lives, bombs and a score, playable with the keyboard, the mouse, a game controller and touch, on desktop, mobile and the web.

It shows:

- A fixed-step `game.Loop` with interpolated drawing into a full-screen `core.Canvas`, through the 270×480 `screen.Viewport` that `game.yaml` sets.
- Bullet patterns from YAML with `bullets`: the ship's four weapon levels, the enemies' aimed shots and rings, and the boss's fans, spirals and blooms.
- A stage in YAML of its own: the ship, the enemy kinds and their paths, the waves and their drops, and the boss's phases.
- Explosions from a `particles` definition in YAML, sprites painted in code into one `sprite.Atlas`, and sounds and music from the `placeholder` pack through a `sound.Mixer` whose buses `game.yaml` sets.
- Controls from `game.yaml`'s input map through `input.Actions`, and dragging with a finger or the mouse.
- Title, play and end screens with `scene.Manager`, and a native HUD and menus over the canvas.
- Headless `nsgotest` playthroughs that reach both the win and the loss.

## Playing

| | Keyboard | Controller | Touch and mouse |
| --- | --- | --- | --- |
| Fly | Arrow keys or WASD | Left stick or D-pad | Drag anywhere: the ship moves as far as the finger or the mouse does |
| Fire | Z, Space or J | South (A) or right trigger | Held while dragging |
| Focus, a slower ship | Shift | Left shoulder or left trigger | |
| Bomb | X or K | East (B) | On a touch screen, the bomb button at the bottom right; with the mouse, X or K |
| Pause | Escape or P | Start | The Pause button |
| Start, play again | Return | Start | The Start and Play again buttons |

Destroy every enemy of a wave and the last one drops its item: **P** raises the weapon a level, **B** gives a bomb and **L** a life. A hit costs a life and a weapon level and clears the screen; a bomb clears every enemy bullet, damages everything on the screen and shields the ship for a moment. Only the white dot in the middle of the ship can be hit. Defeat the boss to clear the stage; lose the last life and the game is over.

## Running and testing

Run with hot reload on your desktop, or on another target:

```sh
go tool nsgo dev
go tool nsgo dev -target web
go tool nsgo dev -target ios_simulator
go tool nsgo dev -target android
```

Run the tests, vet across platforms and build:

```sh
go tool nsgo test ./...
go tool nsgo check -target macos,windows,linux,android,web
GOFLAGS=-tags=release go tool nsgo build -target macos_arm64,windows_x64,linux_x64,web
```

`go test -run '^$' -fuzz FuzzParseStage .` fuzzes the stage format, and `go test -run '^$' -bench Step .` measures a step of the boss fight.

## Layout

- `game.yaml`: the project file, with the window, the 270×480 playfield, the input map, the audio buses and the export presets. `nsgo.yaml` follows it; run `go run github.com/nsgo-dev/game/cmd/gameproject sync` after editing it.
- `data/stage.yaml`: the stage. `stage.go` defines its format, which refuses unknown fields and checks every value.
- `data/patterns.yaml`: the bullet patterns, in the `bullets` package's format.
- `data/explosion.yaml`: the explosion particles, in the `particles` package's format.
- `world.go`: one run of the stage, its components (the ship, enemies, waves, items and boss) and the systems that step them in order: the timeline, movement, firing, bombs, collisions and pickups. It reads no device and plays no sound; it takes one step's `controls` and reports `events`, so tests drive it directly.
- `draw.go`: draws a run over the scrolling starfield.
- `controls.go`: the input map's actions and the drag, turned into one step's `controls`.
- `game.go`: the root component, the loop and the title, play and end screens; `hud.go`: the HUD, with the boss's health bar, and the menus.
- `art.go`: the sprites, painted in code; `audio.go`: the sounds and music; `data.go`: loads the data files.
- `world_test.go`, `game_test.go`: the tests, which play the stage with `pilot_test.go`'s pilot through a controller to the win, and leave the ship idle through the keyboard to the loss; `stage_test.go`: the data files' format and the fuzz test; `project_test.go`: `game.yaml` and `nsgo.yaml`.

## Where to change it

- **The stage**: edit `data/stage.yaml`. Add a wave with a time, an enemy kind, a count, a place and a drop; tune the ship's speed, lives and bombs; give the boss more hit points or another phase. The tests check the file still loads and is in canonical form, and the pilot still clears it.
- **Bullet patterns**: edit `data/patterns.yaml` (`ring`, `spiral` or `aimed`, with counts, speeds, spins and intervals), and name a pattern from an enemy kind, a boss phase or the ship's weapons.
- **A new enemy path or boss move**: add a case to `moveEnemies` or `moveBoss` in `world.go` and its name to the checks in `stage.go`.
- **A new item**: add its kind to `stage.go`, its effect to `collectItems` and its frame to `art.go`.
- **More stages**: load another stage file in `data.go` and start a run of it when one is cleared.
- **Art and sound**: replace `makeAtlas` with `sprite.LoadAtlas` or a `placeholder.Sheet`, keeping the frame names, and `cues` in `audio.go` with your own sounds under `resources/`.
- **Controls**: edit the input map in `game.yaml`. The touch bomb button is `bombRegion` in `controls.go`, outside the input map so that a finger already steering the ship cannot set it off by sliding over it.

`CREDITS.md` lists where the assets come from.

## Git setup

Each clone enables the object-by-object scene merge driver once (`.gitattributes` names it, while Git keeps driver config local to `.git/config`):

```sh
go tool game merge -setup
```
