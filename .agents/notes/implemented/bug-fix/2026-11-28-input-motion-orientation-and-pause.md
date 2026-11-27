# Agent Note: MotionInput screen-orientation remapping and pause suspension

Status: implemented

## Problem

`input.MotionInput` (`pkg/input/motion.go`) previously passed NSGo's `core.MotionSample` vectors directly into tilt and gyroscope axes without rotating them out of NSGo's natural portrait device frame (`+X` right, `+Y` up, `+Z` out of screen). In landscape games, tilting like a steering wheel moved gravity along the device's `Y` axis instead of `X` (and flipped sign between landscape sides), which `Calibrate` could not fix because calibration only subtracts an offset. In addition, `WatchMotion` only stopped sensors in `core.LifecycleBackground`, leaving accelerometer and gyroscope providers streaming while a game was paused via `game.Loop.Paused`.

## Decision

`MotionInput` rotates both tilt (accelerometer/gravity) and gyroscope vectors from NSGo's natural device frame into screen axes before smoothing and calibration, and exposes `SetPaused(bool)` for `game.Loop.OnPause`:

- **`MotionOrientation` enum (`pkg/input/motion.go`)**:
  - `MotionOrientationPortrait` (zero value): `(+X, +Y, +Z)`, preserving backward compatibility for callers that omit `MotionOptions.Orientation`.
  - `MotionOrientationPortraitUpsideDown`: `(-X, -Y, +Z)`.
  - `MotionOrientationLandscapeLeft`: `(-Y, +X, +Z)` (90° counter-clockwise, device top on screen left, matching NSGo's `core.OrientationLandscapeLeft` on Android, HarmonyOS, and Web).
  - `MotionOrientationLandscapeRight`: `(+Y, -X, +Z)` (90° clockwise, device top on screen right).
  - `MotionOrientationAuto`: watches `cx.Env().Window()` via a scope `Effect` in `Start(cx)` (or accepts sizes via `SetWindowSize` when headless) and selects `MotionOrientationPortrait` when `Width <= Height` or `MotionOptions.LandscapeSide` (defaulting to `MotionOrientationLandscapeLeft`) when `Width > Height`.
- **Rotation before smoothing and calibration**: `OnMotion` applies `rotateMotionVector` before `ingestTilt` and `ingestGyro`, so `zeroTilt` captured by `Calibrate` is stored in screen axes and remains meaningful across orientation changes and pause/resume cycles. Whenever the resolved orientation changes (`SetOrientation`, `SetOptions`, or a window aspect flip under `MotionOrientationAuto`), smoothing history is invalidated (`hasTilt = false`, `hasGyro = false`, `gapSeq++`) so samples from the previous frame do not bleed across the transition.
- **Pause/resume lifecycle (`SetPaused` / `Paused`)**: `MotionInput` retains the scope from `Start(cx)` (guarded by a generation counter and `cx.Cleanup`). `SetPaused(true)` stops the active `core.SensorStream`, clears live and snapped readings via `Reset()` (preserving calibration and options), and ignores any `OnMotion` calls while paused. `SetPaused(false)` restarts `cx.WatchMotion` if the scope is still alive and `Stop()` was not called. `Start(cx)` called while already paused records the scope without opening a native stream until unpaused.

## Alternatives considered

- **Reuse `core.Orientation` directly as `MotionOptions.Orientation`**: `core.Orientation` is a bitmask of *allowed* interface orientations (`OrientationLandscape = OrientationLandscapeLeft | OrientationLandscapeRight`), has no `Auto` value, and uses zero for "system policy". A dedicated `MotionOrientation` enum makes every single-direction mode and `Auto` unambiguous while keeping zero as portrait.
- **Default zero value of `MotionOptions.Orientation` to `MotionOrientationAuto`**: would silently change axis mappings for existing callers and tests running in landscape desktop windows that construct `MotionOptions{}` expecting portrait/natural axes. Keeping portrait as the zero value preserves strict backward compatibility while games opt into `MotionOrientationAuto` or a fixed landscape orientation.
- **Only ignore samples in `OnMotion` during pause instead of stopping `core.SensorStream`**: would keep the underlying native accelerometer and gyroscope providers running and draining battery while on a pause menu. Stopping and recreating the `core.SensorStream` releases the native provider immediately and lets NSGo's lifecycle effect continue gating background resumes cleanly.

## Consequences

- **Benefits**: Landscape and upside-down games receive consistent screen-relative `tilt_x`/`tilt_y`/`gyro_x`/`gyro_y` axes regardless of device rotation; calibration works identically in every orientation and survives pause/resume; wiring `loop.OnPause = motion.SetPaused` releases native motion providers whenever gameplay pauses, matching `sound.Mixer.SetPaused`.
- **Costs and known limits**: Until NSGo exposes the platform's exact active interface orientation (distinguishing landscape-left from landscape-right), `MotionOrientationAuto` can only infer portrait vs. landscape from window aspect ratio and relies on `MotionOptions.LandscapeSide` (or `SetOrientation`) for the landscape side.
