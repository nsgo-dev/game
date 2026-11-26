# Agent Note: Editor keyboard-only focus rings, F6 region cycling, and dialog focus restoration

Status: implemented

## Problem

The scene editor relied on NSGo's default theme and unmanaged focus flow: `ui.FocusRing` drew NSGo's cyan info token (`#64D2FF` / `#0891B2`) rather than the Spec accent `#3B7BFF`, `ui.Switch` had no `:focus-visible` style, `ui.IconButton` hardcoded `-2 px` inset outlines on ghost buttons and omitted `:focus-visible` on circular buttons, `ui.Input` changed only a 1 px border without an inset outline, there was no `F6` / `Shift-F6` landmark region cycling or per-region focus memory, the viewport had no `Tab` / `Shift-Tab` object cycling, and closing modal dialogs with `Esc` left focus nowhere because NSGo v0.3.0's `core.Modal` / `ui.Dialog` do not restore opener focus and `nsgotest`'s `Tab` traversal did not scope to `KindModal`.

## Decision

1. **Editor focus theme (`FocusTheme` / `editorFocusTheme` in `packages/game/editor/focus_regions.go`)**:
   - `EditorView` installs a clone of `core.DefaultTheme()` whose Light and Dark `info`, `Focus`, and `ring` roles are set to Spec accent `#3B7BFF`. Because every NSGo `ui` control calling `ui.FocusRing(cx, offset)` reads `ColorsOf(cx).Info()`, all `ui.Button`, `ui.ToggleButton`, `ui.Select`, `ui.Checkbox`, `ui.Tree`, and `ui.NumberInput` controls automatically draw `#3B7BFF` on `:focus-visible` (keyboard focus only; mouse clicks set `ModalityPointer` and draw no ring) while preserving light and dark appearance switching.
   - Editor-level control primitives (`SpecSwitch`, `SpecIconButton`, `SpecFocusWithinCard`, `specInputField`) bridge NSGo v0.3.0 gaps: `+2 px` offset on switches and standalone icon buttons, `-2 px` inset offset on tree rows, list rows (`AssetBrowserView`, `FilesView`), and text fields (preventing clipping by scrolling parents while keeping selection fills visible on selected+focused rows), and `+2 px` `:focus-within` rings on composer cards (`hud-composer-card`).
2. **Fixed-order `F6` / `Shift-F6` region controller (`focusRegionController`)**:
   - Regions cycle in fixed `docs/design/editor/Spec.dc.html` order: `RegionHeader` -> `RegionSceneTree` -> `RegionViewport` -> `RegionInspector` -> `RegionStatusBar` -> `RegionToast` (included only while an editor toast is active via `Editor.ShowToast` in `packages/game/editor/toast.go`).
   - `F6` and `Shift-F6` are bound with `core.KeyAlways` (working even inside text fields) and `OverModals: false` (suppressed behind modal dialogs). Before leaving the current region, the controller snapshots `core.FocusedRef(cx)` into `lastFocused[region]` so returning via `F6` or `Shift-F6` restores the exact last control used in that region, falling back to registered enabled candidates or the region root when empty (such as the status bar without a project).
   - Entering a region via `F6` / `Shift-F6` draws the `-2 px` inset `#3B7BFF` ring on the region container (`regionOutlineStyle`); pointer presses clear the region ring via `OnTouchCapture` on the root view.
   - Candidate pruning runs after commit (`pruneCandidates` during `focusRegion`), never inside `registerCandidate`, so sibling refs created during the same render pass are never pruned before mount.
3. **Viewport focus is the selection (`packages/game/editor/viewport.go`)**:
   - `editor-viewport-frame` is `Focusable` (while not playing) with no outline on the canvas or frame itself; its `OnKeyDown` handles `tab` (`Editor.SelectNextObject`) and `shift+tab` (`Editor.SelectPreviousObject`), cycling through visible, unlocked scene objects in deterministic document order.
4. **Dialog focus trap and opener restoration (`bindDialogFocusRestore`, `dialogFocusTrapView`)**:
   - Every editor modal (`EditorSettingsDialogView`, `CommandPaletteView`, `unsaved-dialog`, `scene-path-dialog`, `save-conflict-dialog`) pushes the opener ref (`core.FocusedRef(cx)`, falling back to `pendingOpener` recorded on mouse press) onto `dialogOpeners` when opening and restores it inside `OnClose` after `core.Modal` has fully left the tree (so `Ref.Focus()` is not rejected by a fading-out modal).
   - While any dialog is open (`openDialogCount > 0`), `dialogFocusTrapView` binds `tab` and `shift+tab` (`OverModals: true`) to `core.MoveFocus(..., FocusDirNext/Prev, true)`, which strictly scopes stops to `topModal(host)` in both headless `nsgotest` and native platforms. Global shortcuts remain suppressed behind dialogs except `mod+.` (`OverModals: true`, `Editor.OnStopAgent`), matching Spec rule 04 and complementing [editor shortcuts, menu bar, and Command Palette](2026-11-25-editor-shortcuts-menu-palette.md).
5. **Focus-stability guards**:
   - `Editor.IsDragging()`, `Editor.IsTyping()`, `Editor.FocusInAgentPanel()`, `Editor.CanAutoMoveFocus()`, and `Editor.ShouldFocusAgentPrompt()` enforce Spec rule 05: nothing steals focus during a drag or while typing, toasts announce politely without moving focus, and agent permission cards or questions may take focus only when focus was already in the agent panel.

## NSGo v0.3.0 gaps worked around in the editor

- `ui.FocusRing` reads `Palette.Colors["info"]` instead of a dedicated focus-ring role -> bridged by `FocusTheme()`.
- `ui.Switch` carries no `FocusVisible` style -> bridged by `SpecSwitch` (`base.Switch` with `HitSlop` and `+2 px` offset `FocusVisible`).
- `ui.IconButton` hardcodes `-2 px` offset on ghost buttons and omits `FocusVisible` on circular buttons -> bridged by `SpecIconButton`.
- `ui.Input` draws only a 1 px border color change when focused -> bridged by `specInputField` adding `-2 px` inset `FocusVisible`.
- `core.Modal` / `ui.Dialog` do not save or restore opener focus on dismissal, and `nsgotest.App.moveFocus` walks views behind `KindModal` -> bridged by `bindDialogFocusRestore` + `dialogFocusTrapView` delegating to `core.MoveFocus`.

## Alternatives considered

- **Wrapping every toolbar and panel button in a custom focus-tracking container** — Would bloat the view tree, risk wrapping the compact 2D `editor-toolbar`, and duplicate focus logic across dozens of files. Rejected because NSGo already provides `core.FocusedRef(cx)`, `core.Ref.HoldsFocus()`, and `OnFocusIn` / `OnFocusOut`, which let the region controller track and restore any mounted control in O(1) without extra wrappers.
- **Replacing NSGo's entire theme palette before #1002 lands** — Would alter light/dark surface colors tested by existing appearance tests and collide with #1002's YAML design-token theme work. Rejected in favor of overriding only the focus-ring roles (`info`, `Focus`, `ring`) over `core.DefaultTheme()`.
- **Restoring dialog opener focus synchronously inside `OnOpenChange(false)`** — Fails on desktop because `ui.Dialog` plays a 180 ms exit transition keeping `core.Modal` mounted until dismissal completes, and `Ref.Focus()` ignores targets below a modal. Rejected in favor of restoring inside `OnClose`.

## Consequences

- **Benefits**:
  - Every control across the editor shows a consistent 2 px `#3B7BFF` keyboard-only focus ring with the exact Spec offset (`+2 px` controls/cards, `-2 px` rows/fields/regions) and zero layout expansion on `editor-toolbar`.
  - Keyboard users can jump directly between all editor regions with `F6` / `Shift-F6`, keep their exact place in each region, cycle viewport objects with `Tab` / `Shift-Tab`, and open/dismiss dialogs with `Esc` without losing focus.
  - All NSGo v0.3.0 focus workarounds are isolated in `focus_regions.go` so they can be simplified once upstream NSGo adds native support.
- **Costs and known limits**:
  - Panels that mount raw `core.Input` directly instead of `specInputField` must wire `OnFocus` / `OnBlur` to `noteTextInputFocus` if they want `Editor.IsTyping()` to reflect them.
  - When #1002 replaces `DefaultTheme()` with the embedded Spec YAML theme, `FocusTheme()` should fold into that theme definition rather than maintaining a second theme builder.

## Verification

- `(cd packages/game && env -u GOROOT go tool nsgo check ./editor && env -u GOROOT go tool nsgo check -target ios ./editor)`
- `(cd packages/game && TMPDIR=/tmp env -u GOROOT go tool nsgo test ./editor)`
- `(cd packages/game && TMPDIR=/tmp env -u GOROOT go tool nsgo test -target macos -run TestFocusRingGalleryScreenshots ./editor)` (screenshots visually inspected against `docs/design/editor/Spec.dc.html` "Focus ring")
- `env -u GOROOT go run ./tools/changelog check && bun run verify-notes`
