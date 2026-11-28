# eos

```go
import "github.com/nsgo-dev/game/pkg/online/eos"
```

`eos` is the Go boundary for Epic Online Services: exchange-code login, achievement unlocks and progress, leaderboard scores and queries, and rich presence, with every result delivered on the engine goroutine. The package defines the contract a native adapter implements, `eos.Driver`, and the `eos.Platform` that games call. It does not ship the native adapter yet: binding the EOS SDK shared libraries through purego, `epic` IDs in the achievement and leaderboard files, and shipping the library are tracked in [#1770](https://github.com/nsgo-dev/game/issues/1770). Until then a game passes its own driver, and tests pass a fake.

## Platform

`eos.New(cx, cfg, driver)` creates a platform from the product, sandbox and deployment IDs in `eos.Config` and closes it when `cx` is disposed. It returns `eos.ErrInvalidConfig` when an ID is empty and `eos.ErrUnavailable` when `driver` is nil, which is what a native adapter reports when the EOS shared library is missing.

```go
platform, err := eos.New(cx, eos.Config{
	ProductID:    "…",
	SandboxID:    "…",
	DeploymentID: "…",
	ExchangeCode: eos.ExchangeCodeFromArgs(os.Args[1:]),
}, driver)
```

The Epic launcher starts a game with `-AUTH_PASSWORD=<code> -AUTH_TYPE=exchangecode`; `eos.ExchangeCodeFromArgs` returns that code, or `""` for any other login type.

Call `platform.Tick()` every step of the game loop: EOS completes its requests from its tick. Call every other method on the engine goroutine as well:

- `Login(done)` signs in with the exchange code; afterwards `Player()` and `SignedIn()` report the account, and they are reactive reads. Without an exchange code it fails with `eos.ErrUnauthenticated`.
- `UnlockAchievement(id, done)` and `ProgressAchievement(id, percent, done)`, with `percent` in [0,100].
- `SubmitScore(board, score, done)` and `QueryLeaderboard(board, friends, limit, done)`, global or friends rows, `limit` positive.
- `SetPresence(details, status, done)`.

Reporting before `Login` succeeds fails with `eos.ErrUnauthenticated`, an empty ID or an out-of-range argument with `eos.ErrInvalidArgument`, and any call after `Close` with `eos.ErrClosed`. `done` never runs inside the call that started the request: the platform posts it to its scope, and drops results that arrive after `Close` or after the scope is disposed.

## Drivers

A `Driver` starts each request and returns at once, then calls the request's `done` exactly once, normally from inside its `Tick`, where the EOS SDK dispatches callbacks, or at once when the request cannot start. The platform calls the driver only on the engine goroutine; a driver may call `done` from any goroutine.
