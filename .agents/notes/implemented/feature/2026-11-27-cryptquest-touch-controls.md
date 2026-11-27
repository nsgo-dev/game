# Agent Note: CryptQuest view-space touch controls and phone HUD layout

Status: implemented

## Problem

CryptQuest previously placed six tiny virtual-canvas regions (18×18 D-pad buttons and 22×22 `E`/`F` pads in `320×200` game coordinates) inside its letterboxed viewport and drew them unconditionally via `drawTouchPads`. On phones the targets scaled below the 44pt minimum, sat inside the letterbox rather than the safe area, required two fingers for diagonals, and showed on desktop where duplicate `mouseRegion` bindings let stray mouse clicks move the hero. Its native HUD bar also used a single non-wrapping row with no safe-area insets, overflowing on narrow phone screens, and `nsgo.yaml` declared no build targets.

## Decision

- `examples/cryptquest/controls.touch.yaml` defines a canonical `TouchLayout` (reference `844×390`, `maxScale: 1.5`, `idleAfter: 4s`, `idleAlpha: 0.45`) with a bottom-left floating stick (`move`, `320×220` activation zone, `radius: 56`, `deadZone: 0.15`, `axisX: move_x`, `axisY: move_y`), a bottom-right `USE` button (`interact`, `96×96`), and a larger `ATTACK` button (`attack`, `128×128`). All three controls stay at least 44pt and non-overlapping down to 320pt portrait screens as well as landscape phones.
- Because `input.TouchControls`'s built-in `stick` kind sets `UpPositive: true`, `examples/cryptquest/game.yaml` declares `move_y` with `negative: down` and `positive: up`, and `Game.updatePlayer` negates `Actions.Axis("move_y")` to produce screen-down `VY` while preserving proportional speed capped at `playerSpeed` and unit-length facing. All `touchRegion` and `mouseRegion` entries are removed from `game.yaml`, along with `drawTouchPads`, `dpad*Rect`/`pad*Rect`, and `projectRegion`.
- `Game.Draw` renders `g.Touch` in view coordinates outside `g.Viewport.Draw` whenever `!g.Dialogue.Open`, passing `stageSafeArea()` so the stage respects horizontal/remaining window safe-area insets without double-counting the top HUD bar or bottom status bar. Opening dialogue resets held inputs, closing dialogue wakes the controls back to full opacity, and handled keyboard input in `HandleKey` notifies `g.Touch.Tracker().OnKey` so desktop keyboard use hides the overlay even when `stage` lacks focus.
- The native `hud-bar` and `status-bar` in `examples/cryptquest/main.go` enable `core.Wrap` and `PaddingEnv` safe-area edges (and `status-bar` hides the desktop keyboard hint on `core.IdiomPhone`), keeping the HUD clear of the notch and strictly above the game stage and touch controls on narrow phones.
- `examples/cryptquest/game.yaml` and `examples/cryptquest/nsgo.yaml` declare export presets and `build.targets` for `macos`, `windows`, `linux`, `android`, `web`, and `ios`.

## Alternatives considered

- **Keep `move_y` screen-down positive and build a custom non-`UpPositive` `VirtualStick` directly** — avoids negating `move_y` in `updatePlayer`. Rejected because it would bypass `TouchControls` and `controls.touch.yaml`, losing safe-area scaling, idle fade, mirroring, and player layout persistence.
- **Use an 8-way D-pad instead of a floating stick** — matches Cave Run's layout. Rejected because CryptQuest has continuous 360-degree top-down movement where a floating stick gives smooth single-thumb diagonals anywhere in the left zone.
- **Pass raw `env.SafeArea()` directly to `g.Touch.Resize`** — simpler than `stageSafeArea()`. Rejected because `hud-bar` and `status-bar` sit above and below `stage` and already absorb `SafeArea.Top` and `SafeArea.Bottom`; passing them again would shrink the stage's usable vertical area twice on portrait phones.

## Consequences

- **Benefits**: CryptQuest's touch controls are at least 44pt inside the safe area in both portrait and landscape phone orientations, support single-thumb diagonal movement and simultaneous stick-plus-attack multi-touch, stay hidden on desktop until touched without reacting to mouse clicks, hide cleanly during dialogue, and never overlap the wrapped native HUD bar.
- **Costs and known limits**: `move_y` is now up-positive in `game.yaml`, matching `TouchControls` and inverted gamepads; any future binding added to `move_y` should follow the same up-positive convention.
