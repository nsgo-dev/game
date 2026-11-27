# navbake

`game/pathfind/navbake` bakes the 2D navigation meshes that `pathfind.NavMesh` searches: from a level's outline with concave obstacles and holes, or from a tilemap's solid cells, shrunk by each agent type's radius and divided into convex polygons. A baked mesh saves to a versioned YAML file beside the level, and `Draw` shows it as a debug overlay.

```go
import "github.com/nsgo-dev/game/pkg/pathfind/navbake"
```

`pathfind.BuildNavMesh` takes convex polygons that a game has already cut its level into, and its paths run along the polygons' corners. `navbake` does the cutting, and keeps the paths an agent's radius away from every wall, as Godot does when it bakes a navigation polygon and Recast does for 3D meshes.

## Baking from polygons

A `Source` holds the walkable `Outlines`, such as a room's walls, and the `Obstacles` inside them, such as pillars, crates and pits. Polygons may be concave, may overlap each other or the outline, and may be wound either way: a point is walkable when it is inside the outlines and outside the obstacles under the nonzero winding rule.

```go
src := navbake.Source{
	Outlines: [][]core.Point{{{X: 0, Y: 0}, {X: 200, Y: 0}, {X: 200, Y: 150}, {X: 0, Y: 150}}},
	Obstacles: [][]core.Point{
		{{X: 40, Y: 40}, {X: 60, Y: 40}, {X: 60, Y: 60}, {X: 40, Y: 60}},
		{{X: 120, Y: 30}, {X: 170, Y: 30}, {X: 170, Y: 40}, {X: 130, Y: 40}, {X: 130, Y: 90}, {X: 120, Y: 90}},
	},
}
mesh, err := navbake.Bake(src, navbake.Agent{Name: "guard", Radius: 6}, navbake.Options{})
if err != nil {
	return err
}
nav := mesh.NavMesh() // an *pathfind.NavMesh
path, ok := nav.FindPath(from, to)
```

`Bake` inflates the obstacles, and shrinks the outline, by the agent's `Radius`, rounding every convex corner with a polygon that holds the true circle; triangulates the walkable area left over with a constrained Delaunay triangulation; and merges the triangles into convex polygons. The path `FindPath` finds on the result keeps at least `Radius` from every obstacle and outline edge, and is at most a little longer than the true shortest path for a round agent, around the polygonal corners. A radius of 0 gives the walkable area itself.

The bake snaps every point to a grid of `Options.Precision` world units (`DefaultPrecision`, 1/256, when zero) and decides every geometric question exactly on that grid, so obstacles that touch, overlap or share edges never break it, and the same input gives the same polygons on every platform. Coordinates, and the obstacles once grown by an agent's radius, must stay within 2^24 grid steps of the origin, 65536 world units at the default precision, because that is the largest grid the mesh's `float32` coordinates hold exactly; a coarser precision allows more, and a bake past it is an error. A gap or a sliver of an obstacle thinner than one grid step may close or vanish, so pick a precision finer than the smallest feature that matters.

## Several agent types

Agents of different sizes need different meshes: a narrow gap a rat slips through stops an ogre. `BakeAll` bakes one mesh per `Agent`, in order, from one source, sharing the work that does not depend on the radius; each `Agent` needs a unique `Name`, which the mesh keeps.

```go
meshes, err := navbake.BakeAll(src, []navbake.Agent{
	{Name: "rat", Radius: 2},
	{Name: "ogre", Radius: 12},
}, navbake.Options{})
```

## Baking from a tilemap

`FromTilemap` turns a `tilemap.Tilemap` into a `Source`: the map's area is the outline and every cell whose flags match the mask, as `Tilemap.FlagsAt` combines them across tile layers, is an obstacle. Tiled tiles with a `solid` property and LDtk IntGrid walls already carry `tilemap.TileSolid`.

```go
m, err := tilemap.LoadTiled(data, tilemap.TilesetLoader{})
if err != nil {
	return err
}
src, err := navbake.FromTilemap(m, tilemap.TileSolid)
if err != nil {
	return err
}
meshes, err := navbake.BakeAll(src, agents, navbake.Options{})
```

The cells are the shapes the map's `Orientation` gives them, in world space: squares on an orthogonal map, diamonds on an isometric or staggered one and hexagons on a hexagonal one, so the mesh lines up with the tiles as drawn. Neighbouring solid cells merge into one obstacle, two that touch only at a corner leave no gap a round agent fits through, and an open pocket walled in by solid cells stays walkable but unreachable from outside.

## Mesh files

A `Mesh` is the baked result: the format version, the agent's name and radius, and the convex polygons, each a list of `[x, y]` vertices in world units, counter-clockwise in a y-up frame (clockwise on a y-down screen) from its lowest vertex.

```yaml
version: 1
agent: small
radius: 2
polygons:
  - [[2.0078125, 2.0078125], [37.992188, 2.0078125], [37.992188, 17.992188], [19.601562, 17.992188]]
  - [[2.0078125, 2.0078125], [19.601562, 17.992188], [18.863281, 18.296875]]
  - [[2.0078125, 2.0078125], [18.863281, 18.296875], [18.296875, 18.863281]]
  - [[2.0078125, 2.0078125], [18.296875, 18.863281], [17.992188, 19.601562]]
  - [[2.0078125, 2.0078125], [17.992188, 19.601562], [17.992188, 37.992188], [2.0078125, 37.992188]]
```

`Mesh.Encode` writes it with the fields in a fixed order and one polygon per line, each coordinate in the shortest form that reads back as the same `float32`; `Parse` reads it, refusing unknown fields, another version, invalid off-mesh links, and polygons that are not convex, are wound clockwise, have fewer than three vertices or a coordinate that is not finite. Optional point-to-point off-mesh connections (jumps, drops, ladders, teleports) can be carried in `Source.Links` and stored under `links:` (`id`, `start: [x, y]`, `end: [x, y]`, `radius`, `bidirectional`, `cost`, `area`, `flags`, `userData`). Baking the same input twice gives byte-identical files, and loading a file and saving it again changes nothing, so a game can bake its meshes in a build step, commit them beside its levels and review each change as a diff. `Mesh.NavMesh` builds the `pathfind.NavMesh` to search at run time, registering all stored `Links` automatically without baking again. For runtime dynamic obstacles and per-tile rebuilding, call `Mesh.TiledNavMesh(tileSize)` to build an `*pathfind.TiledNavMesh`, which likewise registers all stored `Links` and re-attaches them across tile rebuilds and obstacle carving.

## Debug drawing

`Draw` fills every polygon, outlines its edges and strokes the portals between neighbouring polygons, the edges a path crosses, each in its own color, under whatever camera transform the `core.Drawing` already has; a transparent color skips that layer. `Mesh.Portals` returns the portals themselves. `Mesh.Portals` buckets the polygons on a grid, so it takes time near linear in their number, but `Draw` still finds the portals on every call; a game that draws a large mesh every frame can keep the `Portals` result and stroke it itself.

```go
navbake.Draw(d, mesh, core.RGBA(0x40a0ff, 0.2), core.RGB(0x2060c0), core.RGB(0xffc040), 1)
```

The CryptQuest sample game bakes its crypt's mesh with `FromTilemap` and draws it when `N` is pressed.

## Authoring navigation regions in scenes

You can also author walkable and obstacle `navregion2d` polygon components directly in `*.scene.yaml` files (with optional tilemap collision subtraction) and edit them with polygon vertex handles, a live viewport overlay, and the **Test Path** tool in [`game/editor`](editor.md#built-in-2d-navigation-region-components-888). `sceneformat.Build` runs `navbake.Bake` automatically into `BuiltScene.NavBake2D` and `BuiltScene.NavMesh2D` so the editor's path preview and the game share the same `pathfind.NavMesh`.
