# Agent Note: Native HUD over a 3D view

Status: implemented

## Problem

Games rendering a 3D scene in `gpu.View` need native NSGo `ui` controls (`ui.Button`, `ui.Card`, `ui.Badge`, `ui.Progress`, `ui.Switch`, reactive `core.Text`) floating over the 3D surface that work with both mouse and touch input. Wiring this manually for every 3D scene is error-prone:

- Plain full-screen overlay `core.View` containers default to `core.PointerEventsAuto`, swallowing mouse clicks, mouse drags, and touch gestures meant for the 3D viewport underneath.
- `core.View` padding does not inset direct `Position: core.Absolute` children (CSS absolute positioning uses the parent's padding box), so corner-anchored HUD controls placed directly in a padded overlay ignore safe-area insets unless an intermediate relative stage is inserted.
- `ui.Button` and `base.Button` register `OnPress` rather than `OnMouseDown`; when an underlying 3D viewport container listens to `OnMouseDown` (for left/right-button camera orbit), clicking a native HUD button can leak `MouseDown` through to the 3D viewport unless the button's slot bounds absorb mouse button events.
- `game/scene` is a Stable package (enforced by `TestStabilityTable`) and must not import experimental 3D packages (`game/math3d`, `game/scene3d`, `game/render3d`) or bloat its binary footprint by pulling in `nsgo/ui` directly.

## Decision

Add `HUD3D`, `HUDSlot`, and `HUDAnchor` in `packages/game/scene/hud3d.go` using only stable `nsgo/core` and `nsgo/gpu` dependencies:

1. **Two-layer safe-area overlay (`HUD3D.Layer`)**:
   - The outer overlay `core.View` covers its parent (`Position: core.Absolute`, inset `0`), sets `PointerEvents: core.PointerEventsBoxNone`, and applies `Padding` plus `PaddingEnv: core.EnvAll(core.EnvSafeArea)` (enabled by default; toggleable via `IgnoreSafeArea` / `SetSafeArea`).
   - An inner flex-growing relative stage (`Position: core.Relative`, `FlexGrow: 1`, `FlexShrink: 1`, `PointerEvents: core.PointerEventsBoxNone`) hosts `Children` and anchored `Slots`, ensuring both normal-flow children and absolutely positioned corner/edge/center slots (`HUDTopLeft` through `HUDBottomRight`) stay strictly inside the safe area and react dynamically when `core.Env.SafeArea()` changes.
2. **Mouse and touch pass-through contract**:
   - Open HUD regions and anchor alignment wrappers set `core.PointerEventsBoxNone` so mouse presses, drags, hovers, and touches fall through unobstructed to the 3D view underneath.
   - Decorative slots marked `PassThrough: true` (`AddPassThrough`) set `core.PointerEventsNone` so clicks and touches pass straight through crosshairs and status readouts.
   - Interactive anchored slots (`Anchor != HUDFull && !PassThrough`) wrap their child in a shrink-wrapped `core.View` with no-op `OnMouseDown`/`OnMouseUp` handlers (which do not add keyboard Tab stops and run only if the child does not register its own mouse button handler), preventing clicks on `ui.Button` controls from leaking into an underlying 3D viewport's `OnMouseDown` camera orbit handler.
3. **Flexible 3D composition (`View`, `Wrap`, `Layer`)**:
   - `HUD3D.View(cx, props)` appends `HUD3D.Layer(cx)` to `gpu.ViewProps.Children`, preserving `gpu.View` as a single node (including `NoGPU` fallback) and placing native HUD controls above any preceding children (such as a 2D overlay canvas).
   - `HUD3D.Wrap(cx, view3D)` Z-stacks `view3D` and `HUD3D.Layer(cx)` as siblings inside a relative `core.View` so games wrapping `gpu.View` in an input-handling `core.View` (`OnMouseDown`, `OnMouseMove`, `OnTouch`) never receive bubbled touches or hovers from HUD controls.
4. **Reactive visibility without remounting**:
   - `SetHidden`, `SetVisible`, `Visible`, and `Toggle` update a scope-bound `core.Signal[bool]` read in the outer layer's `StyleFunc`. Hidden HUDs switch to `core.DisplayNone` + `core.PointerEventsNone`, removing them from hit-testing, layout, and accessibility while keeping child signals and control state intact.

## Pointer-routing invariants

| Region / State | Effective `PointerEvents` / `Display` | Mouse (`Press`, `MouseDown`, `Hover`) | Touch (`Touch`, `Move`, `Lift`) |
| --- | --- | --- | --- |
| Open HUD area (visible) | `PointerEventsBoxNone` | Passes through to 3D view | Passes through to 3D view |
| `AddAt` interactive slot (visible) | Anchor `BoxNone` + tight mouse-button absorber | Handled by HUD control; does not leak to 3D view | Handled by HUD control; does not bubble to sibling 3D view |
| `AddPassThrough` slot (visible) | `PointerEventsNone` | Passes through to 3D view | Passes through to 3D view |
| Entire HUD (`Hidden == true`) | `DisplayNone` + `PointerEventsNone` | Passes through to 3D view everywhere | Passes through to 3D view everywhere |

## Alternatives considered

### Unmounting the HUD subtree via `core.Show` when `Hidden` is true

Strongest argument: `core.Show` completely disposes child scopes and releases native HUD views while the HUD is hidden.

Rejected because games frequently toggle HUD visibility (photo mode, cutscenes, quick HUD hide/show) while keeping stateful HUD controls, refs, and reactive bindings mounted. `core.DisplayNone` + `core.PointerEventsNone` already removes the hidden subtree from layout, hit-testing, focus traversal, key bindings, and accessibility in NSGo without tearing down and recreating views on every toggle.

### Importing `nsgo/ui` directly in `packages/game/scene` to provide styled HUD widgets

Strongest argument: bundling pre-styled HUD widgets (health bar, pause button, minimap frame) in `game/scene` would reduce boilerplate for simple demos.

Rejected because `game/scene` is a core stable package used by 2D and 3D games alike, and importing `nsgo/ui` would pull NSGo's entire styled component library and icon set into every game that only uses `game/scene`. Accepting `core.Node` lets callers compose any `nsgo/ui`, `nsgo/base`, or `core` views freely over 3D scenes with zero extra dependencies in `game/scene`.

### Mounting `HUD3D` only via `gpu.ViewProps.Children` without `Wrap`

Strongest argument: a single `View(cx, props)` entry point keeps the API smaller and matches `gpu.ViewProps`'s native children model.

Rejected because `gpu.ViewProps` exposes `OnPress` and `OnLayout` but not `OnMouseDown`, `OnMouseUp`, `OnMouseMove`, or `OnTouch`. Games implementing 3D camera orbit or touch gestures wrap `gpu.View` in an input-handling `core.View`; if the HUD sat inside that wrapper, touches on HUD buttons would bubble to the wrapper's `OnTouch`. Providing `Wrap(cx, view3D)` stacks the HUD as a sibling above the 3D input wrapper so HUD touches never bubble into 3D gesture handlers while open regions still pass through cleanly.

## Consequences

- **Benefit**: Any native NSGo `ui` view works out of the box over a 3D `gpu.View` with both mouse and touch, automatic safe-area insets, reactive visibility toggling, and `NoGPU` fallback support.
- **Benefit**: `game/scene` stays a clean Stable package importing only `nsgo/core` and `nsgo/gpu` in `hud3d.go` and does not touch `overlay3d.go` or other `scene` files.
- **Cost**: `HUD3D.Add` / `AddAt` / `AddSlot` configure the mounted HUD layout before `Layer`/`View`/`Wrap` runs; dynamic list changes inside a HUD should use NSGo reactive regions (`core.Show`, `core.Each`, or reactive `TextFunc`/`StyleFunc`) inside a slot or child.
- **Cost**: Anchored interactive slots (`Anchor != HUDFull && !PassThrough`) absorb `MouseDown`/`MouseUp` within the slot's tight bounding box; callers building a wide full-width container with transparent interactive gaps should place separate anchored slots or use `HUDFull` / `Children` with `core.PointerEventsBoxNone`.

## Verification

- `packages/game/scene/hud3d_test.go`:
  - `TestHUD3DMouseAndTouchOverGPUView` verifies mouse and touch clicks on `ui.Button` controls, pass-through on open 3D regions, and `PassThrough` decorative slots over a live `gpu.View`.
  - `TestHUD3DWrapMouseDownMoveAndTouchPassThrough` verifies `HUD3D.Wrap` with an interactive 3D viewport (`OnPress`, `OnMouseDown`, `OnMouseUp`, `OnMouseMove`, `OnTouch`), confirming HUD `ui.Button` and `ui.Switch` interactions do not leak into 3D handlers while open-region mouse drags, hovers, and touch drags reach the 3D viewport.
  - `TestHUD3DReactiveUpdates` verifies reactive `core.Signal` updates to HUD text, `ui.Progress`, and `ui.Button` labels without remounting `gpu.View`.
  - `TestHUD3DVisibilityToggle` verifies `SetHidden`, `SetVisible`, `Visible`, and `Toggle` hide HUD controls and pass both mouse and touch input at former button positions straight through to `gpu.View`.
  - `TestHUD3DSafeAreaInsetsAndAnchors` verifies all 9 anchors under initial safe-area insets, dynamic `SetEnvironment` rotation updates, and reactive `SetSafeArea(false/true)` toggling.
  - `TestHUD3DNoGPUFallbackAndNilSafety` verifies `nsgotest.NoGPU()` fallback, nil `*HUD3D` safety, and `HUDAnchor.String()`.
- Validated with `gofmt`, `go run ./cmd/gameschema -check`, `go tool nsgo check -target macos,windows,linux,android,web`, `go tool nsgo test ./scene`, `go tool nsgo test -run TestStabilityTable .`, `go run ./tools/changelog check`, and `bun run verify-notes`.
