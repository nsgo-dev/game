// Package gameui provides stylized in-canvas menus and widgets rendered directly into a core.Canvas Drawing.
//
// Build a widget tree from containers (Panel, VBox, HBox) and widgets (Label, Button, Toggle, Slider, ProgressBar, Spacer), wrap it in a Root, and drive layout, input, and drawing from your game's Canvas:
//
//   - Call Root.Layout(bounds) to compute exact non-overlapping widget rectangles with padding, spacing, fixed/flex sizing, and alignment.
//   - Route pointer/touch events via OnPointerMove, OnPointerDown, OnPointerUp, or OnPress, and keyboard/gamepad navigation via OnKeyDown, MoveFocus, FocusNext, FocusPrev, Activate, and Adjust.
//   - Call Root.Draw(d, bounds) inside Canvas.Draw; all operations run inside Save/Restore using Path fills and strokes so caller drawing state is never mutated.
//   - Customize visuals with Theme (starting from DefaultTheme) or plug in sprite/nine-slice skins via any widget's DrawBackground hook.
package gameui
