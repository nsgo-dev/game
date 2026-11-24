# Agent Note: Sound pause and resume with game loop

Status: implemented

## Problem

`game/sound.Mixer` previously modeled only two silencing states: `background` (driven by `core.Env.Lifecycle() == core.LifecycleBackground`) and `Suspend()` (nestable all-group holds for cutscenes). Neither supports a pause menu:

- Calling `Suspend()` when opening a pause menu silences UI click sounds and pause-menu music along with gameplay audio.
- Manually setting group volumes to `0` overwrites the player's persisted `core.Storage` volume settings or requires duplicating volume-restore logic outside the mixer, and can desynchronize when backgrounding occurs while the pause menu is open.
- `game.Loop` had no notification hook when `Loop.Paused` changed, so games had to remember to mirror every `Loop.SetPaused` or direct `Loop.Paused` edit into `sound.Mixer`.

## Decision

Separate game pause from app backgrounding and cutscene suspension inside `sound.Mixer`, and add a zero-dependency `OnPause` callback on `game.Loop` so the two stay in sync without coupling their packages:

1. **Per-group pause exemption and game-pause state on `sound.Mixer`** (`packages/game/sound/sound.go`):
   - `Mixer.SetPaused(paused bool)` / `Mixer.Paused() bool` toggles gameplay pause (`m.paused`).
   - `Mixer.SetPauseExempt(group string, exempt bool)` / `Mixer.PauseExempt(group string) bool` marks groups (such as `"ui"`) that keep playing while `m.paused` is `true`. No group name is hardcoded as exempt by default, matching `Mixer`'s explicit group-name model (`SetVolume`, `SetDefaultVolume`, `SetGroupEffects`).
   - `Mixer.gain(group)` enforces strict precedence: `m.background || m.suspends > 0` returns `0` for **every** group (including pause-exempt groups); otherwise `m.paused && !m.pauseExempt[group]` returns `0`; otherwise it returns `Master() * Volume(group)`.
   - `Mixer.apply()` silences live effect playbacks in zero-gain groups via `audio.Playback.SetVolume(0)` without calling `Stop()`, drops new `Effect.Play` calls while gain is `0`, and pauses/resumes `Music` (`audio.Player`) at its exact playback position.

2. **Decoupled loop sync via `game.Loop.OnPause`** (`packages/game/loop.go`):
   - `Loop.OnPause func(paused bool)` runs whenever `Loop.Paused` changes: synchronously inside `Loop.SetPaused`, on the next frame (or same frame if mutated in `Update`) when `Loop.Paused` is edited directly, and once on `Loop.Run` if `Loop.Paused` starts `true`.
   - Backgrounding does **not** invoke `OnPause`. Both `Loop.Run` and `sound.NewMixer` subscribe to `cx.Env().Lifecycle() == core.LifecycleBackground`, so backgrounding pauses and resumes simulation and all audio in the same reactive flush while preserving an open pause menu (`m.paused` stays `true` across background/foreground transitions, so returning to foreground restores only pause-exempt groups until the pause menu closes).

## State transition contract

| `background` | `suspends > 0` | `paused` | `pauseExempt[group]` | Effective `gain(group)` |
| --- | --- | --- | --- | --- |
| `true` | any | any | any | `0` (all groups silenced/paused) |
| `false` | `true` | any | any | `0` (all groups silenced/paused) |
| `false` | `false` | `true` | `false` | `0` (gameplay silenced/paused) |
| `false` | `false` | `true` | `true` | `Master() * Volume(group)` |
| `false` | `false` | `false` | any | `Master() * Volume(group)` |

Ordering invariants:

- `Mixer.SetPauseExempt` **must** call `m.apply()` immediately when `m.paused` is `true` and the flag changed, so toggling exemption mid-pause takes effect without waiting for another state change.
- `Loop.syncPause` **must** record `l.lastPaused = l.Paused` before invoking `l.OnPause(l.Paused)` so re-entrant `SetPaused` calls cannot recurse infinitely, and `loopRun.frame` **must** abort the frame if `OnPause` calls `stop()`.

## Alternatives considered

### Why not add a `Mixer` field or `BindLoop(*game.Loop)` method coupling `game` and `game/sound`?

Strongest argument: `mixer.BindLoop(loop)` or `loop.Mixer = mixer` would let callers connect loop and mixer with a single method call without manually wiring a callback, and could also handle loop-level lifecycle inside one place.

Rejected because both `game` and `game/sound` are Stable packages with lean dependency trees: making `game` import `game/sound` would pull `nsgo/audio`, `math2d`, and the pure-Go WAV/DSP stack into every WebAssembly or mobile binary that only uses `game.Loop`. Making `game/sound` import `game` would work, but `Loop.OnPause func(paused bool)` matches `Loop.FrameTime` (used by `quality.Selector`), allows direct method-value assignment (`OnPause: mixer.SetPaused`), works identically for other subsystems (particles, timers, HUD), and keeps `sound.Mixer` usable with custom loops or game-specific pause controllers (such as `starburst`'s `Arena.SetPaused`).

### Why not hardcode `"ui"` as pause-exempt by default?

Strongest argument: most games name their UI sound bus `"ui"`, so exempting `"ui"` automatically would save one `SetPauseExempt("ui", true)` line in the common case.

Rejected because `sound.Mixer` has zero magic group names anywhere in its API (`SetVolume`, `SetDefaultVolume`, `SetGroupEffects`, `Effect`, `Music` all treat every group string identically). Implicitly special-casing `"ui"` would surprise games that use `"menu"` / `"hud"` or that expect `"ui"` to pause alongside other groups unless opted out.

### Why silence `audio.Playback` with `SetVolume(0)` instead of freezing its playhead?

Strongest argument: freezing a one-shot sound effect's playhead during pause and resuming at the exact sample offset would preserve mid-flight short effects even across long pauses.

Rejected as impossible on NSGo v0.3.0: `core.Playback` (`nsgo#1088`) exposes only `SetVolume`, `SetRate` (clamped to `[0.5, 2.0]`), `Stop`, and `Ended`, with no `Pause`/`Resume` or start-offset seek on `Sound.Play`. Silencing via `SetVolume(0)` matches `Mixer`'s existing `background` and `Suspend` behavior, preserves looping and still-playing effects without stopping them, and leaves `Music` (`audio.Player`) paused at its exact `Position`.

## Consequences

- **Benefit**: Games wire pause-menu audio in two lines (`mixer.SetPauseExempt("ui", true)` and `loop.OnPause = mixer.SetPaused`) while keeping persisted player volumes untouched and handling nested pause-menu + backgrounding transitions cleanly.
- **Benefit**: `game` and `game/sound` remain decoupled with zero new cross-package imports.
- **Cost**: Short non-looping `Effect` playbacks continue advancing silently in NSGo v0.3.0 while paused and finish naturally if the pause outlasts their remaining duration. **Upgrade trigger**: when a future NSGo release adds `audio.Playback.Pause()` / `Resume()`, `Mixer.apply()` can switch live playbacks from `SetVolume(0)` to playhead pause/resume internally without changing any `game/sound` public API.
- **Cost**: `Loop.OnPause` is a single callback slot (like `Loop.FrameTime`); games that need multiple pause observers compose them inside one closure.

## Verification

- `packages/game/sound/pause_test.go`:
  - `TestSetPausedSilencesGameplayEffectsAndPausesMusicAndResumes` verifies gameplay effects silence without stopping, drop new plays without evicting paused playbacks, pause/resume `Music` at its exact `Position`, and apply volume edits made while paused on resume.
  - `TestMusicStartedOrSectionedWhilePausedWaitsForResume` verifies deferred playback and sectioned intro/loop music under pause.
  - `TestPauseExemptGroupsKeepPlayingWhilePaused` verifies `"ui"` effects and music keep playing while paused, respond immediately to mid-pause exemption toggles, and remain silenced by `Suspend()`.
  - `TestBackgroundPausesAndResumesInSyncWithLoop` and `TestLoopStartingPausedInBackgroundSyncsMixerOnForeground` verify `game.Loop` + `sound.Mixer` sync across active backgrounding, `LifecycleInactive`, pause-menu + backgrounding nesting, and paused startup in background.
- `packages/game/loop_test.go`:
  - `TestOnPauseFollowsSetPausedDirectEditsAndInitialPaused` and `TestOnPauseFromUpdateAndStopInsideOnPause` verify `Loop.OnPause` delivery, idempotency, direct `Paused` edits, background non-invocation, and stopping from inside `OnPause`.
- Checked with `gofmt`, `go run ./cmd/gameschema -check`, `go tool nsgo check -target macos,windows,linux,android,web`, `go tool nsgo test ./sound`, `go tool nsgo test .`, `go run ./tools/changelog check`, and `bun run verify-notes`.
