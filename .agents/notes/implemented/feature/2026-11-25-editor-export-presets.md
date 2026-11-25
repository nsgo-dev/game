# Agent Note: Editor Export presets panel and shared release command planner

Status: implemented

## Problem

`game.yaml`'s `export` section already holds the build output directory and per-platform export presets, and `go tool game release` validates credits (`tooling/credits`) and invokes `nsgo build`, but authors could only edit presets by hand and run release builds from a terminal. Exposing export preset editing and release builds inside `packages/game/editor` (#916) must keep preset edits undoable and validated by `project.Validate`, block uncredited assets before building just like `game release`, stream build output without blocking the UI event loop, and guarantee that editor dry-run output matches `go tool game release -dry-run` byte-for-byte.

## Decision

1. **Shared release planner (`packages/game/internal/releaseplan`)**: Extract release command planning, `nsgo.yaml` reading, output directory resolution, POSIX shell quoting, `credits.Collect` pre-flight, and dry-run formatting out of `packages/game/cmd/game/release.go` into `packages/game/internal/releaseplan`. Both `cmd/game` (`game release`) and `packages/game/editor` (`ExportPanelView`) call `releaseplan.Build` and `releaseplan.WriteDryRun`, guaranteeing byte-for-byte identical `nsgo build` command lines, credits comments, and error behavior without duplicating logic or adding a new public package to the API stability table.
2. **Isolated Export panel (`packages/game/editor/export_panel.go`)**: Add `ExportPanelView` toggled by the toolbar **Export** button (`toolbar-export`) with minimal hooks in `state.go`, `project.go`, `ui.go`, and `doc.go`:
   - **Undoable `game.yaml` edits**: `SetExportOutput`, `AddExportPreset`, `RemoveExportPreset`, `MoveExportPreset`, `SetExportPresetTargets`, and `ToggleExportPresetTarget` all execute through `Editor.EditProject` (validated by `project.Validate`, deterministic `SaveProject` with `nsgo.yaml` sync). Targets are chosen strictly from `project.TargetsOf(platform)`, and removing the last preset with an empty output clears `Project.Export` to `nil` so `game.yaml` stays minimal.
   - **Pre-flight credits check**: Opening the panel, pressing **Refresh Credits**, or launching a build runs `CheckExportCredits` (`credits.Collect`). Missing credits are rendered with each asset's `*.import.yaml` sidecar path and stop both real builds and dry runs before spawning `nsgo build`.
   - **Off-UI streaming builds and injectable hooks**: `StartExportBuild` / `RunExportBuild` execute on a background goroutine, streaming line-split stdout/stderr back onto the UI goroutine via `core.Scope.Post` (with mutex-protected state for headless tests), writing `CREDITS.md` on successful builds when `assets` is configured, and opening the resolved output directory via `Editor.OpenOutputFolder` (defaulting to `open`/`explorer`/`xdg-open` via `cmd.Start`). Both `Editor.ExportCommandRunner` and `Editor.OpenOutputFolder` are injectable for deterministic testing.

## Alternatives considered

- **Spawning `go tool game release` as a subprocess from the editor** — Keeps all release logic in `cmd/game` with zero shared package. Rejected because a project's module might not have `cmd/game` built or on PATH during unit tests, running `go tool game` adds a nested Go toolchain startup on every dry run, and `cmd/game` only reads saved `game.yaml` on disk rather than reflecting in-memory `Editor.Project` edits.
- **Duplicating `releaseArgs` and `shellQuote` directly inside `packages/game/editor`** — Avoids touching `packages/game/cmd/game/release.go`. Rejected because any future change to `game release` flags, default platforms, channel handling, or dry-run formatting would silently drift between the CLI and the editor.
- **Putting the shared planner under `packages/game/tooling/` as an exported package** — Allows external tools to import the planner. Rejected because the release CLI's flag-to-`nsgo build` glue is an internal implementation detail of this module and does not need a public API stability commitment.

## Consequences

- **Benefits**:
  - Editor dry-run output is guaranteed byte-for-byte identical to `go tool game release -dry-run` on the same project.
  - Every preset edit is validated by `project.Validate`, undoable/redoable, and synced to `nsgo.yaml` on save so `gameproject targets` agrees immediately.
  - Credits pre-flight surfaces uncredited assets in the panel before building and blocks releases cleanly, clearing on refresh once sidecars are added.
  - Panel code is isolated in `export_panel.go` / `export_panel_test.go`, keeping shared editor file diffs minimal.
- **Costs and known limits**:
  - `project.TargetsOf` (in `packages/game/project/nsgo.go`) defines the schema-validated targets available in the panel; when NSGo adds new build targets, `project/nsgo.go` must be updated for both `project.Validate` and the panel to expose them.
  - Opening the output folder with the default OS opener is supported on desktop targets (`darwin`, `windows`, `linux`, BSDs) and returns a clear unsupported-platform error on `web` and `android`.

## Verification

- `(cd packages/game && env -u GOROOT go tool nsgo test ./internal/releaseplan/... ./editor/... ./project/... ./cmd/gameproject/...)`
- `(cd packages/game && env -u GOROOT go tool nsgo test ./cmd/game -- -run 'TestRelease|TestEditor')`
- `(cd packages/game && env -u GOROOT go tool nsgo test . -- -run 'TestStabilityTable|TestAgentSkillValid')`
- `(cd packages/game && env -u GOROOT go tool nsgo check -target macos,windows,linux,android,web)`
- `env -u GOROOT go run ./tools/changelog check && bun run verify-notes`
