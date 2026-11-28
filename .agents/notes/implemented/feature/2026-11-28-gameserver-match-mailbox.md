# Agent Note: Execute match callbacks through a bounded mailbox

Status: implemented

## Problem

Handlers and admin goroutines had no safe way to run code against game state owned by a match goroutine (#1695).

## Decision

`Match.Post` and `Match.Call` enqueue callbacks behind a bounded FIFO mailbox. The runner drains up to 64 callbacks before each update, preserving one-writer execution and spreading bursts across ticks. A stopped match rejects new posts and discards queued work.

## Alternatives considered

- Require every game to add its own channel: rejected because it duplicates ordering, bounds, and shutdown behavior.
- Run callbacks under a match mutex: rejected because it permits game state access from outside the match goroutine and can block tick scheduling.

## Consequences

Games can route room-scoped work through a standard one-writer contract. A full mailbox reports `ErrMailboxFull`, and callback panics are contained so the runner continues; callers should keep callbacks short.
