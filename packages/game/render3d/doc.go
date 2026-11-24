// Package render3d provides the GPU-agnostic pure-Go 3D render-queue pipeline
// and developer tools for NSGo 3D games.
//
// # Render Pipeline (#244–#248)
//
// [Queue.Build] executes the complete CPU-side frame preparation stage with
// zero steady-state allocations:
//
//   - Frustum culling (#244): extracts six view-frustum planes via
//     [math3d.FrustumFromMat4] (or [math3d.FrustumFromMat4NO] when
//     [Queue.FrustumNO] is set) and discards items whose world AABB lies
//     outside the view volume.
//   - Software occlusion culling (#380): [OcclusionBuffer] rasterizes static
//     [Occluder] volumes conservatively into a low-resolution CPU depth buffer,
//     builds a max-Z Hi-Z mip pyramid, and performs O(1) zero-false-negative
//     AABB visibility queries integrated into [Queue.BuildWithOcclusion].
//   - Levels of detail (#247): selects the active [LODLevel] per visible item
//     from camera distance and projected screen-height ratio with configurable
//     hysteresis ([SelectLOD]) to eliminate boundary popping.
//   - Opaque sorting & batching (#245): orders opaque and alpha-masked draws
//     by (PipelineID, MaterialID, MeshID, Blend, front-to-back distance) to
//     minimize pipeline/material state switches and early-Z overdraw.
//   - Instancing (#246): merges consecutive opaque draws sharing the same
//     (PipelineID, MaterialID, MeshID, Blend) into [InstanceBatch] entries
//     holding contiguous [math3d.Mat4] transform slices.
//   - Transparency (#248): separates [BlendAlphaBlend] draws and sorts them
//     strictly back-to-front by distance from the camera.
//
// # Developer Tools (#286–#289)
//
//   - Debug drawing & performance (#286): [DebugDraw] records 3D wireframe
//     primitives (lines, AABBs, spheres, rays, axes, grids, frustums) and
//     projects them with near-plane clipping into [*core.Drawing];
//     [FrameProfiler] tracks rolling frame-time and draw-call metrics.
//   - Gizmos & wireframes (#287): [Gizmo] generates 3D translate/rotate/scale
//     handles, performs ray-based axis picking, computes constrained drag
//     deltas with optional snapping, and renders into [*core.Drawing] via
//     [DrawGizmo].
//   - Render statistics (#288) & mobile GPU budgets (#289): [RenderStats]
//     captures exact per-frame culling, batching, geometry, state-switch, and
//     memory totals; [CheckBudget] validates frames against [MobileBudget],
//     [DesktopBudget], or custom [Budget] limits.
//
// # Mesh Buffers (#112)
//
// [Mesh] holds triangle geometry (positions, optional normals and UVs, optional indices) on the CPU. [UploadMesh] validates it and uploads it into the vertex and index buffers of NSGo's GPU view ([gpu.Device], nsgo-dev/nsgo#1059) as a [GPUMesh], whose buffers belong to a [core.Scope]. Every mesh shares one interleaved vertex layout, [MeshVertexLayout], so one pipeline draws any of them, and [GPUMesh.Draw] binds the buffers and issues the draw inside a [gpu.RenderPass]. The index width is chosen per mesh (uint16 below 0xFFFF vertices, otherwise uint32), which every GPU view backend accepts, WebGL 2 included.
//
// # Textures and Samplers (#114)
//
// [UploadTexture] converts any [image.Image] into a straight-alpha RGBA8 [PixelBuffer] ([PixelBufferFromImage]) and uploads it as a scope-owned [GPUTexture] on [gpu.Device]; [UploadTexturePixels] uploads raw RGBA8, RG8, or R8 pixel buffers directly with optional row padding. Setting [TextureUpload.SRGB] uses [gpu.FormatRGBA8UnormSRGB] for color maps while keeping linear formats for data maps; setting [TextureUpload.Mipmaps] builds a CPU area-weighted box mipmap chain (in linear light when sRGB) and uploads every level via [gpu.Device.WriteTextureMip], so mipmapping works on every GPU backend and survives device loss when Retained is set. [NewGPUSampler] validates filtering, wrapping, LOD bounds, and WebGPU anisotropy constraints from [SamplerOptions] (with presets such as [SamplerLinearRepeat] and [SamplerNearestClamp]); [TextureBindingLayout] and [BindTexture] bind a texture and sampler to a shader group.
//
// # Budgeted Async Uploads (#387)
//
// [Uploader] streams large assets without frame hitches: [Uploader.QueueMesh], [Uploader.QueueTexture], [Uploader.QueueTexturePixels], [Uploader.QueueRawBuffer] and [Uploader.QueueRawTexture] run the caller's decode function and the CPU packing (mesh validation and interleaving, image conversion, mip generation) on background workers, and [Uploader.Step], called once per frame on the engine goroutine, writes the results to the [gpu.Device] in chunks without exceeding a per-frame byte budget. Streamed meshes and textures are the same [GPUMesh] and [GPUTexture] that [UploadMesh] and [UploadTexture] return, uploads run in [UploadPriority] order, and each [UploadHandle] reports progress and can cancel its upload.
//
// # Effects (#281)
//
//   - Billboards: [Billboard] builds a quad facing a [CameraBasis] in view-plane, view-point or axis-locked mode, [BillboardQuad.DrawItem] feeds it to transparent sorting, and [BillboardRenderer] fills billboards back to front into [*core.Drawing] with near-plane clipping.
//   - Ribbon trails: [Trail] records world positions and builds a triangle strip whose edges run along tangent × toCamera, so the ribbon faces the viewer at every angle, with width and color from the particles package's curves.
//
// # GPU Submission Integration
//
// [CameraBuffer] (#115) keeps a [scene3d.Camera]'s view, projection and eye point in a uniform buffer on an NSGo gpu.Device and binds it at [CameraGroup], with [CameraWGSL] declaring the matching block for 3D shaders.
//
// [GPULightBuffer] (#116) keeps a scene's [LightRig] (ambient fill, up to [MaxDirectionalLights] directional lights, and up to [MaxPointLights] point lights with glTF KHR_lights_punctual smooth range windowing) in a uniform buffer bound at [LightGroup], with [LightWGSL] providing the matching block and glTF 2.0 metallic-roughness Cook-Torrance GGX PBR shading functions, [LitPBRWGSL] and [NewLitPBRPipeline] offering a complete lit PBR shader pipeline, and [EvaluatePBR] / [ShadePoint] providing the matching CPU reference evaluator.
//
// [GPUFogBuffer] (#254) keeps a [Fog] (linear, exponential or exponential-squared distance fog, analytical exponential height fog, or both) in a uniform buffer. [NewLitPBRFogPipeline] draws [LitPBRWGSL]'s lighting with fog with the buffer at [FogGroup], and [NewMaterialPipeline] does the same for materials when [MaterialPipelineOptions].FogLayout is set, with the buffer at [MaterialFogGroup]. [FogWGSL] holds the shader functions for custom shaders, and [Fog.Apply] with [FogCameraPos] is the matching CPU reference.
//
// [SkyboxPipeline] (#253) draws a [GPUCubemap] as a background behind opaque geometry, and [GPUIBLBuffer] holds an environment's [IrradianceSH] diffuse irradiance and [PrefilterEnvironment] specular cubemap for image-based lighting. [NewLitPBRIBLPipeline] adds that lighting to [LitPBRWGSL]'s with the buffer at [IBLGroup], [IBLWGSL] holds the shader functions for custom shaders, and [EvaluateIBL] is the matching CPU reference.
//
// The CPU culling, LOD, sorting, instancing, gizmo, debug, statistics, and budget pipeline stays deterministic, pure Go, and independent of the GPU view, so it runs the same in headless tests and servers; the camera, light, mesh, texture, and sampler helpers are the parts that talk to [gpu.Device].
package render3d
