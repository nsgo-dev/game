# Agent Note: Mini-game leaderboard provider acceptance

Status: implemented

## Problem

Mini-game hosts expose friend leaderboard data only through user cloud storage and an isolated open data context. Treating simulated mobile environments as native Game Center or Play Games would show the wrong UI.

## Decision

The leaderboard catalog carries a `miniGame` key and sort order. `ProviderFor` chooses `ProviderMiniGame` only when the host advertises mini-program sign-in, otherwise simulated environments return `ProviderNone`. The service keeps the best local score, writes through `MiniGameHost`, and leaves friend rows in the host context while `Scores` remains unsupported.

## Alternatives considered

Reading friend rows into the main game would violate host open-data isolation. Reusing Game Center for simulated mobile builds would misrepresent availability.

## Consequences

Mini-game games can submit best scores and show host-owned friend rankings with correct platform detection. Native Game Center and Play Games behavior remains unchanged.

## Verification

Existing leaderboard tests cover provider selection, simulated safeguards, catalog key limits, best-score submission, Show/Hide calls and unsupported friend score queries. Targeted tests and `go tool nsgo check` pass.
