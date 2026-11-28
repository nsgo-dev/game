# Agent Note: Epic Online Services boundary

Status: implemented

## Problem

Games need one asynchronous contract for Epic login, achievements, leaderboards and presence, but EOS is a platform-specific C SDK that is unavailable on most test and non-desktop targets. EOS operations are asynchronous: each call starts a request and its callback fires later from inside `EOS_Platform_Tick`, on the thread that ticks.

## Decision

`pkg/online/eos` exposes a callback-style `Driver` interface and an engine-goroutine `Platform`. Each driver method starts a request and calls its `done` exactly once, normally from `Tick`. `Platform` validates product identifiers and arguments, refuses reporting before login and after `Close`, calls the driver only on the engine goroutine, posts every result to its core scope, and drops results that arrive after `Close`. `ExchangeCodeFromArgs` reads the launcher's `-AUTH_PASSWORD` when `-AUTH_TYPE=exchangecode`. A nil driver yields `ErrUnavailable`. Tests inject a fake that completes requests only from `Tick`, as the SDK does.

## Alternatives considered

Synchronous driver methods taking a `context.Context`, as the first version of this package had, were rejected: a native adapter would have to block the engine goroutine until a callback that only fires from the `Tick` the same goroutine has to call, which deadlocks or forces the adapter to tick on another thread, which the EOS SDK does not allow. Binding the EOS C SDK directly in the shared package was rejected because it would make pure-Go tests and unsupported targets depend on native headers and shared libraries. Reusing `gameservices` was rejected because EOS has exchange-code login and richer leaderboard and presence operations that the generic service cannot represent.

## Consequences

Games can integrate EOS without changing gameplay code when a native adapter is added, and CI can cover login and reporting with a fake. The package does not ship the purego bindings, the developer auth tool login, the achievements `Reporter`, leaderboard provider or `presence.Provider` adapters, or the `epic` catalog IDs, which remain in #1770 and depend on the multi-store catalog work in #1764.
