# Agent Note: Editing scene instances and inherited scenes through overrides

Status: implemented

## Problem

Although `sceneformat` already resolved scene instances (`ObjectDoc.Scene` + `ObjectDoc.Overrides`) and inherited scenes (`SceneDoc.Inherits` + `SceneDoc.Overrides` + `SceneDoc.LayerOverrides`), and `#873` added multi-scene tabs with `OpenInstancedScene` / `OpenBaseScene`, the editor still collapsed viewport picks on instanced children to the instance root, omitted instanced/inherited children from the scene tree, refused edits to inherited layers and non-local objects, and lacked UI workflows for placing scenes/prefabs, inspecting and reverting field overrides, detaching instances locally, or pushing instance overrides back to their source file (#870).

## Decision

- **Resolved-lookup vs. document-lookup separation (`overrides.go`)**: keep `Editor.FindObject(id)` strictly for objects defined directly in `Editor.Doc` (preserving structural commands like `ReparentObject`, `DeleteObject`, `SetObjectID`), while introducing `FindResolvedObject(id)`, `resolvedDoc()`, `resolvedObjectPath(id)`, and `ObjectOverrideInfo(id)` (`ObjectLocal`, `ObjectInstanced`, `ObjectInherited`) for selection, 2D/3D viewport picking, gizmo drags, scene tree rows, and inspector controls.
- **Unified validated override command (`editObjectOverride`, `editLayerOverride`)**: route all value-level object mutations (`MoveObject`, `RotateObject`, `ScaleObject`, `SetObjectAlpha`, `SetObjectZ`, `SetObjectName`, 3D transform/depth/mesh/model/gridMap/occluder/light/camera, sprite/origin/flip/material, particles, `SetProperty`/`DeleteProperty`, `SetComponentField`, `AddComponent`/`RemoveComponent`, `AddProperty`) and inherited layer mutations (`SetLayerVisible`, `SetLayerSpace`, `SetLayerParallax`, `SetLayerRepeat`) on non-local targets to `ObjectDoc.Overrides`, `SceneDoc.Overrides`, or `SceneDoc.LayerOverrides`. Every candidate is validated with `sceneformat.Validate` + `sceneformat.Resolve` (using `editedScene` so cycles are caught) + post-resolve `sceneformat.Validate` before pushing to `Editor.History`, and empty override maps are pruned to `nil` so saved YAML remains clean.
- **Field-level component override detection and revert**: because `sceneformat.ObjectOverride.Components` replaces component maps key-by-key, editing one component field clones the resolved component map, updates that field, and stores it in `ov.Components[cn]`. `ObjectOverrideFields`, `HasObjectOverride`, `ObjectBaseValue`, and `RevertObjectOverrideField` compare `ov.Components[cn]` against the cached un-overridden source scene (`baseSceneCache`, invalidated by `revision`/`scenesRev`/`Doc`) so only changed fields show override markers and reverting the last changed field removes the component entry and empty `Overrides` map.
- **Single-undo `MakeInstanceLocal` and fallible `ApplyOverridesToSource`**:
  - `MakeInstanceLocal` resolves only the target instance's `Scene` (preserving any local `Children` attached to the root), replaces the instance object with its expanded local copy, and appends prefixed instanced connections as one `History` step.
  - `ApplyOverridesToSource` implements `fallibleCommand` (`do() error` / `undo() error`), reads the raw source file bytes, applies each override into the source `SceneDoc` (including deep component merges), writes the updated source file atomically via `writeFileAtomic`, clears `inst.Overrides`, syncs clean open tabs for that source scene, and rebuilds dependent tabs. Undo restores the exact original source file bytes on disk and restores `inst.Overrides` in one step.
- **UI wiring across existing panels without bloating the 2D toolbar**:
  - `FilesView` adds a quick-place `+` button per scene file row, a header **Place** button, and `dragdrop.Source` dragging onto the viewport (`modelDropTarget`).
  - `AssetBrowserView` adds a **Prefabs** list (`prefabListView`) with click-to-place (`SelectedPrefab` + `ToolPlace`), quick-place `+`, and drag-drop onto the viewport.
  - `SceneTreeView` walks `resolvedDoc()`, renders instanced/inherited rows muted (`colors.Muted()`) with `inst`/`base` tags and override dots, disables structural actions on non-local rows, and exposes Open Source / Make Local / Apply to Source / Revert All in context menus.
  - `InspectorView` renders an instance/inherited bar (source path, instance count, override count, Open Scene / Open Base Scene, Revert All, Apply to Source, Make Local), per-field override dots with source-value tooltips, and per-field Revert icon buttons.

## Alternatives considered

- **Flattening overrides on `Editor.Doc` directly and diffing against source on Save**: would avoid routing individual setters through `editObjectOverride`. Rejected because unsaved YAML previews (`SaveYAML`), undo snapshots, `SceneDirty`, and `sceneformat.Resolve` validation all assume `Editor.Doc` itself holds the canonical sparse YAML representation (`scene:` + `overrides:` / `inherits:` + `overrides:`).
- **Two separate undo steps for `ApplyOverridesToSource` (write file, then clear overrides)**: simpler than implementing `fallibleCommand`. Rejected because issue #870 requires applying overrides to source and clearing them on the instance as one undo step that cleanly restores the source file on Undo.
- **Adding instance/override buttons to the 2D toolbar**: rejected because the 2D toolbar must stay compact (`<= 2` rows at `1120x680`) and instance/override actions are selection-scoped, belonging in the Inspector, Scene Tree context menu, Edit menu, and Command Palette.

## Consequences

- **Benefits**: authors can drag/place scenes and prefabs, pick and edit any instanced child or inherited object in 2D/3D or the inspector without mutating source files, inspect and revert individual or all overrides with clean YAML output, detach instances locally in one step, or push overrides back to source with full Undo rollback.
- **Costs**: pre-#870 tests that asserted viewport picking collapsed instanced children to their root (`instance_test.go`) or that inherited layer visibility/space/parallax edits returned errors (`layers_test.go`, `parallax_test.go`) were updated to verify the new override behavior.
- Related: [editor-scene-tabs](2026-11-25-editor-scene-tabs.md), [editor-layers-panel](2026-11-25-editor-layers-panel.md), [editor-scene-tree](2026-11-25-editor-scene-tree.md).
