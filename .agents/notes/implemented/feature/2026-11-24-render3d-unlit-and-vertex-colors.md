# Agent Note: 3D Unlit Materials and Vertex Colors in render3d

Status: implemented

## Problem

`render3d` provides 3D camera uniforms (`CameraBuffer`), base mesh buffers (`GPUMesh` with positions, normals, and UVs), and 2D textures/samplers ([render3d textures and samplers](2026-11-24-render3d-textures-and-samplers.md)), while `gltf` already parses `KHR_materials_unlit` (`Material.Unlit`) and `COLOR_0` (`Primitive.Colors0`). However, games have no built-in unlit material shader, uniform binding contract, or vertex-color buffer layout to render glTF unlit assets and vertex-colored meshes consistently across all NSGo GPU backends.

Several cross-backend constraints shape the implementation:
- Unlit glTF assets may omit `COLOR_0`, `baseColorTexture`, or sampler definitions, yet pipelines need a fixed vertex layout and valid texture/sampler bindings on every backend.
- `KHR_materials_unlit` specifies linear-space multiplication (`baseColorFactor * baseColorTexture * COLOR_0`) with sRGB base-color decoding, alpha-mode handling (`OPAQUE`, `MASK` discard, `BLEND`), and linear-to-sRGB output without scene lighting; NSGo's software renderer and WebGL 2 / GLES 3.0 targets do not implement WGSL `exp()`/`exp2()`/`log2()` builtins in the software shader VM, and `discard;` must execute inside the fragment entry point rather than helper functions for MSL/HLSL/GLSL translation.
- Non-sRGB render targets (`FormatRGBA8Unorm`, used by headless `gpu.View` and `gpu.NewSoftwareRenderer`) require shader-side linear-to-sRGB encoding, whereas hardware sRGB swapchains (`TargetFormat.IsSRGB()`) encode linear fragment outputs automatically.
- Retained GPU resources recreated after device loss rely on NSGo's dependency-ordered restore (`buffers -> textures -> samplers -> bind group layouts -> bind groups -> pipelines`) and must not cache stale `*gpu.Device` pointers.

## Decision

`packages/game/render3d/unlit.go` adds vertex-colored geometry, unlit material bindings, WGSL pipeline creation, glTF helpers, and a pure-Go CPU reference without modifying existing `render3d` files:

- **Extended interleaved vertex layout (`ColoredMesh`, `GPUColoredMesh`, `ColoredVertexLayout`)**: `ColoredVertexLayout` preserves `MeshVertexLayout` locations 0–2 (position, normal, UV) and appends `@location(3) color: vec4<f32>` at byte offset 32 (48-byte stride). When `ColoredMesh.Colors` is empty, `AppendVertexData` writes linear opaque white `(1, 1, 1, 1)` so meshes with and without `COLOR_0` share one pipeline. `ColoredMeshFromGLTF` validates triangle topology, extracts `COLOR_0` (already expanded from VEC3 RGB to alpha 1.0 by `gltf`), and applies `KHR_texture_transform` UVs when present on `baseColorTexture`.
- **Material bind group at `@group(1)` (`UnlitMaterial`, `GPUUnlitMaterial`, `UnlitMaterialBindingLayout`)**: `UnlitMaterialGroup = 1` pairs directly with `CameraGroup = 0`, binding a 32-byte 16-byte-aligned uniform block (`base_color_factor`, `(alpha_cutoff, alpha_mode, use_vertex_color, 0)`) at binding 0, a sampled 2D texture at binding 1, and a sampler at binding 2. When `BaseColorTexture` or `BaseColorSampler` is nil, `NewGPUUnlitMaterial` allocates a material-owned 1×1 white `FormatRGBA8UnormSRGB` texture and/or linear-repeat sampler (respecting `UnlitMaterialOptions.Retained`) and closes only material-owned resources on `Close()`. `AppendUnlitUniforms` and `GPUUnlitMaterial.Update(dev, mat)` encode uniforms with zero heap allocations and accept `dev *gpu.Device` explicitly so retained materials remain updatable after device-loss restoration.
- **Portable WGSL shader and target-aware pipeline (`UnlitWGSL`, `NewUnlitPipeline`)**: `UnlitWGSL` prepends `CameraWGSL`, runs `discard;` directly inside fragment entry points when `alphaMode == MASK` and `base.a < alphaCutoff`, and implements IEC 61966-2-1 linear-to-sRGB using `clamp`, `if`, and `pow` (strictly positive domain `v > 0.0031308`, avoiding `exp`/`exp2`/`log2`). `UnlitWGSL` exposes both `fs_main` (linear-to-sRGB) and `fs_linear` (linear passthrough); `NewUnlitPipeline` selects `fs_linear` when `TargetFormat.IsSRGB()` and `fs_main` otherwise, configures `gpu.BlendAlpha()` with `DepthWrite: false` for `UnlitAlphaBlend`, and maps `DoubleSided` to `gpu.CullNone` vs `gpu.CullBack`.
- **glTF integration and CPU reference (`UnlitMaterialFromGLTF`, `UploadGLTFUnlitMaterial`, `UnlitSamplerFromGLTF`, `EvaluateUnlit`)**: `UnlitMaterialFromGLTF` enforces `KHR_materials_unlit` (`mat.Unlit == true`), `UploadGLTFUnlitMaterial` decodes and uploads `baseColorTexture` as sRGB alongside its glTF sampler, and `EvaluateUnlit` evaluates the exact shading equation and alpha blending (`UnlitSample.RGBA8`, `UnlitSample.BlendOver`) in pure Go for tests and tooling.

## Alternatives considered

### Expand `Mesh` / `MeshVertexStride` in `mesh.go` to 48 bytes for all meshes

Adding vertex colors directly to `Mesh` would let one mesh type serve both lit and colored geometry, avoiding a second `ColoredMesh` type. It was rejected because it would increase vertex buffer memory by 50% for every uncolored mesh, break existing `MeshVertexStride == 32` layout contracts and tests, and conflict with concurrent `render3d` changes. Keeping `ColoredMesh` additive with locations 0–2 matching `MeshVertexLayout` preserves backward compatibility and zero-conflict composition.

### Bind vertex colors in a separate secondary vertex buffer slot

Placing `COLOR_0` in vertex buffer slot 1 alongside `MeshVertexLayout` in slot 0 would let callers reuse existing `GPUMesh` vertex buffers. It was rejected because every draw would need to manage and bind a dummy per-vertex color buffer matching the mesh's vertex count whenever `COLOR_0` is omitted, complicating `Draw()` and device-loss retention. A single 48-byte interleaved buffer guarantees every draw is self-contained and cache-friendly.

### Always encode sRGB in `UnlitWGSL` with a uniform toggle instead of dual fragment entry points

Encoding linear-to-sRGB conditionally via a uniform flag avoids a second fragment entry point. It was rejected because target format sRGB-ness is an immutable property of the `gpu.Pipeline` (`ColorTargetState.Format`), not the material; choosing `fs_main` vs `fs_linear` at `NewUnlitPipeline` time lets one `GPUUnlitMaterial` draw cleanly to either linear (`FormatRGBA8Unorm`) or sRGB hardware targets without mutating material uniforms or risking double gamma encoding.

## Consequences

- **Benefits**: Games can load and render glTF `KHR_materials_unlit` and `COLOR_0` assets across all NSGo GPU backends with pixel-accurate CPU reference parity (`EvaluateUnlit`), zero-allocation uniform updates (`AppendUnlitUniforms`), automatic fallback textures/samplers, and `Retained` device-loss restoration. Verified by glTF fixture tests (opaque, alpha-mask, alpha-blend, double-sided, VEC3/VEC4 `COLOR_0`, Khronos `TextureTransformMultiTest` and `MeshoptCubeTest`), golden reference render (`unlit_vertex_colors_scene.png`), and headless `gpu.View` tests on both full and `nsgotest.NoGPUCompute` (WebGL 2 / GLES 3.0) profiles.
- **Costs**: Unlit materials own a small 1×1 white fallback texture and default sampler per textureless `GPUUnlitMaterial` instance rather than sharing a process-global device cache (keeping scope ownership and device-loss lifecycles explicit and race-free), and swapping a material's texture or sampler requires creating a new `GPUUnlitMaterial` rather than mutating the bind group inside `Update`.
