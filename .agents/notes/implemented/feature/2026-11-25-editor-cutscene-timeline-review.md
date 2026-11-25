# Agent Note: Editor cutscene timeline and agent change review

Status: implemented

## Problem

Version 2 timeline cutscenes (`*.cutscene.yaml`) implemented in [cutscenes-timeline-yaml](./2026-11-25-cutscenes-timeline-yaml.md) can be authored as YAML and played by `cutscene.TimelinePlayer`, and the editor MCP server lets coding agents read and edit project files, but the editor has no timeline UI to preview, scrub, tune, or review an agent's cutscene changes before writing them to disk.

Implementing `Animation.dc.html` Screen 2 introduces three cross-cutting invariants that need explicit contracts:

1. **Runtime-exact scrubbing**: scrubbing the editor timeline must pose the scene identically to `cutscene.TimelinePlayer.Seek(t)` rather than re-implementing camera/actor math in UI code.
2. **Valid partial change reverts**: `cutscene.Validate` forbids overlapping clips on non-sound tracks (camera, actor, dialogue, music). When an agent retimes consecutive camera clips (e.g. extending `Hold on door` from `1.0s` to `1.6s` and shifting `Pan` and `Follow`), reverting one clip independently would produce an invalid overlapping cutscene.
3. **Single-undo-step commit with review-joined manual edits**: while a review is open, manual edits (`±0.1 s` timing or easing tweaks) must join the review and be individually undoable via `⌘Z` without closing the review, while pressing **Keep N of M** must commit all kept agent changes plus any remaining manual edits to both memory and disk as **one atomic undo step**.

## Decision

### Runtime-backed timeline preview and thumbnail sampling

`Editor` owns `cutsceneEditorState` (`packages/game/editor/cutscene.go`) and renders `CutscenePanelView` (`packages/game/editor/cutscene_ui.go`) as a full-workspace view when a cutscene is open.

- `refreshCutscenePreview` clones the target `sceneformat.SceneDoc`, builds an isolated `*sceneformat.BuiltScene`, loads project localization tables (`loc/*.yaml`, `locales/*.yaml`, `Editor.l10n`, or `Editor.CutsceneCatalog`), and constructs a real `cutscene.TimelinePlayer`.
- `SeekCutscene` and `StepCutscene` drive that player directly so `CutscenePreviewScene()`, `CutsceneActiveDialogue()`, and `CutsceneCameraLabel()` match game runtime playback exactly.
- The 24-frame scrub strip is sampled on a separate lightweight `SceneDoc` clone using `cutscene.TimelineTarget{Doc: ...}` so thumbnail generation never mutates the live preview scene.

### Conflict-safe change grouping (`DiffCutsceneChanges`) and review composition

`DiffCutsceneChanges(before, after)` matches tracks by explicit ID, actor target, or track kind ordinal, and matches clips by clip ID or track-local key:

- Whole added or removed tracks become one `CutsceneChange` (e.g. adding the `slime_01` actor track with `Drop` and `Patrol` is one revertible change).
- On non-sound tracks, modified/added/removed clips whose `[min(beforeStart, afterStart), max(beforeEnd, afterEnd)]` intervals overlap are merged via union-find into one atomic `CutsceneChange`. This guarantees every subset of reverted/restored changes composes into a valid, non-overlapping cutscene accepted by `cutscene.Validate`.

### Two-layer undo for review-joined edits and single-step Keep

- While `e.cutscene.review != nil`, manual clip edits (`SetCutsceneClipTiming`, `SetCutsceneClipEasing`) record snapshots on a review-local undo/redo stack (`review.editUndo` / `review.editRedo`).
- `Editor.canUndo()`, `Editor.canRedo()`, `Editor.Undo()`, and `Editor.Redo()` consult the review-local edit stack first while a review is open. Pressing `⌘Z` undoes only the user's review edit while keeping the review open, and leaves zero stale review-scoped commands on `Editor.History`.
- Pressing **Keep N of M** (`KeepCutsceneReview`) composes `base` + kept changes + remaining manual edits, validates and encodes canonical YAML, and executes one `cutsceneCommitCmd` (`fallibleCommand`) on `Editor.History`. Its `do()` writes the canonical file atomically and closes the review; its `undo()` restores the exact pre-review document and disk bytes in one step.

### MCP tools and Spec tokens

- `packages/game/editor/editormcp/cutscene.go` registers `cutscene_write` (`ActionEdit`, opening a pending review without modifying disk by default when updating an existing cutscene or when `prompt` is provided, and refusing invalid YAML or symlink escapes) and `cutscene_scrub` (`ActionRead`).
- `CutscenePanelView` uses only `Spec.dc.html` color tokens, enforces `#101216` (`specBg`) text on `#3B7BFF` (`specAccent`) for the primary Keep button (4.88:1 AA), and draws dashed borders (`SetLineDash(4, 3)`) for added/changed clips and solid borders for selected clips so overlays never rely on hue alone.

Related notes: [cutscenes-timeline-yaml](./2026-11-25-cutscenes-timeline-yaml.md), [sprite-state-machine-yaml](./2026-11-25-sprite-state-machine-yaml.md).

## Alternatives considered

1. **Push manual review edits onto `Editor.History` directly and squash them on Keep**: rejected because mutating `Editor.History`'s committed command slice mid-session breaks `History`'s linear invariant and leaves orphaned review-edit commands if the user cancels or reverts the review.
2. **Make every individual clip diff its own `CutsceneChange` without interval grouping**: rejected because reverting a single retimed camera clip (e.g. `cam2` without `cam1`) causes adjacent clip overlap errors rejected by `cutscene.Validate`.
3. **Dock the cutscene timeline as a fixed-height bottom strip below the 2D viewport (like `StatesPanelView`)**: rejected because Screen 2 of `Animation.dc.html` combines a 16:9 live preview stage, a 6-track timeline with a 24-thumbnail scrub strip, a 232 pt left rail (`CUTSCENES` + `LINES`), and a 380 pt right rail (`N CHANGES` + clip inspector), which cannot fit without severe clipping inside a bottom strip at 1440×900.

## Consequences

- **Benefits**:
  - Scrubbing and playback are guaranteed identical to `cutscene.TimelinePlayer` with zero duplicated interpolation logic.
  - Every subset of reverted changes in an agent review produces a valid YAML cutscene, and **Keep N of M** writes and undoes atomically in one `Editor.History` step.
  - Agent proposals via `cutscene_write` never overwrite files unseen on disk until explicitly kept.
- **Costs**:
  - Coupled timing changes on the same non-sound track revert together as one group rather than per-clip; users who want fine-grained retiming after keeping or during review can still adjust individual clip `Start`/`Duration` via `±0.1 s` controls.
  - Opening a cutscene temporarily swaps out the scene-editing `mainRow` until closed (`CloseCutscene`, `KeepCutsceneReview`, or `RevertAllCutsceneReview`).
