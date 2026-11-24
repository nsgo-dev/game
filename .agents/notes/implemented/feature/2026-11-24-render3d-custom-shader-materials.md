# Agent Note: Custom WGSL and Kage 3D Shader Materials in render3d

Status: implemented

## Problem

Games using `game/render3d` need custom 3D surface shaders (stylized cel shading, holograms, dissolves, procedural surfaces) written in WGSL or Kage with strongly-typed parameters bound cleanly alongside `CameraBuffer` (`@group(0)`) and `GPUMesh` (`MeshVertexLayout`) on NSGo's `gpu.View`.

Authoring custom materials directly on raw `gpu.Device` primitives exposes several cross-backend hazards:
- Hand-packing uniform bytes risks subtle std140/WGSL alignment bugs (`vec2` 8-byte, `vec3` 16-byte align / 12-byte size, `vec4` 16-byte, 16-byte total buffer multiple) and WGSL forbids host-unshareable `bool` fields inside uniform structs.
- `gpu.CompileKage` only compiles 2D UI fragment shaders (`Fragment(dst, pos)` with Ebitengine-style `image0At` coordinates) and cannot be attached directly to a 3D `MeshVertexLayout` + `CameraWGSL` render pipeline.
- WebGPU and NSGo's `gpu.Device.CreatePipeline` reject `nil` entries in `PipelineDesc.Layouts`, so binding material parameters at `@group(1)` requires a valid `@group(0)` layout even for clip-space shaders that do not bind a camera.
- Unset texture slots crash or fail bind-group validation unless backed by a live 1×1 fallback texture and sampler, and caching a `*gpu.Device` on a `Retained` material breaks updates after device loss and restore (same constraint documented in [3D GPU Textures and Samplers in render3d](2026-11-24-render3d-textures-and-samplers.md)).

## Decision

`packages/game/render3d/custom_material.go` adds `CustomMaterial`, `CustomShaderDesc`, `ShaderParamDesc`, `ShaderParamKind`, `ShaderLanguage`, `ComputeUniformLayout`, `CustomShaderWGSL`, `CompileCheck`, and `CompileCheckDesc`; the only edit to another `render3d` file is `LightWGSLForGroup` in `light.go`:

- **Fixed `@group(1)` material contract (`CustomMaterialGroup = MaterialGroup`)**: Non-texture parameters (`ParamFloat`, `ParamVec2`, `ParamVec3`, `ParamVec4`, `ParamColor`, `ParamInt`, `ParamBool`) pack in declared order into a `MaterialParams` uniform struct at `@group(1) @binding(0)` (`material`) using WGSL/std140 alignment rules (`4/8/16`-byte alignment, `ParamBool` lowered to `u32` `0`/`1`, total buffer rounded up to a 16-byte multiple). Each `ParamTexture` allocates consecutive `(texture_2d<f32>, sampler)` bindings in `@group(1)` named `<Name>` and `<Name>_sampler`, backed by an owned 1×1 fallback `GPUTexture` (opaque white by default, customizable via `Default`) and a shared default `GPUSampler` (`SamplerLinearRepeat`) until overridden by `SetTexture`.
- **Automatic preamble for WGSL and Go-AST translator for Kage**: `ShaderWGSL` prepends `CameraWGSL` (when `CameraLayout` is provided and `@group(0)` is not already declared) plus the generated `MaterialParams` struct and texture/sampler declarations (opt-out via `ManualBindings: true`), adjusting WGSL compile-error line numbers back to the caller's source. `ShaderKage` parses Kage with Go's standard `go/parser` (`go/ast`), validates package-level `var` uniform declarations against `Params` (or infers `Params` when omitted), and translates helper functions and `func Surface(...) vec4` (or classic `func Fragment(dst vec4, pos vec2) vec4`) into portable WGSL wired to `MeshVertexLayout` and optional `CameraWGSL`, supporting `sample(<tex>, uv)` and `imageNAt(uv)` without emitting `exp()`/`exp2()`/`log2()` builtins.
- **Explicit `*gpu.Device` on `Sync` / `Update` and `Retained` recovery**: `CustomMaterial` does not store a `*gpu.Device` pointer; callers pass `dev` to `Sync(dev)` / `Update(dev)`. Typed setters (`SetFloat`, `SetVec2`, `SetVec3`, `SetVec4`, `SetColor`, `SetInt`, `SetBool`, `SetTexture`) and atomic `ApplyMap` mutate the pre-allocated CPU uniform buffer in place (marking dirty only when bytes actually change, giving zero allocations in steady state) and reject unknown names, type mismatches, non-finite numbers, and `Min`/`Max` violations with `*CustomMaterialError` (matching `ErrInvalidCustomMaterial`). Setting `Retained: true` propagates retention to the uniform buffer, fallback textures, default sampler, and pipeline so NSGo restores the full material across device loss.
- **Groups 2 and 3 for placement and lights**: `DrawLayout` puts a `PBRDrawBuffer` layout at `PBRDrawGroup`, and the Kage vertex stage then applies its model and normal matrices so `worldPos` is world-space; `ExtraLayout` adds group 3 (lights via `LightWGSLForGroup(3)`, since `LightGroup` is the material's group 1, or a shadow map). Without `DrawLayout`, Kage passes object-space positions through.
- **Bind group cache**: `Sync` keeps the bind groups of the 8 most recently bound texture/sampler combinations. NSGo v0.3.0 keeps every bind group a device creates in its restore list and never prunes closed ones, so creating one per texture change grew memory without bound.
- **Kage error lines**: the translator records the Kage line of every generated function, constant and statement, so a WGSL compile error in translated code reports the Kage line.
- **Cross-backend `CompileCheck`**: `CompileCheck` and `CompileCheckDesc` compile the generated WGSL and build a pipeline against a headless 1×1 `gpu.SoftwareRenderer` with `gpu.NewMemoryPipelineCache()`, exercising NSGo's MSL, HLSL, GLSL 3.30, GLSL ES 3.00 (WebGL 2 / Android), and WGSL emitters.

## Alternatives considered

### Pass `ShaderKage` directly to `gpu.CompileKage`

Calling `gpu.CompileKage` avoids writing a Kage-to-WGSL translator, and already powers 2D fragment hot-reload in [Shader and Material Hot-Reload](2026-11-24-shader-material-hot-reload.md). It was rejected for 3D materials because `gpu.CompileKage` produces a legacy 2D fragment IR with no `@vertex` stage, no `MeshVertexLayout` attributes (normal/UV/world position), no `CameraWGSL` `@group(0)` binding, and no `@group(1)` `MaterialParams` uniform struct. Translating Kage surface shaders via `go/parser` to portable WGSL keeps authentic Kage syntax (`var` uniforms, `func Surface`/`Fragment`, `image0At`) while sharing one 3D pipeline and all five NSGo shader backends.

### Pad `ParamVec3` with an explicit dummy `f32` field in `MaterialParams`

Metal's `float3` has `sizeof == 16` rather than `12`, so a scalar placed immediately after a `vec3<f32>` lands at offset `+16` on Metal versus `+12` in WGSL, GLSL std140, HLSL `cbuffer`, and NSGo's `shaderir.Type.StructFieldOffset`. Emitting an automatic `_pad: f32` field after every `vec3<f32>` was rejected because Metal would still advance `float3` by 16 bytes and place `_pad` at `+16` (pushing the next field to `+20`), breaking both Metal and WGSL/std140 alignment. Storing `ParamVec3` as `vec4<f32>` would keep both layouts equal but change the type user WGSL sees. `ComputeUniformLayout` instead refuses a 4-byte parameter directly after a `ParamVec3`, which is the only case where WGSL and Metal disagree, and tells the author to reorder or use `ParamVec4`.

### Require a mandatory `*CameraBuffer` for every `CustomMaterial`

Requiring `CameraLayout != nil` would eliminate the empty `@group(0)` layout helper. It was rejected because clip-space effects, full-screen/UI 3D quads, and unit tests frequently draw meshes without a `CameraBuffer`. Creating an owned empty `@group(0)` layout only when `CameraLayout == nil` and the shader declares no `@group(0)` resource satisfies `gpu.Device.CreatePipeline`'s non-nil layout rule while keeping 3D camera integration (`CameraLayout: camBuf.Layout()`) one field away.

## Consequences

- **Benefits**: Both WGSL and Kage 3D materials compile across all five NSGo GPU backends (verified via `PipelineCache` artifact inspection in `TestCustomMaterialCompilesForEveryBackend`), draw reference 3D scenes (`custom_material_wgsl.png`, `custom_material_kage.png`) on `full` and `nsgotest.NoGPUCompute` (WebGL 2 / GLES 3.0) profiles, support dynamic frame-to-frame parameter updates with zero steady-state allocations, and recover cleanly across `Retained` GPU device loss.
- **Costs**: The Kage translator supports the portable surface-shader subset of Kage (scalars, vectors, matrices, helper functions, control flow, `sample`/`imageNAt`, `var` uniforms) rather than full arbitrary Go/Kage packages (no structs, arrays, or `imageNSrcSize` 2D atlas helpers). The `[vec3, scalar]` parameter order is refused until NSGo's MSL emitter lays out `float3` struct members as WGSL does. `Fragment`'s `pos` is the mesh UV, not a texel position as in 2D Kage, and `imageNAt` wraps through the sampler instead of returning transparent outside the image.
