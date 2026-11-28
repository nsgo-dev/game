# Agent Note: Idempotent match result commit boundary

Status: implemented

## Problem

Finished match rewards had no server-owned commit boundary. Retried requests could double grant records or wallets, while partial updates could leave scores and rewards inconsistent.

## Decision

`gameserver.ResultsBackend` keys a result by `MatchID`, validates record versions before mutation and applies record, wallet and best-score grants atomically under one lock. Repeating the same result is a no-op receipt. A different payload for an existing match ID returns `ErrResultConflict`.

The in-memory implementation is the contract boundary for persistent database adapters and score outboxes. It avoids importing wallet or records packages that are still under development.

## Alternatives considered

- Let match code call each service independently. Separate calls can fail between grants and duplicate on retry.
- Use a timestamp as idempotency key. Timestamps collide across retries and are not stable across processes.

## Consequences

Match handlers can safely retry a finished result and know that a successful receipt is applied once. Production deployments still need a durable adapter to survive process restarts and to deliver external leaderboard outbox messages.
