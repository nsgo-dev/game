### Added

- `game/presence`: rich presence on Steam (Steamworks `ISteamFriends` via purego) and Discord (pure Go local IPC v1 over Unix domain sockets and Windows named pipes) behind a provider-neutral `Activity` and `Manager` with level updates, deduplication, rate limiting and graceful degradation (#538).
