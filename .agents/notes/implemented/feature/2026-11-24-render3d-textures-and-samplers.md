# Agent Note: 3D GPU Textures and Samplers in render3d

Status: implemented

## Problem

`render3d` already provides GPU mesh buffers (`GPUMesh`) and camera uniforms (`CameraBuffer`), but games and upcoming PBR materials (`#113`) need a portable way to upload 2D textures from Go `image.Image` values and raw pixel buffers with sRGB/linear color-space control, mipmaps, filtering, coordinate wrapping, and device-loss recovery on NSGo's `gpu.Device`.

Raw `gpu.Device.CreateTexture` and `CreateSampler` leave several cross-backend traps to every caller:
- Native backends in NSGo v0.3.0 do not implement `Device.GenerateMipmaps` (`Features.MipmapGeneration` is false), and naive byte averaging darkens sRGB mipmaps.
- Go's `image.Image` types mix premultiplied (`*image.RGBA`), straight (`*image.NRGBA`), indexed, grayscale, and non-zero-origin sub-images, whereas 3D textures expect straight-alpha bytes and packed data maps (normals, metallic-roughness) must keep raw RGB channels intact even when alpha is zero.
- WebGPU rejects samplers with `MaxAnisotropy > 1` unless min, mag, and mipmap filters are all `FilterLinear`.
- Storing a `*gpu.Device` pointer on a retained texture wrapper breaks after device loss and restore, because NSGo retargets `gpu.Texture` to the new device instance.

## Decision

`packages/game/render3d/texture.go` adds `PixelBuffer`, `TextureUpload`, `GPUTexture`, `SamplerOptions`, `GPUSampler`, `TextureBindingLayout`, and `BindTexture` following the scope-owned handle pattern of `GPUMesh`:

- **Two upload paths, one validated model**: `PixelBufferFromImage` converts any `image.Image` (including non-zero-origin sub-images) into a tightly packed straight-alpha RGBA8 `PixelBuffer`, copying `*image.NRGBA` rows verbatim so zero-alpha packed data maps are preserved. `UploadTexturePixels` validates dimensions, stride, slice length (overflow-safe `uint64` arithmetic), and format (`FormatRGBA8Unorm`, `FormatRGBA8UnormSRGB`, `FormatRG8Unorm`, `FormatR8Unorm` — all core formats supported across every NSGo backend without `ExtendedFormats`).
- **Portable CPU mipmap chain**: When `TextureUpload.Mipmaps` is true, `uploadTextureLevels` computes an area-weighted box mipmap chain on the CPU (decoding sRGB RGB channels to linear light before averaging and re-encoding afterward, while averaging alpha linearly) and uploads each level with `dev.WriteTextureMip`. This works on Metal, Direct3D 11, OpenGL 3.3, WebGPU, WebGL 2, OpenGL ES 3.0, and the software device, and automatically records every mip level in NSGo's `Retained` shadow writes for device-loss restoration.
- **No cached `*gpu.Device` on `GPUTexture`**: `UpdateImage` and `UpdatePixels` accept `dev *gpu.Device` explicitly so a retained `GPUTexture` continues updating cleanly after a device loss and restore without holding a stale device pointer.
- **Cross-backend sampler validation and group-agnostic binding**: `SamplerOptions.Validate` enforces finite non-negative LOD bounds (`MaxLOD >= MinLOD`) and WebGPU's `MaxAnisotropy > 1` all-linear filter requirement, wrapping errors in `ErrInvalidSampler`. `TextureBindingLayout` and `BindTexture` bind `(texture_2d, sampler)` at bindings 0 and 1 without claiming a fixed `@group` index, avoiding collisions with upcoming material bind groups (`#113`).

## Alternatives considered

### Rely on `gpu.Device.GenerateMipmaps` on the GPU

GPU mipmap generation avoids CPU work during upload, but NSGo v0.3.0 reports `Features.MipmapGeneration == false` on native backends and only implements it on the software device. Relying on it would fail with `gpu.ErrUnsupported` on real desktop/mobile/web targets and would not replay mip writes across `Retained` device restores. CPU generation with per-level `WriteTextureMip` works everywhere and guarantees deterministic linear-light sRGB filtering.

### Reserve a fixed `TextureGroup = 1` and `TextureWGSL` constant

Mirroring `CameraGroup = 0` and `CameraWGSL` would make single-texture shaders slightly shorter, but metallic-roughness materials (`#113`) will own group 1 with a material uniform plus multiple texture/sampler bindings. Keeping `TextureBindingLayout` and `BindTexture` group-index-agnostic lets callers bind standalone textures at any group while leaving group 1 free for materials.

### Store `*gpu.Device` inside `GPUTexture` for zero-argument `UpdateImage`

Caching `dev` on `GPUTexture` (as `CameraBuffer` does for non-retained per-frame writes) simplifies update signatures, but after `TriggerGPUDeviceLost` + `RestoreGPUDevice` NSGo swaps the internal device inside a retained `*gpu.Texture`. Passing `dev` to `UpdateImage` / `UpdatePixels` matches `GPUMesh` and ensures retained textures remain updatable after restoration.

## Consequences

- **Benefits**: Callers upload Go images or padded raw pixel buffers (linear or sRGB, 1/2/4 channels) with portable linear-light mipmaps, validated sampler presets, exact `ByteSize()` budget accounting across all mip levels, dynamic updates, and `Retained` device-loss recovery. Golden reference renders (`textured_mesh_scene.png`, `textured_filtering_wrapping.png`) and `nsgotest` `gpu.View` tests (full and `NoGPUCompute` profiles) prove filtering, wrapping, sRGB decoding, mipmap sampling, and lifecycle behavior.
- **Costs**: Mipmap generation runs on the CPU at upload and update time (an extra allocation per mip level), and `PixelBuffer` intentionally restricts uploads to core uncompressed 8-bit formats (`RGBA8`, `RG8`, `R8`), leaving block-compressed GPU texture uploads to `#271` and floating-point HDR/render-target textures to lower-level `gpu.Device` APIs.
