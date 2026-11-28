# Agent Note: Account session lifecycle boundary

Status: implemented

## Problem

Account sessions had no expiry, refresh rotation, reuse detection, device inventory or revoke-all primitive. Adding those directly to every Store and HTTP handler would require a coordinated schema migration before the lifecycle semantics were stable.

## Decision

`accounts.SessionDirectory` models lifecycle state independently from existing stores. `SessionPolicy` supports idle and absolute expiry, optional secret rotation, a bounded retry overlap and deterministic injected time. `Check` expires stale sessions and detects old-secret reuse, `Refresh` rotates generations, `List` returns device sessions, `RevokeAll` signs out other sessions and `Sweep` removes expired entries.

The existing Store and service APIs remain source-compatible. Persistent stores and HTTP endpoints can adopt the fields and operations through adapters while preserving the lifecycle rules.

## Alternatives considered

- Add all fields and methods to Store, SQLite and Postgres immediately. This would provide persistence in one change, but expands the migration and risks inconsistent expiry behavior across implementations.
- Keep sessions permanent and rely only on short-lived access tokens. That leaves stolen refresh secrets valid indefinitely and gives players no device control.

## Consequences

Games can test and adopt session expiry and rotation semantics now with deterministic clocks. Deployments needing restart persistence must wire the directory to a Store migration before enabling it as their source of truth.
