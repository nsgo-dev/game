# Agent Note: Game UI deviceClasses intersection and per-node-type field validation

Status: implemented

## Problem

Two silent-misbehavior defects existed in `pkg/gameui`'s `*.screen.yaml` loader and runtime builder (originally introduced in [Game UI *.screen.yaml format, runtime builder, checks, and reference tracking](../feature/2026-11-25-gameui-screen-files-yaml.md)):

1. **Disjoint nested `deviceClasses` showed on every device:** `intersectDeviceClasses` returned an empty `[]DeviceClass` when a child's `deviceClasses` did not overlap its parent's (for example a `tablet` label inside a `phone` `vbox`). Because `deviceClassMatches` treated `len(allowed) == 0` as "unrestricted", an empty intersection meaning "never" was interpreted as "always" in both `BuildScreen`/`SetDeviceClass` and `CheckScreen`, and `validateScreenNode` did not reject the combination.
2. **Ignored per-node-type fields passed strict YAML validation:** `ParseScreen` enforced `KnownFields(true)` across the union struct `ScreenNodeDoc`, so styling and behavior fields that `BuildScreen` never reads for a given node type—such as `padding` on `container`/`sprite_row`/`color_rect`/`touch_stick`/`touch_button`, `cornerRadius` on `sprite_row`/`touch_stick`/`touch_button`, `focusOrder` on non-focusable nodes, `sizeMode` outside `label`/`button`, or `sprite*` outside `sprite_row`—parsed and round-tripped with no error and no effect.

## Decision

1. **Explicit `deviceClassFilter` (`restricted bool`, `allowed []DeviceClass`):** Replace bare `[]DeviceClass` propagation in `pkg/gameui/screen_build.go`, `pkg/gameui/screen_check.go`, and `pkg/gameui/screen_format.go` with `deviceClassFilter`. An unrestricted node has `restricted == false` (matches every device class); a restricted node with `len(allowed) == 0` represents a disjoint intersection and `matches` always returns `false` (including for empty `DeviceClass` and every `SetDeviceClass` update), hiding the node and its entire subtree.
2. **Reject disjoint nested `deviceClasses` at validation time:** `validateScreenNode` threads the parent's `deviceClassFilter` down the tree and returns a `*loaderr.Error` at `deviceClasses` with a fix hint whenever `effClasses.isEmpty()`.
3. **Strict per-node-type field validation:** `validateScreenNode` validates every type-specific field against the exact set of node types that `BuildScreen` reads:
   - Common on all 10 node types: `id`, `type`, `anchor`, `offset`, `safeArea`, `width`, `height`, `deviceClasses`.
   - `background`: all node types except `image_progress` (fill color on boxes/containers/labels/buttons/color rects, icon fallback tint on `sprite_row`, ring base color on `touch_stick`).
   - `children`: `vbox`, `hbox`, `container`.
   - `padding`: `vbox`, `hbox`, `label`, `button`.
   - `spacing`: `vbox`, `hbox`, `sprite_row`.
   - `cornerRadius`: `vbox`, `hbox`, `container`, `label`, `button`, `color_rect`.
   - `sizeMode`: `label`, `button`.
   - `textKey`, `fontSize`: `label`, `button`, `touch_button`.
   - `textColor`: `label`, `button`, `touch_button`, `touch_stick` (thumbstick knob color).
   - `focusOrder`: `button`, `touch_button`.
   - `touchHitSize`: `button`, `touch_stick`, `touch_button`.
   - `sprite`, `spriteFrame`, `spriteCount`: `sprite_row`.
   - `under`, `progress`, `over`, `fill`, `startAngle`, `span`, `min`, `max`, `value`: `image_progress`.

## Alternatives considered

1. **Wire `padding` and `cornerRadius` into `touch_button` and `padding` into `container` instead of refusing them.**
   - *Strongest argument:* Authors who add `padding` to a `container` or `cornerRadius` to a `touch_button` would see visual effects instead of a load error.
   - *Why rejected:* `TouchButton` is explicitly a circular touch action button (`Arc` geometry with centered text), where `cornerRadius` and box `padding` contradict its circular shape; `ScreenCanvas` (`container`) positions children by anchor/offset/safeArea and supports `PassThrough` mode, where box padding conflicts with safe-area pass-through anchoring. Refusing fields a widget type does not support keeps the YAML contract honest and matches the widget APIs without muddying their layout semantics.
2. **Only reject disjoint `deviceClasses` in `validateScreenNode` without changing `intersectDeviceClasses` / `deviceClassMatches`.**
   - *Strongest argument:* Validated screens could never reach `BuildScreen` with a disjoint child, making the runtime flag redundant.
   - *Why rejected:* `CheckScreen` walks `ScreenDoc` without calling `ValidateScreen`, and future internal or editor callers manipulating `ScreenDoc` in memory could still pass an unvalidated tree into filtering logic. Using `deviceClassFilter` everywhere eliminates the empty-slice ambiguity structurally.

## Consequences

- **Benefits:** Disjoint nested `deviceClasses` are caught with file, line, path, and fix hint at parse/validate time and can never leak into visibility at runtime or during `CheckScreen`; every accepted field in a `*.screen.yaml` file has an observable effect in `BuildScreen`; `docs/en/gameui.md`, `docs/zh/gameui.md`, and `screen.v1.json` document the exact per-node-type field support.
- **Costs:** Any hand-authored `*.screen.yaml` file that previously included ignored fields (such as `focusOrder` on a `label` or `padding` on a `container`) or disjoint nested `deviceClasses` now fails `ParseScreen`/`ValidateScreen` until the dead field or contradictory restriction is removed.
