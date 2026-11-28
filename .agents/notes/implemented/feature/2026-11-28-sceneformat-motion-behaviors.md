# Agent Note: Built-in 2D motion behavior components and pkg/motion

Status: implemented

## Problem

Built-in scene components covered physics (`body2d`, `collider2d`, `trigger2d`), 2D lighting (`light2d`, `occluder2d`), navigation (`navregion2d`, `nav_link`), skeletal rigs, animators, on-screen notifiers, and timers, while `BuiltScene.Update` stepped only tilemap animations, particle emitters, skeletons, and `on_screen` notifiers. Simple 2D gameplay and decorative motion — bullets, bobbing pickups, spinning hazards, orbiting shields, point-to-point movers, edge wrapping, bounds clamping, off-screen cleanup, fade-out text, and hit flashes — had to be hand-coded in Go per game and stood still in Play-in-Editor (issue #1751).

## Decision

Ten built-in 2D motion behavior components are registered through `sceneformat.RegisterBehaviorComponents` / `BehaviorComponentRegistry` (each carrying `version: 1` and optional `enabled: true`) and backed by matching pure-Go, zero-allocation types in `github.com/nsgo-dev/game/pkg/motion`:

### Component schemas, events and handlers

- `bullet`: `version` (`1`), `speed` (`240`), `acceleration` (`0`), `gravity` (`0`), `angle` (`0` deg, `0 = +X`, `90 = +Y`), `setAngle` (`true`), `bounceOffSolids` (`false`), `enabled` (`true`). Emits `bounced`; handles `setSpeed(speed)`, `setAngle(angle)`, `start`, `stop`.
- `sine`: `version` (`1`), `property` (`x|y|angle|scale|alpha`, default `y`), `period` (`1.5`, `>= 0`), `magnitude` (`6`), `wave` (`sine|triangle|sawtooth|reverse_sawtooth|square`, default `sine`), `phase` (`0` cycles), `enabled` (`true`). Handles `start`, `stop`.
- `rotate`: `version` (`1`), `speed` (`180` deg/s), `acceleration` (`0` deg/s²), `enabled` (`true`). Handles `setSpeed(speed)`, `start`, `stop`.
- `orbit`: `version` (`1`), `target` (`""` = initial position, or another object ID), `radius` (`32`, `>= 0`), `speed` (`90` deg/s), `angle` (`0`), `setAngle` (`false`), `enabled` (`true`). Handles `setSpeed(speed)`, `start`, `stop`.
- `move_to`: `version` (`1`), `maxSpeed` (`120`, `>= 0`), `acceleration` (`0`, `>= 0`), `stopOnSolids` (`false`), `targetX` (`0`), `targetY` (`0`), `active` (`false`), `enabled` (`true`). Emits `arrived`; handles `moveTo(x, y)`, `setSpeed(speed)`, `stop`.
- `wrap`: `version` (`1`), `mode` (`scene|view`, default `scene`), `margin` (`0`, `>= 0`), `enabled` (`true`). Emits `wrapped`; handles `start`, `stop`.
- `bound`: `version` (`1`), `mode` (`edge|origin`, default `edge`), `enabled` (`true`). Handles `start`, `stop`.
- `destroy_outside`: `version` (`1`), `margin` (`64`, `>= 0`), `mode` (`scene|view`, default `scene`), `enabled` (`true`). Emits `destroyed`; handles `start`, `stop`.
- `fade`: `version` (`1`), `fadeIn` (`0`, `>= 0`), `wait` (`1`, `>= 0`), `fadeOut` (`0.5`, `>= 0`), `destroy` (`true`), `autostart` (`true`), `enabled` (`true`). Emits `finished`; handles `start`, `stop`.
- `flash`: `version` (`1`), `on` (`0.05`, `>= 0`), `off` (`0.05`, `>= 0`), `duration` (`0`, `>= 0`), `enabled` (`true`). Emits `finished`; handles `flash(duration)`, `stop`.

### Deterministic stepping, solids and freeing

- `BuiltScene.Step(dt)` (and `StepWithViews(dt, views...)`) steps all behavior-bearing objects in document order, applying components per object in fixed pipeline order (`bullet` -> `move_to` -> `orbit` -> `rotate` -> `sine` -> `wrap` -> `bound` -> `destroy_outside` -> `fade` -> `flash`) so additive oscillation, edge constraints, and lifecycle cleanup compose predictably.
- Every trigonometric call goes through `internal/detmath` so arm64, amd64, and wasm stay bit-for-bit identical. `Bullet`, `Rotate`, and `MoveTo` integrate constant acceleration analytically (exact closed-form displacement per segment, RK2 when bullet acceleration and gravity both act), `Sine`, `Fade`, and `Flash` evaluate against accumulated simulated time, and `Wrap` preserves modulo overshoot so 60 Hz and 120 Hz runs agree per simulated second.
- `BuildPhysics` attaches the constructed `*PhysicsScene` to `BuiltScene.Physics`. When `BuiltScene.Physics != nil`, `bounceOffSolids` and `stopOnSolids` perform continuous swept queries against both `PhysicsScene.World2D` (excluding the moving object's own collider) and lazily cached `tileshape.View` grids for scene tilemaps so bullets bounce and movers halt accurately against both collider bodies and `TileSolid` tiles.
- `BuiltScene.Free(id)` removes an object and its descendants from `BuiltScene.Objects`, layer draw lists/batches/emitters/skeletons, notifiers, motion behaviors, 2D physics worlds, and (when `SpawnConnected` created `Signals`) live ECS entities. `destroy_outside` and `fade` emit their events just before calling `Free` so connected handlers and listeners observe them.
- The editor registers all ten components in `builtinEditorComponents` and `editorcmd`'s registry, `tools/gameschema` embeds all ten in `scene.v1.json` through `scene.v4.json`, and Play-in-Editor connects signals against the merged built-in registry and runs `e.playScene.Step(dt)` on every fixed step.

## Alternatives considered

- **Advancing behaviors inside `BuiltScene.Update(dt)` instead of a new `BuiltScene.Step(dt)`**: simpler call site for callers that already call `Update`, but `Update` steps presentation state (tile animations, emitters, skeletons) while motion behaviors are gameplay simulation that must run on the fixed loop with `PhysicsScene.Step` and `Signals.Update`, matching issue #1751's contract.
- **Keeping the math private inside `pkg/sceneformat` instead of a public `pkg/motion` package**: avoids a new package entry in the stability table and skill catalog, but code-first games that do not author `.scene.yaml` files would have to reimplement bullet bouncing, sine waves, and fade envelopes instead of sharing one tested deterministic implementation.
- **Semi-implicit Euler integration (`v += a*dt; p += v*dt`)**: shorter code, but causes position drift proportional to `a * dt` between 60 Hz and 120 Hz fixed rates, failing rate-independent trajectory guarantees.

## Consequences

- **Benefits**: scenes can express common 2D projectile, hazard, pickup, boundary, and feedback behaviors declaratively in YAML with strict schema validation, editor inspection, live Play-in-Editor preview, and zero Go boilerplate; Go-only games get zero-alloc, cross-architecture deterministic helpers in `pkg/motion`.
- **Costs**: adds a new experimental package `game/pkg/motion` (tracked in stability tables, skill catalog, and PR labeler) and expands the published scene JSON Schemas with ten component definitions; `BuiltScene.Free` currently removes built objects cleanly for destruction behaviors and will integrate with runtime scene instancing when #1355 lands.
- **Verification**: `pkg/motion/motion_test.go` verifies rate independence (60 Hz vs 120 Hz), golden waveform and bounce values, and 0-alloc hot paths; `pkg/sceneformat/motion_behaviors_test.go` verifies schema validation (`*ComponentError` and `loaderrtest`), rate independence across all ten components, physics and tilemap solid bouncing, `Free` with ECS cleanup, signal handlers, view bounds, and the end-to-end `testdata/behaviors.scene.yaml` showcase fixture; `pkg/editor/behaviors_play_test.go` verifies inspector discovery and Play-in-Editor execution; `gameschema -check`, `repocheck`, `changelog check`, and `verify-notes` all pass.
