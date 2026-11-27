# Agent Note: Per-project scene schema contract

Status: implemented

## Problem

Published scene schemas (`apps/website/public/schemas/scene.v*.json`) are generated from `nsgo-dev/game`'s own Go types by `tools/gameschema`, so they only know built-in document fields and `navregion2d`. A game project's custom components—and runtime plugins' components—never reach the JSON Schema that editors, CI checks, and agents validate scenes against. In addition, `ComponentRegistry` had no merge operation for combining plugin and game registries, and `tools/gameschema`'s unexported `componentTypeSchema` left the 2D shape field kinds (`FieldPoint`, `FieldRect`, `FieldCircle`, `FieldPolygon`) untyped and dropped `default` values.

## Decision

`pkg/sceneformat` owns component registry merging and JSON Schema generation, and `game schema` delegates to a project's own editor command via `editorcmd -schema` to extract the schema of the project as built.

### Registry merge and schema generation in `pkg/sceneformat`

- `(*ComponentRegistry).Merge(other *ComponentRegistry) error` merges all types from `other` into the receiver atomically (all-or-nothing): it checks every name in sorted `other.Names()` order before mutating the receiver, refuses any collision with an error naming the component and summarizing both existing and incoming definitions, and deep-clones each type.
- `editorcmd`'s `withPhysicsComponents` keeps its explicit "game's type wins" rule rather than calling `Merge`, because games are allowed to override built-in physics or skeletal component definitions while plugin merges must fail loudly on collisions.
- `ComponentTypeSchema(t ComponentType) *jsonschema.Schema` replaces `tools/gameschema`'s unexported helper and maps every `FieldKind`—including inline draft-07 schemas for `FieldPoint`, `FieldRect`, `FieldCircle`, and `FieldPolygon`—plus `default`, `enum`, numeric bounds, descriptions, and `additionalProperties: false`. Inline shape schemas keep `ComponentTypeSchema` and `ComponentRegistry.JSONSchema()` self-contained without requiring external `#/definitions/` entries.
- `(*ComponentRegistry).JSONSchema()` returns the components-map schema in ascending `Names()` order (`additionalProperties: false` when non-nil; `{}` when nil).
- `SceneSchema(reg *ComponentRegistry)` builds a full draft-07 scene schema accepting versions `MinVersion` through `CurrentVersion` (so one per-project schema validates every scene file in a project regardless of version, while published `scene.v*.json` files remain pinned to a single `const` version) and wires `ObjectDoc.components` and `ObjectOverride.components` to `#/definitions/Components`. When `reg` is nil, `SceneSchema(nil)` types built-in physics, skeletal, and `navregion2d` components while keeping `additionalProperties: {}` so projects without an editor command still validate custom components.

### CLI delegation contract (`game schema` and `editorcmd -schema`)

- Because the `game` CLI binary cannot inspect a downstream project's Go types directly, `game schema [-out file] [project-dir | game.yaml]` discovers the project's own editor command using the exact same `editorcmd.ProjectRoot` and `projectEditor` rules as `game editor` (`cmd/editor` importing `pkg/editor/editorcmd`, or a `-tags gameeditor` main package).
- When the project has an editor command, `game schema` runs it with `-schema` (`go run ./cmd/editor -schema` or `go run -tags gameeditor . -schema`), capturing stdout into memory before writing to stdout or `-out` so a failing build never overwrites `-out`.
- `editorcmd.Run` handles `-schema` before `New`/`Open`/`launch`: it applies `withPhysicsComponents(opts.Components)`, writes `sceneformat.SceneSchema(components)` to `opts.Stdout` (defaulting to `os.Stdout`), and returns immediately without touching user settings, layout files, or opening a window.
- When the project has no editor command of its own (or `cmd/editor` does not import `editorcmd`), `game schema` emits `sceneformat.SceneSchema(nil)`.
- All schema output is deterministic: sorted component names, declaration-order fields, sorted JSON Schema definitions, lowercase JSON tags on `Point`/`Rect`/`Circle`, and no timestamps.

## Alternatives considered

- **Parsing a project's Go AST in `game schema` to find `Register` calls**: avoids compiling the project's editor binary, but breaks as soon as components come from helper functions, loops, or external plugin modules (`pkg/plugin`), and cannot evaluate runtime defaults or plugin composition. Delegating to `editorcmd -schema` uses the exact compiled registry the project's editor runs with.
- **Emitting only the `components` subschema from `editorcmd -schema` and splicing it in `gamecli`**: splits schema construction across two binaries and couples them via an ad-hoc wire format. Emitting the full draft-07 scene schema directly from `editorcmd -schema` makes `go run ./cmd/editor -schema` usable standalone as well as via `game schema`.
- **Using `$ref` to `#/definitions/Point` inside `ComponentTypeSchema`**: avoids repeating `{x, y}` in large schemas, but breaks standalone `ComponentTypeSchema(t).Validate(...)` and `reg.JSONSchema().Validate(...)` calls that have no root `definitions` map. Inlining the small shape object schemas keeps every returned schema valid on its own.

## Consequences

- **Benefits**: Editors, CI checks, and agents can run `nsgo game schema -out ...` to get an exact, deterministic scene schema covering every game, plugin, and built-in component; `ComponentRegistry.Merge` gives the upcoming runtime plugin loader (#1272) a strict all-or-nothing merge primitive; all 10 `FieldKind`s and field defaults are validated in both per-project and published schemas.
- **Costs**: Running `game schema` on a project with its own editor command invokes `go run`, which compiles the project's editor package when the Go build cache is cold; `pkg/sceneformat` now imports `pkg/jsonschema` (pure Go, stdlib + yaml only).
