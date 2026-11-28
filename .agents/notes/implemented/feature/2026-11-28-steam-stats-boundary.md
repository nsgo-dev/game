# Agent Note: Steamworks stats boundary

Status: implemented

## Problem

Steam achievements and leaderboards need a shared asynchronous contract, while the Steamworks SDK is native and unavailable in CI and unsupported builds.

## Decision

`pkg/online/steam` exposes a narrow `Driver` for user stats, achievements, leaderboard scores and rich presence. `Platform` owns the driver, posts callback results to its core scope, pumps callbacks through `Tick`, and reports `ErrUnavailable` when no native driver is injected. A fake driver verifies login and achievement flow without native libraries.

## Alternatives considered

Direct C bindings in the shared package were rejected because they break pure-Go and unsupported targets. Reusing the generic game-services API was rejected because Steam requires stat storage, keep-best score uploads and friends/global query ranges.

## Consequences

Games can share one Steam-facing API and defer platform loading to a native adapter. YAML catalog wiring and the adapter itself remain separate work that depends on the shared Steam client issue #1763.
