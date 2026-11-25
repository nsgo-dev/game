# Agent Note: Editor viewport Overlays menu shared with MCP viewport images

Status: implemented

## Problem

The 2D scene editor viewport had scattered overlay toggles (`ShowAllShapes`, `ShowPhysicsDebug`, `ShowNavMesh2D`) with no unified Overlays menu, no visualization for scene `connections`, patrol paths, camera bounds captions, or object labels, no plugin extension point for custom overlays, and no per-user persistence of overlay choices. Meanwhile, `editormcp`'s `viewport_image` tool had no way for an agent to request specific overlays (`viewport_overlays`) or see connection/object labels rendered into its PNG, so the agent could not inspect the same viewport picture the author sees (#1040).

## Decision

Introduce a unified `ViewportOverlay` registry, a floating 2D viewport bar with an **Overlays N** `ui.Popover` (`SHOW IN VIEWPORT`), per-user `EditorSettings.ViewportOverlays` persistence, and shared rendering with `editormcp`'s `viewport_image` tool:

### Overlay registry and plugin extension (`packages/game/editor/overlays.go`, `plugin.go`)
- Seven built-in overlays are registered with stable IDs: `collision_shapes`, `triggers`, `connections`, `patrol_paths`, `navigation`, `camera_bounds`, and `labels`. Default active set (`DefaultViewportOverlays`) is `["camera_bounds", "collision_shapes", "navigation", "triggers"]`, preserving existing `ShowPhysicsDebug`, `ShowNavMesh2D`, and camera bounds frame defaults while keeping `connections`, `patrol_paths`, and `labels` opt-in.
- `ViewportOverlay` carries `ID`, `Name`/`NameKey`, `DefaultOn`, `Color`, `Swatch`, `Count func(*Editor) int`, `CountUnit`, and `Draw func(*Editor, *core.Drawing, *sceneformat.BuiltScene)`. Built-in `patrol_paths` uses this exact mechanism, and plugins extend the same list via `Plugin.Overlays` or `Editor.RegisterViewportOverlay` (validated for non-empty whitespace/control-free unique IDs).
- `connections` draws an arched cubic Bezier curve from the emitting object's center to the handler object's center using `core.Drawing.Arc` for the source dot, `CubicTo` for the shaft, a two-segment arrowhead along the end tangent, and a dark Spec-token pill labelled `"event → handler"` (for example `"pulled → open"`). Using `Arc` and `Rect` rather than `Ellipse` and `RoundRect` ensures both the native GPU canvas and the software rasterizer render every primitive identically.

### Floating 2D viewport bar and Spec-token popover (`packages/game/editor/overlays_ui.go`)
- Mount `viewportOverlaysBarView` at bottom-center inside `editor-viewport-container` (matching `docs/design/editor/Inspector.dc.html`), leaving `editor-toolbar` compact (`<= 2` rows at `1120 × 680`). The wrapper uses `PointerEventsNone` with `PointerEventsAuto` only on the pill bar so canvas gestures elsewhere are untouched.
- The **Overlays N** button triggers `ui.Popover` (`Placement: core.PlaceTop`, `Align: core.AlignEnd`) titled `SHOW IN VIEWPORT` with a drawn swatch, localized label, live scene count, and `ui.Switch` per overlay, plus the footer `"The agent's screenshots use the same set: viewport_overlays"`. All colors come strictly from `Spec.dc.html` tokens (`specRaised`, `specPanelBackdrop`, `specLineRaised`, `specTextStrong`, `specText`, `specTextSecondary`, `specTextMuted`, `specAccent`, `specWarning`).
- Mirror active overlays under `View > Overlays` in `menubar.go` and register `CmdViewportOverlays` in `commands.go`.

### Per-user persistence (`packages/game/editor/settings.go`, `apps/website/public/schemas/editor-settings.v1.json`)
- Add optional `ViewportOverlays *[]string` (`yaml:"viewportOverlays,omitempty"`) to `EditorSettings` (version 1).
- Using a pointer-to-slice distinguishes "unset" (`nil`, omitted on encode for byte-for-byte compatibility with existing `editor.yaml` and `testdata/settings.yaml`, resolving to `DefaultViewportOverlays()`) from "explicitly all off" (`&[]string{}`, encoded as `viewportOverlays: []`), and sorts entries canonically on encode.
- Toggling any overlay via `SetViewportOverlay`, `ToggleViewportOverlay`, or `SetViewportOverlays` writes only to `Editor.SettingsPath` (outside the project) and never touches project or scene files. `navigation` stays synchronized with `Editor.ShowNavMesh2D`.

### Shared MCP rendering (`packages/game/editor/overlays.go`, `packages/game/editor/editormcp/tools.go`, `viewport_text.go`)
- `Editor.DrawViewportWithOverlays(d, size, overlayIDs)` temporarily overrides `e.drawOverlayOverride` during `DrawViewport` without mutating persisted settings, refusing unknown overlay IDs with a message listing available IDs.
- `editormcp`'s `viewport_image` accepts optional `viewport_overlays` (`*[]string`): omitted uses `ed.ActiveViewportOverlays()`; explicit list (including `[]`) overrides for that capture.
- Because `packages/game/mcp.RenderPNG` intentionally skips `core.DrawFillText` (verified by `packages/game/mcp/screenshot_test.go`), `editormcp/viewport_text.go` replays `DrawFillText` ops from the recorded `core.Drawing` onto the PNG with a deterministic 5×7 bitmap font (including `→`, `×`, and `·`) and removes `DrawFillText` from `ViewportImage.SkippedOps`. Both the live editor and the agent's PNG therefore show the same connection arrow and `"pulled → open"` label.

## Alternatives considered

1. **Adding the Overlays button to `editor-toolbar` (`ui.go`)**: simpler layout, but `editor-toolbar` is already near the 2-row budget at `1120 × 680` and `docs/design/editor/Inspector.dc.html` places **Overlays N** in the bottom-center floating viewport bar beside Zoom, Snap, and Fit. Keeping it in the viewport bar honors the artboard and avoids toolbar wrapping regressions.
2. **Using plain `[]string` with `omitempty` on `EditorSettings.ViewportOverlays`**: simpler Go type, but turning off all overlays would omit the field on save and silently revert to `DefaultViewportOverlays()` after restart. `*[]string` preserves both backward-compatible omission and an explicit empty list `[]`.
3. **Modifying `packages/game/mcp.RenderPNG` to rasterize `DrawFillText` globally**: avoids a helper in `editormcp`, but `packages/game/mcp/screenshot_test.go` asserts `DrawFillText` is reported in `SkippedOps` as part of `RenderPNG`'s contract. Post-rendering text ops in `editormcp` keeps `game/mcp`'s contract intact while making overlay labels legible in `viewport_image` PNGs.

## Consequences

- **Benefits**: authors and coding agents inspect the exact same viewport overlays (including `"pulled → open"` connection arrows, shapes, triggers, patrol paths, navigation regions, camera bounds, labels, and plugin overlays); user preferences survive restarts outside project files; existing `editor.yaml` files and `ShowAllShapes`/`ShowPhysicsDebug`/`ShowNavMesh2D` workflows stay backward compatible.
- **Costs**: `editormcp/viewport_text.go` maintains a small self-contained 5×7 bitmap glyph table for ASCII plus `→`, `×`, and `·` (non-ASCII labels outside that table render as spaces in software-rasterized PNGs while rendering natively via platform fonts in the live viewport).
- **Verification**: unit and `nsgotest` UI tests in `packages/game/editor/overlays_test.go`, MCP integration tests in `packages/game/editor/editormcp/overlays_test.go`, real macOS `nsgotest` screenshots, `cmd/gameschema -check`, `TestFormatsPage`, `TestLocaleCatalogsComplete`, `nsgo check -target ios`, `tools/changelog check`, and `bun run verify-notes`.
