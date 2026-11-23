### Added

- `game dev`, run as `go tool game dev`, runs the game with `nsgo dev` and passes it its asset directory (`resources` by default) in `NSGO_GAME_ASSETS`; `tooling/hotreload`'s `NewDevWatcher` starts a watcher there, `Watcher.WatchAssets` reloads the atlases, images and raw bytes an `assets.Manager` has loaded when their files are saved, within a second on the native and tui targets, and `Watcher.WatchAtlasFiles` reloads an atlas's JSON and image for atlases the game loads itself (#454).
- `game/assets`: `Manager.Reload` replaces the loaded assets read from a URI, updating atlases in place, and `Manager.Revision` counts those reloads for canvases and effects to redraw on (#454).
