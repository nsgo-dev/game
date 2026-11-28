### Added

- `gameui.FirstLaunchSettings` (`github.com/nsgo-dev/game/pkg/gameui`) shows a one-action accessibility prompt before a game's first sound, turns captions on when the player continues, and records its completion under the `first_launch_accessibility_complete` custom key of the player's settings in `save.DefaultSettingsStore`, so it is not shown again; the starter template mounts it (#1783).
