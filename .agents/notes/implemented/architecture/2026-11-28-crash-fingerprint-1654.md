# Agent Note: crash report fingerprints

Status: implemented

## Problem

Crash reports had full stacks but no stable grouping key. File and line drift and goroutine details created separate signatures for the same failure.

## Decision

`crashreport.CrashReport.Fingerprint` hashes up to eight top application function frames, excluding runtime/testing frames and location details. It falls back to the panic message when a stack has no application frames.

## Consequences

Uploaders and self-hosted crash stores can group reports deterministically without exposing a separate mutable signature field. The full stack remains available for diagnosis. On-disk queueing and consent-aware upload remain follow-up work.

## Alternatives considered

Hashing the entire stack was rejected because paths, line numbers and scheduler goroutine IDs are unstable across builds and runs.
