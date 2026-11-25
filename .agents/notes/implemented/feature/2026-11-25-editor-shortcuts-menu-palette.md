# Agent Note: Editor unified command registry, keyboard shortcuts, desktop menu bar, and Command Palette

Status: implemented

## Problem

The scene editor in `packages/game/editor` previously drove every action exclusively through toolbar and inspector buttons: authors had no keyboard shortcuts (save, undo, redo, delete, duplicate, tools, 3D gizmos, frame selection, play/stop, run game), no desktop application menu bar, no quick-launch command palette, and no way for plugins to register keyboard-driven commands. Wiring shortcuts, menus, and palette actions independently would invite drift in enabled states, labels, and shortcut conflicts, and careless global key bindings would hijack keys while typing in inspector text fields or search inputs.

## Decision

1. **Single unified command registry (`packages/game/editor/commands.go`)**:
   - `Editor` owns a unified slice of built-in and custom `EditorCommand` entries (`RegisterCommand`, `Commands`, `Command`, `ExecuteCommand`, `TriggerShortcut`), merged dynamically with `Plugin.Commands` from active plugins (`plugin.<plugin>.<id>`).
   - Shared action methods (`Save`, `DeleteSelected`, `DuplicateObject`/`DuplicateSelected`, `TogglePlay`, `RunGame` with injectable `OnRunGame`, and `FrameSelection`) back toolbar buttons, keyboard shortcuts, the desktop menu bar, and the Command Palette identically.
   - `FrameSelection` delegates to `FrameSelection2D` (moving the independent editor `View2D` without mutating `SceneDoc.Camera`) in 2D mode, and frames `Camera3D` on the selected object's AABB (or all objects) in 3D mode.
2. **Cross-platform shortcut normalization and clash detection**:
   - `NormalizeShortcut` canonicalizes modifiers (`mod`, `ctrl`, `alt`, `shift`, `meta`) and key aliases (`cmd` -> `meta`, `option` -> `alt`, `return` -> `enter`, `del` -> `delete`, `esc` -> `escape`). `FormatShortcut` formats shortcuts via `core.KeyLabel` for any target `core.OS`.
   - Every shortcut's collision keys are expanded for both Apple (`mod` -> `meta`) and non-Apple (`mod` -> `ctrl`) platforms. `RegisterPlugin` and `RegisterCommand` check against all built-in, custom, and already-registered plugin commands (including inactive plugins) before mutating state, refusing any cross-platform clash at registration time.
3. **Text-field focus guard via NSGo primitives**:
   - `EditorView` binds all active shortcuts inside a memo-gated `cx.Effect` using `cx.OnKeyWith(key, core.KeyOptions{When: core.KeyNotEditing}, ...)`, and every menu item sets `When: core.MenuNotEditing`. While any text input (`ui.Input`, `ui.NumberInput`, or palette search box) has keyboard focus, editor shortcuts never fire.
4. **Native desktop menu bar (`packages/game/editor/menubar.go`)**:
   - `Editor.ApplicationMenu` builds `File`, `Edit`, `View` (including `Tools` with active plugin tools and `3D Gizmo`), and `Project` (including `Plugins` when active plugins declare commands) directly from the command registry. `EditorView` keeps `desktop.SetApplicationMenu` in sync through a memo-gated `cx.Effect` so 60 Hz play-mode frame ticks do not re-push unchanged menus.
5. **Searchable Command Palette (`packages/game/editor/palette.go`)**:
   - `CommandPaletteView` hosts `ui.CommandDialog` (`TestID: "editor-command-palette"`), opened via `mod+k`, `View > Command Palette...`, or the toolbar **Commands** button.
   - It combines registered commands, active plugin tools (`SetPluginTool`), active plugin panels (`FocusPluginPanel`), and open project scenes/files (`OpenProjectFile`, cached on `ProjectPath` and `filesRev`), ranked by `base.FilterCommands` with a case-insensitive fuzzy subsequence fallback.

## Alternatives considered

- **Using `core.KeyAuto` instead of `core.KeyNotEditing`** — Would let `mod+s` or `mod+z` fire while focused in a text field because NSGo's `EditsText` only reserves text-editing keys. Rejected because issue #920 explicitly requires all editor shortcuts to be ignored while any text field has focus so typing or editing in the inspector's Name field never triggers editor Undo, Save, Delete, or tool switches.
- **Rendering an in-window custom menu strip instead of `desktop.SetApplicationMenu`** — Would display a fake menu row inside the window on macOS alongside the OS menu bar and bypass NSGo's native desktop menu integration and `nsgotest.App.ChooseMenu`. Rejected in favor of `desktop.SetApplicationMenu` plus the Command Palette button in the toolbar.
- **Checking plugin shortcut clashes only among currently enabled plugins** — Would allow two registered plugins to share a shortcut until a project enables both, failing late instead of at registration. Rejected because issue #920 requires clashes to be refused cleanly at registration time.
- **Mutating `SceneDoc.Camera` on 2D `FrameSelection`** — Would center the viewport by changing the scene document directly. Rejected because framing the viewport is a view navigation action and must delegate to `View2D` (`FrameSelection2D`) so it never dirties or modifies the author's saved scene YAML.

## Consequences

- **Benefits**:
  - Toolbar buttons, keyboard shortcuts, desktop menus, and the Command Palette execute the exact same command handlers and enabled rules with zero divergence.
  - Platform modifiers (`mod` -> Cmd on macOS, Ctrl on Windows/Linux) and text-field focus guards are enforced natively by NSGo and verified across macOS, Windows, and Linux in headless `nsgotest`.
  - Plugins extend the command palette, menu bar, and shortcuts declaratively via `Plugin.Commands` with deterministic registration-time conflict checks.
- **Costs and known limits**:
  - Shortcuts are currently static at registration time (user-remappable keybinding files are out of scope for #920).
  - `RunGame` delegates to `Editor.OnRunGame` when set and falls back to `Play()` when a UI scope is attached; launching an external standalone binary remains the host editor command's responsibility.

## Verification

- `(cd packages/game && env -u GOROOT go tool nsgo test ./editor/...)`
- `(cd packages/game && env -u GOROOT go tool nsgo test . -- -run 'TestStabilityTable|TestAgentSkillValid')`
- `(cd packages/game && env -u GOROOT go tool nsgo check -target macos,windows,linux,android,web)`
- `env -u GOROOT go run ./tools/changelog check && bun run verify-notes`
