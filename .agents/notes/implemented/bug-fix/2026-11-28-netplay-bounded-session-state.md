# Agent Note: Netplay sessions bound their per-tick state against hostile and silent peers

Status: implemented

## Problem

`LockstepSession` and `RollbackSession` keep per-tick maps (checksums, snapshots, used and confirmed inputs) that the docs treat as bounded, but three paths grew them without limit. A lockstep member could send checksums for any future tick, and `pruneChecksums` only drops ticks behind its floor. A rollback member could send checksums for any past tick, and `pruneHistory` deleted `remoteChecksums` only for ticks it found in `computedChecksums`. And while one rollback peer sent nothing, `pruneHistory` held its floor at `confirmedTick - 1`, so `Step` kept a snapshot and inputs for every tick it predicted, none of which `rollbackTo` could use past `MaxRollback`, and `predictInput`, which scans `confirmedInputs`, got slower every tick. The first two let any member grow another member's memory one small message per entry; the third happens without malice whenever a peer drops before the host removes it.

## Decision

- `LockstepSession.recordRemoteChecksum` ignores a checksum for a tick more than `lockstepChecksumHistory` (256) ticks behind or `maxLockstepLeadTicks` (256) ticks ahead of `tick`, the same window inputs and pruning already use.
- `RollbackSession.checksumFloor()` is the one definition of the oldest tick whose checksums the session keeps: `start` until there is history to prune, otherwise `tickMin(currentTick-rollbackChecksumHistory, currentTick-MaxRollback-2, confirmedTick-1)` clamped to `start`. `recordRemoteChecksum` refuses ticks before it (and, as before, more than `maxRollbackLeadTicks` ahead), and `pruneHistory` prunes `computedChecksums`, `remoteChecksums`, `sentChecksum` and `reportedDesync` each by its own keys against it.
- `RollbackSession.Step` stalls once `currentTick - nextToConfirm() >= MaxRollback`: it polls, and if still stalled simulates nothing, ignores the local input, and returns `ErrRollbackStalled`. Stepping further would put the oldest unconfirmed tick beyond the rollback window, so a late input for it would fail anyway. `ErrRollbackStalled` wraps `ErrRollbackWindowExceeded`, so `errors.Is(err, ErrRollbackWindowExceeded)` holds as the docs always promised for a peer falling beyond `MaxRollback`, while callers can tell the temporary stall from the fatal late-input case. Confirmation only moves forward, so a poll can lift a stall but never cause one mid-step.

## Alternatives considered

- **Return plain `ErrRollbackWindowExceeded` on stall** — matches the advisory text and the existing doc sentence with no new symbol. Rejected because the same sentinel also means a late input beyond the window, after which the simulation is wrong; a caller following the docs example (`return err`) could not keep a stalled match alive. The wrapping sentinel keeps `errors.Is` compatibility and makes the stall distinguishable.
- **Keep advancing but cap the maps (drop the oldest snapshot)** — the game never pauses for a silent peer. Rejected: a late input for a dropped tick can no longer be rolled back to, so the session desyncs silently instead of failing loudly, and unconfirmed inputs still have to be kept.
- **Track the latest confirmed input per peer so `predictInput` is O(peers)** — the advisory suggests it. Not done: with the stall, `confirmedInputs` is bounded by `MaxRollback` plus the 512-tick input lead, so the scan is bounded too; changing prediction bookkeeping touches rollback correctness for no further safety gain.

## Consequences

- **Benefits**: every per-tick map in both sessions is bounded by its window (about 256 to 768 ticks, or `MaxRollback` plus the lead when `MaxRollback` is larger) times the member count, whatever members send or fail to send.
- **Costs and known limits**: a `RollbackSession` with a silent peer now pauses after `MaxRollback` ticks instead of predicting indefinitely; a peer whose first input arrives later than that still confirms, but the local peer waits for it. Callers that return every `Step` error must skip `ErrRollbackStalled`. A remote checksum for a confirmed tick older than the checksum floor is ignored, so its desync goes unreported.

## Verification

`go tool nsgo test -run 'StayBounded|SilentPeerStalls|LateFirstInput' ./pkg/online/netplay/` covers each path: `TestLockstepRemoteChecksumsStayBounded`, `TestRollbackRemoteChecksumsStayBounded`, `TestRollbackSilentPeerStalls` (stall, bounded history, resume with a rollback to the oldest tick) and `TestRollbackLateFirstInputBeyondMaxRollbackConfirms` (stall and resume across the `2^32` wrap).
