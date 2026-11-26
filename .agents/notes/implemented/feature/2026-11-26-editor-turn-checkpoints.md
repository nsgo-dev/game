# Agent Note: Editor persistent turn checkpoints under `.nsgo/checkpoints/`

Status: implemented

## Problem

The editor's in-memory `History` command stack only tracks linear undo steps for the current session and only covers commands executed inside the open scene tab. When an agent turn edits multiple files across a project (scenes, prefabs, data tables, `game.yaml`, Go code, and binary media), or when an autonomous task or automation runs, restarting the editor drops the undo stack and leaves no built-in way to preview or restore every file a turn touched back to its pre-turn bytes.

## Decision

The editor stores a persistent checkpoint for every agent turn, autonomous task, and automation under `.nsgo/checkpoints/<id>/` inside the project root, ignored by Git (writing `.nsgo/checkpoints/.gitignore` and `.nsgo/.gitignore` when absent):

- **On-disk layout**: each checkpoint directory holds a strict versioned YAML manifest (`manifest.yaml`, `version: 1`, `editor.CheckpointVersion`, rejecting unknown fields, duplicate keys, trailing documents, invalid paths, and invalid SHA-256 digests) plus verbatim pre-turn file bytes under `files/<relative-path>`. Binary media is copied verbatim with no conversion, and non-existent files created by a turn are recorded with `existed: false` so restoring removes them.
- **Lazy first-write capture**: before a turn first writes any project file (either via `WriteAgentFile`/`DeleteAgentFile`/`RecordCheckpointFile` or via `History.beforeCommand` intercepting scene/project commands and `editormcp` `ActionEdit` calls), the editor captures that file's pre-turn bytes (verbatim disk bytes when clean, or canonical in-memory YAML if an open document already has unsaved edits). Subsequent writes to the same file in the same turn are no-ops, preserving the original pre-turn state. You-only files (`MatchYouOnlyPath` / `CheckYouOnlyTarget`), `.nsgo/`, `.git/`, symlinks, and files exceeding `32 MiB` are strictly refused.
- **Pre-start checkpoints for tasks and automations**: `TakeCheckpointBeforeTask`, `RunTaskWithCheckpoint`, `launchTaskRunner` in `tasks.go`, `TakeCheckpointBeforeAutomation`, `RunAutomationWithCheckpoint`, `editormcp`'s `game_playtest`, and automation-mode edit calls persist a checkpoint before work begins so partial or crashed runs remain restorable.
- **Size cap**: `Editor.CheckpointMaxBytes` (default `128 MiB`) prunes the oldest completed checkpoints while keeping at least the newest checkpoint and never deleting an active turn.
- **History panel and one-step undoable restore**: `HistoryPanelView` (`⌘Y`, `CmdHistoryPanel`) renders `UnifiedHistoryEntries` combining persisted checkpoints, current-session edits, saved points, and Git milestones by day with `All` / `You` / `Agent` filters, read-only viewport preview (`ActiveScene()` shows the checkpoint's scene snapshot while editing is off; `Esc` exits preview), and `RestoreCheckpoint` executed as a single `fallibleCommand` (`checkpointRestoreCmd`) that verifies byte length and SHA-256 digests, atomically restores all touched files on disk, synchronizes open scene tabs and `game.yaml` without triggering disk-conflict banners, and rolls back every file and open editor state on one `Undo()`.
- **MCP surface**: `editormcp` exposes `checkpoint_list` (`ActionRead`) and `checkpoint_restore` (`ActionEdit`, evaluating per-file `edit`/`code`/`delete` permission rules and You-only boundaries via `targets`) and automatically wraps file-mutating `ActionEdit` calls in panic- and cancel-safe turn checkpoints (preserving partial checkpoints on error/panic and discarding empty turns).

## Alternatives considered

- **Storing checkpoints in Git stash or shadow commits**: Git repositories may not be initialized in new projects, and creating Git objects/refs can interfere with hooks, worktrees, or user workflows. Plain files under `.nsgo/checkpoints/` work identically with or without Git and stay strictly ignored.
- **Content-addressed custom binary pack or `encoding/gob` archive**: Violates the repository's plain-text format rule (`AGENTS.md`), prevents human/AI inspection and diffing of manifests, and adds custom binary parsing risk. A versioned YAML manifest alongside verbatim files under `files/` keeps manifests human-readable and media byte-exact.
- **Full-project snapshots per turn**: Copying the entire project on every turn would quickly blow through the disk budget on media-heavy projects. Capturing each file lazily before its first write in a turn keeps checkpoints tiny and proportional to the turn's actual footprint.

## Consequences

- **Benefits**: Every agent turn, task, and automation is restorable byte-for-byte across editor restarts, across all file types (YAML, Go, and binary media), and `Restore` integrates cleanly into `Undo`/`Redo` as a single step. Checkpoints are inspectable YAML manifests with JSON Schema validation (`checkpoint.v1.json`) and SHA-256 integrity checks.
- **Costs**: Restoring a checkpoint rewrites touched files on disk immediately (to support non-scene files like prefabs and Go code) and synchronizes open tabs; if disk writes fail mid-restore, `checkpointRestoreCmd` rolls back already-written files. Large media files touched repeatedly in separate turns consume disk space until the size cap drops older checkpoints.
