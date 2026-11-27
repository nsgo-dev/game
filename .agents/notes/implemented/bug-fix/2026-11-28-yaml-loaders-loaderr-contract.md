# Agent Note: Repository-wide `*loaderr.Error` contract for all YAML loaders

Status: implemented

## Problem

`docs/en/errors.md` and `docs/zh/errors.md` promise that every loader and validator in the module returns `*loaderr.Error` carrying file, 1-based line and column, field path, problem, and fix. In practice, 35 YAML-decoding packages returned plain `fmt.Errorf` / `errors.New` errors (covering gameplay data such as `pkg/quest`, `pkg/rooms`, `pkg/loot`, `pkg/inventory`, `pkg/ability`, `pkg/stats`, genre kits, engine/resource formats, online service configs, and tooling), and 6 packages that already imported `pkg/loaderr` still had partial YAML entry points returning plain or unlocated errors (`pkg/ai`, `pkg/editor`, `pkg/online/netplay/wire`, `pkg/playtest`, `pkg/sceneformat.ParseConflict`, and `pkg/tooling/bench.Decode`). Neither `internal/formattest` nor `internal/repocheck` checked the error type, allowing new YAML loaders to drift silently.

## Decision

Every YAML loader across all 58 YAML-decoding packages now returns `*loaderr.Error` located against the input bytes, and CI enforces the contract automatically:

- **Consistent validation + location pipeline**: Every YAML validator builds `*loaderr.Error` via `loaderr.Errorf(field, fix, ...)` with explicit field paths (walking map keys in sorted order for determinism), custom `UnmarshalYAML` helpers return `&loaderr.Error{Line, Column, ...}`, byte-level `Parse*` / `Decode*` entry points wrap decode and validation errors with `loaderr.Locate`, and path/FS/storage entry points attach the source name via `loaderr.WithFile` or `loaderr.Locate(..., path, nil, fix)`.
- **Sentinel and typed-error preservation**: Package sentinels (`quest.ErrInvalidBook`, `rooms.ErrInvalidMap`, `stats.Err*`, `render3d.ErrInvalidProbe`, `editor.errViewStateNewer`) and typed errors (`*animclip.StateMachineError`) are placed inside `*loaderr.Error.Err` rather than wrapped outside it, because `loaderr.Locate` extracts and returns the inner `*loaderr.Error`. Decode errors that carry a sentinel use double `%w` (`fmt.Errorf("%w: decode: %w", ErrSentinel, err)`) so `loaderr.Locate` still inspects `*yaml.TypeError` while keeping `errors.Is` intact.
- **Lightweight token dependency budget**: `pkg/online/accounts/token/imports_test.go` allows `github.com/nsgo-dev/game/pkg/loaderr` alongside `go.yaml.in/yaml/v3`, because `pkg/loaderr` itself imports only the standard library and `go.yaml.in/yaml/v3`, preserving the token package's minimal transitive dependency graph.
- **Automated enforcement**: `internal/formattest.Run` now requires every `unknown_field`, `missing_version`, `zero_version`, and `future_version` refusal to satisfy `loaderr.As(err)` with `e.Line == wantLine` and non-empty `e.Fix`. `internal/repocheck/conformance_test.go` additionally checks that every non-exempt YAML-decoding package calls `loaderrtest.Expect` or `loaderrtest.ExpectField`, backed by `load_errors_test.go` tests across all format packages.

## Alternatives considered

### Enforce `*loaderr.Error` only in `internal/repocheck` via an import check

Checking that every YAML-decoding package imports `pkg/loaderr` would be a small AST test, but would miss partial conversions where a second parser in the same package (such as `pkg/ai.ParseGOAP`, `pkg/sceneformat.ParseConflict`, `pkg/tooling/bench.Decode`, or `pkg/editor.ParseLayout`) returns plain errors or forgets `loaderr.Locate`. Enforcing `*loaderr.Error`, exact line, and non-empty `Fix` directly inside `internal/formattest.Run` verifies runtime behavior for every registered format.

### Exempt internal/service YAML configs (`assetbuild`, `undo`, `statedump`, `online/*`)

Exempting smaller or internal YAML formats would have reduced initial churn, but would leave inconsistent diagnostics for `game check`, editor logs, and server startup failures, and require maintaining an exclusion list in `formattest` and `repocheck`. Converting every YAML-decoding package keeps zero format exceptions.

## Consequences

- **Benefits**: Every YAML loader in the repository now reports actionable `file:line:column: field: problem; fix: ...` diagnostics while keeping all existing `errors.Is`/`errors.As` contracts intact. Both `formattest.Run` and `repocheck` block future YAML loaders from shipping plain errors or omitting `loaderrtest` coverage.
- **Costs**: Existing unit tests that matched raw `yaml.v3` text (`"field X not found"`) or asserted a leading `"<pkg>: "` prefix on `err.Error()` had to be updated to match the structured `<input>:line:col: field: unknown field "X"; fix: ...` output format.
