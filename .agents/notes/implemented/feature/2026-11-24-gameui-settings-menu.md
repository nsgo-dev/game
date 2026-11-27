# Agent Note: Ready Settings Menu in game/gameui

Status: implemented

## Problem

Games built with `game/gameui` had primitives (`Panel`, `VBox`, `HBox`, `Button`, `Toggle`, `Slider`, `List`, `SpatialNavigator`) and player-settings persistence (`save.DefaultSettingsStore`, `save.Apply`), plus runtime helpers (`screen.Video`, `quality.Selector`, `input.Actions`, `l10n.Catalog`, `a11y`), but every game still had to assemble its own multi-section settings UI, control-rebinding capture state machine, and gamepad-safe navigation graph.

## Decision

`game/gameui` provides `SettingsMenu` (`pkg/gameui/settings_menu.go` and `pkg/gameui/settings_bindings.go`), a ready-to-use five-tab settings screen wired directly to `save.Settings`, `save.DefaultSettingsStore`, and `screen.Video`, and integrated into `examples/caverun` (`examples/caverun/settings.go`).

### Structure and persistence contract

- `SettingsMenu` organizes controls into five tabs (`SettingsTabAudio`, `SettingsTabVideo`, `SettingsTabControls`, `SettingsTabLanguage`, `SettingsTabAccessibility`) over a persistent header, tab bar, active content panel, and footer (`Apply`, `Save`, `Reset`, `Back`). The Audio tab exposes mute, master, configurable bus sliders, a tri-state **Mono Audio** cycler (`System Default` / `On` / `Off`), and a **Balance** slider (`[-1, 1]`) backed by `save.AudioSettings` (see [Mono Audio and Left/Right Balance in game/sound](2026-11-28-sound-mono-and-balance.md)). In the Video tab, VSync is hidden by default (`SettingsMenuConfig.ShowVSync`) because NSGo v0.3.4 exposes no swap-interval control, and Resolution is hidden on phones, tablets, and the web when `SettingsMenuConfig.Env` is supplied (or `HideResolution` is set); windowed resolution is applied through `screen.Video.ApplyResolution` via `SettingsMenuConfig.Video` or `ApplySettingsResolution` (see [Settings Video tab resolution and VSync](../bug-fix/2026-11-28-settings-video-resolution-vsync.md)).
- All standard fields map directly onto `save.Settings` (`Audio`, `Display`, `Bindings`, `Locale`, `Accessibility`). Selected resolution ID remains stored in `save.Settings.Custom` under `SettingsCustomResolution` (`"resolution"`) with typed accessors (`SettingsResolutionFrom`, `SetSettingsResolution`). Flash reduction (`ReduceFlashes`) and the remaining assist toggles (`SFXCaptions`, `Invincibility`, `AutoAim`, `HoldToToggle`) moved onto `save.AccessibilitySettings` directly, with `SettingsReduceFlashesFrom` / `SetSettingsReduceFlashes` kept as deprecated wrappers and automatic migration from legacy `Custom["reduce_flashes"]` (see [Unify Accessibility Preferences Across save, a11y, and gameui](../bug-fix/2026-11-28-a11y-settings-assist-sync.md)).
- When `SettingsMenuConfig.Store` is set, `NewSettingsMenu` loads from `Store` only when `Store.Exists()` is true; otherwise it starts from `Initial` or `Defaults`, so game-specific defaults (such as Cave Run's `integerScale: true` from `game.yaml`) are not overwritten by `save.DefaultSettings()` before the player saves for the first time.

### Input, rebinding, and gamepad-alone invariants

- Every tab's focusable controls, tab-bar buttons, and footer buttons receive explicit `NavOverrides` whenever the active tab changes (`wireNav`), ensuring deterministic D-pad and left-stick traversal without spatial-search traps.
- Horizontal directions (`NavLeft` / `NavRight`) adjust sliders, flip toggles, and step option cyclers (window mode, resolution, scale mode, quality preset, frame cap, color-blind mode) inside the content panel, while moving horizontally across the tab bar and footer bar.
- `StepGamepad(pads []gamepad.Pad)` tracks per-pad button bitmasks and left-stick direction hysteresis (`0.5` activate / `0.35` release) to emit crisp rising edges for navigation, LB/RB tab switching, and rebinding capture. `SyncGamepad` records already-held buttons when opening the menu so the button used to open Settings never fires inside it.
- Control rebinding stores portable tokens (`key:<name>`, `keycode:<code>`, `gamepad:<button>`, `mouse:<button>`) in `save.Settings.Bindings`. Capturing a binding replaces the existing binding of the same device class (keyboard vs gamepad vs mouse) while preserving bindings from other device classes, and `ApplyBindingsToActions` updates `input.Actions` while optionally preserving `TouchRegion` and `MouseRegion` inputs from `game.yaml`.
- `OnTouch` tracks a single active touch ID across `TouchStart` / `TouchMove` / `TouchEnd` / `TouchCancel`, cancelling active presses and slider drags cleanly on `TouchCancel`.
- All user-visible strings live in `SettingsText` (updated dynamically via `SetText`), and when `Theme` is nil the menu switches automatically to `HighContrastTheme()` while `Accessibility.HighContrast` is true.

## Alternatives considered

### Extending `save.DisplaySettings` and `save.AccessibilitySettings` directly

Adding `Resolution` and `ReduceFlashes` fields to `save.DisplaySettings` and `save.AccessibilitySettings` would keep every setting as a first-class struct field. That was rejected because `game/save` is a `Stable` package whose JSON schema and `DefaultSettings` are relied on by existing tests and migrations; `save.Settings.Custom` is specifically designed for optional extra settings and round-trips losslessly through `DefaultSettingsStore`.

### Using `gameui.List` for Language and Video option pickers

Reusing `gameui.List` for locale and preset selection would require fewer button widgets. That was rejected because `SpatialNavigator.HandleAction` moves focus out of a `List` on `NavUp`/`NavDown` rather than stepping its selected index, breaking gamepad-only selection. Individual focusable language buttons and left/right-steppable cycle buttons work identically with mouse, touch, keyboard, and gamepad.

### Building the settings menu as native NSGo views (`core.View` / `ui.*`)

Native NSGo widgets (`ui.Button`, `ui.Slider`) already handle mouse and touch well, as Cave Run's pause menu demonstrates. However, issue #541 belongs to `game/gameui` (#307), which renders stylized, virtual-resolution-scaled in-canvas UI driven by `SpatialNavigator` for full gamepad navigation. Keeping `SettingsMenu` in `gameui` ensures consistent theming, `Viewport` scaling, and gamepad focus rings across games.

## Consequences

- **Benefits**: Any game can drop in a complete five-tab settings screen backed by `save.DefaultSettingsStore` with mouse, touch, keyboard, and gamepad-alone support, verified in both `packages/game/gameui/settings_menu_test.go` and `examples/caverun/settings_test.go`.
- **Costs**: `game/gameui` now imports `game/input`, `game/quality`, `game/save`, and `nsgo/gamepad`. Because `game/gameui` is `Experimental` in `docs/en/stability.md` and `docs/zh/stability.md`, this obeys the stability rule (`TestStabilityTable` passes), and if `game/gameui` is later promoted to `Stable`, the `game/quality` preset list default will need to copy the canonical level names as `game/save` does (unless `game/quality` is promoted first).
- **Trade-off**: Resolution lives under `save.Settings.Custom["resolution"]` rather than a typed `save.DisplaySettings` field, so callers reading it directly from `save.Settings` use `SettingsResolutionFrom` (while `ReduceFlashes` is now a first-class field on `save.AccessibilitySettings`, see [Unify Accessibility Preferences Across save, a11y, and gameui](../bug-fix/2026-11-28-a11y-settings-assist-sync.md)).
