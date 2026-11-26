# Agent Note: Editor change-by-change agent turn review

Status: implemented

## Problem

Reviewing a multi-file agent turn across scenes, prefabs, data tables, and Go files requires inspecting each change in context—seeing the scene or prefab impact alongside the agent's reason and exact YAML diff—while independently reverting individual changes, kind groups, or whole files before keeping the remainder in one undo step (`docs/design/editor/Review.dc.html`, `docs/design/editor/Spec.dc.html`, issue #1014).

Before this change, the editor had region-based tile review ([editor-mcp-tile-review](./2026-11-26-editor-mcp-tile-review.md)) and cutscene timeline review ([editor-cutscene-timeline-review](./2026-11-25-editor-cutscene-timeline-review.md)), but no unified full-window multi-file turn review screen, no `ScopeReview` keyboard scope (`J`/`K`, `X`, hold `B`, `⌘↵`, `⌘⌫`), no pending `Esc`/Resume workflow with main-viewport overlays and an Agent panel card, and no read-only MCP tool for agents to observe seen/reverted review state.

## Decision

### Turn diffing, selective reconstruction, and single-step History commands (`packages/game/editor/review.go`)

- `Editor.StartTurnReview(ReviewTurnInput)` accepts before/after file snapshots across scenes, prefabs, data tables, Go files, and other text files, auto-detects `ReviewFileKind`, and computes deterministic per-change entries grouped by file and kind (`added`, `changed`, `removed`) with line-numbered YAML diffs (`ReviewYAMLLine`).
- **Scene diffing & live scene previews**: `diffReviewSceneFile` parses before/after `SceneDoc`s via `sceneformat.ParseWith`, flattens nested objects, and diffs added/changed/removed objects. `effectiveReviewSceneDoc` reconstructs the scene from `beforeDoc` applying only non-reverted changes; if all changes in a file are reverted, `effectiveReviewFileBytes` returns the exact `beforeYAML` bytes verbatim. `reviewSceneBuilt` caches both pre-request (`beforeBuilt`) and effective (`currentBuilt`) `BuiltScene`s so switching scenes on `J`/`K` or peeking with `B` renders without mutating the editor's open scene tab until Keep.
- **Prefab impact**: `diffReviewPrefabFile` walks `yaml.Node` leaves to produce field diffs with numeric `Δ%` (`formatDeltaPercent`). `computePrefabImpact` counts references across both effective scenes in the current review and other project scenes on disk via `Editor.FindUsages`.
- **Small data tables**: `diffReviewDataFile` parses `datatable.Document`s, diffs rows by ID with per-cell `OldValue`/`NewValue`/`DeltaPct`, and `ReviewDataTablePreviewFor` exposes the live table model honoring per-row reverts.
- **One-step atomic Keep and Revert all**: `KeepTurnReview` checks `ReviewBlockers()` first (jumping to the first blocker and returning `ErrReviewBlocked` when blocked) and then executes one `agentTurnKeepCmd` (`fallibleCommand`) in `Editor.History` that writes every touched file's effective bytes, syncs the open scene and autosave status if touched, updates the right-rail `AgentPanelView` card (`"Kept K · reverted R · one undo step"`), and shows a Spec `Toast` with **Undo**. One `Undo` restores every touched file byte-for-byte to `BeforeYAML`. `ConfirmReviewRevertAll` executes one `agentTurnRevertAllCmd` so `⌘Z` / **Undo** restores the pending review cleanly.
- **Plug-in hooks**: `ReviewChange` carries `Note`, `Blocker`, `GoRequiresConfirm`, and `GoConfirmed` fields (`SetReviewChangeNote`, `SetReviewChangeBlocker`) so follow-up issues (notes & round 2, conflicts & in-review YAML editing, large data tables, Go code review) plug directly into the same review screen and blocker gate.

### Full-window Review UI, Spec overlays, keyboard scope, and Agent rail (`review_ui.go`, `agent_ui.go`, `shortcuts.go`, `commands.go`, `focus_regions.go`, `ui.go`, `viewport.go`)

- `ReviewScreenView` renders strictly with `SpecPalette` tokens in both Dark and Light appearances (using `p.Accent.OnFill` `#101216` on dark accent for the **Keep** button per `Spec.dc.html`'s contrast table).
- **Focused overlay rule**: `ReviewOverlays()` and `drawReviewSceneCanvas` draw unfocused active changes at `1.5 px` and the focused change at `2.5 px` in its **own** semantic kind color and dash (`added` dashed green, `changed` dashed amber, `removed` solid red around a 25% grey ghost) with an outlined pill—never Spec accent, which stays reserved for selection. Reverted changes draw as they were before the request.
- **`ScopeReview` and keyboard accessibility**: `ScopeReview` (`scope.review`) registers `J`, `K`, `X`, `B`, `⌘↵`, and `⌘⌫` in the unified command registry (`commands.go`) with priority 5 while `ReviewOpen()`. Holding `B` is driven continuously by `core.KeyHeld(cx, "b")` (with `bindShortcuts` skipping `"b"` while `ReviewOpen()` so `CmdToolPaintTile` never steals it). `F6`/`Shift-F6` cycles Review's 4 regions (`review-header` → `review-list` → `review-center` → `review-right`), `Up`/`Down` walks visible file/kind/card rows so `X` can revert either a single card or a whole file/kind group, and the Revert All modal uses `core.Modal` + `bindDialogFocusRestore`.
- **Esc / Pending state**: `PauseReview` closes `ReviewScreenView` while keeping `ReviewOutcomePending`, pauses autosave via `HasOpenReview()`, draws pending overlays in the main 2D viewport (`drawPendingReviewOverlay`), and activates the right-rail **Agent** tab in `RightRailView` / `AgentPanelView` (with a pending dot on `right-tab-agent`) showing `"Review waiting · N changes, F files"` and **Resume review**.

### Read-only MCP tool (`packages/game/editor/editormcp/review.go`)

- Adds `review_status` (`ActionRead`) returning `editor.ReviewSnapshot` (seen/reverted flags, file summaries, blockers, and outcome). No MCP tool keeps or reverts the review, preserving human-in-the-loop approval.

Related notes: [editor-mcp-tile-review](./2026-11-26-editor-mcp-tile-review.md), [editor-find-usages-and-safe-rename](./2026-11-26-editor-find-usages-and-safe-rename.md), [editor-changes-panel](./2026-11-26-editor-changes-panel.md).

## Alternatives considered

1. **Mutate files on disk during the review and roll back on Cancel/Revert**: rejected because pending agent turns must never touch disk until the author explicitly clicks **Keep**, and `Esc` must leave the review pending without dirtying disk files. Staging before/after snapshots in memory and reconstructing effective bytes on Keep keeps disk untouched until Keep and guarantees byte-for-byte `Undo`.
2. **Use Spec accent (`#3B7BFF`) to highlight the focused change in the Review viewport**: rejected because `Spec.dc.html` explicitly reserves accent for the person's selection; using `2.5 px` in the change's own semantic color/dash with an outlined pill distinguishes the focused change without colliding with selection.
3. **Build a separate right-rail tab bar for Review cards instead of integrating with `RightRailView` / `AgentPanelView`**: rejected because the editor already has a unified `RightRailView` / `AgentPanelView` right rail; rendering the pending/kept/reverted review card directly inside `AgentPanelView` (with a pending dot on `right-tab-agent`) keeps a single right-rail architecture across Ask-in-place, Agent sessions, Tasks, and Review.

## Consequences

- **Benefits**:
  - Authors can inspect multi-file agent turns across scenes, prefabs, and small data tables change-by-change with full keyboard accessibility, Before peek, per-file/kind/card revert, pending `Esc`/Resume, and guaranteed single-step Undo.
  - Follow-up issues (notes & round 2, conflicts & in-review YAML editing, large data tables, Go code review) plug directly into `ReviewChange` blockers/notes and the adaptive center/right panes.
  - Connected MCP agents can query exact seen/reverted state via `review_status` without being able to self-approve changes.
- **Costs**:
  - `reviewFileState` holds in-memory before/after YAML bytes and cached `BuiltScene` instances for touched scenes while a review is pending; these are released immediately on Keep or Revert all.
  - Partial revert reconstruction for `*.scene.yaml` and `*.prefab.yaml` re-encodes modified YAML deterministically (dropping hand-written comments inside partially kept files, though fully reverted files restore exact `BeforeYAML` byte-for-byte).
