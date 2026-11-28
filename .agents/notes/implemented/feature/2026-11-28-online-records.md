# Agent Note: Server-authoritative records transaction boundary

Status: implemented

## Problem

Cloud saves are client-owned opaque slots, so server economies and progression had no conditional, multi-record write boundary with visibility permissions.

## Decision

`pkg/online/records` models owner/collection/key JSON records with read permissions, monotonic versions and `Store.Apply` compare-and-set transactions. All expected versions are checked before any put or delete. `List` returns stable key order and copies JSON values. The in-memory implementation is the contract for SQLite/Postgres and wallet adapters.

## Alternatives considered

- Reuse cloud-save slots. Cloud saves are private and client-authoritative, so they cannot support public records or server-only writes.
- Apply each record independently. Independent writes permit partial grants and lost updates when a match result retries.

## Consequences

Server code has a small transaction contract for progression, public data and match results. Persistent storage, schema migrations, HTTP auth and JSON Schema validation remain adapter work and must preserve the CAS and permission semantics.
