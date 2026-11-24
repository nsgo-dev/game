### Added

- `game/net`: a replication epoch. `NewServerWorld` picks a random one, every snapshot carries it, and a `ClientWorld` that receives a complete full snapshot of a new epoch drops the old run's snapshots and predictions and continues from it without the game calling `Reset`, while late snapshots of an epoch it has left are ignored unless that epoch keeps sending, 32 snapshots in a row, which marks it as the live one. `ServerWorld.Epoch` and `SetEpoch` read and replace it, for example to start a new match with the ticks back at zero, and `ClientWorld.Epoch` returns the one the client follows (#745).
- `game/net`: `ReplicationVersion`, `ErrReplicationVersion`, `ClientWorld.Err` and `ServerWorld.PeerErr` report a peer that speaks another version of the replication wire protocol (#745).

### Changed

- `game/net`: the replication wire protocol is now version 2: every message starts with a version and type byte and the 4-byte epoch, as documented in `docs/en/net.md`. Clients and servers built with an earlier release cannot replicate to this one; each side refuses the other's messages and reports `ErrReplicationVersion` instead of misreading them (#745).
