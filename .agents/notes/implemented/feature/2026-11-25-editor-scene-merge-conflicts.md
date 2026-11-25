# Agent Note: Object-by-object scene merge conflict resolution in the editor

Status: implemented

## Problem

After the [scene Git merge driver](2026-11-25-scene-git-merge-driver.md) (#1058) stops on conflicting edits to a `*.scene.yaml` file, authors still had no visual way inside the editor to inspect only the conflicted objects, compare MINE/THEIRS/RESULT positions side by side, review or take an agent's field-combining suggestion, hand-edit object YAML safely, verify the resolved scene with `game check` and a short playtest, and finish or abort the Git merge. Furthermore, opening a project whose `mainScene` currently holds a `ConflictDoc` YAML report failed scene parsing instead of opening the conflict resolver.

## Decision

Add a dedicated full-window **Scene merge conflict** screen (`MergeConflictView`) and Git/session controller (`packages/game/editor/merge_conflict.go`, `merge_git.go`, `merge_conflict_ui.go`) backed directly by Git index stages (`:1:`, `:2:`, `:3:`) and `sceneformat.Merge`, paired with four `editormcp` tools (`merge_status`, `merge_resolve`, `merge_finish`, `merge_abort`) in `packages/game/editor/editormcp/merge.go`.

## Architecture and contracts

- **Three-way session from Git stages or in-memory docs**: `OpenMergeConflict` reads `:1:<path>` (base), `:2:<path>` (ours/mine), and `:3:<path>` (theirs) from Git along with branch, author, short SHA, and commit subject metadata from `HEAD`, `MERGE_HEAD`, and `MERGE_MSG`, then runs `sceneformat.Merge(base, ours, theirs)`. This works seamlessly both when the `nsgo-scene` merge driver left a `ConflictDoc` in the working tree and if Git left raw conflict markers. `OpenProject` detects when a `mainScene` parses via `sceneformat.ParseConflict` and transitions cleanly into the conflict resolver instead of failing.
- **Object-level grouping and clean MERGED BY ID separation**: `MergeResult.Conflicts` entries sharing an identity key (`object:<id>`, `layer:<name>`, `scene`, etc.) group into one `MergeConflictItem`. `MERGED BY ID` filters out items that appear in `CONFLICTS` so each object appears in exactly one section.
- **Deterministic built-in + pluggable Agent suggester**: `DefaultMergeSuggestion` inspects all three versions plus `AutoMerged` IDs to combine non-conflicting fields from both sides (`fields from both`), prefer positions coupled to newly added objects (such as a door referencing a newly added lever), preserve deliberate deletions with the incoming commit subject as context, and pick the higher `z` ordering when both sides raised `z`. Callers and MCP agents can override or update suggestions via `Editor.MergeSuggester` or `SetMergeAgentSuggestion` / `merge_resolve`.
- **Safe hand-edited YAML**: `ApplyMergeCustomYAML` parses either a bare YAML mapping or a 1-element YAML sequence with `KnownFields(true)`, enforces that the object `id` stays unchanged, and dry-runs `sceneformat.Validate` on the assembled `ResultDoc()` before accepting the edit.
- **Verification gate before Git commit**: `FinishMerge` validates and atomically writes canonical `sceneformat.Encode` YAML, runs `game check` (`runProjectCheck` or `Editor.MergeCheckRunner`) and a short 10-second (600-step at 60 Hz) deterministic headless playtest (`sceneformat.Build` + `BuiltScene.Update` + `PhysicsScene.Step` or `Editor.MergePlaytestRunner`), restores the working file on verification failure without touching Git, and only runs `git add` + `git commit --no-edit` once both checks pass. `AbortMerge` runs `git merge --abort` and reloads the restored scene.
- **Spec-token visual fidelity and native macOS screenshot harness**: `MergeConflictView` uses exclusively `docs/design/editor/Spec.dc.html` color tokens and WCAG contrast pairs (including `#101216` text on `#3B7BFF` accent buttons). Real macOS `nsgotest` screenshots live in `packages/game/editor/editorcmd/merge_screenshot_test.go`, which links `github.com/nsgo-dev/nsgo` so `go tool nsgo test -target macos` renders on the native Cocoa/Metal host rather than falling back to headless mode.

## Alternatives considered

### Parse only the working tree's `ConflictDoc` instead of Git index stages `:1:`, `:2:`, `:3:`

Reading only `ConflictDoc` avoids calling `git show` for three stages. This was rejected because `ConflictDoc` intentionally stores only conflicting field deltas (not the full `MineDoc` and `TheirsDoc` scenes needed to render complete `MINE`, `THEIRS`, and dual-ghost `RESULT` 2D scene previews, or to reconstruct a full `ObjectDoc` when switching between Mine, Theirs, and Agent).

### Dock conflict resolution inside the normal editor layout instead of a full-window screen

Keeping the standard scene tree, asset browser, and inspector visible would reuse the main viewport. This was rejected because `docs/design/editor/VersionControl.dc.html` (screen 2) defines a dedicated 3-column conflict screen (`CONFLICTS`/`MERGED BY ID`, `MINE`/`THEIRS`/`RESULT`, focused object options) and blocking normal scene edits during a merge prevents saving half-merged state over a conflict file.

### Run `go tool game playtest` as an external subprocess by default during `FinishMerge`

Shelling out to `go tool game playtest` would exercise project-specific bot suites automatically. This was rejected as the unconditional default because many projects (and minimal fixture repos) do not yet ship a compiled `playtest.yaml` suite; instead `FinishMerge` runs a built-in deterministic 10-second `sceneformat.Build` + `Update` + `PhysicsScene.Step` simulation by default and exposes `Editor.MergePlaytestRunner` so projects with custom bot playtests can plug them in cleanly.

## Consequences

- **Benefits**: Authors resolve scene merge conflicts visually object by object with live `MINE`/`THEIRS`/`RESULT` previews, reasoned agent suggestions, safe custom YAML editing, automatic `game check` + playtest verification, real 2-parent Git merge commits, clean `git merge --abort`, and full MCP agent parity.
- **Costs**: `MergeConflictView` is an additional full-window mode alongside `StartScreenView` that normal editor commands guard against (`!e.MergeConflictOpen()`), and projects without Git must open sessions programmatically via `OpenMergeConflictSession`.

## Verification

Verified with `env -u GOROOT go tool nsgo check` (host and `-target ios`) on `./editor`, `./editor/editormcp`, and `./editor/editorcmd`, headless `env -u GOROOT TMPDIR=/tmp go tool nsgo test` across `./editor`, `./editor/editormcp`, `./editor/editorcmd`, and `.` (`TestFormatsPage`), real end-to-end Git merge commit & abort tests, real macOS native screenshots via `go tool nsgo test -target macos -screenshots ... ./editor/editorcmd` visually inspected against `VersionControl.dc.html` and `Spec.dc.html`, `go run ./tools/changelog check`, `gofmt`, and `bun run verify-notes`.
