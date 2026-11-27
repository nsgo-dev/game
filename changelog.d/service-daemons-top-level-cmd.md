### Changed

- The service daemons move to the top-level `cmd/`: install them with `go install github.com/nsgo-dev/game/cmd/accountsd@latest`, `go install github.com/nsgo-dev/game/cmd/cloudsaved@latest` and `go install github.com/nsgo-dev/game/cmd/leaderboardd@latest` instead of from `pkg/accounts/cmd/accountsd`, `pkg/cloudsave/cmd/cloudsaved` and `pkg/leaderboard/selfhost/cmd/leaderboardd`.
