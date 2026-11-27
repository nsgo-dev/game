# Agent Note: Image-Filled Progress Widget in game/gameui

Status: implemented

## Problem

`game/gameui` had only the flat `ProgressBar`, so a boss bar or ornate meter built from art had to be hand-drawn per game. GitHub issue #1427 asked for Godot's `TextureProgressBar` equivalent: an under/progress/over image stack revealed by directional or radial fills, in both the widget API and the screen file format.

## Decision

`gameui.ImageProgress` (in `pkg/gameui/widgets.go`, `layout.go`, `draw.go`) holds up to three `NineSlice` layers. The under layer draws in full first, the over layer last, and the progress layer is clipped to the revealed area in between. `FillMode` (`left_to_right`, `right_to_left`, `top_to_bottom`, `bottom_to_top`, `center_h`, `center_v`, `clockwise`, `counter_clockwise`) selects the reveal direction; radial fills sweep `Span` degrees (0 defaults to 360) from `StartAngle` (0 points right, positive is clockwise) around the center, with the radius reaching the corners so the pie covers the rectangle.

### Why clip the destination instead of scaling the source

Linear fills wrap the progress layer's whole destination rectangle in a `ClipPath` and draw the full nine-slice through it. Scaling the source rectangle by the fraction would stretch or drop the caps near zero, while clipping keeps the caps at their fixed destination positions at every value, which the issue's cap-preservation requirement asks for. Radial fills use the same clip-through approach with a pie path, matching how Godot reveals radial progress.

### Screen format

The `image_progress` node type joins the `*.screen.yaml` schema (version unchanged at 1; the change is additive): `under`/`progress`/`over` image paths, `fill`, `startAngle`, `span`, and `min`/`max`/`value` as the starting range. `BuildScreen` resolves each layer through the existing `ScreenBuildOptions.Sprites` callback into a plain `NineSlice` (no insets, since screen files carry no nine-slice metadata yet; the Go API is where nine-slice layers are declared). `game check` (`assetcheck`) reports missing layer images, and `refs`/`game mv` track and rewrite the layer paths alongside `sprite`.

## Alternatives considered

- A separate `image_progress` screen value type with its own nine-slice inset fields: too much schema surface for a first cut; plain stretched layers cover the HUD cases screen files target today.
- A nine-slice-aware clip that scales only the middle section: visually it animates cap size, which reads as morphing art rather than a meter filling.

## Consequences

- The screen schema version stays 1; the new node type and fields are additive, and existing screen files parse unchanged.
- Games that do not need image progress pay nothing: the widget shares the standard measure/layout/draw paths and draws no ops when all layers are nil and no colors are set.
- `refs` and `game mv` now rewrite `under`/`progress`/`over` in place, so moving a layer image does not silently break screens.

## Proof

- Fill geometry is verified op-exact in `pkg/gameui/image_progress_test.go`: the recorded clip rectangle per mode at 0.5, the pie path's center/ray/arc angles for both radial directions at 0.25, and deterministic, mutually distinct op digests at fractions 0, 0.25, 0.5, and 1.
- Cap preservation is verified by comparing the under and progress layers' nine destination tuples under a 0.5 clip: identical, with 12 pt corners kept.
- Screen round trip, node validation (bad `fill`, `span > 360`, invalid layer path) and `BuildScreen` layer resolution are tested in `TestImageProgressScreenNode`; the missing-layer `game check` report in `pkg/tooling/assetcheck/screen_test.go`.
