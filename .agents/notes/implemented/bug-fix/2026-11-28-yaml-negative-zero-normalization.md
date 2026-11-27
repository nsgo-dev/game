# Agent Note: Normalize negative zero floats across YAML format codecs

Status: implemented

## Problem

`go.yaml.in/yaml/v3` encodes a float `-0.0` as the plain scalar `-0`. On the next load, YAML resolves `-0` to integer `0` (decoded into a float field as `+0.0`), so the next save writes `0`. Materials (`pkg/tooling/hotreload`), resource files (`pkg/resource`), `game.yaml` (`pkg/project`), and over twenty other YAML format packages carried float fields (non-`omitempty` floats, float pointers, maps, slices, arrays, and `any` property maps) without normalizing negative zero, breaking the repository invariant that a load and save round trip changes nothing (`nsgo-dev/game#1605`). Earlier packages had added one-off `normalize()` or `cleanZero` helpers (`pkg/importsettings`, `pkg/replay`, `pkg/rooms`, `pkg/tooling/bench`, `pkg/playtest`, `pkg/project/brief.go`, `pkg/render3d/probe_io.go`, `pkg/online/netplay/wire`), leaving every new format prone to repeating the bug.

## Decision

Introduce a small standard-library-only internal package, `internal/yamlzero`, and apply it uniformly at every YAML format's parse and encode boundaries:

- **`internal/yamlzero`**: provides `Has`, `Normalize`, and generic `Clean` over Go's `reflect` and `math` packages with no `yaml` dependency and a recursion depth bound (`128`).
  - `Normalize` replaces every reachable `float32`/`float64` negative zero with `+0.0` in place (including pointers, slices, arrays, exported struct fields, embedded structs, maps of scalars/arrays/structs/interfaces, and `any` values).
  - `Clean` checks `Has(v)` first and returns `v` untouched with zero allocations on the common path where no `-0.0` is present; only when a `-0.0` exists does it copy along the affected path so `Encode` never mutates caller-owned structs.
- **Format boundaries**: every YAML format package with float fields calls `yamlzero.Normalize` after validation in `Parse`/`Load` (so parsed values never carry `-0.0` in memory) and passes `yamlzero.Clean(...)` to the YAML encoder in `Encode` (so programmatic `-0.0` values also encode as `0` without mutating the input). Custom float scalar formatters (`pkg/datatable/yaml.go`, `pkg/datatable/document.go`, `pkg/sceneformat/propfloat.go`, `pkg/tilemap/tileset_doc.go`) also collapse `-0.0` to `0` before `strconv.FormatFloat`.
- **Fuzz and regression coverage**: materials (`FuzzParseMaterialRoundTrip`), resources (`FuzzParseRoundTrip`), and project files (`FuzzParse`) carry `-0.0` seeds and regression tests covering both YAML parse→encode→parse→encode and in-memory `-0.0` encode stability.

## Alternatives considered

- **Per-package hand-written `normalize()` and `clone()` helpers (the `importsettings` / `rooms` pattern)**: avoids `reflect`, but requires listing every float field across ~30 format packages and deep-cloning every struct in `Encode` to avoid mutating callers; any newly added float field silently reintroduces the bug.
- **Encode to `yaml.Node`, rewrite `"-0"` scalars, and re-encode**: `yaml.Node.Encode` in `go.yaml.in/yaml/v3` marshals to bytes and re-parses the entire document internally (double work, and `-0` is resolved as `!!int` during the internal re-parse), and does not normalize `-0.0` in values returned by `Parse`.

## Consequences

- **Benefits**: every YAML format in the module satisfies byte-for-byte idempotence on `-0.0` inputs and programmatically constructed `-0.0` values without mutating caller inputs, with zero allocations on the common clean path. New float fields added to existing format structs are normalized automatically.
- **Costs**: format `Parse` and `Encode` paths perform a lightweight reflection traversal via `internal/yamlzero` (verified at zero allocations for clean structs without maps). Custom `MarshalYAML` implementations that inspect unexported float fields directly still need to handle `-0.0` themselves (existing examples such as `pkg/replay` and `pkg/tooling/bench` already do).
