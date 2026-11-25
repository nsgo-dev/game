# The rpg2d template

`rpg2d` is a small top-down RPG called Hollowbrook: a town and the meadow beyond its gate, three people to talk to, a pack of items and equipment, one quest, turn-based battles and three save slots. It is one of the [templates](templates.md), and the starting point for a game of that kind.

```sh
go run github.com/nsgo-dev/game/packages/game/cmd/game@latest new -template rpg2d mygame
cd mygame
go tool nsgo dev
```

`nsgo init -template github.com/nsgo-dev/game/template/rpg2d -module example.com/mygame mygame` writes the same project.

## What it shows

The shrine lamp of Hollowbrook has gone out. Mira the healer needs three glowcaps from Whisper Meadow to light it again, and Thornhide, an old bramble boar, keeps the gatherers away. The starting stick cannot beat Thornhide: the player earns the coins for Bram's iron sword from the moss slimes, finds a leather vest in the meadow, equips both, drives Thornhide off and brings the glowcaps back to win. Losing every HP in a battle ends the game, with a save to load or a new game to start.

| Part of the genre | How the template does it | Package |
| --- | --- | --- |
| Maps | Two maps drawn as rows of characters in `data/world.yaml`, built into tile maps with solid tiles flagged for collision and a camera that follows the player | [tilemap](tilemap.md) |
| Walking | Tile-by-tile movement on a fixed-step loop, exits between maps, items picked up by walking onto them and foes that patrol | [loop](loop.md) |
| Dialogue | A Yarn Spinner script with branching options, conditions on what the player has, and commands that give and take items, start the quest and heal, played with portraits and a typewriter | [vn](vn.md), [flow](flow.md) |
| Items and equipment | A bag of slots, and weapon, armor and charm slots whose bonuses come from named effects | [inventory](inventory.md), [stats](stats.md) |
| Quest | One quest with objectives to gather, defeat and report, and rewards granted when it completes | [quest](quest.md) |
| Battles | Turn-based fights with Attack, Potion and Flee, where a boss cannot be fled from | |
| Saves | Three slots in the app's storage holding the bag, equipment, quest, dialogue state and the map | [save](save.md) |
| Screens | Title, map, battle, defeat and victory scenes with fades between them | [scene](scene.md) |

Every control is bound in `game.yaml`'s input map: the arrows, WASD, Enter, Escape and Tab on a keyboard; the D-pad, left stick, South, East and Start on a gamepad; and an on-screen pad with A and B buttons, drawn on phones and tablets, for touch and the mouse. Menus are native buttons, so a tap or a click picks an entry, and the keyboard and gamepad move an outline through them.

The tests play the game through the app: to the victory screen with the keyboard, and to the defeat screen with a gamepad and with the touch pad, saving and loading on the way.

## How it is organised

| File | What it holds |
| --- | --- |
| `game.yaml` | The project file: the window, the 480×270 virtual resolution, the input map and the export presets |
| `data/world.yaml` | The tiles, the foes' stats, where a new game starts, and each map's rows, exits, people, foes with their patrols, and items. The format is defined in `world.go`: it has a version, refuses unknown fields and is written back unchanged |
| `data/items.yaml`, `data/stats.yaml`, `data/quests.yaml` | The item catalog, the player's stats and the equipment's effects, and the quest book |
| `data/cast.yaml`, `data/dialogue.yarn` | The people's names, colors and portraits, and what they say |
| `data.go` | Loads the data files and checks that they agree: that every item, foe, person, dialogue node, effect and picture they name exists |
| `game.go`, `menu.go`, `save.go` | The rules, the menus and the save slots, with no drawing or input devices, so tests drive them directly |
| `input.go`, `draw.go`, `art.go`, `ui.go` | The input map read into one step's intent, the drawing of each screen, the pixel art painted at start-up, and the app with its HUD |

A data file that does not load shows its error in the window instead of the game, so it can be fixed under `go tool nsgo dev`.

## Where to change it

- **A map**: add one to `maps` in `data/world.yaml`, with an exit to it from another map. A new kind of tile or foe goes in `tiles` or `foes`, and its picture in `art.go`.
- **A person**: add them to a map's `people`, to `data/cast.yaml` with their portraits, and give them a node in `data/dialogue.yarn`.
- **An item**: add it to `data/items.yaml`. A piece of equipment also needs an effect of the same name in `data/stats.yaml`.
- **A quest**: add it to `data/quests.yaml` and start it from a conversation with `<<start id>>`. `mainQuest` in `data.go` is the quest whose completion wins the game.
- **Balance**: the foes' stats are in `data/world.yaml`, the player's in `data/stats.yaml`. `TestThornhideNeedsTheIronSword` checks the fight the game is built around.
- **Controls**: edit the input map in `game.yaml`; the touch pad is drawn where its regions are.
- **Art**: replace `newAtlas` in `art.go` with `sprite.LoadAtlas`, keeping the frame names the data files use.
