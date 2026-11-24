### Added

- `game/net`: players can join and leave a running lockstep or rollback match. The match's host (`LockstepConfig.Host` / `RollbackConfig.Host`, the lowest peer ID by default) admits a peer with `AddPeer` and removes one with `RemovePeer`; every peer applies the change at the same tick, which no peer can have run or confirmed yet, and rollback peers that predicted past it roll back. A session created with `Join` receives the host's `Sim` state at its join tick, in parts, and plays on from there; inputs before that tick are not expected from it. A leave carries the host's view of the leaving peer's inputs, with zero inputs for ticks it never received, so the match continues without waiting for it. `Members` and `Active` report who takes part, and messages the transport cannot deliver yet, such as to a peer a relay has not announced, are retried in order; once 4096 wait for one peer, the session returns the new `ErrSessionBacklog` for it (#744).

### Changed

- `game/net`: `LockstepSession.SubmitInput` ignores an input for a tick already run or more than 256 ticks ahead instead of sending it to peers that would drop it (#744).
