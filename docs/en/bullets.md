# bullets

`game/bullets` spawns, moves and draws the bullet patterns of shooters: rings, spirals and aimed bursts defined in YAML, taken from a `mempool.Pool` and drawn through one `sprite.Batch`.

```go
import "github.com/nsgo-dev/game/pkg/bullets"
```

## Pattern files

A pattern file is YAML with a `version` and a list of named `patterns`:

```yaml
version: 1
patterns:
  - name: bloom
    kind: ring
    frame: orb
    count: 24
    speed: 90
    spin: 7.5
    shots: 6
    interval: 400ms
    lifetime: 6s
    radius: 4
  - name: vortex
    kind: spiral
    frame: orb
    count: 5
    speed: 140
    speedJitter: 8
    angleJitter: 2.5
    spin: 11
    shots: 0
    interval: 50ms
    accel: -20
    turn: 15
    radius: 3
  - name: volley
    kind: aimed
    frame: dart
    count: 7
    speed: 220
    spread: 45
    shots: 3
    delay: 1s
    interval: 150ms
    lifetime: 3s
    radius: 2.5
    faceHeading: true
```

`Parse` reads a file and refuses unknown fields, a `version` other than `FormatVersion` (1), trailing YAML documents and invalid patterns. `Encode` writes a file back with 2-space indents, its fields in a fixed order and zero optional fields left out, so the same file always encodes to the same bytes and a load and save changes nothing. `File.Pattern(name)` finds a pattern.

Angles are in degrees clockwise from +X on the screen, speeds in points a second, and durations are Go durations (`50ms`, `2s`).

| Field | Meaning |
| --- | --- |
| `name` | Unique, not empty |
| `kind` | `ring`, `spiral` or `aimed` |
| `frame` | Atlas frame each bullet draws; empty spawns bullets that move and collide but draw nothing |
| `count` | Bullets each shot fires, 1 to `MaxCount` (4096) |
| `speed`, `speedJitter` | Starting speed, varied by a random amount in `[-speedJitter, speedJitter)`, never below 0 |
| `angle`, `angleJitter` | Direction of a shot's first bullet (for `aimed`, the turn from the target's direction), varied per bullet by `[-angleJitter, angleJitter)` |
| `spread` | Arc an `aimed` burst covers from its first bullet to its last; `aimed` only, above 0 when `count` is above 1 |
| `spin` | Degrees each shot turns more than the one before; a `spiral` needs one other than 0 |
| `shots`, `delay`, `interval` | Number of shots (0 fires until stopped), time before the first, time between shots (at least `1ms`, or 0 when `shots` is 1) |
| `lifetime` | How long each bullet lives; 0 keeps it until it leaves the field's bounds |
| `accel`, `turn` | Speed change in points a second each second (a bullet slowed to 0 stops), and path curve in degrees a second |
| `radius` | Hit radius, also used to tell when a bullet has left the bounds |
| `faceHeading` | Turns the sprite to the direction the bullet moves |
| `z` | Sprite order in the field's batch |

- `ring` spaces `count` bullets evenly around a full turn, the first at `angle`, turned by `spin` more each shot.
- `spiral` is a ring with a `spin`: with a short `interval`, each of its `count` bullets traces an arm.
- `aimed` spreads `count` bullets evenly across `spread`, centered on the direction from the emitter to its target and turned by `angle` and `spin`. With an odd `count` and no `angle` or `spin`, the middle bullet flies straight at the target; a single bullet flies at the target turned by `angle`.

## Field and Emitter

A `Field` owns the live bullets. `NewField(atlas, capacity)` keeps `capacity` bullets ready in a pool, so spawning up to that many allocates nothing. Set `Bounds` to remove bullets whose hit circle has wholly left the playfield, and `Limit` to cap how many the field holds.

`NewEmitter(field, pattern, seed)` fires a copy of the pattern into the field, and checks that the field's atlas has the pattern's frame. Move it with `X`, `Y`, aim it with `TargetX`, `TargetY`, pause it with `Stopped`, and start it over with `Reset`. A stopped emitter's clock stops too, so when it resumes it picks up its pattern where it paused instead of firing the shots it missed.

```go
file, err := bullets.Parse(data)
field := bullets.NewField(atlas, 6000)
field.Bounds = math2d.Rect{W: 480, H: 640}
volley, _ := file.Pattern("volley")
boss, err := bullets.NewEmitter(field, volley, seed)

loop := &game.Loop{
	Update: func(dt time.Duration) {
		boss.X, boss.Y = bossX, bossY
		boss.TargetX, boss.TargetY = playerX, playerY
		field.Update(dt) // move the bullets already in flight
		boss.Update(dt)  // then fire the shots due in this step
		field.RemoveFunc(func(b *bullets.Bullet) bool {
			return b.Circle().Overlaps(playerHitbox)
		})
	},
}
// In a core.Canvas's Draw:
field.Draw(d)
```

- Update the field before its emitters each step. A shot due partway through a step spawns its bullets where they are at the step's end, so bullets do not bunch up when the interval is not a whole number of steps.
- One `Update` fires at most 4096 bullets' worth of shots (at least one shot). When more are due, as after a long step from a hitch, it fires the latest and skips the earlier ones, which count as fired but spawn nothing and draw no random numbers.
- `Field.Bullets()` lists the live bullets oldest first, each with its position, `Speed`, `Heading` (radians), `Radius`, `Age`, `Lifetime`, `Pattern` name, `Shot` index and `Sprite`. The bullets are the field's: do not keep a pointer past the next `Update`, `RemoveFunc` or `Clear`, as the field reuses it.
- `Field.Update` keeps the order of the bullets it keeps, and `Field.Draw` draws them lowest `Z` first and oldest first for the same `Z`.
- A shot past the field's `Limit` spawns only the bullets that fit, and still draws the same random numbers.

## Determinism

An emitter draws its jitter from its own `random.Stream`. Two emitters with the same pattern and seed, moved and stepped with the same `dt`s and targets, spawn the same bullets in the same places: on one build target the results are bit-for-bit equal, which `random.AssertDeterministic` checks in the package's tests. Floating-point results can differ across CPU architectures, so a lockstep game across platforms should compare inputs, not bullet positions.

## Performance

`BenchmarkFrame5000` runs one 60 fps frame of a steady 5000-bullet scene: ten spiral emitters with jitter and curving paths, half of them with turned sprites, moving, expiring, spawning and drawing into a `core.Drawing`. On a desktop Ryzen 9 3900X it takes about 0.63 ms of the 16.7 ms a frame has; moving 5000 bullets alone (`BenchmarkUpdate5000`) takes about 0.14 ms. The performance baselines in CI (`benchmarks/baseline.yaml`) fail a change that makes a frame take more than 6.25 ms on the CI runner.

This measures the CPU work of a frame: simulating the bullets and recording their draw ops. It does not measure how long the platform's renderer takes to put those ops on screen, which is NSGo's and depends on the GPU.

- Moving and firing allocate nothing once the pool is warm.
- `sprite.Batch` draws the bullets as runs through NSGo's `Drawing.DrawAtlas`, which copies each run and allocates three values per run, however many bullets it holds.
