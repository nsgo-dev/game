### Added

- `game/screen`: `Video` applies the saved display settings at start and on change: windowed or borderless full screen on a chosen display (`save.DisplaySettings.DisplayID`, falling back to `DisplayIndex`), `FrameCap` as the loop's `DrawRate`, and with `AutoDropLowPower` a lower cap while the system is in low-power mode or under serious thermal pressure, restored once it clears (#356).
