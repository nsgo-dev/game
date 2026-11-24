# Agent Note: 3D positional audio in game/sound

Status: implemented

## Problem

`game/sound` previously provided only 2D positional audio (`Listener2D`, `Effect.PlayAt`, `Cue.PlayAt`), which projects offsets onto a 2D plane using `math2d.Vec2` and fixes pan and volume once at playback start. With 3D cameras landed in `game/scene3d` and `game/render3d` (#9), 3D games need spatial panning, 3D distance attenuation, rear head-shadow attenuation (an effect behind the camera must play quieter than an equidistant effect in front while remaining panned to its left or right side), optional directional source cones, and live volume tracking when the camera or sound source moves — without violating `game/sound`'s Stable package boundary or `TestStabilityTable`.

## Decision

Add 3D positional audio directly to `game/sound` (`packages/game/sound/positional3d.go`) using a layout-compatible `sound.Vec3` value type and integrating live `Playback3D` handles into `Mixer`'s existing `Effect.live` playback table:

1. **Zero-import stability boundary via `sound.Vec3`**:
   - `game/sound` is Stable (`docs/en/stability.md`), whereas `game/math3d`, `game/scene3d`, and `game/render3d` are Experimental; `TestStabilityTable` forbids any Stable package from importing an Experimental package.
   - Define `type Vec3 struct { X, Y, Z float32 }` and `V3(x, y, z float32)` in right-handed Y-up coordinates (`+X` right, `+Y` up, `-Z` forward), matching `math3d.Vec3` and `physics3d.Vec3` field-for-field so callers convert camera and entity vectors with zero-cost `sound.Vec3(cam.Pos)`, `sound.Vec3(cam.Forward())`, and `sound.Vec3(cam.Up())`.

2. **3D spatialization math (`Listener3D`, `Spatial3DOptions`, `Spatial3D`, `RolloffExponential`)**:
   - Build a guaranteed-finite, right-handed orthonormal frame `(right, up, forward)` in `float64` (`right = normalize(cross(forward, up))`) with deterministic fallbacks for zero, non-finite, or collinear `Forward`/`Up` vectors (e.g., looking straight along `±Y`).
   - Compute overflow- and underflow-safe 3D Euclidean distance and unit direction (`offset3`), guaranteeing zero `NaN` outputs for coinciding listener/source positions, directly overhead/underfoot sources, or non-finite coordinates.
   - Share distance attenuation via `rolloffGain` across `RolloffLinear`, `RolloffInverse`, and the new `RolloffExponential` (default exponent 2, rescaled to reach `1` at `Near` and `0` at `Far`, configurable via `RolloffFactor`).
   - Apply smooth rear head-shadow attenuation when `dot(dir, forward) < 0`: multiplier interpolates linearly from `1` at the lateral plane down to `RearGain` directly behind (`DefaultRearGain = 0.7` when omitted/non-positive/NaN; `RearGain >= 1` disables it).
   - Compute stereo pan as `clamp(dot(dir, right), -1, 1)` by default (or lateral offset divided by `PanWidth` when `PanWidth > 0 && finite`), keeping sounds directly ahead, behind, overhead, or underfoot centered at `0`.
   - Support optional directional emission cones (`SourceForward`, `InnerConeAngle`, `OuterConeAngle`, `OuterGain`) and per-source distance overrides in `Spatial3DOptions`.

3. **Stateless and live 3D playback on `Mixer`, `Effect`, and `Cue`**:
   - Mirror 2D fire-and-forget APIs with `Effect.PlayAt3D` and `Cue.PlayAt3D`.
   - Add `Mixer.SetListener3D` / `Mixer.Listener3D`, `Effect.Play3D`, `Cue.Play3D`, and `Playback3D` (`SetPosition`, `SetOptions`, `Spatial`, `Gain`, `Pan`, `SetRate`, `Stop`, `Ended`, `Playing`).
   - Store each live `*Playback3D` on its entry in `Effect.live` (`playback.spatial`) so `Mixer.apply()`, `Effect.Limit` eviction, `SetPaused` / `SetPauseExempt`, `Suspend`, `LifecycleBackground`, and `SetVolume` / `SetMaster` all apply the exact product `baseVoiceVolume * spatial.Gain * mixerGroupGain` through a single code path.

## Spatialization and mixer invariants

- `Listener3D.Pan(pos)` **must** always return a finite value in `[-1, 1]`, and `Listener3D.Gain(pos)` **must** always return a finite value in `[0, 1]`, for every `float32` input including `NaN`, `±Inf`, zero distance, and collinear basis vectors.
- `Listener3D.Voice` **must** set `Muted = true` whenever the resulting volume is `0`, because `nsgo/audio` treats an unmuted `Volume: 0` as full volume.
- `Effect.Play3D` and `Effect.PlayAt3D` **must** skip starting a backend voice (and must not evict older playbacks under `Effect.Limit`) when initial effective gain is `<= 0` (beyond `Far`, outside a zero-gain cone, muted, paused, suspended, backgrounded, or zero group/master volume).
- `Mixer.SetListener3D` and `Playback3D.SetPosition` / `SetOptions` **must** multiply by `m.gain(group)` when calling `audio.Playback.SetVolume`, so moving the camera or a source while the mixer is paused, suspended, or in the background never un-silences a silenced playback.

## Alternatives considered

### Why not import `game/math3d` or `game/scene3d` in `game/sound`?

Strongest argument: taking `math3d.Vec3` and `*scene3d.Camera` directly in `Listener3D` avoids a `sound.Vec3(...)` conversion at call sites and provides a one-line `Listener3DFromCamera(cam)` constructor.

Rejected because `game/sound` is a Stable package while `game/math3d`, `game/scene3d`, and `game/render3d` are Experimental, so importing any of them fails `TestStabilityTable` and violates the stability guarantee in `docs/en/stability.md` (it would also pull 3D math/scene code into `wasmprobe`'s minimal 2D web size budget). Defining `sound.Vec3` with identical `X, Y, Z float32` fields matches `physics3d.Vec3` and keeps conversion to a zero-cost type cast.

### Why not create a separate `game/sound3d` Experimental package?

Strongest argument: an experimental `game/sound3d` package could import both `game/sound` and `game/scene3d` directly without touching `game/sound`'s API surface.

Rejected because live 3D playbacks must integrate directly with `Mixer`'s private playback list (`Effect.live`), `Mixer.apply()`, `Effect.Limit` eviction, and pause/suspend/background state (see [sound pause note](2026-11-24-sound-pause-with-game.md)), and `Listener2D` already lives in `game/sound` alongside `Mixer`, `Effect`, and `Cue`. Splitting 2D and 3D listeners across packages would either duplicate voice tracking or expose internal `Mixer` hooks.

### Why not update stereo pan live on `audio.Playback` when the camera rotates?

Strongest argument: continuously rotating a 3D camera around a looping ambient sound should pan the active playback smoothly from left to right in the audio backend.

Rejected as unsupported in NSGo v0.3.0: `core.Playback` provides only `SetVolume`, `SetRate`, `Stop`, and `Ended`, with no `SetPan` method. `Playback3D` updates live volume continuously via `SetVolume` and tracks the latest computed pan in `Playback3D.Pan()`.

## Consequences

- **Benefit**: 3D games get full spatialization (distance rolloff, rear head-shadow attenuation, directional cones, and live camera/source tracking) in `game/sound` with zero new package imports, zero allocations on the hot `Listener3D.Voice` path, and full compatibility with `Mixer`'s pause, suspend, background, group volume, and voice limit mechanics.
- **Benefit**: `game/sound` remains Stable with `TestStabilityTable` passing and `sound.Vec3` directly convertible from `math3d.Vec3`.
- **Cost**: Callers with `math3d.Vec3` write `sound.Vec3(v)` at the boundary rather than passing `math3d.Vec3` directly. **Upgrade trigger**: if `game/math3d` is promoted to Stable in a future release, a type alias `type Vec3 = math3d.Vec3` can make `sound.Vec3` and `math3d.Vec3` identical without breaking any existing `sound.Vec3(v)` conversions.
- **Cost**: Active `Playback3D` instances keep their initial backend stereo pan on NSGo v0.3.0 while updating distance, rear, and cone volume live. **Upgrade trigger**: when a future NSGo release adds `audio.Playback.SetPan(float32)`, `Mixer.SetListener3D` and `Playback3D.refresh` can call `p.playback.SetPan(p.spatial.Pan)` internally with no public API change.

## Verification

- `packages/game/sound/positional3d_test.go`:
  - `TestEffectBehindCameraIsQuieterAndPannedBySide` verifies issue #106 acceptance criteria end-to-end with a real `scene3d.Camera`, proving behind-camera effects are quieter than equidistant front effects and panned accurately to their left/right side.
  - `TestListener3DPanAndOrientation`, `TestListener3DGainAndRolloffModels`, `TestSpatial3DSourceConeAndOverrides`, and `TestListener3DSetPoseAndLookAt` cover linear/inverse/exponential rolloff, rear attenuation, directional source cones, rotated cameras, overhead/underfoot sources, collinear fallback basis, and NaN/Inf edge cases.
  - `TestMixerLivePlayback3DTracksListenerAndSourceMovement`, `TestPlayback3DRespectsMixerVolumePauseSuspendAndBackground`, `TestPlay3DRespectsLimitAndSkipsSilentSources`, and `TestCuePlayAt3DAndPlay3D` verify live `Playback3D` tracking under `nsgotest` alongside `Mixer` volume, pause, pause-exempt, suspend, background, and `Effect.Limit` rules.
  - `FuzzListener3D` fuzzes arbitrary `float32` listener/source parameters for NaN-free bounded invariants, and `BenchmarkListener3DVoice` verifies zero allocations per call.
- `packages/game/sound/example_test.go`: `ExampleListener3D` provides a runnable example.
- Verified with `gofmt`, `go run ./cmd/gameschema -check`, `go tool nsgo check -target macos,windows,linux,android,web`, `go tool nsgo test ./sound`, `go tool nsgo test .`, `go run ./tools/changelog check`, and `bun run verify-notes`.
