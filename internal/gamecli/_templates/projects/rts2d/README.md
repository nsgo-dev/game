# Shardfront: a 2D real-time strategy template

A small skirmish built with [NSGo](https://github.com/nsgo-dev/nsgo) and [`github.com/nsgo-dev/game`](https://github.com/nsgo-dev/game): two bases on one map, crystals to mine, barracks to build, three unit types, fog of war, box selection, control groups and a computer opponent. Destroy the opponent's headquarters before it destroys yours; both falling in the same step is a draw.

It shows:

- `game/rts`: one `rts.Crowd` moves every unit over the map's grid on shared flow fields, `rts.BoxSelector` and `rts.Selection` turn clicks, drags, taps and the gamepad cursor into selections, `rts.ControlGroups` keeps ten numbered groups, and ordered groups take the formation in `data/formations.yaml`.
- `game/fog`: each side sees from its units and buildings; rocks block sight, the opponent is hidden until scouted, and explored ground stays dimmed. An enemy building out of sight shows as it was when last seen.
- `game/ai`: the opponent scores its choices with a utility reasoner built from `data/opponent.yaml`, and gives its orders through the same functions as the player. It sees through its own fog of war, and knows the map, so it knows where your headquarters stands.
- `game/input` and `game.yaml`'s input map: keys and gamepad buttons are named actions, so a player or a settings screen can rebind them.
- A fixed-step `game.Loop`, a `core.Canvas` drawn through the `screen.Viewport` from `game.yaml`'s `display`, and native NSGo buttons for the HUD and the command bar.
- Headless tests that play whole skirmishes to a win and a loss through the mouse and keyboard, and check the gamepad and touch controls.

## Playing

| | Keyboard and mouse | Gamepad | Touch |
| --- | --- | --- | --- |
| Select | Click a unit; drag a box; double-click for every unit of that type in view; Shift adds, Ctrl or Command toggles | Move the cursor with the left stick or the D-pad; South picks, hold South and move to drag a box | Tap a unit; drag a box |
| Command | Right-click: on an enemy to attack it, on crystals to mine them with workers, on ground to go there, fighting on the way | East at the cursor | Tap ground, crystals or an enemy with units selected |
| Control groups | Ctrl or Command with a digit binds the selection, Shift with a digit adds to it, a digit recalls it, twice to highlight it | Keyboard only | Keyboard only |
| Train worker, soldier, ranger | Q, W, E | West, North, right shoulder | Command bar |
| Build barracks | B | Left shoulder | Command bar |
| Select army, workers | A, S | Left trigger, right trigger | Command bar |
| Clear selection | Escape | Back | Clear button |
| Pause, play again | P; Enter on the result screen | Start | Pause button |

The arrow keys, Space and Enter drive the same cursor as the gamepad, so the keyboard alone plays too.

Workers mine on their own from the start and after they are trained. Combat units left idle attack the enemies they see, so units at home defend the base.

## Running and testing

```sh
go tool nsgo dev                    # this machine, with hot reload
go tool nsgo dev -target web        # the browser
go tool nsgo dev -target ios_simulator
go tool nsgo dev -target android
go tool nsgo test ./...             # headless tests, playthroughs included
go tool nsgo check -target macos,windows,linux,android,web
go tool nsgo build -target macos_arm64,windows_x64,linux_x64,web
```

The game's settings live in `game.yaml`, its project file. Edit the name and identifier there before shipping it, then write them into `nsgo.yaml` and check the project:

```sh
go run github.com/nsgo-dev/game/cmd/gameproject sync
go run github.com/nsgo-dev/game/cmd/gameproject check
```

## Layout

- `game.yaml`: the project file, with the window, the 480 by 264 virtual resolution the battlefield is drawn at, the input map and the export presets. `project.go` embeds it as `Project`; `main.go` runs `Root` with its window.
- `data/rules.yaml`: units, buildings and the economy: health, speed, damage, range, sight, costs, training and build times, starting units, queue and unit limits.
- `data/map.yaml`: the battlefield as rows of characters (`.` ground, `#` rock, `*` crystals, `A` and `B` the two headquarters, `a` and `b` each side's build pads).
- `data/formations.yaml`: the formation ordered groups take, in `game/rts`'s format.
- `data/opponent.yaml`: the opponent's actions and the considerations that score them, in `game/ai`'s utility format.
- `data.go`: reads and checks the data files; each schema has a version, refuses unknown fields and writes back the same bytes it read.
- `world.go`: the simulation: units, buildings, gathering, production, combat, fog of war and the end of the game. It knows nothing of views or input.
- `orders.go`: the orders both sides give: train, build, command, attack-move.
- `opponent.go`: the computer player.
- `control.go`: the player's selection, control groups, cursor, and mouse, touch, keyboard and gamepad handling.
- `view.go` and `draw.go`: `Root`, the HUD, the command bar, the pause and result screens, and the battlefield drawn by code.
- `*_test.go`: data checks, simulation and opponent tests, every input scheme, and the two playthroughs.

## Where to change it

- Balance: edit `data/rules.yaml`; the opponent's priorities and its attack size: `data/opponent.yaml`. The playthrough tests play real skirmishes, so a large change to either may need their strategy in `playthrough_test.go` adjusted.
- A new map: edit `data/map.yaml`, keeping one `A` and one `B` with ground under their footprints and no two footprints sharing a cell.
- A new unit type: the controls, the command bar and the opponent train `worker`, `soldier` and `ranger` by name, so `data/rules.yaml` must keep those three (loading it fails otherwise). To add one, add it to `data/rules.yaml`, then give it a shape in `drawUnit`, a hotkey in `game.yaml` and `readActions`, and a button in `commandBar`.
- A plain move, to retreat: a ground command is an attack-move, so units fight on the way. Give `command` a modifier or a second button that calls `moveGroup` with `orderMove` instead, which ignores enemies until the units arrive.
- Art: everything is drawn by code in `draw.go`; swap a kind of thing at a time for sprites from `game/sprite`.
- A larger map than the screen: draw under a camera (`tilemap.Camera`) and center it where a control group recalled twice is, where `focus` highlights it now.
- Sound, a title screen, saves and replays: the world steps deterministically on fixed steps, so `game/sound`, `game/scene`, `game/save` and `game/replay` fit around `skirmish.update` without changing the simulation.

## Git setup

Each clone enables the object-by-object scene merge driver once (`.gitattributes` names it, while Git keeps driver config local to `.git/config`):

```sh
go tool game merge -setup
```
