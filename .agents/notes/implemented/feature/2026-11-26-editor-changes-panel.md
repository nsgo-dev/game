# Agent Note: Editor Changes and Commit panel with HEAD comparison

Status: implemented

## Problem

Although every authored file in an NSGo game project is plain text (YAML scenes, data tables, Go scripts) alongside binary media assets, the editor had no built-in way to review working-tree changes against `HEAD`, compare modified scenes object by object visually, audition newly added sound assets with their incoming references, check `nsgo check` / `nsgo test` status, or stage and commit a chosen subset of files with an agent-drafted message and PR description.

## Decision

Add a full-window **Changes and commit** screen (`ChangesPanelView`) and Git working-tree controller (`packages/game/editor/changes.go`, `packages/game/editor/changes_ui.go`) triggered from the status bar (`status-changes`), desktop `Project > Changes & Commit` menu, and Command Palette (`CmdChanges`), without adding buttons to the compact 2D `editor-toolbar`. Reuse `Editor.GitRunner`, `Spec.dc.html` color tokens, and the 2D scene preview primitives from the [scene merge conflict screen](2026-11-25-editor-scene-merge-conflicts.md).

## Architecture and contracts

- **Category grouping and exact Git porcelain parsing**: `RefreshChanges` runs `git -c core.quotepath=false status --porcelain=v1 -uall` through `runGitRaw` (preserving leading status column spaces that trimmed output drops) and groups changed files deterministically into `SCENES`, `DATA`, `CODE`, and `ASSETS`.
- **Per-kind Review layout against `HEAD`**:
  - **Scenes**: parses `git show HEAD:<path>` and the working-tree YAML with `sceneformat.ParseWith`, diffs every object by ID across layers and nested children (`added`, `changed`, `removed` with human field summaries such as `x 584 → 560`), and renders side-by-side `HEAD` and `Now` 2D scene previews (plus single-view `HEAD` / `Now` toggles) with dashed Spec outlines and per-object change badges.
  - **Data and Go code**: displays the latest `nsgo check` and `nsgo test` results and timestamp (via injectable `Editor.ChangesCheckRunner` or `go tool nsgo check` / `go tool nsgo test`) over a color-coded unified diff (`git diff --no-color -U3 HEAD -- <path>`, or full `+` lines for untracked files).
  - **Audio assets**: probes WAV (`sound.DecodeWAV`) or Ogg Vorbis headers for duration, sample rate, and a 60-bar waveform, collects incoming references from `refs.Scan` / `FindReferences` plus scene object properties and components, and auditions playback cleanly through `sound.Mixer` (stopping on panel close, Play mode, project switch, or scope cleanup).
- **Strict subset commits and History milestones**: `CommitSelectedChanges` stages only checked paths (`git add -A -- <included>`) and commits strictly those paths with `git commit --only -m <msg> -- <included>` when `HEAD` exists, guaranteeing that unchecked files—even if previously staged in Git's index—remain uncommitted. Successful commits append a `CommitMilestone` to `Editor.History` (`History.RecordMilestone` / `History.Milestones`) without disturbing the undo/redo command stack. No Git commit or push ever runs automatically without explicit user invocation.
- **Drafting and PR sharing**: `RedraftCommitMessage` invokes injectable `Editor.CommitMessageDrafter` or deterministic `DefaultCommitDraft` with provenance (`Drafted from N reviews and a playtest`) and attribution (`Made from N agent requests, reviewed and kept`). `CopyPRDescription` writes structured Markdown through `Editor.ChangesClipboard` or `core.Scope.SetClipboard`; `OpenPullRequest` enables when `gh` is on `PATH` or `Editor.GHRunner` is injected.

## Alternatives considered

### Stage via `git reset` + `git add` instead of `git commit --only -- <paths>`

Resetting the entire Git index before staging checked files is simpler to reason about for untracked files. This was rejected because it silently destroys any staged hunks or index state the author intentionally prepared on unchecked files outside the editor; `git add -A -- <included>` followed by `git commit --only -m <msg> -- <included>` commits exclusively the checked files while preserving unchecked files' working-tree and index state.

### Record commit milestones as no-op `Command` entries on `History.commands`

Pushing a commit marker onto the undo/redo stack would let `History` keep one slice. This was rejected because pressing `⌘Z` immediately after committing would consume a no-op milestone instead of undoing the author's last scene edit; storing milestones in a separate `History.milestones` slice keeps undo/redo semantics intact while exposing commit milestones directly on `Editor.History`.

### Add a Changes button to the 2D `editor-toolbar`

Putting a Changes toggle in the top toolbar would match other panels. This was rejected because `docs/design/editor/VersionControl.dc.html` places Changes in the status bar (`Status bar › Changes`) and the 2D toolbar must stay compact (at most 2 rows at `1120×680`).

## Consequences

- **Benefits**: Authors can visually inspect every changed scene, data table, Go file, and sound asset against `HEAD`, commit exact file subsets safely, track commit milestones in `History`, run `nsgo check`/`nsgo test`, and copy or open PR descriptions directly from the editor.
- **Costs**: `ChangesPanelView` is an additional full-window screen alongside `StartScreenView` and `MergeConflictView` that editor commands guard against (`!e.ChangesOpen()`), and `RefreshChangesCount` invokes lightweight `git status --porcelain` when opening or saving a project inside a Git repository.

## Verification

Verified with `env -u GOROOT go tool nsgo check` (desktop and `-target ios`) on `./editor` and `./editor/editorcmd`, `TMPDIR=/tmp env -u GOROOT go tool nsgo test ./editor`, real Git fixture repository partial-commit tests, real native macOS screenshots via `go tool nsgo test -target macos -screenshots ... ./editor/editorcmd` visually inspected against `VersionControl.dc.html`, `go run ./tools/changelog check`, `gofmt`, and `bun run verify-notes`.
