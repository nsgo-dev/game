# Agent Note: Editor project-wide search and Find References

Status: implemented

## Problem

In the scene editor, the file browser (`FilesView`) was an unfiltered list of project files, and although `packages/game/refs` (`refs.Scan`) already knows every file UID and inter-file reference across a project, authors had no way to search across files, UIDs, and scene contents (object IDs/names, component types, prefab and scene references) or to answer "what uses this file?" with exact file and line numbers (#914).

Adding project search and Find References introduces three constraints:

1. **Live open-scene consistency without clobbering unsaved edits**: an author editing the open scene may search for a newly placed or renamed object before saving, and activating a search or reference hit in the *already-open* scene must not call `OpenFile` and wipe out unsaved edits or undo history.
2. **Exact `refs.Scan` parity for Find References**: Find References must report the exact referring files, 1-based lines, columns, fields, and written values that `refs.Scan` produces, and follow file moves (including `MoveFile` Undo/Redo).
3. **Freshness on both editor and external file changes**: the index must stay warm across keystrokes while reflecting saves, file moves, pack installs/removals, live scene edits, and external disk changes without breaking existing viewport layout dimensions in `EditorView`.

## Decision

Implement project-wide search and Find References in `packages/game/editor/search.go` and `packages/game/editor/search_ui.go`, wired into `state.go`, `project.go`, `refs.go`, `files.go`, `ui.go`, `animation.go`, `datatable.go`, `localization_panel.go`, and `library.go` (reusing `Editor.SelectedFile` and `isProjectScene` introduced by [editor-problems-panel](./2026-11-25-editor-problems-panel.md)):

### 1. Cached project index with stat-only fingerprint and live open-scene overlay

`projectSearchIndex` caches `refs.Scan(os.DirFS(rootDir))` together with all project files (following `ProjectFiles` / `refs.Scan` skip rules: `.`, `_`, `testdata`), UIDs (from `refs.File.UID` with `game.index.yaml` fallback), and parsed scene metadata/objects (`sceneformat.Parse` with a `yaml.Node` fallback for valid YAML scenes that currently fail strict validation).

- **Disk freshness**: `ensureProjectIndex` validates a cheap stat-only directory fingerprint (`path`, `size`, `ModTime().UnixNano()`) and an explicit `dirty` flag invalidated on `SaveFile`, `SaveProject`, `SaveClipFile`, `SaveDataTableFile`, `SaveLocalizationsTo`, `followMove`, `InstallPack`/`RemovePack`, custom `OnSave`, and `RefreshSearch` (also wired to `FilesView`'s Refresh button).
- **Live open-scene overlay**: `queryProjectIndex` overlays `indexSceneDoc(e.Doc)` for the open scene's project-relative path (`openSceneRelPath`, resolving `ScenePath` or `FilePath` relative to `ProjectPath`), and `afterMutate` refreshes active `searchResults` in memory with zero disk I/O.
- **Deterministic ranking and deduplication**: multi-term case-insensitive matching emits at most one `SearchResult` per file or scene object (choosing the highest-priority matched `SearchKind`: `SearchFile`, `SearchUID`, `SearchObject`, `SearchComponent`, `SearchPrefab`, or `SearchSceneRef`), sorted deterministically by `(rank, File, ObjectID, Kind, Detail)`.

### 2. Direct `refs.Ref` Find References and line-aware navigation

- `FindReferences(target)` filters the cached `refs.Scan` output directly and returns `[]refs.Ref`, guaranteeing exact parity with `refs.Scan` on `File`, `Line`, `Column`, `Field`, `Value`, and `Target` (supporting both individual files and directories).
- `OpenSearchResult` and `OpenReference` check `isOpenScene(file)` before calling `OpenFile` so jumping within the already-open scene preserves unsaved edits and undo history. For scenes not yet open, they open the file, update `ScenePath` and `SelectedFile`, and select the matching object (`OpenSearchResult` falls back to `TopObjectID` for nested children since `FindObject` selects top-level objects; `OpenReference` uses `sceneObjectAtLine` over `yaml.Node` line bounds to select the top-level object owning `r.Line` when applicable). For non-scene files (or scenes that fail to parse), they select the file in `FilesView`.
- `followSearchMove` updates `refsTarget` via `forth(refsTarget)` alongside `MoveFile` and Undo/Redo so an active Find References view follows renamed files automatically.

### 3. Toggleable `SearchPanelView` and `FilesView` integration

`SearchPanelView` docks below the viewport (collapsed by default so existing viewport geometry is unchanged) and opens when toggling `FilesView`'s **Search** button (`files-search`), typing a query (`SetSearchQuery`), or selecting a file in `FilesView` and pressing **Find Refs** (`files-find-refs`). Placing the Search toggle inside `FilesView` instead of the top toolbar keeps the top toolbar within the `1120px` window width used by smaller editor windows.

## Alternatives considered

### Putting a separate search list directly inside `FilesView`'s 224pt left column

Filtering `FilesView` inline would avoid a bottom panel. Rejected because `FilesView` is only 224pt wide and shares half the left column with `AssetBrowserView`, leaving too little room to display scene object IDs, component/prefab details, and `file:line (field = value)` rows from `refs.Scan` without severe truncation.

### Always reopening scene files via `OpenFile` when activating a search or reference hit

Unconditionally calling `OpenFile` simplifies `OpenSearchResult` and `OpenReference`. Rejected because searching for an object in the currently open scene (including newly placed unsaved objects) and clicking it would discard unsaved edits and clear `Editor.History`.

### Maintaining a separate reference scanner inside `packages/game/editor`

Writing an editor-specific YAML/JSON reference walker could overlay unsaved edits on disk references. Rejected because issue #914 specifically requires Find References to match `refs.Scan` (which also covers `game.yaml`, Tiled `.tmj`/`.tsj`, LDtk `.ldtk`, and sprite atlases), and duplicating `refs.Scan`'s rules would risk drift.

## Consequences

- **Benefits**: Authors can search across files, paths, UIDs, scene objects, component types, prefabs, and scene references and jump directly to any file or scene object (including scenes not currently open); Find References answers "what uses this file?" with exact `refs.Scan` parity and line-aware navigation; results update live on scene edits, saves, moves, and refreshes.
- **Costs**: `Search` and `FindReferences` perform a stat-only directory walk (`projectFingerprint`) when queried directly to catch external disk changes automatically; large projects with thousands of files may eventually benefit from a filesystem watcher hook (tracked separately in #906) instead of stat polling on query.

## Verification

- `packages/game/editor/search_test.go` verifies:
  - Searching a fixture project in `EditorView` for an object ID in a closed scene, activating the result row, and asserting the editor opens that scene and selects that object (`TestSearchOpensClosedSceneAndSelectsObject`).
  - Selecting a material in `FilesView`, pressing `files-find-refs`, matching every scene and line against `refs.Scan`, and activating both scene and non-scene reference rows (`TestFindReferencesOnMaterialMatchesRefsScan`).
  - File/path search, UID search, object display name search, component type search (direct, nested child fallback, and scene overrides), prefab/scene/inherits reference search, multi-term queries, `ErrNoProject` handling, and preserving unsaved edits/undo stack when jumping within the already-open scene (`TestSearchCoversFilesUIDsObjectsComponentsPrefabsAndSceneRefs`).
  - Live index updates on `PlaceObject`/Undo/Redo, `SaveFile`, `MoveFile`/Undo, external file creation/deletion, and `RefreshSearch` (`TestSearchAndReferencesRefreshAfterEditsSavesMovesAndExternalChanges`).
- Verified via `gofmt`, `go tool nsgo test ./editor/...`, `go tool nsgo test ./refs/...`, `TestStabilityTable|TestAgentSkillValid`, `go tool nsgo check -target macos,windows,linux,android,web`, `go run ./tools/changelog check`, and `bun run verify-notes`.
