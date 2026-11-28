# Agent Note: YAML map-event pages

Status: implemented

## Problem

The flow package only accepted Go-built sequences, leaving map events, page conditions, triggers, and command vocabulary embedded in each game.

## Decision

`pkg/flow` defines a versioned `Event` document with ordered pages, self-switch conditions, four trigger kinds, and a closed command vocabulary. Strict YAML decoding rejects unknown fields and commands. `SelectPage` scans from the last page so the most specific matching page wins, matching RPG-style event precedence.

## Alternatives considered

Keeping map events in each template's world format would duplicate page selection and make editor and mod tooling incompatible. Treating commands as arbitrary maps would permit typos that fail only at runtime. Both were rejected.

## Consequences

Games and tools have a shared, validated event-file contract and deterministic page selection. The resumable interpreter, save snapshots, and RPG2D migration remain follow-up work.
