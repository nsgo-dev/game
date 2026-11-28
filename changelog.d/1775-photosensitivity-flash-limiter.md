### Added

- `postfx.FlashLimiter` is a rolling one-second budget of full-screen flashes that `postfx.FlashConfig.Limiter` shares between flashes, and `postfx.FlashConfig.ReduceFlashes` turns a flash into a low-contrast tint, peaking at most at `postfx.ReducedFlashPeak` and fading over at least `postfx.ReducedFlashFade`, while it reports true: pass `a11y.Assist.ReduceFlashes`, the player's Reduce Flashes setting (#1775).

### Changed

- A `postfx.Flash` runs at most `postfx.MaxFlashesPerSecond` (3) times in any one second, `Trigger` returning false past that, and `postfx.NewFlash` desaturates a saturated-red flash color to the gray of its relative luminance (#1775).
