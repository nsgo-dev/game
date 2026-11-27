# Agent Note: Keep moved data table refs in game.index.yaml and disambiguate .tsx assets

Status: implemented

## Problem

Two classification rules in `pkg/refs` misclassified project files (`nsgo-dev/game#1608`):

1. `collectDataTableCellRefs` (`pkg/refs/scan.go`) only treated a string cell as a file reference while its target existed on disk or carried a known project YAML suffix. When a referenced asset such as `art/hero.png` was moved by hand to `art/heroes/hero.png`, `Scan` dropped the cell reference, `Check` reported no error, `PlanIndex` rewrote nothing, and the rewritten `game.index.yaml` dropped the old `art/hero.png` hash entry, permanently losing the link to the moved file.
2. `IsAssetPath` (`pkg/refs/assets.go`) unconditionally treated every `.tsx` file as a Tiled XML tileset, even though script mods also use `.tsx` entry scripts (`mod.tsx` declared in `mod.yaml`). As a result, `Project.UnreferencedAssets` and the editor's asset drawer (`pkg/editor/asset_drawer.go`) listed every mod script as an unused asset, encouraging accidental deletion.

## Decision

Split path-only asset checks from project-aware `.tsx` classification and widen data table cell reference recognition to cover indexed and asset-typed paths:

- **Project-aware asset classification (`pkg/refs/assets.go`, `pkg/refs/scan.go`, `pkg/editor/asset_drawer.go`)**: Remove `".tsx"` from `IsAssetPath` so extension-only checks never mark TypeScript JSX scripts as assets. Add `(*Project).IsAsset(name string) bool` (falling back to `IsAssetPath` on a nil receiver), which returns `false` for any file named as a mod's `entry` in a `mod.yaml` manifest, returns `true` for unambiguous `IsAssetPath` paths, and classifies a `.tsx` file as a Tiled tileset only when it parses as well-formed XML rooted at `<tileset>` (`isTiledTSX`) or is referenced as an external tileset from a Tiled map (`.tmj` or `.tmx`) or scene `tilesetFile`. Update `Project.MissingAssets`, `Project.UnreferencedAssets`, and the editor's `ensureAssetDrawerCatalog` (see [editor-asset-drawer](../feature/2026-11-25-editor-asset-drawer.md)) to use `Project.IsAsset`.
- **Mod manifest and `.tmx` scanning without extra package coupling (`pkg/refs/scan.go`)**: `Scan` reads `mod.yaml` files directly with `yaml.v3` to record each mod's in-directory `entry` path (without importing `pkg/modding`), reads `.tmx` files with `encoding/xml` to collect `<tileset source="..."/>` paths for `.tsx` classification (without adding unrewritable XML refs to `Project.Refs`), and validates `.tsx` files with `encoding/xml` before scanning data tables.
- **Data table cell reference matching (`pkg/refs/scan.go`)**: In `collectDataTableCellRefs`, reject URI schemes (`hasScheme`, such as `asset:` or `placeholder:`) and drive volumes (`hasVolume`), and treat a scalar cell as a `FieldDataTable` reference when the target exists, is listed in the project root's `game.index.yaml`, matches `isKnownProjectYAMLRef`, or is classified as an asset by `Project.IsAsset`. Hand-moved or deleted targets therefore stay visible to `Check`, `Moved`, `PlanIndex` (which rewrites the cell or preserves the old index entry), and `MissingAssets`.

## Alternatives considered

- **Keep `.tsx` in `IsAssetPath` and exclude only paths under `mods/`**: Rejected because mods can live in any directory a game passes to `modding.Manager.Discover`, and helper `.tsx` scripts beside `mod.tsx` are still scripts rather than Tiled tilesets. Inspecting `mod.yaml` entries, XML root `<tileset>`, and map references works regardless of folder naming.
- **Import `pkg/modding` and `pkg/tilemap` into `pkg/refs`**: Rejected because `pkg/modding` pulls in the TSX runtime and core UI layers, whereas `pkg/refs` is a lightweight analysis package that parses YAML/JSON/XML directly with standard library and `yaml.v3` decoders.
- **Emit `Ref` entries for `.tmx` XML files during `Scan`**: Rejected for this fix because `refs.PlanMove` rewrites every `Ref` in place via byte spans, and XML span rewriting is not implemented in `pkg/refs`; tracking `.tmx` `<tileset source>` targets only in `Project.mapTSX` classifies referenced `.tsx` tilesets without breaking `PlanMove`.

## Consequences

- **Benefits**: `game check` and `game index` detect and repair hand-moved files referenced from data tables (and keep their index entries if deleted before being restored), while mod `.tsx` scripts no longer appear in `UnreferencedAssets` or the editor's asset drawer and genuine Tiled `.tsx` tilesets remain tracked.
- **Costs**: Callers that inspect a `.tsx` path without a `*refs.Project` get `false` from `IsAssetPath` and must call `(*refs.Project).IsAsset` for `.tsx` disambiguation. `Scan` now reads `.tsx` and `.tmx` files once per scan to classify tilesets.
