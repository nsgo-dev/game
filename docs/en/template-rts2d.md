# The rts2d template

`rts2d` is a 2D real-time strategy skirmish, Shardfront: two bases on one map, crystals to mine, barracks to build, three unit types, fog of war, box selection, control groups and a computer opponent. The side whose headquarters falls first loses; both falling in the same step is a draw. It is small enough to read in an afternoon and laid out the way a strategy game grows: the rules, the map and the opponent's priorities are YAML data files, the simulation knows nothing of views or input, and the player and the opponent give their orders through the same functions.

```sh
go run github.com/nsgo-dev/game/packages/game/cmd/game@latest new -template rts2d mygame
cd mygame
go tool nsgo dev
```

Or, with NSGo's own command, `nsgo init -template github.com/nsgo-dev/game/template/rts2d -module example.com/mygame mygame` ([Templates](templates.md)).

## What it shows

- [rts](rts.md): one `rts.Crowd` moves every unit of both sides over the map's grid on shared flow fields; `rts.BoxSelector` and `rts.Selection` turn clicks, drags, taps and the gamepad cursor into selections; `rts.ControlGroups` keeps the ten numbered groups; and ordered groups take the formation `data/formations.yaml` defines.
- [fog](fog.md): each side sees from its units and buildings, rocks block sight, the opponent's base is hidden until scouted, and explored ground stays dimmed. An enemy building out of sight shows as it was when last seen, and one put up since stays hidden until it is seen.
- [ai](ai.md): the opponent scores its actions (train a worker, build a barracks, train a soldier or a ranger, attack, defend) with a utility reasoner built from `data/opponent.yaml` once a second, and carries out the best one. It sees through its own side's fog of war, as the player does, and knows the map, so it knows where the player's headquarters stands.
- [input](input.md) and the [project file](project.md)'s input map: keys and gamepad buttons are named actions in `game.yaml`, the gamepad's stick and the arrow keys drive one cursor, and the mouse and fingers act on the battlefield view directly.
- A fixed-step [game loop](loop.md) at 60 steps a second, the battlefield drawn by code on a `core.Canvas` through the [screen](screen.md) viewport `game.yaml`'s `display` sets, and native NSGo buttons for the HUD and the command bar.
- Headless tests that play whole skirmishes to a win and a loss through the keyboard and mouse, test the gamepad and touch controls, and check that every data file is canonical.

## Controls

| | Keyboard and mouse | Gamepad | Touch |
| --- | --- | --- | --- |
| Select | Click a unit; drag a box; double-click for every unit of that type; Shift adds, Ctrl or Command toggles | Left stick or D-pad moves the cursor; South picks, hold it and move to drag a box | Tap a unit; drag a box |
| Command | Right-click an enemy to attack it, crystals to mine them, or ground to go there, fighting on the way | East at the cursor | Tap with units selected |
| Control groups | Ctrl or Command with a digit binds, Shift with a digit adds, a digit recalls, twice highlights | Keyboard only | Keyboard only |
| Train worker, soldier, ranger | Q, W, E | West, North, right shoulder | Command bar |
| Build barracks | B | Left shoulder | Command bar |
| Select army, workers | A, S | Left trigger, right trigger | Command bar |
| Clear selection | Escape | Back | Clear button |
| Pause, play again | P; Enter on the result screen | Start | Pause button |

The arrow keys, Space and Enter drive the gamepad's cursor too, so the keyboard alone plays. A barracks goes on a free build pad of the player's base, and workers mine on their own once trained. Combat units left idle attack the enemies they see, so units at home defend it.

## How it is organised

| File | What it holds |
| --- | --- |
| `game.yaml` | The project file: the window, the 480 by 264 virtual resolution, the input map and the export presets. |
| `data/rules.yaml` | Units, buildings and the economy: health, speed, damage, range, sight, costs, training and build times, the starting units, the queue and unit limits. |
| `data/map.yaml` | The battlefield as rows of characters: `.` ground, `#` rock, `*` crystals, `A` and `B` the headquarters, `a` and `b` the build pads. |
| `data/formations.yaml` | The formation ordered groups take, in rts's format. |
| `data/opponent.yaml` | The opponent's actions and the considerations that score them, in ai's utility format. |
| `data.go` | Reads and checks the data files. |
| `world.go`, `orders.go` | The simulation and the orders both sides give. |
| `opponent.go` | The computer player. |
| `control.go` | The player's selection, control groups, cursor, and mouse, touch, keyboard and gamepad handling. |
| `view.go`, `draw.go` | `Root`, the HUD, the command bar, the pause and result screens, and the drawing. |

`data/rules.yaml` and `data/map.yaml` are schemas of the template's own: each has a `version`, refuses unknown fields, and writes back the bytes it read, which `TestDataFilesAreCanonical` checks along with the two files in the packages' formats.

## Where to change it

- Balance: `data/rules.yaml`. The opponent's priorities and the army it attacks with: `data/opponent.yaml`. The playthrough tests play real skirmishes, so a large change may need the strategy in `playthrough_test.go` adjusted.
- The map: `data/map.yaml`, with one `A` and one `B`, ground under each footprint, and no two footprints sharing a cell.
- A unit type: the controls, the command bar and the opponent train `worker`, `soldier` and `ranger` by name, so `data/rules.yaml` must keep those three (loading it fails otherwise). To add one, add it to `data/rules.yaml`, then a shape in `drawUnit`, a key and a gamepad button in `game.yaml` read in `readActions`, and a button in `commandBar`.
- A plain move, to retreat: a ground command is an attack-move, so units fight on the way. Give `command` a modifier or a second button that calls `moveGroup` with `orderMove` instead, which ignores enemies until the units arrive.
- Art: `draw.go` draws everything by code; swap one kind of thing at a time for sprites from [sprite](sprite.md).
- A map larger than the screen: draw under a `tilemap.Camera` and center it on a control group recalled twice, which `focus` highlights now.
- Sound, a title screen, saves and replays: the world steps deterministically, so [sound](sound.md), [scene](scene.md), [save](save.md) and [replay](replay.md) fit around `skirmish.update` without changing the simulation.
