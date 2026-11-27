# Hearthkeeper: an idle game template

An idle game built with [NSGo](https://github.com/nsgo-dev/nsgo) and [`github.com/nsgo-dev/game`](https://github.com/nsgo-dev/game). You keep a fire alive: stoking it gathers embers, generators make embers every second, upgrades multiply them and automate the fuel, the hearth keeps earning while the game is closed, scattering the ashes restarts the run for a lasting bonus, and lighting the Everflame wins. A fire left without fuel goes out, which loses the run.

It shows:

- `idle.Number` for every amount, so costs and production run past float64 without changing the code, and `idle.Format` for writing them in short (1.23M), letter, scientific and engineering notation.
- `idle.Offline` for progress while the game is closed or in the background, capped and at half rate, replayed in fixed steps so the fire can run dry while you are away just as it can while you watch.
- `save.Store` for a versioned JSON save in the app's storage, with a backup copy and a check that refuses a save the game cannot use.
- `gameui` for the shop drawn on the canvas, with spatial focus navigation for the keyboard and controllers, and touch and mouse presses on the same widgets.
- `game.yaml`'s input map binding keys and controller buttons to actions, and a fixed-step `game.Loop`.
- Headless `nsgotest` tests that play the game to a win and to a loss with touch, mouse, keyboard and a controller.

The game's settings live in `game.yaml`, its project file. Edit the name and identifier there before shipping it, then write them into `nsgo.yaml` and check the project:

```sh
go run github.com/nsgo-dev/game/cmd/gameproject sync
go run github.com/nsgo-dev/game/cmd/gameproject check
```

## Playing

| | Keyboard | Controller | Touch and mouse |
| --- | --- | --- | --- |
| Stoke the fire | Space | West (X on Xbox, □ on PlayStation) | Tap the hearth |
| Move the focus | Arrow keys or W, A, S, D | D-pad | |
| Press the focused control | Enter | South (A, ✕) | Tap the control |
| Close a card | Enter or Escape | South or East | Its button |
| Switch the notation | N | North (Y, △) | The button at the top right |

Buy the Bellows and the Coal Chute early: until the fire feeds itself, it burns out within a hundred seconds unless you stoke it, and leaving it unfed while away loses the run too.

## Running and testing

Run with hot reload on your desktop:

```sh
go tool nsgo dev
```

Run on other targets:

```sh
go tool nsgo dev -target web
go tool nsgo dev -target ios_simulator
go tool nsgo dev -target android
```

Run the headless tests, and vet across platforms:

```sh
go tool nsgo test ./...
go tool nsgo check -target macos,windows,linux,android,web
```

Build release artifacts into `dist/`:

```sh
go tool nsgo build -target macos_arm64,windows_x64,linux_x64,web
```

## Layout

- `hearth.yaml`: the balance. Fuel, generators, upgrades, prestige, the goal and the offline rules are all here, versioned and checked when the game starts; change the numbers here rather than in code.
- `data.go`: reads, checks and writes `hearth.yaml`.
- `sim.go`: the rules, with no view and no clock: stoking, buying, production, fuel, prestige, winning and the offline replay. Everything else calls into it.
- `save.go`: the save slot, by generator and upgrade id so reordering `hearth.yaml` keeps saves.
- `game.go`: the NSGo side: the loop, the input actions, saving, crediting time away and the native HUD and cards.
- `shop.go`: the `gameui` shop on the canvas and the drawn hearth.
- `game.yaml`, `project.go`: the project file, with the virtual resolution and the input map, and `Project`.
- `main.go`: the entry point.
- `*_test.go`: the rules and the balance (`sim_test.go`), the data file (`data_test.go`), playthroughs with every input (`game_test.go`) and the project file (`project_test.go`).

## Where to change it

- New generator or upgrade: add it to `hearth.yaml`; the shop shows it. A new kind of upgrade effect is a constant in `data.go`, a case in `validate`, and a line where `sim.go` applies it.
- Pace: `TestBalanceReachesTheGoal` plays the game with a simple strategy and fails when the goal comes too early or too late, so it tells you what a change to `hearth.yaml` did.
- New saved state: add a field to `savedGame`, raise `saveVersion` and register a migration with `save.WithMigration`.
- Art: replace `drawFlame` in `shop.go`, or the `gameui` theme in `newShop`.

## Git setup

Each clone enables the object-by-object scene merge driver once (`.gitattributes` names it, while Git keeps driver config local to `.git/config`):

```sh
go tool game merge -setup
```
