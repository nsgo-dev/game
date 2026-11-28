# Agent Note: Caption producer bridge

Status: implemented

## Problem

CaptionTrack had the rendering and timing policy but each sound or dialogue producer had to duplicate suppression, directional metadata and cleanup logic. That made it easy for repeated effects to spam captions or for a dialogue caption to survive after a cutscene ended.

## Decision

`a11y.CaptionBridge` keeps a deterministic clock, deduplicates sound labels within a caller-selected interval, forwards the listener pan to `ShowSFXAt`, and owns one active dialogue ID. `SyncDialogue` reuses an unchanged line, replaces a changed line, and dismisses it when inactive. The bridge accepts primitive values so it can be used from cutscene, VN and flow callbacks without import cycles.

## Alternatives considered

Adding direct dependencies from `a11y` to cutscene and VN would provide typed callbacks but introduce package coupling and cycles as those packages evolve. Requiring every caller to manage IDs and timers preserves flexibility but repeats the bug-prone state machine, so the bridge centralizes only that shared state.

## Consequences

Sound captions remain directionally aligned and repeated sounds respect `minInterval`. Dialogue cleanup is consistent across producers. Existing CaptionTrack callers continue to work, while producers opt into the bridge explicitly.

## Verification

Tests cover directional sound deduplication, interval expiry, dialogue reuse, replacement and clearing. The a11y package passes `go tool nsgo test` and `go tool nsgo check`.
