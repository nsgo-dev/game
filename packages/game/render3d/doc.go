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
// # GPU Submission Integration
//
// [CameraBuffer] (#115) keeps a [scene3d.Camera]'s view, projection and eye point in a uniform buffer on an NSGo gpu.Device and binds it at [CameraGroup], with [CameraWGSL] declaring the matching block for 3D shaders.
//
// The CPU culling, LOD, sorting, instancing, gizmo, debug, statistics, and budget pipeline stays deterministic, pure Go, and independent of the GPU view, so it runs the same in headless tests and servers; the camera and mesh buffers are the parts that talk to [gpu.Device].
package render3d
