# Agent Note: Editor Start Screen, per-user recent projects YAML, and dynamic template creation

Status: implemented

## Problem

Running `go tool game editor` without arguments previously jumped straight into an in-memory Demo Room with no way to browse recently opened projects, open an existing project folder through a picker, or scaffold a new project from `game new`'s templates without dropping to a terminal. Switching projects also had no File-menu workflow or unsaved-edits confirmation prompt, and there was no per-user recent-projects file format outside project directories.

## Decision

1. **Strict versioned per-user YAML format (`packages/game/editor/recent_projects.go`)**:
   - `RecentProjectsFile` (`RecentProjectsVersion = 1`) stores `version` and `projects` (`name`, `path`, `lastOpened` in UTC RFC3339) at `<UserConfigDir>/nsgo-game/recent-projects.yaml` (`DefaultRecentProjectsPath`), outside any game project.
   - `ParseRecentProjects` enforces `KnownFields(true)`, rejects duplicate keys, trailing documents, missing/zero/future `version`, and duplicate paths; `EncodeRecentProjects` writes deterministic 2-space YAML that passes `formattest.Run` (`packages/game/editor/conformance_test.go`, `docs/en/formats.md`, `docs/zh/formats.md`).
   - `packages/game/editor` is removed from `notFormats` in `packages/game/conformance_test.go` and `editor.RecentProjectsVersion` is registered in `packages/game/formats_test.go`.
   - `Editor.RecentProjectsPath` defaults to empty in `editor.New` so unit tests never mutate the developer's host config directory; `cmd/game/editor.go` sets it to `DefaultRecentProjectsPath()` (or a test temp path). `RecentProjects()` checks each directory and `game.yaml` on disk to flag missing folders (`Missing: true`).
2. **Dynamic template registry and shared `game new` runner (`packages/game/cmd/game/new.go`, `packages/game/cmd/game/editor.go`)**:
   - `cmd/game/new.go` exposes `createProject(opts, stdout, stderr)` shared by `game new` and the editor's Start Screen.
   - `cmd/game/editor.go` populates `Editor.SetProjectTemplates` directly from `projectTemplates` so new templates added to `game new` appear automatically in the Start Screen without editing `packages/game/editor`.
   - `Editor.StartCreateProject` runs `Editor.CreateProject` (defaulting to `go tool game new` on desktop) off the UI goroutine, streams stdout/stderr lines into the Start Screen output panel, and opens the created `game.yaml` on success.
3. **Start Screen UI and design tokens (`packages/game/editor/start_screen_ui.go`)**:
   - Follows `docs/design/editor/Start.dc.html`, step 1 of `FirstRun.dc.html`, and `Spec.dc.html` color/contrast tokens (including `#101216` text on `#FFFFFF` for the single primary **Create** button, hiding **RECENT** until at least one recent project exists, filter chips `All`/`2D`/`3D`, and live project-name validation with **Open it** / **Use `<name>-2`** actions for existing folders and parent writability checks).
4. **Clean project switching and unsaved-edits confirmation (`packages/game/editor/scenefile.go`, `scenefile_ui.go`, `project.go`, `start_screen.go`, `commands.go`, `menubar.go`)**:
   - `Editor` tracks dirty state across scene (`SceneDirty`), project (`ProjectDirty`), clip (`ClipDirty`), data table (`DataTableDirty`), and localizations (`LocalizationDirty`) via `HasUnsavedChanges()` and `SaveAll()`.
   - `File > New Project...` (`mod+n`), `File > Open Project...` (`mod+o`), `File > Start Screen`, and Start Screen open/create/demo actions route through `RequestOpenProject` / `RequestCreateProject` / `RequestOpenDemo`, which share `scenefile_ui.go`'s unified `unsaved-dialog` `AlertDialog` (`Save`, `Discard`, `Cancel`) whenever `HasUnsavedChanges()` is true.
   - `Editor.PickProjectFolder` provides an injectable folder-picker hook for tests and falls back to `core.Scope.PickDirectories` when nil.
   - `OpenProject` clears stale clip, data table, localization, play-mode, and running-game state and resets `Doc` to `DefaultSceneDoc()` when switching projects (or leaving the Start Screen) into a project with no `autoload.mainScene`.

## Alternatives considered

- **Moving embedded templates out of `packages/game/cmd/game/_templates/projects` into a new package** — Would let `packages/game/editor` embed templates directly, but `docs/en/templates.md`, `docs/zh/templates.md`, `template/README.md`, and `cmd/game/templates_test.go` all anchor template copies under `packages/game/cmd/game/_templates/projects` and `templates.go`. Keeping the registry in `cmd/game` and injecting it via `SetProjectTemplates` + `CreateProject` avoids duplicating embedded assets or breaking template contributor instructions while keeping `editor` decoupled from CLI embedding.
- **Auto-writing to `os.UserConfigDir()` inside `editor.New`** — Would persist recent projects without caller setup, but every headless unit test in `packages/game/editor` calling `OpenProject(t.TempDir())` would pollute the developer's real user config file. Making `RecentProjectsPath` explicit (wired automatically by `cmd/game/editor.go`) keeps library tests isolated and deterministic.
- **Blocking `OpenProject` directly on unsaved changes** — Would break existing programmatic callers and tests that replace projects directly (`TestOpenProjectDropsTheHistoryOfTheProjectBefore`). Keeping `OpenProject` direct and routing interactive UI/menu flows through `RequestOpenProject` preserves backward compatibility while protecting interactive edits.

## Consequences

- **Benefits**:
  - `go tool game editor` launches into a Start Screen aligned with the design artboards and Spec tokens, while `game editor <path>` continues opening projects and scenes directly.
  - Recent projects follow every repository YAML conformance gate (`formattest.Run`, `TestFormatsPage`, `TestFormatPackagesRunConformance`).
  - Templates registered in `cmd/game/templates.go` automatically appear in the Start Screen with zero editor changes.
- **Costs and known limits**:
  - The Start Screen's "Start with AI" composer displays a placeholder notice explaining that agent connection is a separate workflow; wiring the live agent connection flow remains in its own issue.
  - Non-desktop targets (Android/Web) cannot run `go tool game new` locally unless a host provides `Editor.CreateProject`.

## Verification

- `(cd packages/game && env -u GOROOT go tool nsgo test ./editor)`
- `(cd packages/game && env -u GOROOT go tool nsgo test ./cmd/game)`
- `(cd packages/game && env -u GOROOT go tool nsgo test . -- -run 'TestFormatPackagesRunConformance|TestFormatsPage|TestStabilityTable|TestAgentSkillValid')`
- `(cd packages/game && env -u GOROOT go tool nsgo check -target macos,windows,linux,android,web)`
- `env -u GOROOT go run ./tools/changelog check`
- `bun run verify-notes`
