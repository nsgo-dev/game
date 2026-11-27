# Racing 3D Template

The `racing3d` template is a small 3D racing game: two laps of a closed circuit against two computer-driven rivals, from the back of the grid, with a countdown, checkpoints, a ghost of your best lap and a results screen. It plays with a keyboard, a gamepad or touch, on the desktop, phones and the web.

```sh
go run github.com/nsgo-dev/game/cmd/game@latest new -template racing3d myracer
cd myracer
go tool nsgo dev
```

[Templates](templates.md) describes `game new` and `nsgo init -template`, which writes the same project from the module `github.com/nsgo-dev/game/template/racing3d`.

## What it shows

- Raycast cars from [`vehicle`](vehicle.md) on a [`physics3d`](physics3d.md) world, every car tuned by one `vehicle.Config` file.
- Laps and checkpoints with `vehicle.Race`: a lap counts only when a car passes every checkpoint in order, so cutting the track does not pay.
- A three-second countdown, a HUD with the lap, the position and the lap clock, and a results screen. Finishing first wins; finishing lower, or not finishing before both rivals do, loses.
- Rivals that follow a racing line: they steer toward a point ahead on the line and hold a speed planned to brake for every corner, scaled by each rival's pace.
- A ghost of your best flying lap, recorded with `vehicle.GhostRecorder`, raced against on every lap after the first and kept between launches in the app's storage as the YAML `vehicle.EncodeGhost` writes. Lap 1 starts on the grid behind the line, so it is neither saved as the ghost nor raced against one, and a one-lap race has no ghost. The saved ghost names a hash of the track file, so editing anything in `data/track.yaml` retires it.
- A scene on a GPU view drawn with [`render3d`](render3d.md)'s lit PBR pipeline: the road, curbs, posts and cars are meshes built in code, and the crates beside the road are the [placeholder pack](placeholder.md)'s `crate.glb`, read with `gltf`.
- One input map in `game.yaml` for the keyboard, gamepads and touch, read with `input.Actions` and `vehicle.Controls`, and an `input.VirtualPad` for the on-screen stick and pedals.
- Tests that play whole races through the app with each kind of input, reaching a win and a loss.

## Controls

| | Keyboard | Gamepad | Touch |
| --- | --- | --- | --- |
| Accelerate | Up or W | Right trigger or A | GAS |
| Brake, then reverse | Down or S | Left trigger or X | BRAKE |
| Steer | Left and Right, or A and D | Left stick or the D-pad | the stick, anywhere on the lower left |
| Handbrake | Space | B | |
| Back on the road | R | Y | R |
| Race again | Enter | Start | Race again |

## How it is organised

| File | What it holds |
| --- | --- |
| `game.yaml` | The project file: the window and the input map, with the actions `throttle`, `brake`, `steerLeft`, `steerRight`, `handbrake`, `respawn` and `confirm` and the `steer` axis over the keys and the left stick. |
| `data/track.yaml` | The circuit: its centerline points, width, laps, checkpoints, starting grid, and the rivals' names, colors, pace and lanes. |
| `data/car.yaml` | The car's tuning, a `vehicle.Config`. |
| `track.go` | The track file's format: `ParseTrack` refuses unknown fields and out-of-range values, and `EncodeTrack` writes it back unchanged. |
| `course.go` | The track turned into geometry: samples every couple of meters along the centerline, the checkpoints, the grid, and the racing line's speed at each sample. |
| `race.go` | The race, with no drawing: the physics world, the cars, the countdown, the laps, the standings, the results and the ghost. |
| `driver.go` | The racing-line driver the rivals use. |
| `scene.go`, `meshes.go`, `renderer.go` | The meshes, the lights and the GPU drawing. |
| `game.go` | The root component: the GPU view, the HUD, the results screen, the touch controls, the chase camera and the `game.Loop`. |
| `game_test.go`, `race_test.go`, `track_test.go` | Races played through the app with a gamepad, the keyboard and touch; races run on the simulation alone; the track format. |

The race is plain Go over `physics3d` and `vehicle` with no drawing, so `race_test.go` runs whole races in milliseconds. The app's tests start it with `nsgotest.NoGPU`, as `nsgotest` draws a GPU view with a software rasterizer far too slow for minutes of racing; `TestRaceViewDraws` checks the drawing on its own at a small size.

## Where to change it

- **The circuit**: edit `points`, `width` and `start` in `data/track.yaml`, keeping every point within 300 m of the origin, where the ground is. The road, curbs, checkpoints, grid and racing line all follow the points, and `TestCourseGeometry` checks the result.
- **The rivals**: each rival's `pace` makes it faster or slower, and the file takes up to seven rivals. `laps` sets the race's length.
- **The car**: edit `data/car.yaml`; [vehicle](vehicle.md) describes each field.
- **Collisions between cars**: the cars pass through each other so that a crowded corner never wrecks the race. Add `layerCar` to the chassis mask in `newRace`, in `race.go`, to make them collide.
- **The controls**: change the bindings in `game.yaml`; the code reads only the action and axis names.
- **The art**: load your own glTF models with `gltf.LoadFS` in `sceneMeshes` and add their primitives as parts, as the crates are.
