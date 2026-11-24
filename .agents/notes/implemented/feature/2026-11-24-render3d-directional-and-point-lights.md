# Agent Note: 3D directional and point lights in PBR shading

Status: implemented

## Problem

`game/render3d` had GPU mesh upload (`GPUMesh`, #112) and camera uniform binding (`CameraBuffer` at `CameraGroup = 0`, #115), but no lighting representation, uniform buffer contract, or physically based shading implementation. Every 3D shader had to invent its own ad-hoc lighting struct, bind group slot, attenuation formula, and color-space conversion, risking layout mismatches across NSGo's GPU backends (WebGPU, Metal, Direct3D 11, OpenGL 3.3, WebGL 2, OpenGL ES 3.0, and the headless software renderer) and inconsistent material response across scenes.

## Decision

`packages/game/render3d/light.go` establishes the shared 3D lighting uniform contract, GPU resource wrapper, portable WGSL PBR shading functions, ready-to-use lit PBR pipeline, and matching CPU reference evaluator:

- **Fixed bind group and 16-byte-aligned uniform layout (`LightGroup = 1`, `LightUniformSize = 672`)**: `GPULightBuffer` binds a fixed-size `LightRigUniform` at `@group(1) @binding(0)` holding premultiplied linear `ambient: vec4<f32>`, `counts: vec4<u32>`, up to `MaxDirectionalLights = 4` `DirectionalLightUniform` entries (32 bytes each: normalized `-Direction` in `to_light.xyz` and premultiplied `Color * Intensity` in `color.rgb`), and up to `MaxPointLights = 16` `PointLightUniform` entries (32 bytes each: `Position` + `Range` in `position_range` and premultiplied `Color * Intensity` in `color.rgb`). Every struct field is a 16-byte `vec4<f32>` or `vec4<u32>`, guaranteeing identical byte offsets under WGSL uniform, GLSL std140 (WebGL 2 / GLES 3.0), HLSL cbuffer, and MSL constant buffer rules without implicit padding.
- **glTF 2.0 metallic-roughness PBR + `KHR_lights_punctual` attenuation**: Both `LightWGSL` (`point_light_attenuation`, `pbr_brdf`, `shade_pbr`, `linear_to_srgb`) and CPU functions (`PointLightAttenuation`, `EvaluatePBR`, `ShadePoint`, `LinearToSRGB`) evaluate identical float32 equations:
  - Directional light rays travel along `Direction`, so `L = -normalize(Direction)`.
  - Point light attenuation uses `max(min(1 - (d/Range)^4, 1), 0)^2 / max(d^2, 1e-4)` when `Range > 0` and `1 / max(d^2, 1e-4)` when `Range == 0`, eliminating singularities near `d = 0` and hard cutoff seams at `d = Range`.
  - Cook-Torrance microfacet specular uses Trowbridge-Reitz GGX NDF with perceptual roughness clamped to `>= 0.045`, height-correlated Smith-GGX visibility `V` (analytically cancelling the `4 * NdotV * NdotL` denominator), Schlick Fresnel with dielectric `F0 = 0.04` blended to `BaseColor` by `Metallic`, and energy-conserving Lambertian diffuse `(1 - F) * (1 - metallic) * baseColor / pi`.
  - Linear radiance converts to sRGB via the piecewise IEC 61966-2-1 transfer function rather than a `1/2.2` approximation because `gpu.FormatRGBA8Unorm` stores sRGB bytes directly without hardware conversion.
- **Device-loss-safe `GPULightBuffer` (`Retained` + explicit `*gpu.Device` on `Update`)**: Passing `LightBufferOptions{Retained: true}` retains the underlying uniform buffer's CPU shadow bytes, which causes NSGo's `gpu.Device` restore path to automatically recreate the buffer, its bind group layout, and its bind group. Because NSGo rebinds `Buffer.dev` to the new `Device` on restore while `gpu.Buffer` does not expose its device pointer, `GPULightBuffer.Update(dev, rig)` accepts the current `*gpu.Device` (such as `frame.Device()`, or `nil` to reuse the stored device) so post-restore writes target the restored device instead of a stale lost device.
- **Composable WGSL plus built-in lit PBR pipeline (`LitPBRWGSL`, `PBRDrawGroup = 2`, `NewLitPBRPipeline`)**: Custom shaders prepend `CameraWGSL + LightWGSL`, while standard metallic-roughness meshes can bind `PBRDrawBuffer` at group 2 (containing `model`, `normal_matrix` from `math3d.NormalMatrix`, `base_color`, `pbr_params`, and `emissive`) and draw immediately with `NewLitPBRPipeline`.

## Alternatives considered

### Why not use a storage buffer (`var<storage, read>`) for an unbounded light list?

A storage buffer removes the `MaxDirectionalLights` / `MaxPointLights` caps and scales cleanly to hundreds of lights in forward+ or clustered pipelines. It was rejected here because WebGL 2 and OpenGL ES 3.0 (`nsgotest.NoGPUCompute()`) do not support read-only storage buffers (`BindingStorageRead` fails device capability checks on those targets). A 672-byte uniform buffer works identically on every NSGo target, well below WebGL 2's 16 KiB minimum uniform block size, and keeps `LightGroup = 1` compatible with future clustered/storage extensions.

### Why not use Karis separable Schlick-GGX (`G / (4 * NdotV * NdotL)`) instead of Heitz height-correlated Smith-GGX visibility `V`?

Separable Schlick-GGX is widely taught and slightly simpler to read in isolation. It was rejected because it requires dividing by `4 * NdotV * NdotL + eps` (introducing epsilon sensitivity at grazing angles) and diverges from the glTF 2.0 specification (Appendix B), whereas height-correlated Smith-GGX visibility `V` cancels `4 * NdotV * NdotL` analytically and matches glTF 2.0 reference renderers.

### Why not fold tone mapping (ACES / Reinhard) into `LitPBRWGSL`?

Applying a tone curve in the lighting shader prevents clipped highlights under bright HDR lights. It was rejected because tone mapping belongs in post-processing rather than the direct lighting BRDF; keeping `shade_pbr` strictly linear before IEC 61966-2-1 sRGB encoding preserves exact linear light superposition (`ShadePoint(A+B) == ShadePoint(A) + ShadePoint(B)`) and allows pixel-exact GPU-vs-CPU verification without confounding tone curves.

## Consequences

- **Benefits**:
  - Single portable lighting contract across Metal, Direct3D 11, OpenGL 3.3, WebGPU, WebGL 2, OpenGL ES 3.0, and the headless software renderer.
  - Zero-allocation uniform encoding (`LightRig.AppendUniformData`, `LightUniforms.Encode`, `PBRDrawUniform.Encode`) suitable for per-frame updates.
  - Exact parity between WGSL shading and CPU `EvaluatePBR` / `ShadePoint`, enabling deterministic unit and pixel-level regression tests.
  - `Retained` light buffers survive GPU device loss and continue accepting `Update(frame.Device(), rig)` after recovery.
- **Costs & limits**:
  - Scenes using `LightRig` / `GPULightBuffer` are capped at 4 directional lights and 16 point lights; spot lights, shadows, image-based lighting (IBL), and clustered light culling for large light counts remain separate future layers.
  - Ambient lighting uses isotropic `Ambient.Color * Ambient.Intensity * BaseColor` so metallic surfaces remain visible in shadowed regions without IBL; once IBL lands, ambient fill will be superseded by irradiance/prefiltered environment maps.
  - `LitPBRWGSL` shades untextured uniform `PBRMaterial` parameters; textured/normal-mapped materials compose `CameraWGSL + LightWGSL` with their own texture bindings.

## Verification

- `go tool nsgo test ./render3d/...` runs validation, `KHR_lights_punctual` attenuation, 672-byte std140 layout and zero-allocation checks, CPU PBR physics tests, pixel-for-pixel GPU-vs-CPU `ShadePoint` verification, two golden-image lit PBR scene reference renders (`testdata/golden/lights_pbr_scene.png` and `lights_point_falloff.png`), `gpu.View` tests on both full and `nsgotest.NoGPUCompute` WebGL 2 / GLES 3.0 profiles, `Retained` device-loss + post-restore update tests, and scope-disposal tests.
- `go tool nsgo check -target macos,windows,linux,android,web` verifies cross-platform compilation and vetting.
- `bun run verify-notes` validates this note's placement and format.
