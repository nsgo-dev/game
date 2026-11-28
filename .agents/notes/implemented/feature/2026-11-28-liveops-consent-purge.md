# Agent Note: Consent-aware analytics purge

Status: implemented

## Problem

`Analytics` stopped new events after consent was revoked, but a provider that buffered events could still deliver events collected while consent was granted. That made revocation incomplete for durable or offline queues.

## Decision

Providers that retain events may implement `ConsentRevoker.Purge`. `Analytics.SetConsent` updates the persisted state and in-memory state first, then calls `Purge` once when the previous effective state was `ConsentGranted` and the new state is denied or unknown. Providers that do not buffer remain source-compatible through the existing `AnalyticsProvider` interface.

## Alternatives considered

- **Make `Purge` mandatory on every provider**: rejected because existing providers only send and flush and should not need a meaningless queue operation.
- **Call `Purge` before changing consent**: rejected because a failing storage write could leave a granted state while the provider had already discarded events.

## Consequences

Consent revocation clears provider-owned queues when the provider opts into the contract, while old providers continue to compile. A purge failure is returned after the consent state has already been revoked, so callers can report the operational failure without reopening collection. Network batching, queue persistence and collector transport remain separate work.

## Verification

Analytics tests cover purge on a granted-to-denied transition, no repeated purge for an unchanged denial, and storage-backed reset to unknown. `go tool nsgo check ./pkg/online/liveops` and package tests pass.
