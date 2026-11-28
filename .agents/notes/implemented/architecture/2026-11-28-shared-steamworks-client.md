# Agent Note: Shared Steamworks client ownership

Status: implemented

## Problem

Presence and Workshop each owned Steamworks initialization and shutdown, so closing one feature could terminate the library underneath another. Launch joins and callback pumping had no shared game-layer seam.

## Decision

`pkg/online/steamworks` exposes one `Client` around an injected platform `Driver`. `Open` initializes once, `Pump` delegates callback dispatch once per frame, and `Close` shuts down idempotently. Typed events are subscribed through the client, and a `+connect` launch argument is queued until a join listener subscribes. A missing driver returns `ErrUnavailable` for non-Steam targets and tests.

## Alternatives considered

Keeping independent initialization in presence and Workshop preserves their current constructors but cannot prevent shutdown races. Registering callbacks separately in each package also duplicates dispatch and makes overlay and join ordering inconsistent.

## Consequences

Native bindings can implement one driver and future integrations share its lifecycle and events. Existing presence and Workshop constructors still need an adapter migration in a follow-up slice, while the ownership contract is now testable without a Steam installation.

## Verification

Fake-driver tests cover one initialization, callback pumping, idempotent shutdown, queued `+connect` delivery and invalid join arguments. `go tool nsgo test` and `go tool nsgo check` pass for the package.
