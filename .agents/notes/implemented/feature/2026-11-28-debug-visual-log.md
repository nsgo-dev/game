# Agent Note: Gameplay visual log

Status: implemented

## Problem

The debug overlay only showed current-frame shapes and one-off system snapshots. History was lost when an AI or combat decision needed investigation later.

## Decision

`pkg/debug` provides a bounded fixed-step `VLog` for entity/category messages and shapes, plus JSON Lines export/import. A small `Category` interface lets systems provide snapshots without coupling the package to AI, stats or navigation implementations.

## Alternatives considered

Unbounded in-memory logging was rejected because mobile sessions need predictable memory. A renderer-owned binary capture was rejected because logs must be reviewable and portable as text.

## Consequences

Editor and runtime adapters can scrub or filter deterministic entries by entity and step. Release-build exclusion and built-in category adapters remain follow-up work.
