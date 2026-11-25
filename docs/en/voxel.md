# voxel

`game/voxel` provides chunked 3D voxel worlds for sandbox games: dense cubic chunks, per-voxel sky and block light propagation (0..15) across chunk borders, greedy face meshing into [render3d](render3d.md) buffers, cross-chunk neighbor culling and ambient occlusion, minimal dirty-chunk tracking on edit, 3D DDA voxel raycasting, direct solid-block collision for [physics3d](physics3d.md) bodies and characters (plus an axis-separated AABB move-and-slide character mover), and deterministic versioned YAML saves.

```go
import "github.com/nsgo-dev/game/packages/game/voxel"
```

## Blocks and registry

Register block types in a `Registry` to configure transparency, solidity, `MaterialID` sub-mesh grouping, `Emissive` block light level (`0..15`), and `LightOpacity` attenuation (`0..15` for transparent blocks). Block ID `0` (`voxel.Air`) is reserved for empty space; unregistered non-zero IDs default to opaque (`LightOpacity: 15`), solid, non-emissive, and `MaterialID: 0`:

```go
reg := voxel.NewRegistry()
stone := reg.MustRegister(voxel.BlockDef{Name: "stone", MaterialID: 0})
grass := reg.MustRegister(voxel.BlockDef{Name: "grass", MaterialID: 1})
glass := reg.MustRegister(voxel.BlockDef{Name: "glass", Transparent: true, MaterialID: 2})
water := reg.MustRegister(voxel.BlockDef{Name: "water", Transparent: true, NonSolid: true, LightOpacity: 2, MaterialID: 3})
torch := reg.MustRegister(voxel.BlockDef{Name: "torch", Transparent: true, NonSolid: true, Emissive: 14, MaterialID: 4})
```

Face culling follows standard opaque/transparent rules:
- Opaque blocks emit faces toward `Air` and transparent blocks.
- Transparent blocks emit faces toward `Air` and *different* transparent block IDs, culling faces pressed against opaque blocks or identical transparent neighbors.

## Sky and block light propagation

Each voxel stores two 4-bit light channels (`0..15`, packed into one byte per voxel):

- **Sky light**: unobstructed columns directly exposed to the sky carry level `15` straight down through `Air` and zero-opacity transparent blocks (such as glass). Hitting a light-attenuating block (`LightOpacity > 0`, or an opaque block with effective opacity `15`) shadows the column below, and sky light diffuses horizontally and vertically from brighter neighbors by `max(1, LightOpacity)` per voxel.
- **Block light**: blocks with `Emissive > 0` (such as torches or glowstone) seed block light at their emission level and flood fill outward across chunk boundaries, attenuating by `max(1, LightOpacity)` per voxel.

`World.SetBlock` updates both light channels incrementally using flood-fill addition and removal BFS across chunk borders—matching `World.RecomputeLight()` identically while marking only chunks whose mesh geometry or border lighting actually changed as dirty. Query light levels at any world coordinate with `w.SkyLight(pos)`, `w.BlockLight(pos)`, or `w.Light(pos)` (or locally on a chunk via `ch.SkyLight`, `ch.BlockLight`, `ch.Light`).

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

`SetBlock` marks the edited block's chunk dirty, and marks adjacent existing chunks dirty only when the edit changes neighbor face visibility, cross-chunk ambient occlusion, or cross-chunk sky/block lighting. Replacing one opaque non-emissive block with another never dirties neighboring chunks.

Call `w.PruneEmptyChunks()` after remeshing if you want to release chunks that have been emptied back to all `Air` (non-default light passing through a pruned chunk is preserved).

## Greedy meshing

Keep a reusable `Mesher` to avoid temporary buffer allocations across frames. `RemeshDirty` meshes only dirty chunks in deterministic `CompareChunkCoords` order and clears their dirty flags:

```go
mesher := voxel.NewMesher()
opts := voxel.MeshOptions{
	AmbientOcclusion: true, // per-vertex AO in ChunkMesh.AO + diagonal flip
	Lighting:         true, // per-vertex SkyLight, BlockLight, and linear RGBA Colors
	SmoothLighting:   true, // 4-voxel smooth corner interpolation across chunk borders
	UVMode:           voxel.UVWorld,
}

for _, cm := range mesher.RemeshDirty(w, opts) {
	if cm.Empty() {
		releaseChunkGPU(cm.Coord)
		continue
	}
	// cm.Mesh is a CCW render3d.Mesh ready for render3d.UploadMesh.
	// cm.ColoredMesh() returns a render3d.ColoredMesh with linear RGBA light+AO
	// vertex colors ready for render3d.UploadColoredMesh and colored render3d pipelines.
	// cm.SubMeshes partitions cm.Mesh.Indices by BlockDef.MaterialID.
	uploadChunkGPU(cm.Coord, cm.Origin, cm.ColoredMesh(), cm.SubMeshes)
}
```

Key options in `MeshOptions`:
- `AmbientOcclusion`: computes per-vertex AO levels (`0..3`) in `ChunkMesh.AO` (convert with `AOFactor`) across chunk boundaries, flips quad diagonals to avoid interpolation seams, and merges faces only when all four AO corners match.
- `Lighting` / `SmoothLighting` / `LightStyle`: populates per-vertex `ChunkMesh.SkyLight` (`0..15`), `ChunkMesh.BlockLight` (`0..15`), and linear RGBA `ChunkMesh.Colors` (combining `LightFactor` of sky/block light tinted by `LightStyle` with `AOFactor` when AO is enabled). Greedy meshing merges faces only when lighting matches so there are no chunk-border seams. Call `cm.ColoredMesh()` to hand the mesh straight to `render3d.UploadColoredMesh` (compatible with `render3d.NewUnlitPipeline` and `render3d.NewMaterialPipeline` with `VertexColors: true`).
- `WorldSpace`: emits vertices offset by `ChunkMesh.Origin` instead of chunk-local `[0, ChunkSize*VoxelSize]`.
- `UVMode`: `UVWorld` (default) scales UVs by merged quad width and height for seamless texture tiling; `UVUnit` assigns `[0, 1]` per quad.
- `DisableGreedy` / `IgnoreNeighbors`: disable greedy rectangle merging or cross-chunk culling/lighting for debugging or comparisons.

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

## Voxel collision and character movement

Use `w.Collider()` (or `voxel.NewCollider(w)`) to attach a voxel world directly to [physics3d](physics3d.md) without generating or rebuilding a triangle mesh:

```go
phys := physics3d.NewWorld()
phys.AddVoxelCollider(w.Collider())

crate := phys.AddBody(physics3d.NewDynamicBody(
	physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.4, Y: 0.4, Z: 0.4}}),
	physics3d.Vec3{X: 4.5, Y: 8.0, Z: 4.5},
	1.0,
))

cc := physics3d.NewCharacterController(physics3d.Vec3{X: 1.5, Y: 4.0, Z: 1.5}, 0.3, 1.8)
cc.StepHeight = w.VoxelSize() + 0.05
```

- **Zero rebuild on edit**: overlap, raycast, and sweep queries read `World.IsSolid` directly (skipping `Air` and `NonSolid` blocks such as water), so `SetBlock` and `Clear` take effect immediately and automatically wake sleeping dynamic bodies near edited voxels so resting crates fall when their support block is removed.
- **No internal seam snagging**: occluded faces shared between adjacent solid blocks are filtered out so sliding boxes, spheres, capsules, and `CharacterController` glide smoothly across flat voxel floors and step up single-block ledges.

For games that only need simple box characters against voxel terrain without a full `physics3d.World`, use `MoveAndSlideAABB` or `CharacterMover`:

```go
mover := voxel.NewCharacterMover(math3d.Vec3{X: 1.5, Y: 4.0, Z: 1.5}, 0.3, 1.8)
mover.StepHeight = w.VoxelSize()
res := mover.Move(w, math3d.Vec3{X: 4.0}, dt) // axis-separated Y -> X -> Z sweep + 1-block step-up
_ = res
```

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
