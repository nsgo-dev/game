# world3d

`game/world3d` provides 3D world-building primitives built on [math3d](math3d.md): heightmap terrain, multi-level 3D navigation meshes, modular 3D mesh `GridMap`s, procedural scattered foliage with frustum/distance culling, and floating-origin chunk streaming.

```go
import "github.com/nsgo-dev/game/packages/game/world3d"
```

## Heightmap terrain (`Heightmap`)

Load a grayscale heightmap image (`LoadHeightmapGray`) or populate `Heights` directly, then sample bilinear heights and normals, raycast against triangles, and build chunked `TerrainMesh` geometry:

```go
hm := world3d.LoadHeightmapGray(grayImg, 2.0, 40.0)

y, ok := hm.HeightAt(player.X, player.Z)
normal := hm.NormalAt(player.X, player.Z)
hit, hitOK := hm.Raycast(ray, 500)

// Build a 32x32 cell mesh chunk for GPU upload:
chunkMesh := hm.BuildMesh(0, 0, 32, 32)
_, _, _, _, _ = y, ok, normal, hit, chunkMesh
_ = hitOK
```

## Multi-level 3D NavMesh

Build a 3D `NavMesh` from convex walkable polygons (ramps, bridges, and stacked floors). `BuildNavMesh` filters polygons steeper than `maxSlopeRad`, links shared edges in 3D, and runs portal A* with 3D string-pulling in `FindPath`:

```go
nav := world3d.BuildNavMesh(walkablePolys, math3d.DegToRad(45))
if path, ok := nav.FindPath(startPos, goalPos); ok {
	follow3DWaypoints(path)
}
```

Use `nav.FindPolygon(p)` to locate the closest walkable polygon vertically within `VerticalTolerance` (supporting stacked floors and overpasses).

## Modular 3D `GridMap`

Place reusable 3D mesh tiles on an integer `GridCoord{X, Y, Z}` lattice with 24 axis-aligned cube orientations (`GridOrientationMat4`), fast collision box extraction, DDA raycasting, and frustum-culled instanced batches:

```go
lib, err := world3d.LoadMeshLibraryFromGLTF(gltfBytes) // or world3d.NewMeshLibrary()
if err != nil {
	return err
}
grid := world3d.NewGridMap(math3d.Vec3{X: 2, Y: 2, Z: 2}, lib)
floorDef, _ := lib.ByName("stone_floor")
grid.FillBox(world3d.GridCoord{X: 0, Y: 0, Z: 0}, world3d.GridCoord{X: 15, Y: 0, Z: 15}, floorDef.ID, 0)

batches = grid.BuildInstances(&frustum, batches[:0])
colliders := grid.CollisionBoxesIn(playerAABB)
hit, ok := grid.Raycast(ray, 100)
_, _, _, _ = batches, colliders, hit, ok
```

## Procedural foliage culling (`FoliageField`)

Deterministically scatter grass, rocks, or trees across a `Heightmap` constrained by slope, height, and an optional `DensityMap`, then collect frustum- and distance-culled instances scaled by a `QualityProfile`:

```go
field := world3d.Scatter(hm, world3d.FoliageLayer{
	Name:            "grass",
	MeshID:          grassMeshID,
	MaterialID:      grassMatID,
	Density:         1.5,
	MinScale:        0.8,
	MaxScale:        1.3,
	AlignToNormal:   0.6,
	MaxSlopeDeg:     35,
	BaseMaxDistance: 80,
	Seed:            42,
}, hm.Bounds())

quality := world3d.DefaultQualityProfile(world3d.QualityHigh)
visible = field.CollectVisible(camPos, frustum, quality, visible[:0])
```

Each returned `FoliageInstance` provides its world `Transform` and a smooth distance `Fade` factor in `[0, 1]`.

## Floating-origin chunk streaming

Prevent `float32` jitter far from the world origin with `FloatingOrigin` (`WorldPos64` in `float64`) and load/unload surrounding world chunks asynchronously with `ChunkStreamer[T]`:

```go
origin := world3d.NewFloatingOrigin(512) // rebase when player exceeds 512m from local origin
streamer := world3d.NewChunkStreamer(world3d.ChunkStreamerConfig[*ChunkData]{
	ChunkSize:               64,
	LoadRadius:              3,
	UnloadRadius:            5,
	MaxIntegrationsPerFrame: 2,
	Workers:                 2,
	Loader:                  loadChunkAsync,
	OnLoad:                  attachChunk,
	OnUnload:                releaseChunk,
})
defer streamer.Close()

// Each frame:
if rebased, shift := origin.Update(playerLocalPos); rebased {
	rebaseSceneObjects(shift) // shift Mat4 transforms with world3d.RebaseMat4(m, shift)
}
streamer.Update(origin.LocalToWorld(playerLocalPos))
```
