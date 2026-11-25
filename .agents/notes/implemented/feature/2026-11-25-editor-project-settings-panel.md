# Agent Note: Editor Project Settings panel over game.yaml

Status: implemented

## Problem

Although `Editor.OpenProject`, `Editor.EditProject`, `Editor.SaveProject`, and `Editor.SetPluginEnabled` already supported validated, undoable edits of `game.yaml` with automatic `nsgo.yaml` synchronization, nothing in the editor UI exposed project settings. Authors had to edit `game.yaml` by hand to configure general metadata, window properties, virtual display settings, autoloaded scenes/resources and main scene, or editor plugins, and had no in-editor notice that saving rewrites `game.yaml` in canonical YAML without preserving comments.

## Decision

Add a dedicated Project Settings panel in `packages/game/editor/project_settings_panel.go` wired into `EditorView` via a toolbar **Settings** toggle button (`toolbar-project-settings`), with minimal hooks in `state.go`, `project.go`, `ui.go`, and `doc.go`:

- **Exclusively `EditProject`-backed mutations**: Every field setter (`SetProjectName`, `SetProjectID`, `SetProjectAppVersion`, `SetProjectWindowTitle`, `SetProjectWindowSize`, `SetProjectWindowLimits`, `SetProjectWindowResizable`, `SetProjectWindowAppearance`, `SetProjectDisplayEnabled`, `SetProjectDisplaySize`, `SetProjectDisplayMode`, `SetProjectDisplayIntegerScale`, `SetProjectDisplayPixelSnap`, `SetProjectDisplayBarColor`, `SetProjectMainScene`, `AddProjectAutoloadScene`, `UpdateProjectAutoloadScene`, `RemoveProjectAutoloadScene`, `AddProjectAutoloadResource`, `UpdateProjectAutoloadResource`, `RemoveProjectAutoloadResource`) and plugin toggle (`SetPluginEnabled`) mutates `game.yaml` exclusively through `Editor.EditProject`. Every edit validates against `project.Validate`, participates in `Editor.History` (Undo/Redo), and writes both `game.yaml` and synced `nsgo.yaml` through `SaveProject`. Input, audio, and export settings remain outside this panel because each has its own panel.
- **Per-field inline validation without silent clamping**: Text fields commit on Enter or blur and retain their rejected draft alongside an inline `<testID>-error` message when `project.Validate` refuses the value, leaving `Editor.Project` and `Editor.History` untouched. Numeric inputs omit `ui.NumberInput` min/max clamping so out-of-range values (such as `0` width or `minWidth > width`) reach `project.Validate` and display the exact schema error beside the field rather than silently rewriting the user's input.
- **Canonical YAML first-save notice**: `Editor` tracks `projectSavedOnce` (reset to `false` on `OpenProject`, set to `true` after `SaveProject` writes `game.yaml`). While `ProjectCanonicalSavePending()` is true, the panel renders a banner (`project-settings-canonical-notice`) stating that saving writes `game.yaml` in canonical YAML form without preserving comments.
- **Project-file pickers for Autoload and plugin inspection**: Autoload scenes and resources pick slash-separated paths from `ProjectSceneFiles()` (`.yaml`/`.yml` excluding `game.yaml`/`nsgo.yaml`) and `ProjectResourceFiles()` via searchable `ui.Select` controls, keeping any existing path selectable even if missing on disk. The Plugins section renders a `ui.Switch` for each `RegisteredPlugins()` entry via `SetPluginEnabled` and lists `MissingPlugins()` with a Remove action.

## Alternatives considered

- **Clamping numeric fields in `ui.NumberInput` before calling `EditProject`** — Prevents users from typing invalid numbers like `0` or negative limits. Rejected because cross-field constraints (`minWidth <= width <= maxWidth`) still require `project.Validate`, and silent clamping hides why an input was rejected instead of showing `project.Validate`'s error next to the field as required.
- **Showing the canonical YAML notice only when the on-disk `game.yaml` differs byte-for-byte from `project.Encode`** — Avoids warning on already-canonical files. Rejected because authors may not know whether saving preserves formatting or comments across edits; showing the notice before the first `SaveProject` of any opened project guarantees consistent visibility before the first canonical rewrite.
- **Putting all five sections in a single long scrolling form without category tabs** — Removes one click between sections. Rejected because the bottom panel is height-bounded (~300 pt); a left category rail (`General`, `Window`, `Display`, `Autoload`, `Plugins`) keeps each section scannable without excessive scrolling, matching Godot's Project Settings organization.

## Consequences

- **Benefits**:
  - Authors inspect and edit all core `game.yaml` sections visually with full Undo/Redo, immediate `project.Validate` feedback beside each field, and automatic `nsgo.yaml` synchronization on Save.
  - Because every edit routes through `EditProject` and `project.Validate`, pack ownership constraints (`validatePacks`) and schema invariants are enforced uniformly across Go API and UI.
  - Keeping all logic and views in `project_settings_panel.go` / `project_settings_panel_test.go` minimizes shared-file churn in `editor`.
- **Costs and known limits**:
  - Editing `window.width` and `window.height` (or `display.width` and `display.height`) separately pushes one undo step per field; changing width and height sequentially takes two Undo steps to revert both.
  - Non-numeric text typed into a `ui.NumberInput` is reverted by the widget itself before `OnChange` fires, whereas numeric constraint violations (`0`, negative values, inverted limits) reach `project.Validate` and display inline errors.

## Verification

- `(cd packages/game && env -u GOROOT go tool nsgo test ./editor/...)`
- `(cd packages/game && env -u GOROOT go tool nsgo test ./project/...)`
- `(cd packages/game && env -u GOROOT go tool nsgo test . -- -run 'TestStabilityTable|TestAgentSkillValid')`
- `(cd packages/game && env -u GOROOT go tool nsgo check -target macos,windows,linux,android,web)`
- `env -u GOROOT go run ./tools/changelog check && bun run verify-notes`
