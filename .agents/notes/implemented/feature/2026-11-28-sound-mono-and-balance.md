# Agent Note: Mono Audio and Left/Right Balance in game/sound

Status: implemented

## Problem

Positional audio in `pkg/sound` (`Listener2D.Pan`, `Listener3D.Pan`, `Effect.PlayAt`, `Effect.PlayAt3D`, `Effect.Play3D`, and `Cue` variants) pans voices across `[-1, 1]` with no player-facing mono switch or left/right balance control (#1781). A player who hears with one ear misses sounds panned to the other side, and games cannot cleanly override panning from outside `pkg/sound`. Meanwhile, NSGo v0.3.4 does not yet expose output stereo-to-mono down-mixing (`nsgo-dev/nsgo#2676`), live `audio.Playback.SetPan`, or a platform mono-audio environment signal (`nsgo-dev/nsgo#2677`).

## Decision

Add mono voice panning and stereo left/right balance end-to-end across `pkg/sound`, `pkg/save`, and `pkg/gameui` while preserving geometric listener directions for captions and preparing forward-compatible hooks for upcoming NSGo APIs:

### Cross-package contract (`pkg/sound`, `pkg/save`, `pkg/gameui`)

- **`sound.Mixer` (`pkg/sound/sound.go`, `pkg/sound/positional.go`, `pkg/sound/positional3d.go`)**:
  - Adds `SetMono(bool)`, `Mono() bool`, `FollowSystemMono()`, `SetBalance(float32)`, and `Balance() float32`.
  - Every voice started through `Effect.Play` (including `Effect.PlayAt`, `Cue.Play`, `Cue.PlayAt`, `Effect.PlayAt3D`, `Cue.PlayAt3D`) or `Effect.Play3D` (and `Cue.Play3D`) runs its requested pan through `Mixer.effectivePan`: pan is `0` while `Mono()` is `true`, and `clamp(basePan + Balance(), -1, 1)` otherwise.
  - Distance, rear head-shadow, and source-cone attenuation are unchanged under mono, and `Listener2D.Pan`, `Listener3D.Pan`, `Listener3D.Spatialize`, and `Playback3D.Pan()` continue to return the listener's true geometric direction so directional captions (`a11y.CaptionTrack.ShowSFXAt`, #1774) preserve directional cues even under mono.
  - `NewMixer` watches `cx.Env()` inside its existing lifecycle `cx.Effect` and reads `envMonoAudio(env)` via an optional `interface{ MonoAudio() bool }` check, so follow-system mono activates automatically once `nsgo-dev/nsgo#2677` ships (returning `false` on NSGo v0.3.4). Likewise, `Mixer.apply`, `Mixer.SetListener3D`, and `Playback3D.refresh` store each live playback's base pan and forward `effectivePan` through an optional `interface{ SetPan(float32) }` check so live playbacks update automatically once NSGo exposes `Playback.SetPan`.
- **`save.AudioSettings` (`pkg/save/settings.go`)**:
  - Adds `Mono *bool` (`json:"mono,omitempty"`, `nil` by default for follow-system, matching the tri-state pattern in `pkg/project` and #1771) and `Balance float64` (`json:"balance,omitempty"`, clamped to `[-1, 1]` by `Settings.Validate`, `NaN` reset to `0`), plus `AudioSettings.ResolveMono(systemMono bool) bool`.
  - `AudioSettings.UnmarshalJSON` preserves omitted defaults, treats explicit JSON `null` as `nil` (follow-system), and decodes `true`/`false` into distinct pointers; `cloneSettings` deep-copies `Mono` to prevent caller pointer aliasing.
  - `save.Apply` wires `Audio.Mono` (`SetMono(*m)` when non-nil, `FollowSystemMono()` when nil) and `Audio.Balance` (`SetBalance`) to `SettingsTargets.Sound`.
- **`gameui.SettingsMenu` (`pkg/gameui/settings_menu.go`)**:
  - Adds **Mono Audio** (three-state cycle button: `System Default` -> `On` -> `Off`) and **Balance** (`Slider` over `[-1, 1]` with `0.1` step and localized `L` / `Center` / `R` readout) rows to `SettingsTabAudio` with corresponding `SettingsText` fields. See [Ready Settings Menu in game/gameui](2026-11-24-gameui-settings-menu.md).

## Alternatives considered

### Applying Balance on top of Mono (`0 + balance`)

Strongest argument: on iOS and Android, enabling Mono Audio collapses stereo channels together while the Balance slider can still shift the centered mono sum slightly toward a stronger ear.

Rejected because #1781 specifies that every voice gets pan `0` under mono and a balance offset otherwise; keeping mono strictly at pan `0` guarantees mono always centers voices regardless of any previously configured balance while retaining `Balance()` for when mono is turned off (parallel to `AudioSettings.Muted` preserving `AudioSettings.Master`).

### Storing `AudioSettings.Mono` as a plain `bool` or string enum (`"auto"` / `"on"` / `"off"`)

Strongest argument: a plain `bool` avoids pointer handling, while a string enum matches `DisplaySettings.Quality`.

Rejected: a plain `bool` cannot distinguish "unset (follow platform switch)" from "explicitly off" as required by #1781 and #1771, and `*bool` with `omitempty` is the repository's established tri-state boolean convention (`pkg/project`, `pkg/sceneformat`, `pkg/gameui/screen_format.go`) with native JSON `true` / `false` / `null` values.

### Down-mixing stereo PCM WAV assets inside `Mixer.ProcessSource`

Strongest argument: averaging stereo WAV channels to mono inside `ProcessSource` would collapse in-memory WAV music right away.

Rejected because `ProcessSource` only runs on in-memory 16-bit PCM WAV buffers at load time, would not cover streamed `.m4a`/`.mp3` music or live toggles without reloading assets, and #1781 explicitly defers stereo asset down-mixing to NSGo output down-mixing (`nsgo-dev/nsgo#2676`).

## Consequences

- **Benefits**: Every positional and direct effect played through `sound.Mixer` honors mono centering and left/right balance immediately, persists across launches via `save.DefaultSettingsStore`, is editable via mouse, touch, keyboard, and gamepad in `gameui.SettingsMenu`, and preserves listener direction for directional captions.
- **Costs**: Callers constructing `save.AudioSettings` in Go with an explicit mono override pass a `*bool` (`new(true)` / `&v`). Stereo music and ambience files keep their channels until `nsgo-dev/nsgo#2676` lands, and OS mono-audio detection stays `false` until `nsgo-dev/nsgo#2677` lands (both documented in `docs/en` and `docs/zh` `sound.md` and `a11y.md`).
