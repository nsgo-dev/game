# Agent Note: Symmetric matchmaking ticket queries

Status: implemented

## Problem

Matchmaking tickets only grouped by mode, region and rating. Games encoded platform, version and latency compatibility into mode strings or rewrote the complete party-placement rule, and every match used exactly `RoomSize` players.

## Decision

Tickets carry copied string and numeric property maps and a typed `Query` of required, forbidden and optional terms. Candidate pairs satisfy both tickets' required and forbidden terms, so compatibility is symmetric. Inclusive numeric ranges cover latency fields. `MinCount`, `MaxCount` and `CountMultiple` bound valid match sizes while parties remain indivisible. Zero values preserve the previous exact room-size behavior. A mutex protects queue operations.

The existing `Rule` hook remains available for specialized scoring and placement. The default rule applies property compatibility and count bounds while retaining deterministic oldest-first rating windows.

## Alternatives considered

- Continue packing properties into `Mode` and `Region`. This avoids new fields, but makes unrelated compatibility dimensions opaque and one-sided.
- Replace `Rule` with a query string language. A typed query is easier to validate, deterministic to test and avoids parsing an executable mini-language in the server.

## Consequences

Queues can express platform, version, map and ping compatibility without custom grouping code, and low-population queues can start at a configured minimum. Existing callers remain source-compatible, though applications should treat property maps as immutable after enqueue because the queue copies them.
