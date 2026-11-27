# Agent Note: Editor audio bus mixer over game.yaml with live ConfigureMixer audition

Status: implemented

## Problem

`game.yaml`'s `audio` section defines the game's master volume, named audio buses, per-bus volumes, and per-bus DSP effect chains (`lowPass`, `highPass`, `delay`, `reverb`, `gain`, `bitcrush`), which runtime games apply through `project.File.ConfigureMixer` onto a `sound.Mixer`. Authors previously had to edit `game.yaml` by hand and launch the game before hearing volume balances or DSP effect changes.

## Decision

Add a dedicated Audio Bus Mixer panel in `packages/game/editor/audio_mixer_panel.go` wired into `EditorView` via a toolbar **Mixer** toggle button (`toolbar-audio-mixer`), with minimal hooks in `state.go`, `project.go`, `play.go`, and `ui.go`:

- **All mutations go through `Editor.EditProject`**: Master volume (`SetAudioMasterVolume`), bus CRUD and reordering (`AddAudioBus`, `RenameAudioBus`, `DeleteAudioBus`, `MoveAudioBus`, `SetAudioBusVolume`), and per-bus effect chain CRUD and reordering (`AddAudioBusEffect`, `AddAudioBusEffectKind`, `SetAudioBusEffect`, `RemoveAudioBusEffect`, `MoveAudioBusEffect`) each execute as a single validated, undoable `projectCmd`. Out-of-range parameters fail `project.Validate` cleanly without mutating the project or history; UI sliders and `ui.NumberInput` fields bound values to `project.Validate`'s exact ranges and apply `ClampAudioEffect` before committing.
- **Exact runtime parity for audition via `ConfigureMixer` + `sound.Mixer`**: The editor owns a `sound.Mixer` on its attached `core.Scope` (`"editor.audioMixer"` storage prefix, never writing to `core.Storage` so project defaults always take effect). On every project mutation, Undo/Redo, or project open, `configureAuditionMixer` resets only fields or buses that became unset or removed (avoiding spurious pause/play glitches on muted buses) and then calls `e.Project.ConfigureMixer(m)` verbatim.
- **Live volume updates vs. DSP re-processing**: `AuditionAudioBus` reads the chosen project audio file (validated via `fs.ValidPath` inside the project root), processes it with `m.ProcessSource(bus, audio.Source{Data: raw})` to validate the chain can run on it and to expose `AuditionSource`, and hands the raw bytes to a single reused `*sound.Music` handle on `m`, which applies the bus chain itself as runtime `Music.Play` does (see [bus effects at run time](../bug-fix/2026-11-28-sound-bus-effects-runtime.md)). While auditioning, volume edits update the playing `audio.Player` immediately through `sound.Mixer.apply()` without restarting playback; effect chain edits (detected by fingerprinting the bus's `Effects` slice in `syncAudioMixer`) immediately re-process `auditionRaw` through `ProcessSource` and swap playback so DSP changes and Undo/Redo are heard at once.

## Alternatives considered

- **Rebuilding a fresh `sound.Mixer` on every edit instead of tracking `knownBuses` and resetting cleared fields** — Creating a new `sound.Mixer` per edit guarantees no stale defaults or effect chains survive Undo or deletion. Rejected because each `sound.NewMixer` registers a `core.Scope.Effect` lifecycle subscription on the editor's scope (leaking effects across edits) and drops the live `*sound.Music` handle, interrupting continuous audition playback on every slider drag.
- **Using `sound.Effect` (`audio.LoadSound`) instead of `sound.Music` (`audio.NewPlayer`) for audition playback** — `sound.Effect` is designed for short fire-and-forget SFX clips. Rejected because `audio.LoadSound` is asynchronous and clips sounds at 5 seconds on mobile/test backends, whereas `sound.Music` opens synchronously, supports optional looping for continuous parameter tweaking, handles long music tracks as well as short SFX WAVs, and still applies `m.Master() * m.Volume(bus)` and `m.ProcessSource` identically.
- **Always showing the Audio Mixer panel whenever a project is open** — Keeps mixer controls one click closer with no toolbar toggle. Rejected because keeping a 270 pt bottom panel permanently mounted crowds smaller editor windows and breaks layout expectations when editing scenes without touching audio; toggling via the toolbar **Mixer** button matches `AnimationPanelView`'s bottom-dock behavior.

## Consequences

- **Benefits**:
  - Authors edit `game.yaml`'s `audio` section visually with full Undo/Redo, deterministic YAML persistence via `SaveProject`, and zero drift from `project.Validate`.
  - Auditioning runs through the same `ConfigureMixer` + `sound.Mixer` bus chain and gain pipeline that runtime games execute in `Music.Play`, verified byte-for-byte on processed PCM and live player volume.
  - Implementation lives in isolated `audio_mixer_panel.go` / `audio_mixer_panel_test.go` files with minimal shared-file wiring, avoiding merge conflicts with concurrent editor panels.
- **Costs and known limits**:
  - DSP effect auditioning inherits `sound.Mixer.ProcessSource`'s 16-bit PCM WAV requirement; compressed formats (`.ogg`, `.mp3`, `.m4a`) audition cleanly on dry buses at bus volume, and return a clear WAV decode error when a bus has DSP effects configured.
  - Because `audio.Player` plays pre-processed buffers, changing a DSP effect parameter during audition restarts playback from the start of the newly processed buffer, while volume changes update seamlessly in place.

## Verification

- `(cd packages/game && env -u GOROOT go tool nsgo test ./editor/... ./project/... ./sound/...)`
- `(cd packages/game && env -u GOROOT go tool nsgo test . -- -run 'TestStabilityTable|TestAgentSkillValid')`
- `(cd packages/game && env -u GOROOT go tool nsgo check -target macos,windows,linux,android,web)`
- `env -u GOROOT go run ./tools/changelog check && bun run verify-notes`
