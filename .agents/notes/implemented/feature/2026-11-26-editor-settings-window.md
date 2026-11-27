# Agent Note: Unified Settings window (Cmd-,), General and Viewport pages, and extended per-person editor.yaml schema

Status: implemented

## Problem

Although earlier issues created the per-user `editor.yaml` file ([editor i18n and appearance](./2026-11-25-editor-i18n-and-appearance.md)) and consolidated the `PROJECT · game.yaml` pages into `SettingsView` ([unified project settings pages](./2026-11-26-editor-settings-project-pages.md)), five gaps kept the Settings window short of `docs/design/editor/Settings.dc.html` and issue #1005:

1. `⌘,` (`mod+,`) was bound to `CmdProjectSettings` (disabled when no project was open) rather than opening the unified Settings view globally, and `bindViewFocusRestore` captured opener focus inside a post-commit `cx.Effect` after `Disabled: settingsOpen` could already clear focus and lost its local `opener` ref whenever a language/appearance change remounted `workspace`.
2. There was no **Viewport** page (`SettingsPageViewport`), no **Interface size** control on **General**, and snapping/grid preferences lived only in per-project `.game/editor.yaml` ([editor snapping](./2026-11-25-editor-snapping.md)) rather than persisting per person in `$UserConfigDir/nsgo-game/editor.yaml`.
3. The Settings search field only dispatched on Enter/button press and did not live-narrow sidebar navigation, hide empty groups, narrow page rows, or show a dedicated no-match handoff to the agent.
4. The left navigation list lacked `ArrowDown` / `ArrowUp` / `Home` / `End` keyboard navigation, did not follow the `AGENT` → `PROJECT · game.yaml` → `EDITOR · THIS COMPUTER` group order, used `ui.Switch` (missing `:focus-visible` ring) for Auto-save, and rendered `+ Connect another agent or model` as a clickable `core.View` rather than a real `ui.Button`.

## Decision

Complete the unified Settings window and extend the version-1 per-person `EditorSettings` YAML format (`packages/game/editor/settings.go`, `settings_ui.go`, `focus_regions.go`, `commands.go`, `snap.go`, `viewstate.go`, `viewport.go`, `viewport3d_ui.go`, `ui.go`, `brief_rules.go`):

- **Extended per-person `EditorSettings` v1 schema (`$UserConfigDir/nsgo-game/editor.yaml`)**: Add four optional `omitempty` fields to `EditorSettings` while preserving byte-for-byte backwards compatibility with existing version-1 files (`testdata/settings.yaml`):
  - `interfaceSize` (`compact | default | large`, scaling editor typography and controls by `0.9x`, `1.0x`, or `1.15x` via `Editor.InterfaceSize` / `SetInterfaceSize`).
  - `snap` (`*SnapSettings`: `grid`, `gridSize`, `pixel`, `rotation`, `rotationStepDeg`, `scale`, `scaleStep`, `showGrid`), validated strictly for finite non-negative numbers and persisted whenever snap/grid preferences change with `SettingsPath` configured. In `viewstate.go`, `restoreProjectSnapState` falls back to `e.settings.Snap` (instead of `DefaultSnapSettings()`) when a project has no `.game/editor.yaml` snap override, so per-project overrides still win while fresh projects inherit the person's defaults.
  - `showAgentChanges` (`*bool`, defaulting to `true` when omitted; wired into `drawAgentTaskOverlays` via `Editor.ShowAgentChanges` / `SetShowAgentChanges`).
  - `navigation3D` (`default | blender | maya`; resolved by `Navigation3D.Resolve` across `viewport3DGestures`'s `pan`, `mouseDown`, and `wheel`, and reflected in `view3d-help` via `Editor.Navigation3D` / `SetNavigation3D` — see [3D navigation mouse mappings](../bug-fix/2026-11-28-editor-3d-navigation-mappings.md)).
  All fields are strictly decoded with `KnownFields(true)`, refuse YAML anchors/aliases/merge keys and duplicate keys, return `*loaderr.Error` on invalid values, serialize deterministically with `SettingsSchemaComment`, and regenerate `apps/website/public/schemas/editor-settings.v1.json` via `cmd/gameschema`.
- **Global `⌘,` / `Esc` and synchronous opener focus capture**: Move `"mod+,"` onto `CmdEditorSettings` (with `CmdProjectSettings` on `"mod+shift+,"`) so `⌘,` opens the full-window Settings view whether or not a project is open. Store `settingsOpener` and `settingsOpenerRegion` on `focusRegionController` and capture `core.FocusedRef` synchronously inside `OpenSettingsPage` before `e.settingsOpen = true` disables the underlying workspace. Pressing `Esc` (`core.KeyAlways`, yielding to shortcut/input key capture) closes Settings and restores focus to the exact opener control (or its region fallback if a live language/theme/size remount replaced the workspace tree).
- **Grouped sidebar with live search narrowing and arrow-key navigation**: Order sidebar groups `AGENT` → `PROJECT · game.yaml` → `EDITOR · THIS COMPUTER` with a live meta line on every entry. Typing in `settings-search` live-matches page titles, subtitles, row labels, help text, overlay legend entries, and shortcut commands across all pages; non-matching pages and empty groups are hidden, an inactive-match auto-switches to the first matching page, General/Viewport/Shortcuts narrow their visible rows, and a 0-match query renders `settings-search-no-match` with a primary **Send to agent** button whenever `Editor.AgentMode() != AgentModeNone`. Pressing `down` / `up` / `home` / `end` while focus is on a sidebar nav button moves focus and switches pages across the visible enabled items.
- **General & Viewport pages using Spec tokens and real controls**: Add **Interface size** to General, add the complete **Viewport** page (`SettingsPageViewport`: Snap switch + step `px` + angle `°` with inline validation, Grid switch, Show agent changes switch, 3D navigation toggle group, and all `ViewportOverlays()` legend switches), upgrade all toggles to `SpecSwitch` (`RoleSwitch`, `aria-checked`, 2 px accent focus ring), and replace the clickable `editor-settings-connect-agent` `core.View` with a real `ui.Button`.

## Alternatives considered

- **Bumping `EditorSettings` to `version: 2` for the new General/Viewport fields** — Strongest argument: makes the expanded schema explicit by version number. Rejected because all four new fields (`interfaceSize`, `snap`, `showAgentChanges`, `navigation3D`) are strictly additive and optional (`omitempty`), preserving lossless compatibility with existing `version: 1` `editor.yaml` files and `apps/website/public/schemas/editor-settings.v1.json` without forcing a migration path.
- **Replacing per-project `.game/editor.yaml` snap storage with per-user `editor.yaml` only** — Strongest argument: single source of truth for snap settings. Rejected because existing per-project snap tests (`TestSnapSettingsPersistPerProject` in `snap_test.go`) and authors switching between 8 px and 32 px tile projects rely on per-project `.game/editor.yaml` overrides; keeping `.game/editor.yaml` as a project override that falls back to `EditorSettings.Snap` satisfies both workflows cleanly.
- **Moving `OnKeyDown` arrow handling onto a custom clickable `core.View` nav row** — Strongest argument: encapsulates key handling on the row itself. Rejected because `Spec.dc.html` requires real `<button>` elements (`ui.Button`), and scoping `cx.OnKeyWith("down" / "up" / "home" / "end")` to `navRefs[pg].HoldsFocus()` preserves native `ui.Button` accessibility, Tab order, and focus rings without interfering with text inputs or radio groups.

## Consequences

- **Benefits**:
  - `⌘,` opens Settings from anywhere and `Esc` returns focus to the exact control that had it, even after live theme/language changes or typing in the search field.
  - Every General and Viewport preference (including snap step/angle, grid, show agent changes, 3D navigation, interface size, and overlay legend choices) writes deterministic, schema-validated per-user `editor.yaml`, survives restarts, and round-trips byte-for-byte unchanged.
  - Search narrows both navigation (with zero empty groups) and rows across pages, handing unmatched natural-language queries to the agent when available.
  - Full keyboard accessibility across General and Viewport (`Tab`, arrow keys in the left list, `RoleSwitch` + `aria-checked`, 2 px Spec focus rings, real buttons only).
- **Costs and known limits**:
  - Updates the search limitation previously noted in [unified project settings pages](./2026-11-26-editor-settings-project-pages.md) for General, Viewport, and Shortcuts rows; deep row-level filtering inside the specialized Project/Input/Audio/Export form sub-panels remains at the page level.
  - `InterfaceSize` scales Settings view typography and controls directly and triggers a live workspace remount via `chromeKey`; deeper per-panel control scaling across legacy docked panels can build on `Editor.InterfaceScale()` incrementally.

## Verification

- `(cd packages/game && env -u GOROOT go run ./cmd/gameschema -check)`
- `(cd packages/game && env -u GOROOT TMPDIR=/tmp go tool nsgo test ./editor/...)`
- `(cd packages/game && env -u GOROOT TMPDIR=/tmp go tool nsgo test -target macos -run '^TestSettingsWindowScreenshots$' ./editor)`
- `(cd packages/game && env -u GOROOT go tool nsgo check -target macos,windows,linux,android,web ./editor/...)`
- `(cd packages/game && env -u GOROOT go tool nsgo check -target ios,ios_simulator ./editor/...)`
- `env -u GOROOT go run ./tools/changelog check && bun run verify-notes`
