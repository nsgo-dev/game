### Added

- `game/sound`: `Mixer.SetPaused`, `Mixer.Paused`, `Mixer.SetPauseExempt`, and `Mixer.PauseExempt` pause and resume gameplay effects and music while letting pause-exempt groups (such as `"ui"`) keep playing, while backgrounding continues to silence and resume all groups in sync with `game.Loop` (#358).
- `game`: `Loop.OnPause` notifies listeners whenever `Loop.Paused` changes so `sound.Mixer.SetPaused` and other subsystems stay in sync with game pause (#358).
