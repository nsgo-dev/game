# Agent Note: Row-by-row Data Table agent review with lossless Keep and MCP check figures

Status: implemented

## Problem

Agents can edit versioned YAML data tables with dozens of records (such as shop price or loot rebalances), but reviewing those changes line-by-line in a raw text diff obscures numeric percentage deltas (`old → new` and `Δ%`), makes reverting individual records error-prone, and offers no way for the agent to attach verifiable check figures (like playtest-derived coin-rate metrics) that turn stale once the person reverts or tweaks rows (#1033). Meanwhile, saving kept changes must preserve the Data Table editor's lossless guarantee from [Data Table editor](2026-11-25-datatable-editor.md): a kept change to one cell changes only that line, and one Undo restores the original file byte for byte.

## Decision

Add a dedicated full-window Data Table review mode (`packages/game/editor/datatable_review.go` and `packages/game/editor/datatable_review_ui.go`) backed by `datatable.Document` line metadata (`RowLine` / `CellLine` in `packages/game/tooling/datatable/document.go`) and three MCP tools in `packages/game/editor/editormcp/datatable_review.go`:

- **Record matching and lossless Keep composition**:
  - Each file in a turn pairs `Base` and `Proposed` rows by a unique identity column (`id`, falling back to `key`, `slug`, `name`, or row index).
  - On `KeepDataTableReview`, when no rows were added, removed, or reordered, `composeDataTableReviewFile` clones `f.base` (preserving its raw bytes) and invokes `doc.SetCell` only on non-reverted cells whose active value (proposed or person-edited) differs from `base`. `datatable.Document.Marshal` then patches only those exact lines in `raw`, leaving reverted records, untouched fields, comments, and blank lines byte-for-byte identical.
  - When rows were added, removed, or reordered, composition still starts from `f.base.Clone()` and applies `DeleteRow` / `AppendRow` / `SetCell` / `MoveRow` on the YAML AST so untouched rows keep their key order and comments.
  - All file writes in a Keep commit run inside one `dataTableReviewKeepCmd` on `Editor.History`, recording exact pre-Keep bytes and file existence so a single `Undo()` restores every file byte for byte (and deletes newly created files).
- **Interactive review surface (`DataTableReviewView`)**:
  - Uses exclusively `SpecPalette` tokens (`Dark` and `Light`), matching `docs/design/editor/ReviewData.dc.html` and `Spec.dc.html` (including `p.Accent.OnFill` on `p.Accent.Fg` for the Keep button to meet WCAG 2.2 AA contrast).
  - Left rail shows all turn files (`27 of 40 changed`, `new · 0 records`) and the **Agent's check** card (`DataTableReviewCheck`: `Label`, `Value`, `Before`, `Basis`). Any user revert, restore, cell edit, or row reorder sets `checksOutOfDate = true`, displaying a warning badge until refreshed via `SetDataTableReviewChecks` / `datatable_review_checks`.
  - Center pane renders the empty-file message (`"No records — ask the agent to add some"`) when a table has 0 rows, or the filter chip bar (`All`, `Changed`, `Unchanged`, `Reverted`, `Yours`), search input, and sortable table (`CycleDataTableReviewSort`, `DataTableReviewAriaSort`). Numeric columns with changes show a `Δ` header and sort by percentage delta without mutating file order; zero numeric values render as `—`; changed cells use dashed `Changed` borders while person-edited cells switch to solid `Accent` borders.
  - In-place cell edits (`SetDataTableReviewCellText` / `StepDataTableReviewCellNumber`) validate against any registered `datatable.RowType` or inferred `ValueKind` and join the review under `Yours`; drag-to-reorder uses `dragdrop.Area`/`Source`/`Target` calling `ReorderDataTableReviewRecord` and also joins under `Yours`.
  - Right pane shows the focused record's YAML with real 1-based line numbers from `Document.CellLine`, `+`/`-` diff styling, and trailing `you` tags on person-edited or reordered lines.
  - Pressing `Esc` (`PauseDataTableReview`) pauses the review while keeping `HasOpenReview()` true (pausing autosave), showing a `"Review paused"` toast with a **Resume** action and a pending review card in `AgentPanelView` (without duplicating `RightRailView`, `AgentPanelView`, or `AgentTurn`).
- **MCP tools (`packages/game/editor/editormcp/datatable_review.go`)**:
  - `datatable_review_propose` validates proposed YAML files against `datatable.ParseDocument` and any registered `RowType` and opens the review without touching disk.
  - `datatable_review_checks` attaches or refreshes named check figures and clears `checksOutOfDate`.
  - `datatable_review_status` returns open/paused state, per-file counts, active record statuses, current checks, and `checksOutOfDate`.

## Alternatives considered

### Compose Keep by saving `Proposed` and rolling back reverted rows

Applying reverts on top of `Proposed` instead of applying kept edits on top of `Base` would lose `Base`'s exact raw bytes, comments, blank lines, and indentation whenever the agent re-serialized `Proposed`. Starting from `Base.Clone()` and patching only kept/edited cells via `SetCell` guarantees byte-exact preservation of untouched and reverted lines.

### Recompute agent check figures automatically inside the editor on user edits

The editor cannot generically know how an agent derived domain-specific figures (such as coin rates from 20 playtests of floor 3). Marking the attached figures **Out of date** on any revert, edit, or reorder and exposing `checksOutOfDate` through `datatable_review_status` lets the agent recompute them accurately and push fresh numbers with `datatable_review_checks`.

### Render Data Table review inside the bottom `DataTablePanelView`

A 256 pt bottom panel cannot comfortably host the three-column file list, agent's check card, 40-row sortable `old → new` table, and focused YAML diff simultaneously. Mounting `DataTableReviewView` as a full-window screen in `EditorView` matches `ReviewData.dc.html` and the existing full-window `ChangesPanelView` / `PlaytestReportView` screens while leaving `DataTablePanelView` untouched for direct editing.

## Consequences

- Designers can review multi-file agent data table turns row by row, sort by numeric `Δ%`, revert individual records, tweak values in place, reorder rows, inspect focused YAML diffs with `you` tags, pause/resume via `Esc`, and keep changes in one lossless, single-undo step.
- Agents can propose data table reviews and attach/refresh verifiable check figures over MCP, and detect when user edits have invalidated prior figures.
- Structural changes (reordering, adding, or removing rows) still fall back to AST serialization via `yaml.v3` with 2-space indentation, matching the trade-off documented in [Data Table editor](2026-11-25-datatable-editor.md).

## Verification

- `cd packages/game && env -u GOROOT go tool nsgo test ./tooling/datatable`: verifies `RowLine` and `CellLine` line-number tracking alongside existing lossless round-trip tests.
- `cd packages/game && TMPDIR=/tmp env -u GOROOT go tool nsgo test ./editor -run "TestDataTableReview|TestLocaleCatalogsComplete"`: verifies the 27-of-40 shop review, sorting by `price Δ` with `aria-sort`, reverting two records, editing one value, lossless kept-only line diffs, single-step byte-for-byte Undo, out-of-date agent check figures on revert/edit/reorder, `Esc` pause/resume toast and Agent panel card, and real macOS `nsgotest` screenshots in Dark and Light appearances.
- `cd packages/game && TMPDIR=/tmp env -u GOROOT go tool nsgo test ./editor/editormcp -run TestDataTableReview`: verifies `datatable_review_propose`, `datatable_review_status`, and `datatable_review_checks` end-to-end.
- `cd packages/game && env -u GOROOT go tool nsgo check` (host and `-target ios`) on `./tooling/datatable`, `./editor`, and `./editor/editormcp`, plus `env -u GOROOT go run ./tools/changelog check` and `bun run verify-notes`.
