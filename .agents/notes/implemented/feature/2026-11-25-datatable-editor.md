# Agent Note: Data Table editor and untyped round-trip datatable YAML

Status: implemented

## Problem

`packages/game/tooling/datatable` previously loaded and saved YAML data tables only through a generic Go struct row type (`LoadYAML[T]` / `MarshalYAML[T]`). That left two gaps tracked by #918:

1. Generic tooling and the scene editor could not open, inspect, or edit a datatable YAML file without compiling in every game's Go row structs, nor preserve per-row key order, comments, and formatting across edits.
2. Designers had no spreadsheet-style grid in `packages/game/editor` to browse, sort, filter, add, delete, reorder, or edit datatable YAML rows with undo/redo and optional Go row-type validation.

## Decision

Split the work across `packages/game/tooling/datatable` (format, AST round-trip, and row-type validation) and `packages/game/editor` (grid panel, undo history, and file browser integration):

- **Untyped round-trip YAML document (`packages/game/tooling/datatable/document.go`)**:
  - `ParseDocument` / `LoadDocument` enforce the same versioned YAML envelope rules as `LoadYAML` (`version: 1`, `rows` list of mappings, no unknown top-level keys, no merge keys, no duplicate row keys, no mapping cell values, no second YAML document) without requiring a Go struct type.
  - Each `Row` stores an ordered `[]Cell` (`Key`, `Value`) so per-row key order survives intact; `Columns()` returns the union of row keys in deterministic first-appearance order, and `ColumnKind(col)` infers `KindString`, `KindNumber`, `KindBool`, `KindDuration`, or `KindList` (keeping scalar values in exact text form so large `int64`/`uint64` values and integer-vs-float distinctions survive round-trips).
  - `Document` retains both the original raw bytes (`raw`) and a synchronized `yaml.Node` AST (`ast`). Saving without edits returns the exact input bytes (`bytes.Clone(raw)`). Editing an existing single-line scalar or flow-sequence cell on a block-style row patches only that cell's token on its line in `raw` while keeping comments, indentation, blank lines, and every other line untouched; structural edits (`AddRow`, `DeleteRow`, `MoveRow`, inserting/deleting keys) invalidate `raw` and encode deterministically from `ast` with two-space indentation while preserving untouched rows' key order and comments.
- **Go row-type validation and pattern registry (`packages/game/tooling/datatable/registry.go`)**:
  - `RowTypeFor[T]()` / `RowTypeOf(reflect.Type)` reuse `buildYAMLPlan` and `assignFromYAMLNode` from `LoadYAML` so `ValidateCell`, `ValidateRow`, `ValidateDocument`, and `ParseCellText` accept and refuse the exact same columns, case-insensitive duplicates, and cell values as `LoadYAML[T]`, returning `*CellError`.
  - `Registry` maps slash-separated file globs (including basename patterns without `/` and `**` recursive segment globs) to `*RowType` entries matched in registration order.
- **Data Table editor panel (`packages/game/editor/datatable.go`, `packages/game/editor/datatable_ui.go`)**:
  - `Editor` exposes `OpenDataTableFile`, `OpenDataTableProjectFile`, `OpenDataTableYAML`, `CloseDataTable`, `SaveDataTableYAML`, `SaveDataTableFile`, `SetDataTableCell`, `SetDataTableCellText`, `AddDataTableRow`, `DeleteDataTableRow`, `MoveDataTableRow`, `SetDataTableSort`, `SetDataTableFilter`, and `RegisterDataTableType[T]` / `RegisterDataTableRowType`.
  - Every table open, close, row mutation, and cell edit runs through `Editor.History` using cloned `Document` snapshots so Undo/Redo restores exact bytes and formatting. Sorting and filtering operate only on `DataTableViewRows()` without mutating row order in the document.
  - `FilesView` adds an Open Table button for `.yaml`/`.yml` files, `followMove` updates an open table's path when `MoveFile` renames it, and `DataTablePanelView` renders the grid below the viewport with kind-aware cell controls (toggle buttons for bools; draft-buffered inputs committing on submit/blur for scalars and lists) and an inline error banner when a registered row type or inferred kind refuses a cell value.

## Alternatives considered

### Re-encode every edit through `yaml.v3` `yaml.Encoder` without raw line patching

Encoding directly from the mutated `yaml.Node` AST is simpler and already preserves key order and comments for canonical 2-space YAML. However, `yaml.v3`'s encoder normalizes indentation, blank lines, and line endings across the entire document, so opening and saving a hand-formatted file or editing one cell in a file with 4-space indentation or blank lines would rewrite unrelated lines. Retaining `raw` for unedited saves and applying surgical single-line token replacement for single-line cell updates guarantees byte-for-byte identity on no-op saves and single-line diffs on single-cell edits while still falling back cleanly to AST encoding for structural changes.

### Represent cell values as `any` (`int64`, `float64`, `bool`, `string`, `[]any`)

Using `any` matches `sceneformat` properties, but loses the distinction between `uint64` values above `2^53`, integral floats versus integers, and explicit duration strings versus plain strings (`"500ms"` vs `time.Duration`), making deterministic YAML round-tripping and `LoadYAML`-identical validation harder. Storing `Value{Kind, Text, List}` keeps exact numeric and duration text while still exposing typed accessors (`Int64`, `Uint64`, `Float64`, `Bool`, `Duration`).

### Validate grid edits with a separate editor-side validator instead of `assignFromYAMLNode`

Writing a parallel type checker in `packages/game/editor` would inevitably drift from `datatable.LoadYAML`'s rules for `table`/`json` tags, embedded structs, case-insensitive column matching, `ErrDuplicateField`, `time.Duration`, pointer nullability, and slice parsing. Sharing `buildYAMLPlan` and `assignFromYAMLNode` inside `datatable.RowType` guarantees the grid refuses every value `LoadYAML` refuses.

## Consequences

- Designers can open datatable YAML files from the project file browser, inspect columns and inferred kinds, sort and filter views, add/delete/reorder/edit rows with full undo/redo, and save clean minimal diffs.
- Games with custom editor binaries can register their Go row structs per file pattern with `editor.RegisterDataTableType` and enforce `LoadYAML` validation live in the grid.
- Because `dataTableEditCmd` clones the `*datatable.Document` (including its `yaml.Node` AST and raw byte slice) per edit, very large tables (tens of thousands of rows) will consume more undo-stack memory than incremental per-cell patches; this is acceptable for game data tables and keeps undo byte-exact.
- Structural edits (add/delete/reorder row or adding a new key to a row) drop `raw` and encode via `yaml.v3` with 2-space indentation, so non-2-space files normalize once a structural edit occurs.

## Verification

- `cd packages/game && go tool nsgo test ./tooling/datatable/...`: verifies byte-for-byte round-trip, 1-line single-cell diffs (including inline comments, lists, durations, bools, and ambiguous strings), per-row key order preservation, add/delete/move rows, value-kind inference/parsing, and `Registry`/`RowType` validation parity with `LoadYAML`.
- `cd packages/game && go tool nsgo test ./editor/...`: verifies opening tables from `FilesView`, byte-identical open/save, 1-line cell edits with Undo/Redo restoring exact bytes, union columns and inferred kinds, non-destructive sort/filter, add/delete/reorder rows, and registered Go row types refusing invalid values in the grid.
- `cd packages/game && go tool nsgo check -target macos,windows,linux,android,web`: verifies cross-platform compilation and vetting.
- `go run ./tools/changelog check && bun run verify-notes`: verifies changelog fragment and note compliance.
