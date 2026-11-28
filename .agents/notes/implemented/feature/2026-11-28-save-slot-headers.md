# Agent Note: Save slot headers

Status: implemented

## Problem

Slot lists required loading full save values to show chapter or location metadata, which made load screens slow and encouraged drifting hand-written indexes.

## Decision

The save envelope accepts an optional JSON `header`. `SaveWithHeader` writes it atomically with data, and `Headers` reads only the envelope header for each slot. Legacy envelopes remain valid and are omitted from header results until rewritten.

## Alternatives considered

Keeping a second index slot was rejected because it can disagree with data after interrupted writes. Parsing every full data value was rejected because large worlds make listing visibly expensive.

## Consequences

Games can store small deterministic slot summaries without changing existing Load behavior. Background-safe async writes and thumbnails remain follow-up work.
