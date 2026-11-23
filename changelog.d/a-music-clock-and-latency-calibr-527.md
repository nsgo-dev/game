### Added

- `game/rhythm`: versioned YAML beat maps with tempo and meter changes, a music `Clock` on NSGo's audio clock that syncs to a player and stays within a millisecond of the map, and audio/video latency calibration (`Calibration`, a gameui `CalibrationScreen`) with per-device offsets kept as YAML in `core.Storage` (#527).
