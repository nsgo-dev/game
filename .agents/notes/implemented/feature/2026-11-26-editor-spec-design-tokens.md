# Agent Note: Editor Spec design tokens, geometry and motion constants, and zero-literal enforcement

Status: implemented

## Problem

While [the Spec light/dark theme note](./2026-11-26-editor-spec-light-theme.md) established the embedded `spec_theme.yaml` file and WCAG 2.2 contrast table for `SpecPalette`, dozens of editor views, 2D/3D viewport overlays, gizmos, handles, and debug layers still hardcoded `core.RGB`/`core.RGBA` and hex color literals (including Tailwind slate/sky/emerald hues and duplicate local token tables). There was also no exported single source of truth for the Spec's typography, spacing, corner radius, height, and reduced-motion constants, no 3D axis tokens in `spec_theme.yaml`, and no automated test preventing new hardcoded UI color literals from creeping back into `packages/game/editor`.

## Decision

`packages/game/editor/spec_theme.yaml` and `packages/game/editor/spec_theme.go` are the sole source of UI color, type, spacing, shape, height, and motion tokens across the editor, enforced by both WCAG contrast tests and an AST-based zero-literal test.

### Key design choices

- **3D axis and interaction tokens in `spec_theme.yaml`**: Added `specAxisX` (`#FF6B6B` dark / `#C92A2A` light), `specAxisY` (`#4FD38A` dark / `#0D7A43` light), `specAxisZ` (`#3B7BFF` dark / `#1D5CE6` light) from `docs/design/editor/Viewport3D.dc.html`, plus `specPrimaryHover`, `specViewport3DGround`, and `specViewport3DGrid`, and exposed `HoverOverlay`/`ActiveOverlay` from `core.Palette.SurfaceHover`/`SurfacePressed`. All three 3D axis tokens are checked at `>= 3.0:1` against `Viewport` in `SpecContrastTable` for both dark and light appearances.
- **Exported Spec geometry, type, and motion constants**: Defined `SpecFont*` (Inter title `14/600`, body `12.5/400`, section `11/600` at `+6%` tracking, JetBrains Mono `11.5`), `SpecSpace4`–`SpecSpace24` (`4, 8, 12, 16, 24`), `SpecRadiusControl`/`Button`/`Card`/`Dialog` (`6, 8, 12, 18`), `SpecRowHeight`/`SpecMinHitSize`/`SpecButtonHeight`/`SpecButtonTallHeight`/`SpecHeaderHeight` (`28, 28, 30, 32, 44`), `SpecBorderWidth` (`1`), `SpecIconSize`/`SpecIconStrokeWidth` (`15, 1.8`), and `SpecMotionHover`/`Panel`/`Overlay`/`AgentPulse` (`120 ms, 180 ms, 240 ms, 1.6 s`). `SpecMotionDuration` and `SpecAgentPulseOpacity` turn off all motion (`0` duration, solid `1.0` opacity) under `Env.ReduceMotion()`.
- **Var-time theme loading**: `parsedSpecTheme`, `darkSpecPalette`, and `lightSpecPalette` are loaded by a package-level variable initializer (`loadSpecTheme()`) rather than `init()`, guaranteeing deterministic initialization order for package-level derived color variables across other files in `editor`.
- **Zero hardcoded UI color literals outside `spec_theme.go`**: Every non-test `.go` file in `packages/game/editor` (including `editormcp/`) replaces `core.RGB`/`core.RGBA` and UI hex literals with `SpecPalette` tokens, `specAlpha`, `CompositeOver`, or `colorFromRGBA8` (for dynamic raster asset pixels and heightmap layer RGB data). Scene content defaults (such as `BackgroundColor`, default 3D light/sky/fog colors, and hex text-field placeholders) remain scene data, not UI chrome. Side panels (`SceneTreeView`, `LayersView`, `AssetBrowserView`, `FilesView`, `LibraryView`) render in `panel` (`colors.Surface()`) with `line` dividers, and `InspectorView` inherits the root `SpecTheme()` / `Editor.SpecPalette(cx)` directly instead of overriding a dark-only subtree theme.
- **AST-enforced guardrail and WCAG mutation test**: `TestSpecNoHardcodedColorLiteralsInEditor` parses every non-test `.go` file under `packages/game/editor` (excluding `spec_theme.go`) with `go/parser` and fails on any `core.RGB`/`core.RGBA` call or hardcoded UI hex color literal. `TestSpecContrastTableRejectsRetiredMutedColor` verifies that mutating dark `textMuted`/`specTextFaint` back to `#6F7684` causes the contrast check to fail.
- **PR checklist and synchronized docs**: Added the Spec's "Acceptance for every editor PR" checklist to `.github/PULL_REQUEST_TEMPLATE.md` and documented the full design-token contract in both `docs/en/editor.md` and `docs/zh/editor.md`.

## Alternatives considered

- **Regex-only grep test instead of `go/parser` AST scanning**: rejected because regex scans produce false positives on non-color 64-bit PRNG constants (`splitmix64` in `tilebrush.go`) and scene content YAML defaults (`BackgroundColor`, sky/fog placeholders), whereas AST inspection accurately targets `core.RGB`/`core.RGBA` calls and color-typed expressions while keeping exemptions minimal and explicit.
- **Keeping separate Tailwind hues for 2D/3D debug overlays (physics, navmesh, rig, terrain, LOD)**: rejected because the Spec explicitly forbids off-board UI color literals in `packages/game/editor`, and mapping debug overlays onto `SpecPalette` roles (`Accent`, `Agent`, `Added`, `Changed`, `Removed`, `Warning`, `AxisX`/`Y`/`Z`) ensures visual consistency and WCAG-checked contrast in both dark and light modes.

## Consequences

- **Benefits**: Every editor chrome region, gizmo, overlay, and debug canvas now draws strictly from `spec_theme.yaml` with dark/light parity; CI automatically blocks accidental re-introduction of `core.RGB`/`core.RGBA` or retired contrast-failing colors; typography, spacing, radii, heights, and reduced-motion rules are codified as reusable exported constants.
- **Costs**: Adding any new semantic color to the editor requires adding a named token to both `dark` and `light` in `spec_theme.yaml`, wiring it into `SpecPalette`, and verifying it in `SpecContrastTable` rather than writing an inline hex literal.
