# world3d

`game/world3d` provides 3D world-building primitives built on [math3d](math3d.md): heightmap terrain, modular 3D mesh `GridMap`s, procedural scattered foliage with frustum/distance culling, and floating-origin chunk streaming. [pathfind3d](pathfind3d.md) builds 3D navigation meshes and agents on its chunk coordinates.

```go
import "github.com/nsgo-dev/game/pkg/world3d"
```

## Heightmap terrain (`Heightmap`)

Load a 16-bit (or 8-bit) grayscale PNG heightmap (`LoadHeightmapGray`, `DecodeHeightmapPNG`) or create one with `NewHeightmap`, then sample bilinear heights and normals, raycast against triangles, build chunked `TerrainMesh` geometry, and write back lossless 16-bit grayscale PNGs (`ToGray16`, `EncodePNG`, `EncodeHeightmapPNG`) without 8-bit slope banding:

```go
hm := world3d.LoadHeightmapGray(grayImg, 2.0, 40.0)

y, ok := hm.HeightAt(player.X, player.Z)
normal := hm.NormalAt(player.X, player.Z)
hit, hitOK := hm.Raycast(ray, 500)

// Build a 32x32 cell mesh chunk for GPU upload:
chunkMesh := hm.BuildMesh(0, 0, 32, 32)
// Or bridge directly to a physics3d.HeightfieldCollider:
hfCollider := hm.Collider()
_, _, _, _, _, _ = y, ok, normal, hit, chunkMesh, hfCollider
_ = hitOK
```

## Terrain level of detail (`TerrainLOD`)

Large terrains can contain tens of millions of triangles at full resolution. `TerrainLOD` partitions a `Heightmap` into chunks (default 64×64 cells), selects per-chunk power-of-two vertex strides (`1, 2, 4, ...`) from camera-to-AABB distance with hysteresis, applies bias from the game-wide [quality](quality.md) preset (`TerrainLODBiasFor`), culls chunk AABBs against the camera frustum, and caches crack-free stitched `TerrainMesh` geometry per chunk:

```go
terrain := world3d.NewTerrainLOD(hm, world3d.TerrainLODConfig{
	ChunkCells: 64,
	Hysteresis: 0.10, // 10% deadband around each LOD threshold
	LODBias:    world3d.TerrainLODBiasFor(sel.Level()),
})

// Each frame (or use UpdateWithOrigin with a *FloatingOrigin):
visibleChunks := terrain.Update(camPos, &frustum)
for _, ch := range visibleChunks {
	if ch.Changed {
		uploadChunkMesh(ch.Coord, ch.Mesh, ch.Revision)
	}
	drawTerrainChunk(ch.Coord, ch.Transform)
}
```

- **Crack-free seams**: whenever a chunk borders a coarser neighbor along North, South, West, or East, `BuildMeshLOD` re-triangulates that border strip to connect directly to the neighbor's coarse edge vertices, avoiding both height cracks and T-junctions at every LOD pair (including partial heightmap border chunks).
- **Quality presets**: call `terrain.SetQualityLevel(level)` (or `terrain.SetLODBias(bias)`) when the player's quality tier changes. `TerrainLODBiasFor` returns `+1.0` for `quality.Low`, `+0.5` for `quality.Medium`, and `0.0` for `quality.High`, scaling distance thresholds by `2^(-bias)`.
- **Floating origin & streaming**: set `WorldOrigin` and call `UpdateWithOrigin(camLocal, &frustumLocal, origin)` so LOD distances stay invariant in `float64` world space across rebases while returned `Bounds` and `Transform` track local space without rebuilding cached meshes. Set `StreamManaged: true` and hook `SetChunkActive` to `ChunkStreamerConfig.OnLoad` / `OnUnload` (with `ChunkSize: terrain.ChunkWorldSize()`) to stream chunks on demand.
- **Direct meshing**: `hm.BuildMeshLOD(col, row, cols, rows, lod, neighbors)` and `hm.ChunkBounds(col, row, cols, rows)` are read-only and safe to call directly from background streaming workers.

## Terrain material layers and splat maps (`TerrainSplatMap`, `TerrainDoc`)

Blend up to 8 PBR material layers across a heightmap using 1 or 2 RGBA weight (splat) maps (channels R, G, B, A of map 0 drive layers 0..3; map 1 drives layers 4..7), author the layers in a versioned YAML terrain definition (`*.terrain.yaml`), query the surface underfoot for gameplay via `Heightmap.LayerAt(x, z)`, and render the terrain with [render3d](render3d.md)'s `TerrainMaterial` / `GPUTerrainMaterial`:

```go
doc, err := world3d.ParseTerrain(yamlBytes) // strict version 1 YAML (EncodeTerrain writes canonical form)
if err != nil {
	return err
}

splat, err := world3d.LoadTerrainSplatMap(doc.LayerInfos(), splatImg0) // pass splatImg0, splatImg1 for 5..8 layers
if err != nil {
	return err
}
splat.HeightBlendContrast = doc.HeightBlendContrast
hm.Splat = splat

// Zero-allocation gameplay query for footsteps, particles, and traction:
sample, inBounds := hm.LayerAt(player.X, player.Z)
playFootstep(sample.Name, sample.Dominant, sample.Weight(sample.Dominant))
_ = inBounds
```

- **Bilinear sampling & clamping**: `TerrainSplatMap.SampleUV` and `Heightmap.LayerAt` use texel-center bilinear filtering with edge clamping, matching GPU linear clamp-to-edge splat sampling. Outside the heightmap footprint, `Heightmap.LayerAt` returns `ok = false` alongside the clamped edge sample.
- **Height-based blending**: attach per-layer grayscale height maps (`LoadTerrainHeightLayer`) and set `HeightBlendContrast` in `(0, 1]` to sharpen layer transitions (such as rock protruding through dirt) using the same algebraic `BlendTerrainWeights` formula as `render3d`'s terrain shader.
- **Text definition**: `ParseTerrain` / `EncodeTerrain` read and write version 1 YAML (`https://game.nsgo.dev/schemas/terrain.v1.json`) declaring `size` (`[cols, rows]`), `heightmap` (16-bit grayscale PNG), `cellSize`, `heightScale`, `origin`, `chunkSize` (default `64` cells per LOD chunk), either packed RGBA `splatMaps` or per-layer grayscale `weightMap` PNGs (`PackLayerWeightMaps`, `LoadTerrainSplatMapFromWeights`), `heightBlendContrast`, `triplanar`, 1–8 `layers` (each with optional `material` resource path, `weightMap`, `uvScale`, PBR maps/factors, and `heightBias`), and optional embedded `foliage` scatter layers with grayscale `densityMap` PNGs (`DensityMap.EncodePNG`, `Heightmap.FoliageDensityAt`, `TerrainDoc.ScatterFoliage`). Unknown fields, duplicate layer names, mixing `splatMaps` with per-layer `weightMap` paths, size mismatches, and mismatched splat-map counts are refused with `*loaderr.Error`. Call `doc.BuildHeightmap(imgs)` / `doc.BuildLOD(hm)` (or `world3d.LoadTerrainFS(fsys, path)`) to build the runtime `*Heightmap`, attached `*TerrainSplatMap`, attached foliage density maps, and `*TerrainLOD` directly from YAML. `game check` validates every `*.terrain.yaml` file and its referenced heightmap, splat/weight maps, material resources, layer textures, and foliage meshes and density maps.

## Modular 3D `GridMap`

Place reusable 3D mesh tiles on an integer `GridCoord{X, Y, Z}` lattice with 24 axis-aligned cube orientations (`GridOrientationMat4`, `RotateGridOrientation`, `FindGridOrientation`), fast collision box extraction, chunk-culled raycasting (chunks are sorted along the ray and the occupied cells of each are tested), cell enumeration (`EachCell`), sorted library inspection (`MeshLibrary.Items`), and frustum-culled instanced batches. `LoadMeshLibraryFromGLTF` reads both `.gltf` JSON and binary `.glb` mesh libraries:

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

To populate a [`physics3d.World`](physics3d.md) from a `GridMap`'s `ColliderBoxes`, call `grid.AddCollidersToWorld(world, opts)` (or `grid.BuildStaticBodies(opts)` / `grid.MergedColliderBoxes()`). Adjacent aligned cell boxes are greedily merged along X, then Z, then Y via `physics3d.MergeBoxes`, cutting static body counts and removing internal cell seams while producing identical collision outcomes (set `GridColliderOptions{DisableMerge: true}` to emit one body per cell box).

Grid maps can also be authored and saved as version 1 YAML (`*.gridmap.yaml`, schema `https://game.nsgo.dev/schemas/gridmap.v1.json`) with `origin`, `cellSize`, `meshLibrary`, non-overlapping filled box runs (`boxes`), and individual cells (`cells`), all written in canonical `(y, z, x)` order:

```go
grid, doc, err := world3d.LoadGridMapFS(projectFS, "levels/dungeon.gridmap.yaml")
if err != nil {
	return err
}
exported, err := grid.ToDoc(doc.Name, doc.MeshLibrary)
if err != nil {
	return err
}
yamlBytes, err := world3d.EncodeGridMap(exported) // deterministic, refuses unknown fields on ParseGridMap
_, _ = exported, yamlBytes
```

`game check` validates every `*.gridmap.yaml` file (and every scene object `gridMap` reference), checks that its `meshLibrary` exists and loads (`.gltf` or `.glb`), and verifies that every `boxes[*].tile` and `cells[*].tile` name exists in that library. Scene objects can attach a `.gridmap.yaml` file via `gridMap` for automatic instancing in [scenemodel](scenemodel.md) and interactive painting in the [3D editor](editor.md#3d-gridmap-painting-899).

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

Foliage layers can also be authored in version 1 YAML (`*.foliage.yaml`, schema `https://game.nsgo.dev/schemas/foliage.v1.json`) specifying each layer's `mesh`, optional `material` and `densityMap`, either `density` (instances/m²) or `spacing` (average distance in meters, where `density = 1 / spacing²`), scale range (`minScale`, `maxScale`), `alignToNormal`, slope limits (`minSlopeDeg`, `maxSlopeDeg`), height limits (`minHeight`, `maxHeight`), `baseMaxDistance`, `boundingRadius`, and `seed`:

```go
foliageDoc, err := world3d.LoadFoliageFS(projectFS, "terrain/meadow.foliage.yaml")
if err != nil {
	return err
}
fields, err := foliageDoc.ScatterAll(hm, hm.Bounds(), world3d.FoliageBuildOptions{
	ResolveMesh:     resolveMeshID,
	ResolveMaterial: resolveMaterialID,
	FS:              projectFS,
})
```

`ParseFoliage` refuses unknown fields and contradictory settings (such as setting both `density` and `spacing`), `EncodeFoliage` writes deterministic canonical YAML, and `game check` validates every `*.foliage.yaml` file and its referenced terrain, meshes, materials, and density map images.

`world3d.QualityLevelFor(level)` maps a game-wide [quality](quality.md) level to the foliage tier of the same name, so `DefaultQualityProfile(QualityLevelFor(sel.Level()))` follows the player's preset. `QualityUltra` stays an explicit choice above every preset.

Each returned `FoliageInstance` provides its world `Transform` and a smooth distance `Fade` factor in `[0, 1]`. Set `FoliageLayer.BoundingRadius` to the radius of a sphere around the mesh pivot that encloses the mesh at scale 1: culling and the draw distance then use each instance's scaled sphere and measure from its nearest point, so large trees stay visible while any part of them is in range. Without it, culling assumes a unit-radius mesh and distances are measured from the pivot.

`Heightmap.Raycast` walks only the cells under the ray and never scans the whole height grid.

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
	playerLocalPos = playerLocalPos.Add(shift) // the player moves with everything else
}
streamer.Update(origin.LocalToWorld(playerLocalPos))
```
