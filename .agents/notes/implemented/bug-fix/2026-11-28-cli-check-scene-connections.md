# Agent Note: Delegate `game check` to project editor commands for scene connection validation

Status: implemented

## Problem

`game check` only called `assetcheck.Check(os.DirFS(dir))` with a nil `ComponentRegistry`, so it validated that each connection's `from` and `to` named an existing object but never checked connection events, handlers, handler arguments, or custom component fields—even in projects that already expose their compiled component registry via a project editor command. A typo such as `door.opne` passed `game check` and CI, failing only at runtime in `sceneformat.Connect` or when opening the scene in the editor.

## Decision

`game check` follows the same project-editor delegation contract as `game editor` and `game schema` (see [per-project scene schema](../../implemented/architecture/2026-11-27-per-project-scene-schema.md)):

- `editorcmd.Run` adds a `-check <dir>` flag that runs `assetcheck.CheckWith(os.DirFS(dir), assetcheck.Options{Components: components})` where `components` is `withPhysicsComponents(opts.Components)` (preserving any game override of a built-in component name while adding built-in physics and skeletal components). It writes problems to `stderr` in `file:line: message` form, writes `report.Summary()` to `opts.Stdout` (defaulting to `os.Stdout`) when clean, and exits without opening a window. `-schema` and `-check` are mutually exclusive and `-check` refuses extra positional arguments.
- `internal/gamecli/check.go` resolves `editorcmd.ProjectRoot(dir)` and `projectEditor(root)`. When the project has an editor command (`cmd/editor` importing `pkg/editor/editorcmd`, or a `-tags gameeditor` main package), `game check` runs it with `-check <target>` (`"."` when checking `.`, otherwise the absolute path of `dir`).
- When no editor command is present (or `cmd/editor` does not import `editorcmd`), `game check` keeps the object-only `assetcheck.Check` path. `assetcheck.Report.Connections` counts connections encountered across checked scenes (including scenes failing `sceneformat.Parse` whose top-level YAML `connections` sequence is non-empty, or scenes bringing connections from non-`*.scene.yaml` references); when `report.Connections > 0` and no editor command ran, `game check` prints one note on `stderr` explaining that connection events and handlers were not checked and pointing to `game generate editor`.
- `template/action3d/template/project_test.go` (and its embedded copy under `internal/gamecli/_templates/projects/action3d/project_test.go`) adds `TestScenesCheck` calling `assetcheck.CheckWith` with `levelComponents()` so template projects that use connections validate events and handlers during `go test` even without generating an editor command.

## Alternatives considered

- **Always printing the unchecked-connections note whenever no editor command exists**: simpler because `assetcheck.Report` would not need a `Connections` count, but it emits misleading noise on directories and projects that use no connections (such as tilemap-only scenes, cutscenes, or `examples/`) and breaks tests asserting clean or exact `stderr` output. Gating the note on `report.Connections > 0` keeps noise-free behavior for projects without connections while always warning when unchecked connections exist.
- **Inspecting Go ASTs in `game check` to extract registered events and handlers**: avoids running `go run` for the project's editor command, but fails whenever components are registered dynamically, built by helper functions, or supplied by external plugins. Running `editorcmd -check` uses the exact compiled `ComponentRegistry` the editor uses.
- **Passing a separate display-prefix flag to `editorcmd -check` for relative `dir` paths**: would make `game check examples/fieldmedic` print relative `examples/fieldmedic/...` paths rather than absolute paths, at the cost of leaking an internal CLI plumbing flag into `editorcmd`'s public flag set. Passing `"."` for `.` and `filepath.Abs(dir)` otherwise matches `game editor`'s path-resolution convention and keeps every printed path directly openable in terminals and editors.

## Consequences

- **Benefits**: Projects with a `gameeditor` or `cmd/editor` command now catch missing events, missing handlers, invalid or unknown handler arguments, invalid component fields, and overrides that break connections from instanced or inherited scenes in `game check` and CI. Projects without an editor command get a single actionable hint whenever their scenes contain connections, and `action3d` projects validate connections directly in `go test`.
- **Costs**: Running `game check` on a project with an editor command invokes `go run`, which compiles the project's editor package when the Go build cache is cold. `pkg/editor/editorcmd` now imports `pkg/tooling/assetcheck` (which `pkg/editor` already imported).
