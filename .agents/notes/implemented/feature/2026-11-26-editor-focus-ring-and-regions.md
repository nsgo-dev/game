# Agent Note: Editor keyboard-only focus rings, F6 region cycling, and dialog focus restoration

Status: implemented

## Problem

The scene editor needed consistent keyboard-only focus visibility and predictable landmark navigation per `docs/design/editor/Spec.dc.html`: `ui.Switch` had no `:focus-visible` style, `ui.IconButton` hardcoded `-2 px` inset outlines on ghost buttons and omitted `:focus-visible` on circular buttons, `ui.Input` changed only a 1 px border without an inset outline, there was no `F6` / `Shift-F6` landmark region cycling or per-region focus memory, the viewport had no `Tab` / `Shift-Tab` object cycling, and closing modal dialogs or full-window Settings with `Esc` left focus nowhere because NSGo v0.3.0's `core.Modal` / `ui.Dialog` do not restore opener focus and `nsgotest`'s `Tab` traversal did not scope to `KindModal`.

## Decision

1. **Unified Spec Dark/Light focus theme (`SpecTheme` / `FocusTheme` in `packages/game/editor/spec_theme.go` and `focus_regions.go`)**:
   - `FocusTheme()` folds directly into `SpecTheme()` (`spec_theme.yaml`), mapping `focus`, `info`, and `ring` to Spec accent (`#3B7BFF` in Dark, `#1D5CE6` in Light) while preserving both Spec Dark and Spec Light WCAG 2.2 AA surfaces.
   - Editor-level control primitives (`SpecSwitch`, `SpecIconButton`, `SpecFocusWithinCard`, `specInputField`) resolve `SpecPaletteFor(cx.Dark())` and `p.FocusRing(±2)`: `+2 px` offset on switches and standalone icon buttons, `-2 px` inset offset on tree rows, list rows (`AssetBrowserView`, `FilesView`), and text fields (preventing clipping by scrolling parents while keeping selection fills visible on selected+focused rows), and `+2 px` `:focus-within` rings on composer cards (`hud-composer-card`).
2. **Fixed-order `F6` / `Shift-F6` region controller (`focusRegionController`)**:
   - Regions cycle in fixed `docs/design/editor/Spec.dc.html` order: `RegionHeader` -> `RegionSceneTree` -> `RegionViewport` -> `RegionInspector` -> `RegionStatusBar` -> `RegionToast` (wired directly into `#1192`'s unified `ToastView` / `SpecToastCard` in `packages/game/editor/spec_feedback_ui.go` and participating only while `Editor.HasActiveToast()` is true, with `RegionToast` checked ahead of `RegionViewport` since `ToastView` renders inside `ViewportView`).
   - `F6` and `Shift-F6` are bound with `core.KeyAlways` and `OverModals: false`, suppressed during `#1191` shortcut capture (`shortcutCaptureActive`) and full-window screens (`StartScreenOpen`, `MergeConflictOpen`, `PlaytestReportOpen`, `UsagesScreenOpen`, `ChangesOpen`, `HUDLayoutOpen`, `BuildReleaseScreenOpen`, `SettingsOpen`, `CutsceneOpen`), and close any open `#1195` object context menu.
   - Region landmark labels are localized in `locales/en.yaml` and `locales/zh.yaml` (`region.*`).
   - Candidate refs registered via `registerCandidate(cx, ...)` unregister automatically in `cx.Cleanup` so dynamic inspector rebuilds never leak candidates or stale `lastFocused` entries.
3. **Viewport focus is the selection (`packages/game/editor/viewport.go`)**:
   - `editor-viewport-frame` is `Focusable` (while not playing) with no outline on the canvas or frame itself; its `OnKeyDown` handles unmodified `tab` (`Editor.SelectNextObject`) and `shift+tab` (`Editor.SelectPreviousObject`) while not playing, capturing shortcuts, or showing an object context menu.
4. **Dialog and Settings focus trapping & opener restoration (`bindDialogFocusRestore`, `bindViewFocusRestore`, `dialogFocusTrapView`)**:
   - Every editor modal (`ShortcutsSheetView`, `CommandPaletteView`, `unsaved-dialog`, `scene-path-dialog`, `save-conflict-dialog`) stores its own `opener *core.Ref` locally in its `bindDialogFocusRestore` closure (immune to out-of-order closes and `chromeKey` remount cleanups) and restores focus in `OnClose` after `core.Modal` leaves the tree. Unified `SettingsView` uses `bindViewFocusRestore` to restore opener focus via `cx.Post` after dismissal.
   - While any modal dialog is open (`openDialogCount > 0`), `dialogFocusTrapView` binds `tab` and `shift+tab` (`OverModals: true`, suppressed during `shortcutCaptureActive`) to `core.MoveFocus(..., FocusDirNext/Prev, true)`. Global shortcuts remain suppressed behind dialogs except `mod+.` (`OverModals: true`, `Editor.OnStopAgent`, also suppressed during `shortcutCaptureActive`), complementing [editor shortcuts, menu bar, and Command Palette](2026-11-25-editor-shortcuts-menu-palette.md).
5. **Focus-stability guards**:
   - `Editor.IsDragging()` covers all 2D/3D drags, marquee, inspector scrubbing (`InspectorScrubbing`), rig drags, HUD touch drags, 3D orbit/pan/fly, and tile/GridMap/terrain strokes.
   - `Editor.IsTyping()` verifies the tracked focused text input ref is still mounted and holds focus (plus `cx.Cleanup` and lifecycle resets on tab switch, Play, `OpenProject`, and `OpenDemo`), preventing sticky typing state if a focused input unmounts without `OnBlur`.
   - `Editor.CanAutoMoveFocus()` and `Editor.ShouldFocusAgentPrompt()` enforce Spec rule 05.

## NSGo v0.3.0 gaps worked around in the editor

- `ui.FocusRing` reads `Palette.Colors["info"]` instead of a dedicated focus-ring role -> bridged by `SpecTheme()` / `FocusTheme()`.
- `ui.Switch` carries no `FocusVisible` style -> bridged by `SpecSwitch` (`base.Switch` with `HitSlop` and `+2 px` offset `FocusVisible`).
- `ui.IconButton` hardcodes `-2 px` offset on ghost buttons and omits `FocusVisible` on circular buttons -> bridged by `SpecIconButton`.
- `ui.Input` draws only a 1 px border color change when focused -> bridged by `specInputField` adding `-2 px` inset `FocusVisible`.
- `core.Modal` / `ui.Dialog` do not save or restore opener focus on dismissal, and `nsgotest.App.moveFocus` walks views behind `KindModal` -> bridged by `bindDialogFocusRestore` + `dialogFocusTrapView` delegating to `core.MoveFocus`.

## Alternatives considered

- **Maintaining a separate `toast.go` queue and `DefaultTheme()`-based `FocusTheme()`** — Rejected because `#1192` already shipped `SpecTheme()` (Dark + Light WCAG 2.2 AA tokens) and `ToastView` (`spec_feedback_ui.go`). Duplicating either breaks Spec Light contrast and creates compile/UI collisions.
- **Storing dialog openers in a shared LIFO slice on `focusRegionController`** — Rejected because a `chromeKey` remount (`cx.Cleanup`) or out-of-order close can leak/pop the wrong dialog's opener; storing `opener *core.Ref` locally per binding closure is leak-free.
- **Counting text focus depth without checking ref mount/focus** — Rejected because unmounting a focused `ui.Input` (such as finishing inline rename or switching inspector selection) can skip `OnBlur` and permanently stick `IsTyping() == true`.

## Consequences

- **Benefits**:
  - Every control across the editor shows a consistent 2 px Spec accent keyboard-only focus ring (`#3B7BFF` dark, `#1D5CE6` light) with exact Spec offsets (`+2 px` controls/cards, `-2 px` rows/fields/regions).
  - Keyboard users can jump directly between all editor regions with `F6` / `Shift-F6`, keep their place in each region, cycle viewport objects with `Tab` / `Shift-Tab`, and dismiss dialogs or Settings with `Esc` without losing focus.
  - Zero conflict with `#1191` shortcut capture, `#1192` Spec Light/Toasts, `#1195` object context menu, or `#1199` unified Settings.
- **Costs and known limits**:
  - Panels that mount raw `core.Input` directly instead of `specInputField` must wire `OnFocus` / `OnBlur` to `noteTextInputFocus` if they want `Editor.IsTyping()` to reflect them.

## Verification

- `(cd packages/game && go tool nsgo check ./editor && go tool nsgo check ./editor/editorcmd)`
- `(cd packages/game && go tool nsgo test ./editor -- -run "TestFocus|TestRegion|TestSpec|TestToast|TestShortcut|TestInspector|TestContextMenu|TestLocaleCatalogs|TestOverlay|TestSettings")`
- `go run ./tools/changelog check && bun run verify-notes`
