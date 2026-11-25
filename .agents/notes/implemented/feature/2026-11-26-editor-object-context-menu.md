# Agent Note: Editor Unified Object Context Menu

Status: implemented

## Problem

Previously only Scene Tree rows exposed a small context menu (order, indent/outdent, layer move, rename, hide/lock, delete), while the 2D and 3D viewports had no object context menu at all and keyboard users could not open object actions without the pointer. Issue #1039 requires one shared context menu on objects across the 2D viewport, 3D viewport, and Scene Tree (also opened from the keyboard via `Shift+F10` or the Context Menu key), adding canonical YAML copy/paste, single-undo Move under reparenting, Group, Select same prefab, Find usages, Rename, and Ask the agent—with every action backed by an exported `Editor` method also exposed through `packages/game/editor/editormcp`. Introducing this menu crosses viewport pointer gestures (3D right-drag fly, 2D tile tool right-drag, polygon vertex right-click), global command shortcut registries, multi-object selection, and deterministic `sceneformat` serialization.

## Decision

- **Shared `ui.ContextMenu` builder**: `objectContextMenuItems` in `packages/game/editor/context_menu_ui.go` builds the unified menu header (object display name plus kind and layer subtitle) and items for both viewports and Scene Tree rows (`includeTreeActions` keeps existing Scene Tree `TestID`s and hierarchy-order actions intact). Unavailable items (`Move under` with no valid candidate parents, `Select same prefab` on objects without `prefab`/`scene`, `Find usages` without an open project) are omitted rather than rendered disabled.
- **Viewport trigger and gesture arbitration**: the viewport frame (`packages/game/editor/viewport.go`) wraps the canvas container in a single controlled `ui.ContextMenu` (`object-context-menu`) whose `Open()` state tracks keyboard (`shift+f10`, `contextmenu`) and pointer (secondary click, Control-click) openings. `OnOpenChange` suppresses menu opening while playing, when a 2D tile tool is active, when a polygon collider vertex owns the right-click, or when a 3D right-drag actually orbited/flew the camera (`fly3DMoved`). Pointer hit-testing tracks the view mode of the last pointer coordinate and clears it on consumption so a 2D click never contaminates a subsequent 3D right-click.
- **Canonical YAML copy/paste contract**: `ObjectYAML` / `CopyObjectYAML` encode a single-layer, single-object `sceneformat.SceneDoc` with `sceneformat.Encode`, preserving schema comments, float formatting, and deterministic key ordering so `sceneformat.Parse` round-trips back to the exact same `ObjectDoc`. Clipboard writes route through `writeClipboard` (`Editor.WriteClipboard` or `core.Scope.SetClipboard`), populate the shared clipboard cache used by `Paste`, and emit a Spec-styled floating viewport toast (`Copied <name> as YAML · N lines`). `PasteObjectYAML` accepts both full `SceneDoc` YAML and bare `ObjectDoc` mappings, assigns unique scene-wide IDs, and commits in one undo step.
- **Single-undo hierarchy edits and cycle safety**: `MoveUnderCandidates` filters out the target object, all of its descendants, and targets that would exceed `sceneformat.MaxObjectDepth`, annotating each candidate with `current`, `locked`, child count, or layer. `MoveObjectUnder` delegates to `ReparentObject` (from [editor scene tree](2026-11-25-editor-scene-tree.md)) so one Undo restores the object and its exact local transform fields. Group, Paste, Duplicate, and multi-object selection wire directly to the existing `GroupObjects`, `Paste`, `DuplicateObjects`, and `SelectObjects` methods.
- **Shortcut coexistence**: `bindObjectContextMenuKeys` registers `shift+f10`, `contextmenu`, `h`, `shift+f12`, and `mod+l` with `core.KeyNotEditing`, avoiding any collision with the central command registry (`mod+c`, `mod+v`, `mod+d`, `mod+g`, and `l` for `ToolLineTile`).
- **MCP parity**: `packages/game/editor/editormcp/object_menu.go` registers `object_copy_yaml`, `object_paste_yaml`, `object_duplicate`, `object_group`, `object_move_under`, `object_select_same_prefab`, `object_visibility`, and `object_find_usages`, each delegating directly to the corresponding exported `Editor` method, and extends `selection_get` with `selectedIds`.

## Alternatives considered

### Separate context menus for 2D viewport, 3D viewport, and Scene Tree

Building distinct menu views per surface would simplify gesture handling locally, but items, shortcuts, omission rules, and labels would drift between the viewports and the Scene Tree. One builder (`objectContextMenuItems`) with an additive `includeTreeActions` flag guarantees parity while preserving all existing Scene Tree tests and `TestID`s.

### Serialize bare `ObjectDoc` YAML on Copy as YAML

Marshaling an `ObjectDoc` fragment directly with `yaml.v3` produces shorter text, but it bypasses `sceneformat.Encode` (losing schema header, canonical float/component formatting, and `sceneformat.Parse` compatibility) and fails the requirement that `sceneformat` parses the clipboard YAML back to the exact same object. Encoding a single-layer, single-object `sceneformat.SceneDoc` guarantees strict round-trip fidelity while `PasteObjectYAML` still accepts bare `ObjectDoc` snippets pasted from external editors.

### Register a new `l` command for Lock in `commands.go`

Adding a global Lock command bound to `l` in the central command registry would collide with `CmdToolLineTile`, breaking shortcut-uniqueness invariants and the desktop menu bar contract. Keeping `l` on `ToolLineTile` globally while exposing Lock/Unlock in the object context menu and Scene Tree row buttons preserves all existing commands and tests without adding toolbar buttons.

## Consequences

- **Benefits**: authors can inspect and invoke every common object action from either viewport, the Scene Tree, or the keyboard (`Shift+F10` / Menu key); Copy as YAML round-trips through `sceneformat.Parse`; Move under and Group each undo in one step; coding agents over `editormcp` have exact programmatic parity with the UI.
- **Costs**: `Move under` enumerates candidate parents in the open scene whenever the context menu opens (linear in object count), and `l` remains bound to `ToolLineTile` in global shortcuts while Lock/Unlock is available from the object context menu and Scene Tree row buttons.
