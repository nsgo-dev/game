# Agent Note: Sample-accurate intro-and-loop music via PlayLooped and pre-scheduled PlaySections handoff

Status: implemented

## Problem

`Music.PlaySections` and `Music.PlayWithIntro` implemented intro-then-loop music by running two `audio.Player` instances and starting the preloaded loop player inside the intro's `OnEnd` callback. Because NSGo dispatches player callbacks on the engine goroutine in a later turn than the native event, the loop began at least one engine turn plus native player startup after the intro's last sample, leaving an audible gap or click at the seam. NSGo already supports single-player sample-accurate looping via `audio.PlayerOptions.LoopStart`, but `Music` had no way to pass it, and neither `PlaySections` nor `SectionedTrack` was documented in `docs/en/sound.md` or `docs/zh/sound.md` (#1497).

## Decision

- **Add `Music.PlayLooped(src audio.Source, loopStart, fade time.Duration)`**: opens a single `audio.Player` with `Loop: true` and `PlayerOptions.LoopStart: max(0, loopStart)`, so the first pass plays from `0` and subsequent passes loop back to `loopStart` inside the native player with no handoff seam. It applies the group's bus effect chain (`Mixer.process`), crossfades and pauses identically to `Play`, stops over `fade` on an empty source, and leaves `Music.Loop` unchanged for later `Play` calls.
- **Pre-schedule two-file `PlaySections` handoff ahead of `OnEnd`**: keep `PlaySections` and `PlayWithIntro` for split assets, poll intro progress at `sectionProgressInterval` (16 ms), and arm/tighten a `core.Scope.AfterFunc` timer from `OnLoad`/`OnProgress` to trigger `handoff` `min(sectionHandoffLead, dur/2)` (20 ms) before the intro's reported duration. The timer is cancelled on pause, mute, background, suspend, buffering, error, stop, or track replacement, and re-armed on resume. After an early handoff, the finishing intro stays linked as `next.prev` so volume, fade, pause, and `Stop` apply to both players until the intro's `OnEnd` closes it. When duration is unknown (`0`), `OnEnd` remains the fallback.
- **Document both paths**: `docs/en/sound.md` and `docs/zh/sound.md` add an intro-and-loop section recommending single-file `PlayLooped` and explicitly documenting the potential cross-player seam of two-file `PlaySections` / `PlayWithIntro`.

## Alternatives considered

- **Adding a `LoopStart` field to `SectionedTrack` instead of `Music.PlayLooped`** — avoids adding a new method on `Music`. Rejected because `SectionedTrack` has both `Intro` and `Loop` source fields, making a single-file `LoopStart` representation ambiguous (which field holds the track?) and risking breakage for unkeyed struct literals, whereas `PlayLooped(src, loopStart, fade)` directly mirrors `Play(src, fade)` with one source.
- **Closing the intro immediately on pre-scheduled handoff in `PlaySections`** — simpler state with no `prev` tail pointer. Rejected because closing the intro 20 ms early clips its final samples; keeping `intro` linked as `next.prev` until `OnEnd` preserves the full intro while ensuring `Stop`, fades, and pauses still close or pause it cleanly.
- **Replacing two-file `PlaySections` entirely with `PlayLooped`** — removes the imperfect two-player code path. Rejected because existing games and third-party asset packs still ship separate intro and loop files, and breaking `PlaySections` / `PlayWithIntro` would regress callers that cannot merge their audio assets.

## Consequences

- **Benefits**:
  - Games using a single intro+loop file get sample-accurate, native-player looping via `PlayLooped` with full crossfade, mixer volume, pause/background/suspend, and bus DSP support.
  - Existing two-file `PlaySections` / `PlayWithIntro` callers avoid the post-`OnEnd` turn delay whenever the backend reports duration, while retaining backward compatibility and clean cancellation/pause behavior.
  - Both English and Chinese sound guides now cover intro-and-loop playback and clearly steer authors toward `PlayLooped`.
- **Costs and known limits**:
  - Two-file `PlaySections` still coordinates two `audio.Player` instances across engine turns, so a small seam or brief overlap remains possible under load or on web backends with variable media-element start latency.
  - Streams with unknown duration (`Duration == 0`) in `PlaySections` still fall back to `OnEnd`.

## Verification

- `gofmt -l pkg/sound/`
- `go tool nsgo test ./pkg/sound/...` — verifies `PlayLooped` single-player `Loop`/`LoopStart` wrapping, crossfades, stop, pause/suspend/background, negative/empty edge cases, WAV bus effects, and `PlaySections` pre-scheduled handoff and tail pause/stop handling.
- `go tool nsgo check -target macos_arm64,linux_x64,windows_x64 -jobs 2 ./pkg/sound`
- `(cd tools && go run ./changelog check)` and `bun run verify-notes`
