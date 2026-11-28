# Agent Note: Server match result commits

Status: implemented

## Problem

Finished matches had no server-side idempotency key for applying records, wallet grants and scores exactly once.

## Decision

`gameserver.MatchResults` groups result payloads under a match ID, and `MemoryResultCommitter` applies the first result, treats identical retries as no-ops, and rejects conflicting retries.

## Alternatives considered

Letting callers issue separate writes allows partial rewards on crashes. A client-generated retry token unrelated to the match would not identify the authoritative result.

## Consequences

Database-backed committers can implement the same contract with a unique match-results row and outbox delivery. The in-memory implementation supplies deterministic tests and local prototypes.
