# Agent Note: Live problem detection on every edit and agent-proposed fixes

Status: implemented

## Problem

The editor's Problems panel ([2026-11-25-editor-problems-panel.md](2026-11-25-editor-problems-panel.md)) previously ran `assetcheck.CheckWith` and `project.Check` against disk only after opening, saving, or moving files, and `assetcheck.Problem` carried only the 1-based line without the column. Authors and coding agents could rename an object referenced by a connection (for example in an inherited or instanced scene) and see no warning until saving, had no live visual cue on the offending object in the viewport or scene tree, had no way to ignore a problem until the next edit to that file, and had no safe channel for an agent to propose a fix with a diff that only the person can apply (#1036).

Implementing live unsaved checks with agent-proposed fixes introduces four structural constraints:

1. **Checking unsaved open documents off the UI goroutine**: `assetcheck.CheckWith` and `project.Check` read through an `fs.FS`, while `Editor` state (`Doc`, `tabs`, `Project`, `tileset`) is owned by the UI/caller goroutine.
2. **Validating cross-scene fixes before either scene is saved**: when `boss_room.scene.yaml` inherits or instances `doors.scene.yaml` and `door_3` is renamed to `gate` in memory without saving, applying a connection fix `to: gate` in `boss_room.scene.yaml` must resolve `doors.scene.yaml` from the editor's unsaved tab rather than stale disk contents.
3. **File-scoped Ignore semantics**: ignoring a problem must hide it across the Problems panel, status bar, scene tree badge, and viewport overlay, keep it hidden when unrelated files change, and restore it automatically on the next edit to the problem's own file if the problem still holds.
4. **Hard safety boundary on MCP**: the agent may read live problems and propose a validated fix, but no MCP tool may ever apply a fix or mutate a scene on the problem-fix path.

## Decision

Extend `packages/game/tooling/assetcheck`, `packages/game/editor`, and `packages/game/editor/editormcp` with four coordinated mechanisms:

### 1. Column tracking in `assetcheck.Problem`

Add `Column int` and `Location() string` (`file:line:column` when both are known, `file:line` when only line is known, else `file`) to `assetcheck.Problem`, preserving `Problem.String()` (`file:line: message`) for CLI compatibility. `assetcheck/scene.go` adds `yamlPos` returning both 1-based line and column, and preserves `Column` from `*sceneformat.ComponentError`, `*sceneformat.ConnectionError`, `*sceneformat.FieldError`, and `*loaderr.Error`.

### 2. Debounced overlay-FS live checking on every edit (`problems.go`, `tileset_editor.go`, `connections.go`)

- `Editor.afterMutate()` calls `scheduleLiveProblemCheck()`, which captures immutable YAML snapshots of every dirty open document (`unsavedProjectFiles()`: dirty scene tabs via `sceneformat.Encode` with lenient YAML fallback when a dirty doc currently violates `Validate`, plus dirty `game.yaml` and dirty tilesets) on the editor goroutine, then schedules a debounced `time.AfterFunc` (default 80 ms, configurable via `Editor.ProblemCheckDebounce`) that runs `runProjectCheckWithOverrides` off the UI goroutine over an `overlayFS` layering those snapshots atop `os.DirFS(dir)`.
- Generation sequencing (`gen++`) drops stale runs; `WaitForProblems()` waits for both pending debounce timers and in-flight checks; `EditorView` cancels pending timers in `cx.Cleanup`.
- `liveResolver()` resolves dirty open scene tabs in `Scene(p)` before falling back to `e.Resolver`, and `validateConnections` uses `liveResolver()` so proposing and applying fixes across unsaved inheriting/instancing scenes succeeds without saving first.

### 3. Content-hash Ignore, object attribution, and Spec UI (`problems.go`, `problems_fix.go`, `problems_ui.go`, `scenetree.go`, `viewport.go`)

- `IgnoreProblem(p)` records the SHA-256 hex digest of `p.File`'s live bytes (unsaved snapshot if dirty, else disk). On every check, any ignored entry whose file's current digest differs from the recorded digest is evicted, so editing another file keeps `p` hidden while any new edit to `p.File` immediately restores `p` if it still holds.
- `openSceneObjectProblems()` caches active-scene problem-to-object attribution by `(revision, problemVersion, ScenePath)` across connection diagnostics (attaching broken target errors to the live source object), component/override errors, and YAML object line spans. `SceneTreeView` renders a Spec problem row fill and alert-icon count badge (`scene-tree-problem-<id>`), and `DrawViewport` calls `drawProblemOverlays2D` to draw the 2 px solid Spec Problem outline and filled alert pill (`Problem.Fg` / `Problem.OnFill`, alert triangle icon plus concise message) directly on live objects.
- `ProblemAgentPanelView` (docked on the right when `ProblemAgentPanelOpen()` is true, styled strictly with `SpecPalette` tokens) renders the problem card (`file:line:col`), explanation, `-`/`+` diff, **Apply fix**, **Show me**, **Ignore**, safety note, and **"Ask about this problem…"** composer.

### 4. Non-mutating MCP proposal and one-undo-step user application (`editormcp/problems.go`, `problems_fix.go`)

- `editormcp`'s `game_check` checks open unsaved documents and returns `column` alongside `file`, `line`, and `message`.
- `problem_propose_fix` (`ActionRead`, `ReadOnlyHint: true`, no `apply` flag in its strict schema) calls `Editor.ProposeProblemFix`, which validates the structured edit against a cloned scene document, computes the minimal before/after YAML diff, opens the Agent panel card, and returns `applied: false` without mutating any document, history, or disk file. No MCP tool applies a problem fix.
- Only `Editor.ApplyProposedFix` (invoked by the person pressing **Apply fix**) executes the edit as one undoable `History` step, shows a Spec `ToastSuccess` with an **Undo** action, and re-runs `CheckProblems()`. One Undo restores the previous YAML and brings the problem back.

## Alternatives considered

### Changing `assetcheck.Problem.String()` to emit `file:line:column: message` everywhere

Printing columns inside `Problem.String()` would surface columns in `go tool game check` automatically, but breaks existing CLI and `assetcheck` test assertions expecting `file:line: message`. Adding `Column int` plus `Location() string` gives the editor UI and `editormcp` exact `file:line:column` positions while keeping `Problem.String()` backward compatible.

### Tracking Ignore by editor revision number instead of per-file content hash

Clearing ignored problems on any `Editor.revision` increment would be simpler, but editing an unrelated file (or toggling selection/viewport zoom) would prematurely un-ignore problems in untouched files. Hashing the live content of `p.File` ensures only edits to `p.File` itself evict its ignores, covering both unsaved in-editor edits and external disk edits.

### Adding an optional `apply: true` parameter to `problem_propose_fix`

Mirroring `refactor_rename`'s `apply` boolean would give agents a shortcut to apply fixes directly. Rejected because #1036 explicitly mandates that the agent only proposes a fix and never applies one without the person, and requires an acceptance test proving no MCP tool applies a fix.

## Consequences

- **Benefits**: broken references and connections appear live before saving with 1-based line and column, live viewport Problem overlays, and scene tree badges; agents inspect live problems via `game_check` and propose validated diffs via `problem_propose_fix` without ever mutating project state; authors review and apply fixes in one undoable step with a Spec Undo toast, jump to the object and line with Show me, ask follow-up questions with the problem attached, or Ignore until the file changes again.
- **Costs**: each edit in an open project encodes dirty open tabs into an in-memory snapshot and schedules an 80 ms debounced background check goroutine; `overlayFS` must be maintained alongside any new editable project file formats added to the editor.
- **Verification**: `packages/game/editor/editormcp/problems_test.go` verifies the full rename-before-save -> live line/column -> MCP `game_check` -> MCP `problem_propose_fix` (non-mutating) -> `ApplyProposedFix` -> YAML check -> one Undo brings problem back flow plus the no-MCP-tool-applies-a-fix invariant; `packages/game/editor/problems_live_test.go` verifies Ignore/reappear semantics, viewport overlay drawing, scene tree badges, Show me, Ask about this problem, and UI Apply/Undo; `packages/game/editor/editorcmd/problem_screenshot_test.go` captures real macOS `nsgotest` screenshots in Dark and Light appearances beside `docs/design/editor/Problem.dc.html`.
