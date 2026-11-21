// Package gameui provides stylized in-canvas menus, spatial focus navigation, virtual-resolution scaling,
// and world-anchored native NSGo overlays.
//
// Build a widget tree from containers (Panel, VBox, HBox) and widgets (Label, Button, Toggle, Slider, List, ProgressBar, Spacer), wrap it in a Root, and drive layout, input, and drawing from your game's Canvas:
//
//   - Call Root.Layout(bounds) to compute exact non-overlapping widget rectangles with padding, spacing, fixed/flex sizing, and alignment.
//   - Configure Root.Viewport (or Viewport.Draw / Viewport.WindowToVirtual) for virtual-resolution Fit/Letterbox/Fill/Stretch/Integer scaling and accurate pointer/touch hit-testing.
//   - Route pointer/touch events via OnPointerMove, OnPointerDown, OnPointerUp, or OnPress, and keyboard/gamepad navigation via SpatialNavigator (2D spatial lookup, explicit NavOverrides, StepActions, OnConfirm/OnBack, and customizable FocusRingStyle) or Root.OnKeyDown.
//   - Call Root.Draw(d, bounds) inside Canvas.Draw; all operations run inside Save/Restore using Path fills and strokes so caller drawing state is never mutated.
//   - Customize visuals with Theme (starting from DefaultTheme), NineSlice / FrameDrawer hooks, and custom TextDrawer / TextMeasurer hooks for bitmap fonts.
//   - Mount AnchorOverlay over your game Canvas and call Sync(proj) each frame to pin native NSGo views (name tags, speech bubbles, text inputs) to 2D world positions with automatic off-screen hiding.
package gameui
