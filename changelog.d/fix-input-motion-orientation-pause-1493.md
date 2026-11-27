### Fixed

- `game/input`: rotate tilt and gyroscope motion vectors from the device's natural frame into the active screen orientation via `MotionOptions.Orientation` (`Portrait`, `PortraitUpsideDown`, `LandscapeLeft`, `LandscapeRight`, and `Auto` with `LandscapeSide`) before smoothing and calibration, and add `MotionInput.SetPaused` so sensors stop while a game loop is paused and resume with calibration intact (#1493).
