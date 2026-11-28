# Agent Note: Persisted age bands and child mode

Status: implemented

## Problem

Games had no shared privacy primitive for asking a neutral age question and consistently recognizing child-directed play. Storing a date of birth would collect more personal data than this decision needs, while malformed local state must never enable child-restricted features.

## Decision

`liveops.AgeGate` stores version 1 JSON containing only one of four coarse bands in `core.Storage`. `NewAgeGate` rejects unknown fields, unsupported versions and invalid bands by returning `AgeUnknown`. `ChildMode` derives from `under13` and `13to15`. `AgeUnknown` clears the record and leaves the gate fail-closed.

## Alternatives considered

- **Persist the date of birth**: rejected because the privacy gate only needs a band and a date creates unnecessary sensitive data and retention obligations.
- **Treat malformed storage as adult**: rejected because corruption, migration errors or tampering must never silently enable child-restricted analytics or offers.

## Consequences

Games can persist a minimal age decision and feed one consistent child-mode signal to analytics and other app-owned features. The core does not guess a jurisdiction's digital-consent age, call platform age-signal APIs or implement a parental challenge. Apps must compose those policies and purchase gates around this primitive.

## Verification

`TestAgeGateStoresBandsAndFailsClosed` covers persistence, child-mode boundaries, invalid bands and future-version fail-closed behavior through `nsgotest.Storage`. `go tool nsgo check ./pkg/online/liveops` and the focused package test pass.
