# Agent Note: Editor autosave and status bar save status

Status: implemented

## Problem

Authors editing scenes in the NSGo Game Editor had to remember to press `⌘S` after every edit, and had no ambient status bar confirmation of when changes landed on disk or why a write failed. Meanwhile, edits inside an open review (such as cutscene, playtest proposal, safe rename, or agent reviews) are not kept yet and must not overwrite disk prematurely. When saving fails (for example on a read-only folder or a full disk), silently dropping the error or showing an ephemeral toast risks losing work and lets a connected MCP agent continue building on edits that are not on disk.

## Decision

1. **Per-user `autoSave` preference (`EditorSettings.AutoSave`)**:
   - Stored as an optional `*bool` (`yaml:"autoSave,omitempty"`) in per-user `editor.yaml` alongside `language` and `appearance` (see [editor i18n and appearance](2026-11-25-editor-i18n-and-appearance.md)).
   - Omitting `autoSave` defaults to enabled (`EditorSettings.AutoSaveEnabled() == true`), while explicit `true`/`false` values round-trip byte-for-byte through `ParseEditorSettings` and `EncodeEditorSettings`.
   - Exposed in **Editor Settings › General › Save automatically** via `Editor.AutoSave()` / `Editor.SetAutoSave(bool)`.

2. **Debounced, review-gated asynchronous autosave (`autosave.go`)**:
   - Kept edits to file-backed scenes and `game.yaml` arm a 2-second debounce timer (`DefaultAutoSaveDelay = 2s`, overridable via `Editor.AutoSaveDelay`). Untitled scenes without `FilePath` or `OnSave` are skipped so autosave never spams `ErrNoSceneFile` failures.
   - `Editor.HasOpenReview()` pauses autosave whenever any review is active (`BeginReview`/`KeepReview`, cutscene review, playtest proposal review, safe rename review, merge conflict screen, or unresolved external disk conflict); closing and keeping the review resumes scheduling.
   - Before writing, the editor snapshots immutable YAML bytes on the editor goroutine, checks external disk conflicts, and runs atomic file writes off the editor goroutine (`SaveFileWriter` / `writeFileAtomic`). Completion posts back to the UI goroutine (`core.Scope.Post`), or buffers in a mutex-guarded slot for headless `StepAutoSave()` / `WaitAutoSave()`.
   - A 300 ms indicator timer (`DefaultSavingIndicatorDelay = 300ms`, overridable via `Editor.SavingIndicatorDelay`) switches `SaveStatus().Kind` to `SaveStatusSaving` only if the write is still in flight at 300 ms, so fast local saves never flicker to `Saving…`.

3. **Persistent status-bar failure and Spec tokens (`autosave_ui.go`, `scenefile_ui.go`, `tabs_ui.go`)**:
   - The status bar renders three Spec-token states: muted `Saved` with a hover tooltip showing `HH:MM:SS`, `Saving…` with a 12 px arc indicator after 300 ms, and a persistent `RoleAlert` failure pill (`Couldn't save: folder is read-only`, `Couldn't save: disk is full · <free> free`, or the concise underlying error) with **Retry** (`Editor.RetrySave`) and **Save as…** actions.
   - Failed autosaves stay in the status bar and never pop a toast, while explicit `⌘S` saves continue reporting errors through the existing explicit-save path.
   - While `SaveFailed()` is true, the unsaved dot beside the scene name and on the failing scene tab turns Spec problem red (`#FF6B6B`), and `Editor.HasUnsavedChanges()` remains `true` so closing or quitting asks first.

4. **MCP agent warning (`editormcp/server.go`, `editormcp/automation.go`)**:
   - `Server.call` checks `Editor.SaveStatus()` after each tool invocation. While `Kind == SaveStatusFailed`, it appends a clear `"Warning: edits are not on disk — ..."` message to the tool's text output, populates `Result.SaveWarning` / `AutomationResult.SaveWarning`, and sets `nsgo.dev/editsNotOnDisk`, `nsgo.dev/saveError`, and `nsgo.dev/saveWarning` in MCP `_meta`. Once a retry or manual save succeeds, the warning clears on the next tool call.

## Alternatives considered

- **Synchronous autosave on the UI goroutine**: Simpler than a background write goroutine, and avoids posting completions back to `core.Scope`. Rejected because a synchronous write blocks the UI goroutine for its full duration, making a real 300 ms `Saving…` anti-flicker threshold impossible to observe during slow disk writes.
- **Replacing the `*` dirty indicator only on explicit save failure**: Keeps the toolbar unchanged for autosave errors. Rejected because `Checkpoints.dc.html` explicitly specifies that the unsaved dot beside the scene name turns problem red while saving fails.
- **Injecting save warnings only into `scene_read` and `game_check`**: Narrower surface inside `editormcp`. Rejected because an agent calling `selection_get`, `scene_edit`, `viewport_image`, or any plugin-registered tool could still build on edits not on disk; attaching the check centrally in `Server.call` covers every tool uniformly without duplicating logic.

## Consequences

- **Benefits**: Kept edits persist automatically without manual `⌘S`, quick saves never flicker, unkept review drafts never clobber disk, failed saves stay visible with one-click recovery (**Retry** / **Save as…**), quitting cannot silently lose failed edits, and MCP agents stop building on unsaved state until disk writes recover.
- **Costs**: Adds two platform-tagged free-space helpers (`autosave_unix.go` / `autosave_other.go`) and a small mutex-protected handoff for off-goroutine disk writes; tests that assert on `toolbar-unsaved` or `scene-tab-dirty-*` see a 6×6 Spec dot instead of `*` (still preserving all TestIDs and toolbar height stability).
