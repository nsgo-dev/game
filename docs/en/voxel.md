# voxel

`game/voxel` provides chunked 3D voxel worlds for sandbox games: dense cubic chunks, greedy face meshing into [render3d](render3d.md) buffers, cross-chunk neighbor culling and ambient occlusion, minimal dirty-chunk tracking on edit, 3D DDA voxel raycasting, and deterministic versioned YAML saves.

```go
import "github.com/nsgo-dev/game/packages/game/voxel"
```

## Blocks and registry

Register block types in a `Registry` to configure transparency, solidity, and `MaterialID` sub-mesh grouping. Block ID `0` (`voxel.Air`) is reserved for empty space; unregistered non-zero IDs default to opaque, solid, and `MaterialID: 0`:

```go
reg := voxel.NewRegistry()
stone := reg.MustRegister(voxel.BlockDef{Name: "stone", MaterialID: 0})
grass := reg.MustRegister(voxel.BlockDef{Name: "grass", MaterialID: 1})
glass := reg.MustRegister(voxel.BlockDef{Name: "glass", Transparent: true, MaterialID: 2})
water := reg.MustRegister(voxel.BlockDef{Name: "water", Transparent: true, NonSolid: true, MaterialID: 3})
```

Face culling follows standard opaque/transparent rules:
- Opaque blocks emit faces toward `Air` and transparent blocks.
- Transparent blocks emit faces toward `Air` and *different* transparent block IDs, culling faces pressed against opaque blocks or identical transparent neighbors.

## Chunked worlds and dirty tracking

Create a `World` with a power-of-two `ChunkSize` (`4` to `64`, default `16` for 16×16×16 chunks) and a world-space `VoxelSize` (default `1`):

```go
w, err := voxel.NewWorld(voxel.WorldConfig{
	ChunkSize: 16,
	VoxelSize: 1,
	Registry:  reg,
})
if err != nil {
	return err
}

w.FillBox(voxel.BlockCoord{X: 0, Y: 0, Z: 0}, voxel.BlockCoord{X: 31, Y: 3, Z: 31}, stone)
w.SetBlock(voxel.BlockCoord{X: 8, Y: 4, Z: 8}, grass)
```

`SetBlock` marks only the edited block's chunk dirty. An adjacent existing chunk is marked dirty only when the edit lies on a shared boundary and changes neighbor face visibility or cross-chunk ambient occlusion (for example, carving an opaque block into air next to a neighboring block). Replacing one opaque block with another opaque block never dirties neighboring chunks.

Call `w.PruneEmptyChunks()` after remeshing if you want to release chunks that have been emptied back to all `Air`.

## Greedy meshing

Keep a reusable `Mesher` to avoid temporary buffer allocations across frames. `RemeshDirty` meshes only dirty chunks in deterministic `CompareChunkCoords` order and clears their dirty flags:

```go
mesher := voxel.NewMesher()
opts := voxel.MeshOptions{
	AmbientOcclusion: true, // per-vertex AO in ChunkMesh.AO + diagonal flip
	UVMode:           voxel.UVWorld,
}

for _, cm := range mesher.RemeshDirty(w, opts) {
	if cm.Empty() {
		releaseChunkGPU(cm.Coord)
		continue
	}
	// cm.Mesh is a CCW render3d.Mesh ready for render3d.UploadMesh.
	// cm.SubMeshes partitions cm.Mesh.Indices by BlockDef.MaterialID.
	uploadChunkGPU(cm.Coord, cm.Origin, cm.Mesh, cm.SubMeshes)
}
```

Key options in `MeshOptions`:
- `AmbientOcclusion`: computes per-vertex AO levels (`0..3`) in `ChunkMesh.AO` (convert with `AOFactor`) across chunk boundaries, flips quad diagonals to avoid interpolation seams, and merges faces only when all four AO corners match.
- `WorldSpace`: emits vertices offset by `ChunkMesh.Origin` instead of chunk-local `[0, ChunkSize*VoxelSize]`.
- `UVMode`: `UVWorld` (default) scales UVs by merged quad width and height for seamless texture tiling; `UVUnit` assigns `[0, 1]` per quad.
- `DisableGreedy` / `IgnoreNeighbors`: disable greedy rectangle merging or cross-chunk culling for debugging or comparisons.

## Voxel raycasting and editing

Use `World.Raycast` (3D Amanatides & Woo DDA traversal) to pick blocks and place neighbors against the struck face:

```go
if hit, ok := w.Raycast(ray, 64); ok {
	// Break the struck block:
	// w.SetBlock(hit.Block, voxel.Air)

	// Or place a new block on the struck face:
	w.SetBlock(hit.Adjacent, stone)
}
```

By default `Raycast` skips `NonSolid` blocks (such as water); pass `RaycastOptions{IncludeNonSolid: true}` to `RaycastWith` to hit any non-air voxel.

## Saving and loading YAML

Worlds serialize to deterministic, versioned YAML text with run-length encoded chunks (`"id*count"` or `"id"` in `(y, z, x)` order), sorted palette entries, and sorted chunk coordinates. Unknown YAML fields and trailing documents are refused, and errors are returned as `*loaderr.Error` values with file, line, column, field path, and fix hints:

```go
data, err := voxel.SaveYAML(w, "Overworld")
if err != nil {
	return err
}

loaded, name, err := voxel.LoadYAML(data)
if err != nil {
	return err
}
_ = name
// Every loaded chunk starts dirty so RemeshDirty builds initial meshes:
initialMeshes := mesher.RemeshDirty(loaded, opts)
_ = initialMeshes
```

Saved files start with the schema comment `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/voxel.v1.json`; see [Schemas for YAML files](schemas.md).
