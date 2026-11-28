# platformer

`github.com/nsgo-dev/game/pkg/platformer` turns a [`physics.CharacterBody`](physics.md) into a platformer character with coyote time, jump buffering, variable jump height, wall slides and wall jumps. Each helper's timing is a tunable in a YAML config.

```go
import "github.com/nsgo-dev/game/pkg/platformer"
```

Coordinates follow `physics`: X increases to the right and Y increases downward. Speeds are in points per second and are written as positive numbers; the controller applies the direction.

## Controller

```go
body := physics.NewCharacterBody(physics.NewBoxBody(32, 200, 12, 16))
ctl, err := platformer.NewController(body, platformer.DefaultConfig())
if err != nil { ... }

loop := &game.Loop{
	Update: func(dt time.Duration) {
		r := ctl.Step(platformer.Input{
			Move:        float32(actions.Axis("move")),
			JumpPressed: actions.JustPressed("jump"),
			JumpHeld:    actions.Pressed("jump"),
		}, grid, slopes, platforms, dt)
		if r.Jump != platformer.JumpNone {
			playJumpSound()
		}
		showWallSlide(r.WallSliding)
	},
}
```

`Step` runs once per fixed step. It reads the contacts the body's previous `Move` left on it (`Grounded`, `OnLeftWall`, `OnRightWall`), sets the body's velocity from the `Input` and the config, and moves it with `CharacterBody.Move`, so slopes, one-way tiles and moving platforms work as they do for the body alone. To collide against static or kinematic bodies (such as doors, blocks and `Body.OneWay` platforms) and static `RigidBody` colliders alongside tiles, slopes and moving platforms, call `StepWorld(in, world, slopes, platforms, dt)` (or assign `body.World = world`). The `StepResult` reports the `MoveResult`, the jump the step started (`JumpGround`, `JumpWall` or `JumpNone`), whether it cut a jump short (`JumpCut`), slid on a wall (`WallSliding`), ignored horizontal input (`InputLocked`), and which wall the body could jump from (`WallSide`, -1 left, 1 right, 0 none).

For a tilemap, pass a [`tileshape.View`](tileshape.md) as `grid` and its `Slopes()` (or `AppendSlopesIn` around the body) as `slopes`: the character then walks the slope, 2:1 and half-height tiles drawn in Tiled's collision editor with no hand-placed segments.

`Reset` forgets every timer, such as after a respawn, and `SetConfig` swaps the tunables while the game runs.

## Timing

Every timing is measured in step time. An event opens a window on the step it happens, and the window stays open on later steps while less than its duration has passed since that step. At a fixed `dt`, a duration `D` covers `ceil(D/dt)` steps counting the event's own step, and a zero duration covers only that step. With `coyote_time: 50ms` at 100 Hz, a jump pressed 1 to 4 steps after the last grounded step still leaves from the ground, and one pressed 5 steps after does not. `game.Loop` passes `dt` as `time.Second/Rate` truncated to whole nanoseconds, so each step is allowed 1ns of slack: at the default 60 Hz, `coyote_time: 100ms` covers 6 steps, not 7.

| Helper | Window starts when | Config |
| --- | --- | --- |
| Coyote time | the body is on the ground | `coyote_time` |
| Jump buffer | jump is pressed | `jump_buffer` |
| Wall coyote time | the body touches a wall in the air | `wall_coyote_time` |
| Wall-jump lock | a wall jump starts | `wall_jump_lock` |

- **Coyote time and jump buffer**: a step jumps when a press is still buffered and the body is on the ground or still within coyote time, so a press shortly before landing jumps on landing and a press shortly after running off a ledge still jumps. A jump closes both windows, so one press jumps once. A body that is already rising in the air, such as from a spring, cannot use coyote time, so pressing jump does not replace that launch with a weaker ground jump.
- **Variable jump height**: when the jump button is up during a step of a rising controller jump, the upward speed is multiplied by `jump_cut` once, if it is above `jump_cut_min_speed`. A tap that presses and lets go within one step gets the shortest jump. A launch the game sets itself, such as a spring, is never cut: when the body's `VY` differs from the one the previous `Step` left, the controller jump is over.
- **Wall slide**: while falling against a wall and pushing into it, the fall speed is capped at `wall_slide_speed`. The contact comes from the previous step's `Move`, which reports a wall only when the body moved into it.
- **Wall jump**: a buffered press in the air while touching a wall, or within `wall_coyote_time` of leaving it, starts `wall_jump_speed_x` away from the wall and `wall_jump_speed_y` up. A ground jump wins when both are possible. Horizontal input is then ignored for `wall_jump_lock`, so holding toward the wall does not cancel the push.

Setting `wall_slide_speed` or `wall_jump_speed_y` to 0 turns that helper off, `jump_cut: 1` turns off variable height, and `run_speed: 0` leaves horizontal velocity to the game.

## Config file

A `Config` is a versioned YAML document. `ParseConfig` refuses unknown fields, a missing or unsupported `version`, negative or non-finite values, `jump_cut` above 1 and trailing documents; durations are Go duration strings. `EncodeConfig` writes a canonical form: every field in a fixed order, durations as Go duration strings and numbers in their shortest form. Encoding a parsed config gives that form, and parsing and encoding the canonical form again gives the same bytes, so a hand-written file with comments, another key order or omitted fields is rewritten in canonical form. `DefaultConfig()` is:

```yaml
version: 1
coyote_time: 100ms
jump_buffer: 100ms
jump_speed: 284
jump_cut: 0.55
jump_cut_min_speed: 110
gravity: 720
max_fall_speed: 320
run_speed: 98
ground_accel: 960
ground_decel: 1150
air_accel: 680
wall_slide_speed: 60
wall_coyote_time: 80ms
wall_jump_speed_x: 150
wall_jump_speed_y: 260
wall_jump_lock: 150ms
```

`ground_accel` and `air_accel` move the horizontal speed toward `move * run_speed`, and `ground_decel` slows it on the ground without input; 0 reaches the target at once. `max_fall_speed: 0` leaves the fall uncapped.

## Scene behavior configs

[Input-driven behavior configs](scene.md#input-driven-behavior-configs) describe a `platform` behavior's settings in a scene-side schema. No component wraps `Controller` yet, so a game still drives the controller from Go.
