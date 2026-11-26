# Agent Note: Editor review notes and second-round redo

Status: implemented

## Problem

When reviewing an agent's multi-file edits (`docs/design/editor/Review.dc.html`, #1014), authors often want the agent to adjust one or two specific changes ("Move it about 40 px left") rather than reverting everything or hand-editing YAML. Sending feedback in a separate chat turn loses per-change provenance, risks modifying un-noted changes, and splits the undo history into multiple steps. Notes must also remain strictly ephemeral to the turn so they never pollute project YAML files or leak to the agent before the author explicitly sends them (#1031).

## Decision

Layer change-scoped notes and per-round immutable snapshots cleanly onto `Editor.review` (`packages/game/editor/review.go`, `review_notes.go`, `review_ui.go`) and expose explicit MCP handoff tools (`review_notes_get`, `review_redo` in `packages/game/editor/editormcp/review.go`) with full privacy, permission, You-only, and noted-only isolation guarantees:

- **Turn-scoped notes & immutable round versions (`review_notes.go`, `review.go`)**: Each `ReviewChange` stores `Note` (unsent draft, bounded to `maxReviewNoteLen = 4096` runes and `maxReviewNotesCount = 256`), `SentNote`, `RedoneInRound`, `RedoneNote`, and `Versions []ReviewChangeVersion` (1-based). Each `reviewFileState` keeps `rounds []reviewFileRound` (bounded to `maxReviewRounds = 20`). Notes live strictly in memory and are never written to any scene, prefab, data, or Go file. `RemoveReviewNote` clears `Note` before `SendReviewNotes`, so removed notes never enter `pendingBatch` and never reach the agent.
- **Noted-only MCP handoff, privacy & isolation (`editormcp/review.go`, `editormcp/privacy.go`)**: `review_status` scrubs unsent `Note` fields and redacts diff lines for privacy-blocked files (such as `.go` when `GoSource` privacy is off). `review_notes_get` returns only `pendingBatch` notes sent via **Send N notes** or **Revert and redo with this note**, naming `change_id`, `file`, `object`, `field`, `kind`, `note`, and `revert_first`. `review_redo` maps targeted change IDs to `PermissionRequest` targets (enforcing MCP permission profiles/rules and You-only checks) and privacy rules, atomically refuses un-noted changes (`ErrReviewChangeNotNoted`), extracts only the noted object/field/row even when `file_content` is supplied so un-noted items in the same file cannot be altered, advances to Round `N+1` while keeping earlier rounds read-only (`ErrReviewReadOnlyRound`), and marks redone changes unseen again with `"Redone from your note: <note>"` as their `Why`.
- **Single-step Keep and Undo**: `KeepTurnReview` always reconstructs files from the latest round (`e.reviewLatestRound()`), even if the author is inspecting Round 1, and records one `agentTurnKeepCmd` on `Editor.History` whose `beforeBytes` are the exact pre-request bytes. One `Undo()` restores every touched file and open scene tab to its exact pre-request state.
- **Focus-stable, Spec-token UI (`review_ui.go`, `commands.go`)**: `CmdReviewNote` (`C`) is registered in `ScopeReview` and bound strictly inside `ReviewScreenView` via `bindReviewShortcuts` (`core.KeyNotEditing`), so it never steals keys when Review is closed or when typing in inputs. The inline note composer uses a dedicated draft signal synced only on composer target switches so typing never remounts rows or trims spaces. Both Dark and Light Spec palettes (`SpecPalette`) style the **Round 1 | Round 2** switcher, round banner, saved note box, composer, **Send N notes** accent CTA, viewport speech-bubble pins (in both Review and paused 2D viewports), paused Agent panel card, and Revert all warning dialog.

## Alternatives considered

- **Persisting notes as YAML comments or sidecar files**: Rejected because the editor's "Text is the truth" principle and #1031 require notes to belong strictly to the turn and never touch version-controlled game files.
- **Replacing the whole file from `review_redo.file_content`**: Rejected because an agent could accidentally or silently modify un-noted objects, prefab fields, or data rows in the same file; extracting only the noted object/field/row guarantees structural isolation for un-noted changes.
- **Recording one `History` step per review round**: Rejected because `Review.dc.html` and #1031 specify that both rounds keep and undo as one single step.

## Consequences

- **Benefits**: Authors iterate on specific changes with full Round 1 vs. Round 2 comparison, viewport pins, and guaranteed one-step Undo across multi-file turns; unsent or removed notes can never leak over MCP, and un-noted changes cannot be tampered with.
- **Costs**: Holding per-round file snapshots in memory grows with rounds (bounded by `maxReviewRounds = 20` and `maxReviewFileBytes = 8 MiB`), and snapshots are freed immediately on `KeepTurnReview`, `ConfirmReviewRevertAll`, or `resetTurnReviewForProject`.
- **Verification**: Covered by `packages/game/editor/review_notes_test.go`, `packages/game/editor/editormcp/review_notes_test.go`, `packages/game/editor/editorcmd/review_notes_screenshot_test.go`, `go tool nsgo check ./editor/...`, `go run ./cmd/gameschema -check`, `go run ./tools/changelog check`, and `bun run verify-notes`.
