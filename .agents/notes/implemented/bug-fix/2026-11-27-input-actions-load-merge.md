# Agent Note: Actions.Load merges bindings per name instead of replacing

Status: implemented

## Problem

`Actions.Save` (`pkg/input/storage.go`) serializes all action and axis bindings in an `Actions` instance as `version: 1` JSON, and `Actions.Load` previously replaced `a.bindings`, `a.axes`, `a.motionAxes`, `a.pointerAxes`, and `a.gpAxes` wholesale. Because games bind default controls on startup (in code or via `project.File.BindInput`) and then call `Actions.Load`, any action or axis added in a newer build was absent from a player's older saved controls and silently disappeared on load.

## Decision

`Actions.Load` merges saved bindings per name across all five binding maps (`bindings`, `axes`, `motionAxes`, `pointerAxes`, and `gpAxes`) while keeping the `version: 1` JSON schema:

- Any action or axis name present in the saved JSON replaces that name's binding on `Actions`.
- Any name absent from the saved JSON keeps its existing (default) binding.
- Explicit unbindings made through `Unbind`, `UnbindAxis`, `UnbindMotionAxis`, `UnbindPointerAxis`, or `UnbindGamepadAxis` (including `Bind` with no inputs or a zero/invalid axis) are tracked in per-category tombstone maps on `Actions` and cleared when that name is bound again.
- `Actions.Save` writes explicitly unbound entries rather than omitting them: `[{"kind":"none"}]` for actions, `{}` for opposite-action axes, and `{"axis":"none"}` for motion, pointer, and gamepad axes.
- `Actions.Load` decodes empty action lists (`[]`) or all-`none` action lists (`[{"kind":"none"}]`), empty opposite-action axes (`{}`), and `"none"` / `""` motion, pointer, or gamepad axes as explicit unbindings: it deletes the binding and records the tombstone so subsequent `Save` calls remain round-trip stable and keep the control unbound across future restarts.
- `Actions.LoadReplace` provides the previous full-snapshot replacement semantics for callers and tests that want bindings absent from storage dropped; both `Load` and `LoadReplace` decode and validate the full document atomically before mutating `Actions`.

## Alternatives considered

- **Save only diffs against defaults (like Unity's `SaveBindingOverridesAsJson`)**: would require `Actions` to track a separate baseline/defaults snapshot and a lifecycle boundary between "binding defaults" and "player rebinding", complicating callers that bind controls incrementally in code and `project.File.BindInput`. Per-name merge with explicit unbind markers solves the upgrade bug without changing how games register defaults.
- **Bump the storage schema to `version: 2`**: unnecessary because `version: 1` already has `kind: "none"` for inputs, empty `{}` for opposite-action axes, and `"none"` for motion and pointer axes, and adding `"none"` for gamepad axes is backwards-compatible with every existing `version: 1` save.
- **Drop `LoadReplace` and change only `Load`**: some callers and tests restore a full snapshot onto a pre-populated `Actions` and rely on absent bindings being removed; exposing `LoadReplace` preserves that capability explicitly without making it the surprising default.

## Consequences

- **Benefits**: Adding new actions or axes in a game update no longer wipes their defaults for players who previously saved custom controls; players can still explicitly unbind default controls and have that unbinding survive restarts and repeated saves; existing `version: 1` saves and load-error diagnostics keep working unchanged.
- **Costs and known limits**: Callers that relied on `Actions.Load` discarding pre-existing bindings not listed in storage must switch to `Actions.LoadReplace`. Because `Save` still writes all currently bound names (not only diffs), changing a default binding for an *already existing* action name in a newer build will still be overridden by the value a player previously saved under that name.
