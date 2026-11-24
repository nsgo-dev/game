# Agent Note: MCP input recording and deterministic replay

Status: implemented

## Problem

`game/replay` provides fixed-step input recording (`replay.Recorder`) and deterministic playback (`replay.Player`) over versioned YAML files, and [MCP scene, layer, entity and component inspection](2026-11-24-mcp-scene-entity-inspection.md) lets coding agents inspect a running game via `nsgo mcp`. However, agents have no MCP tools to start or stop an input recording of a running session or replay a stored or inline YAML recording back through the live simulation to reproduce exact state.

## Decision

Add `ReplayController` (configured via `ReplayConfig` on `mcp.Config.Replay`) and six `game_replay_*` automation tools in `packages/game/mcp/replay.go`, appended to `Bridge.Tools()` alongside the inspection tools. `ReplayController` itself implements `replay.ActionSource`, switching transparently between live `Source`, an active `replay.Recorder`, and an active `replay.Player` so a game wires MCP record and replay into one unchanged update loop.

## Architecture and tool contract

- **Single `ActionSource` facade (`ReplayController`)**: Games construct `ctrl := mcp.NewReplayController(cfg)`, pass `ctrl` to their simulation as its `replay.ActionSource`, and supply `Replay: ctrl` in `mcp.Config`. During live play `ctrl` forwards to `cfg.Source`; during recording it delegates to a `replay.Recorder` wrapping `cfg.Source` (or a pre-built `cfg.Recorder` fallback); during playback it delegates to a `replay.Player` created from the target `replay.Recording`.
- **Final-step playback lifecycle**: In `replay.Player.Step()`, advancing the last recorded frame (`TotalSteps - 1`) sets `Done() == true` before the simulation reads `Pressed`/`JustPressed`/`Axis` for that tick. `ReplayController` therefore keeps `player` attached through the remainder of that final step and only clears it on the subsequent `Step()` (resuming live input), on `game_replay_stop`, at the end of synchronous `run_to_end`/`steps` execution, or before starting a new record/play session. Meanwhile `game_replay_status` reports `replaying: false` and `replay_done: true` as soon as `player.Done()` becomes true so polling agents observe completion without needing an extra live step.
- **Six tools with strict JSON decoding**:
  - `game_replay_status`: returns recording/playback flags, frame counters, active names/metadata, and sorted `saved_recordings`.
  - `game_replay_record_start`: starts recording with optional `name` (auto-allocated sequentially when omitted), `seed` (falls back to `SeedFunc` or `0`), `step_rate` (falls back to `ReplayConfig.StepRate`, `Loop.Rate`, or `60`), `actions`/`axes` overrides, string `metadata`, and optional `reset: true`. Refuses starting while already recording or actively replaying.
  - `game_replay_record_stop`: validates optional `save_path` before mutating state so an invalid path never discards an active recording, encodes canonical `replay` v2 YAML via `Recording.Encode()`, stores a clone in memory under its name, optionally writes inside `ReplayConfig.SaveDir` (strictly rejecting absolute paths and `..` escapes), and returns summary fields plus the YAML text.
  - `game_replay_play`: accepts either a stored `name` or inline `yaml` (decoded and validated via `replay.Decode`), invokes `ResetFunc(rec.Seed)` by default (rolling back state if `ResetFunc` fails), and supports frame-by-frame playback (`run_to_end: false`), bounded synchronous stepping (`steps: N`), or full synchronous playback (`run_to_end: true`) using `StepFunc` or `Loop.Update`.
  - `game_replay_stop`: stops an active playback early and fires `OnReplayStop`.
  - `game_replay_list`: returns sorted summaries (`name`, `frames`, `seed`, `step_rate`, `events`, `actions`, `axes`, `metadata`) of stored recordings.
- **Pure `game/replay` v2 YAML preservation**: Optional MCP `metadata` is stored alongside the `replay.Recording` in `ReplayController`'s in-memory map rather than added to the YAML schema, keeping exported YAML 100% compatible with `replay.Decode`'s `KnownFields(true)` check and `go tool game replay`.

## Alternatives considered

### Putting `Recorder`/`Player` wiring callbacks on `mcp.Config` without implementing `replay.ActionSource`

`mcp.Config` could expose `OnStartRecord` / `OnStartReplay` hooks and require every game to manually swap its own `ActionSource` variable across live, record, and playback modes. This was rejected because every game would reimplement the same three-mode multiplexer and easily hit the final-step `Player.Done()` subtlety (dropping inputs on the last recorded step or getting stuck after playback ends). Making `ReplayController` itself a `replay.ActionSource` encapsulates those invariants once while still offering lifecycle hooks (`OnRecordStart`, `OnRecordStop`, `OnReplayStart`, `OnReplayStop`).

### Persisting MCP `metadata` inside the `.replay.yaml` file

Adding a `metadata` field to `replay.Recording` would persist agent labels on disk with the recording. This was rejected because `packages/game/replay` is a Stable package with a strict version-2 YAML contract checked by conformance tests and `go tool game replay`; keeping recordings in canonical v2 YAML ensures recordings created through MCP can be dropped straight into `testdata/` for CI regression tests with no format divergence.

### Allowing arbitrary filesystem paths in `game_replay_record_stop`

Letting an agent pass any file path to `save_path` would simplify one-off exports without setting `SaveDir`. This was rejected because MCP tools may be invoked by autonomous coding agents; confining disk writes to an explicit opt-in `ReplayConfig.SaveDir` (and returning the full YAML inline by default with in-memory storage) keeps playback self-contained on every target including WebAssembly while preventing unintended writes outside the project directory.

## Consequences

- **Benefits**: Agents can record live input sequences over MCP, inspect their deterministic v2 YAML, store or export them safely, and replay them (by name, from inline YAML, synchronously, or frame-by-frame) to reproduce bit-identical simulation and ECS component state. Games wire the capability in a handful of lines by passing `ReplayController` as their `replay.ActionSource`, and parallel MCP tool groups remain isolated in separate files.
- **Costs**: Games that want `run_to_end` or `steps` synchronous playback must expose a deterministic `StepFunc` (or `Loop`) and `ResetFunc`, and must configure watched `Actions`/`Axes` names (matching `replay.NewRecorder`) because `input.Actions` does not enumerate all dynamic action names by itself. In-memory recordings live for the lifetime of the `ReplayController` unless deleted or replaced by name.

## Verification

Verified with `go tool nsgo test ./mcp`, `go tool nsgo test ./replay`, `go tool nsgo test -run TestStabilityTable .`, `go tool nsgo check -target macos,windows,linux,android,web`, `go run ./cmd/gameschema -check`, `go run ./tools/changelog check`, `gofmt`, and `bun run verify-notes`.
