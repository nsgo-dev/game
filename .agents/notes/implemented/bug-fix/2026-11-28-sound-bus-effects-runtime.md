# Agent Note: Bus effect chains apply at run time for in-memory WAV sources

Status: implemented

## Problem

`project.File.ConfigureMixer` installed each `game.yaml` bus's DSP chain onto the `sound.Mixer` (`SetGroupEffects`), but nothing at run time ever ran the chain: `Music.Play` handed the source to the player unchanged, and `Mixer.Effect` wraps an already decoded `*audio.Sound`. The only caller of `Mixer.ProcessSource` was the editor's audition, so an author heard the effect in the editor and shipped a game that played dry, with nothing reporting it (#1397, part of #1354).

## Decision

Apply the chain at the two points where a source is still in memory before it reaches the player:

- `Music.Play` runs a source holding in-memory 16-bit PCM WAV data through its group's chain (`Mixer.process`, the same processing as `ProcessSource`) before creating the `audio.Player`; `PlaySections` and `PlayWithIntro` do the same for each section.
- The new `Mixer.LoadEffect(src, group, limit, done)` does the same before `audio.LoadSound`, so short effects play with the chain applied; `Mixer.Effect` on an already decoded sound stays dry and its doc comment says so.

A source the chain cannot run on (a URI, or in-memory MP3/AAC/Ogg/non-PCM WAV data) plays unchanged, with one `log.Printf` per group saying its effects were skipped; `SetGroupEffects` re-arms a group's diagnostic. The editor's audition now hands the raw file bytes to `Music.Play` instead of pre-processing them, because `Play` applies the chain itself; it still calls `ProcessSource` up front to validate the chain can run on the file and to expose `AuditionSource`, and keeps reporting the WAV decode error for compressed files on buses with effects (see [editor audio bus mixer](../feature/2026-11-25-editor-audio-bus-mixer.md)). Docs (`sound.md`, `project.md`, `editor.md` in en and zh) now state exactly what runs at run time. Live per-voice effects on every format stay with the streaming mixer in #1398.

## Alternatives considered

- **A `Mixer.Check()`-style report listing buses whose sounds will play dry** — the mixer never sees the set of sources a game will play, so such a report could only restate the chains' configuration, not find the affected sounds. Rejected; the once-per-group log fires at the moment a source actually plays dry and names the group and the reason.
- **Decoding compressed sources to PCM so every format takes the chain** — that is the streaming mixer's job (#1398); doing it here would decode whole music tracks into memory and duplicate the platform's decoders. Rejected for a size/S fix.
- **Playing nothing (or failing `Play`) when a bus has effects the source cannot take** — surfaces the problem loudly. Rejected because silently dropping a game's music is worse than playing it dry; the log line carries the diagnostic instead.

## Consequences

- **Benefits**:
  - Bus effects from `game.yaml` are heard in shipped games for in-memory WAV music and for effects loaded through `LoadEffect`; the editor's audition and the runtime now run the same chain at the same point.
  - Compressed or streamed sources degrade to dry playback with a one-line-per-group diagnostic instead of silence or an unnoticed no-op.
- **Costs and known limits**:
  - Chains still apply only to in-memory 16-bit PCM WAV; compressed music plays dry until the streaming mixer (#1398).
  - `Music.Play` on a WAV with effects decodes, processes and re-encodes the whole buffer on each `Play` call.
  - The diagnostic goes through the standard `log` package, so a game that routes logs nowhere sees no warning.

## Verification

- `go tool nsgo test ./pkg/sound/ ./pkg/editor/ ./pkg/project/` — `TestMusicPlayAppliesBusChainToWAVSource` asserts the player receives the processed WAV bytes, `TestLoadEffectAppliesBusChainToWAVSource` asserts `LoadEffect` loads the processed source, and `TestBusChainSkipsNonWAVSourcesWithOneDiagnosticPerGroup` asserts URI and non-WAV data sources play dry with one diagnostic per group, re-armed by `SetGroupEffects`.
- `go tool nsgo check`
- `go run ./tools/changelog check && bun run verify-notes`
