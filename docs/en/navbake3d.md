# navbake3d

`game/navbake3d` bakes 3D navigation meshes from a level's geometry, as Recast, Godot's `NavigationRegion3D` and Unity's `NavMeshSurface` do, so a 3D game no longer authors the convex polygons of a [world3d](world3d.md) `NavMesh` by hand. It reads the level from glTF models, scene files, terrain heightmaps, `GridMap`s and physics colliders, bakes one mesh per agent type, and saves them in a versioned YAML file that a scene names. For a 2D level, [navbake](navbake.md) bakes `ai.NavMesh` polygons instead.

```go
import "github.com/nsgo-dev/game/packages/game/navbake3d"
```

## Baking a level

Collect the level's triangles in a `Geometry`, then `Bake` it for each agent type:

```go
var g navbake3d.Geometry
g.AddGLTF(levelModel, math3d.Mat4Identity(), 1)   // a *gltf.Document
g.AddHeightmap(terrain, 1)                        // a *world3d.Heightmap
g.AddPhysicsWorld(physicsWorld, navbake3d.LayerAll) // static physics3d colliders

file, err := navbake3d.Bake(&g, navbake3d.Settings{CellSize: 0.2, CellHeight: 0.1}, []navbake3d.AgentType{
	{Name: "humanoid", Radius: 0.4, Height: 1.8, MaxSlope: 45, MaxClimb: 0.4},
	{Name: "rat", Radius: 0.1, Height: 0.3, MaxClimb: 0.1},
})
if err != nil {
	return err
}
data, err := navbake3d.Encode(file) // write it to levels/level1.navmesh.yaml
```

The bake follows Recast's pipeline:

1. It rasterizes the triangles into a heightfield of solid spans, columns `CellSize` wide and deep and `CellHeight` tall.
2. It keeps the tops of spans whose triangle is no steeper than the agent's `MaxSlope`, lets the agent step onto a low obstacle within `MaxClimb`, and drops tops at a ledge deeper than `MaxClimb` and tops with less than `Height` of clearance, so nothing is walkable under a ceiling lower than the agent.
3. It links each walkable top to its neighbours an agent can step to and erodes the walkable area by `Radius`, so the mesh keeps the agent's radius from walls and ledges.
4. It partitions the walkable area into monotone regions and drops islands smaller than `MinRegionArea`.
5. It traces each region's outline and simplifies it: edges between two regions keep only the corners where the neighbouring region changes, so both sides share them, and walls stay within `MaxSimplificationError` of the cells and no longer than `MaxEdgeLength`.
6. It triangulates each outline and merges the triangles into convex polygons of at most `MaxVertsPerPoly` vertices.
7. It adds height detail: samples every `DetailSampleDistance`, at least one `CellSize` (a zero takes 1.8 or the cell size when that is larger), along each polygon's edges and inside it, and adds the worst-fitting ones as vertices of the polygon's triangles until the triangles are within `DetailSampleMaxError` of the ground, so polygons follow terrain and stairs.

Stairs whose risers are within `MaxClimb` bake into a ramp through the steps' edges, which the agent climbs; taller steps leave the floors above them unreachable. A bridge with `Height` of clearance under it bakes into a deck and a floor beneath it that are not linked, and a path between them goes round by the stairs or ramp that join them.

A zero field of `Settings` or `AgentType` takes its default (`DefaultCellSize`, `DefaultAgentRadius` and so on), and the baked file records the value used. `BakeMesh` bakes one agent type. Baking the same geometry with the same settings always writes the same bytes, on any CPU architecture, so the file belongs in version control beside its scene and a bake that changes nothing shows no diff. Go lets a compiler fuse a multiply and an add into one rounding, which arm64 does and amd64 does not, so the bake rounds every product explicitly, and a test compares a bake of a reference level with a committed file on Linux x86 and macOS arm64. Geometry the game computes before adding it, such as node transforms from `math3d` or terrain from `world3d`, is outside the bake and can itself differ in a coordinate's last digit between architectures.

### Precision

Walls, floors and the erosion are measured in whole cells, so the mesh keeps the agent's radius from a wall to within one cell, and heights are rounded up to a whole `CellHeight`. A smaller `CellSize` and `MaxSimplificationError` follow the level more closely and take longer to bake; a level of more than `MaxCells` cells is refused, asking for a larger `CellSize`, and so is an agent whose height, climb or radius spans more cells than the heightfield can hold.

## Level geometry

A `Geometry` holds world-space triangles, each on layers a `Settings.LayerMask` selects from; a layer of 0 counts as every layer. Triangles wind counter-clockwise seen from their front, as glTF's and this module's meshes do: a triangle facing up is a floor, and every triangle is solid whatever way it faces.

| Method | Adds |
| --- | --- |
| `AddTriangle`, `AddMesh` | Triangles, or an indexed triangle list at a world matrix; a mirroring matrix flips the winding back |
| `AddGLTF` | Every mesh of a glTF document's default scene at its node's world transform; strips and fans are expanded, points and lines skipped |
| `AddScene` | Every enabled `scene3d` node outside a disabled node's subtree whose `Layer` shares a bit with the mask, its triangles from a `NodeMesh` (`ColliderMesh` reads a `*scene3d.MeshCollider`) |
| `AddSceneFile` | A [scene file](editor.md): it resolves instanced and inherited scenes, builds the 3D node tree and loads the glTF model each object's `mesh` names (`SceneMeshes`) |
| `AddHeightmap` | A `world3d.Heightmap`, triangulated as its `BuildMesh` does |
| `AddGridMap` | The collider boxes of every occupied cell of a `world3d.GridMap` |
| `AddPhysicsWorld` | The static bodies, triangle meshes, heightfields and voxel colliders of a `physics3d.World` whose layers share a bit with the mask; dynamic and kinematic bodies and triggers are left out, as is a voxel collider whose grid does not implement `physics3d.VoxelBoundsGrid`, since its solid voxels cannot be listed |

Triangles are surfaces, as in Recast, so a closed mesh tall enough to stand in is hollow, and its inside floor is baked as an island no path reaches, unless it is smaller than `MinRegionArea`. The collider shapes `AddPhysicsWorld` and `AddGridMap` add, boxes, spheres, capsules and voxels, are filled instead: each column they cross is solid from their lowest to their highest point.

## The navigation mesh file

A scene names its baked file in its `navMesh` field, a path from the project root, and `game navmesh` writes it there. The file holds the settings it was baked with and one mesh per agent type:

```yaml
version: 1
settings:
  cellSize: 0.3
  cellHeight: 0.2
  minRegionArea: 1
  maxEdgeLength: 12
  maxSimplificationError: 0.4
  maxVertsPerPoly: 6
  detailSampleDistance: 1.8
  detailSampleMaxError: 0.2
  layerMask: 4294967295
meshes:
  - agent:
      name: humanoid
      radius: 0.6
      height: 2
      maxSlope: 45
      maxClimb: 0.9
    vertices:
      - [0, 0, 0]
      - [0, 0, 4]
      - [4, 1, 4]
      - [4, 1, 0]
    polygons:
      - vertices: [0, 1, 2, 3]
        detail:
          - [2, 0.75, 2]
        triangles:
          - [0, 1, 4]
          - [1, 2, 4]
          - [2, 3, 4]
          - [3, 0, 4]
```

| Field | Meaning |
| --- | --- |
| `version` | Schema version, `1` |
| `settings` | The `Settings` the meshes were baked with, every field written |
| `meshes[].agent` | The `AgentType` the mesh was baked for, every field written; names are unique |
| `meshes[].vertices` | The polygons' corners in world space, `[x, y, z]`, shared between neighbouring polygons |
| `meshes[].polygons[].vertices` | Indices of the polygon's corners in `vertices`, 3 to `maxVertsPerPoly` of them, around the polygon |
| `meshes[].polygons[].detail` | Extra vertices sampled on the polygon's edges and inside it where the ground is uneven |
| `meshes[].polygons[].triangles` | The triangles covering the polygon, counter-clockwise seen from above; index `i` below the polygon's vertex count names its `i`th corner, and index `n + i` names `detail[i]` |

`Parse` refuses unknown fields, a missing or unsupported version, non-finite coordinates and indices out of range, with the line, field and a fix; `Encode` writes the fixed key order above, and loading then saving a file changes nothing. [File Formats](formats.md#3d-navigation-meshes) lists it with the others.

## Finding paths

Load the scene's file and turn one agent type's mesh into a `world3d.NavMesh`:

```go
file, err := navbake3d.LoadFS(assets, scene.NavMesh)
if err != nil {
	return err
}
mesh, ok := file.Mesh("humanoid")
if !ok {
	return errors.New("no humanoid navigation mesh")
}
nav := mesh.NavMesh()
path, ok := nav.FindPath(start, goal)
```

`Mesh.NavMesh` makes each detail triangle a walkable polygon of the `NavMesh`, linked to its neighbours across shared edges, so `FindPath` follows ramps, stairs and terrain at the baked heights and keeps stacked floors apart. Its `VerticalTolerance` is the larger of the agent's height and climb, so `FindPolygon` snaps a point to the surface under or over it within that distance.

## Debug drawing

`DrawDebug` strokes a mesh's polygon outlines and detail triangles onto a `core.Drawing` through a `[0, 1]` depth projection such as `math3d.Perspective` or a render3d camera's, clipped at the near plane; `DebugStyle` sets the colors, line width and how far the lines are lifted off the ground. `Mesh.DebugLines` returns the same segments for a renderer of your own. A release build draws nothing.

```go
navbake3d.DrawDebug(d, viewProj, viewport, mesh, navbake3d.DefaultDebugStyle())
```

## From the command line

```sh
go tool game navmesh scenes/level1.scene.yaml
```

[`game navmesh`](cli.md#navmesh) bakes a scene's glTF models for every agent type in the `navigation` section of the [project file](project.md) and writes the file the scene's `navMesh` names.
