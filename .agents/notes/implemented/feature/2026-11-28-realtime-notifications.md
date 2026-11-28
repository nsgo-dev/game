# Agent Note: Persist and fan out realtime notifications

Status: implemented

## Problem

Online services had no common notification inbox or in-process delivery primitive for a future authenticated realtime socket (#1686).

## Decision

`realtime.Store` tracks online state, persists notifications marked persistent, exposes cursor-friendly history and deletion, and fans out online notifications through bounded subscriptions. Socket authentication, SQL persistence, and cross-process Bus remain separate interfaces.

## Alternatives considered

- Poll each service independently: rejected because it duplicates storage queries and delays delivery.
- Persist every event: rejected because typing/status-style events should be transient.

## Consequences

Server code can deliver offline persistent notifications and online ephemeral events through one contract. Socket and database implementations can build on this transport-neutral core.
