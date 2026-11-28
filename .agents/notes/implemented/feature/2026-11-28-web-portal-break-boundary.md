# Agent Note: Web portal break boundary

Status: implemented

## Problem

Web portal SDKs own ad timing, but games still need to pause simulation, mute audio and clear held input around SDK callbacks. Rewarded ads must never grant a reward when the SDK fails or times out.

## Decision

`liveops.Portal` exposes a small `PortalSDK` seam for page adapters and coordinates loading and gameplay lifecycle calls. `Break` and `Rewarded` run the game hooks in a fixed order, invoke the SDK under a timeout, and restore audio and simulation with deferred cleanup. Rewarded results are returned only when the provider explicitly returns true. `PortalNone` is a no-op for non-web targets and tests.

## Alternatives considered

Calling `syscall/js` directly from each game would duplicate ordering and timeout handling. Embedding provider SDK calls in `sound` or `game.Loop` would couple unrelated packages and make native builds carry web concerns.

## Consequences

Poki and CrazyGames adapters can share the pause, mute and input contract while provider-specific JavaScript remains outside the core package. Newgrounds achievements and leaderboard mapping remain a later slice, and actual web script loading is left to page adapters.

## Verification

Fake-SDK tests cover break ordering, rewarded success and failure, no-op behavior, and restoration after timeout. `go tool nsgo test` and `go tool nsgo check` pass for `pkg/online/liveops`.
