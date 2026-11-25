# Agent Note: The racing3d template

Status: implemented

## Problem

Issue #792, under the per-genre template plan of #786, asks for a `racing3d` template: a 3D racer on one track with laps, checkpoints, a countdown, a ghost of the best lap, computer rivals on a racing line and a results screen, built on `vehicle`, `physics3d`, `render3d` and `gltf`, playable with keyboard, gamepad and touch, and with a headless playthrough that reaches a win and a loss. It has to be small enough to read in an afternoon and to build against the game layer version its `go.mod.tmpl` pins, outside the workspace.

## Decision

The template is the module `github.com/nsgo-dev/game/template/racing3d`, its project in `template/racing3d/template/`, copied byte for byte into `packages/game/cmd/game/_templates/projects/racing3d` and registered in `projectTemplates`.

### Simulation apart from drawing

`race.go` holds the whole race as plain Go over `physics3d` and `vehicle`, with no view, GPU or clock: `Race.Step(input, respawn)` is one fixed step. `game.go` feeds it from `input.Actions` inside a `game.Loop` and `renderer.go` draws it on a `gpu.View`. Every rule (countdown, laps through `vehicle.Race`, standings, win and loss, the ghost) lives in `race.go`, so `race_test.go` runs whole races in milliseconds and the app's tests only add the input path and the screens.

### Rules

- The player starts last on the grid; the race ends when the player finishes (win if first, loss otherwise) or when every rival has finished first (loss, the player marked as not finished). There is always an outcome without a timer.
- Standings order finished cars by finish step and the rest by distance covered: laps, the checkpoint last passed and the distance along the centerline past it, so the HUD position is right between checkpoints, which `vehicle.Race.Standings` alone cannot say.
- Cars are on their own physics layer that does not collide with itself; wheels ray only the ground layer; checkpoints are trigger spheres on a third layer. Off the road a car faster than 12 m/s is held back with partial brake, and a car on its side for a second, or 5 m below the ground after leaving its edge, is put back on the centerline.
- One bound is authoritative for the ground: `Validate` keeps centerline points within `maxTrackRadius` (300 m) of the origin, and both the physics ground box and the drawn ground reach `groundHalf` (400 m), so any valid track's road, half width and curve overshoot included, lies on the ground. `TestLargeTrackStaysOnTheGround` races on a 300 m circle.
- The best lap's ghost is the player's fastest flying lap as a `vehicle.Ghost` across races, saved with `vehicle.EncodeGhost` in `core.Storage` under `racing.bestLap`. Its `Name` is the track's name and a SHA-256 of the canonical `EncodeTrack` output, and it is ignored when it does not parse, names another layout, or was recorded at another step rate.
- Lap 1 is a standing lap: `vehicle.Race` times it from the lights with the car on the grid behind the line, while later laps run from the line at speed. Only laps after the first are compared and saved as the ghost, and the ghost only races on laps after the first, so a replay always starts at the line next to the player. A one-lap race never records a ghost. Timing lap 1 from the line instead was rejected: the line is a trigger sphere that the front row already overlaps on the grid, so its first enter event does not mark a crossing.

### Track format

`data/track.yaml` is a versioned YAML file: `version`, `name`, `laps`, `width`, `points` (`{x, z}` flow mappings, one per line, of a closed centripetal Catmull-Rom loop through `math3d.NewCatmullRom3DAlpha`), `start`, `checkpoints`, `grid` and `rivals` (`name`, `color`, `pace`, `lane`). `ParseTrack` refuses unknown fields, trailing documents and out-of-range values; `EncodeTrack` writes fields in declaration order, and `TestTrackFileIsCanonical` keeps the shipped file byte-identical to its encoding. The car's tuning is a `vehicle.Config` in `data/car.yaml`, checked the same way against `vehicle.EncodeConfig`.

### Racing line

`course.go` samples the centerline about every 2 m. Each sample's speed is the corner speed at 0.85 g over its curvature, then lowered by two backward passes so a car braking at 7 m/s² reaches every corner at its speed. `driver.go` steers by pure pursuit toward a point 5 m + 0.45 s ahead and holds the slowest planned speed over the next few samples times its pace. The tests drive the player with the same driver through gamepad, keyboard or touch input.

### Testing through the app without the GPU

`nsgotest` draws a `gpu.View` with a software rasterizer that interprets the WGSL a pixel at a time: one 960×540 frame of the lit PBR pipeline takes seconds. The playthroughs therefore start the app with `nsgotest.NoGPU()`, where the HUD and input still run, and `TestRaceViewDraws` alone renders at 96×54 and checks the draw count and that the frame is not one color. The gamepad playthrough wins, the keyboard playthrough at 0.6 pace loses, and the touch test drives each on-screen control in turn.

### Pin

`go.mod.tmpl` pins `packages/game` at `v0.0.0-20261125052836-0ec1f801ee71`, the first `main` with key and gamepad bindings in `game.yaml` (#908) and with `render3d.NewLitPBRPipeline`, `PBRDrawBuffer` and `NewLightBuffer`; the starter template's older pin has none of them.

## Alternatives considered

### Cars that collide

Real contact between chassis boxes is what a racing game ends up with, and the physics supports it. It is off by default because the rivals' pure-pursuit driver has no notion of other cars: a collision in a corner pushes a rival off its line or spins it, and a template whose rivals pile up teaches nothing. The README and the docs point at the one mask to change.

### Walls along the road

Static boxes along both edges would keep every car on the road without a grass rule. They were not used because a raycast car hitting a wall at speed bounces or climbs it, which needs tuning out of scope for a template, and about a hundred extra static bodies cost more than the grass check, one nearest-sample search per car per step that the race already does for standings.

### Drawing the 3D scene on a `core.Canvas`

Projecting the meshes with `math3d` and filling polygons on a canvas would have kept the whole app testable with nsgotest at full speed. It was rejected because the issue names `render3d` and a racing template should show the GPU path a 3D game grows on; the cost is the `NoGPU` split in the tests.

### Testing the playthrough on the simulation only

`race_test.go` already reaches a win and a loss. The acceptance asks for an nsgotest playthrough, and only one through the app proves that the input map, the virtual pad, the results screen and the restart work together, so both exist.

## Consequences

- A developer gets a working racer with the rules readable in one file and the drawing in two, and changes the circuit, rivals and car without touching Go.
- The cars passing through each other and the grass rule are simplifications a real racer replaces; both are documented as where to change it.
- Gamepad triggers act as buttons: `project.Input` binds gamepad buttons to actions, and `input.Actions` has no analog trigger axis, so throttle and brake from a trigger are all or nothing. Analog pedals need a trigger axis kind in `input` and `game.yaml`; reported to the maintainers for an issue in this repository.
- `nsgotest.App.Touch` gives every finger touch ID 1 (`nsgotest/pointer.go:172` in NSGo v0.3.0, reported to be filed in nsgo-dev/nsgo), so a test cannot hold the gas pedal and steer at once; the touch test lifts each finger before the next, and multi-finger routing stays covered by `input`'s own `VirtualPad` tests.
- The GPU view retains every resource, so after a device loss NSGo restores the buffers, meshes and pipeline and the renderer keeps them; `TestRaceViewDraws` checks it draws again with the same pipeline.
- The pin must move forward when the template starts using a newer package of the game layer.
