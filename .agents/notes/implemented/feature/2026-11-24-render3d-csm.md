# Agent Note: Cascaded Shadow Maps in render3d

Status: implemented

## Problem

A single directional shadow map stretched across a large 3D view frustum cannot maintain crisp shadow detail both near the camera and far across the horizon: near objects suffer severe perspective aliasing unless the shadow resolution is impractically large, while moving the camera causes jagged shadow edges to swim across static geometry. At the same time, `game/render3d` must run identically on all six NSGo GPU backends—including WebGL 2 and OpenGL ES 3.0 (`nsgotest.NoGPUCompute`)—and on NSGo's software GPU renderer without relying on `texture_depth_2d_array`, compute shaders, or unsupported WGSL transcendental ops (`exp`/`exp2`/`log2`).

## Decision

`packages/game/render3d/csm.go` implements portable directional Cascaded Shadow Maps (CSM) using a single 2D `gpu.FormatDepth32Float` atlas, practical frustum splits, rotation-invariant bounding-sphere texel snapping, and matching WGSL and pure-Go evaluators:

- **Practical split scheme (`ComputeCSMSplits`)**: computes `N+1` view-space depths (`N` in `[1, MaxCascades]` where `MaxCascades = 4`) via `lambda * near*(far/near)^(i/N) + (1-lambda)*(near + (far-near)*i/N)` (default `SplitLambda = 0.75`), clamped to `[near, min(far, MaxShadowDistance)]`.
- **Linear frustum corner unprojection & stable texel snapping (`FitCascades`, `FitCascadesFromCamera`)**: unprojects the full camera frustum's 8 NDC corners (`z = 0` near, `z = 1` far) and interpolates linearly along the 4 corner rays to obtain exact sub-frustum corners for both perspective and orthographic cameras. When `TexelSnap` is true, each cascade uses a rotation-invariant bounding sphere padded by `res/(res-1)` and quantized to `1/16` world unit so `TexelSize` never fluctuates under camera rotation or float rounding, snaps the cascade center in a fixed world-anchored light view basis to whole texel increments, and pulls the light orthographic near plane backward by `CasterPadding` so off-frustum casters behind the camera still cast shadows into view. `FitCascades` and `CSMFrame.EncodeUniforms` allocate zero heap memory.
- **Portable 2D depth atlas (`CascadedShadowMap`)**: packs cascades into a single `gpu.FormatDepth32Float` 2D render target (`1×1` for 1 cascade, `2×1` for 2, `2×2` for 3–4). `BeginCascadePass(enc, cascadeIdx)` accepts any `RenderPassStarter` (satisfied by both `*gpu.Encoder` and `*gpu.Frame`), clears the depth atlas on the first pass (`LoadClear`) and preserves prior tiles on subsequent cascades (`LoadLoad`), and applies both `SetViewport` and `SetScissor` to the cascade's tile. Each cascade owns a dedicated 64-byte uniform buffer for `light_view_proj` (updated in `Update`), while `DrawCaster` writes non-identity model matrices into aligned slots of a dynamic-offset uniform ring buffer (slot 0 permanently holds Identity) so all cascades and casters can be recorded in a single command buffer without `WriteBuffer` overwrites. Setting `CSMConfig.Retained` propagates `Retained` to every owned render target, sampler, buffer, and pipeline.
- **Portable WGSL & CPU reference (`CSMDepthWGSL`, `CSMWGSL`, `CSMLitWGSL`, `EvaluateCSM`, `EvaluateCSMShadow`)**: `CSMWGSL` binds at `CSMGroup = 1` alongside `CameraGroup = 0` with an explicit 416-byte 16-byte-aligned `CSMUniforms` layout (`light_vp0..3`, `atlas_rect0..3`, `splits`, `split_near`, `light_dir`, `bias_params`, `atlas_params`, `cascade_scales`) so no uniform-array dynamic indexing is needed. Sampling uses `textureSampleCompareLevel` (valid inside non-uniform cascade branches under WebGPU), clamps every hard or 3×3 PCF tap strictly within `[rect.xy + 0.501*texel, rect.xy + rect.zw - 0.501*texel]` to prevent cross-tile bleed, blends smoothly into cascade `i+1` across `CascadeBlendRange` (and fades cleanly toward 1.0 approaching `MaxShadowDistance`), and avoids `exp`/`exp2`/`log2`. `EvaluateCSMShadow` and `EvaluateCSM` mirror the WGSL math in pure Go, paired with `ReadCSMDepthAtlas` / `ReadCSMDepthAtlasAsync`.

## Alternatives considered

### Using `texture_depth_2d_array` instead of a 2D depth atlas

A depth texture array gives each cascade independent UV `[0, 1]` clamping without manual tile rects or scissor calls. It was rejected because NSGo's `gpu.RenderTargetDesc` and `shaderir` target portable 2D depth attachments (`texture_depth_2d`), and packing up to 4 cascades into a `2×2` 2D depth atlas works identically across Metal, D3D11, OpenGL 3.3, WebGPU, WebGL 2, OpenGL ES 3.0, and the software GPU renderer with a single depth render target and single bind group.

### Tight light-space AABB fitting without bounding-sphere snapping

Projecting each sub-frustum's 8 corners into a tight light-space AABB maximizes texel density for a static camera. It was rejected as the default because rotating the camera changes the AABB's width and height every frame, changing world-units-per-texel and causing severe shadow shimmer across static geometry. Tight AABB fitting remains available when callers explicitly set `TexelSnap: false`.

### Fixed world-unit `CascadeBlendRange` instead of a cascade-span fraction

Specifying cascade blend distance in meters is straightforward for one camera range, but a fixed distance either overwhelms the narrow near cascade (e.g. 1 m inside a 3 m span) or becomes imperceptible on the wide far cascade (1 m inside a 35 m span). Using a fraction in `[0, 0.5]` of each cascade's depth interval scales naturally across arbitrary near/far ranges and scene units.

## Consequences

- **Benefits**:
  - Crisp shadows simultaneously near the camera (3×+ higher texel density than a single shadow map at the same per-cascade resolution) and across distant cascades with smooth inter-cascade transitions and zero sub-texel swimming.
  - Identical behavior across all six NSGo GPU backends (including `nsgotest.NoGPUCompute` WebGL 2 / GLES 3.0) and the software GPU renderer, with zero steady-state allocations for cascade fitting and uniform packing.
  - Automatic recovery across GPU device loss when `CSMConfig.Retained` is enabled.
- **Costs and boundaries**:
  - `TexelSnap: true` trades a modest amount of shadow-map texel utilization (bounding sphere vs tight frustum AABB) in exchange for rotation- and translation-invariant stability.
  - Packing 3 or 4 cascades into a `2×2` atlas doubles the texture dimension relative to per-cascade `Resolution` (e.g. `Resolution: 1024` allocates a `2048×2048` depth texture), which callers targeting strict `2048` WebGL 2 limits must keep in mind; `NewCascadedShadowMap` checks `dev.Limits().MaxTextureDimension2D` up front and returns `ErrInvalidCSM` when exceeded.
  - `DrawCaster`'s built-in dynamic uniform ring caps non-identity caster draws per frame at `CSMConfig.MaxCasters` (default 256); larger dynamic scenes should raise `MaxCasters` or bind their own buffers via `DrawCasterGroup`.
  - All new code lives in `packages/game/render3d/csm.go` and `packages/game/render3d/csm_test.go` (plus `testdata/golden/csm_large_scene.png`) to avoid merge conflicts with parallel `render3d` lighting/shadow PRs.

## Verification

- `packages/game/render3d/csm_test.go` verifies practical split monotonicity/bounds, texel-snapping invariance under sub-texel/multi-meter translations and camera rotations, `CasterPadding` off-frustum coverage, cascade blend transitions, validation/error wrapping, 0-alloc `FitCascades` + `EncodeUniforms`, hard vs 3×3 PCF penumbra behavior, non-identity `DrawCaster` transforms, `Retained` device-loss restoration, and `Close`/nil safety.
- `TestCSMLargeSceneReferenceRender` renders a 6-pillar corridor spanning all 4 cascades (`z = -1.2` to `z = -37`) through `gpu.View` on both full capabilities and `nsgotest.NoGPUCompute` (WebGL 2 / GLES 3.0), asserting golden PNG parity (`testdata/golden/csm_large_scene.png`), >=3× near-cascade texel density gain over a single shadow map, debug cascade tinting, and ±4/255 RGB parity against pure-Go `EvaluateCSM` using read-back depth atlas data.
