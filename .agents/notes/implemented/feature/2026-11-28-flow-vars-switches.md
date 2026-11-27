# Agent Note: Versioned vars.yaml format and shared flow.Vars store

Status: implemented

## Problem

Gameplay facts were fragmented across private subsystem maps: `dialogue.Runner` kept its own variable map (requiring games to copy state in before every conversation and hiding `<<set>>` mutations from the rest of the game), world switches lived in ad-hoc save structs, and typos such as `$quest_stat` in Yarn dialogue scripts silently evaluated to `false` instead of failing static checks.

## Decision

Project-wide game variables and switches are declared in a standalone versioned YAML file (`vars.yaml` or `*.vars.yaml`, schema version 1, `https://game.nsgo.dev/schemas/vars.v1.json`) owned by `pkg/flow`, backed at runtime by `*flow.Vars`:

- **File format (`pkg/flow/vars_format.go`)**: Top-level `version: 1` and `vars:` list of `{name, kind, default, min, max, doc}` entries. `kind` must be `bool`, `int`, `float`, or `string`; `name` follows Yarn identifier rules without a leading `$`; `min`/`max` are valid only for `int` and `float` (and whole integers for `int`); effective defaults must lie within `[min, max]`. Unknown fields, duplicate keys, and trailing YAML documents are refused with `*loaderr.Error` values naming file, line, column, field, and fix. `EncodeVars` writes canonical deterministic YAML omitting zero defaults and empty lists.
- **Runtime store (`pkg/flow/vars.go`)**: `flow.NewVars(doc)` initializes all declared variables to their defaults with strict kind and range checks, synchronous `OnChange(name, fn)` notifications in registration order (plus `""` wildcard), scoped per-object `Self(objectID)` variables (`bool`, `int`, `float`, `string`), a `Lookup(name)` adapter ready for the `#1364` expression environment, and deterministic `VarsSnapshot` (`Version`, `Vars`, `Self`) with atomic `Restore` that fires no listeners and accepts JSON-decoded whole numbers for `VarInt`.
- **Dialogue integration (`pkg/dialogue/dialogue.go`)**: `dialogue.Options.Vars` connects a `dialogue.Runner` directly to a shared `*flow.Vars` store. When configured, `<<declare>>` and `<<set>>` of undeclared names return `flow.ErrUndeclaredVar`, and assignments enforce declared kinds and bounds. `dialogue.Script.VarRefs()` extracts every variable reference with its 1-based line number (excluding bare node titles in `visited(...)`/`visited_count(...)`).
- **Static check & live inspection (`pkg/tooling/assetcheck/vars.go`, `pkg/mcp/vars.go`, `pkg/editor/remote*.go`)**: `go tool game check` validates `vars.yaml` / `*.vars.yaml` files and verifies every variable reference in `.yarn` scripts under the same project root when declarations exist. `mcp.Config.Vars` exposes `game_vars_list`, `game_var_get`, and `game_var_set`, which the editor's Remote Inspector polls and edits live alongside active per-object self variables.

## Alternatives considered

- **Add a `vars:` section inside `game.yaml` (`pkg/project`)**: Keeps one fewer file at the project root, but couples `pkg/project` to gameplay flow declarations and inflates `game.yaml` diffs whenever narrative or quest designers add switches. A dedicated `vars.yaml` matches `quests.yaml` and `*.states.yaml` and keeps schema ownership cleanly inside `pkg/flow`.
- **Depend directly on `pkg/expr` (#1364) before landing `flow.Vars`**: Would couple `flow.Vars` directly to `expr.Env`, but `pkg/expr` does not exist on `main` yet. Providing `Vars.Lookup(name string) (any, bool)` keeps the runtime contract ready for `#1364` to wire directly without blocking variable declarations, dialogue sharing, static checks, or remote inspection.
- **Store `Self` variables as declared schema entries**: Would give static checking for per-chest keys, but forces authors to declare every scene instance's local switches globally. Keeping `Self(objectID)` dynamically keyed per scene object matches RPG Maker self switches and future `map_event` pages (#1755) while still persisting deterministically in `VarsSnapshot.Self`.

## Consequences

- **Benefits**: Dialogue scripts, Go game systems, save slots, `go tool game check`, and the editor's Remote Inspector all read and write one validated variable store with deterministic YAML and JSON round-trips.
- **Costs**: Projects that adopt `vars.yaml` must declare every variable referenced by their `.yarn` scripts before `go tool game check` passes. Published project templates (`template/*/template/`) stay pinned to the released `github.com/nsgo-dev/game` version until a release publishes `flow.Vars`, so migrating `rpg2d`'s `startTalk` to `dialogue.Options.Vars` waits for the template dependency bump.
