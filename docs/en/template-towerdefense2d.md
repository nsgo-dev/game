# Tower defense template

The `towerdefense2d` [template](templates.md) is Bramble Gate, a small 2D tower defense game: enemies walk a road from a spawn to a gate, and the player builds towers on the grass beside it, upgrades them with the money the enemies drop and calls each wave when ready. Stopping all five waves wins; letting ten lives' worth of enemies through the gate loses. It plays with a keyboard, a gamepad or touches on desktop, mobile and the web, and all of its art is drawn in code.

```sh
go run github.com/nsgo-dev/game/packages/game/cmd/game@latest new -template towerdefense2d mygame
cd mygame
go tool nsgo dev
```

`nsgo init -template github.com/nsgo-dev/game/template/towerdefense2d -module example.com/mygame mygame` writes the same project.

## What it shows

- The level, its waves and enemies, the towers with their upgrades, and the projectiles are three YAML files that `game.yaml` [autoloads](project.md), checked against each other when the game starts.
- The road is the shortest path over the map's road tiles, found with `ai.AStar` ([ai](ai.md)); the map is a [tilemap](tilemap.md) whose cells are flagged road, buildable or solid.
- Towers and enemies keep their numbers on [stats](stats.md) sheets: an upgrade is a permanent stat effect, and a frost tower's slow is a timed modifier on the enemy's speed.
- Towers fire [bullets](bullets.md) patterns at the enemy furthest along the road, leading it by the projectile's flight time; a mortar's hit splashes, a frost bolt slows.
- A fixed-step [loop](loop.md) runs the world at 60 steps a second, up to three steps an update at the fastest speed, and a [screen](screen.md) viewport fits the 480 by 320 map to any window under a native HUD.
- The [input](input.md) map in `game.yaml` binds every action to keys and gamepad buttons, and the cursor to the arrow keys, the d-pad and the left stick; a tap on the battlefield moves the cursor and a second tap builds.
- Headless `nsgotest` playthroughs play one plan to a win and another to a loss with the keyboard, a gamepad and touches.

## How it is organised

| File | What it holds |
| --- | --- |
| `game.yaml` | The project file: window, virtual resolution, input map, autoloaded data files, export presets. |
| `data/level.yaml` | The level: tile size, starting money and lives, the map, enemy kinds and waves. |
| `data/towers.yaml` | The tower kinds: cost, color, stats, upgrades, the pattern each fires, and the share selling refunds. |
| `data/projectiles.yaml` | The projectiles, a [bullets](bullets.md) pattern file. |
| `data.go` | The level and tower formats, their validation, and `LoadData`. |
| `world.go` | The simulation: the road, spawning, walking, targeting, hits, money, lives and the outcome. |
| `game.go` | The controller: the loop, the input actions, the cursor and the HUD's state. |
| `view.go` | The status bar, the battlefield canvas, the toolbar and the end banner, and the battlefield's drawing. |
| `art.go` | The ground tiles and projectiles, painted in code. |
| `*_test.go` | Format, simulation and playthrough tests. |

`World` has no clock or randomness of its own: the same calls give the same game, so its tests drive it directly and the playthroughs drive the whole app through `nsgotest`.

## The data files

`data/level.yaml` and `data/towers.yaml` are the template's own formats, each with a `version` field; they refuse unknown fields and are written back unchanged, which `TestDataFilesAreCanonical` checks. The level's `map` is one string per row, one character per tile:

| Character | Tile |
| --- | --- |
| `.` | Grass, where towers are built |
| `=` | Road |
| `S` | The spawn, on the road; exactly one |
| `G` | The gate, on the road; exactly one |
| `^` | Rock |
| `~` | Water |

Every row has the same length and road must join the spawn to the gate. Enemies have `health`, `speed` and an optional `armor` stat, a `reward` and the lives a `leak` costs. A wave is groups that spawn side by side, each a `count` of one enemy every `interval` after a `delay`, and a `bonus` paid when the wave is cleared.

A tower needs `damage`, `range` (game points) and `cooldown` (seconds) stats; `splash` damages everything within that radius of a hit, and `slow` with `chill` takes a share of a hit enemy's speed for that many seconds. An upgrade is a `cost` and stat `modifiers` as [stats](stats.md) writes them. A tower's `pattern` names a pattern in `data/projectiles.yaml` that fires one shot.

## Where to change it

- Draw a new map, add enemies or waves, or retune the economy in `data/level.yaml`.
- Add a tower kind or an upgrade in `data/towers.yaml`, with its projectile in `data/projectiles.yaml`; the HUD offers every kind, and the number keys choose the first three.
- Give a tower a new behavior by reading a new stat in `World.fire` or `World.hit` in `world.go`.
- Replace the painted art in `art.go` with `sprite.LoadAtlas` and image files.
- Rebind a control in the `input` section of `game.yaml`.

After a change, `go tool nsgo test ./...` shows whether the files still load and whether `winningPlan` in `plan_test.go` still wins; after changing a data file, `go run github.com/nsgo-dev/game/packages/game/cmd/game index` updates `game.index.yaml`.
