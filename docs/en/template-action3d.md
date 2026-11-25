# Action Adventure Template

`action3d` is a third-person 3D action adventure: a knight crosses a walled courtyard, fights the guards, jumps onto a ledge for the key, and the key opens the gate to the shrine. Reaching the shrine wins the run; losing all health loses it. Create a project from it with:

```sh
go run github.com/nsgo-dev/game/packages/game/cmd/game@latest new -template action3d mygame
cd mygame
go tool nsgo dev
```

or, without the `game` tool, `nsgo init -template github.com/nsgo-dev/game/template/action3d -module example.com/mygame mygame` ([Templates](templates.md)).

## What it shows

- **Orbit camera.** A [`scene3d.OrbitController`](scene3d.md) follows the hero at a fixed height and distance, turns when the player turns it, and moves in front of any wall between it and the hero, found with a [physics3d](physics3d.md) raycast.
- **Character movement.** Each actor is a `physics3d.CharacterController` capsule in a world of static boxes: running relative to the camera, jumping onto a ledge too high to step up, and a door whose body leaves the world when it opens.
- **Animation blended by a state machine.** Run, jump, attack, flinch and fall clips play on a rig of boxes through an [`anim3d.StateMachine`](anim3d.md). Its locomotion state blends idle into run by the actor's speed with an `anim3d.BlendSpace1D`, conditional transitions start the jump when the actor leaves the ground and return to locomotion when a swing ends, and each blow lands on the attack clip's `hit` event, so the animation decides when a hit happens.
- **Melee enemies.** Enemies chase the hero inside their sight and leash, wind up a slow swing that a hit from the hero interrupts, and walk home when the hero gets away.
- **A level that is a scene file.** Blocks, the spawn, the enemies, the key, the door, the goal and the lamps are objects with components in a [scene file](editor.md), and a connection in that file, not Go code, makes picking up the key unlock the door.
- **Rendering.** [render3d](render3d.md)'s lit PBR pipeline draws every box from one cube mesh, lit by a moon, an ambient fill and the scene's lamps, on every GPU backend NSGo has, WebGL 2 included.
- **Every kind of input.** Keyboard and mouse, game controllers and touch all go through one [`input.Actions`](input.md) bound from the input map in `game.yaml`; on-screen sticks and buttons (`input.VirtualPad`) appear once the screen is touched.
- **Tests that play it.** Headless nsgotest tests play the whole run to a win with a game controller and to a loss with the keyboard, drive the touch controls and the mouse, check every data file is valid and canonical, and draw the level on a software GPU.

## Controls

| | Keyboard and mouse | Game controller | Touch |
| --- | --- | --- | --- |
| Move | W A S D or the arrow keys | Left stick or D-pad | Drag on the left half |
| Turn the camera | Q and E, or drag with the mouse | Right stick or the shoulder buttons | Drag on the right half |
| Jump | Space | A (south) | Jump button |
| Attack | J or Enter | X (west) or the right trigger | Attack button |
| Play again | R | Start | Play again button |

## How it is organised

| File | What it holds |
| --- | --- |
| `game.yaml` | The [project file](project.md): name, id, window, the input map, the scene and data files it autoloads, and export presets. |
| `scenes/keep.scene.yaml` | The level. Each object's `x`, `y` (up) and `posZ` place it in meters, its scale sizes a box, and its components say what it is: `block`, `spawn`, `enemy`, `key`, `door`, `goal` or `lamp`. The `connections` wire the key's `collected` event to the door's `unlock` handler. |
| `data/actors.yaml` | The hero's and the enemies' tuning, the rig of boxes both share, and the animation clips, in meters, degrees and seconds. |
| `actors.go` | The format of `data/actors.yaml`: version 1, unknown fields refused, and an encoder whose output reads back to the same bytes. |
| `level.go` | The level's component types and how the scene becomes boxes, spawns and lamps. |
| `game.go` | The simulation: the physics world, the hero's and the enemies' rules, the key, the door, winning and losing. It knows nothing of devices or the GPU, so tests step it directly. |
| `rig.go` | Turns the rig and clips into anim3d clips, builds each actor's state machine and poses its boxes. |
| `render.go` | The camera, the lights and the GPU renderer. |
| `view.go` | The root component: the fixed-step [`game.Loop`](loop.md), input, the HUD and the touch controls. |
| `*_test.go` | The playthroughs and the input, rendering and file-format tests. |

## Where to change it

- **The level.** Move walls, add enemies or lamps, or draw a new level in `scenes/keep.scene.yaml`, by hand or in the scene editor (`go tool game editor .`). The tests check that it still loads; the bot's route in `game_test.go` says how a run is won, so a new layout needs a new route.
- **The fight.** `data/actors.yaml` holds every number: an enemy's `attackSpeed` below 1 slows its swing, and `health`, `reach`, `damage`, `cooldown` and `knockback` change the difficulty without touching code.
- **Animation.** Edit the clips' keys in `data/actors.yaml`, or add a clip and a state for it in `newAnimator` in `rig.go`.
- **New objects.** Register a component type in `levelComponents` in `level.go`, such as a moving platform or a lever, read it in `buildLevel`, and connect its events to handlers in the scene.
- **Models.** Replace the boxes with glTF models: load them with package `gltf` ([3D models from Blender](3d-models-blender.md)), upload their meshes with `render3d.UploadMesh` and draw them in `renderer.draw`.

After editing `game.yaml`, `go run github.com/nsgo-dev/game/packages/game/cmd/gameproject sync` writes what `nsgo.yaml` shares with it, and after moving or adding files `go tool game index` keeps `game.index.yaml`, the project's index of stable file IDs, up to date.
