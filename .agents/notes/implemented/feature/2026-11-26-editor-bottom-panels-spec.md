# Agent Note: Bottom editor panels aligned to the editor design Spec

Status: implemented

## Problem

The five bottom panels that remain in the main editor window — Animation (`animation_ui.go`), Data Table (`datatable_ui.go`), Localization (`localization_panel.go`), Search & References (`search_ui.go`), and Problems (`problems_ui.go`) — pre-dated `docs/design/editor/Spec.dc.html`. Each panel picked its own spacing (2/3/6 px), radii (3/4 px), type sizes, and NSGo default `ui.ColorsOf` surfaces, used plain ASCII characters (`^`, `v`, `x`, `×`) instead of Lucide icons, lacked tooltips and accessible names on several controls, and rendered clickable `core.View` elements without keyboard focus rings or `Space`/`Return` activation (notably animation keyframe markers, frame cells, event markers, search result rows, reference rows, and problem rows). Several controls also failed WCAG 2.2 contrast or layout rules (white text on accent fills, stacked vertical interpolation toggles, and `ui.NumberInput` / `ui.Select` overflowing fixed-width wrappers), and missing translations in the Localization panel relied primarily on a border tint rather than a non-color warning badge with an icon.

## Decision

Introduce a shared Spec-token primitive layer in `packages/game/editor/spec_panel_ui.go` and migrate all five bottom panels onto it, without touching the panels moving into Settings (Project, Input, Audio, Export):

### 1. Shared Spec bottom-panel primitives (`spec_panel_ui.go`)

All colors in the five panels come exclusively from the Spec token table (`specBg`, `specPanel`, `specRaised`, `specLine`, `specLineRaised`, `specTextStrong`, `specText`, `specTextSecondary`, `specTextMuted`, `specAccent`, `specAdded`, `specChanged`, `specProblem`, `specWarning`, `specWarningFill`, `specWarningEdge`, and Spec-base alpha tints). Spacing uses only `4 / 8 / 12 / 16 / 24` px, radii use `6` (controls/rows), `8` (buttons/groups), and `12` (cards), headers use `44` px min height, and every interactive control maintains at least a `28 × 28` px hit area.

- **Real buttons and toggles (`specPanelButton`, `specPanelToggle`, `specPanelToggleGroup`, `specPanelRowButton`)**: built directly on NSGo's `base.Button`, `base.Toggle`, and horizontal `base.ToggleGroup` instead of clickable `core.View`s. Every control joins Tab or arrow-key roving focus, activates on `Space` and `Return`, carries an explicit accessible `Label`, shows a Spec tooltip (`specTooltipNode`) formatted with platform shortcuts via `FormatShortcut`, and draws the 2 px `#3B7BFF` focus ring (`specFocusRingStyle` at `+2` px offset or `specRowFocusRingStyle` at `-2` px inset inside scroll views so the ring is never clipped).
- **Contrast-safe variants**: primary buttons use `#101216` (`specBg`) on `#FFFFFF` (`specTextStrong`, 18.75:1 AAA), accent fills use `#101216` on `#3B7BFF` (4.88:1 AA), and white text on accent fill is never used.
- **Input and picker width clamping (`specInputStyle`, `specNumberInputStyle`, `specSelectStyle`)**: override NSGo `ui.NumberInput`'s default 160 px width and `ui.Select`'s default 200 px width with `Width: core.Pct(100)` inside sized wrappers, and format animation time inputs to 2 decimals so steppers and text never clip or overlap neighbors.
- **Unified state cards (`specEmptyState`, `specLoadingState`, `specStatusBanner`, `specSeverityPill`)**: provide consistent Lucide icons (`15` px stroke, `currentColor`), spinners, and retry/dismiss/quick-add actions across all five panels.

### 2. Semantic severity and panel states

- **Problems (`problems.go`, `problems_ui.go`)**: error rows use `specProblem` (`#FF6B6B`) with `lucide.TriangleAlert` and an `"Error"` pill; warning rows (`problemIsWarning`, matching `"warning:"` / `"warn:"` prefixes) use `specWarning` (`#F2C14E`) on `specWarningFill` (`#2A2414`) with `specWarningEdge` (`#4A3F1F`) and a `"Warning"` pill + alert icon. Empty (`problems-empty`), loading (`problems-loading`), and error (`problems-error` with `problems-error-retry`) states are all explicit.
- **Localization (`localization_panel.go`)**: missing translations use `specWarningFill` / `specWarningEdge` plus a top-anchored `lucide.TriangleAlert` + `"Missing translation"` badge (`l10n-missing-<loc>-<key>`) so plural cells never clip the cue and color is never the sole indicator. Adds `LocalizationError` / `ClearLocalizationError` and `LocalizationLoading` / `SetLocalizationLoading` for `l10n-error`, `l10n-loading`, and `l10n-empty` states.
- **Animation (`animation.go`, `animation_ui.go`)**: converts numeric key markers, sprite frame cells, and event markers from clickable `core.View`s to real `base.Button`s with `>= 28 × 28` hit targets, inset focus rings, and tooltips; places interpolation toggles (`Linear` / `Smooth` / `Step`) in a horizontal `base.ToggleGroup`; adds `ClipLoading` / `SetClipLoading`, `anim-empty`, `anim-loading`, and a Spec `anim-error` banner.
- **Data Table (`datatable.go`, `datatable_ui.go`)**: replaces ASCII sort/reorder/delete characters (`^`, `v`, `x`, `+`) with Lucide icons (`ArrowUp`/`ArrowDown`/`ArrowUpDown`, `ChevronUp`/`ChevronDown`, `Trash`, `Plus`, `Save`, `X`), adds `DataTableLoading` / `SetDataTableLoading` and `ClearDataTableError`, and adds empty (`datatable-empty` with Add Row / Clear Filter action), loading (`datatable-loading`), and dismissible error (`datatable-error`) states.
- **Search & References (`search.go`, `search_ui.go`)**: moves `Search` / `References` tabs into a horizontal `base.ToggleGroup` with Lucide icons, converts every result and reference row into a real `base.Button` with kind icons and inset focus rings, and adds `SearchLoading` / `SetSearchLoading`, `search-loading`, `search-empty` / `refs-empty`, and `search-error` with `search-error-retry`.

Related panel implementation notes: [Problems panel](2026-11-25-editor-problems-panel.md), [Data Table editor](2026-11-25-datatable-editor.md), [Project Search and References](2026-11-25-editor-project-search-and-references.md), and [Localization YAML editor](2026-11-25-l10n-yaml-editor.md).

## Alternatives considered

### Styling `ui.Button` and `ui.ToggleButton` in place without `base.Button` / `base.Toggle` helpers

Reusing `ui.Button` and `ui.ToggleButton` directly would avoid defining `specPanelButton` and `specPanelToggle`. Rejected because `ui.Button.Render` merges `FocusRing(cx, 2)` in `StyleFunc` (which tracks the OS palette accent instead of Spec's `#3B7BFF`), `ui.ButtonPrimary` and `ui.ToggleButton` render `#FFFFFF` on the accent fill (explicitly forbidden by the Spec contrast table at 3.84:1), and `ui.Button` only supports single-icon-plus-single-text faces rather than structured rows (severity pills, monospace paths, thumbnail canvases) that still need real button semantics. Building on `base.Button` and `base.Toggle` — the same primitives `ui.Button` wraps — preserves full NSGo accessibility, roving focus, and keyboard activation while enforcing Spec tokens everywhere.

### Adding a `Severity` field directly to `assetcheck.Problem`

Adding a public `Severity` enum to `packages/game/tooling/assetcheck.Problem` would make warning classification structured across CLI and editor. Rejected for this UI-focused issue because `assetcheck` is a separate public package with strict parity tests (`TestProblemsParityWithGameCheck`) and no CLI warning producers yet; classifying `"warning:"` / `"warn:"` prefixes in `problemIsWarning` keeps `assetcheck.Problem` backwards-compatible while enabling full Spec warning styling and testing today.

## Consequences

- **Benefits**: All five bottom panels share one Spec token and spacing vocabulary, pass WCAG 2.2 contrast ratios, expose Lucide icons and shortcut tooltips, support full Tab/arrow keyboard navigation with visible `#3B7BFF` focus rings, and present clear empty, loading, and error states. Visual inspection on macOS also eliminated pre-existing layout overlaps in the Animation panel (`NumberInput` steppers, `Select` width, and vertically stacked interpolation toggles).
- **Costs**: `spec_panel_ui.go` maintains dark Spec token styles explicitly rather than inheriting `ui.ColorsOf(cx)`; when light-theme Spec tokens land (#1062), `spec_panel_ui.go` and the other Spec screens (`profiler_ui.go`, `asset_drawer_ui.go`, `cutscene_ui.go`) will swap their shared `spec*` variables to theme-aware lookups together.
- **Verification**: Covered by `packages/game/editor/bottom_panels_spec_test.go` (empty, loading, error, hit-area, role, Tab/arrow focus, and Space activation for all five panels), `packages/game/editor/problems_internal_test.go` (Problems loading, error, retry, warning-vs-error classification, and empty state), all existing panel and i18n tests, cross-target `go tool nsgo check` (host and `-target ios`), and before/after native macOS screenshots in `packages/game/editor/editorcmd/bottom_panels_screenshot_test.go`.
