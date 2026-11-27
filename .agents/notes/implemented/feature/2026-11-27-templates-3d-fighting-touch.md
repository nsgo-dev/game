# Agent Note: Migrate action3d, fps3d, racing3d and fighting2d templates onto TouchControls

Status: implemented

## Problem

The four showcase touch templates (`action3d`, `fps3d`, `racing3d`, and `fighting2d`) each wired `input.VirtualPad` by hand with inconsistent visibility, fixed pixel offsets that collided with notches and home indicators, missing touch controls (`weapon1`/`weapon2` in `fps3d`, P2 controls for local versus on one phone in `fighting2d`), split vertical stick axes in `fps3d` (`touchMoveY`/`touchLookY`), and no landscape orientation request even though all four are landscape games.

## Decision

- Every template declares its layout in a canonical `controls.touch.yaml` (`version: 1`, `844x390` reference, `maxScale: 1.5`, `idleAfter: 4s`, `idleAlpha: 0.45`) loaded via `input.ParseTouchLayout` and checked by `TestTouchLayoutIsCanonical`, built into `input.TouchControls` bound to `input.Actions` with `input.NewHaptics(cx)`, attached after `Actions.Attach`, resized with `Env.SafeArea()`, stepped in the fixed-step loop via `TouchControls.Update(dt)`, and woken via `TouchControls.Wake()` on restart or resume.
- Every template calls `cx.RequestOrientation(core.OrientationLandscape)` in its root scope so phones lock to landscape.
- Because `TouchControls` builds sticks with `UpPositive: true`, all three templates with vertical sticks (`action3d`, `fps3d`, and `fighting2d`) align on up-positive vertical axes across keyboard, gamepad (`invert: true`), and touch:
  - `fps3d` deletes `touchMoveY` and `touchLookY`, binds both touch sticks directly to `moveY` and `lookY`, and adds `W1` (`weapon1`) and `W2` (`weapon2`) rectangular buttons above `SWAP` and `RELOAD`.
  - `action3d` aligns `moveY` (`positive: forward`, `negative: back`) and `stickY` (`invert: true`) so keyboard, gamepad, and touch share one sign convention.
  - `fighting2d` sets `yUp: true` in `basalt.moves.yaml` and `kestrel.moves.yaml` and `invert: true` on `p1_move_y` and `p2_move_y` in `game.yaml`, removes the old `touchRegion`/`mouseRegion` bindings from `game.yaml`, and draws `TouchControls` outside `viewport.Draw` in view points.
- For two-player local versus on one phone in `fighting2d`, `versus.touch.yaml` compacts player 1's floating stick and `L`/`H` buttons onto the left half of the safe area, and `mirrorVersusLayout` builds player 2's `TouchControls` with `Mirrored: true`, `p2_` bindings, and `p2-` control IDs on the right half, bound to `actions[1]` only during versus matches.
- In `action3d` and `racing3d`, the loop's `Draw` callback bumps `padTick` only while the touch controls are visible or when their visibility or alpha changes, avoiding redundant empty-canvas redraws during desktop keyboard/gamepad play while smoothly animating stick drags, button presses, idle fade, and device switches.
- All four template directories are copied byte-for-byte into `internal/gamecli/_templates/projects/<name>` so `game new` matches `nsgo init -template`.

## Alternatives considered

- **Keep down-positive `moveY` in `action3d` and `fighting2d` with separate touch axes** — smaller diff to `game.yaml` and `moves.yaml`. Rejected because `TouchControls` sticks are unconditionally up-positive, and separate touch axes reintroduce the exact duplication #1296 deletes from `fps3d`.
- **Two separate versus YAML files (`versus-p1.touch.yaml` and `versus-p2.touch.yaml`) in `fighting2d`** — avoids runtime string replacement. Rejected because `TouchControls` already supports `Mirrored: true` specifically to mirror a layout symmetrically across left and right, so deriving P2 from `versus.touch.yaml` guarantees symmetry and avoids drift.

## Consequences

- **Benefits**: consistent safe-area layout (>= 44pt targets) and first-frame visibility on native phones and phone browsers across all four templates; shared `moveY`/`lookY` axes in `fps3d` plus direct weapon-slot buttons; playable two-player touch versus on a single phone in `fighting2d`; landscape orientation requested across all four templates.
- **Costs**: `action3d`'s internal `controls.moveY` and `fighting2d`'s `move_y` are now up-positive; tests and data files in those templates reflect the unified sign.
