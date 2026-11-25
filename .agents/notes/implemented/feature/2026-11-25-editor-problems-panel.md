# Agent Note: Editor Problems panel running project and asset checks

Status: implemented

## Problem

`go tool game check` (`assetcheck.Check`) finds broken references, missing atlas frames, invalid components, and invalid connections across an entire project with file and line numbers, and `project.Check` validates `game.yaml`, `nsgo.yaml` sync, autoloaded scenes/resources, and installed pack files — but the scene editor previously only surfaced `BuildErr` for the single currently open scene. Authors working in the editor had to drop to the command line to discover project-wide faults (#913).

Running full project checks inside the editor introduces three non-obvious constraints:

1. **Event-loop safety**: `Editor` fields (`Doc`, `Status`, `revision`, `listeners`, `scope`) are owned by the UI/caller goroutine, whereas project checks perform recursive filesystem walks and YAML/JSON/image decoding that must run off the UI goroutine and work both when mounted in `EditorView` and headlessly in unit tests (`scope == nil`).
2. **Exact parity without duplicate reports**: `assetcheck.CheckWith` (via `refs.Scan`) already checks `game.yaml` parsing, autoload file existence, and `*.scene.yaml` parsing with exact line numbers, while `project.Check` overlaps on `game.yaml` parse and `LoadAutoloads` while adding `nsgo.yaml` sync, installed pack file checks, and non-`.scene.yaml` autoload scene parsing.
3. **File browser selection coordination**: `FilesView` previously kept its selected path and move-target text in local UI signals, preventing other panels (such as the Problems panel) from selecting a non-scene file without breaking `FilesView`'s undo-clears-selection behavior.

## Decision

Implement the Problems panel in `packages/game/editor/problems.go` and `packages/game/editor/problems_ui.go` with minimal hooks in `state.go`, `project.go`, `refs.go`, `animation.go`, `files.go`, and `ui.go`:

### 1. Mutex-isolated, generation-sequenced async runner

`problemState` guards its mutable fields (`gen`, `running`, `hasRun`, `projectPath`, `problems`, `checkErr`, `done`, `scope`) with its own `sync.Mutex`, completely decoupled from UI-goroutine-only `Editor` fields:

- `CheckProblems()` captures immutable inputs (`projectPath`, `dir`, `reg := e.components`), increments `gen`, replaces `done` with a fresh channel, and launches a goroutine running `runProjectCheck(dir, reg)`.
- When a goroutine finishes, it locks `mu` and drops its results if `p.gen != gen` (a newer check or project switch superseded it). Otherwise it publishes `problems`/`checkErr`, enqueues `scope.Post(e.notify)` when a `core.Scope` is attached, and closes its `done` channel *after* posting so `WaitForProblems()` followed by `app.Settle()` observes the UI update deterministically.
- `Problems()`, `ProblemCount()`, `ProblemErr()`, `CheckingProblems()`, and `WaitForProblems()` are safe to call from any goroutine (including `nsgotest.App.WaitUntil`).
- Automatic triggers fire on `OpenProject`, `SaveFile`, `SaveProject` (which also covers `InstallPack`/`RemovePack`), `SaveClipFile`, `toolbar-save` with custom `OnSave`, `followMove` (covering `MoveFile`, Undo, and Redo), and `SetComponentRegistry` when a project is open.

### 2. Combined `assetcheck.CheckWith` + `project.Check` with targeted deduplication

`runProjectCheck` runs `assetcheck.CheckWith(fsys, assetcheck.Options{Components: reg})` first, then `project.Check(fsys)`, and merges `project.Check` errors through `projectCheckProblems`:

- `nsgo.yaml` disagreement/parse errors map to `project.NSGoFileName` (`"nsgo.yaml"`).
- Missing installed pack files map to `project.FileName` (`"game.yaml"`).
- `game.yaml` parse errors and `LoadAutoloads` missing-file / `*.scene.yaml` parse errors are skipped when `assetcheck` already recorded the same fault with file and line, while non-`.scene.yaml` autoload parse errors (e.g. `scenes/broken.yaml`) are kept with line numbers extracted from `*loaderr.Error`.
- Combined problems are sorted by `(File, Line, Message)` and compacted with `slices.Compact`, guaranteeing exact parity with `go tool game check` when `ComponentRegistry` is nil.

### 3. Shared file selection via `Editor.SelectedFile` and revision-tagged draft

Promote the file browser's selected path to `Editor.SelectedFile` (updated via `SelectFile` / `selectedFileRev`), while keeping `FilesView`'s editable destination path as a local `targetDraft{rev, text}` signal that falls back to `ed.SelectedFile` whenever `selectedFileRev` advances. `OpenProblem` opens `*.scene.yaml`/`*.scene.yml` or autoloaded scenes with `OpenFile` (falling back to `SelectFile` on parse failure) and selects any non-scene file in `FilesView`. `followMove` deliberately leaves `SelectedFile` unchanged so undoing a file move still clears selection when the moved path disappears from disk.

## Alternatives considered

### Running `assetcheck.CheckWith` and `project.Check` synchronously on open/save/move

Synchronous checks would avoid goroutines and `problemState.mu`, simplifying tests. Rejected because issue #913 explicitly requires running checks off the UI goroutine so large projects with many scenes, maps, and PNG/JPEG images never stall frame rendering or input handling on save or move.

### Appending every `project.Check` error without deduplication

Always converting every error from `project.Check` into an `assetcheck.Problem` would avoid inspecting `LoadAutoloads` error prefixes. Rejected because projects with a missing autoload scene/resource or invalid `game.yaml` would list duplicate problems (one from `assetcheck` with line number and one from `project.Check` at line 0), breaking exact parity with `go tool game check`.

### Passing a `context.Context` into `assetcheck.CheckWith` for cancellation

Mid-walk cancellation would abort I/O earlier on rapid saves. Rejected because neither `assetcheck.CheckWith` nor `project.Check` accepts a `context.Context` and changing those package APIs is out of scope for #913; generation sequencing (`gen++`) already guarantees stale checks never publish results and exit immediately upon completion.

## Consequences

- **Benefits**: The editor surfaces project-wide asset, component, connection, and project-file errors live in `ProblemsView` and the status bar (`status-problems`) without blocking the UI thread; clicking a row jumps directly to the scene or selects the file in `FilesView`; results match `go tool game check` on the same project; all background access is race-free under `-race`.
- **Costs**: Opening, saving, or moving files in a project spawns a short-lived background goroutine; headless tests inspecting `Problems()` call `WaitForProblems()` to synchronize before reading results or cleaning up temporary directories.

## Verification

- `packages/game/editor/problems_test.go` verifies the missing-atlas-frame fixture end-to-end (open -> list with file & line -> press row to open scene -> fix & save -> cleared & status bar count 0), non-scene row selection in `FilesView`, exact parity with `assetcheck.Check` / `assetcheck.CheckWith`, `project.Check`-only issues (`nsgo.yaml` sync, missing pack file, invalid non-`.scene.yaml` autoload), and automatic re-check on `MoveFile` and `Undo`.
- `packages/game/editor/problems_internal_test.go` verifies out-of-order stale-generation rejection and zero goroutine leaks under `-race`.
- Verified via `gofmt`, `go tool nsgo test ./editor/... ./tooling/assetcheck/... ./cmd/game`, `TestStabilityTable|TestAgentSkillValid`, `go tool nsgo check -target macos,windows,linux,android,web`, `go run ./tools/changelog check`, and `bun run verify-notes`.
