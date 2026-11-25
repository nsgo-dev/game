# Agent Note: Editor HUD and menu layout screen

Status: implemented

## Problem

Authors designing HUDs and menus in `*.screen.yaml` (introduced in [Game UI `*.screen.yaml` format, runtime builder, checks, and reference tracking](2026-11-25-gameui-screen-files-yaml.md)) previously had no visual editor screen to inspect how a single layout behaves across notched phones, tablets, desktops, and ultrawide monitors or across multiple project languages at once. Localized strings—especially CJK copy—frequently overflow fixed boxes, touch targets fall under 44 pt, and manual touch-button placement required hand-editing YAML coordinates without seeing safe areas, anchors, or focus order side by side.

## Decision

Add a dedicated HUD and menu layout screen (`packages/game/editor/hud_layout.go` and `packages/game/editor/hud_layout_ui.go`) implementing `docs/design/editor/HUDLayout.dc.html` strictly with the color, contrast, spacing, radius, typography, and focus-ring tokens from `docs/design/editor/Spec.dc.html`:

1. **Shared runtime layout and diagnostics (`gameui.BuildScreen`, `gameui.ReferenceDevicesFor`, `gameui.CheckScreen`)**:
   - `Editor.HUDLayoutFrames` lays out the active `ScreenDoc` at all four reference profiles (`Phone 19.5:9 (notch)`, `Tablet 4:3`, `Desktop 16:9`, `Ultrawide 21:9`) in the active language using `gameui.BuildScreen`, so the preview, hit-testing, and touch dragging share the exact geometry the runtime uses.
   - `Editor.HUDProblems` delegates to `gameui.CheckScreen` over the project's `locales/*.yaml` tables (plus any unsaved edits in `Editor.l10n`), filtering locale-specific issues (`IssueTextOverflow`, `IssueMissingTextKey`) to the currently selected language while always reporting language-independent issues (`IssueTouchAreaTooSmall`, `IssueLowContrast`).
   - `Editor.HUDTextWidths` computes the `TEXT WIDTH, PX` comparison table across every project locale using `gameui.MeasureScreenText`, highlighting overflowing locales in `specProblem`.

2. **Direct file-backed undoable edits (`hudScreenCmd`)**:
   - Manual edits—dragging touch controls (`BeginHUDTouchDrag` / `UpdateHUDTouchDrag` / `EndHUDTouchDrag`), reordering children inside a container (`ReorderHUDNode`), adjusting container spacing (`SetHUDNodeSpacing`), changing anchors (`SetHUDNodeAnchor`, 3×3 grid plus `fill`), toggling safe-area clamping (`SetHUDNodeSafeArea`), and toggling `fixed` vs `fit_text` (`SetHUDNodeSizeMode`)—each execute as a single `fallibleCommand` (`hudScreenCmd`) on `Editor.History`.
   - When the active screen is backed by a `*.screen.yaml` file on disk, `hudScreenCmd` atomically writes canonical YAML (`gameui.EncodeScreen`) on Do/Redo and restores the exact previous file bytes on Undo, triggering `CheckProblems()` and `invalidateProjectIndex()`. Cancelling a touch drag (`CancelHUDTouchDrag` or Escape) restores the document without recording history or touching disk.

3. **Full-window view mode and Spec-token compliance**:
   - `EditorView` swaps into `HUDLayoutView` whenever `Editor.HUDLayoutOpen()` is true (keeping `CommandPaletteView` mounted above it for `⌘K` search), reachable from the toolbar **HUD** button, `Project > HUD & Menu Layout` (`CmdHUDLayout`, `mod+shift+u`), opening a `*.screen.yaml` file in `FilesView` / `OpenProjectFile`, or clicking a screen diagnostic in `ProblemsView`.
   - Every surface, border, text pair, and focus ring uses Spec tokens exclusively; primary actions on `specAccent` (`#3B7BFF`, such as `"Ask the agent to fix N"`) and `specTextStrong` (`#FFFFFF`, such as `Play` and `Send`) use `specBg` (`#101216`) foreground text to satisfy the Spec WCAG 2.2 AA/AAA contrast table.
   - Empty and malformed-YAML error states render explicit cards rather than a blank canvas, and the bottom composer (`SubmitHUDPrompt`, `AskAgentToFixHUDProblems`, `Editor.OnHUDPrompt`) routes broader layout requests to the agent.

## Alternatives considered

- **Embedding HUD layout as a short bottom drawer panel like `LocalizationPanelView`**: Would keep the 2D/3D scene viewport visible, but cannot legibly show four device aspect ratios, the `SCREENS` / `NODES` rail, the `TEXT WIDTH, PX` table, and the full inspector with `1.5×` overflow zoom simultaneously. Switching `EditorView` into the full-window `HUDLayoutView` matches `HUDLayout.dc.html` at `1440 × 900` while letting the breadcrumb back button and `CmdHUDLayout` return cleanly to the scene viewport.
- **Keeping HUD edits in memory until a separate Save button is pressed**: Would mirror scene editing (`Editor.Save`), but contradicts the acceptance requirement that dragging a touch button writes its offset to the screen file in one undo step and conflicts with the Spec principle that text on disk stays the truth for coding agents reading via MCP. Persisting canonically on each undoable command (mirroring `EditAssetImportSettings` sidecar writes) keeps disk, undo stack, and `game check` always synchronized.
- **Computing device layout in `editor` with custom placement formulas matching `HUDLayout.dc.html`'s inline JS**: Would duplicate anchor and box math and risk drifting from `gameui.ScreenCanvas` and `gameui.CheckScreen`. Delegating directly to `gameui.BuildScreen` and `gameui.CheckScreen` keeps editor preview, runtime rendering, and CI diagnostics identical.

## Consequences

- **Benefits**:
  - Authors catch CJK text overflow, sub-44 pt touch targets, notch clipping, and contrast failures across all four reference screen sizes and every project language at a glance.
  - Touch control dragging, container reordering, spacing, anchor, safe-area, and fit-text edits round-trip deterministically to `*.screen.yaml` in single undo steps.
  - Strictly uses Spec tokens and passes headless UI tests plus real macOS `nsgotest` screenshot verification (`TestEditorHUDLayoutScreenshots`).
- **Costs and known limits**:
  - `Editor.HUDLayoutOpen()` switches the full editor workspace to the HUD layout view rather than docking side-by-side with the 2D/3D scene viewport until dockable workspace layouts (#919) land.
  - Live agent execution from the HUD composer currently dispatches through the `Editor.OnHUDPrompt` hook and records `Editor.LastHUDPrompt()`; direct built-in agent thread wiring awaits the unified editor agent panel (#1007 / #1011).
