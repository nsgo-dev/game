### Added

- `game/input`: handle mid-game controller disconnects on `Players` with `SetDisconnectPolicy` (`DisconnectPolicy`, `DisconnectEvent`, `ReconnectEvent`), automatic pause and resume (`OnPause`), same-pad reconnect and press-any-button reassignment (`AllowReassignOnPress`), single-player keyboard/touch fallback (`FallbackToKeyboardTouch`), and `PollGamepads` (#316).
