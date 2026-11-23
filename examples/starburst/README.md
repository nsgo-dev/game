# Starburst

**Starburst** is a crisp, high-energy top-down twin-stick / auto-aim arena shooter built with NSGo and the 2D Game SDK (`game`, `sprite`, `particles`, `physics`, `tilemap`, `screen`, `input`, `random`).

It demonstrates high-throughput 2D rendering (**1,000+ active moving sprites, bullets, orbs, and particles through a single `sprite.Batch`**), deterministic seeded simulation, procedural pixel-art assets with zero external files, camera shake and screen flashes, and a native NSGo HUD.

## Features

- **Zero external assets**: Builds a premultiplied procedural pixel-art RGBA atlas (`ship`, `drone`, `swarm`, `bullet`, `orb`, `spark`, `ring`) in memory at startup.
- **Single-batch 1000+ sprite pipeline**: Preallocated object pools (`enemies`, `bullets`, `orbs`), a zero-allocation intrusive spatial hash grid for `physics.TestCircleCircle` collisions, and three deterministic `particles.Emitter` instances (thruster trails, hit sparks, and explosions) rendered through one reusable `sprite.Batch`.
- **1000-Sprite Stress Mode**: Toggle **STRESS 1000** from the HUD (or press `T`) to spawn and maintain 1,000+ active moving swarmers, drones, plasma bolts, XP orbs, and particle bursts on the fixed 60 Hz `game.Loop`.
- **Camera & Visual Effects**: `tilemap.Camera` smooth follow with world bounds clamping, deterministic screen shake on explosions/damage/Nova, expanding Nova shockwave rings, and decaying screen-space flashes inside a letterboxed 480×270 `screen.Viewport`.
- **Cross-Platform Input & Gamepad Readiness**:
  - **Keyboard**: `WASD` or Arrow keys to move, `Space` for Nova pulse, `T` for Stress 1000, `P` to Pause/Resume, `R` to Restart.
  - **Mouse**: Aim at cursor, Left-Click to fire (auto-aims nearest enemy when cursor is idle), Right-Click for Nova pulse.
  - **Touch (`input.Actions`)**: Bottom-left virtual D-pad regions (`move_x` / `move_y` axes), right-half fire zone, and bottom-right Nova zone mapped through `screen.Viewport.ViewToGame`.
  - **Gamepad readiness**: Hardware gamepad bindings await [`nsgo#1049`](https://github.com/nsgo-dev/nsgo/issues/1049). `Arena.Control` (`ControlInput`) already accepts normalized `MoveX`/`MoveY`, `AimStickX`/`AimStickY`, `Fire`, and `Nova` per fixed step so gamepad sticks and triggers drop straight in with zero simulation changes.

## Leaderboard

`leaderboards.yaml` declares a `high-score` board for the [`leaderboard`](../../docs/en/leaderboard.md) package. The game signs the player in at launch, submits the score when a run ends (stress mode excluded), and shows a **LEADERBOARD** button on the game-over screen that opens Game Center's board. A player who skipped the launch sign-in gets a **SIGN IN** button there instead, which signs in and submits that run. To ship it, register `dev.nsgo.game.starburst.high_score` in App Store Connect; for Android, create the board in the Play Console and add its generated ID as `playGames`. Platforms without a game service show no leaderboard UI.

## Run, Test, and Benchmark

From `examples/starburst/`:

```sh
# Launch interactively
go tool nsgo dev

# Run headless tests
go tool nsgo test ./...

# Run the 1000-sprite benchmark
go tool nsgo test -bench BenchmarkArena1000Sprites ./...

# Cross-compile check across desktop, Android, and Web
go tool nsgo check -target macos,windows,linux,android,web
```
