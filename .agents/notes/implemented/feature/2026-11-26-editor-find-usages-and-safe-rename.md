# Agent Note: Editor Find Usages and Safe Rename Review

Status: implemented

## Problem

The editor previously exposed raw incoming references in the docked Search & References panel ([Project Search and References](2026-11-25-editor-project-search-and-references.md)) and renamed files immediately through `MoveFile` in the file browser without previewing affected lines, showing scene instance overrides, or warning about Go string constants that YAML-only renames intentionally leave untouched. Authors and coding agents needed one unified review workflow matching `docs/design/editor/References.dc.html` where finding usages and safely renaming a prefab or scene happen in one screen, nothing is written to disk until Keep, and one Undo restores every moved and rewritten file.

## Decision

- **Extend `packages/game/refs` for prefabs and data tables without breaking bare resolver names**:
  - `refs.Scan` recognizes `*.prefab.yaml` / `*.prefab.yml` as `KindPrefab` (tracking `inherits`, `uid`, and asset references) and records `ObjectDoc.Prefab` / `ObjectOverride.Prefab` as `FieldPrefab` only when the scalar is path-shaped (contains `/` or `.yaml`/`.yml`, or names an existing project file), leaving bare prefab names untouched for `AssetResolver`.
  - Pending YAML files whose top-level sequences hold row mappings referencing existing project files or known YAML asset suffixes are indexed as `KindDataTable` (`FieldDataTable`), so `refs.PlanMove` and `Editor.MoveFile` rewrite prefab variants, scene instances, and data table rows in place while preserving comments and formatting.
- **Full-window References & Safe Rename review (`packages/game/editor/usages.go`, `usages_ui.go`)**:
  - `FindUsages` merges `refs.Scan` results with a `go/parser` + `go/ast` scan over project `*.go` files, grouping usages into **SCENES** (with object positions and override counts), **PREFAB VARIANTS**, **DATA TABLES**, and **GO CODE** (`NotRewritten: true`).
  - `PreviewSafeRename` / `SetSafeRenameInput` run `refs.PlanMove` purely in memory against `os.DirFS`, validating live against `game mv` rules and generating exact `−`/`+` line diffs with surrounding context (preserving instance overrides) and `"<file> loads the old path and is not rewritten"` problems without touching disk.
  - `ApplySafeRename` executes the YAML-only move through `Editor.MoveFile` as a single `Editor.History` command, syncing open tabs (`moveScenePaths` now also moves `ObjectDoc.Prefab` and `ObjectOverride.Prefab`) and open data tables (`followDataTableMove` reloads rewritten tables). Undoing that single step restores every file on disk byte-for-byte, restores open tabs, and returns the open screen from Done to Usages on the original path (`followUsagesMove`).
  - `ReferencesScreenView` mounts as a full-window mode inside `EditorView` styled strictly with `Spec.dc.html` tokens, avoiding any additions to the compact 2D `editor-toolbar`. Entry points live in `CmdFindUsages` (`shift+f12`), the desktop `Project` menu, Command Palette, and file browser / scene tree context menus.
- **Unified agent path (`packages/game/editor/editormcp/refs.go`)**:
  - `refs_find` (`ActionRead`) and `refactor_rename` (`ActionEdit`) call the same `Editor.FindUsages`, `Editor.OpenUsagesScreen`, `Editor.PreviewSafeRename`, and `Editor.ApplySafeRename` methods. Without `apply: true`, `refactor_rename` opens the editor's safe-rename review without writing disk.

## Alternatives considered

- **Rewriting Go string constants automatically during rename**: Rejected because Go code may build paths dynamically, hold test fixtures, or rely on custom loaders, and `References.dc.html` explicitly requires safe rename to write YAML only while surfacing Go references as problems with an opt-in **Ask the agent to update it** flow.
- **Adding a toolbar button to the 2D `editor-toolbar`**: Rejected because the 2D toolbar must stay within two rows at `1120x680`; placing Find Usages in `shift+f12`, the desktop menu bar, Command Palette, and file browser / scene tree context menus matches the design without crowding the toolbar.
- **Replacing the docked `SearchPanelView` References tab**: Rejected to preserve backwards compatibility with existing `ShowReferences` / `SearchPanelView` callers and tests; the new full-window `ReferencesScreenView` coexists cleanly alongside it.

## Consequences

- **Benefits**: Authors and agents share one preview-before-write rename pipeline across scenes, prefab variants, and data tables with exact line diffs, Go constant warnings, and guaranteed single-step Undo across disk and open tabs.
- **Costs**: `FindUsages` parses project `.go` files on demand with `go/parser` (skipping `.`, `_`, `testdata`, `vendor`, `node_modules`), which is fast for typical game repos but may warrant caching if projects accumulate thousands of Go files.
- **Verification**: Covered by `packages/game/refs/prefab_datatable_test.go`, `packages/game/editor/usages_test.go`, `packages/game/editor/editormcp/refs_test.go`, and real native macOS `nsgotest -target macos` screenshots in `packages/game/editor/editorcmd/references_screenshot_test.go`.
