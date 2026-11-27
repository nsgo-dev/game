### Added

- `game/input`, `game/project`, `game/save`, `game/a11y`, `game/gameui`: added fixed-step player motor assists (`input.Assists` with `Toggle`, `MashToHold`, `MinPress`, and `AcceptDelay`) on `Actions` and per `Player` with `SetPaused` / `ReleaseToggles`, `game.yaml` action `assist: {toggle, mash, mashRate}` declarations (`project.ActionAssist`), persistence in `save.Settings.InputAssists` and `a11y.Assist` (`ApplyToActions` expanding `HoldToToggle` from declared toggleable actions), replay determinism, and Controls tab rows in `gameui.SettingsMenu` (#1777).
