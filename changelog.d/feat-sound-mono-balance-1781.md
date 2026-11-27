### Added

- `game/sound`, `game/save`, `game/gameui`: add mono voice panning (`Mixer.SetMono`, `Mixer.Mono`, `Mixer.FollowSystemMono`) and left/right stereo balance (`Mixer.SetBalance`, `Mixer.Balance`) backed by `save.AudioSettings.Mono` (follow-system by default) and `save.AudioSettings.Balance` plus Audio tab rows in `gameui.SettingsMenu` (#1781).
