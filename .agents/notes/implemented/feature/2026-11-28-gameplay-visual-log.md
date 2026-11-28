# Agent Note: Gameplay visual log core

Status: implemented

## Problem

Gameplay diagnostics and debug shapes existed only for the current frame. There was no bounded, portable record of an entity's system messages and shapes to inspect after a reproduction.

## Decision

`debug.VLog` stores bounded per-step entries tagged by entity and category, with message and numeric shape records. JSON Lines export/import preserves recording order, and `Entries` filters by entity and inclusive step range for scrubbing.

## Alternatives considered

Using the rolling input replay alone cannot capture system decisions or shapes. Keeping an unbounded in-memory log risks device memory growth. Both were rejected in favor of a bounded diagnostic stream.

## Consequences

Games can attach a deterministic visual-log recorder and send JSON Lines to an editor or device debugger. Built-in category snapshots, drawing integration, editor timeline UI, MCP queries, and release-tag elimination remain follow-up work.
