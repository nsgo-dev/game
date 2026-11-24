# Agent Note: 3D spot lights in the render3d light rig

Status: implemented

## Problem

`game/render3d` needs punctual spot lights with inner and outer cones (`#252`, part of `#251`) in its PBR shading model. That model is the `LightRig` from `#116`: `LightWGSL` declares the light block at `LightGroup` (group 1), `shade_pbr` and `ShadePoint` shade a fragment with every light in it, and `LitPBRWGSL` with its fog, IBL, shadow and skinned variants and custom materials all build on it. A game needs a spot light in the same pass as its other lights, such as a sun plus a flashlight, and it must shade exactly like a point light of the same color and intensity inside its cone.

## Decision

Spot lights are a fourth member of `LightRig`, not a separate lighting system:

- **`SpotLight`** (`spotlight.go`) has the same shape as its neighbours (`Position`, `Direction`, `Color math3d.Vec3`, `Intensity`, `Range`) plus `InnerConeAngle` and `OuterConeAngle`. `Validate` wraps `ErrInvalidLight` and enforces the glTF `KHR_lights_punctual` bounds `0 <= inner < outer <= pi/2`, so a zero outer cone, which would light nothing, and equal cones, which would need a clamped division, are refused rather than silently accepted.
- **The uniform block grows at its end**: `LightRigUniform` gains `spot: array<SpotLightUniform, 8>` after the point lights, three `vec4<f32>` per light (`position_range`, `direction_scale`, `color_offset`), and `counts.z` carries the spot count. The cone's `angleScale = 1 / max(cos(inner) - cos(outer), 0.001)` and `angleOffset = -cos(outer) * angleScale` are precomputed on the CPU (`SpotLight.ConeScaleOffset`) into the `.w` lanes. Ambient, counts, directional and point lights keep their offsets, and `LightUniformSize` becomes 1056 bytes.
- **One shading path**: `shade_pbr_shadowed` gains a spot loop that reuses `point_light_attenuation` for distance falloff and `pbr_brdf` for the BRDF, multiplied by `spot_cone_attenuation`. `ShadePointShadowed` mirrors it with `PointLightAttenuation`, `SpotConeAttenuation` and `EvaluatePBR`. Ambient, output encoding (`linear_to_srgb`, or linear for an `HDRTarget`), the `n.v <= 0` and `v + l = 0` guards and every lit pipeline variant therefore apply to spot lights unchanged.

## Alternatives considered

### A separate spot light block and pipeline

The first version of this change added `SpotLightSet`, `GPUSpotLights`, `SpotLightsWGSL`, `SpotLitWGSL`, `SpotLitRenderer` and a `SpotMaterial`, because `#116` was still open. Its block used group 1 like `LightWGSL` with a different struct, so no shader could have both and a scene could not mix spot lights with directional or point lights. It also had its own BRDF copy, Reinhard plus 2.2 gamma output and a different ambient term, so the same material looked different under a spot light. It was replaced once `#116` merged.

### A storage buffer for an unlimited number of lights

Rejected for the same reason the rest of the rig is a uniform block: `gpu.View` targets WebGL 2 and OpenGL ES 3.0 (`nsgotest.NoGPUCompute`), where fragment shaders cannot read storage buffers. Eight spot lights per pass fit the uniform block on every backend; clustered lighting can lift the limit on capable backends.

## Consequences

- A scene shades directional, point and spot lights together in one pass, through every pipeline built on `LightWGSL`, with no new bind group or pipeline.
- `LightUniformSize` grows from 672 to 1056 bytes; code that read the light block through `LightWGSL` or `LightUniforms.Encode` needs no change.
- Spot lights do not cast shadows yet; shadow-mapped local lights are scoped to `#255`. Other spot light work (local shadows, clustered lighting) should take `SpotLight` and `SpotConeAttenuation` rather than define its own cone math.

## Verification

- `packages/game/render3d/spotlight_test.go` covers validation including the cone boundaries, cone and distance attenuation against the glTF formulas, the uniform layout and zero allocations, a spot light inside its inner cone shading exactly like the matching point light, a rig mixing a sun, a point light and a spot light equalling the sum of each alone, a light opposite the viewer staying finite, and the `spot_lights_scene` golden scene rendered through `NewLitPBRPipeline`.
- `TestLitPBRMatchesCPUReference` in `light_test.go` compares GPU pixels with `ShadePoint` for a spot light alone and for a sun plus a point light plus a spot light.
