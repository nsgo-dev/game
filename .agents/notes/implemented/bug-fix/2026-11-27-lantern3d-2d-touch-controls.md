# Agent Note: Lantern 3D 2D courtyard touch controls

Status: implemented

## Problem

Lantern 3D's 3D shrine scene handled touch drag-to-orbit and two-finger pinch-to-zoom, and its HUD buttons worked on touch, but the 2D courtyard map only read keyboard bindings (`move_left`/`move_right`/`move_up`/`move_down`, `move_x`/`move_y`, and `interact`). On phones and tablets, switching to the 2D courtyard left the player unable to move the traveler or enter the shrine portal.

## Decision

- Declare Lantern 3D's 2D touch layout in `examples/lantern3d/controls.touch.yaml` (`touchlayout.v1.json` canonical YAML, verified by `TestTouchLayoutIsCanonical`): a floating stick (`id: move`) bound to `move_x` and `move_y` in the lower-left half and a round `USE` button (`id: use`) bound to `interact` on the lower-right, positioned inside the safe area and clear of the bottom-right HUD buttons.
- Align `move_y` to up-positive (`BindAxis("move_y", "move_down", "move_up")`) and negate it when passing to `Map2D.Step` (where 2D canvas Y points downward). This matches `input.TouchControls`'s up-positive stick convention (`VirtualStick.UpPositive: true`) so keyboard and touch stick agree on one axis without mutating built controls after `NewTouchControls`.
- Build two `input.TouchControls` layers (`moveTouch` and `useTouch`) by filtering `controls.touch.yaml` by control ID rather than calling `SetLayout` on portal proximity changes. `SetLayout` resets active fingers, which would drop the player's left thumb mid-walk when crossing into the portal ring; splitting lets `useTouch` bind/draw strictly when `nearPortalSig` is true while `moveTouch` stays uninterrupted, and keeps `pkg/input`'s stable API unchanged.
- Give `map2D` a dedicated `View` (`build2DSceneView`, `id: lantern-map2d`) with a single `core.Canvas` that draws the `screen.Viewport`-scaled courtyard followed by the touch controls in view points (respecting `Env.SafeArea()`), and attaches both touch layers to that 2D stage view. This keeps touch controls completely absent in 3D mode (leaving `lantern-viewport`'s 1-finger orbit and 2-finger pinch untouched), preserves the single-canvas invariant inside `lantern-stage`, and hides controls on desktop until touched.
- Deactivate (reset and unbind from `Actions`) both touch layers on `shrine3D.Enter`, `map2D.Exit`, and `map2D.Pause`, and reactivate/wake them on `map2D.Enter` and `map2D.Resume`. Deactivating immediately in `shrine3D.Enter` guarantees that switching to 3D mid-drag during a fade transition cannot leak a held direction into 3D or back into 2D.

## Alternatives considered

- Adding a per-control visibility method to `pkg/input.TouchControls`: `pkg/input` is marked Stable and issue #1292 is scoped to `examples/lantern3d`; filtering the single canonical `controls.touch.yaml` into `moveTouch` and `useTouch` handles contextual visibility cleanly without changing the stable `pkg/input` API.
- Calling `SetLayout` on a single `TouchControls` when `nearPortalSig` toggles: `SetLayout` rebuilds controls and calls `pad.Reset()`, which would drop an active thumb off the floating stick the moment the player walks into the portal ring.
- Mutating `Stick("move").UpPositive = false` after construction: fragile if a layout is reloaded, whereas making `move_y` up-positive matches `TouchControls`'s documented axis convention directly.

## Consequences

- Touch-only devices can walk the traveler in all eight directions (full analog) in the 2D courtyard and tap `USE` at the portal to enter the 3D shrine.
- Desktop users see no touch overlay unless they touch the 2D canvas, and keyboard input hides the overlay again.
- 3D mode touch gestures and existing 3D/2D switching, pause, and GPU tests remain unaffected.
