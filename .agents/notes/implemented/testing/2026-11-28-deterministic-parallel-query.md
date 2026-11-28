# Agent Note: Deterministic parallel query iteration

Status: implemented

## Problem

Dense component queries were serial only, leaving component-local crowd and projectile updates unable to use multiple cores without each game inventing unsafe fan-out code.

## Decision

`ParEach2` drives a fixed dense store and partitions it by caller-selected `ChunkSize`. It runs inline for one worker and uses independent goroutines for multiple workers. The callback contract forbids structural world changes and limits writes to its supplied component pointers.

## Alternatives considered

- **Let each caller split stores manually**: rejected because it duplicates membership checks and makes structural races easy.
- **Partition by worker count**: rejected because changing workers would change chunk boundaries and invalidate ordered reductions.

## Consequences

Component-local systems can opt into parallel work while serial callers retain the zero-goroutine path. Callbacks have a stricter contract and ordered reduction APIs remain a follow-up.

## Verification

`TestParEach2IsStableAcrossWorkerCounts` runs the same query with one, two and eight workers. Package check and focused tests pass.
