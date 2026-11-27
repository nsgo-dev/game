# Agent Note: Strict validation for anim3d clip event sidecars and glTF extras

Status: implemented

## Problem

`pkg/anim3d/events.go` did not hold clip event sidecars or glTF animation `extras` to the repository's strict text-format rules:

1. `LoadEventsSidecar` decoded with plain `json.Unmarshal`, silently ignoring misspelled or unknown keys (so `{"tme": 0.4, "name": "hit"}` loaded at time `0`), never validated `eventsSidecarFile.Version` (accepting missing versions and arbitrary numbers like `7`), and fell back to an unversioned flat `{"clipName": [...]}` shape whenever the `clips` key was absent (causing a typo such as `"clip"` to produce a misleading type error instead of reporting the unknown key).
2. `LoadEventsFromGLTFExtras` and `ClipFromGLTF` (`pkg/anim3d/playback.go`) swallowed every `json.Unmarshal` error on `anim.Extras` so non-object glTF extras would be ignored. As a result, malformed `events` or `nsgo_events` payloads (such as a string `"time"` or misspelled event key) silently dropped all events for that clip without returning an error.
3. The `internal/repocheck/formats_test.go` check for `anim3d` only verified that the documented example loaded, without checking that unknown fields or unsupported versions were refused.

## Decision

`pkg/anim3d/events.go` and `pkg/anim3d/playback.go` strictly validate clip event sidecars and glTF `extras` event arrays while preserving glTF 2.0's openness for non-object and third-party `extras` (extending the glTF clip bridge documented in [glTF 2.0 animation playback in anim3d](../feature/2026-11-24-anim3d-gltf-playback.md)):

- **Versioned sidecar contract (`EventsSidecarVersion = 1`)**: `LoadEventsSidecar` requires the `{"version": 1, "clips": {...}}` envelope and drops the unversioned flat-map fallback. It validates syntax and trailing data, rejects `null` roots and duplicate keys, decodes the envelope and every `ClipEvent` element with `DisallowUnknownFields`, requires `version == EventsSidecarVersion`, and sorts clip names before per-clip validation so errors are deterministic. Every failure returns a `*loaderr.Error` located in the original JSON bytes via `loaderr.LocateJSON`.
- **Element-level event decoding with document relocation**: Both `LoadEventsSidecar` and glTF `extras` share `decodeClipEventSlice`, which decodes each event object strictly and relocates field paths (such as `clips.attack[0].tme` or `animations[1].extras.events[0].time`) back into the full source JSON document when available. This preserves exact line and column reporting even when the same misspelled key appears in multiple events or inside `ClipEvent.Data`.
- **Selective glTF `extras` validation**: `parseGLTFAnimationExtras` probes `anim.Extras` as a generic JSON value first so absent, `null`, and non-object `extras` (strings, numbers, booleans, arrays) are ignored per glTF 2.0. When `extras` is a JSON object, only `"events"` and `"nsgo_events"` are inspected; unrelated keys from Blender or other exporters are left untouched, while malformed `"events"` / `"nsgo_events"` values return a `*loaderr.Error` naming `animations[i].extras.<key>...`. Both `LoadEventsFromGLTFExtras` (with full document line/column) and `ClipFromGLTF` / `ClipsFromGLTF` (with field path and fix) call this helper.
- **Duplicate key case semantics**: `refuseDuplicateJSONKeys` uses case-folded comparison (`strings.EqualFold`) on struct objects (`eventsSidecarFile`, `ClipEvent`), matching `encoding/json`'s struct field matching, and exact comparison on map objects (`clips`, `ClipEvent.Data`) so distinct clip or metadata names differing only in case remain valid.
- **Repocheck enforcement**: `internal/repocheck/formats_test.go` maps `anim3d.EventsSidecarVersion` to `anim3d` in `formatVersions` and checks in `readClipEvents` that `LoadEventsSidecar` refuses an unknown top-level field, an unsupported future version, and an unknown event field.

## Alternatives considered

### Gate the legacy flat-map shape behind a separate `LoadEventsSidecarFlat` function

Keeping the unversioned `{"clipName": [...]}` reader under a new exported name would preserve a migration helper for callers using flat maps. Rejected because no code, example, or documentation in the repository uses or documents the flat shape (only two unit tests exercised it), and exporting an unversioned file-format reader works against the repository's versioned-schema rule.

### Add a standalone YAML clip event sidecar format in this change

Authoring clip events in YAML would align hand-edited event files with the repository's YAML rule immediately. Rejected for this change because per-clip YAML settings and events in model sidecars are tracked by #1430 (still open); adding a separate YAML schema now would create a second competing sidecar format before #1430 settles the model sidecar layout.

### Validate the entire glTF `extras` object with `DisallowUnknownFields`

Decoding `animations[i].extras` directly into a strict struct would be shorter than probing into `map[string]json.RawMessage`. Rejected because glTF 2.0 `extras` is shared across every exporter and tool (such as Blender custom properties); refusing unrelated sibling keys in `extras` would break valid glTF assets.

## Consequences

- **Benefits**: Misspelled sidecar keys (`clip`, `tme`), missing or unsupported versions, duplicate keys, and malformed glTF `extras` event payloads (`time: "0.4"`, non-array `events`, misspelled event fields) now fail immediately with actionable `*loaderr.Error` diagnostics (file, line, field path, and fix) in `LoadEventsSidecar`, `LoadEventsFromGLTFExtras`, `ClipFromGLTF`, and `ClipsFromGLTF`. Using `loaderr.LocateJSON` also avoids pulling `yaml/v3` into WASM builds that only load JSON.
- **Costs**: Unversioned flat clip-event JSON maps and sidecars omitting `"version": 1` are no longer accepted by `LoadEventsSidecar`, and glTF assets with malformed `events` or `nsgo_events` in animation `extras` now cause `ClipFromGLTF` / `ClipsFromGLTF` to return an error instead of silently producing a clip with no events.
