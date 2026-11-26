# Agent Note: Spec viewport overlays, toggle legend, and object-level problem attribution

Status: implemented

## Problem

Viewport overlays in the 2D and 3D scene editors previously used ad-hoc colors and line styles (such as a hardcoded `0x38BDF8` selection polygon, separate 3D selection/wireframe colors, and inline drawing code for agent tasks and problems), with no single Spec-compliant drawing helper for the seven semantic overlay kinds in `docs/design/editor/Spec.dc.html`, no shared top-left toggle legend persisted per person, no reduced-motion handling for change fades and agent pulses, and no object ID on `assetcheck.Problem` when scene diagnostics reported only a file and line (#1006).

## Decision

Centralize every Spec semantic viewport overlay behind `DrawObjectOverlay`, expose a shared top-left toggle legend persisted in per-user `EditorSettings`, attach `Object` directly to `assetcheck.Problem`, and migrate non-semantic viewport guides (camera frame, camera bounds, tile grid, snap grid) to neutral `SpecPalette` hairline/dotted tokens.

### Single Spec overlay helper and live API (`packages/game/editor/viewport_overlay.go`)
- `DrawObjectOverlay(d, SpecPalette, ObjectOverlay)` renders all seven `OverlayKind` values exclusively from `SpecPalette` tokens at the Spec's exact widths and non-color second cues:
  - `OverlayKindSelection`: `Accent`, 1.5 px solid, four 8×8 corner handles, and accent name tag (person's selection only).
  - `OverlayKindAgent`: `Agent`, 1.5 px solid, no handles, 1.6 s pulse between 100% and 45% (`OverlayAgentPulseAlpha`), dark pill with 4-point sparkle glyph or numbered task badge (`Holding` variant uses 45% dashed outline + clock glyph).
  - `OverlayKindAdded`: `Added`, 1.5 px dashed (`5, 3`), dark pill with `"+ <name>"`.
  - `OverlayKindChanged`: `Changed`, 1.5 px dashed (`5, 3`), dark pill with `"field old → new"`.
  - `OverlayKindRemoved`: `Removed`, 1.5 px solid around a 25% grey ghost (`TextFaint` at 25%), dark pill with struck-through name + `"removed"`.
  - `OverlayKindProblem`: `Problem`, 2 px solid on the live object, filled `Problem.Fg`/`Problem.OnFill` pill with alert triangle glyph and message (never resembling a removed ghost).
  - `OverlayKindFocusedChange`: renders its base `ChangeKind` color and dash at 2.5 px with a full-opacity outlined pill, never `Accent`.
- When `PairedWithSelection` is set on `OverlayKindAgent`, the agent outline expands an extra `OverlaySelectionAgentGap` (3 px) outside Selection's outline so Selection keeps its corner handles unobstructed and the agent pill stacks above.
- Change overlays fade in over 240 ms (`OverlayFadeAlpha`); when `ReduceMotion` is active (`Scope.Env().ReduceMotion()` or `Editor.SetReduceMotion(true)`), fades resolve immediately to 100%, agent outlines stay solid at 100%, and `viewport.go` skips scheduling the 33 ms `overlayAnimRev` timer.
- `SceneOverlayEntry` and `Editor.SetSceneOverlays` / `AddSceneOverlay` / `RemoveSceneOverlay` / `ClearSceneOverlays` provide the live scene API for agent edits, reviews, and plans in both 2D (`drawSceneOverlays2D`) and 3D (`drawSceneOverlays3D`).

### Shared top-left toggle legend and per-user persistence (`spec_feedback_ui.go`, `settings.go`, `apps/website/public/schemas/editor-settings.v1.json`)
- `viewportOverlayLegendView` mounts in the top-left corner of the shared viewport container (`viewport.go`) for both 2D and 3D edit modes, rendering six `core.RoleSwitch` rows (`legend-selection`, `legend-agent`, `legend-added`, `legend-changed`, `legend-removed`, `legend-problem`) with drawn line-style + second-cue swatches.
- `EditorSettings.OverlayLegend` (`*[]string`, `yaml:"overlayLegend,omitempty"`) follows the same pointer-to-slice persistence pattern as `ViewportOverlays` in [2026-11-26-editor-viewport-overlays.md](./2026-11-26-editor-viewport-overlays.md): `nil` omits the key and enables all six kinds by default, while `&[]string{}` serializes `overlayLegend: []` so turning all six rows off survives restart without mutating project or scene files. Unknown or duplicate values are refused by `ParseEditorSettings`.
- `OverlayKindProblem` stays synchronized with `Editor.ProblemOverlayVisible()` (see [2026-11-26-editor-live-problems-agent-fix.md](./2026-11-26-editor-live-problems-agent-fix.md)), and `SpecOverlayLegendView` (see [2026-11-26-editor-spec-light-theme.md](./2026-11-26-editor-spec-light-theme.md)) draws every card through `DrawObjectOverlay`.

### Object-level problem attribution and neutral guides (`tooling/assetcheck/assetcheck.go`, `scene.go`, `viewport.go`, `viewport3d.go`, `overlays.go`, `snap.go`)
- `assetcheck.Problem` adds `Object string` (and `editormcp.Problem` exposes `object`), populated by `sceneChecker.withObject` during scene/component/asset traversal and by `yamlFieldErrorObject` for `sceneformat.FieldError` paths (including `layers[*].objects[*]`, `overrides.<id>`, and `connections[*]` live endpoints). `Editor.attributeProblemObject` checks `p.Object` first before falling back to message regex and line spans.
- Camera frame, camera bounds, camera tether, tile grid, snap grid, and 3D grid/wireframe use neutral `SpecPalette` `TextMuted`/`TextFaint` 1 px hairlines and `2, 4` dotted strokes so no guide shares a color or dash style with the semantic legend.
- `gametest.RenderOps` (`gametest/golden.go`, `golden_shapes.go`) adds `DrawRoundRect`, `DrawArc`, `DrawEllipse`, `DrawLineDash`, scaled dash/line widths, and deterministic 5×7 bitmap text rendering so 1x and 2x golden-image tests verify every Spec overlay primitive headlessly.

## Alternatives considered

1. **Keeping separate 2D and 3D overlay drawing code paths**: 3D already drew wireframe bounding boxes and dots directly in `viewport3d.go`. Rejected because Selection, Agent, Problem, and diff overlays would drift in stroke width, pill layout, reduced-motion behavior, and legend toggles across 2D and 3D. Projecting 3D object corners to canvas bounds and calling `DrawObjectOverlay` keeps both viewports identical.
2. **Storing legend toggles inside `ViewportOverlays` (`SHOW IN VIEWPORT` popover)**: Combining semantic overlay kinds with scene-inspection overlays (`collision_shapes`, `connections`, `navigation`) would avoid a new `EditorSettings` field, but `Spec.dc.html` and `Main.dc.html` place the six semantic kinds in a dedicated top-left legend with explicit line-style swatches separate from the bottom-center inspection bar. Separate `overlayLegend` persistence keeps both independent and backward compatible.
3. **Inferring Problem object IDs solely in `editor` by re-parsing YAML**: Avoiding a field on `assetcheck.Problem` would keep `assetcheck` untouched, but `assetcheck` already walks every scene object, override, and `FieldError` path during validation; emitting `Problem.Object` at the source eliminates fragile regex heuristics and makes object attribution available to `editormcp`'s `game_check` too.

## Consequences

- **Benefits**: One Spec-compliant overlay renderer across 2D and 3D viewports, greyscale-distinguishable semantic cues, instant reduced-motion compliance, persistent per-user legend switches outside project files, exact object-level Problem overlays, and 1x/2x golden-image + native macOS screenshot coverage.
- **Costs**: `gametest.RenderOps` now maintains software rasterizers for rounded rects, arcs, dashed strokes, and 5×7 bitmap glyphs for golden comparisons (so changes to overlay geometry or styling require intentional `-update-golden` updates to `viewport_overlays_1x.png` and `viewport_overlays_2x.png`).
- **Verification**: `packages/game/editor/viewport_overlay_test.go` verifies 1x and 2x golden images, pairwise greyscale distinguishability, Selection+Agent 3 px offset, 240 ms fade / 1.6 s pulse / reduced-motion static rendering, 2D+3D legend toggles and restart persistence (including `[]` and invalid YAML refusal), live Problems panel integration in 2D and 3D, neutral camera/grid tokens, and native macOS Dark/Light `nsgotest` screenshots; `packages/game/tooling/assetcheck/assetcheck_test.go` and `connections_test.go` verify `Problem.Object`.
