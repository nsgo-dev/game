# Agent Note: Shared Sound Concurrency Groups, Priority, Retrigger Time, and Ducking

Status: implemented

## Problem

Voice limiting in `pkg/sound` previously existed only per individual `Effect` (`Effect.Limit`), always stopping the oldest playback of that single sound. There was no way to share a concurrency limit across multiple sounds (such as different impact effects in one explosion), choose a voice-stealing policy other than oldest, scope limits per emitter/owner, enforce a minimum retrigger interval, fade out stolen voices, duck older overlapping voices or whole buses (such as lowering `music` while `dialogue` plays), or protect high-priority cues (dialogue, player gunfire) when approaching NSGo's 32-voice platform limit (#1731).

## Decision

Add shared concurrency groups, priority-based global voice budgeting, retrigger cooldowns, steal fade-outs, older-voice group ducking, bus ducking, `game.yaml` configuration, `game check` validation, and debug/MCP readouts across `pkg/sound`, `pkg/project`, `pkg/tooling/assetcheck`, `pkg/tooling/gamedebug`, and `pkg/mcp`.

### Runtime contract (`pkg/sound`)

- **Shared concurrency groups (`ConcurrencyGroup`, `Mixer.SetConcurrencyGroup`)**:
  - `Effect.Concurrency` / `CueOptions.Concurrency` / `PlayOptions.Concurrency` bind playbacks to a named group configured on `Mixer`.
  - Resolution order on every `Effect.Play` / `PlayWith` / `PlayAt` / `PlayAtWith` / `PlayAt3D` / `PlayAt3DWith` / `Play3D` / `Play3DWith` (and matching `Cue` methods) must follow a strict 4-stage pipeline before touching active voices:
    1. **Retrigger cooldown** (`ConcurrencyGroup.Retrigger`, measured via `cx.Now()`): calls arriving sooner than `Retrigger` after the last accepted start in the group (or owner) are refused immediately without stealing active voices.
    2. **Concurrency group cap & resolution** (`Max`, `Resolve`: `ResolveRefuse` default, `ResolveOldest`, `ResolveFarthest`, `ResolveQuietest`, `ResolveLowestPriority`): evaluates all currently active voices in the group (or owner bucket for `ScopeOwner`) before per-effect `Effect.Limit`, so `Effect.Limit` can never bypass `ResolveRefuse`. Ties in `ResolveLowestPriority` break by oldest monotonic sequence number; strictly worse incoming sounds are refused.
    3. **Per-effect `Effect.Limit`**: excludes voices already scheduled to be stolen by the concurrency group, then evicts oldest remaining playbacks of that effect.
    4. **Global voice budget (`Mixer.MaxVoices`, default `DefaultMaxVoices = 30`)**: kept below NSGo's 32-voice platform limit so brief `StealFade` tails never trigger hard backend eviction. When full, steals the lowest-priority remaining active voice across all groups (ties broken by oldest), refusing strictly lower-priority sounds.
- **Steal fade-out (`ConcurrencyGroup.StealFade`)**: stolen voices move out of `Effect.live` into `Mixer.fading` so they immediately free active concurrency slots while ramping to silence via `cx.Animate` before calling `audio.Playback.Stop()`. If live + fading voices ever reach 32, the oldest fading voice is hard-stopped first.
- **Ducking (`DuckEnvelope`, `BusDuckRule`, `Mixer.SetBusDucking`)**:
  - `Amount` is a reduction fraction in `[0, 1]` (`0` = no ducking / `1.0x` gain, `0.4` = 40% reduction / `0.6x` gain, `1.0` = full silence / `0x` gain), ensuring Go and YAML zero values never accidentally mute audio.
  - Concurrency-group ducking scales older active voices in the same scope by `(1 - Amount)^newer` with linear `Attack` and `Release` animations via `cx.Animate`.
  - Bus ducking (`BusDuckRule{Bus, By, Amount, Attack, Release}`) triggers whenever any active `Effect` (matching `Effect.Group == By` or `Concurrency == By`) or active `Music` (matching `Music.Group == By`) plays. Multiple rules targeting the same `Bus` multiply together. For `Music`, bus ducking scales the track level rather than pausing the `audio.Player`, so `Amount: 1.0` mutes music while keeping its playhead in sync.
  - Voices participating in group or bus ducking attach a reactive `cx.Effect` watching `audio.Playback.Ended()` (cleaned up on end or `Stop`), so release envelopes start deterministically as soon as a voice finishes naturally.
- **Owner safety**: `PlayOptions.Owner` accepts any value (`any`) and normalizes uncomparable values to the shared `nil` bucket so callers using `entities.Entity`, strings, integers, or pointers work without reflection panics.

### Project schema, validation, and observability (`pkg/project`, `pkg/tooling/assetcheck`, `pkg/tooling/gamedebug`, `pkg/mcp`)

- **`game.yaml` (`pkg/project`)**: extends `Audio` with `maxVoices`, `concurrency:` (`name`, `max`, `resolve`, `scope`, `retrigger`, `stealFade`, `duck: {amount, attack, release}`), and `duck:` (`bus`, `by`, `amount`, `attack`, `release`), all validated with `*loaderr.Error` field paths and applied by `File.ConfigureMixer`. `website/public/schemas/game.v1.json` is regenerated via `go run ./tools/gameschema`.
- **`game check` (`pkg/tooling/assetcheck`)**: scans `*.sound.yaml` / `*.sound.yml` files for top-level `concurrency:` references and reports unknown group names against the nearest `game.yaml`'s `audio.concurrency` with exact file and line numbers (forward-compatible with #1401's sound resource files).
- **Debug overlay & MCP (`pkg/tooling/gamedebug`, `pkg/mcp`)**: `Mixer.DebugSnapshot()` returns sorted deterministic telemetry (`ActiveVoices`, `FadingVoices`, `MaxVoices`, `Groups`, `Buses`), surfaced in `gamedebug.Overlay` via `gamedebug.Options.Sound` (zero-cost stub in `-tags release`) and over MCP via `game_sound_readout` (`mcp.Config.Sound`, `SoundFunc`, or `Overlay.Sound()`).

## Alternatives considered

### Expressing `Duck.Amount` as a target gain instead of a reduction fraction

Strongest argument: target gain (`0.6` means play at 60%) avoids subtracting from `1` when computing multipliers.

Rejected because zero-value `DuckEnvelope{}` in Go and omitted `amount` in YAML must mean "no ducking" (`1.0x`), not silence (`0.0x`), and #1731 specifically defines ducking as lowering volume by an amount (`amount: 0.4`).

### Polling active playbacks on a fixed timer to trigger ducking release

Strongest argument: a periodic timer avoids creating a reactive watcher per ducking-relevant playback.

Rejected because polling introduces release latency, wastes idle wakeups when nothing plays, and couples tests to poll intervals. Watching `audio.Playback.Ended()` via `cx.Effect` only for voices that participate in group or bus ducking fires immediately when a playback ends naturally and cleans itself up with zero leaked timers.

### Deferring `game check` sound group validation until #1401 lands

Strongest argument: `*.sound.yaml` does not yet have a `pkg/resource` parser.

Rejected because #1731 explicitly requires `game check` to validate concurrency group names used by sound files. Inspecting the YAML node tree for `concurrency` in `*.sound.yaml` / `*.sound.yml` delivers line-accurate validation immediately without conflicting with #1401's upcoming schema fields.

## Consequences

- **Benefits**: Multiple `Effect`s and `Cue`s can share concurrency caps, resolution rules, retrigger intervals, steal fades, and ducking envelopes driven from `game.yaml` or Go; high-priority sounds stay protected under global voice pressure; music smoothly ducks under dialogue on all existing native and web backends without waiting for #1398's live sidechain DSP graph; developers and agents can inspect voice counts and ducking live in `gamedebug.Overlay` and MCP `game_sound_readout`.
- **Costs**: `Effect.PlayWith` / `PlayAtWith` / `PlayAt3DWith` / `Play3DWith` and matching `Cue` methods expand `pkg/sound`'s stable API surface to carry `PlayOptions` without breaking existing `Play*` signatures. Bus ducking applies via existing mixer group gain multipliers until #1398 introduces live submix sidechain DSP processing.
