# Agent Note: Shared SemVer and Constraint Contract in internal/semver

Status: implemented

## Problem

Previously, semantic version parsing and range evaluation existed only inside `pkg/modding/manifest.go`, while library packs validated `packVersion` as any non-space 1–64 character string (`project.ValidatePackVersion`). As the plugin system introduces `plugin.yaml`, `editor.Plugin`, `game plugin` commands, and library index v2, mods, packs, and plugins all need to parse and check versions—including Go module versions (`v0.4.0`, `v2.0.0+incompatible`) and Go pseudo-versions (`v0.0.0-20261120093000-0123456789ab`, `v0.4.1-0.20261120093000-0123456789ab`). The existing modding parser also rejected partial versions like `^1.2` even though its own validation fix hint suggested `^1.2`, rejected leading `v`, did not validate build metadata or pre-release identifier syntax, and used `strconv.Atoi` on numeric pre-release identifiers (which could overflow on long digit strings).

## Decision

Semantic version parsing, SemVer 2.0.0 comparison, and constraint evaluation live in the dependency-free `internal/semver` package (`Version`, `Parse`, `Version.Compare`, `Version.String`, `Constraint`, `ParseConstraint`, `Constraint.Check`, `Constraint.String`, `Check`). `pkg/modding` exposes `SemVer` as a type alias (`type SemVer = semver.Version`) and keeps `ParseSemVer`, `ValidateConstraint`, and `CheckConstraint` as thin wrappers so its public API is unchanged.

### Shared version and constraint contract

- **Optional leading `v`**: `semver.Parse` and `semver.ParseConstraint` accept an optional single lower-case leading `v` so Go module versions from `go.mod` and `debug.ReadBuildInfo` parse directly. `Version.String()` always omits `v` and build metadata. `project.ValidatePackVersion` explicitly rejects a leading `v` before delegating to `semver.Parse`, keeping `packVersion` in `game.yaml`, `pack.yaml`, and `index.yaml` on one canonical spelling (such as `1.0.0`).
- **Strict SemVer 2.0.0 validation**: `Parse` requires `MAJOR.MINOR.PATCH` with no leading/trailing whitespace, no leading zeros on numeric segments, valid `[0-9A-Za-z-]+` dot-separated pre-release identifiers (no leading zeros on purely numeric identifiers), and valid `[0-9A-Za-z-]+` dot-separated build metadata (validated and then discarded). `Version.Compare` compares purely numeric pre-release identifiers by arbitrary-length decimal value without `int` overflow.
- **Partial versions in constraints**: `ParseConstraint` accepts `MAJOR` and `MAJOR.MINOR` (without pre-release or build metadata) alongside full versions, matching npm and Cargo range bounds:
  - `^1.2` expands to `>=1.2.0 <2.0.0`, `^0.2` to `>=0.2.0 <0.3.0`, `^0.0` to `>=0.0.0 <0.1.0`, `^1` to `>=1.0.0 <2.0.0`, `^0` to `>=0.0.0 <1.0.0`.
  - `~1.2` expands to `>=1.2.0 <1.3.0`, `~1` to `>=1.0.0 <2.0.0`.
  - `=1.2` or bare `1.2` expands to `>=1.2.0 <1.3.0`; `=1` or bare `1` expands to `>=1.0.0 <2.0.0`.
  - `>=1.2` means `>=1.2.0`, `<1.2` means `<1.2.0`, `<=1.2` means `<1.3.0`, and `>1.2` means `>=1.3.0`.
  - Upper-bound increments saturate safely against `math.MaxInt` without integer overflow.
- **Pre-release semantics (pure `Compare`)**: Every constraint clause evaluates purely by SemVer 2.0.0 `Version.Compare` precedence, matching `pkg/modding`'s prior behavior. Pre-releases—including Go pseudo-versions—satisfy any range whose bounds bracket them: `>=1.0.0` matches `1.1.0-beta` and `v1.0.1-0.20261120093000-0123456789ab`, while `1.0.0-beta` and `v1.0.0-0.20261120093000-0123456789ab` do not; `*` and the zero `Constraint` match all versions including pre-releases. As a direct consequence of pure `Compare`, `<2.0.0` (including the upper bound of `^1.0.0`) matches `2.0.0-alpha` because `2.0.0-alpha < 2.0.0`; callers or authors who want to exclude `2.0.0` pre-releases write `<2.0.0-0`.

## Alternatives considered

- **Using `golang.org/x/mod/semver`**: Handles Go module `v` versions and `Compare`, and is maintained by the Go team. Rejected because it does not implement constraint ranges (`^`, `~`, `>=`, conjunctions, partial versions), requires a leading `v` on every input, and would add an external dependency for only half the contract.
- **npm/Cargo tuple-restricted pre-release matching**: Prevents `^1.0.0` or `>=1.0.0 <2.0.0` from matching `2.0.0-alpha` and prevents `>=1.0.0` from matching `1.1.0-beta`. Rejected because Go pseudo-versions (such as `v0.4.1-0.20261120093000-0123456789ab` produced by `go get ...@main`) are SemVer pre-releases; tuple-restricted matching would cause plugin `engine: ">=0.4.0 <0.5.0"` and `requires: { foo: "^0.1.0" }` checks to fail whenever a project tracks an untagged commit on a compatible minor line, and it would break existing `pkg/modding` semantics.
- **Rejecting partial versions and changing the modding fix hint**: Simpler parser with only 3-part versions everywhere. Rejected because npm and Cargo users routinely write `^1.2` and `~1.2`, and `pkg/modding` already advertised `^1.2` in its validation error hint.

## Consequences

- **Benefits**: Mods, library packs, and plugins share one zero-dependency SemVer parser and constraint evaluator; Go module versions and pseudo-versions work directly with plugin constraints; `project.ValidatePackVersion` prevents malformed `packVersion` values from entering `game.yaml`, `pack.yaml`, or `index.yaml`; fuzz tests pin round-trip formatting and `Compare` antisymmetry.
- **Costs**: Tightening `project.ValidatePackVersion` is a stricter read of `game.yaml`, `pack.yaml`, and library `index.yaml`—legacy non-semver pack versions such as `"1"` or `"latest"` are now rejected (all checked-in library packs already use `1.0.0`). Pure `Compare` pre-release semantics mean `<2.0.0` and `^1.0.0` accept `2.0.0-alpha` unless `<2.0.0-0` is specified, so docs and downstream plugin checks must rely on this documented rule rather than assuming npm's pre-release filter.
