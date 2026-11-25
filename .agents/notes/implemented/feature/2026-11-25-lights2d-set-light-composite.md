# Agent Note: Explicit SourceOver Light Composite on lights2d.Scene

Status: implemented

## Problem

`lights2d.Scene.LightComposite` is a `core.CompositeOp` field whose zero value is documented to default to `core.CompositeLighter`. Because `core.CompositeSourceOver` is itself the zero value (`0`) of `core.CompositeOp`, callers had no way to ask `Scene.Draw` for plain alpha-over light glows (#1166). This blocks callers that need `CompositeSourceOver` glows—for example, when working around backend constraints on non-`SourceOver` fills inside a clip.

## Decision

Keep the zero value of `Scene` defaulting to `core.CompositeLighter` for full backward compatibility, and track explicit selection with an unexported `lightCompositeSet bool` field on `lights2d.Scene`:

- `Scene.SetLightComposite(op core.CompositeOp)` sets `s.LightComposite = op` and `s.lightCompositeSet = true` (no-op on a nil receiver), allowing `core.CompositeSourceOver` to be chosen explicitly.
- `Scene.EffectiveLightComposite() core.CompositeOp` returns `core.CompositeLighter` when `s == nil` or when `s.LightComposite == 0 && !s.lightCompositeSet`, and `s.LightComposite` otherwise. Direct struct-field assignments to non-zero ops (such as `s.LightComposite = core.CompositeScreen`) continue to work unchanged.
- `Scene.Draw` reads `s.EffectiveLightComposite()` instead of checking `s.LightComposite == 0`.
- `sceneformat.BuiltScene.SyncLights2D` (introduced in [Scene 2D Lights, Occluders, Ambient Color, and Editor Lighting Preview](2026-11-25-editor-lights2d-preview-and-handles.md)) inspects `EffectiveLightComposite()` so an explicit `CompositeSourceOver` survives scene rebuilds while an unset default stays unset.

## Alternatives considered

### Changing `Scene.LightComposite` to `*core.CompositeOp`

A pointer field makes `nil` mean "default to `CompositeLighter`" and any non-nil pointer mean "use `*LightComposite`", including `&sourceOver`. Rejected because it breaks existing callers that assign `scene.LightComposite = core.CompositeScreen` directly.

### Leaving `LightComposite` unchanged

The doc comment already documented the zero-value default, so no code was strictly violating its contract. Rejected because it forces callers that need `CompositeSourceOver` glows to reimplement the glow pass outside `Scene.Draw`.

## Consequences

- **Benefits**: Existing scenes keep `core.CompositeLighter` glows with zero breaking changes, direct non-zero `LightComposite` assignments remain valid, callers can opt into `core.CompositeSourceOver` via `SetLightComposite`, and `sceneformat.BuiltScene.SyncLights2D` preserves all three states.
- **Trade-offs**: Assigning `s.LightComposite = 0` directly after a prior `SetLightComposite(...)` call now produces `core.CompositeSourceOver` rather than reverting to `core.CompositeLighter`; callers switching back to additive glows call `SetLightComposite(core.CompositeLighter)`.
- **Verification**: Covered by `TestSceneLightCompositeModes` in `packages/game/lights2d/lights2d_test.go` (checking recorded `DrawComposite` / `DrawFillGradient` ops for default, `SetLightComposite(CompositeSourceOver)`, direct `CompositeScreen`, and switching back to `CompositeLighter`) and `TestSyncLights2DPreservesLightComposite` in `packages/game/sceneformat/lights2d_test.go`.
