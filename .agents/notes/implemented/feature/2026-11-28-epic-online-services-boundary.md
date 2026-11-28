# Agent Note: Epic Online Services boundary

Status: implemented

## Problem

Games need one asynchronous contract for Epic login, achievements, leaderboards and presence, but EOS is a platform-specific C SDK that is unavailable on most test and non-desktop targets.

## Decision

`pkg/online/eos` exposes a small `Driver` interface and an engine-goroutine `Platform`. Native adapters own shared-library loading and callback translation behind the interface. `Platform` validates product identifiers, posts all callbacks to its core scope, pumps `Driver.Tick`, and returns `ErrUnavailable` when no driver is available. Tests inject a deterministic fake.

## Alternatives considered

Binding the EOS C SDK directly in the shared package was rejected because it would make pure-Go tests and unsupported targets depend on native headers and shared libraries. Reusing `gameservices` was rejected because EOS has exchange-code login and richer leaderboard and presence operations that the generic service cannot represent.

## Consequences

Games can integrate EOS without changing gameplay code when a native adapter is added, and CI can cover login and reporting with a fake. The current package does not ship the platform-specific purego bindings or catalog schema changes, which remain dependent on the multi-store catalog work in #1764.
