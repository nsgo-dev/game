# Agent Note: Scene format version 3 and editor parallax layers

Status: implemented

## Problem

`game/scene` already supports per-layer parallax scrolling (`ParallaxX`, `ParallaxY`) and seamless repeat tiling (`RepeatX`, `RepeatY`, `RepeatWidth`, `RepeatHeight`), but YAML scenes in `game/sceneformat` had no layer fields for them and `game/editor` neither previewed parallax offsets while panning the 2D viewport nor offered a layer inspector to edit them. Authors building 2D side-scrollers therefore had to assemble parallax backgrounds in Go instead of plain-text scenes.

## Decision

Introduce scene format version 3 (`sceneformat.CurrentVersion = 3`, schema `https://game.nsgo.dev/schemas/scene.v3.json`) while preserving full backward compatibility with version 1 and version 2 scenes (`MinVersion = 1`):

- **Schema and validation** (`packages/game/sceneformat/format.go`, `resolve.go`, `cmd/gameschema/main.go`):
  - Add `ParallaxX *float32`, `ParallaxY *float32`, `RepeatX bool`, `RepeatY bool`, `RepeatWidth float32`, and `RepeatHeight float32` on `LayerDoc` (and matching pointer fields on `LayerOverride`), with `EffectiveParallaxX()` / `EffectiveParallaxY()` defaulting to `1`.
  - Using `*float32` for `ParallaxX` and `ParallaxY` distinguishes an explicit `0` (a layer pinned against camera translation) from an omitted field (default `1`) and guarantees v1/v2 scenes without parallax fields parse and encode byte-for-byte unchanged.
  - `Validate` refuses any version 3 parallax or repeat field in version 1 or version 2 documents (both on `layers` and `layerOverrides`), refuses non-finite parallax/repeat values and negative repeat periods, and `Resolve` applies `LayerOverride` parallax/repeat fields while refusing them on layers that merely extend a base layer.
  - `cmd/gameschema` filters version 3 fields out of `scene.v1.json` and `scene.v2.json` and emits `scene.v3.json`.
- **Runtime parity with `scene.Layer`** (`packages/game/sceneformat/build.go`):
  - `BuiltLayer` carries `ParallaxX`, `ParallaxY`, `RepeatX`, `RepeatY`, `RepeatWidth`, and `RepeatHeight`. When `RepeatX` or `RepeatY` is true and the explicit period is `0`, `Build` falls back to the layer's tilemap pixel size and then the scene's virtual viewport size so a simple `repeatX: true` flag works out of the box while explicit periods match `scene.Layer` exactly.
  - `BuiltScene.Draw` scales the clamped camera position by `(ParallaxX, ParallaxY)` and tiles repeating world-space layers across the camera's visible bounds using the same math as `scene.Layer.drawWithParallax`.
- **Editor 2D parallax preview and layer inspector** (`packages/game/editor/view2d.go`, `viewport.go`, `layeredit.go`, `inspector.go`):
  - In 2D edit mode, `DrawViewport` renders each visible layer through `layerAffineFor(space, parallaxX, parallaxY)`, which applies `(-View2D.X * parallaxX, -View2D.Y * parallaxY)` against the editor's 2D view and tiles repeating layers across the canvas visible bounds. Play mode still calls `BuiltScene.Draw` directly clipped to the virtual viewport.
  - `HitTest`, `HandleCanvasPress` (`ToolMove`, `ToolPlace`), `CanvasToTile`, `drawTileGridOverlay`, and `drawSelectionOverlay` all use `layerAffineFor` / `canvasToLayerFor` so picking, placing, painting, and selection outlines stay aligned with where parallax layers draw.
  - `InspectorView` shows a Layer Inspector for `Editor.ActiveLayer` whenever no object is selected (for example after clicking a layer row in `SceneTreeView`), exposing `Parallax X`, `Parallax Y`, `Repeat X/Y`, `Repeat W`, and `Repeat H` (disabled on screen-space layers). `SetLayerParallax` and `SetLayerRepeat` execute undoable commands through `History` that raise version 1/2 scenes to `CurrentVersion` when writing non-default v3 fields and restore exact `nil` pointers and `prevVersion` on Undo.

## Alternatives considered

- **Add parallax fields to scene version 2 instead of bumping to version 3**: avoids adding `scene.v3.json` and updating generated scene version expectations. Rejected because issue #885 explicitly calls for a new scene version and published schema files are version-immutable so existing `version: 2` files continue validating strictly against `scene.v2.json`.
- **Apply `screenAffine` + `BuiltScene.Draw` in edit mode instead of per-layer `layerAffineFor`**: reuses `BuiltScene.Draw` directly in the editor viewport. Rejected because `screenAffine` cancels `SceneDoc.Camera` once for all layers, which would make parallax layers scroll at 1x speed when panning `View2D` and would not preview parallax at all when `SceneDoc.Camera` is nil.
- **Require explicit `repeatWidth`/`repeatHeight` whenever `repeatX`/`repeatY` is set**: simpler fallback logic. Rejected because authors setting a repeat flag on a tiled background or full-viewport backdrop expect `repeatX: true` alone to tile cleanly without duplicating tilemap or virtual viewport dimensions, while explicit periods remain supported for custom spans.

## Consequences

- **Benefits**: 2D side-scroller parallax and repeating backgrounds can be authored in plain-text YAML scenes, overridden across inherited scenes, rendered with exact `scene.Layer` parity, previewed smoothly while panning in the editor 2D viewport, and edited undoably in the layer inspector. Existing v1 and v2 scenes remain byte-for-byte compatible.
- **Costs**: `CurrentVersion` advances to 3, so newly generated scenes (`game generate scene`, `editor.DefaultSceneDoc`) emit `version: 3` and `scene.v3.json`. Edit-mode viewport rendering maintains its own per-layer draw loop (`drawEditScene`) parallel to `BuiltScene.Draw`, so future layer-level draw features must be kept in sync between both.
