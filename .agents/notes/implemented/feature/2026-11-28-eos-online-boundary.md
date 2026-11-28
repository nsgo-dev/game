# Agent Note: EOS online boundary

Status: implemented

## Problem

Epic Online Services needs a native SDK boundary with asynchronous callbacks, launcher exchange-code login, and a deterministic way to exercise online features without loading a platform library in tests.

## Decision

`pkg/online/eos` defines a small `Driver` interface for login, achievement progress, score submission and ranges, presence, ticking, and shutdown. `Service` owns the driver, reads `-AUTH_PASSWORD` exchange codes, and posts every callback to the engine scope. A caller-provided fake driver is required by `New`, while targets without a binding return `ErrUnavailable`.

## Alternatives considered

Calling EOS SDK functions directly from game code would expose callback lifetime and thread rules. A synchronous service would violate EOS's asynchronous API model and make callbacks race UI state. Both were rejected in favor of the driver boundary.

## Consequences

Games can integrate a purego/native driver later without changing game-facing calls, and tests can verify login, progress, scores, presence, and callback scheduling with no EOS installation. Catalog `epic` keys and a production SDK binding remain follow-up work.
