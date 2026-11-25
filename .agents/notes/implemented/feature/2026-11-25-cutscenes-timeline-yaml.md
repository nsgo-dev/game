# Agent Note: Version 2 YAML timeline cutscenes, deterministic player, schema, refs and assetcheck

Status: implemented

## Problem

`game/cutscene` previously supported only version 1 video cutscenes (`source` played in `core.Video`). Games authored in NSGo's text-first workflow (and the NSGo Game Editor cutscene review canvas in `docs/design/editor/Animation.dc.html`) need in-engine timeline cutscenes in YAML with camera, actor, localized dialogue, sound and music tracks that run deterministically on `game.Loop` fixed steps, remain skippable like video cutscenes, publish JSON Schemas, and are validated by `game check` and tracked by `refs` (#1055).

## Decision

Extend `packages/game/cutscene` to schema version 2 (`MinVersion = 1`, `CurrentVersion = 2`) alongside unchanged version 1 video cutscenes, add a deterministic `TimelinePlayer` plus `Player` integration, publish `cutscene.v1.json` and `cutscene.v2.json`, and wire `*.cutscene.yaml` files into `packages/game/refs` and `packages/game/tooling/assetcheck`:

- **Version 2 schema with lossless v1 preservation**: `Cutscene` adds `Kind` (`video` | `timeline`), `Scene`, `Length`, and `Tracks []Track` (each holding typed `Clip` entries with `Start`, `Duration`, `Easing`, and kind-specific fields). Version 1 files refuse all v2 fields and `Encode` writes v1 bytes unchanged without a schema header; version 2 files write `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/cutscene.v2.json` and round-trip deterministically. Dialogue clips accept only `line` (l10n key) and optional `speaker`, refusing any inline text field.
- **Stateless pose evaluation in `TimelinePlayer`**: `NewTimelinePlayer` snapshots initial scene object and camera positions at construction. On every `Update(dt)` or `Seek(at)`, `applyPose` evaluates all actor tracks first (`walk`, `state`, `drop`, `patrol`) and then camera tracks (`hold`, `pan`, `follow`) from the initial snapshot, chaining camera clip start positions from prior clips' end poses. This guarantees `Seek(at)` and fixed-step `Update(dt)` produce identical deterministic coordinates while keeping audio/completion cues restricted to forward `Update(dt)` crossings.
- **Unified `Player` skip and lifecycle surface**: `Player.PlayTimeline`, `Player.Step`, `Player.Dialogue`, and `Player.TimelineScene` reuse `Player`'s skip keys, press-to-skip, back interception, `CanSkip()`, `Skip()`, `Stop()`, and scope cleanup. Unlike `Player.Play` for video cutscenes, `PlayTimeline` keeps `Options.Loop` and `Options.Mixer` running so the scene continues rendering and playing cutscene audio.
- **Published schemas, `refs` and `game check`**: `cmd/gameschema` generates both `cutscene.v1.json` (skipping v2 fields and requiring `source`) and `cutscene.v2.json` with `cutscene.Duration` mapped to JSON string. `refs.Scan` tracks `*.cutscene.yaml` / `*.cutscene.yml` `scene`, project-relative video `source`, and sound/music `source` paths (skipping URI schemes such as `asset:`). `assetcheck` validates cutscene syntax/semantics, referenced scene object IDs (resolving inherited/instanced scenes), audio file existence, and dialogue `line` IDs against YAML l10n string tables discovered under the same project root.

### Invariants

- Version 1 video cutscenes must parse, encode, and play byte-for-byte identically to before.
- `TimelinePlayer` must never spawn background goroutines or use wall-clock time or hidden randomness.
- Dialogue clips must store only localization line IDs; inline dialogue text is never added to the cutscene schema.
- `game/cutscene` remains Experimental in `docs/{en,zh}/stability.md` so it may import `game/sceneformat` and `game/l10n` without violating stability rules.

## Alternatives considered

### Why extend `game/cutscene` to version 2 instead of creating a separate `game/timeline` package?

A separate package would keep `game/cutscene` strictly about `core.Video` and avoid changing `Cutscene` from a comparable struct to one containing slices. However, issue #1055 and the editor design explicitly unify video and timeline cutscenes under `*.cutscene.yaml` with shared skip controls, `Player`, and schema versioning (`cutscene.v1.json` beside `cutscene.v2.json`), matching how `sceneformat` evolved from version 1 to version 2.

### Why use a unified `Clip` struct with per-track validation instead of separate YAML fields per clip kind?

Separate typed structs (`CameraClip`, `ActorClip`, etc.) would let JSON Schema enforce per-kind required fields structurally without custom validation. However, YAML files would require nested kind wrappers or separate track-list fields per kind, losing the unified track order shown in `Animation.dc.html` (where tracks are an ordered list of lanes). A unified `Clip` with strict `Validate` checks per `TrackKind` keeps the YAML flat and diff-friendly while `KnownFields(true)` and `additionalProperties: false` still refuse unknown keys everywhere.

### Why check dialogue line IDs against all discovered l10n YAML string tables in the project root?

Requiring a cutscene file to name a specific `loc/en.yaml` path would couple cutscenes to one locale file and break multi-locale projects. Discovering all valid `l10n.ParseYAML` string tables under the cutscene's project root works for both `loc/*.yaml` and `locales/*.yaml` layouts and reports any missing line ID with exact cutscene file and line.

## Consequences

- **Benefits**: In-engine cutscenes are plain-text YAML with published schemas, deterministic fixed-step playback, lossless scrubbing, shared skip UX, safe path refactoring via `game mv`, and CI checking of missing scene object IDs, audio paths, and l10n line IDs via `game check`.
- **Costs**: Adding `Tracks []Track` makes `cutscene.Cutscene` non-comparable with `==` (acceptable because `game/cutscene` is Experimental and no external callers compared `Cutscene` directly). `assetcheck` scans non-scene YAML files under the project root lazily when a project contains timeline dialogue tracks.
