# Agent Note: Photosensitivity flash limiter

Status: implemented

## Problem

`postfx.Flash.Trigger` (`pkg/postfx/flash.go`) jumps the whole frame toward a color and fades it, and refused only under reduced motion. A game that flashed on every hit of a fast weapon strobed the whole screen as often as it was hit, a saturated-red flash was treated like any other color, and the player's Reduce Flashes setting (`a11y.Assist.ReduceFlashes`) reached a flash only if the game folded it into `FlashConfig.ReduceMotion`, which turns the flash off rather than softening it. More than three flashes in a second, or a saturated-red flash, fail WCAG 2.3.1 and XAG 118 (#1775).

## Decision

- **Rate limit, on by default**: `postfx.FlashLimiter` counts the flashes it allows in a rolling one-second window and refuses one that would make more than `MaxPerSecond` in any closed second. `MaxPerSecond` of 0 or less, or above `MaxFlashesPerSecond` (3), means 3: the limit may be lowered, never raised past the guideline. `NewFlash` gives a flash whose `FlashConfig.Limiter` is nil a limiter of its own, so every flash is limited without configuration; a game shares one limiter between the full-screen flashes of a screen to budget them together. A refused `Trigger` returns false and leaves a running flash alone, like the other refusals.
- **The scope's clock**: `Trigger` passes `cx.Now()` to `FlashLimiter.Allow(now)`. That is the system clock in a running game and the manual clock in `nsgotest`, so `Advance` moves the window and the limiter's tests are deterministic. `Allow` takes the time instead of reading a clock of its own so that a caller with its own clock (a replay) can drive it, and it holds a mutex because the limiter is meant to be shared.
- **Saturated red**: `NewFlash` replaces a `Color` whose red is 80% or more of red, green and blue together (XAG 118's red flash test) with the gray of the same relative luminance, linearizing sRGB, weighting by Rec. 709 and encoding back, and keeps its alpha. Pure red becomes a mid gray, not the dark gray a weighted sum of the encoded components gives.
- **Reduce Flashes is separate from reduced motion**: `FlashConfig.ReduceFlashes`, asked untracked at each `Trigger`, caps the peak at `ReducedFlashPeak` (0.2) and stretches the fade to at least `ReducedFlashFade` (100 ms), so the feedback stays as a soft tint. Reduced motion still stops the flash entirely. A nil `ReduceFlashes` reduces nothing, unlike a nil `ReduceMotion`, which falls back to the platform's setting, because no platform has a reduced flashing setting to fall back to.

## Not covered here

The issue also asks for `ReduceFlashes` on particles and sprite flashes, lowering a peak while the previous flash still fades, and a `gametest` analyzer of recorded frames with a `game check flashes` command. Those remain open on #1775; the accessibility guide still lists them as engine gaps.

## Alternatives considered

- **A limiter only when one is configured**, leaving `FlashConfig.Limiter` nil unlimited. Its strongest argument is that no existing `Trigger` changes behavior. Rejected because the limit protects players from a game that never thought about it, which is exactly the game that would not configure one; a game that needs more than three flashes a second from one `Flash` is the case the guideline forbids.
- **One limiter per scope, found through the scope**, so every flash under a screen shares a budget without the game passing one. It matches the issue's wording, but `core.Scope` has no place to hang per-scope values from this package without a context lookup at each `NewFlash`, and which flashes belong to "one screen" is the game's call. An explicit shared `FlashLimiter` states it.
- **Reading `time.Now` in the limiter.** Simpler to call, but the window then ignores `nsgotest`'s manual clock, so a test that advances a second still sees the flashes as recent, and replays could not drive it.
- **Folding Reduce Flashes into reduced motion**, as the accessibility guide used to recommend. Rejected because the needs differ (photosensitive versus vestibular) and turning flashes off removes gameplay feedback a soft tint keeps.

## Consequences

- Benefit: every `postfx.Flash` stays within three flashes a second and never flashes saturated red, without the game doing anything; Reduce Flashes now has an engine meaning for full-screen flashes.
- Cost: a game that triggered one flash more than three times a second now sees `Trigger` return false for the extra ones, and a red flash color now draws gray; both are changes users can see and are in the changelog. Several flashes still need a shared limiter to be budgeted together.
- Tests in `pkg/postfx/flash_limiter_test.go` cover the rolling window (eight hits a second allow three), the cap on `MaxPerSecond`, a shared limiter, the Reduce Flashes tint, and the red desaturation.
