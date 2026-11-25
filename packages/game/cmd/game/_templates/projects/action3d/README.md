# Action Adventure

A third-person 3D action adventure built with [NSGo](https://github.com/nsgo-dev/nsgo) and [`github.com/nsgo-dev/game/packages/game`](https://github.com/nsgo-dev/game). A knight crosses a walled courtyard, fights the guards, jumps onto a ledge for the key, and the key opens the gate to the shrine. Reaching the shrine wins; losing all health loses.

It shows:

- An orbit camera (`scene3d.OrbitController`) that follows the hero, turns with the player and moves in front of walls between it and the hero.
- A capsule character controller per actor in a `physics3d.World` of static boxes, with walking, jumping onto a ledge and a door that leaves the world when it opens.
- Run, jump, attack, flinch and fall animations on a rig of boxes, played by an `anim3d.StateMachine` whose locomotion state blends idle into run by speed (`anim3d.BlendSpace1D`), with conditional transitions and a `hit` clip event that lands each blow.
- Enemies that chase the hero inside their sight and leash, wind up a slow swing the hero can interrupt, and walk home when the hero gets away.
- A level that is a scene file: blocks, the spawn, enemies, the key, the door, the goal and the lamps are objects with components, and a connection in the scene, not Go code, makes picking up the key unlock the door.
- render3d's lit PBR pipeline drawing every box from one cube mesh, lit by a moon, an ambient fill and the scene's lamps.
- Keyboard and mouse, game controller and touch, all through one `input.Actions` bound from `game.yaml`, with on-screen sticks and buttons (`input.VirtualPad`) that appear once the screen is touched.
- Headless tests that play the whole run to a win with a game controller and to a loss with the keyboard, drive the touch controls and draw the level on a software GPU.

## Controls

| | Keyboard and mouse | Game controller | Touch |
| --- | --- | --- | --- |
| Move | W A S D or the arrow keys | Left stick or D-pad | Drag on the left half |
| Turn the camera | Q and E, or drag with the mouse | Right stick or the shoulder buttons | Drag on the right half |
| Jump | Space | A (south) | Jump button |
| Attack | J or Enter | X (west) or the right trigger | Attack button |
| Play again | R | Start | Play again button |

## Running and testing

```sh
go tool nsgo dev                 # the desktop, with hot reload
go tool nsgo dev -target web     # the browser
go tool nsgo test ./...          # the headless tests
go tool nsgo check -target macos,windows,linux,android,web
```

## Layout

- `game.yaml` — the project file: name, id, window, the input map (actions, axes and game controller sticks), the scene and data files to autoload, and export presets.
- `scenes/keep.scene.yaml` — the level, a scene file the scene editor opens (`go run github.com/nsgo-dev/game/packages/game/cmd/game editor .`). Each object's `x`, `y` (up) and `posZ` place it in meters, its scale sizes a box, and its components say what it is: `block`, `spawn`, `enemy`, `key`, `door`, `goal` or `lamp`. The `connections` at the end wire the key's `collected` event to the door's `unlock` handler.
- `data/actors.yaml` — the hero's and the enemies' tuning (health, speed, jump, reach, damage, swing speed, knockback and colors), the rig of boxes both share, and the animation clips in degrees and seconds. `actors.go` defines and checks its format.
- `level.go` — the level's component types and how a scene becomes boxes, spawns and lamps.
- `game.go` — the simulation: the physics world, hero and enemy rules, the key, the door and winning and losing. It knows nothing of devices or the GPU.
- `rig.go` — turns the rig and clips into `anim3d` clips and a state machine per actor, and poses the boxes.
- `render.go` — the camera, the lights and the GPU renderer.
- `view.go` — the root component: the fixed-step `game.Loop`, input, the HUD and the touch controls.
- `project.go`, `main.go` — embed the project and its files and start the app.
- `*_test.go` — the playthroughs, input, rendering and file-format tests.

## Where to change it

- Move walls, add enemies or lamps, or build a new level in `scenes/keep.scene.yaml`; the tests check that it still loads, and the bot's route in `game_test.go` says how a run is won.
- Tune the fight in `data/actors.yaml`: slower enemy swings (`attackSpeed`), more health or a longer `reach` change the difficulty without touching code.
- Add an animation by adding a clip to `data/actors.yaml` and a state to `newAnimator` in `rig.go`.
- Add a component type in `levelComponents` (`level.go`), such as a moving platform, and read it in `buildLevel`.
- Replace the boxes with glTF models: load them with package `gltf`, upload their meshes with `render3d.UploadMesh` and draw them in `renderer.draw`.

After editing `game.yaml`, write the name and id it shares with `nsgo.yaml` and check the project:

```sh
go run github.com/nsgo-dev/game/packages/game/cmd/gameproject sync
go run github.com/nsgo-dev/game/packages/game/cmd/gameproject check
```

After moving or adding files, `go run github.com/nsgo-dev/game/packages/game/cmd/game index` keeps `game.index.yaml`, the project's index of stable file IDs, up to date.
