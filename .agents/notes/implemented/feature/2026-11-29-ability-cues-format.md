# Agent Note: Ability cue format

Status: implemented

## Problem

Ability and effect events had no shared data contract for mapping gameplay tags to client-only cosmetic handlers.

## Decision

`CuesDoc` defines strict version-1 YAML mapping cue tags to particle, sound, shake, and once/while-active policy fields. Validation rejects unknown YAML fields, empty handlers, negative shake amplitudes, and trailing documents.

## Alternatives considered

Hard-coding assets in ability callbacks couples simulation to presentation and breaks headless servers. Untyped maps allow misspelled handlers to silently do nothing.

## Consequences

Clients can load reviewable cue mappings while servers remain asset-free. Event subscription, rollback reconciliation, built-in cue player, and asset existence checks remain follow-up work.
