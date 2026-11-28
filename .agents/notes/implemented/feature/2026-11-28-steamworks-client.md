# Agent Note: Steamworks shared client

Status: implemented

## Problem

Steam integrations need one initialized client and one callback pump. Presence and Workshop previously had separate native entry points, which risks double initialization, missed callbacks and inconsistent shutdown.

## Decision

`pkg/online/steamworks` owns a process-wide client with scope attachments. The first `Open` initializes the selected driver and the shared frame pump, later scopes reuse it, and the last scope cleanup shuts it down. Manual-dispatch callbacks are decoded centrally for overlay, joins, shutdown, auth tickets and Steam Deck text input. Purego desktop loading and an in-process fake share the `Driver` contract. Presence and Workshop reuse the client driver where available. The pump runs from one attached scope's animation frame; when that scope is disposed the pump moves to the next attached scope, and the last disposal fails pending `AwaitCall` and auth ticket waits with `ErrClosed` before `SteamAPI_Shutdown`. A launch `+connect` or `+connect_lobby` is held until the first handler is registered, so a handler registered after `Open` still receives it.

## Alternatives considered

Keeping one native client in each consumer was rejected because SteamAPI initialization and callback dispatch are process-wide. Requiring games to call Steam callbacks themselves was rejected because it makes callback ordering depend on each game's loop.

## Consequences

Steam services can share lifecycle and callback delivery, while unsupported builds return `ErrUnavailable` and tests avoid native libraries. The client boundary does not define achievement or leaderboard schemas, which remain in their packages and can attach to this client.
