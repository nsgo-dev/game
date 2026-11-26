# Agent Note: Project, Input map, Audio buses, and Export as unified Settings pages

Status: implemented

## Problem

Previously, `ProjectSettingsPanelView`, `InputMapPanelView`, `AudioMixerPanelView`, and `ExportPanelView` rendered as four separate height-constrained bottom panels beneath the scene viewport, while per-user language and appearance preferences lived in a separate modal dialog (`EditorSettingsDialogView`). This split contradicted the editor design specification (`docs/design/editor/Settings.dc.html` and `Spec.dc.html`), made `game.yaml` settings harder to scan and compare alongside `"Agent may change"` vs `"You only"` policies, omitted Export preset readiness badges, and left three clickable `core.View` rows (`RoleButton` + `OnPress` in `audio_mixer_panel.go` and `input_map_panel.go`) unreachable via keyboard `Tab` navigation and missing visible focus rings.

## Decision

Consolidate all four `game.yaml` configuration editors and the per-computer General editor preferences into one unified `SettingsView` overlay (`packages/game/editor/settings.go`, `settings_ui.go`, `ui.go`) while preserving every existing Go API and test contract:

- **Single unified Settings state (`SettingsPage`)**: `Editor` stores `settingsOpen bool` and `settingsPage SettingsPage` (`SettingsPageProject`, `SettingsPageInput`, `SettingsPageAudio`, `SettingsPageExport`, `SettingsPageGeneral`) accessed via `OpenSettingsPage`, `SettingsPage`, `SettingsOpen`, `CloseSettings`, and `SetSettingsOpen`. Existing panel toggles (`ProjectSettingsOpen` / `SetProjectSettingsOpen`, `InputMapOpen` / `SetInputMapOpen`, `AudioMixerOpen` / `SetAudioMixerOpen`, `ExportPanelOpen` / `SetExportPanelOpen`, and `EditorSettingsOpen` / `SetEditorSettingsOpen`) delegate directly to the corresponding `SettingsPage`, so toolbar buttons, menu bar items, command palette entries, and existing Go callers work unchanged without bottom-panel duplication.
- **Workspace-body overlay preserving toolbar and background lookup semantics**: `SettingsView` mounts as an absolute overlay filling the workspace body below `toolbarView` and above `statusBarView`. Keeping `toolbarView` visible lets Save/Undo/Redo toolbar actions remain interactive while any Settings page is open (matching both user workflow and existing panel tests). Keeping `mainRow` mounted underneath with `Disabled: settingsOpen`, `AccessibilityElementsHidden: settingsOpen`, and `PointerEventsNone` prevents `Tab` focus or pointer hits from leaking behind Settings while preserving live-locale verification tests that inspect main workspace views while Settings is open.
- **Spec-token layout, policy badges, and Export readiness**: All four project pages use the shared `Spec.dc.html` color palette (`specBg`, `specPanel`, `specRaised`, `specLine`, `specTextStrong`, `specTextMuted`, `specAccent`, `specAgent`, `specAdded`, `specWarning`, `specProblem`) and `settingsRow` layout (label, help text, value control, and `"Agent may change"` or `"You only"` badge). The Project overview section surfaces Name, ID, Window size, Main scene, and App version while keeping the full Window, Display, Physics, Autoload, and Plugins tabs. The Input map page leads with a per-action summary of keyboard, gamepad, and touch triggers before the interactive binding/region/axes editor. The Export page computes preset readiness (`ExportPresetReadiness` / `ExportNavWarningMeta`) from `nsgo.yaml` Apple team ID presence and credits pre-flight, rendering `"Ready"` or `"Set up"` pills on each row and a warning pill on the sidebar's Export item.
- **Real accessible buttons everywhere**: Replaced all remaining clickable `core.View{Role: core.RoleButton}` rows in `audio_mixer_panel.go` (`mixer-bus-%d-select`) and `input_map_panel.go` (`input-action-%d-select`, `input-binding-%d-select`), and built all new Settings sidebar and row action controls with `ui.Button` carrying `FocusVisible: specFocusRingStyle` so every interactive control across all four pages is reachable by `Tab`, activatable via keyboard, and renders a 2px `#3B7BFF` focus ring.

## Alternatives considered

- **Unmounting `mainRow` completely while Settings is open** — Simpler DOM tree with no background nodes. Rejected because switching language live in Settings (`TestEditorUIChineseAndLiveSwitchWithoutRestart`) inspects workspace panel labels while Settings remains open, and unmounting would also reset transient scroll/viewport state in the scene editor whenever the user briefly opens Settings.
- **Covering the entire window (including `toolbarView`) with a fullscreen modal** — Matches a standalone preferences window. Rejected because users and tests trigger Save, Undo, and Redo from the editor toolbar while editing `game.yaml` settings, and keeping `toolbarView` visible avoids duplicating Save/Undo/Redo controls inside Settings.
- **Replacing `project_settings_panel.go`, `input_map_panel.go`, `audio_mixer_panel.go`, and `export_panel.go` with stripped-down summary-only views** — Smaller code surface. Rejected because the editor already exposes deep editing capabilities (DSP effect chains, live audition, touch/mouse region drag canvas, axes, autoload/physics/plugins, and live streaming release builds) with extensive regression tests; styling and embedding those complete editors inside the unified `SettingsView` preserves 100% of existing functionality and test coverage.

## Consequences

- **Benefits**:
  - `Project`, `Input map`, `Audio buses`, `Export`, and `General` share one coherent navigation sidebar (`PROJECT · game.yaml` + `EDITOR · THIS COMPUTER`), search bar, row anatomy, policy badges, and keyboard focus behavior.
  - All existing Project Settings, Input Map, Audio Mixer, Export, and Editor Settings APIs and tests pass without breaking changes, and `editor-toolbar` stays compact (`<= 2` rows at `1120x680`).
  - Every control on all four project Settings pages is keyboard-accessible via `Tab` with visible focus rings and inline `project.Validate` error reporting.
- **Costs and known limits**:
  - Supersedes the bottom-panel placement described in `.agents/notes/implemented/feature/2026-11-25-editor-project-settings-panel.md`, `2026-11-25-editor-input-map.md`, `2026-11-25-editor-audio-bus-mixer.md`, and `2026-11-25-editor-export-presets.md`, while preserving all of their `EditProject`, validation, undo/redo, and Go API contracts.
  - The Settings search input (`settings-search`) filters sidebar navigation entries by label/keywords rather than deep-filtering individual form rows across inactive pages.

## Verification

- `(cd packages/game && env -u GOROOT go tool nsgo check ./editor)`
- `(cd packages/game && env -u GOROOT go tool nsgo check -target ios ./editor)`
- `(cd packages/game && env -u GOROOT go tool nsgo check -target ios ./editor/editorcmd)`
- `(cd packages/game && env -u GOROOT TMPDIR=/tmp go tool nsgo test ./editor)`
- `(cd packages/game && env -u GOROOT TMPDIR=/tmp go tool nsgo test -target macos -run '^TestSettingsProjectPagesScreenshots$' ./editor)`
- `env -u GOROOT go run ./tools/changelog check && bun run verify-notes`
