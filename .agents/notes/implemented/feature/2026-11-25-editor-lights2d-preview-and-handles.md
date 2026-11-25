# Agent Note: Scene 2D Lights, Occluders, Ambient Color, and Editor Lighting Preview

Status: implemented

## Problem

2D lights, shadow-casting occluders, and ambient darkness (`packages/game/lights2d`) could previously only be wired in Go code, so authors could not author a level's lighting in `*.scene.yaml` files or inspect and adjust light reach, spotlight cones, and occluder polygons visually in `game/editor` (#887).

## Decision

Extend `packages/game/sceneformat` and `packages/game/editor` using the same versioned built-in component and in-place 2D gizmo drag patterns established by [Scene Physics Bodies, Colliders, Triggers, and Named Layers](2026-11-25-scene-physics-components.md) and [The editor's 2D transform gizmo edits in place and records one step per drag](2026-11-25-editor-2d-transform-gizmo.md):

- **Scene ambient color (`SceneDoc.AmbientColor`)**: Add optional `ambientColor` (CSS color validated via `core.ParseColor`) to `sceneformat.SceneDoc`, supported in both v1 and v2 schemas (`scene.v1.json`, `scene.v2.json`), preserved across `Resolve` inheritance (`inherit` overrides base when set) and `Clone`, and decoded onto `BuiltScene.AmbientColor` / `BuiltScene.HasAmbient`.
- **Built-in 2D lighting components (`Lights2DComponentVersion = 1`)**:
  - Extend `ComponentRegistry` (`packages/game/sceneformat/components.go`) with `FieldPointList` (`[][2]float64`) for 2D polygon vertex arrays, and canonicalize point-list fields in `sceneformat.Encode` (`canonicalComponents` / `flowPointList`) as compact deterministic flow YAML (`[[x, y], ...]`) matching `tilemap.TilesetDoc`'s polygon convention so YAML and Go edits round-trip byte-for-byte unchanged.
  - Define `light2d` (point and spot lights: `kind`, `color`, `radius`, `intensity`, `falloff`, `direction`, `angle`, `castShadows`, `enabled`, `offsetX`, `offsetY`) and `occluder2d` (`shape`: `rect`, `polygon`, or `tilemap`, plus `width`, `height`, `offsetX`, `offsetY`, `points`, `layer`, `tileLayers`, `enabled`) in `packages/game/sceneformat/lights2d.go`, registered via `RegisterLights2DComponents` / `Lights2DComponentRegistry`.
  - `BuildLights2D` turns `BuiltScene.AmbientColor`, world-space `light2d` / `occluder2d` components (including nested children via composed `BuiltObject.World`), and tilemap collision occluders (`tileshape.New(bl.Map, ...).Occluders()`, see [Per-tile collision shapes](2026-11-25-tilemap-per-tile-collision-shapes.md)) into `BuiltScene.Lights2D` (`*lights2d.Scene`). `Build` runs it automatically, `SyncLights2D` refreshes it after runtime moves, and `BuiltScene.Draw` renders `Lights2D` after the last visible world-space layer (under the scene camera transform, before screen-space HUD layers) unless `SkipLights2D` is set.
- **Editor lighting preview and interactive handles (`packages/game/editor`)**:
  - Add `Editor.ShowLightingPreview` (default `true`, toggled by `SetShowLightingPreview`, toolbar button `view-lighting-preview`, and command `CmdToggleLightingPreview`) and undoable `SetAmbientColor` (also exposed in the Inspector when no object is selected).
  - In edit mode, `DrawViewport` renders the exact same `BuiltScene.Lights2D` pass as the game (expanded to the visible 2D canvas world bounds via `BuiltScene.Lights2DBounds` when panned/zoomed) when preview is on, and sets `SkipLights2D = true` when off. Play mode leaves `SkipLights2D = false` and calls `SyncLights2D` each tick so moving physics bodies carry live lights and shadows.
  - Extend `GizmoHandle2D` (`gizmo2d.go`, `lights2d_editor.go`) with `Handle2DLightRadius`, `Handle2DLightSpotAngle`, `Handle2DLightSpotDirection`, `Handle2DOccluderVertex`, `Handle2DOccluderEdgeMid`, and `Handle2DOccluderRectN/E/S/W`. Component handle drags edit in place (rebuilding `Lights2D` live on every pointer move), cancel cleanly on Escape, and commit one undoable `componentMapCmd` on `EndDrag2D` so Undo restores the exact pre-drag YAML.
  - Combine built-in physics and 2D lighting schemas in the Inspector's fallback `builtinComponents` registry (preserving `Editor.ComponentRegistry() == nil` by default) and add `SetOccluderPolygonPoints`, `InsertOccluderVertex`, `RemoveOccluderVertex`, and `ConvertOccluderToPolygon`.

## Alternatives considered

### Separate editor-only lighting renderer instead of sharing `BuiltScene.Lights2D`

Rendering a simplified editor approximation (e.g., flat circles without `lights2d.VisibilityPolygon` clipping) would diverge from runtime shadow geometry and break the guarantee that a light and occluder cast the same shadow in preview and in the game. Driving both `BuiltScene.Draw` and `Editor.DrawViewport` through the exact same `*lights2d.Scene` built by `BuildLights2D` guarantees identical visibility polygons, cone attenuation, and ambient blending.

### Top-level `lights` and `occluders` lists on `SceneDoc` instead of built-in components

Dedicated root-level lists would duplicate object transforms, parent-child parenting, overrides, instancing, and animation targeting already solved by `ObjectDoc` and `Components`. Making `light2d` and `occluder2d` versioned built-in components lets lights attach as children of moving actors (such as a hero's flashlight), inherit/override across scene files, and edit through the existing Inspector and 2D gizmo pipeline without bumping the scene schema version.

## Consequences

- **Benefits**: Authors can place point lights, spotlights, and rect/polygon/tilemap occluders directly in `*.scene.yaml`, toggle live 2D lighting preview in the editor, drag radius/cone/polygon handles with single-step Undo/Redo, and see the exact same shadows in editor preview and in the game while preserving byte-for-byte YAML round-tripping.
- **Trade-offs**: `sceneformat` now imports `game/lights2d` and `game/tileshape` (both Experimental, matching `sceneformat`'s Experimental status in `TestStabilityTable`). Calling `Build` on a scene with an invalid `light2d.color` fails build (surfacing in `Editor.BuildErr`), while scenes with no ambient color or lighting components keep `Lights2D == nil` with zero runtime overhead.
- **Verification**: Covered by `packages/game/sceneformat/lights2d_test.go`, `packages/game/sceneformat/testdata/canonical/lit_room.scene.yaml` (`formattest.Run`), `packages/game/editor/lights2d_test.go` (matching editor preview and game shadows, toolbar toggle, handle drags with Undo/Redo, Escape cancel, YAML round-trip), `cmd/gameschema -check`, `TestStabilityTable`, `TestFormatsPage`, and multi-target `nsgo check`.
