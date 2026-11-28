# Agent Note: Photosensitivity flash limiter

Status: implemented

## Problem

Full-screen flash effects could retrigger without a shared budget, and the reduced-flashes preference was not available to post effects.

## Decision

`postfx.FlashLimiter` tracks a rolling one-second budget with a default maximum of three flashes. `FlashConfig` accepts a limiter and a separate `ReduceFlashes` callback. Reduced flashes use a low peak and minimum fade duration. Saturated-red colors are desaturated when a flash is created.

## Alternatives considered

Silently dropping all flashes would remove important gameplay feedback, so the limiter keeps the first flashes and drops only over-budget triggers. Reusing reduced-motion for photosensitivity would conflate distinct player needs, so the flash configuration exposes a separate callback.

## Consequences

Games must share a limiter between full-screen flashes to enforce a global budget. Existing flashes without the new options preserve their behavior. The limiter is deterministic when its clock is supplied internally by tests or callers.

