# The fps3d template

`fps3d` is a first-person shooter arena, Relay Yard: the player holds a walled yard against three waves of drones and wardens with a hitscan rifle and a projectile launcher, picking up health and ammo between fights. Clearing the last wave wins; reaching 0 health loses. It is the template to start from for a 3D shooter, and a working example of the 3D packages together: `physics3d`, `render3d`, `ai` and `input`.

```sh
go run github.com/nsgo-dev/game/packages/game/cmd/game@latest new -template fps3d mygame
cd mygame
go tool nsgo dev
```

## What it shows

- Movement: the player and every enemy are `physics3d.CharacterController` capsules in one `physics3d.World`, so they slide along walls, step and jump. Walls and crates are static boxes, and enemies are kinematic bodies the player bumps into and shots hit.
- Shooting: the rifle is hitscan, a `RaycastFirstLayer` through the wall and enemy layers the moment it fires; the launcher fires a round that flies a segment each step and bursts with splash damage. Magazines, reserves, reloads and fire rate come from the arena file, and an empty magazine reloads by itself.
- Enemies: each one runs an `ai` behavior tree, a selector of guarded branches that attacks the player in sight, hunts the place the player was last seen or heard (a shot alerts every enemy within earshot), and otherwise patrols the arena's route. It walks A* paths over an `ai.BoolGrid` of the layout, smoothed with `ai.SmoothGridPath`, and fires bolts the player can dodge. An enemy sees along a line that clears every wall edge by a margin, so it never shoots at the player through a corner the player cannot see back around.
- Waves: a countdown, then each wave's enemies enter at the spawns one at a time, with a pause between waves.
- Rendering: `render3d`'s lit PBR pipeline draws the level as three baked meshes (floor, walls and crates) and every moving thing, the enemies, rounds, tracers, pickups and the weapon, as one unit cube scaled and placed per draw, under a `render3d.LightRig` of a sky fill, a key light and glows over the spawns and pickups, inside a `gpu.View`. The weapon is drawn in view space and kicks back on each shot. A native HUD and an overlay `core.Canvas` with the crosshair, the hit marker, the hurt flash and the touch controls sit on top.
- Input: one `input.Actions` hears all three devices. `game.yaml`'s input map binds the keys, the mouse button and the controller's buttons and sticks; mouselook reads locked pointer motion (`BindPointerAxis` and `LockPointer`, requested on each press of the arena); an `input.VirtualPad` adds a floating move stick, a floating look stick and fire, jump, reload and swap buttons on touch screens.

| | Keyboard and mouse | Game controller | Touch |
| --- | --- | --- | --- |
| Move | W A S D | Left stick | Left stick |
| Look | Mouse with the pointer locked, or the arrow keys | Right stick | Look stick (drag in the lower middle) |
| Fire | Left button or F | Right trigger | Fire |
| Jump | Space | South | Jump |
| Reload | R | West | Reload |
| Next weapon | Q, or 1 and 2 | North | Swap |
| Pause, play again | P or Enter | Start | Play again |

## How it is organised

| File | What it holds |
| --- | --- |
| `game.yaml` | The project file: name, id, window, export presets and the input map. |
| `arena.yaml` | The level and every gameplay number: the layout drawn in characters, the player, the weapons, the enemy kinds, the pickups, the waves, the countdown and the intermission. |
| `arena.go` | The arena file's schema: each field documented, a version field, unknown fields refused, validation (a walled border, one start, a patrol route without gaps, every open cell reachable) and an encoder whose output reads back unchanged. |
| `match.go` | One match: the world, the player's movement and weapons, rounds in flight, pickups and waves. It takes a `controls` value each step and never reads a device, so it is deterministic and tests drive it directly. |
| `enemy.go` | An enemy's body, behavior tree, sight and path following. |
| `controls.go` | The actions, the touch pad's layout and how a step's input becomes `controls`. |
| `render.go` | The renderer: the baked level meshes, the unit cube, the materials, the lights and the per-frame draws. |
| `game.go` | The root component, the 60 Hz `game.Loop`, pause and play again, the HUD, the overlay and the end panel. |
| `*_test.go` | The arena file's round trip, refusals and fuzz test; the rules of the match; a bot that aims, fires, strafes and heals; playthroughs that win with the keyboard and mouse and with a controller and lose by standing still; the touch controls; and a frame rendered on the headless GPU. |

The arena file is the place to start reading: everything `match.go` does is tuned by it, and the layout is readable as a map.

```yaml
layout:
  - '#################'
  - '#S......#......S#'
  - '#.##.........##.#'
  - '#.#..1..c..2..#.#'
  - '#.....#####.....#'
  - '#..c....H....c..#'
  - '#...#...P...#...#'
```

`#` is a wall, `c` a crate low enough to shoot over, `P` the start, `S` an enemy spawn, `1` to `9` the patrol route in order, and each pickup's `symbol` (`H` and `A` here) places that pickup.

## Where to change it

- A level: edit `layout`. `ParseArena` refuses a layout it cannot run, with the row and column of the problem.
- Balance: weapon damage, fire interval, magazine and reserve; enemy health, speed, sight range, field of view, attack range and bolts; the size and order of the waves. All of it is in `arena.yaml`, and the playthrough tests show at once whether the game can still be won and lost.
- Weapons: add one to `weapons` as `hitscan` or `projectile`. Slots 1 and 2 are bound in `game.yaml`; bind `weapon3` and read it in `readControls` for a third.
- Enemy behavior: add a guarded branch to the selector in `newEnemy`, such as retreating to cover when hurt with `ai`'s tactical queries, ahead of the patrol.
- Controls: rebind actions in `game.yaml`'s input map, or move the touch controls in `layoutTouchPad`.
- Looks: change the materials and lights at the top of `render.go`, or draw meshes loaded with `gltf` in place of the cube.

The template's tests run with `go tool nsgo test ./...` and need no GPU except for the one rendered frame, which uses the headless GPU `nsgotest` provides.
