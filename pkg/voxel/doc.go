// Package voxel provides chunked 3D voxel worlds with greedy meshing,
// cross-chunk face culling, incremental per-voxel sky and block light
// propagation, incremental dirty-chunk remeshing, DDA voxel raycasting,
// and deterministic versioned YAML persistence.
//
// Coordinates follow [github.com/nsgo-dev/game/pkg/math3d] and
// [github.com/nsgo-dev/game/pkg/render3d]: right-handed Y-up space
// (+X right, +Y up, -Z forward), with counter-clockwise front-facing triangles.
// Voxel (x, y, z) occupies the world-space cube from (x, y, z)*VoxelSize to
// (x+1, y+1, z+1)*VoxelSize.
//
// Editing a block with [World.SetBlock] updates sky and block light (0..15
// each) incrementally across chunk boundaries, marks the edited chunk dirty,
// and marks adjacent existing chunks dirty only when face visibility, ambient
// occlusion, or lighting changes across their boundaries. Call
// [Mesher.RemeshDirty] (or [World.RemeshDirty]) each frame to rebuild only
// affected chunks into [render3d.Mesh] (or [ChunkMesh.ColoredMesh] for
// vertex-shaded [render3d.ColoredMesh]) buffers.
package voxel
