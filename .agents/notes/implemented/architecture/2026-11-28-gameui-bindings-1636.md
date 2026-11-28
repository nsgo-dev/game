# Agent Note: game UI binding registry

Status: implemented

## Problem

Screen layouts had no shared typed path registry for connecting labels, visibility and counts to game state.

## Decision

Add `gameui.Bindings` with typed String, Int and Bool getters, function sources and explicit missing/type errors. Values are read on demand by screen integrations.

## Consequences

Screen builders can validate path and type contracts without introducing per-frame polling in the registry. YAML bind parsing, core.Signal adapters and generated screen structs remain follow-up work dependent on #1345/#1344/#1346.

## Alternatives considered

An untyped map was rejected because misspelled paths and wrong value types would fail at runtime without a useful contract. Polling every source each frame was rejected because signal-backed values can be read on change.
