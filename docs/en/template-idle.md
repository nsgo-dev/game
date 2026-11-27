# The idle template

`idle` is Hearthkeeper, a small idle game: a fire to keep alive, generators and upgrades bought with the embers it makes, progress while the game is closed, a prestige reset and a goal that wins. Create a project from it with:

```sh
go run github.com/nsgo-dev/game/cmd/game@latest new -template idle mygame
```

or with NSGo's own command, `nsgo init -template github.com/nsgo-dev/game/template/idle -module example.com/mygame mygame`. [Templates](templates.md) lists the others.

## What it shows

- **Large numbers.** Every amount is an [`idle.Number`](idle.md), so prices and production keep working past float64's range, and the HUD writes them with `idle.Format` in short (1.23M), letter, scientific or engineering notation, which the player switches and the save keeps.
- **Offline progress.** When the game starts or comes back from the background it credits the time since it was last seen with `idle.Offline`: at least a minute, at most eight hours, at half rate. The absence is replayed in one-second steps (`idle.Away.Steps`) through the same rules as online play, so a fire left without fuel goes out while the player is away, and a clock set back credits nothing.
- **Generators, upgrades and prestige.** Generators cost `Cost × Growth^owned` (`idle.Number.Pow`), upgrades multiply one generator, all of them or a stoke, or feed the fire, and scattering the ashes restarts the run for `⌊√(earned / threshold)⌋` ash, each a lasting +10%.
- **Win and loss.** Buying the Everflame wins, and play goes on after it. Letting the fuel gauge empty, online or away, loses the run; rekindling starts a new one and keeps the ash. The fire waits while a card is up, and a card changes with the game, so a win card turns into the loss when the fire went out during an absence.
- **Saves.** [`save.Store`](save.md) keeps a versioned JSON save with a backup copy in the app's storage. Generators and upgrades are saved by id, numbers as text, and a save the game cannot use is refused rather than overwritten.
- **One shop for every input.** The shop is a [`gameui`](gameui.md) tree on the canvas: touch and mouse press its buttons, and the keyboard and controllers move a focus over them with `SpatialNavigator`, through actions [`game.yaml`](project.md) binds. Its 360 by 600 virtual display requests portrait orientation on phones (`cx.RequestOrientation(core.OrientationPortrait)`).

| | Keyboard | Controller | Touch and mouse |
| --- | --- | --- | --- |
| Stoke | Space | West | Tap the hearth |
| Move the focus | Arrow keys, W A S D | D-pad | |
| Press the focused control | Enter | South | Tap the control |
| Close a card | Enter, Escape | South, East | Its button |
| Switch the notation | N | North | The notation button |

## How it is organised

| File | What it holds |
| --- | --- |
| `hearth.yaml` | The balance: fuel, generators, upgrades, prestige, the goal and the offline rules, in a versioned YAML file that refuses unknown fields and is checked when the game starts. |
| `data.go` | Reads, validates and writes `hearth.yaml` deterministically. |
| `sim.go` | The rules, with no view and no clock, so the loop, the offline replay and the tests all run the same code. |
| `save.go` | The save slot and the notation choice. |
| `game.go` | The NSGo side: the fixed-step loop, input actions, autosave, background and return, and the native HUD and cards. |
| `shop.go` | The `gameui` shop and the drawn hearth. |
| `game.yaml`, `project.go` | The project file, with the virtual resolution and the input map. |
| `sim_test.go`, `data_test.go`, `game_test.go`, `project_test.go` | The rules and the balance, the data file with a fuzz test, a touch playthrough to the win, losses with the mouse, the keyboard and the controller, every input playing the game, and the project file. |

## Where to change it

- **Numbers.** Edit `hearth.yaml`; a new generator or upgrade shows in the shop with no code. `TestBalanceReachesTheGoal` plays the game with a simple strategy and fails when the goal comes too early or too late, so it reports what a change did to the pace.
- **A new kind of upgrade.** Add an effect constant and its rule to `data.go`, and apply it in `sim.go`.
- **Saved state.** Add a field to `savedGame`, raise `saveVersion` and register a `save.WithMigration` for older saves.
- **Offline rules.** `offline` in `hearth.yaml` sets the minimum, the cap, the rate and the replay step.
- **Look.** `drawFlame` in `shop.go` draws the hearth, and `newShop` sets the `gameui` theme.
