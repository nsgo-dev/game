# Agent Note: Editor Settings › Shortcuts, command scopes, and ⌘/ shortcuts sheet

Status: implemented

## Problem

The editor command registry ([unified command registry](./2026-11-25-editor-shortcuts-menu-palette.md)) bound every built-in and plugin command to fixed shortcuts with global flat conflict checks, and `EditorSettings` ([language and appearance settings](./2026-11-25-editor-i18n-and-appearance.md)) only stored language and appearance. Authors had no way to inspect all commands in one place, rebind keys per keyboard scope, swap conflicting bindings within a scope, or persist custom shortcuts across restarts. In addition, `ViewportView` duplicated a hardcoded `f` key handler and `view2DToolsView` hardcoded `cx.KeyLabel("f")` in its Frame button label instead of reading the active binding from the registry.

## Decision

Command scopes, per-user shortcut overrides, the Settings › Shortcuts UI, and the `⌘/` reference sheet are integrated directly into the unified command registry and `EditorSettings`:

- **Keyboard scopes on every command**: `EditorCommand.Scope` and `PluginCommand.Scope` classify commands into `ScopeGlobal`, `ScopeViewport`, `ScopeViewport2D`, `ScopeViewport3D`, `ScopeSceneTree`, `ScopeTile`, `ScopeAnimation`, and `ScopePlay`, defaulting to `ScopeGlobal` when unset so existing commands and plugins remain backwards-compatible.
- **Scope-checked rebinding and swap**: `ShortcutConflict(cmdID, newShortcut)` checks only active commands in the **same** scope. `RebindShortcut` refuses same-scope clashes with `*ShortcutConflictError` while allowing a key already used only in a different scope to bind immediately without prompting. `SwapShortcut` exchanges the two commands' keys within the same scope so both remain bound. `ResetShortcut` restores a single command's default (handing its previous key to a same-scope occupant if needed) and `ResetAllShortcuts` clears all overrides.
- **Scope-aware runtime key dispatch**: `bindShortcuts` deduplicates keys shared across different scopes and registers one `cx.OnKeyWith(..., core.KeyNotEditing)` handler per unique key that dispatches through `resolveShortcutCommand` (ranking active context scope priority, with explicit user overrides breaking ties over untouched defaults in other scopes). The duplicate hardcoded `cx.OnKey("f", ...)` in `ViewportView` is removed so `CmdFrameSelection` is the single source of truth. While key capture is active in Settings › Shortcuts, `Editor.shortcutCaptureActive` prevents editor shortcuts from firing and `OnKeyDown` consumes captured keys and `Escape` cleanly without closing the dialog.
- **Per-user persistence in `EditorSettings`**: `EditorSettings.Shortcuts` (`yaml:"shortcuts,omitempty"`) stores normalized overrides by command ID (omitted when empty) in `$UserConfigDir/nsgo-game/editor.yaml`, outside any game project, validated strictly by `ParseEditorSettings` and published in `editor-settings.v1.json`.
- **Immediate updates across every UI surface**: `Editor.Commands()` applies normalized scopes and effective overrides to built-in, custom, and active plugin commands alike. `bindApplicationMenu` includes `shortcutsDigest()` in its memo key, `CommandPaletteView` and `ShortcutsSheetView` (`CmdShortcutsSheet` / `mod+/`) read `e.Commands()` reactively, and toolbar tooltips and labels read `EffectiveShortcut`. Both `EditorSettingsDialogView` (General + Shortcuts sidebar pages) and `ShortcutsSheetView` are styled exclusively with `Spec.dc.html` tokens.

## Alternatives considered

- **Storing custom shortcuts inside `game.yaml`**: rejected because keyboard preferences belong to the author on their computer, not shared project files committed to Git, matching `Settings.dc.html` ("This computer") and the boundary established in [language and appearance settings](./2026-11-25-editor-i18n-and-appearance.md).
- **Keeping flat global conflict checks during rebinding**: rejected because `Spec.dc.html` rule 07 ("Rebinding checks the scope") requires allowing the same key in distinct scopes (such as rebinding Viewport's Frame Selection from `F` to `G` while Tile's Fill Tile also uses `G`) and flagging conflicts only within the same scope with an in-place **Swap** action.
- **Relaxing `RegisterPlugin` / `RegisterCommand` clash checks for unscoped registrations**: rejected because existing plugin registration guarantees (`TestPluginCommandShortcutClashRefused`) expect unscoped registrations (`Scope == ""`) to reject collisions with built-in shortcuts at registration time; only registrations declaring an explicit `Scope` scope-filter at registration time, while interactive rebinding always checks strictly within the command's scope.

## Consequences

- **Benefits**: Every built-in, custom, and plugin command is searchable by name, key, or scope, rebindable with portable `mod` normalization (`ShortcutFromKeyEvent`), swappable in place on same-scope conflicts, resettable individually or all at once, persisted outside projects, and reflected immediately in tooltips, the desktop menu bar, Command Palette, runtime key dispatch, and the `⌘/` shortcuts sheet.
- **Costs**: Adding `Shortcuts map[string]string` makes `EditorSettings` non-comparable with Go's `==` operator (callers and tests compare via `reflect.DeepEqual` or field checks). When two enabled commands in different scopes share a key, runtime dispatch resolves ambiguity by active scope priority and explicit user overrides rather than requiring explicit region focus tracking on every panel.
