# Agent Note: Baking 2D navigation meshes in game/navbake

Status: implemented

## Problem

`ai.BuildNavMesh` accepts only convex polygons a game has already cut its level into, and `FindPath` runs its paths along those polygons' corners, so every game that wants a 2D navmesh decomposes its level by hand and its agents scrape the walls. Issue #822 asks for a bake from an outline with concave obstacles and holes, and from a `tilemap.Tilemap`'s solid cells, shrunk by an agent radius, with one mesh per agent type, a versioned YAML mesh file that is byte-identical across bakes and round trips losslessly, and a debug overlay.

The bake has to survive what level geometry really looks like: obstacles that touch, overlap, share edges or meet only at a corner, which is every tilemap with adjacent solid cells. Floating-point polygon clipping and triangulation fail on exactly those inputs, and their results differ between platforms, which would break byte-identical YAML.

## Decision

A new Experimental package, `packages/game/navbake`, bakes meshes and depends only on `game/ai`, `game/tilemap` and `nsgo/core`; `game/ai` (Stable) is unchanged and keeps searching the result through `Mesh.NavMesh`.

### Exact integer geometry

Every input point is snapped to a grid of `Options.Precision` world units (`DefaultPrecision`, 1/256), and every predicate works on the integer grid: `orient` is exact in `int64` for coordinates within `maxCoord` (2^29 steps), while the bake's input and grown geometry are held to `maxGrid` (2^24 steps), the largest grid the mesh's `float32` world coordinates represent exactly, and `incircle` filters in `float64` with Shewchuk's error bound and falls back to `big.Int`. Crossings created by the arrangement are snapped back to the grid and the arrangement repeats until no new crossing appears (`maxArrangePasses`). The same input therefore gives the same polygons on every platform, and degenerate contacts never break an invariant.

### Two phases sharing one arrangement

1. Outlines and obstacles become directed segments carrying a pair of winding counters (`counts{outline, obstacle}`). `arrange` splits them at every crossing, touch and collinear overlap and merges coincident segments by adding their counters, dropping those that cancel. A constrained Delaunay triangulation (`cdt`) of the result classifies each triangle by the counters accumulated from the bounding box's corner: walkable is nonzero outline winding and zero obstacle winding.
2. For each agent with a radius, the boundary of the blocked region from phase one is inflated: a rectangle of width `radius + minRadiusSteps` on the free side of every straight run of boundary, and at every convex corner a wedge whose arc is a polygon circumscribing the true circle (`radius / cos(delta/2)`, at most `maxArcStep` per segment). Pinch points where two blocked corners meet and concave corners get a whole disc, which closes the slivers that snapped crossings can open. This second segment set is arranged and triangulated the same way.

The walkable triangles are merged into convex polygons with Hertel-Mehlhorn, removing the longest shared edges first, collinear vertices removed, each counter-clockwise from its lowest vertex and sorted, so the output order does not depend on input order. `BakeAll` runs phase one once and phase two per agent.

### File format

`Mesh` is `version`, `agent`, `radius`, `polygons`, each polygon a flow sequence of `[x, y]` on one line with each coordinate in the shortest form that reads back as the same `float32`. `Parse` refuses unknown fields, a second document, another version, and polygons that are not convex with positive area, have fewer than three vertices or a non-finite coordinate. The schema is documented in `docs/en|zh/formats.md` and `navbake.md`.

### Tilemaps and drawing

`FromTilemap` turns the map's area into the outline and each cell matching the flag mask (through `Tilemap.FlagsAt`) into an obstacle shaped as the map's `Orientation` draws it: squares, diamonds for isometric and staggered maps, hexagons for hexagonal ones, in world space. Neighbouring cells merge in the arrangement rather than in `FromTilemap`. `Draw` fills polygons, outlines their edges and strokes the `Portals` between them, in the style of `rts.DrawFlowField`; CryptQuest bakes its crypt and toggles the overlay with `N`.

## Alternatives considered

### Floating-point clipping and an ear-clipping or Bowyer-Watson triangulation

Float geometry is less code and what many engines started with. Rejected because tilemaps are made of polygons that share edges and corners exactly, which is where float clipping produces slivers, duplicate vertices or a failed triangulation, and because float results drift between architectures, which defeats the byte-identical YAML acceptance item and committed golden files.

### Recast-style voxelization

Rasterizing the level into a grid, eroding it by the radius and tracing contours handles any input robustly and is what Recast and Unity do. Rejected because its accuracy is bounded by the voxel size, so the clearance guarantee holds only to within a cell, and a fine grid over a large map costs memory and time a polygon bake does not; the polygonal input here is already exact.

### Minkowski sum of each obstacle with a disc, then a union

Inflating each obstacle polygon separately and unioning the results is the textbook construction. Rejected because the per-obstacle sums overlap everywhere on a tilemap, so the union has to resolve far more crossings than inflating the already merged boundary from phase one, and concave obstacles need their own decomposition first.

### Extending game/ai with the bake

Putting `Bake` in `game/ai` beside `BuildNavMesh` would keep navigation in one package. Rejected because `game/ai` is Stable while the bake's API and file format are new and should stay free to change, as an Experimental package listed in `docs/en|zh/stability.md` is.

## Consequences

- **Benefits**: games get navmeshes from level outlines and tilemaps of every orientation with no hand decomposition; paths keep at least the agent radius from every wall; meshes can be baked in a build step, committed and reviewed as diffs; the bake never fails on touching or overlapping geometry.
- **Costs**: coordinates are limited to `maxGrid`, 2^24 grid steps (65536 world units at the default precision), because past it neighbouring grid vertices collapse in `float32` and the clearance margin and convexity break, and gaps or slivers thinner than one step may close or vanish. Rounded corners are polygonal, so paths are slightly longer than the true shortest path for a round agent. `Mesh.Portals` buckets polygon bounds on a uniform grid sized by the median polygon, so it is near linear for a baked mesh, but `Draw` still recomputes portals each call, so a large mesh drawn every frame should keep the result.

## Verification

- `bake_test.go` checks a reference path on an open room, a concave room with holes whose `FindPath` results keep the radius from every obstacle edge (`testdata/concave.paths.txt`), obstacle order independence, determinism and round trip, `BakeAll` against `Bake`, pinched corners, walkable pockets, errors, and Tiled orthogonal and isometric maps against golden YAML files in `testdata/`.
- `format_test.go` covers the YAML conformance, refusals, `float32` shortest form, `Portals` and `Draw`; `fuzz_test.go` fuzzes `Bake` against a nonzero winding reference, with `BenchmarkBakeTilemap` and `BenchmarkBakeRoom`.
- Run with `go tool nsgo test ./navbake`, `go tool nsgo check -target linux,web ./navbake`, `go test ./navbake -fuzz FuzzBake`, CryptQuest's tests, and the full `go tool nsgo test .` in `packages/game`.
