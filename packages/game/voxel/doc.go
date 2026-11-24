// Package voxel provides chunked 3D voxel worlds with greedy meshing,
// cross-chunk face culling, incremental dirty-chunk remeshing, DDA voxel
// raycasting, and deterministic versioned YAML persistence.
//
// Coordinates follow [github.com/nsgo-dev/game/packages/game/math3d] and
// [github.com/nsgo-dev/game/packages/game/render3d]: right-handed Y-up space
// (+X right, +Y up, -Z forward), with counter-clockwise front-facing triangles.
// Voxel (x, y, z) occupies the world-space cube from (x, y, z)*VoxelSize to
// (x+1, y+1, z+1)*VoxelSize.
//
// Editing a block with [World.SetBlock] marks only its chunk dirty, plus an
// adjacent existing chunk only when editing on a shared boundary changes face
// visibility or ambient occlusion. Call [Mesher.RemeshDirty] (or
// [World.RemeshDirty]) each frame to rebuild only affected chunks into
// [render3d.Mesh] buffers ready for [render3d.UploadMesh].
package voxel
