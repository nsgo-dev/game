### Fixed

- `game/tooling/hotreload`: settle 0-byte reads before delivering file changes so truncate-then-write editor saves do not emit spurious empty-source `ReloadError`s while genuinely empty files still report errors, and make shader hot-reload tests write atomically (#962).
