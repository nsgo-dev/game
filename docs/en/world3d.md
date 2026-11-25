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
- **Text definition**: `ParseTerrain` / `EncodeTerrain` read and write version 1 YAML (`https://game.nsgo.dev/schemas/terrain.v1.json`) declaring `heightmap`, `cellSize`, `heightScale`, `origin`, `chunkSize` (default `64` cells per LOD chunk), `splatMaps`, `heightBlendContrast`, `triplanar`, and 1–8 `layers` (with per-layer `uvScale`, PBR maps/factors, and `heightBias`). Unknown fields, duplicate layer names, and mismatched splat-map counts are refused with `*loaderr.Error`. Call `doc.BuildHeightmap(imgs)` / `doc.BuildLOD(hm)` (or `world3d.LoadTerrainFS(fsys, path)`) to build the runtime `*Heightmap`, attached `*TerrainSplatMap`, and `*TerrainLOD` directly from YAML. `game check` validates every `*.terrain.yaml` file and its referenced heightmap, splat maps, and layer textures.

## Multi-level 3D NavMesh

Build a 3D `NavMesh` from convex walkable polygons (ramps, bridges, and stacked floors). `BuildNavMesh` filters polygons steeper than `maxSlopeRad`, links shared edges in 3D, and runs portal A* with 3D string-pulling in `FindPath`:

```go
nav := world3d.BuildNavMesh(walkablePolys, math3d.DegToRad(45))
if path, ok := nav.FindPath(startPos, goalPos); ok {
	follow3DWaypoints(path)
}
```

Use `nav.FindPolygon(p)` to locate the closest walkable polygon vertically within `VerticalTolerance` (supporting stacked floors and overpasses), or `Contains(p)` and `NearestPolygon(p)` for membership and closest-surface queries.

### Polygon areas, runtime flags, and query filters

Tag 3D polygons at build time with `BuildNavMeshWithAreas` using `NavPolygonInput{Vertices, Area, Flags}`. `world3d.NavArea`, `world3d.NavFlags`, and `world3d.NavFilter` alias the types in [ai](ai.md), so the same filter and area/flag constants work seamlessly across 2D and 3D navigation meshes:

```go
nav := world3d.BuildNavMeshWithAreas([]world3d.NavPolygonInput{
	{Vertices: grassQuad, Area: world3d.NavAreaGrass},
	{Vertices: roadQuad, Area: world3d.NavAreaRoad},
	{Vertices: waterQuad, Area: world3d.NavAreaWater, Flags: world3d.NavFlagSwim},
	{Vertices: gateQuad, Area: world3d.NavAreaDoor, Flags: world3d.NavFlagWalk | world3d.NavFlagDoor},
}, math3d.DegToRad(45))

filter := world3d.DefaultNavFilter()
filter.SetAreaCost(world3d.NavAreaRoad, 0.5)
filter.ExcludeFlags |= world3d.NavFlagSwim

path, ok := nav.FindPathWithFilter(startPos, goalPos, filter)

// Toggle a gate or drawbridge polygon at runtime without rebuilding:
nav.AddPolygonFlags(gatePolyIndex, world3d.NavFlagDisabled)
detour, ok := nav.FindPathWithFilter(startPos, goalPos, filter)
nav.ClearPolygonFlags(gatePolyIndex, world3d.NavFlagDisabled)
_, _, _ = path, detour, ok
```

- **Default compatibility**: `BuildNavMesh` assigns `NavAreaDefault` and `NavFlagWalk` to every walkable `NavPoly`, and `FindPath` delegates to `FindPathWithFilter` with `DefaultNavFilter()`.
- **Admissible 3D weighted portal A\***: traversal inside each polygon is weighted by `filter.AreaCost(poly.Area)` with an admissible 3D heuristic scaled by the minimum allowed area cost across the mesh, followed by exact 3D funnel smoothing across ramps and stacked levels.
- **Runtime mutation & filtered queries**: inspect or update polygons in O(1) via `PolygonCount`, `PolygonArea`, `SetPolygonArea`, `PolygonFlags`, `SetPolygonFlags`, `AddPolygonFlags`, and `ClearPolygonFlags`, and run filtered lookups with `FindPolygonWithFilter`, `ContainsWithFilter`, and `NearestPolygonWithFilter`.

For unreachable targets or frame-budgeted multi-agent queries, pass `ai.PathOptions{MaxExpansions, AllowPartial}` to `nav.FindPathWithOptions(startPos, goalPos, opts)` (returning a `NavPathResult` with `Partial`, `Exhausted`, and `Expansions`), or create a deterministic multi-frame `*world3d.NavMeshSearch` via `nav.StartSearch(startPos, goalPos, opts)`:

```go
search := nav.StartSearch(startPos, goalPos, ai.PathOptions{
	MaxExpansions: 128,
	AllowPartial:  true,
})
// Step directly, or schedule alongside 2D searches in an ai.PathQueue:
queue.Submit(search, 0, func(_ ai.PathRequestID, _ ai.SearchStatus) {
	res := search.Result()
	if res.Found {
		follow3DWaypoints(res.Path) // res.Partial is true if goal was unreachable or budgeted out
	}
})
```

Stepping `NavMeshSearch` across any positive per-step budget returns the exact same 3D surface path as `FindPathWithOptions`, and partial paths end at the closest reachable 3D point on the explored polygons.

## Modular 3D `GridMap`

Place reusable 3D mesh tiles on an integer `GridCoord{X, Y, Z}` lattice with 24 axis-aligned cube orientations (`GridOrientationMat4`), fast collision box extraction, chunk-culled raycasting (chunks are sorted along the ray and the occupied cells of each are tested), and frustum-culled instanced batches:

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

`game check` validates every `*.gridmap.yaml` file, checks that its `meshLibrary` exists and loads (for `.gltf`), and verifies that every `boxes[*].tile` and `cells[*].tile` name exists in that library.

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
