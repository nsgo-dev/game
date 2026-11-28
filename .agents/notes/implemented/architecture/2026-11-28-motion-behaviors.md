# Agent Note: Deterministic motion behavior helpers

Status: implemented

## Problem

Scene objects repeatedly reimplemented bullet motion, oscillation and rotation in game code. Those copies often depend on frame rate and cannot be previewed or tested as shared behavior rules.

## Decision

`pkg/motion` provides small stateful `Bullet`, `Sine` and `Rotate` helpers. Each `Step` consumes only an explicit fixed-step duration. Bullet motion supports speed, acceleration, gravity and optional angle tracking, Sine supports sine and triangle waves, and Rotate keeps degrees normalized.

## Alternatives considered

Adding behavior code directly to the scene builder would couple math to scene presentation and physics ownership. Keeping every behavior in templates would prevent other games and headless tests from sharing the deterministic rules.

## Consequences

Scene components can adopt the helpers without duplicating formulas, and code-driven objects can use the same math. Collision-aware bounce, orbit, bounds, fade and flash remain separate component layers.

## Verification

Tests compare one-second bullet simulation at two fixed rates, check sine and triangle golden values, and verify rotation wrapping. `go tool nsgo test` and `go tool nsgo check` pass for `pkg/motion`.
