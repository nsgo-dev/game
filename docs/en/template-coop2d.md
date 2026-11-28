# The coop2d template

`coop2d` holds the authoritative rules of a 2D co-op room for one to four players: room codes and invite links, a fixed tick, leaving and rejoining within a grace period, a teammate reviving a downed player, and a solo respawn when no teammate is standing. Create a project from it with:

```sh
go run github.com/nsgo-dev/game/cmd/game@latest new -template coop2d mygame
```

or with NSGo's own command, `nsgo init -template github.com/nsgo-dev/game/template/coop2d -module example.com/mygame mygame`. [Templates](templates.md) lists the others.

## What it holds

- **Rooms by code.** `Server.CreateRoom` opens a room under a code, which `RoomCode` derives as six upper-case characters, and `Server.Join` adds a player to it, refusing an unknown code, a full room and a player already in it. `InviteLink` writes the code into a link's `room` query parameter, keeping the link's other parameters, and `RoomFromInvite` reads it back.
- **Leaving and rejoining.** `Server.Leave` disconnects a player, who keeps their place and state, a downed player staying downed, for `rejoinGrace`. `Server.Rejoin` reconnects them within it. When it runs out, `Server.Tick` removes the player and returns their ID, and closes a room no one is left in.
- **Revive and respawn.** A player the game's rules mark with `Server.Down` is revived after `reviveTicks` consecutive ticks while a teammate is connected and standing, and gets up alone after `respawnTicks` ticks while no one is. Who is standing is decided at the start of each tick, so the result does not depend on the order of the players.
- **Tuning in YAML.** `coop.yaml` holds the version, the tick rate, the room size, the grace period and the two timers. `ParseCoopConfig` refuses an unknown field, a second document and an out-of-range value, and `EncodeCoopConfig` writes the file back byte for byte.

`Server.Room` returns a copy of a room's state with its players sorted by ID, and `Server` is safe for concurrent use.

## What it does not hold yet

The template has no network transport, lobby screen or play screen: the client is one screen showing the rules from `coop.yaml`, and the tests drive `Server` in process. A game connects `Server` to its clients over [netplay](netplay.md) or its own transport and runs it on its server; players are identified by the ID the game gives them, not by a resume token.
