# Agent Note: Editor external disk watching, per-object scene merge, and save conflict guard

Status: implemented

## Problem

Scenes, animation clips, and `game.yaml` are plain text YAML files edited by humans, git operations, and coding agents while the editor has them open (#906). Prior to this change (see [scene files and unsaved tracking](./2026-11-25-editor-scene-files-unsaved.md)), the editor tracked unsaved edits and prompted before switching scenes or closing, but it never noticed external disk writes: clean files went stale until reopened, the file browser required manual Refresh, and `Save` silently overwrote changes made on disk since the file was opened or last saved.

## Decision

- **Content-hash disk snapshots (`diskSnapshot`)**: Whenever the editor opens, saves, reverts, reloads, or moves (`MoveFile`) the open scene, `game.yaml`, or the open clip, it records the file's cleaned OS path, raw bytes (used as the 3-way merge base for scenes), and SHA-256 hash. Comparing SHA-256 hashes avoids filesystem `mtime` granularity races in fast saves and tests.
- **Save conflict guard (`ErrDiskConflict`)**: Before writing, `Save` (atomically before writing either project or scene), `SaveFile`, `SaveProject`, and `SaveClipFile` compare the current on-disk SHA-256 hash against the recorded snapshot when writing back to the same open path. A mismatch records the external conflict, opens `save-conflict-dialog` in `EditorView`, and returns `*DiskConflictError` wrapping `ErrDiskConflict` without modifying the file. Explicit overwrite confirmation (`ConfirmOverwriteSave` / `ForceSave`) or acknowledging the disk version (`KeepLocalVersion`) clears the guard.
- **Cross-platform polling watcher (`diskWatchLoop` + `PollDiskChanges`)**: While a `core.Scope` is attached, a lightweight background goroutine polls the open scene, clip, `game.yaml`, and project tree using pure Go `os`/`io/fs` (matching `packages/game/tooling/hotreload`, so `-target ios` and non-desktop targets vet cleanly without platform-specific file-watch syscalls). The goroutine reads only mutex-mirrored targets and posts `PollDiskChanges` to the UI goroutine via `scope.Post` only when a hash or project tree listing actually changed, avoiding data races and idle UI wakeups.
- **Clean vs. dirty external reload policy**:
  - Clean files reload automatically as an undoable `History` step (batching multi-file git branch switches detected via `.git/HEAD` into a single `History.Group` step) and surface a dismissible `DiskReloadNotice` with object-level diff details.
  - Dirty files never reload under the author; instead, `diskWatchBannerView` (placed below `remotePlayBarView`, never adding controls to `editor-toolbar`) offers **Compare** (for scenes), **Reload from disk** (recording `"Unsaved edits before reload"` in `History`), and **Keep mine** (recording `"Changed outside the editor"` in `History` so Undo can visit the disk version while keeping local edits dirty and saveable).
- **3-way per-object scene merge (`computeSceneConflictEntries` / `mergeSceneDocs`)**: Compares `base` (disk bytes at open/last-save), `mine` (`Editor.Doc`), and `disk` per object ID. Objects touched on only one side are taken from that side automatically; objects touched on both sides expose per-object **Mine** / **Disk** choices and apply as one undoable `History` step.
- **Clip undo-safe saved snapshots**: `clipCmd` preserves `clipSaved` and `clipDisk` on open/new/close commands, and `saveClipFromPanel` updates `clipSaved` for `OnSaveClip`, keeping `ClipDirty()` accurate across save and undo.

## Alternatives considered

- **OS file-system notification libraries (`fsnotify` / FSEvents / inotify)**: Lower latency for huge directories, but introduces external dependencies or platform-specific build tags, can drop events across atomic rename writes (`writeFileAtomic`), and complicates iOS/Web cross-compilation. Polling 1–3 open files plus a skipped-dir walk every 250ms (configurable via `DiskWatchInterval`) matches `tooling/hotreload`, handles atomic renames reliably, and compiles identically everywhere.
- **Placing conflict controls in `editor-toolbar`**: Keeps notifications near Save, but violates the 2-row budget of `editor-toolbar` at 1120×680 and shifts canvas layout in existing tests. Rendering `diskWatchBannerView` in a zero-height `core.Show` row below `remotePlayBarView` keeps default layout unchanged.
- **Line-based text merge for YAML scenes**: Works for arbitrary text, but can yield invalid YAML or duplicate object IDs. Merging per `sceneformat.ObjectDoc` ID and validating with `sceneformat.Validate` guarantees every merged scene is structurally valid.

## Consequences

- **Benefits**: External edits by humans, git, or agents are never silently overwritten on save; clean files and the file browser stay live automatically; overlapping scene edits can be resolved object-by-object without leaving the editor; all reload/keep/merge operations remain undoable.
- **Costs**: Polling walks the open project directory (skipping `.`, `_`, and `testdata`) every `DiskWatchInterval` while a UI scope is attached, and `diskSnapshot` retains the raw bytes of the open scene/project/clip in memory for 3-way base diffing.
- **Verification**: `disk_watch_test.go` exercises clean auto-reload with diff notice, dirty save conflict (`ErrDiskConflict`, dialog Cancel and Overwrite), dirty conflict banner (**Reload from disk**, **Keep mine**, History labels and Undo), 3-way **Compare** per-object merge, clip and `game.yaml` watching/conflict guards, automatic file browser refresh, git branch-switch History grouping, and macOS window close prompting with dirty clips.
