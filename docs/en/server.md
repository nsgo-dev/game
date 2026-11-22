# server

`github.com/nsgo-dev/game/packages/game/server` runs a game's fixed-step updates in a plain Go process, without an NSGo app, window, [`core.Scope`](loop.md) or cgo.

The `Update` function a game hands to [`game.Loop`](loop.md) runs unchanged on the server: both step it with the same fixed `1/Rate` duration, so a dedicated server and the client share one simulation.

## Runner

```go
r := &server.Runner{
	Matches: []*server.Match{
		{Name: "match-1", Rate: 60, Update: update},
		{Name: "match-2", Rate: 30, Update: update},
	},
	OnOverrun: func(m *server.Match, lag time.Duration, missed int) {
		log.Printf("%s overran by %v (%d ticks missed)", m.Name, lag, missed)
	},
}
if err := r.Run(ctx); err != nil { ... }
```

`Runner.Run` blocks until its context is canceled, then waits for each match's in-flight tick and runs its `OnStop` before returning. Each match runs on its own goroutine at its own rate.

## Ticks and overruns

A match's step is `1/Rate` seconds (60 Hz when `Rate` is zero or negative). When `Update` runs longer than one step, `Runner` calls `OnOverrun` with the lateness and the number of whole steps it covers. Missed ticks are **not** caught up: the next tick is scheduled from the moment the slow update returns, so a slow update slows the match rather than producing a burst.

## Stopping

A dedicated server usually runs under `signal.NotifyContext`:

```go
ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer stop()
```

On SIGTERM/SIGINT the context ends; the runner lets the current tick finish, then calls `OnStop` (for example to notify peers and flush state) and returns. `Update` must return promptly for the stop to stay bounded.

The runnable example is `server/cmd/dedicatedserver`:

```sh
go build ./server/cmd/dedicatedserver
```

It builds with the plain Go toolchain and no NSGo runtime.
