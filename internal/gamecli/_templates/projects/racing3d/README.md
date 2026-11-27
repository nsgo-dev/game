# NSGo Game Racing 3D Template

A small 3D racing game built with [NSGo](https://github.com/nsgo-dev/nsgo) and [`github.com/nsgo-dev/game`](https://github.com/nsgo-dev/game): two laps of a closed circuit against two computer-driven rivals, starting from the back of the grid.

It shows:

- Raycast cars from `vehicle3d` on a `physics3d` world: suspension, tire grip, steering and brakes, with every car's tuning in one YAML file.
- Lap and checkpoint counting with `vehicle3d.Race`: a lap counts only when a car passes every checkpoint in order, so cutting the track does not pay.
- A three-second countdown, a position and lap clock in a native HUD, and a results screen that ends in a win (first place) or a loss (any other place, or not finishing before both rivals do).
- Rivals that follow a racing line: pure-pursuit steering toward a point ahead on the line and a speed plan that brakes for each corner, scaled by each rival's pace.
- A ghost of your best flying lap, recorded with `vehicle3d.GhostRecorder`, raced against on every lap after the first and saved between launches as the YAML `vehicle3d.EncodeGhost` writes. Lap 1 starts on the grid behind the line, so it is neither saved as the ghost nor raced against one; a one-lap race has no ghost. The ghost is kept only for the exact track it was driven on: editing anything in `data/track.yaml` retires it.
- A scene drawn on a `gpu.View` with `render3d`'s lit PBR pipeline: the road, curbs, checkpoint posts and cars are meshes built in code, and the trackside crates are the placeholder pack's `crate.glb` read with `gltf`.
- Keyboard, gamepad and touch from one input map in `game.yaml`, read with `input.Actions` and `vehicle3d.Controls`, and an `input.TouchControls` layer built from `controls.touch.yaml` placed inside the safe area.
- Headless tests with `nsgotest` that play whole races with each kind of input and reach both a win and a loss.

## Controls

| | Keyboard | Gamepad | Touch |
| --- | --- | --- | --- |
| Accelerate | Up or W | Right trigger or A | GAS |
| Brake, then reverse | Down or S | Left trigger or X | BRAKE |
| Steer | Left and Right, or A and D | Left stick or the D-pad | the stick, anywhere on the lower left |
| Handbrake | Space | B | |
| Back on the road | R | Y | R |
| Race again | Enter | Start | Race again |

## Running and Testing

```sh
go tool nsgo dev                      # the desktop, with hot reload
go tool nsgo dev -target web          # the browser
go tool nsgo dev -target ios_simulator
go tool nsgo dev -target android
go tool nsgo test ./...               # the headless tests
go tool nsgo check -target macos,windows,linux,android,web
```

The game's settings live in `game.yaml`, its project file. Edit the name and identifier there before shipping it, then write them into `nsgo.yaml` and check the project:

```sh
go run github.com/nsgo-dev/game/cmd/game project sync
go run github.com/nsgo-dev/game/cmd/game project check
```

## Layout

- `game.yaml`: the project file, with the window and the input map: the actions `throttle`, `brake`, `steerLeft`, `steerRight`, `handbrake`, `respawn` and `confirm`, and the `steer` axis over the keys and the left stick.
- `controls.touch.yaml`: the on-screen touch layout (`input.TouchControls`): steering stick, GAS, BRAKE and respawn buttons anchored inside the safe area.
- `data/track.yaml`: the circuit: its centerline points, width, laps, checkpoints, starting grid and the rivals' names, colors, pace and lanes. `ParseTrack` in `track.go` reads it and refuses unknown fields.
- `data/car.yaml`: the car's tuning, a `vehicle3d.Config`.
- `track.go`: the track file's format. `course.go`: the track turned into geometry, samples along the centerline and the racing line's speed at each.
- `race.go`: the race itself, with no drawing: the physics world, the cars, the countdown, the laps, the standings, the results and the ghost.
- `driver.go`: the racing-line driver the rivals use.
- `scene.go`, `meshes.go` and `renderer.go`: the meshes, the lights, and the GPU drawing.
- `game.go`: the root component: the GPU view, the HUD, the results screen, the touch controls, the chase camera and the `game.Loop`.
- `CREDITS.md`: where every asset comes from and its license.
- `*_test.go`: the tests: `game_test.go` plays races through the app, `race_test.go` through the simulation alone, and `track_test.go` checks the track format.

## Where to change it

- A new circuit: edit `points`, `width` and `start` in `data/track.yaml`, keeping every point within 300 m of the origin, where the ground is. The road, curbs, checkpoints, grid and racing line all follow the points; `TestCourseGeometry` checks the result still makes sense.
- Harder or easier rivals: raise or lower each rival's `pace`, or add rivals (up to seven). More laps: `laps`.
- A different car: edit `data/car.yaml`. `vehicle3d.ReferenceConfig` documents each field; a heavier car needs stiffer springs.
- Cars that collide with each other: add `layerCar` to the chassis mask in `newRace`, in `race.go`. They pass through each other by default so a crowded corner never wrecks the race.
- Other controls: change the bindings in `game.yaml`; the game reads only action and axis names.
- Your own models: load a glTF file with `gltf.LoadFS` in `sceneMeshes` and add its primitives as parts, as the crates are.

## Git setup

Each clone enables the object-by-object scene merge driver once (`.gitattributes` names it, while Git keeps driver config local to `.git/config`):

```sh
go tool game merge -setup
```
