# Hollowbrook, a top-down RPG template

A small, complete top-down RPG built with [NSGo](https://github.com/nsgo-dev/nsgo) and [`github.com/nsgo-dev/game`](https://github.com/nsgo-dev/game): the town of Hollowbrook and the meadow beyond its gate, three people to talk to, a pack of items and equipment, one quest, turn-based battles and three save slots.

The shrine lamp of Hollowbrook has gone out. Mira the healer needs three glowcaps from Whisper Meadow to light it again, and Thornhide, an old bramble boar, keeps the gatherers away. The starting stick will not beat Thornhide: earn the coins for Bram's iron sword from the moss slimes, find the leather vest in the meadow, drive Thornhide off and bring the glowcaps back to win. Fall in battle and you can load a save or start again.

## Running and testing

```sh
go tool nsgo dev                      # this machine, rebuilt on save
go tool nsgo dev -target web          # the browser
go tool nsgo dev -target ios_simulator
go tool nsgo dev -target android
go tool nsgo test ./...               # the rules, the data files and full playthroughs
go tool game playtest                 # automated bot playtest and report
go tool nsgo check -target macos,windows,linux,android,web
```

The tests play the game through to the victory screen with the keyboard and to the defeat screen with a gamepad and with the on-screen touch controls, so a change that breaks the quest, the balance or a control fails them.

## Controls

Keyboard and gamepad controls live in `game.yaml`'s input map, and on-screen touch controls live in `controls.touch.yaml` (`input.TouchControls`).

| Action | Keyboard | Gamepad | Touch |
| --- | --- | --- | --- |
| Walk, move through menus | Arrows or WASD | D-pad or left stick | The floating stick at the bottom left |
| Talk, go on, pick | Enter, Space or E | South (A) | The A button, or a tap on the conversation |
| Back, open the pack | Escape or Backspace | East (B) | The B button |
| Open and close the pack | Tab | Start | The Pack button |

The on-screen controls appear from the start on phones and tablets, stay hidden on desktop until the screen is touched, and hide again when a key or controller is used. Menus are native buttons, so a tap or a click picks an entry too.

## How it is organised

| File | What it holds |
| --- | --- |
| `game.yaml` | The project file: name, window, the 480×270 virtual resolution, the input map and the export presets. |
| `controls.touch.yaml` | The on-screen touch layout (a floating stick on `moveX`/`moveY` and A and B buttons) read by `input.TouchControls`. |
| `data/world.yaml` | The maps, drawn as rows of characters, with their exits, people, foes, patrols and items, and the foes' stats. Its format is defined in `world.go`. |
| `data/items.yaml` | The items, read by `inventory`: coins, potions, glowcaps and equipment. |
| `data/stats.yaml` | The player's stats and each piece of equipment's bonuses, read by `stats`. |
| `data/quests.yaml` | The quest and its objectives and rewards, read by `quest`. |
| `data/cast.yaml`, `data/dialogue.yarn` | Who speaks and what they say: a `vn` cast and a Yarn Spinner script with branching options and commands that give items, start the quest and heal. |
| `data.go` | Loads every data file and checks that they agree with each other. |
| `game.go` | The rules: walking, patrols, conversations, the quest, equipment and battles. |
| `menu.go`, `save.go` | The title, pack, battle, defeat and victory menus, and the save slots. |
| `input.go` | Turns the input map's actions into what the player asked for in one step. |
| `art.go`, `draw.go` | The pixel art, written as text and painted at start-up, and the drawing of each screen. |
| `ui.go` | The app: the fixed-step loop, the scenes and the native HUD and menus. |
| `playtest.yaml`, `playtest_test.go` | The `topdown-explorer` playtest configuration and `TestPlaytest` adapter driven by `go tool game playtest`. |
| `*_test.go` | Format and rules tests, and the playthroughs. |

## Where to change it

- A new map: add it to `maps` in `data/world.yaml` with an exit to it from another map. A new kind of tile or foe goes in `tiles` or `foes`, with its picture in `art.go`.
- A new person: add them to a map's `people`, to `data/cast.yaml`, and a node to `data/dialogue.yarn`.
- A new item or piece of equipment: add it to `data/items.yaml`; equipment also needs an effect of the same name in `data/stats.yaml`.
- A new quest: add it to `data/quests.yaml` and start it from a conversation with `<<start id>>`. `mainQuest` in `data.go` names the quest that wins the game.
- Balance: the foes' stats are in `data/world.yaml` and the player's in `data/stats.yaml`; `TestThornhideNeedsTheIronSword` checks the fight the game is built around.
- Real art: replace `newAtlas` in `art.go` with `sprite.LoadAtlas`, keeping the frame names.

The game's settings live in `game.yaml`. Edit its name and identifier before shipping, then write them into `nsgo.yaml` and check the project:

```sh
go run github.com/nsgo-dev/game/cmd/game project sync
go run github.com/nsgo-dev/game/cmd/game project check
```

## Git setup

Each clone enables the object-by-object scene merge driver once (`.gitattributes` names it, while Git keeps driver config local to `.git/config`):

```sh
go tool game merge -setup
```
