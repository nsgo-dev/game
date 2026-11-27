# Relay Yard: an NSGo first-person shooter template

A first-person shooter arena built with [NSGo](https://github.com/nsgo-dev/nsgo) and [`github.com/nsgo-dev/game`](https://github.com/nsgo-dev/game): hold the relay yard against three waves of drones and wardens with a hitscan rifle and a projectile launcher.

It shows:

- A capsule `physics3d.CharacterController` for the player and for every enemy, walls and crates as static boxes, and hitscan shots, rounds and enemy sight as layered raycasts through one `physics3d.World`.
- Enemies driven by an `ai` behavior tree (attack the player in sight, else hunt the last place it was seen or heard, else patrol), walking A* paths over a `pathfind.BoolGrid` smoothed with `pathfind.SmoothGridPath`.
- A `render3d` lit PBR pipeline drawing the level as three baked meshes (floor, walls and crates) and every moving thing as one unit cube, under a `render3d.LightRig`, in a `gpu.View`, with a native HUD and an overlay `core.Canvas` above it.
- One `input.Actions` fed by the keyboard, mouselook under pointer lock, game controllers and an `input.TouchControls` layer built from `controls.touch.yaml` (safe-area anchored sticks and fire, jump, reload, swap and weapon-slot buttons).
- Headless `nsgotest` playthroughs that win with the keyboard and mouse and with a game controller, and lose by standing still.

## Play

| | Keyboard and mouse | Game controller | Touch |
| --- | --- | --- | --- |
| Move | W A S D | Left stick | Left stick (drag in the lower left) |
| Look | Mouse (click the arena to lock the pointer), or the arrow keys | Right stick | Right stick (drag in the lower middle) |
| Fire | Left button or F | Right trigger | Fire |
| Jump | Space | South (A) | Jump |
| Reload | R | West (X) | Reload |
| Next weapon | Q, or 1 and 2 | North (Y) | Swap, W1 and W2 |
| Pause, play again | P or Enter | Start | Play again |

Clear all three waves to win; reaching 0 health loses. Health and ammo pickups beside the start return 20 seconds after they are taken.

## Run and test

```sh
go tool nsgo dev                    # the desktop, with hot reload
go tool nsgo dev -target web        # the browser
go tool nsgo dev -target ios_simulator
go tool nsgo dev -target android
go tool nsgo test ./...             # headless tests and playthroughs
go tool nsgo check -target macos,windows,linux,android,web
```

The game's settings live in `game.yaml`, its project file. Edit the name and identifier there before shipping it, then write them into `nsgo.yaml` and check the project:

```sh
go run github.com/nsgo-dev/game/cmd/game project sync
go run github.com/nsgo-dev/game/cmd/game project check
```

## Layout

- `game.yaml`: the project file: name, id, version, window, export presets and the input map that binds every key and controller button to an action.
- `controls.touch.yaml`: the on-screen touch layout (`input.TouchControls`), placed inside the safe area.
- `arena.yaml`: the level and its tuning: the layout drawn in characters, the player, the weapons, the enemy kinds, the pickups and the waves. `arena.go` documents every field and validates the file.
- `match.go`: one match from the countdown to a win or a loss: the physics world, the player's movement and weapons, rounds in flight, pickups and waves. It reads `controls`, never a device, so tests drive it directly.
- `enemy.go`: an enemy's body, its behavior tree and its path following.
- `controls.go`: the actions and how a step's input becomes `controls`.
- `render.go`: the `render3d` renderer.
- `game.go`: the root component, the 60 Hz `game.Loop`, the HUD, the overlay and the end panel.
- `project.go`, `main.go`: the embedded project and arena files and the entry point.
- `*_test.go`: the arena file's round trip and validation, the match's rules, the bot that plays the playthroughs, the playthroughs on each device and a rendered frame.

## Where to change it

- A new level: edit the `layout` rows in `arena.yaml`. `#` is a wall, `c` a crate you can shoot over, `P` the start, `S` an enemy spawn, `1` to `9` the patrol route, and each pickup's `symbol` places that pickup.
- Balance: weapon damage, fire rate and magazines, enemy health, speed, sight and bolts, and the waves are all in `arena.yaml`.
- A new weapon: add it to `weapons` as `hitscan` or `projectile`; `1` to `9` draw them by slot once `game.yaml` binds `weapon3` and up and `controls.go` reads them.
- A new enemy behavior: add a branch to the selector in `newEnemy` (`enemy.go`), such as a flee when hurt, ahead of the patrol.
- Controls: rebind actions in `game.yaml`'s input map; move or resize the touch controls in `controls.touch.yaml`.
- Looks: materials and lights are at the top of `render.go`; replace `unitCube` with meshes from `gltf` for real models.

## Git setup

Each clone enables the object-by-object scene merge driver once (`.gitattributes` names it, while Git keeps driver config local to `.git/config`):

```sh
go tool game merge -setup
```
