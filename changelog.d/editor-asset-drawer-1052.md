### Added

- `game/refs`: add `IsAssetPath`, `Project.Paths`, `Project.UsedBy`, `Project.MissingAssets`, and `Project.UnreferencedAssets` to query asset references, missing files, and unreferenced assets from one project scan (#1052).
- `game/editor`: add the Assets drawer mode (`LeftRailSwitchView`, `AssetsLeftRailView`, `AssetDrawerView`, `AssetInspectorView`) with Scene / Assets switching, FOLDERS and SHOW ONLY filters (Unused, Missing files, Generated, Proposed), kind tabs, search, recent sort, frame-by-frame clip and audio previews, `refs`-backed USED BY inspector, missing-file replacement, and snapped drag-to-place / drag-to-swap sprites with single-step Undo and Escape cancel (#1052).
