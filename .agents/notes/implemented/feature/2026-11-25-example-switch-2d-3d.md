# Agent Note: Switching between 2D and 3D scenes in examples/lantern3d

Status: implemented

## Problem

Issue #126 (part of #22, "3D: 2D sprites and native views over a 3D scene") requires two halves: the `scene.Manager.View` switching primitive (landed in [Switching between 2D and 3D scenes in scene.Manager](./2026-11-24-scene-switching-2d-and-3d.md)) and a runnable game under `examples/` that switches between a 2D scene and a 3D scene through `Manager.View` while sharing input and sound.

Until this change, `examples/lantern3d` mounted a single standalone 3D `gpu.View` wrapped in `scene.HUD3D.Wrap` with direct `cx.OnKeyWith` handlers and no audio, leaving no runnable example exercising `Manager.View` across a 2D ↔ 3D transition, `Push`/`Pop` of a transparent 2D overlay over a live 3D scene, shared `*input.Actions`, or an uninterrupted `*sound.Mixer`.

## Decision

Extend `examples/lantern3d` (see [3D glTF scene example (Lantern 3D)](./2026-11-24-example-3d-scene.md)) so its root component mounts a `scene.Manager.View` host switching between two complementary scenes while sharing one `*input.Actions` and one `*sound.Mixer`:

1. **Two primary scenes plus a transparent 2D pause overlay (`game.go`, `map2d.go`)**:
   - **3D Lantern Shrine (`Mode3D`, default entry scene)**: mounted through `Controller.View(scx)` using the mounted row's scope `scx` for both `gpu.View(scx, ...)` and `Renderer.Setup(scx, dev, c.Scene)`. Leaving 3D disposes `scx`, triggers `Renderer.Reset()`, and releases all GPU resources cleanly; returning to 3D mounts a fresh `gpu.View` and uploads fresh buffers, textures, and pipelines.
   - **2D Courtyard Overworld (`Mode2D`)**: rendered through `Controller.Draw` on the host's 2D `core.Canvas` using a `scene.Scene`, `tilemap.Camera`, procedural 64×16 pixel-art `*sprite.Atlas` (`traveler`, `lantern`, `crate`, `crystal`), `sprite.Batch`, and a `screen.Viewport` (`320×180`, `ModeFit`, zero `BarColor`) depicting the same twilight courtyard, four corner lanterns, crate, crystal cluster, orbiting wisp, and central glowing shrine portal.
   - **Transparent 2D pause overlay (`Transparent: true`)**: pushed via `Manager.Push` (`P` / `Escape` / Pause button) to pause simulation updates on top of either scene while keeping the 3D `gpu.View` mounted and visible underneath without rebuilding GPU pipelines (zero `BarColor` on `HostOptions.Viewport` stays transparent so margins do not obscure the 3D scene).
   - Transitions between `Mode3D` and `Mode2D` run `scene.Fade(320*time.Millisecond, core.RGB(clearSkyHex))` via `Tab` / `M`, the persistent HUD switch button, or pressing `E` / `Enter` on the 2D shrine portal.
2. **One shared `*input.Actions` (`Manager.Actions`)**:
   - Bound once on the parent scope (`WASD` / Arrows, `=` / `+` / `-`, `Space`, `L`, `R`, `H`, `Tab` / `M`, `E` / `Enter`, `P` / `Escape`, plus `move_x` and `move_y` axes), wired with `WatchKeys(cx)`, and assigned to `Manager.Actions` before `Manager.View`.
   - Drives 3D camera orbit/zoom in `Mode3D` and 2D traveler movement in `Mode2D`. `Manager.Switch`, `Push`, and `Pop` call `Actions.Reset()` automatically so held keys (such as holding `Tab` across a fade) never bounce back or leak into the incoming scene.
3. **One shared `*sound.Mixer` on the Manager's parent scope (`audio.go`)**:
   - Synthesizes deterministic in-memory WAV audio via `sound.EncodeWAV` (a seamless 4-second looping twilight pentatonic ambient track, a two-note scene-switch temple chime, and an action tick) with zero extra binary asset files.
   - Because `sound.NewMixer(cx, "lantern3d.sound")` lives on the Manager's parent scope rather than a scene's `Enter` scope, the looping ambient music plays uninterrupted across every 3D → 2D → 3D transition and pause/resume cycle while the persistent HUD mute button controls master volume for both scenes.
4. **Remount-safe GPU drawable tracking (`renderer.go`)**:
   - `gpuDrawable` stores `meshIdx` and `primIdx` so `buildDrawables` always refreshes `d.prim` from `r.meshes` after any mesh re-upload (on device restore or 3D scene remount), and `Renderer.Reset()` clears stale handles on view scope cleanup.

## Alternatives considered

### Create a separate new example module under `examples/`

Strongest argument: a standalone `examples/switch2d3d/` module would isolate the scene-switching demo from `examples/lantern3d`'s glTF/PBR showcase.

Rejected because a separate module duplicates `go.work`, `.github/workflows/examples.yml`, `game.yaml`/`nsgo.yaml`, `CREDITS.md`, and 3D scene/renderer boilerplate for a tiny toy quad, whereas extending `examples/lantern3d` demonstrates switching between a real 2D sprite overworld and a real glTF PBR 3D shrine with zero new module or CI-job overhead.

### Start `examples/lantern3d` in the 2D Courtyard Map by default

Strongest argument: starting on the 2D overworld and walking into the 3D shrine matches classic RPG overworld-to-interior progression.

Rejected because `examples/lantern3d` is also the repository's primary Stage 2 3D glTF showcase (#205), so opening directly on the lit 3D shrine preserves immediate visual impact and full backward compatibility with existing tests while keeping the 2D Courtyard Map one keypress (`Tab` / `M`) or one HUD click away (and tests cover both 3D → 2D → 3D and 2D → 3D → 2D starting paths).

### Ship pre-rendered PNG/WAV assets under `examples/lantern3d/assets/`

Strongest argument: external image and audio files show asset-pipeline loading from disk.

Rejected because `examples/lantern3d` already ships its generated `.glb` with embedded PNGs; synthesizing the small 64×16 2D sprite sheet and WAV clips deterministically in memory keeps the binary footprint unchanged while still exercising `sprite.Atlas`, `audio.LoadSound`, `sound.Mixer`, `sound.Music`, and `sound.Effect` identically on desktop, mobile, web, and headless `nsgotest`.

## Consequences

- **Benefits**:
  - Completes issue #126's remaining acceptance item with a runnable, cross-target example demonstrating `Manager.View` 2D ↔ 3D `Fade` transitions, `Push`/`Pop` transparent 2D overlays over 3D, shared `*input.Actions`, and uninterrupted `*sound.Mixer` music and SFX.
  - Under `NoGPU`, users see the fallback message in 3D mode and can switch cleanly to the 2D Courtyard Map where the fallback banner hides and 2D gameplay runs normally.
  - Fixes a latent remount/restore hazard in `Renderer.buildDrawables` where `gpuDrawable.prim` could retain a disposed `*render3d.GPUMesh` copy after mesh re-upload.
- **Costs**:
  - `examples/lantern3d` adds ~500 lines across `audio.go` and `map2d.go` and routes keyboard controls through fixed-step `input.Actions.Step()` rather than immediate `cx.OnKeyWith` callbacks.

## Verification

- `examples/lantern3d/game_test.go`:
  - `TestSwitch3DTo2DAndBackWithSharedInputAndSound` runs on both `full` and `nsgotest.NoGPUCompute()` (`webgl2-gles3`) profiles, verifying initial 3D GPU pixel readback, shared `input.Actions` orbiting the 3D camera, holding `Tab` across the 3D → 2D `Fade` without bouncing back, clean 3D view unmount (`ViewCleanups3D == 1`, `!Renderer.Ready()`), active 2D map canvas rendering, shared `input.Actions` walking the 2D traveler to the shrine portal, pressing `E` to fade 2D → 3D with clean `gpu.View` remount and lit 3D pixel readback, switch chime SFX plays, and uninterrupted looping ambient music throughout.
  - `TestSwitch2DTo3DTo2DViaHUDButtonAndPauseOverlayPreserves3D` starts in 2D, switches 2D → 3D via the HUD button, pushes and pops the transparent 2D pause overlay verifying 3D simulation freezes and resumes with zero GPU view rebuilds, switches 3D → 2D via the HUD button, and verifies shared mixer mute/unmute and uninterrupted music.
  - `TestNoGPUFallbackShowsMessageAndAllowsSwitchingTo2D` verifies `NoGPU` fallback in 3D and clean transition to the 2D Courtyard Map.
  - All existing 3D rendering, camera orbit, HUD pass-through, pinch/touch, keyboard, mouse-leave, device loss/restore, glTF asset, and canonical project tests remain green.
- Verified with `gofmt`, `cd examples/lantern3d && go tool nsgo test ./... && go tool nsgo check -target macos,windows,linux,android,web`, `cd packages/game && go run ./cmd/gameschema -check && go run ./cmd/game check ../../examples && go tool nsgo test ./refs ./placeholder ./tooling/credits && go tool nsgo check -target macos,windows,linux,android,web`, `go run ./tools/changelog check`, and `bun run verify-notes`.
