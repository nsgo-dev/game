# Agent Note: Co-op 2D template

Status: implemented

## Problem

None of the templates uses the online packages, and there is no project to start a co-op game from that already has the room rules a co-op game most often gets wrong: joining by a code or a link, keeping a player's place while they reconnect, and what happens when a player goes down with or without a teammate (#1959).

## Decision

`template/coop2d` ships the authoritative room rules as a transport-free `Server` in `coop.go`, driven one fixed tick at a time, with its tuning in a versioned `coop.yaml`. The client is one screen showing those rules; the template has no network transport, lobby or play screen yet, and its README and guide say so.

- Rooms are explicit: `CreateRoom` opens a code and `Join` refuses an unknown code, so a mistyped code never silently opens a new room. `Join` also refuses a player already in the room; a returning player uses `Rejoin`.
- The grace period is counted from the tick a player left (`Player.LeftAt`), not from the room's start. `Rejoin` succeeds while `tick - LeftAt <= GraceTicks()`, and `Tick` removes a player past it and returns their ID as the expiry event, so the two never disagree about whether a player can come back. A room left empty is closed.
- Revive and respawn apply to connected, downed players only. Who is standing is counted once at the start of a tick, so a player revived this tick does not revive a teammate in the same tick and the outcome does not depend on map order. With someone standing the player is revived after `reviveTicks`; with no one standing the player gets up alone after `respawnTicks`. A disconnected downed player makes no progress and comes back still downed.
- `Server` hands out copies (`Room` returns a `RoomState` with players sorted by ID), never pointers into its maps, so callers cannot race the tick.
- `coop.yaml` refuses unknown fields and a second document, and `EncodeCoopConfig` writes it back byte for byte; `time.Duration` round-trips as `30s` through `go.yaml.in/yaml/v3`.
- `InviteLink` and `RoomFromInvite` use `net/url`, so a base link's own query parameters survive.

## Alternatives considered

- Building on `pkg/online/netplay` and `pkg/online/gameserver` in this change, as #1959 proposes: it is the end state, but the per-room match host it needs (#1318) and the resume token (#1692) are not there yet, and wiring a transport around rules that were not yet correct would have tested the transport instead of the rules. The rules land first, with the transport left to a follow-up.
- A dedicated-server `cmd/server` in the template: without a transport it could only tick an empty room, and a template's Go files cannot import the project's own package path (only `go.mod.tmpl` is rendered), so sharing `Server` with it needs the rules in a subpackage. It is left out until there is a transport to serve.
- Creating a room on the first `Join` of an unknown code: it saves a call but turns every mistyped code into a new empty room.

## Consequences

- A co-op game gets tested room rules to keep, with the tuning in a reviewable text file, and the tests cover joining by code, a full room, rejoining within the grace period with state kept, expiry, revive and solo respawn.
- The template does not yet satisfy #1959's networking acceptance: no client joins over a network, there is no server entry point, and players are identified by the ID the game gives them rather than by a resume token. The PR is part of #1959, not its whole.
- Moving `Server` into a subpackage later, for a server entry point, changes the project layout of games made from this version.
