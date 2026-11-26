# Roguelike Game

A turn-based 2D roguelike built with [NSGo](https://github.com/nsgo-dev/nsgo) and [`github.com/nsgo-dev/game/packages/game`](https://github.com/nsgo-dev/game): a delver descends the five floors of the Glimmerdeep, each generated from the run's seed, fights what lives there, gathers gear, draughts, tonics and shards, and lifts the Heart Ember from the last floor, where the Vault Warden stands on it. Death is permanent: the run's save is deleted the moment it ends, and a summary and the records of every run are kept.

It demonstrates:

- Floors from [`procgen`](https://github.com/nsgo-dev/game/blob/main/docs/en/procgen.md): BSP rooms and corridors, and cellular-automaton caves, with the exit as far from the start as the floor allows.
- Turn-based movement and bump-to-attack combat, monsters that notice the player within their sense and hunt down a distance map, and line of sight with explored cells remembered.
- Monsters and items as entities in each floor's [`entities`](https://github.com/nsgo-dev/game/blob/main/docs/en/entities.md) world, with the monsters' turn played by a notice system and a hunt system on an `entities.Scheduler`.
- Weighted floor loot and monster drops with pity counters from [`loot`](https://github.com/nsgo-dev/game/blob/main/docs/en/loot.md), player stats with equipment and timed tonic modifiers from [`stats`](https://github.com/nsgo-dev/game/blob/main/docs/en/stats.md), and a quaff and a cleave from [`ability`](https://github.com/nsgo-dev/game/blob/main/docs/en/ability.md), whose cooldowns count turns.
- Permadeath with [`save`](https://github.com/nsgo-dev/game/blob/main/docs/en/save.md): the run is saved as the player arrives on each floor and after every turn, so it resumes exactly where it stood, and deleted when it ends, and the records slot counts every run.
- One input map in `game.yaml` for the keyboard, game controllers, touch and the mouse, with on-screen controls on phones and tablets.
- Headless tests that play the game to a win with the keyboard, a controller and touch, and to a death.

The game's settings live in `game.yaml`, its project file. Edit the name and identifier there before shipping it, then write them into `nsgo.yaml` and check the project:

```sh
go run github.com/nsgo-dev/game/packages/game/cmd/gameproject sync
go run github.com/nsgo-dev/game/packages/game/cmd/gameproject check
```

## Controls

| | Keyboard | Controller | Touch |
| --- | --- | --- | --- |
| Move, or attack what is in the way | Arrows, WASD | Left stick, d-pad | The arrows at the bottom left |
| Wait a turn | Space, `.` | South button | The dot between the arrows |
| Quaff a draught | Q | West button | `!` at the bottom right |
| Cleave everything next to you | E | North button | `C` at the bottom right |
| Play again after the run ends | R, Enter | Start | The Play again button |

Holding a direction keeps walking. Holding the left mouse button over an on-screen control plays like touching it.

## Running and Testing

Run with hot reload on your host desktop (macOS, Windows, or Linux):

```sh
go tool nsgo dev
```

Run on other targets:

```sh
go tool nsgo dev -target web
go tool nsgo dev -target ios_simulator
go tool nsgo dev -target android
```

Run the headless tests:

```sh
go tool nsgo test ./...
```

Vet and check across platforms:

```sh
go tool nsgo check -target macos,windows,linux,android,web
```

## Layout

- `game.yaml`: the project file, with the window, the 320 by 180 virtual screen, the input map and the export presets.
- `data/dungeon.yaml`: the player's progression, the floors, the monsters and the items, in the format `dungeon.go` describes.
- `data/stats.yaml`, `data/loot.yaml`, `data/abilities.yaml`: the player's stats and item effects, the drop tables, and the abilities, in the formats of `stats`, `loot` and `ability`.
- `data.go`, `dungeon.go`: loading the data files and checking the names they use from each other.
- `floor.go`: generating a floor, the distance map and line of sight.
- `actors.go`: the components of a floor's monsters and items, and the systems that play the monsters' turn.
- `run.go`: one run's rules, one turn at a time, with no input or drawing.
- `records.go`: the run and records save slots and the run summary.
- `game.go`, `draw.go`: the root component, input, the HUD, the end-of-run panel and drawing.
- `*_test.go`: the playthroughs, the rules, floor generation over many seeds, and the data files.
