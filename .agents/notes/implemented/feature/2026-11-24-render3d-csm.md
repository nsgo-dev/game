# Agent Note: Cascaded Shadow Maps in render3d

Status: implemented

## Problem

A single directional shadow map (`DirectionalShadowMap`) stretched across a large view frustum cannot keep shadows sharp near the camera and still reach the horizon: near objects alias unless the map is impractically large, and moving the camera makes shadow edges swim. `game/render3d` must also run identically on every NSGo backend, WebGL 2 and OpenGL ES 3.0 (`nsgotest.NoGPUCompute`) and the software renderer included, without `texture_depth_2d_array`, compute shaders or transcendental WGSL ops.

## Decision

`packages/game/render3d/csm.go` adds directional cascaded shadow maps that shadow the same light the rest of the lit PBR path shades, `LightRig.Directional[0]`:

- **Splits and fitting**: `ComputeCSMSplits` uses the practical split scheme (`SplitLambda`, default 0.75). `FitCascades` cuts each cascade's slice of the camera frustum along the projection's own near and far planes (read from the projection matrix), so orthographic cameras whose near plane is at or behind them fit correctly; shadows start at view depth `max(near, 0.1)`. `FitCascadesFromCamera` uses the near and far planes `scene3d.Projection.Matrix` uses. With `TexelSnap` each cascade is sized from a bounding sphere quantized to 1/16 world unit and snapped to whole texels in a world-anchored light basis.
- **One 2D depth atlas**: cascades are tiles of one `FormatDepth32Float` render target (1x1, 2x1 or 2x2). `Update` refuses frames fitted with another cascade count, resolution or filter than the atlas, because tiles, texel sizes and the sampler's filter come from that config. The first `BeginCascadePass` after `Update` clears the whole atlas, whichever cascade it draws.
- **Casters**: `DrawCaster` gives each distinct model matrix one dynamic-offset slot per frame and reuses it across cascades, so `MaxCasters` counts distinct models, not draws; it returns false when the slots run out instead of silently dropping the shadow.
- **Integration with lit PBR**: `CSMGroup` is `ShadowGroup` (3). `CSMWGSL` (or `CSMWGSLForGroup`) declares `sample_csm_shadow(world_pos, world_normal)`, which reads the view depth from `CameraWGSL` and is passed as `shade_pbr_shadowed`'s visibility. `LitPBRCSMWGSL` / `NewLitPBRCSMPipeline` / `LitPBRPipelineOptions.CSMLayout` mirror the single directional shadow's pipeline, and filtering reuses `ShadowFilter`. `EvaluateCSMShadow` plus `ShadePointShadowed` (and `CSMFrame.DebugTint`) are the CPU reference.

## Alternatives considered

### A separate CSM lit shader and bind group layout

The first version shipped its own Lambert shader (`CSMLitWGSL`) at groups 1 and 2 with its own material type. It was dropped once `LightRig` and the lit PBR pipelines landed: groups 1 and 2 belong to `LightGroup` and `PBRDrawGroup`, and a second lit path would shade the scene differently from every other shadow kind.

### `texture_depth_2d_array` instead of an atlas

An array gives each cascade its own `[0, 1]` UVs, but NSGo's portable render targets are 2D depth attachments. A 2x2 atlas with per-tile viewports, scissors and tile-clamped taps works on every backend with one render target and one bind group.

### Tight light-space AABB fitting by default

A tight AABB maximizes texel density for a static camera but changes world units per texel as the camera turns, so shadows shimmer. It remains available with `TexelSnap: false`.

## Consequences

- Shadows stay sharp near the camera (the large-scene test checks at least 3x the texel density of one map at the same resolution) and blend smoothly between cascades.
- Bind group 3 is shared with the single directional, spot and point shadows, fog, IBL and skinning, so a lit PBR pipeline takes one of them; custom shaders move the bindings with `CSMWGSLForGroup`.
- Three or four cascades double the atlas size per side (`Resolution: 1024` allocates 2048x2048); `NewCascadedShadowMap` checks `MaxTextureDimension2D` and returns `ErrInvalidCSM` when it is exceeded.

## Verification

- `packages/game/render3d/csm_test.go` covers split bounds, texel-snapping stability, cascade selection and blending, validation, zero-allocation fitting and encoding, `Update` refusing mismatched frames, caster slot reuse and overflow, an orthographic camera with a negative near plane, cascade passes drawn in reverse order, hard vs 3x3 PCF, `Retained` device loss and `Close`.
- `TestCSMLargeSceneReferenceRender` renders six pillars across four cascades through `LitPBRCSMWGSL` on the full and `NoGPUCompute` profiles, compares with `testdata/golden/csm_large_scene.png`, and checks GPU pixels (debug tint included) against `ShadePointShadowed` with `EvaluateCSMShadow` within 4/255.
