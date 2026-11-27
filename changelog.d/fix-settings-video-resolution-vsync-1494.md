### Fixed

- `game/screen`, `game/gameui`: apply desktop windowed resolution via `screen.Video.ApplyResolution` (centered on the current display, restoring `NativeWindowSize` on `Native`), hide the Resolution row on phones, tablets, and the web when `SettingsMenuConfig.Env` is set, hide the unwired VSync toggle by default (`SettingsMenuConfig.ShowVSync` opt-in while NSGo v0.3.4 lacks swap-interval control), and wire `examples/caverun` through `screen.Video` (#1494).
