# Agent Note: Settings Video Tab Resolution and VSync

Status: implemented

## Problem

`SettingsMenu`'s Video tab ([Ready Settings Menu in game/gameui](../feature/2026-11-24-gameui-settings-menu.md)) previously rendered two controls that saved values without changing runtime behavior (#1494):

- **VSync**: toggled `save.DisplaySettings.VSync`, which neither `screen.Video` nor any engine package reads because NSGo v0.3.4 exposes no swap-interval or vsync control.
- **Resolution**: persisted an ID into `save.Settings.Custom["resolution"]`, which `examples/caverun` copied into an unread string field (`g.resolution`) without resizing the window or render surface, and displayed the row even on phones and the web where window resizing cannot apply.

## Decision

Wire Resolution end-to-end through `screen.Video` on desktop windowed targets, hide Resolution on platforms without resizable desktop windows, and omit VSync from `SettingsMenu`'s Video tab by default until NSGo adds swap-interval control.

### Cross-package contract (`pkg/screen`, `pkg/gameui`, `examples/caverun`)

- **`screen.Video` (`pkg/screen/video.go`)**: adds `NativeWindowSize core.Size`, `ApplyResolution(width, height core.Points)`, `Resolution() core.Size`, and `SupportsWindowResolution(env *core.Env) bool`.
  - While windowed on desktop (`SupportsWindowResolution` is true: `OSMacOS`, `OSWindows`, `OSLinux` when `!Simulated()`, or headless `nsgotest` with empty `OS` and `IdiomDesktop`/`IdiomMac`), `ApplyResolution` requests `cx.SetWindowFrame` centered on the window's current display work area (`env.Display().VisibleFrame`, falling back to `Frame`, then main/first entry in `env.Displays()`, clamping origin so an oversized resolution never pushes the title bar above the work area).
  - Non-positive width or height (`0, 0`, `"native"`) restores `NativeWindowSize`, falling back to the window's initial windowed size captured when `Run` starts.
  - Deduplication mirrors `applyFullScreen`: `ApplyResolution` skips sending a frame when the target equals the window's current size on the first request or equals the last size requested; `stop()` resets the deduplication flag so a subsequent `Run` compares afresh.
  - While in full screen, the requested resolution is retained and applied if the window later returns to windowed mode; full-screen render-size scaling waits on canvas resolution scaling (#1622).
  - `OSIOS`, `OSAndroid`, `OSWeb`, and `Simulated()` environments never emit `SetWindowFrame`.
- **`gameui.SettingsMenu` (`pkg/gameui/settings_menu.go`)**:
  - Adds `ParseSettingsResolution(id string) (width, height int, ok bool)` and `ApplySettingsResolution(v *screen.Video, s save.Settings, resolutions ...SettingsResolution)` to bridge `save.Settings.Custom["resolution"]` to `screen.Video` without altering `pkg/save`'s `Stable` schema.
  - Adds `SettingsMenuConfig.Env`, `SettingsMenuConfig.Video`, `SettingsMenuConfig.HideResolution`, and `SettingsMenuConfig.ShowVSync`.
  - `ShowVSync` defaults to `false`, omitting the VSync toggle from `VBox.Children` and `TabControls(SettingsTabVideo)` while leaving `save.DisplaySettings.VSync` intact for forward compatibility.
  - When `Env != nil && !screen.SupportsWindowResolution(Env)` (or `HideResolution` is `true`), the Resolution row is omitted from the Video tab and focus navigation skips cleanly from Window Mode to Scale Mode.
  - When `Video != nil`, `Apply()` and `Save()` invoke `ApplySettingsResolution` right after `save.Apply(snap, m.targets)` so full-screen state is updated first.
- **`examples/caverun` (`examples/caverun/settings.go`)**: configures `g.video.NativeWindowSize` from `gameProject.WindowSize()`, passes `Env: cx.Env()` and `Video: g.video` to `NewSettingsMenu`, and calls `gameui.ApplySettingsResolution` when restoring saved settings on startup.

## Alternatives considered

### Adding `Width`/`Height` fields directly to `save.DisplaySettings` and removing `save.DisplaySettings.VSync`

Putting resolution directly on `save.DisplaySettings` would let `save.SettingsTargets.ApplyDisplay` carry it without a separate helper, and removing `VSync` would eliminate an unused struct field. Both were rejected: `pkg/save` is a `Stable` package (`docs/en/stability.md`), so removing `VSync` would break compatibility without a deprecation cycle, and [Ready Settings Menu in game/gameui](../feature/2026-11-24-gameui-settings-menu.md) already established `save.Settings.Custom["resolution"]` as the persistence contract for menu-level resolution.

### Applying window resizing inside `OnResolutionChange` in Cave Run

Calling `SetWindowFrame` directly from `SettingsMenuConfig.OnResolutionChange` would require no changes to `screen.Video`. Rejected because `OnResolutionChange` fires while cycling options before the player confirms with Apply or Save (inconsistent with Fullscreen and FrameCap), would not run on Reset or startup restore, and would duplicate centering and fullscreen-deferral logic across every game.

## Consequences

- **Benefits**: Choosing a windowed resolution in Settings or restoring saved settings at launch resizes and centers the desktop window (with `Native` restoring the project's window size); phones and the web no longer show a dead Resolution control; players no longer see an unbacked VSync toggle by default.
- **Costs**: `pkg/gameui` (`Experimental`) now imports `pkg/screen` (`Stable`), which obeys the stability layering rule. Games with a custom resolution flow that opt into a backend VSync control must set `SettingsMenuConfig.ShowVSync: true`.
- **Deferred work**: Full-screen render resolution scaling remains deferred to canvas resolution scaling (#1622), and VSync remains opt-in until NSGo exposes a swap-interval API.
