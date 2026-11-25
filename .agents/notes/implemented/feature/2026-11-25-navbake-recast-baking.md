# Agent Note: Bake 3D navigation meshes from level geometry

Status: implemented

## Problem

`world3d.NavMesh` paths across ramps and stacked floors, but nothing produced its input: a 3D game had to author convex walkable polygons by hand (nsgo-dev/game#823). Recast, Godot's `NavigationRegion3D` baking and Unity's `NavMeshSurface` all bake from the level's triangles with agent settings, and the baked result must be a reviewable text file like every other file this repository defines.

## Decision

A new package, `packages/game/navbake3d`, ports Recast's pipeline in Go and writes a versioned YAML file; `world3d` stays the runtime that finds paths.

- **Package name**: `navbake3d`, beside the 2D `navbake` (#822) that bakes `ai.NavMesh` polygons, as `world3d` and `physics3d` sit beside their 2D packages. The two bakers take different inputs and write different files, and both declare `Bake`, `Mesh`, `Polygon`, `Parse` and `FormatVersion`.
- **Package boundary**: `navbake3d` imports `world3d`, `physics3d`, `scene3d`, `sceneformat` and `gltf` to collect geometry, and `world3d` imports none of them. The baker never runs in a shipped game's frame loop, so its sources stay out of the runtime package.
- **Pipeline** (`heightfield.go`, `compact.go`, `contour.go`, `polymesh.go`, `detail.go`): rasterize triangles into solid spans, mark tops walkable by `MaxSlope`, filter low-hanging obstacles, ledges and low ceilings by `MaxClimb` and `Height`, build a compact heightfield, erode by `Radius`, partition into monotone regions and drop islands below `MinRegionArea`, trace and simplify contours (region-to-region edges keep only the corners where the neighbour changes, so both sides share them), triangulate and merge into convex polygons of at most `MaxVertsPerPoly`, then add height detail samples until triangles are within `DetailSampleMaxError` of the ground. Every step works in integer cell coordinates and iterates in a fixed order, so the same geometry and settings give the same bytes. Sizes in cells are range-checked while still floats, since an out-of-range float-to-int conversion differs by architecture, and `detailSampleDistance` must be at least `cellSize` (its default rises to the cell size), because the interior sample grid grows with the inverse square of the spacing.
- **Sources**: `Geometry` holds world-space triangles tagged with layer bits; `Settings.LayerMask` selects them. `AddGLTF`, `AddScene` (scene3d nodes by `Layer`, triangles from a `NodeMesh`), `AddSceneFile` (resolved sceneformat scenes and the glTF models their objects name), `AddHeightmap`, `AddGridMap` and `AddPhysicsWorld` (static bodies, triangle meshes, heightfields, and the voxel colliders whose grid implements `physics3d.VoxelBoundsGrid`) feed it. `AddScene` recurses over node children itself rather than using `scene3d`'s `Walk`, whose callback can only stop the whole traversal, so a disabled node skips just its own subtree. Triangles are surfaces, as in Recast; the primitive collider shapes (boxes, spheres, capsules, voxels) from physics3d and GridMaps are filled solid from their lowest to their highest point in each column they cross, so standing inside a box is never walkable.
- **Agent types**: `AgentType` is named (radius, height, max slope, max climb), and `Bake` writes one `Mesh` per agent type in the given order. Zero fields take Recast-sample defaults, and the file records the resolved values.
- **On-disk format**: `navbake3d.File` version 1 (`FormatVersion`), `settings` plus `meshes[]` of `agent`, shared `vertices`, and `polygons[]` with `vertices`, `detail` and `triangles`. `Parse` refuses unknown fields with line and field; `Encode` writes a fixed key order with flow-style points and normalizes `-0` to `0`, so load then save is lossless.
- **Scene and project wiring**: a sceneformat scene names its baked file in `navMesh` (both scene versions, inherited like `name`); `refs` reports it as an output (`Ref.Output`) because `game navmesh` writes it, and the editor's rename moves it. Bake settings and agent types live in game.yaml's `navigation` section, defined in `packages/game/project`, so no other package parses game.yaml.
- **Cross-architecture determinism**: the Go spec lets a compiler fuse `x*y + z` into one rounding; arm64 does and amd64 does not. Every product the bake adds is wrapped in an explicit conversion, which the spec says rounds, and the bake uses its own `cross`, `lenSq`, `norm` and `transformPoint` (`exact.go`) instead of `math3d`'s; capsule facets use a table of 30-degree sines instead of `math.Sin`. `TestBakeMatchesGolden` compares a bake of the reference level with `testdata/reference.navmesh.yaml`, and CI runs it on Linux x86 and macOS arm64.
- **Runtime**: `Mesh.NavMesh` turns every detail triangle into a `world3d.NavMesh` polygon, with `VerticalTolerance` set to the larger of the agent's height and climb, so `FindPath` follows the baked heights over stairs and terrain and keeps a bridge and the floor beneath it apart.
- **Debug draw**: `DrawDebug` strokes polygon outlines and detail triangles onto a `core.Drawing`; it is a no-op in release builds, like the other debug overlays. `Mesh.DebugLines` gives the segments to custom renderers.
- **Schema**: `navbake3d.SchemaURL` (`navmesh.v1.json`) is generated by `cmd/gameschema` from `navbake3d.File`.
- **CLI**: `game navmesh <scene>` bakes a scene file's glTF geometry with game.yaml's navigation settings into the file the scene's `navMesh` names, or `-out`.

## Alternatives considered

- **Accept per-architecture differences in the last digit** — simpler code. Rejected because the file is committed and teams mix Apple Silicon Macs with Linux x86 CI, so every rebake on the other machine would show a diff.
- **Fix fusion in `math3d` instead** — would cover node transforms too. Rejected for this change because `math3d` is shared by the runtime and its own outputs; the bake keeps local fusion-free helpers and leaves `math3d` alone.

- **One `navbake` package for 2D and 3D** — one import path for both bakers. Rejected because their APIs and file formats share names but not shapes (`Mesh` is one 2D agent's polygons in one and one 3D agent's detail mesh in the other), so one package would need `Bake2D`/`Bake3D`-style prefixes on every export.
- **Add baking to `world3d` next to `NavMesh`** — one import for users and the baker sits beside the type it feeds. Rejected because collecting sources needs `physics3d`, `scene3d`, `sceneformat` and `gltf`, and pulling them into `world3d` couples every 3D runtime user to the editor-time baker.
- **Emit the convex polygons straight into `world3d.NavMesh` instead of the detail triangles** — fewer polygons and shorter A* searches. Rejected because `world3d.NavMesh` polygons are planar by height interpolation, so a polygon spanning stairs or uneven terrain would report heights off the ground by up to the climb height; the detail triangles carry the sampled heights.
- **Store the baked mesh in a binary blob or encoding/gob** — smaller and faster to load. Rejected by the repository rule that every file a project keeps is text; the YAML is diffable and its size is bounded by the polygon count, not the voxel count.
- **Treat collider shapes as surfaces like model triangles** — one code path. Rejected because a box's top would then also produce a walkable floor inside it for tall boxes, and GridMap and physics colliders are solid by nature.

## Consequences

- **Benefits**: a 3D level bakes from its glTF models, terrain, GridMaps and static colliders with per-agent clearances; the reference level in `navbake3d/navbake_test.go` (terrain, stairs within and above the climb height, a bridge over a floor, a low ceiling, walls) proves paths climb stairs within `MaxClimb`, never pass under ceilings below `Height` and keep `Radius` from walls. Baking twice writes byte-identical YAML and load then save changes nothing (`TestBakeIsDeterministicAndRoundTrips`, `FuzzParse`). `BenchmarkBakeReferenceLevel` and `BenchmarkNavMeshFromBake` have entries in `benchmarks/baseline.yaml`, and CI's `game bench` checks `./navbake3d`.
- **Costs and known limits**: walls, floors and the erosion are measured in whole cells, so precision is one `CellSize` horizontally and one `CellHeight` vertically. Bakes above `MaxCells` cells are refused rather than tiled; there is no tiled or incremental rebake, and no off-mesh links or area costs. Geometry a game computes before adding it (node world matrices from `math3d`, terrain positions from `world3d`, rotated collider boxes from `physics3d`) is outside the bake and may still differ in a coordinate's last digit between architectures; the reference level is built from values exact in float32. A voxel grid that does not report its bounds cannot be enumerated and bakes nothing. `game navmesh` bakes only the glTF models a scene file names, because terrain, GridMaps and physics worlds are built in code; games bake those with the library API.

## Verification

- `ubuild mise exec go@1.27.3 -- sh -c 'cd packages/game && go tool nsgo test ./navbake3d && go tool nsgo check -target linux,web ./navbake3d'`
- `ubuild mise exec go@1.27.3 -- sh -c 'cd packages/game && go tool nsgo test ./cmd/game'` for `TestNavmesh*`
- `cd packages/game && go test -run '^$' -bench BenchmarkBakeReferenceLevel ./navbake3d`
- `rbuild sh -c 'cd packages/game && go test ./navbake3d'` on arm64 for `TestBakeMatchesGolden`
- `go run ./tools/changelog check && bun run verify-notes`
