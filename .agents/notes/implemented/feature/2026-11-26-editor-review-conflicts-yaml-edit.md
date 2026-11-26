# Agent Note: Review turn conflict resolution and in-place YAML hand editing

Status: implemented

## Problem

Inside the full-window change-by-change agent turn Review screen ([editor-review-change-by-change](./2026-11-26-editor-review-change-by-change.md), `docs/design/editor/Review.dc.html`, issue #1032), two workflows must block **Keep** cleanly without losing either the author's or the agent's work:

1. **Overlapping edits**: the author may edit an object (for example appending a sound entry to `door_1.properties.on_open`) while a pending agent turn edits the same object. Edits to different fields must merge automatically without prompting, whereas overlapping edits to the same field or list must surface both sides (`you` and `agent`), let the author pick **Mine**, **Agent's**, or **Both** (keeping yours first), and block **Keep** until resolved.
2. **In-place YAML hand edits**: the author may edit an added or changed scene object's YAML directly in the Review right pane. Valid edits must update the Review viewport live, tag edited lines `you`, keep/undo as part of the turn, and remain readable by the agent through MCP in round 2; invalid YAML (such as typing `x 444` without a colon) must mark the exact scene line, block **Keep** and **Done**, offer a mechanical one-step fix when the parser names what is missing, and provide **Undo my typing**.

## Decision

Layer conflict resolution and in-place YAML editing directly onto the existing `turnReviewState` / `reviewFileState` / `ReviewScreenView` engine in `packages/game/editor/review.go`, `packages/game/editor/review_conflicts.go`, `packages/game/editor/review_ui.go`, and `packages/game/editor/editormcp/review.go`:

- **Three-way merge on concurrent manual edits (`syncReviewAfterMutate`, `SyncReviewPersonEdits`)**: each scene `reviewFileState` tracks `personDoc` and `personEdited` alongside `beforeDoc` and `afterDoc`. Non-agent mutations through `Editor.afterMutate` update `personDoc` for the matching open scene and run `sceneformat.Merge(beforeDoc, personDoc, afterDoc)`. Disjoint field edits auto-merge into `effectiveReviewSceneDoc` with zero conflicts; overlapping field or list edits populate `ReviewChange.Conflicts` with `you` and `agent` lines. Picking `ReviewConflictBoth` preserves common base items followed by the person's additions first and the agent's additions second.
- **Keep blockers and atomic single-step Undo**: `Editor.ReviewBlockers()` prioritizes active YAML parse errors (`"Fix YAML on line N"`) ahead of unresolved conflicts (`"Choose how <object> merges"`) and custom blockers. `KeepTurnReview()` jumps to the first blocker and returns `*ReviewBlockedError` (wrapping `ErrReviewBlocked`). When `personEdited` is true, `agentTurnKeepCmd` records the encoded `personDoc` as `beforeBytes` so one `Undo` reverts the agent turn, merge choice, and hand edits together while preserving the author's concurrent manual edits; when `personEdited` is false, exact `beforeYAML` bytes are preserved verbatim.
- **Strict, bounded YAML parsing and mechanical one-step fixes (`parseReviewObjectYAML`)**: `SetReviewYAMLDraft` enforces a 256 KiB cap, rejects NUL bytes, checks You-only boundaries (`validateReviewRelPath`), decodes with `yaml.Decoder.KnownFields(true)` (refusing unknown fields), checks duplicate mapping keys, detects plain-scalar continuation folds (`x 444` absorbed into a preceding plain scalar), validates component schemas and full-scene `sceneformat.Validate`, and produces verified `PatchedYAML` one-step fixes (**Add the colon**, **Close the quote**, **Add the closing brace**, **Add the closing bracket**, **Replace tabs with spaces**).
- **Focus-stable YAML editor and Spec Dark/Light UI (`reviewRightView`)**: `review-yaml-input` (`specInputField` with `Multiline: true` and reactive `StyleFunc`) is mounted inside a `core.Dynamic` keyed strictly on `(editing, changeID, yamlEditSessionRev)` rather than `reviewRev`, so typing never remounts `ui.Input`, steals focus, resets caret, or triggers single-letter `ScopeReview` shortcuts (`IsTyping()` guards `bindReviewShortcuts` and `KeyNotEditing`), while `ApplyReviewYAMLFix` and `UndoReviewYAMLTyping` bump `yamlEditSessionRev` to refresh the input cleanly.
- **Round-2 MCP visibility and privacy enforcement (`editormcp/review.go`, `editormcp/privacy.go`)**: `agentTurnKeepCmd.do` retains a deep snapshot of kept changes (`lastChanges`, `lastFiles`) so `Editor.ReviewStatus()` and `review_status` continue reporting conflict choices and `you`-tagged hand-edited fields/lines after Keep for round 2, while capping `HandEditLines`/`HandEditYAML` and enforcing `PrivacySceneAndData` and `PrivacyGoSource`.

Related notes: [editor-review-change-by-change](./2026-11-26-editor-review-change-by-change.md), [editor-mcp-tile-review](./2026-11-26-editor-mcp-tile-review.md).

## Alternatives considered

1. **Maintain a separate single-file `ReviewSession` parallel to `turnReviewState`**: rejected because multi-file review (`#1014`) already landed with hardened path/symlink/You-only checks, multi-tab sync, unified History metadata, and `bindReviewShortcuts` inside `ReviewScreenView`. Layering `#1032` directly onto `turnReviewState` avoids duplicate UI/state and inherits every security boundary automatically.
2. **Mount `ui.Input` inside `reviewRightView`'s `reviewRev`-driven `core.Dynamic`**: rejected because every keystroke calls `SetReviewYAMLDraft` which bumps `uiRev` to update the live viewport and diff lines; mounting `ui.Input` under `reviewRev` would remount the input and lose focus on every keystroke.

## Consequences

- **Benefits**:
  - Concurrent manual edits auto-merge when disjoint and present clear **Mine / Agent's / Both** choices when overlapping, without dropping either side.
  - In-place YAML edits validate strictly, update the Review viewport live, recover cleanly from common syntax typos in one click, and undo with the turn in a single History step.
  - Round-2 agents observe both merge choices and `you`-tagged hand edits through `review_status` under the project's privacy switches.
- **Costs**:
  - `turnReviewState` retains a cloned snapshot of kept `ReviewChange` summaries (`lastChanges`) until the next turn review starts or the project switches so round-2 MCP queries can read them after Keep.
