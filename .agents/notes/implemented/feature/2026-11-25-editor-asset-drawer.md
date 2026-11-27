# Agent Note: Editor asset drawer with refs-backed usage and drag-to-place/swap

Status: implemented

## Problem

The editor split asset workflows across an atlas-frame list (`AssetBrowserView`) and a flat file tree (`FilesView`), with no unified drawer under the viewport for sprites, tiles, audio, and fonts, no Unused / Missing files / Generated / Proposed filters backed by `refs`, no right-panel clip/sound preview with `USED BY` references, and no drag-from-drawer to place snapped sprites or swap an existing object's sprite in one undo step (#1052).

## Decision

Add a **Scene / Assets** mode switch (`LeftRailMode`, `LeftRailSwitchView`) at the top of the left rail and implement the full screen-1 layout of `docs/design/editor/Assets.dc.html` using exclusively `docs/design/editor/Spec.dc.html` color, contrast (`#101216` text on `#3B7BFF` accent fills), spacing, radius, and `:focus-visible` ring tokens:

- **Single source of truth in `game/refs` (`refs/assets.go`)**: `Project.IsAsset`, `Project.UsedBy`, `Project.MissingAssets`, and `Project.UnreferencedAssets` classify project asset files (combining `IsAssetPath` with project-aware `.tsx` tileset vs mod-script disambiguation, excluding `*.import.yaml` / `*.source.yaml` sidecars and `Ref.Output` build artifacts; see [refs-datatable-index-and-tsx-assets](../bug-fix/2026-11-28-refs-datatable-index-and-tsx-assets.md)). `Editor.AssetDrawerSnapshot` populates the drawer catalog, **Unused** and **Missing files** filters, and the right panel's **USED BY** list directly from these methods so the UI can never drift from `refs`.
- **Scene mode compatibility**: `LeftRailScene` remains the default mode so existing Scene-mode panels (`SceneTreeView`, `LayersView`, `AssetBrowserView`, `FilesView`, `InspectorView`) and the compact 2D `editor-toolbar` stay untouched; switching to `LeftRailAssets` swaps to the 3-column Assets layout (`232 pt` `AssetsLeftRailView`, center `290 pt` `ViewportView` over `AssetDrawerView`, `380 pt` `AssetInspectorView`).
- **Shared `dragdrop.Area` for 2D sprite drops (`asset_drawer.go`, `models_ui.go`, `viewport.go`)**: Extending the viewport's existing `dragdrop.Target` (introduced for 3D models in [editor-place-models-3d](2026-11-25-editor-place-models-3d.md)) to accept `AssetDrawerDrag` lets sprite cards drag directly from the bottom drawer onto the 2D viewport. Hovering shows a snapped dashed preview box and pill (`"<name> new Sprite on <Layer> · snapped to <N> px"` or `"swap sprite on <Object>"`), dropping onto an existing object executes one `SetObjectSprite` command (one Undo restores the old sprite), dropping onto empty space executes one snapped `PlaceObject` command, and pressing `Escape` cancels cleanly via `CancelAssetDrag`.
- **Selected-asset preview & import summary (`asset_drawer_ui.go`)**: Multi-frame sprites (matched from atlas frames or `*.import.yaml` slice grids) play frame-by-frame on a fixed `game.Loop` timer with clickable clip chips and frame thumbnails; audio assets render decoded WAV PCM waveforms (with deterministic fallback for non-WAV formats) and play/stop through `sound.Mixer`; import settings summary reuses `InspectAssetImport` from [editor-import-settings](2026-11-25-editor-import-settings.md); missing assets render a problem callout with **Replace with a file** and **Ask the agent**.

## Alternatives considered

- **Replace `AssetBrowserView` and `FilesView` unconditionally on startup**: Would make the asset drawer visible immediately without switching modes, but would break existing Scene-mode workflows and tests expecting `asset-browser`, `file-browser`, and full-height 2D viewport layout at startup. The **Scene / Assets** switch at the top of the left rail matches `Assets.dc.html` screen 1 directly while keeping Scene mode intact.
- **Re-implement missing/unreferenced scanning inside `editor` instead of `refs`**: Would avoid adding methods to `packages/game/refs`, but would risk divergence between `refs.Scan` and the editor's **Unused** / **Missing files** filters. Placing `UsedBy`, `MissingAssets`, and `UnreferencedAssets` in `refs` guarantees exact parity and keeps them reusable by CLI and MCP tools.
- **Add a toolbar toggle button for Assets mode**: Rejected to keep the 2D `editor-toolbar` within two rows at `1120 × 680` and because `Assets.dc.html` places the Scene / Assets switch at the top of the left rail.

## Consequences

- **Benefits**: Every asset kind lives in one searchable, filterable drawer with live `refs`-backed usage, missing-file recovery, playable sprite/audio previews, and one-step undoable drag-to-place / drag-to-swap onto the 2D viewport; `editor-toolbar` height is unaffected.
- **Costs**: Switching to Assets mode constrains the viewport height to `290 pt` to make room for the drawer, and catalog rescans walk the project directory on `filesRev` / `assetsRev` / `drawerRev` changes (cached between edits).
