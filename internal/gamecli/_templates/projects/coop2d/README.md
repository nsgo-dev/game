# Co-op 2D template

The authoritative rules of a 2D co-op room, built with [NSGo](https://github.com/nsgo-dev/nsgo) and [`github.com/nsgo-dev/game`](https://github.com/nsgo-dev/game), for one to four players. `Server` in `coop.go` keeps any number of rooms and advances each one fixed tick at a time:

- Rooms are created with a short code (`RoomCode`) and joined by it; `InviteLink` and `RoomFromInvite` put the code in a link's `room` query parameter and read it back.
- A player who leaves keeps their place and state for the grace period and gets it back with `Rejoin`; when the period runs out, `Tick` removes the player and returns their ID.
- A downed player (`Down`) is revived after `reviveTicks` consecutive ticks while a teammate is connected and standing, and gets up alone after `respawnTicks` ticks when no one is.

The rules and their tuning live in `coop.yaml`, a versioned file that refuses unknown fields and that `EncodeCoopConfig` writes back unchanged. The client is a single screen showing those rules. The template has no network transport yet: a game connects `Server` to its clients over `pkg/online/netplay` or its own transport, and draws its own lobby and play screens.

The game's settings live in `game.yaml`, its project file. Edit the name and identifier there before shipping it, then write them into `nsgo.yaml` and check the project:

```sh
go run github.com/nsgo-dev/game/cmd/game project sync
go run github.com/nsgo-dev/game/cmd/game project check
```

Run the tests, which drive the room rules headlessly, with:

```sh
go tool nsgo test ./...
```
