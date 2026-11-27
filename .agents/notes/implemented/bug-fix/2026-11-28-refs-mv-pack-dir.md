# Agent Note: Track installed pack folders in project.PathRefs and refs.PlanMove

Status: implemented

## Problem

`project.pathFields` (`pkg/project/paths.go`) listed `game.yaml`'s autoload paths and asset pipeline paths, but omitted installed library packs' folders (`packs[].dir`). As a result, `refs.PlanMove` (`game mv` and `editor.MoveFile`) moved an installed pack's folder or a parent directory containing it without rewriting `packs[].dir` in `game.yaml`, leaving `project.Check` reporting every pack file missing and breaking `game library remove` and pack updates (`nsgo-dev/game#1607`). Moving a single tracked file out of an installed pack folder also succeeded silently even though `packs[].files` still listed it under the old directory.

## Decision

Track installed pack directories through the single `project.pathFields` list and enforce pack-file ownership boundaries in `refs.PlanMove`:

- **Single `game.yaml` path list (`pkg/project/paths.go`)**: Export `FieldPackDir = "pack dir"` and include every `packs[i].dir` in `pathFields` with `dir: true, output: false`, in canonical `Encode` order after `autoload` and `assets`. Both `project.PathRefs` and `File.EachPath` follow `pathFields`, so `refs.Scan` records pack folders as directory references, `assetcheck` / `refs.Project.Check` verifies each pack folder exists, `refs.PlanMove` rewrites `packs[].dir` byte-for-byte in place when the pack folder or any ancestor directory moves, and `editor.MoveFile` updates in-memory `Editor.Project.Packs[i].Dir` via `EachPath` with full undo/redo.
- **Refuse moving individual tracked pack files (`pkg/refs/scan.go`, `pkg/refs/plan.go`)**: `refs.Scan` records each installed pack's scanned directory and tracked `files` paths whenever `game.yaml` declares packs. `refs.PlanMove` refuses moving a tracked file or a subfolder holding tracked files strictly inside an installed pack's folder (pointing the user to move the pack folder instead). Untracked user files placed inside a pack folder remain movable because they are not listed in `packs[].files`.
- **Post-edit `game.yaml` validation (`pkg/refs/plan.go`)**: Before returning a move plan, `PlanMove` validates any edited `game.yaml` with `project.Parse`, refusing moves that would cause two installed packs to claim the same file path or otherwise produce an invalid project file.

## Alternatives considered

- **Update `packs[].files` when a single tracked file moves**: Rejected for two reasons. First, `project.Validate` requires every entry in `packs[].files` to be a relative path inside `packs[].dir` (`isProjectPath` forbids `..`), so a file moved outside the pack folder cannot be represented in `packs[].files` without deleting it from the pack record and silently breaking `game library remove` and updates. Second, `packs[].files` must stay strictly sorted and unique; renaming a file inside the pack folder would require reordering YAML sequence items rather than in-place scalar edits, and would diverge from the pack's `pack.yaml` manifest.
- **Refuse moving any file or subfolder inside a pack folder, even untracked ones**: Rejected because `packs[].files` explicitly defines pack ownership (which `library.PlanRemove` and `project.Check` follow), so blocking moves of unrelated user files is unnecessary and surprises users who drop notes or scratch files beside pack assets.

## Consequences

- **Benefits**: Moving an installed pack folder or an ancestor directory with `game mv` or the editor rewrites `packs[].dir` in place while preserving comments, quoting, and canonical `game.yaml` round-trips, keeping `game check`, `project.Check`, and `game library` working. Attempting to split tracked pack files out of their pack folder fails fast before anything changes on disk.
- **Costs**: Users who want to extract a single file from an installed pack must copy it or remove the pack record rather than running `game mv` on that individual file. Because `packs[].dir` is a non-output `Ref.Dir`, `refs.Project.UsedBy` and `UnreferencedAssets` treat files inside an installed pack folder as referenced by `game.yaml`.
