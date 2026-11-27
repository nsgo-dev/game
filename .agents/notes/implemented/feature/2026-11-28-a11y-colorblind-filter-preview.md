# Agent Note: Color-vision frame filter, daltonization, and editor/test preview

Status: implemented

## Problem

`save.AccessibilitySettings.ColorBlindMode` and `gameui.SettingsMenu`'s Color-Blind Mode row were synced to `a11y.Assist` in [a11y-settings-assist-sync](../bug-fix/2026-11-28-a11y-settings-assist-sync.md), but only games that explicitly draw with `a11y.Palette` changed colors on screen. Art, tilemaps, particles, and scenes authored with fixed colors had no whole-frame correction filter, and developers had no way to preview scenes or HUD screens under color-vision deficiencies in the editor or assert post-simulation separability in `gametest`.

## Decision

- **`pkg/postfx` owns the shader and matrix math (`ColorVisionEffect`, `ColorVisionMatrix`, `ColorVisionMode`) without importing `pkg/a11y`**, preserving the one-way dependency `pkg/a11y -> pkg/postfx` and avoiding an import cycle.
- **Exact linear-RGB matrices match `a11y.Simulate` bit-for-bit**: simulation interpolates $I + s(S - I)$ with Viénot/Brettel/Machado matrices; daltonization computes $I + s \cdot E \cdot (I - S)$ in linear RGB so lost chromatic contrast shifts into channels the viewer perceives while neutral grays (where $(I - S)g = 0$) and premultiplied alpha stay invariant. `ColorVisionEffect` bakes the matrix into the Kage source and CPU `Reference` so `Chain.Reference(frame, w, h, nil)` and `postfx.View{Chain: chain, Frame: src}` work with zero required uniforms.
- **`save.AccessibilitySettings.ColorBlindSeverity` defaults to `1.0` via `EffectiveColorBlindSeverity()`** (paired with `ColorBlindSeveritySet` and no `omitempty` on `color_blind_severity`) so legacy JSON and Go struct literals setting only `ColorBlindMode` get full filter strength while an explicit `0.0` persists accurately.
- **`a11y.ColorVisionFilter(settings)` and `Assist.ColorVisionFilter()` return `(postfx.Effect, bool)`** (`false` when mode is `"none"` or effective severity is `0`), letting games omit the pass cleanly.
- **`gametest.RenderOptions`** adds `ColorVision`, `ColorVisionSeverity`, and `ColorVisionCorrect`, backed by `ApplyColorVision`, `SimulateColorVision`, `AssertColorDistinguishable`, and `AssertRegionsDistinguishable`.
- **`pkg/editor`** adds `ColorVisionPreview` state shared across the 2D scene viewport (live `postfx.View` over a retained `core.OffscreenImage` of `DrawViewport`, toggled inside the Overlays popover so the floating bottom bar stays single-row on narrow screens) and HUD Layout (direct color simulation across all four device frames plus a **Color Vision & Contrast** panel beside `TEXT WIDTH, PX` reporting normal and simulated WCAG contrast ratios).

## Alternatives considered

- **Dynamic uniform-only `ColorVisionEffect()`**: passing 9 matrix floats through uniforms every render would require callers to always construct and pass a uniform map alongside the effect, breaking the ergonomic `postfx.ColorVisionEffect(mode, severity, correct)` and `a11y.ColorVisionFilter(settings)` contract. Baking the transform into the effect keeps call sites uniform-free while still compiling cleanly in multi-pass chains; caching compiled chains per deficiency mode in the editor avoids recompilation on toggle.
- **Placing a new top-level button directly on the 2D `viewport-overlay-bar`**: narrow-window layout conformance tests (`TestUnreachableToolbarControls`) require `viewport-overlay-bar` to stay single-row (`<= 44 pt`) without overlapping toasts. Placing the 4-mode selector inside the existing Overlays popover plus a dismissible top-left badge when active keeps the bottom bar compact while making active previews unmistakable.
- **Importing `pkg/a11y` from `pkg/postfx`**: rejected because `a11y.ColorVisionFilter` returns `postfx.Effect`, which would create a circular dependency.

## Consequences

- **Benefits**: Any game using `postfx.View` can honor `ColorBlindMode` and `ColorBlindSeverity` in a few lines; `gametest` can guard color-blind readability in CI; authors can inspect 2D scenes and HUD layouts under protanopia, deuteranopia, and tritanopia directly in the editor.
- **Costs**: `pkg/postfx` keeps a copy of the three 3×3 simulation matrices alongside `pkg/a11y/palette.go`, guarded by `TestColorVisionSimulationMatchesA11ySimulate` so the two cannot drift. Games changing `ColorBlindSeverity` continuously on a slider rebuild the chain when severity changes unless they debounce until slider release.
