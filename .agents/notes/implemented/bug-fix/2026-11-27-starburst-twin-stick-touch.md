# Agent Note: Starburst twin-stick touch controls and no stale touch aim

Status: implemented

## Problem

Starburst is a twin-stick arena shooter, but on touch devices it lacked both sticks and aimed in the wrong direction. Movement used four separate 44×42 `touchRegion` rectangles in `game.yaml`, so diagonals required two fingers, and fire was an undrawn rectangle. `arenaView.OnPressDown` also recorded `pointerView`/`hasPointer` for every non-keyboard press—including touches—while only `OnMouseLeave` cleared it, which a finger never fires. As a result, `HasWorldAim` stayed stuck on the last touched point (usually the movement pad) and overrode nearest-enemy auto-aim for the rest of the run. Finally, `examples/starburst/nsgo.yaml` declared no `build.targets`.

## Decision

- Add `examples/starburst/controls.touch.yaml` (canonical `input.TouchLayout`, verified by `TestTouchLayoutIsCanonical`) with three controls anchored inside the safe area for an 844×390 landscape phone reference: a floating bottom-left `move` stick (`move_x` / `move_y`), a floating bottom-right `aim` stick (`aim_x` / `aim_y`), and a round `NOVA` button anchored above the aim stick so its frame does not overlap the aim zone or the ship at spawn.
- Bind `TouchControls` into `GameController.Actions` with haptics enabled, call `Touch.Update(dt)` in `stepInput`, resize and draw the overlay over `Arena.Draw` in view points while playing (hidden when paused or game over), and wake it on pause/resume/restart. Feed keyboard presses from `bindKeys` into `Touch.Tracker()` so keyboard play on desktop hides the touch overlay, and hide the bottom keyboard/mouse legend while touch controls are visible.
- Because `TouchControls`'s `stick` kind sets `VirtualStick.UpPositive = true` (`+1` up), align `game.yaml`'s `move_y` axis (`negative: move_down`, `positive: move_up`) and negate `move_y` and `aim_y` in `stepInput` when converting into `Arena.Control`'s screen-down coordinates. Hold `ControlInput.Fire` whenever the aim stick is pushed past its dead zone (`aimX != 0 || aimY != 0`), and lower `Arena.stepPlayer`'s stick aim threshold from `0.04` to `1e-4` so every deflection past `VirtualStick.DeadZone` both aims and fires.
- Remove the `touchRegion` bindings from `game.yaml` (keeping left/right `mouseButton` bindings), remove `projectRegion`, `ShowTouchHints`, and `drawTouchHints`, and declare `macos`, `windows`, `linux`, `android`, `web`, and `ios` in `game.yaml`'s `export` presets and `nsgo.yaml`'s `build.targets`.
- Set `pointerView` and `hasPointer` exclusively from `OnMouseMove` and `OnMouseDown`, and clear `hasPointer` on every `OnTouch`, so touch never sets `HasWorldAim` and releasing the aim stick falls back cleanly to nearest-enemy auto-aim while desktop mouse aim stays unchanged.

## Alternatives considered

- **Flip `UpPositive` to `false` on `Touch.Stick("move")` and `Touch.Stick("aim")` at startup** — would avoid negating Y in `stepInput`. Rejected: `TouchControls.SetLayout` rebuilds sticks with `UpPositive: true`, so loading a player-edited layout would silently invert vertical movement and aim.
- **Keep `Arena.stepPlayer`'s `stickSq > 0.04` threshold** — avoids changing `arena.go`. Rejected: `VirtualStick` already applies a 0.15 radial dead zone and rescales linearly from 0, so pushes between 15% and ~32% of radius would set `ControlInput.Fire = true` while failing `stickSq > 0.04` and firing in the auto-aim direction instead of where the stick points.
- **Anchor `NOVA` to `topRight`** — puts it in the upper-right corner. Rejected: on taller displays (such as tablets or 16:9 windows) `topRight` detaches `NOVA` from the aim stick and crowds the top HUD bar; anchoring `NOVA` to `bottomRight` with a vertical offset keeps it consistently right above the aim stick across aspect ratios.

## Consequences

- **Benefits**: Touch players get full 360-degree single-thumb movement, simultaneous aim-and-fire on the right thumb, a labeled `NOVA` button above the aim stick, and automatic nearest-enemy auto-aim whenever the aim stick is released. Mouse aim and desktop keyboard play remain unchanged, and touch controls stay hidden on desktop until touched.
- **Costs and known limits**: `examples/starburst` now depends on `controls.touch.yaml` staying canonical with `input.EncodeTouchLayout` and on `game.yaml` and `nsgo.yaml` keeping their export targets in sync; both invariants are enforced in `project_test.go`.
