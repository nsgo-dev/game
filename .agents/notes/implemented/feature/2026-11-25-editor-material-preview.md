# Agent Note: Editor material assignment and live-preview Material Editor panel

Status: implemented

## Problem

Scene objects reference shared `.material.yaml` resource files (`resource.Material`) carrying both 2D (`opacity`, `blend`) and 3D PBR (`baseColor`, `texture`, `metallic`, `roughness`, `emissive`, `doubleSided`, `unlit`) fields, but the editor could only pick existing material files from the inspector and had no way to create or edit material files in-place. Editing surface appearance required editing YAML externally and reloading the project.

Furthermore, live-previewing unsaved material edits in the 3D viewport had a subtle cache trap: bumping `Editor.assetsRev` triggers `project.Resolver.Refresh()` and resets `scene3DState.files`, which replaces the shared `*resource.Library` with a fresh library that reads the stale, unsaved file back from disk—wiping out any in-memory `Library.Reload` edit.

## Decision

Add material creation and editing directly to `packages/game/editor` while preserving `project.Resolver`'s live `resource.Library` across unsaved edits:

- **Expose `Resolver.Library()` in `packages/game/project`**: `project.Resolver` exposes its shared `*resource.Library` (matching `scenemodel.Assets.Library()`), so `Editor.applyMaterialLive` can call `Library.Reload(relPath, encoded)` on every reachable `*resource.Library` (`ModelResolver`, `Resolver`, `s3d.files`) before saving.
- **Separate `materialRev` / `materialLibRev` from `assetsRev` in `scene3DState`**: Live material reloads increment `Editor.materialRev` and check the sum of reachable `Library.Revision()` values in `Editor.scene3D()`. This forces `build3D` (and increments `s3d.gen` so `viewport3DRenderer` re-uploads uniforms and textures) without triggering `project.Resolver.Refresh()` or discarding `s3d.files`. In 2D, `BuiltScene.SyncMaterials()` runs immediately so every 2D sprite sharing the material updates before the next frame.
- **Docked `MaterialEditorPanelView` and Inspector actions**: Keep the 2D `editor-toolbar` unchanged (staying within 2 rows at 1120×680) and expose material creation/editing through the Inspector (**Edit Material** / **New Material** below the existing searchable Material selector), File Browser (double-click, **Open**, and contextual **Edit Material**), Command Palette (`CmdMaterialEditor` and opening `.material.yaml` files), and `Project > Material Editor`.
- **Every `resource.Material` field through `Editor.History`**: `EditMaterial` and typed setters validate candidates with `resource.Encode` before mutating state or history, support non-history color-picker drag previews committed as a single undoable step on release, reload live on both `Do` and `Undo`, integrate with `HasUnsavedChanges`/`Save`/`SaveAll`/`MoveFile`, and persist strictly via `resource.Encode`.
- **Live `render3d` 3D and 2D sprite preview**: `MaterialPreviewImage` builds a single-object sphere or cube scene and renders it via `scenemodel.NewRenderer` (`render3d`) on `gpu.NewSoftwareRenderer`, switching to `scenemodel.ShadingUnlit` when `Unlit` is enabled, and caches the result keyed by encoded YAML, shape, size, and `assetsRev`. A **Sprite** mode previews 2D texture/sprite tinting, opacity, and composite blend modes on a transparency checkerboard.

## Alternatives considered

- **Bumping `Editor.assetsRev` on live material edits**: Simpler because `scene3D()` already watches `assetsRev`, but rejected because `syncProjectAssets()` calls `project.Resolver.Refresh()` on `assetsRev` changes, replacing the `resource.Library` and clobbering unsaved in-memory `Library.Reload` state with the old on-disk YAML.
- **Adding a Material button to the 2D `editor-toolbar`**: Provides one-click access from the top bar, but rejected because the 2D toolbar is already at its 2-row budget at 1120×680; wrapping to a third row crowds the viewport and breaks layout-sensitive viewport tests. Inspector, File Browser, Command Palette, and menu bar entry points keep the toolbar compact.
- **Adding an `Unlit` field to `scenemodel.Material`**: Would let individual 3D viewport objects draw unlit under lit shading, but rejected because `scenemodel.MaterialFromFile` is explicitly documented and tested to ignore `Unlit` in 3D scenes; handling `Unlit` via `Renderer.SetShading(scenemodel.ShadingUnlit)` in the Material Editor preview respects `Unlit` without breaking `scenemodel`'s contract.

## Consequences

- **Benefits**:
  - Authors can create `.material.yaml` files from the Inspector, edit all 2D and 3D PBR fields with color and texture pickers, and see changes immediately in both the live `render3d`/2D preview and every viewport object referencing the material before saving.
  - Undo/Redo restores previous material values in both the preview and all viewport objects live, and saving always writes canonical `resource.Encode` YAML.
  - `project.Resolver.Library()` aligns `project.Resolver` with `scenemodel.Assets.Library()` for any tool doing live resource reload.
- **Costs**:
  - `scene3DState` tracks two additional revision counters (`materialRev`, `materialLibRev`) alongside `assetsRev` and `importRev`.
  - 3D material preview renders synchronously on a software GPU on material change (cached per encoded YAML); very large textures could add brief latency on texture switch.
- **Verification**:
  - `packages/game/editor/materials_test.go` verifies live roughness updates to both the `render3d` preview and multiple 3D viewport objects before saving, live 2D opacity/blend updates, exact `resource.Encode` file output on save, Undo/Redo restoration of preview and viewport pixels, all material fields via UI controls, Inspector creation/editing, File Browser/Command Palette/MoveFile integration, validation refusals, and `ErrPlaying` guards.
  - Verified with `nsgo check ./project`, `nsgo check ./editor`, `nsgo check -target ios ./editor`, `nsgo test ./project`, `nsgo test ./editor`, `gameschema -check`, `tools/changelog check`, and `bun run verify-notes`.
