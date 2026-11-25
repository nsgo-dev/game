# Agent Note: The editor's 2D view is separate from the scene camera

Status: implemented

## Problem

The editor's 2D viewport drew the scene through the scene's own camera (`SceneDoc.Camera`), and the canvas was exactly the scene's virtual screen. To reach an object outside the camera's frame, an author had to move the camera, which changed the saved scene and put view moves into the undo history (#864). The viewport also could not zoom past the camera's zoom to place things precisely, or zoom out to see a whole level.

## Decision

The editor keeps its own 2D view, `View2D` (`packages/game/editor/view2d.go`): a world point at the canvas's center, a zoom and a rotation. It is editor state only.

- The view is never written to the scene file and never recorded in `History`. Panning, zooming, framing and resetting call `notify` but not `apply`, so they do not mark the scene dirty and undo does not step through them.
- Until the author pans or zooms, and again after `ResetView2D` or `OpenYAML`, the view follows the scene camera: `cameraView2D` centers the virtual screen on the canvas at the camera's zoom and rotation, and keeps following the camera as it is edited. `View2DFollowsCamera` reports this, and reports true while playing as well.
- The camera view's zoom is clamped to `[MinView2DZoom, MaxView2DZoom]`, the same range `SetView2D` accepts, so `View2D` always returns a view `SetView2D` would take and the first wheel turn from the camera's view does not snap to the range's edge.
- While the scene plays, `View2D` is the camera's view whatever the author's view is, and the view cannot be moved. Play shows what the player sees; the author's view comes back on Stop.
- The canvas fills its panel in 2D and 3D. In edit mode the whole canvas is filled with the scene's background color first, so objects away from the camera's frame are judged against the game's clear color; the camera's frame is outlined on top. In Play only the camera's frame is drawn, clipped.

### Canvas coordinate contract

Canvas coordinates now go through the 2D view, not the scene camera:

- `CanvasToWorld` and `WorldToCanvas` map canvas points to and from world space through `View2D`. The camera-only mapping is `ScreenToWorld` and `WorldToScreen`.
- `CanvasToScreen` and `ScreenToCanvas` map canvas points to and from the scene's screen space (the virtual screen as the camera shows it), for screen-space layers. A plugin tool must use these for a HUD layer: the canvas point is no longer the screen point, as `examples/fieldmedic/healthplugin` shows.
- Hit testing, placing, moving, tile painting and `CanvasToTile` use the layer's space: world layers through `viewAffine`, screen layers through `screenAffine` (the view times the inverse of the camera).
- 3D presses map through `CanvasSize`, the size the viewport last laid out, since the canvas is no longer the virtual size.

## Alternatives considered

### Persist the view in the scene file

Saving the view would reopen a scene where the author left it, as some engines do. It would also change the scene file on every pan, which breaks this repository's rule that a person's files diff only on real edits, and two authors' views would conflict in review. The view stays in memory; reopening a scene starts at the camera's view.

### Reuse `CameraDoc` for the editor view

A second `CameraDoc` would reuse the camera's transform code. But `CameraDoc` carries bounds, follow targets and limits that would clamp or move the editor's view as the game's camera is clamped, and the editor needs to look outside those bounds. `View2D` is a plain center, zoom and rotation with its own affine.

### Keep `CanvasToWorld` meaning the camera mapping and add new view functions

This would keep the old meaning for existing plugins. But every caller of `CanvasToWorld` is handling a press on the canvas, and with the old meaning all of them would be wrong as soon as the view moves. The editor package is Experimental, so the functions keep their names and change meaning (recorded under Changed in the changelog), and the camera mapping gets the new names.

### Let the author's view stay during Play

Keeping the view would let the author watch an object outside the camera's frame while the game runs. Play is meant to show what the player sees, and the game's input and screen-space layers assume the camera's frame, so Play forces the camera's view.

## Consequences

- Authors can pan to and edit any part of a level without touching the saved camera, and the undo history holds only scene edits.
- Plugin tools that drew or pressed on screen-space layers using raw canvas points land in the wrong place once the view moves; they must call `CanvasToScreen` and `ScreenToCanvas`. The `PluginTool.Press` and `Draw` doc comments and the editor docs say so.
- The view is lost when the editor closes or a scene is reopened.
- The 2D view group widens the editor's tool bar, which now wraps in a narrow window so its file and play buttons stay on screen.
- Two-finger touch panning is tested only through `viewportGestures.touch` directly, since nsgotest cannot inject a two-finger touch. Space+drag needs the platform to report held keys (`core.KeyHeld`); where it does not, the other pan gestures remain.
