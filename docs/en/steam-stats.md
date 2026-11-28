# steam

```go
import "github.com/nsgo-dev/game/pkg/online/steam"
```

`steam` is the engine-facing boundary for Steam achievements, stats, leaderboards and rich presence. It defines a `Driver` interface that a Steamworks adapter implements and a `Platform` that wraps the driver for one `core.Scope`. This package ships no native adapter: a game injects its own `Driver`, and until one exists `New` with a nil driver returns `ErrUnavailable`. A `Driver` that calls Steamworks should attach to the process's shared client from [`steamworks`](steamworks.md) (`steamworks.Open`, then `ResolveSymbol` and `AwaitCall` for the `ISteamUserStats` flat API) instead of initializing Steamworks itself; that client already pumps callbacks and shuts Steamworks down, so the driver's `RunCallbacks` and `Shutdown` have nothing to do. To ship a build on Steam, see [Shipping on Steam](steam.md).

## Using a Platform

Create the platform with `steam.New(cx, driver)`; it closes itself when the scope is disposed. Call `Init` to sign the user in, pump `Tick` from the fixed loop so the driver can run Steam's callbacks, and use `Unlock`, `SetStatInt`, `SetStatFloat`, `StoreStats`, `UploadScore`, `Scores` and `SetPresence`. Achievements and stats reach Steam only when `StoreStats` commits them.

Every call takes a completion callback that runs on the scope's goroutine through `Scope.Post`, and none runs after the scope is disposed. `Init` reports `ErrUnauthenticated` when the driver returns no Steam ID, and every call on a closed platform reports `ErrClosed` at once.

## Testing

Implement `Driver` with a fake in tests: the fake records the achievements, stats and scores it receives, and `nsgotest.Start` runs the posted callbacks on `Advance`.
