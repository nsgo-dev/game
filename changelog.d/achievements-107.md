### Added

- `game/achievements`: achievements declared in a versioned YAML file with per-service Game Center and Play Games IDs, a `Tracker` with monotonic progress persisted in `core.Storage` and saved outside its lock, and `SignIn`, which reports unacknowledged progress through NSGo's `gameservices`; SlideGem reports its achievements to both services (#107).
