### Changed

- The online service packages move under `pkg/online`: import `github.com/nsgo-dev/game/pkg/online/accounts`, `online/achievements`, `online/cloudsave`, `online/leaderboard`, `online/liveops`, `online/presence` and `online/server` (and their subpackages, such as `online/accounts/token` and `online/leaderboard/selfhost`) instead of `github.com/nsgo-dev/game/pkg/<name>`.
- `game/net` is renamed `netplay` and moves to `github.com/nsgo-dev/game/pkg/online/netplay`, so it no longer shadows the standard library's `net`: write `netplay.NewLoopbackHub` where code wrote `net.NewLoopbackHub`, and read its guide at [netplay](docs/en/netplay.md).
- The File Formats page's entries for these packages carry the `online/` prefix, such as `online/accounts` and `online/liveops#sync`.
