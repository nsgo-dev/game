# Agent Note: Editor game runner and docked Output log panel

Status: implemented

## Problem

The scene editor's Play mode (`Play` / `PlayIn` / `Stop`) previews the open scene inside the editor's `Canvas`, but could not launch the real game process (`go tool game dev`) from the editor as Godot's Run Project (`F5`) does. Additionally, editor errors and status updates lived only in a single `Editor.Status` string that each new message overwrote, leaving no history and nowhere to stream live stdout and stderr from a running game.

## Decision

Add a cross-platform game runner (`run_game.go`, `run_game_unix.go`, `run_game_windows.go`, `run_game_unsupported.go`) and a docked Output / Log panel (`output_panel.go`) wired into `EditorView` without modifying the existing Play-in-editor preview:

- **Process-tree ownership and cross-target safety**: `RunGame` / `RunGameWith` launches `go tool game dev` in the open project directory on desktop targets (`darwin`, `linux`, `freebsd`, `openbsd`, `netbsd` excluding `android` and `ios`; plus `windows`). On Unix desktop, the child starts with `SysProcAttr{Setpgid: true}` and `StopGame()` signals the negative process group (`SIGTERM`, escalating to `SIGKILL` after 500 ms until `kill(-pgid, 0)` returns `ESRCH`) so `go tool`, `nsgo dev`, and the compiled game binary are all terminated with zero orphans. On Windows, `StopGame()` runs `taskkill /T /F /PID` with `Process.Kill()` fallback. Mobile and Web targets compile against `run_game_unsupported.go` returning `ErrRunGameUnsupported` so `nsgo check -target macos,windows,linux,android,web` builds cleanly.
- **Optional start-at-open-scene handoff**: `cmd/game dev` accepts `-scene <path>` and propagates `NSGO_GAME_SCENE` (`hotreload.SceneEnv`, read via `hotreload.DevScene()` with a release-tag stub returning `""`). When `RunStartAtOpenScene` (or `RunGameOptions.StartAtOpenScene`) is enabled, `RunGameWith` passes both `-scene <relPath>` and `NSGO_GAME_SCENE=<relPath>` for the currently open project scene.
- **UI-goroutine safety and persistent status/error history**: Streamed stdout (`OutputStdout`, `OutputInfo`) and stderr (`OutputStderr`, `OutputError`) lines append under `outputPanelState.mu` (capped at 2000 entries) and notify the UI solely through `core.Scope.Post` using a mutex-guarded scope reference (matching `problemState`), preventing data races and deadlocks during `StopGame()`. Explicit error paths (`report`, `Report`, Save/Undo/Redo/Run errors) always append a new `OutputError` entry even for consecutive identical errors, while `notify()` captures changed `Editor.Status` and `Editor.BuildErr` values automatically without duplicating unchanged statuses across ticks.
- **`file:line` navigation and keyboard shortcuts**: `parseFileLine` extracts `file:line[:col]` references (handling project-relative paths, absolute paths inside the project, and Windows drive letters while ignoring timestamps and URLs). Clicking a row calls `OpenOutputEntry`, which opens project scenes (`*.scene.yaml`, `*.scene.yml`, or `autoload.scenes`) in the editor and selects non-scene files in the file browser. `CmdRunGame` (`mod+r`) launches `RunGame()` when a project is open, `EditorView` binds `shift+f5` to `StopGame()`, and `CmdPlayStop` (`mod+p` / `f5`) toggles Play-in-editor consistently ([editor F5 Play-in-editor fix](../bug-fix/2026-11-28-editor-f5-play-in-editor.md)); `EditorView` also calls `StopGame()` on scope cleanup and `OpenProject`.

## Alternatives considered

- **Killing only `cmd.Process.Kill()` instead of process groups** — Simpler and requires no platform-specific build-tagged files. Rejected because `go tool game dev` spawns `go tool nsgo dev`, which in turn spawns the running game binary; killing only the top-level PID leaks descendant processes and keeps stdout/stderr pipes open indefinitely.
- **Replacing the in-editor `Play` button instead of adding separate `Run` / `Stop Game` controls** — Keeps the toolbar narrower with a single play action. Rejected because issue #910 explicitly preserves the fast in-editor `Play` preview alongside launching the full game process.
- **Logging editor messages only when callers invoke a new `LogError` API** — Avoids inspecting `Editor.Status` in `notify()`. Rejected because dozens of existing editor operations report status through `e.Status = ...; e.notify()`; capturing status transitions in `notify()` plus unconditional logging in `report()` / `reportStatusError()` gives complete history without rewriting every caller.

## Consequences

- **Benefits**:
  - Authors can run and stop their real game from the editor (`Run` / `mod+r`, `Stop Game` / `Shift+F5`), inspect live stdout/stderr alongside persistent editor error history, filter to errors, clear logs, and click `file:line` lines to jump straight to scenes.
  - Injectable `Editor.RunGameCommand` makes UI and lifecycle tests fast and deterministic, while a real fixture project `go tool game dev` test verifies live streaming and zero leftover parent or grandchild processes.
- **Costs and known limits**:
  - Keeping the Output panel open by default adds a 140 pt bottom dock (toggleable via the toolbar **Output** button).
  - `OpenOutputEntry` opens the target scene file in the editor but does not scroll a text editor to the exact line number since the scene editor edits structured scene views rather than raw YAML text.

## Verification

- `(cd packages/game && env -u GOROOT go tool nsgo test ./editor/...)`
- `(cd packages/game && env -u GOROOT go tool nsgo test ./cmd/game)`
- `(cd packages/game && env -u GOROOT go tool nsgo test ./tooling/hotreload)`
- `(cd packages/game && env -u GOROOT go tool nsgo test . -- -run 'TestStabilityTable|TestAgentSkillValid')`
- `(cd packages/game && env -u GOROOT go tool nsgo check -target macos,windows,linux,android,web)`
- `env -u GOROOT go run ./tools/changelog check && bun run verify-notes`
