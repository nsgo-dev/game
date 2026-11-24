# Agent Note: Switching between 2D and 3D scenes in scene.Manager

Status: implemented

## Problem

`game/scene.Manager` previously assumed every managed scene rendered into a single caller-owned 2D `core.Canvas` via `Manager.Draw(d, size, alpha)`. Games combining 2D gameplay or menus (`core.Canvas` / `scene.Scene`) with 3D gameplay (`gpu.View` / `Overlay3D.View`) had no built-in way to mount and unmount 3D views on `Switch`, `Push`, and `Pop`, run fade or crossfade transitions across the 2D ↔ 3D boundary without tearing down GPU surfaces mid-transition, preserve 3D GPU state when pushing a 2D pause or inventory overlay, or share one `*input.Actions` and `*sound.Mixer` cleanly across both scene kinds while keeping `game/scene` free of experimental 3D imports (`TestStabilityTable`).

## Decision

Extend `scene.Controller` and `scene.Manager` with a reactive view host (`packages/game/scene/view_switch.go`) and minimal additive hooks in `packages/game/scene/manager.go` and `packages/game/scene/transition.go`, importing only stable packages (`game/input`, `game/sound`, `game/screen`, `nsgo/core`):

- **View-backed scenes (`Controller.View func(cx *core.Scope) core.Node`)**: A 2D scene leaves `View == nil` and draws through `Controller.Draw` or `Controller.Scene` on a host-managed `core.Canvas`. A 3D scene (or native UI screen) sets `Controller.View` to return a `gpu.View`, `Overlay3D.View` (see [2D sprite batches and screen layers over a 3D GPU view](./2026-11-24-scene-2d-layers-over-3d.md)), or native view tree scoped to the mounted row's `*core.Scope`.
- **Two-signal keyed mounting (`entriesRev` + `rev` with `core.Each`)**: Every entered `managedScene` receives a unique monotonic `id uint64`. `Manager.View(cx, opts)` drives `core.Each` over plain `[]uint64` IDs keyed by `id`, reading `entriesRev` (which bumps only when scenes enter or exit, never on transition frames or `Invalidate()`). Each entry wraps its content in an absolutely positioned `core.View` whose reactive `StyleFunc` reads `rev` to update `Visibility`, `Opacity`, and `PointerEvents`. As a result:
  - During `Fade` / `Crossfade` transitions, incoming 3D `gpu.View` surfaces mount once (`OnReady` runs once), never rebuild across animation frames, and outgoing views unmount cleanly on transition completion.
  - During `Push`/`Pop` of a transparent or opaque 2D overlay over a 3D scene, the 3D scene stays mounted at its existing `core.Each` key (preserving its GPU device, pipelines, buffers, and state) while paused and set to `PointerEventsNone` (and `VisibilityHidden` when occluded by an opaque overlay).
- **Cross-boundary transitions**: `Transition` records unexported kind metadata (`transitionCrossfade`, `transitionFadeColor`, `transitionWipe`, `transitionCustom`). A top-level `PointerEventsNone` transition overlay `core.Canvas` renders `Fade(d, color)` color veils across both 2D and 3D scenes (switching scene visibility at progress `0.5`), while `Crossfade(d)` animates wrapper `Opacity` (`1-p` / `p`). Pure 2D ↔ 2D `Wipe` and `CustomTransition` delegate to `Transition.Draw` on the transition overlay for exact parity with `Manager.Draw`; when a `Controller.View` scene is involved, `Wipe` uses smooth crossfade opacity without resizing the GPU swapchain, and `CustomTransition` flips visibility at `0.5` while drawing custom overlay effects on top.
- **Shared input and sound (`Manager.Actions`, `Manager.KeepInputOnSwitch`, `Manager.Mixer`)**: `Manager.View` attaches `Manager.Actions` to a persistent inner stage `core.View` (beneath optional `HostOptions.Children` persistent native HUD views so HUD buttons remain pressable without being captured by stage input) and supports `HostOptions.Ref` for focus and pointer lock. `Switch`, `Push`, and `Pop` call `Actions.Reset()` before entering or resuming the next scene unless `KeepInputOnSwitch` is true, preventing edge presses and held keys from leaking across scene boundaries while keeping all bindings, axes, and pointer lock intact. `Manager.Mixer` lives at or above the manager's scope so music continues uninterrupted and SFX groups remain live across every 2D ↔ 3D switch.

## State and ordering invariants

- `entriesRev` **must** bump only on structural stack/transition-membership changes (`Switch`, `Push`, `Pop`, `finishTransition`), never on per-frame transition ticks or `Manager.Invalidate()`.
- `m.resetInput()` **must** run before `m.enterScene(next)` (and before `under.ctrl.Resume()` on `Pop`) so actions configured or triggered inside `Enter`/`Resume` are not cleared.
- Transparent pushed 2D overlays rendered through `HostOptions.Viewport` **must** substitute `core.Transparent` for a zero `BarColor` on a local copy of `screen.Viewport` so letterbox/pillarbox margins do not obscure an underlying 3D scene or mutate the caller's struct.

## Alternatives considered

### Rebuild the active scene node inside a single `core.Dynamic`

Strongest argument: a single `core.Dynamic` returning the current scene's `core.Node` would require less code than keyed `core.Each` tracking and would not need a second `entriesRev` signal.

Rejected because `core.Dynamic` tears down and rebuilds its entire subtree whenever any signal it reads changes. Any `Push` of a 2D pause menu, any `Pop`, or any transition tick would destroy and recreate the 3D `gpu.View`, forcing GPU device/pipeline re-initialization and losing live 3D state.

### Add a 3D-specific `scene3d.Scene` or `render3d` hook directly onto `scene.Controller`

Strongest argument: a dedicated `Scene3D *scene3d.Scene` field on `Controller` could auto-wire a 3D camera and render loop without requiring callers to construct a `gpu.View`.

Rejected because `game/scene` is a Stable package while `game/scene3d`, `game/render3d`, and `game/math3d` are Experimental (`TestStabilityTable` forbids stable packages from importing experimental ones). Accepting `Controller.View func(cx *core.Scope) core.Node` keeps `game/scene` strictly within stable dependencies and works uniformly for raw `gpu.View`, `Overlay3D.View`, custom 3D pipelines, and native UI scenes alike.

### Resize the 3D `gpu.View` wrapper during `Wipe` transitions

Strongest argument: animating an `Overflow: Hidden` container width or height for `Wipe` would visually match 2D clip wipes across 2D ↔ 3D boundaries.

Rejected because resizing or ancestor-clipping native GPU surfaces (`CAMetalLayer`, Android `SurfaceView`, WebGL/WebGPU swapchains) risks per-frame swapchain reallocation or backend-specific clipping glitches. Falling back to smooth wrapper-opacity crossfade for view-backed `Wipe` keeps transitions deterministic across all targets while preserving exact clip-rect `Wipe` for 2D ↔ 2D.

## Consequences

- **Benefits**:
  - Games switch seamlessly between 2D canvas scenes and 3D `gpu.View` / `Overlay3D.View` scenes using the existing `Manager` lifecycle (`Switch`, `Push`, `Pop`, `Fade`, `Crossfade`, `Wipe`, `CustomTransition`) with zero experimental imports in `game/scene`.
  - Mounted 3D scenes preserve GPU pipelines, buffers, and simulation state across fade/crossfade frames and across pushed 2D overlays.
  - One `*input.Actions` and one `*sound.Mixer` drive both 2D and 3D scenes uninterrupted across switches.
  - Existing 2D callers using `Manager.Draw` directly remain 100% source- and behavior-compatible.
- **Costs & limits**:
  - `game/scene` now imports stable `game/input` and `game/sound` (both already stable with no circular dependency on `game/scene`).
  - `Wipe` across a `Controller.View` boundary uses smooth opacity crossfade rather than a hard geometric split over the GPU surface; `CustomTransition` across a `Controller.View` boundary draws only overlay effects over the switched views rather than re-capturing the 3D swapchain into `core.Drawing`.

## Verification

- `packages/game/scene/view_switch_test.go`:
  - `TestManagerSwitch2DTo3DAndBackWithSharedInputAndSound` runs on both default (`full`) and `nsgotest.NoGPUCompute()` (`webgl2-gles3`) profiles, verifying 2D sprite rendering, shared `input.Actions` driving both 2D and 3D scenes without edge leaks on `Reset`, `Fade` transition overlay opacity and midpoint swap with single `OnReady` mount, 3D `GPUReadPixels` mesh readback, uninterrupted looping `sound.Music` and shared SFX across 2D → 3D → 2D, and clean 3D view unmount/scope cleanup.
  - `TestManagerCrossfadeBetween2DAnd3D`, `TestManagerPushPop2DOverlayOver3DPreserves3DState`, `TestManagerKeepInputOnSwitchViewportAndPersistentHUD`, `TestManagerSwitch2D3DNoGPUFallback`, and runnable `ExampleManager_switch2D3D`.
- Verified with `gofmt`, `go run ./cmd/gameschema -check`, `go tool nsgo check -target macos,windows,linux,android,web`, `go tool nsgo test ./scene`, `go tool nsgo test . -- -run 'TestFormatPackagesRunConformance|TestFormatsPage|TestStabilityTable'`, `go run ./tools/changelog check`, and `bun run verify-notes`.
