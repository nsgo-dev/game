### Fixed

- `game/screen`, `game/gameui`: defer saved web full-screen requests in `screen.Video` until the first user press (`Video.Attach` / `Video.ApplyPending`), sync `Video.Settings()` and `Video.OnWindowChanged` with external `Env.FullScreen` transitions, and hide or synchronize Window Mode in `gameui.SettingsMenu` via `SettingsMenuConfig.Env` and `screen.CanFullScreen` (#1482).
