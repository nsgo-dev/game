# Agent Note: Rewinding an agent thread to before a message

Status: implemented

## Problem

Issue #1021 implements the "Rewind the conversation · visible undo" workflow from `docs/design/editor/Checkpoints.dc.html` on top of the editor's Agent panel ([connecting external agents](../architecture/2026-11-26-editor-agent-connect.md)). Authors need to step back to before any earlier sent message in a thread, pick whether to restore **Files and conversation**, **Files only**, or **Conversation only**, edit the returned prompt in the composer, continue the agent session from before the dropped turns, or click **Redo** to bring both files and conversation back byte for byte.

Two design constraints shaped the implementation:

1. Project-wide turn checkpoints (#1018) and persisted multi-thread files (#1019) are not yet merged on `main`, so rewind must capture accurate, symlink-safe multi-file checkpoints per turn now while staying decoupled from on-disk checkpoint storage.
2. Restoring files on disk must keep open scenes, inactive scene tabs, `game.yaml`, the disk-change watcher, and the file tree synchronized without triggering false external-conflict prompts.

## Decision

### Compact per-turn checkpoints (`agent_rewind.go`)

- Before launching a composer turn in `Editor.SubmitAgentPrompt`, the editor captures a pre-turn snapshot (`captureTurnStartCheckpoint`) of regular files under `agentProjectRoot()` (skipping `.git`, `.nsgo`, `node_modules`, `.svn`, `.hg`, and symlinks, with per-file and total byte caps) along with cloned in-memory `Doc` and `Project` snapshots and `SessionIDBefore`.
- When the turn finishes (`finishAgentRun`) or is stopped (`StopAgentRun`), `finalizeTurnCheckpoint` computes the exact diff against the pre-turn scan, retains only the changed/created/deleted file entries (`beforeFiles` and `afterFiles`), drops the full baseline map to keep memory bounded, records sorted `AgentTurn.ChangedFiles`, and records `AgentTurn.SessionIDAfter`.
- Checkpoints live in `agentState.checkpoints[turnID]` rather than directly inside `AgentTurn` so `Editor.AgentTurns()` never clones file byte slices during UI renders.

### Three rewind modes, atomic rollback, and Redo (`RewindAgentTurn` / `RedoAgentRewind`)

- Rewinding to before `turnID` merges the earliest `beforeFiles` entry across turns `[idx..len-1]` so multi-turn rewinds restore every touched file to its exact state immediately before `turnID`.
- Before writing or removing any file on disk, `snapshotDiskFiles` captures the current on-disk state into `agentRewindRecord.redoFiles` and `resolveSafeCheckpointPath` rejects relative escapes, protected directories, and symlinks on any path segment. `applyFileSnapshots` writes atomically via `writeFileAtomic`, prunes empty directories created by rewound turns without ever removing the project root, and rolls back applied files if any I/O operation fails partway.
- `syncEditorAfterFileRestore` reloads modified open scenes, inactive scene tabs, and `game.yaml`, refreshes `diskWatch` snapshots (`recordSceneDiskSnapshot` / `recordProjectDiskSnapshot`) so no spurious conflict banner fires, rebuilds the active scene, clears stale object selections, and refreshes project files, changes count, and problems.
- Mode behavior:
  - `AgentRewindBoth`: restores files and in-memory scene/project state, truncates `agent.turns` to `[:idx]`, and restores `agent.sessionID` to `target.SessionIDBefore`.
  - `AgentRewindFilesOnly`: restores files and in-memory scene/project state while leaving `agent.turns` and `agent.sessionID` unchanged.
  - `AgentRewindConversationOnly`: truncates `agent.turns` to `[:idx]` and restores `agent.sessionID` while leaving disk files untouched.
- In all three modes, the target turn's prompt returns to the composer with `AgentComposerRewound()` set (highlighting the composer card with `SpecPalette.Accent.Edge`), and `AgentRewindBanner()` exposes the post-rewind summary and **Redo** action (`RedoAgentRewind`). Sending a new prompt clears the Redo record and forwards `AgentRequest.SessionID` (the restored `SessionIDBefore`) plus `AgentRequest.PriorTurns` so the agent session continues from before the dropped turns.

### UI & Spec tokens (`agent_ui.go`)

- Hovering or keyboard-focusing any sent user message reveals **Rewind to here** (`lucide.Undo2`), which toggles the `"Rewind to before this message"` dialog card (`RoleDialog` + `RoleRadioGroup`, dismissed by **Cancel** or `Esc`).
- All colors come exclusively from `SpecPalette` (`SpecPaletteFor(dark)`), including `p.Accent.OnFill` on `p.Accent.Fg` (weight 600) for the primary **Rewind** button so Dark and Light appearances both satisfy WCAG 2.2 AA contrast.

## Alternatives considered

### Drive file rewind exclusively through `Editor.History` commands

Reusing `Editor.History` avoids snapshotting files on disk, because scene edits made through editor commands are already undoable. It is rejected because coding agents routinely modify multiple files outside the open scene (prefabs, particle definitions, scripts, data tables, and newly created files) directly on disk during a turn; `Editor.History` only tracks in-memory editor commands and cannot undo arbitrary multi-file disk mutations or keep them separate from conversation-only rewinds.

### Persist rewind checkpoints into `.nsgo/checkpoints` immediately

Writing checkpoints directly to `.nsgo/checkpoints` would align upfront with issue #1018. It is rejected here because #1018 owns the versioned on-disk checkpoint file schema and History-panel preview/restore UX; keeping #1021's turn checkpoints in memory behind `agentTurnCheckpoint` avoids inventing a duplicate on-disk format that would conflict with #1018 when both merge.

## Consequences

- **Benefits**: Authors can rewind from any earlier message in a thread across all three modes, undo multi-file agent turns byte for byte (including newly created and deleted files), keep open scenes and tabs synchronized without false disk-watch conflicts, continue the external agent session from the rewound point, and recover instantly with **Redo**.
- **Costs**: Turn checkpoints exist in memory for the current editor session until #1018 provides persistent `.nsgo/checkpoints` storage, and each turn boundary performs a bounded directory walk of the project root (capped at 16 MiB per file and 64 MiB total).

## Testing

- `packages/game/editor/agent_rewind_test.go`: tests **Files and conversation** rewind and **Redo** across two file-changing requests, **Files only** (keeping the thread) and **Conversation only** (keeping the files), rewinding to before the very first message, open-scene reload and session continuation on subsequent prompts, Cancel/Play guards, and Spec Dark/Light palette assertions.
- `packages/game/editor/editorcmd/agent_rewind_screenshot_test.go`: captures real native macOS screenshots of the Rewind popover and post-rewind banner + composer in Dark and Light appearances beside `docs/design/editor/Checkpoints.dc.html`.
