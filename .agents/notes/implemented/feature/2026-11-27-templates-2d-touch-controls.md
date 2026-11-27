# Agent Note: Move 2D templates onto TouchControls

Status: implemented

## Problem

The three 2D project templates (`platformer2d`, `roguelike2d`, and `rpg2d`) took touch input through fixed `touchRegion` and `mouseRegion` rectangles inside their virtual canvases (`320x180` with `integerScale` for `platformer2d` and `roguelike2d`, `480x270` for `rpg2d`), drew controls by hand inside the letterboxed canvas, and managed visibility with ad-hoc `pointerSeen`/`showTouchControls`/`touch` flags. On a phone, controls ignored safe area insets, stayed trapped inside the letterbox, and could shrink below 44pt (such as 22pt d-pad cells on portrait phones in `roguelike2d`). Every project scaffolded from these templates inherited this pattern.

## Decision

- Replace `touchRegion` and `mouseRegion` bindings, hand-drawn canvas controls (`drawTouchControls` and `drawPads`), and manual visibility flags in all three templates with `input.TouchControls` driven by an embedded, canonical `controls.touch.yaml` layout in view coordinates:
  - `platformer2d`: an 8-way `VirtualDPad` bound to `left`/`right` at bottom-left (so diagonal thumbs keep running left or right and one finger slides between them) and a `JUMP` button at bottom-right, matching Cave Run's layout.
  - `roguelike2d`: a 4-way `VirtualDPad` bound to `up`/`down`/`left`/`right` at bottom-left, and `WAIT`, `QUAFF`, and `CLEAVE` buttons at bottom-right with enough offset spacing that none overlap even on narrow portrait phones when clamped to `MinTouchTarget` (44pt).
  - `rpg2d`: a floating `VirtualStick` on `moveX`/`moveY` at bottom-left and `A` (`confirm`) / `B` (`cancel`) buttons at bottom-right. Because `TouchKindStick` configures `VirtualStick` with `UpPositive: true`, `rpg2d`'s `moveY` axis is aligned to up-positive across all devices (`axes` flips to `negative: down, positive: up`, `gamepadAxes` adds `invert: true` on `leftY`, and `readIntent` maps `y > 0` to `dirUp`). Dialogue tap-to-advance/choice picking in `OnPressDown` stays active only in `modeTalk` so desktop mouse clicks on the field do not drive movement or reveal touch controls.
- Attach `touch.Attach` after `actions.Attach`, call `touch.Update(dt)` after `actions.Step()`, call `touch.Wake()` when starting/restarting or returning to `modeField`, resize `touch` on layout and draw with `env.SafeArea()`, and draw `touch.Draw(d)` outside `viewport.Draw` only during active play.
- Add `TestTouchLayoutIsCanonical` to each template's `project_test.go`, update touch playthrough tests to drive `TouchControls`, add tests verifying desktop auto-hide, mouse clicks no longer moving or showing controls, 44pt minimum size, safe area bounds, zero overlap, and placement outside the letterboxed playfield on portrait phones.
- Copy each template over `internal/gamecli/_templates/projects/<name>` so `TestTemplatesMatchTemplateModules` passes, and update `README.md`, `docs/en/template-*.md`, and `docs/zh/template-*.md`.

## Alternatives considered

- **Keep `mouseRegion` bindings alongside `TouchControls`** — would let mouse clicks move the player on desktop. Rejected: `TouchControls` stays hidden on desktop until touched and lives in view coordinates rather than virtual canvas coordinates, so invisible mouse regions would disagree with the drawn controls and violate the requirement that mouse clicks no longer drive movement.
- **Keep `rpg2d`'s `moveY` screen-down positive and negate the stick axis in `input.go`** — would avoid touching `game.yaml`'s `axes` and `gamepadAxes`. Rejected: `TouchControls` feeds the stick directly into `Actions.Axis("moveY")` alongside `axes` and `gamepadAxes`, so all three sources must share the same sign (`UpPositive`) on `moveY`.

## Consequences

- **Benefits**: new projects created from `platformer2d`, `roguelike2d`, and `rpg2d` immediately get safe-area-aware, >= 44pt, screen-scaled touch controls outside the letterbox, automatic desktop hiding on keyboard/gamepad use, haptic ticks, accessibility canvas nodes, and canonical `controls.touch.yaml` files.
- **Costs and known limits**: desktop players use keyboard or gamepad rather than clicking virtual buttons with a mouse (menus and RPG dialogue choices remain clickable/tappable).
