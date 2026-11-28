# Agent Note: Keep live inspection on the admin handler

Status: implemented

## Problem

Operators needed runtime and profiling evidence without exposing pprof or process details on the player-facing listener (#1699).

## Decision

`gameserver.Inspector` is an explicit HTTP handler with an authorization callback. It serves a point-in-time JSON runtime view and named pprof profiles only after authorization. Applications mount it only on their admin listener.

## Alternatives considered

- Register pprof globally on `http.DefaultServeMux`: rejected because it can accidentally expose profiling on a player-facing server.
- Make authorization a bearer-token policy inside the package: rejected because deployments use different admin identity systems.

## Consequences

The handler is safe to omit from public listeners and easy to compose with an authenticated admin mux. Runtime values are snapshots and do not hold server locks. Match and room drill-down views remain separate extension points for callers that own those objects.
