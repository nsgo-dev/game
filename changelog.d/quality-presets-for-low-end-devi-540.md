### Added

- `game/quality`: low, medium and high presets (resolution scale, particle budget, effects, shadows) picked from the device class and measured frame times, adapted while on auto and overridable through `save.DisplaySettings.Quality`, with a versioned YAML config; `Loop.FrameTime`, `lights2d.Scene.DisableShadows` and `world3d.QualityLevelFor` wire it in (resolution scale is a value for games to apply to buffers they size; nothing in this repository applies it yet), and Starburst starts low-end Android phones on the low preset (#540).
