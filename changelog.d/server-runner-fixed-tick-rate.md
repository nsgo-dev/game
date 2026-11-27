### Fixed

- `game/online/server`: `Runner` schedules each match tick against a fixed `1/Rate` deadline instead of sleeping a full step after `Update` returns, keeping the configured tick rate under normal load while still resetting the schedule without catch-up bursts after overruns (#1598).
