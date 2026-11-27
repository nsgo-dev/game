# Agent Note: Go plugin manifest format (`plugin.yaml`)

Status: implemented

## Problem

Go plugins in `nsgo-dev/game` had no declarative description outside their Go source code: `editor.Plugin` only carried a `Name` and callbacks, and `game.yaml` only listed bare plugin names under `editor.plugins`. Upcoming plugin tooling (`game plugin add/sync/check/new`), the library index, the website directory, and the scene editor all need to inspect a plugin's runtime/editor/CLI Go entry packages, engine compatibility constraints, target API version, platform support, dependencies, and conflicts before compiling any code.

## Decision

Introduce a dedicated, dependency-light package `pkg/plugin/manifest` (no dependency on `pkg/editor` or `pkg/project`) defining the strict versioned YAML file `plugin.yaml` (`manifestVersion: 1`) at the root of a plugin's Go module, along with its published JSON Schema at `apps/website/public/schemas/plugin.v1.json`.

Key format invariants:

- **No `version` field**: A plugin's version is its Go module version resolved by the game project's `go.mod` (and locked by `go.sum`). `plugin.yaml` omits `version` and refuses it as an unknown key so a plugin never carries two competing version numbers that drift from Git tags and `go.mod`.
- **Module-path keys in `requires` and `conflicts`**: Dependencies (`requires`) and exclusions (`conflicts`) name full Go module paths (for example, `example.com/navkit`) rather than short plugin names. This lets `game plugin sync` and conflict checks match directly against the resolved modules in `go.mod` without ambiguity across registries. `requires` and `conflicts` must be disjoint and neither may name the plugin's own `module`.
- **Module-confined entry packages**: Every declared entry package (`entries.runtime`, `entries.editor`, and every `entries.cli[].package`) must equal `module` or lie strictly under `module/` (verified with a slash boundary so `example.com/healthkit2` cannot match `example.com/healthkit`). At least one entry must be declared.
- **Shared semver constraints**: `engine` (required) and every value in `requires` are validated via `internal/semver.ParseConstraint` so mods, library packs, and plugins share one SemVer 2.0.0 constraint syntax.
- **Strict decoding and deterministic byte-ordered output**: `Parse` uses `yaml.Decoder.KnownFields(true)`, refuses empty files, duplicate keys, trailing YAML documents, and non-plain-decimal literals for `manifestVersion` and `api` (such as `1.0` or `0x1`), and reports `*loaderr.Error` values with file (via `ParseFS`), line, field path, and fix. `Encode` sorts every list (including `entries.cli` by `name`) and the `requires` map keys in strict byte order without mutating caller slices, and omits empty `contributes` blocks.
- **No `pkg/project` import**: `pkg/plugin/manifest` implements the `ValidateName` rule directly (1 to 64 lower-case ASCII letters, digits, and `-`, starting with a letter or digit) rather than importing `pkg/project`, avoiding any pull-in of `nsgo/core` and preventing an import cycle when `pkg/project` validates `game.yaml` v2 `plugins:` entries.

## Alternatives considered

- **Adding a `version` field to `plugin.yaml` (like `mod.yaml` and `pack.yaml`)**: Script mods and asset packs are not Go modules and have no `go.mod` to resolve their versions, so their manifests must carry a version. For a Go module plugin, a manifest `version` duplicates the Git tag / `go.mod` version and creates inevitable drift on every release. Rejected.
- **Keying `requires` and `conflicts` by short plugin `name`**: Short names are easier to read but are not globally unique across third-party libraries or private repositories, and cannot be checked directly against `go.mod` without first fetching every candidate index. Rejected in favor of canonical Go module paths.
- **Importing `golang.org/x/mod/module` for module-path validation**: Adding a new direct dependency to `go.mod` for a small path-syntax check violates the repository's stdlib-first discipline. Implementing the Go module path syntax checks directly in pure Go keeps `pkg/plugin/manifest` self-contained.

## Consequences

- Downstream plugin packages (`pkg/plugin`, `pkg/project`, `pkg/tooling/library`, `internal/gamecli`, `tools/gameschema`) can all import `pkg/plugin/manifest` without pulling in `pkg/editor` or creating import cycles.
- `plugin.yaml` is enforced by `internal/formattest` conformance checks, `internal/repocheck`'s `TestFormatsPage` and `TestStabilityTable`, and `tools/gameschema -check` in CI.
- Any future schema revision bumps `ManifestVersion` and publishes a new `plugin.vN.json` alongside `plugin.v1.json`.
