# Agent Note: game-side accounts client

Status: implemented

## Problem

Games had to hand-roll session persistence and concurrent token refresh, often storing long-lived credentials in plain preferences.

## Decision

Add `liveops.Account` with a small `SecretStore` adapter, session restoration, single-flight `/v1/refresh`, email sign-in, sign-out, and `ErrSignedOut` handling. The account client keeps the token in memory and writes only the session to the caller's keychain.

## Consequences

Cloud clients can use `Account.Token` directly, and concurrent callers share one refresh request. Platform credential linking, conflict resolution, and automatic `core.Secrets` adapters remain follow-up work.

## Alternatives considered

Storing sessions in `core.Storage` was rejected because it is readable app data rather than a keychain boundary. A global token cache was rejected because games may host multiple account services or test accounts.
