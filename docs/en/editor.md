# Editor & Scene Format

This document records the scope decision for NSGo's game editor ([#159](https://github.com/nsgo-dev/game/issues/159)) and specifies the version 1 YAML scene format ([#160](https://github.com/nsgo-dev/game/issues/160)) implemented in `github.com/nsgo-dev/game/packages/game/sceneformat`.

## Editor Scope Decision (#159)

NSGo takes **option (b) growing into (c)**: keep Go code and Tiled first-class, define a clean human-editable YAML scene format shared by hand-authored scenes and tooling, and build a lightweight NSGo-native scene editor on top of it.

### Why this scope

- **Code and Tiled stay first-class.** A game written purely in Go—or authored with Tiled `.tmj` maps and TexturePacker/Aseprite atlases—never has to go through an editor. `sceneformat` is an ordinary library package with zero runtime lock-in.
- **One text format, readable diffs.** Scenes are plain versioned YAML (`version: 1`) with deterministic 2-space encoding. Hand-editing a scene in a text editor and saving it from a visual editor produce the same diff-friendly structure.
- **Dogfooding NSGo.** Rather than wrapping a third-party desktop shell or building a monolithic IDE, the lightweight scene editor is an NSGo app built with NSGo's own `ui` views and `Canvas`. Every improvement to inspectors, tree views, shortcuts, or canvas tooling benefits NSGo itself.

### Planned lightweight editor capabilities

The NSGo-native editor operates directly on `sceneformat.SceneDoc` files and focuses on the workflows where visual feedback beats hand-editing coordinates:

1. **Object placement & transforms**: place, drag, rotate, scale, flip and Z-order objects across world and screen layers.
2. **Tile painting**: paint inline tile grids, and the tile layers of layered grids with flipped tiles, or preview referenced Tiled `.tmj` and LDtk `.ldtk` maps.
3. **Typed property & component editing**: inspect and edit `components` and `properties` with validation against the scene schema.
4. **Undo / redo**: command-based editing history over scene documents.
5. **Play-in-editor**: run the live `game.Loop` and `BuiltScene` directly inside the editor's `Canvas` without leaving the tool.
6. **Asset browsing**: browse project atlases, frames, animations and tilemaps and drag them into layers.

Out of scope for the initial editor: custom scripting languages, bespoke shader/material graphs, and proprietary binary project formats.

---

## Scene Format (`version: 2`) (#160)

```go
import "github.com/nsgo-dev/game/packages/game/sceneformat"
```

### Top-level fields

| Field | Type | Description |
| --- | --- | --- |
| `version` | `int` | Required schema version: `2` (`CurrentVersion`), or `1` for a scene that uses none of the fields version 2 added. `Encode` writes it back unchanged. |
| `uid` | `string` | Optional stable ID that stays the same when the file is renamed or moved, written by `go tool game index` (both versions; see [Stable references](project.md#stable-references)). `Resolve` keeps the scene's own uid, not its base's. |
| `inherits` | `string` | Optional slash-separated path from the project root to a base scene this one inherits (version 2, see Instancing and inheritance below). |
| `name` | `string` | Optional human-readable scene name. |
| `navMesh` | `string` | Optional slash-separated path from the project root to the scene's baked [navigation mesh file](navbake3d.md), which `go tool game navmesh` writes (both versions). In a scene that inherits another, it replaces the base's. |
| `virtualWidth`, `virtualHeight` | `float32` | Virtual viewport size in points. Must both be `0` or both positive. |
| `backgroundColor` | `string` | Optional CSS color string parsed by `core.ParseColor` (for example `"#141824"`). Filled over `(0, 0, virtualWidth, virtualHeight)` before layers draw. |
| `camera` | object | Optional default 2D camera for world-space layers (`x`, `y`, `zoom`, `rotation`, optional `bounds`). |
| `tilemaps` | map | Optional named tilemaps referenced from layers via `tilemap.ref`. |
| `layerOverrides` | map | Changes to inherited layers by name: `space`, `order`, `visible`, `tilemap` (version 2, requires `inherits`). |
| `overrides` | map | Changes to inherited objects by ID (version 2, requires `inherits`). |
| `layers` | list | Ordered list of layers drawn lowest `order` first, preserving document order on ties. |
| `connections` | list | Optional connections from one object's events to another object's handlers, written after the layers (both versions, see [Connections](#connections-490)). |

### Camera

- `x`, `y`: world coordinates placed at the center of the virtual viewport `(virtualWidth/2, virtualHeight/2)` (or `(0, 0)` when virtual dimensions are `0`).
- `zoom`: positive zoom factor; defaults to `1` when omitted.
- `rotation`: camera rotation in radians (clockwise on screen); world layers rotate by `-rotation` around `(x, y)`.
- `bounds`: optional `{x, y, width, height}` world rectangle clamping the visible area (`width > 0`, `height > 0`).
- When `camera` is omitted, world layers draw with the identity transform.

### Tilemaps

A tilemap entry (in top-level `tilemaps` or inline on a layer) uses exactly one mode:

- **Reference** (`ref`, layers only): names an entry in top-level `tilemaps`.
- **External map** (`source`): a slash-separated path from the project root to a Tiled map (`.tmj` or `.tmx`) or an LDtk project (`.ldtk`); for LDtk, `level` names the level to build (version 2), and without it the project's first level is built.
- **Inline grid**: `tileWidth`, `tileHeight`, `width`, `height`, `tileset` (atlas name whose frames are keyed `"1"`, `"2"`, …), and row-major `data` of length `width * height` (`0` is an empty tile).
- **Layered grid** (version 2): `tileWidth`, `tileHeight`, `width`, `height`, `tilesetFile`, the path of a YAML tileset (`*.tileset.yaml`, see [YAML tilesets](tilemap.md#yaml-tilesets)) whose tiles the cells place, and `tileLayers`, up to `MaxTileLayers` named layers drawn in order.

The tileset of a layered grid defines each tile once, with its flags, properties, collision shapes, animation and terrain, so the map only places tiles, as a Godot TileSet does for its TileMapLayers. Each tile layer has a `name`, optional `visible` (default `true`) and `opacity` (default `1`), and `rows`: `height` rows from the top, each `width` cells separated by spaces. A cell is `.` when empty, or the tile's local ID in the tileset (`0`, `1`, …) followed by any of `h`, `v` and `d` to flip it horizontally, vertically or across its diagonal, as Tiled's flip bits do, so `3hv` is tile 3 turned half a turn. A row may align its cells with extra spaces and write the letters in any order; `Encode` writes single spaces and the order `h`, `v`, `d`, so each row is one line in a diff.

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/scene.v2.json
version: 2
name: Cave
virtualWidth: 320
virtualHeight: 180
layers:
  - name: ground
    tilemap:
      tileWidth: 16
      tileHeight: 16
      width: 4
      height: 2
      tilesetFile: tilesets/cave.tileset.yaml
      tileLayers:
        - name: floor
          rows:
            - 0 0 0 0
            - . . 1h 3hvd
        - name: deco
          opacity: 0.5
          rows:
            - 2 . . .
            - . . . .
```

`TileLayerDoc.Cell`, `SetCell` and `Cells` read and write a layer's cells as tile values, the GIDs they have once built: `0` for an empty cell, otherwise the local ID plus 1 with `tilemap.FlipHorizontal`, `FlipVertical` and `FlipDiagonal` set; `ParseTileRow` and `FormatTileRow` do the same for one row.

`Build` turns a single grid of atlas frames into a sprite batch (`BuiltLayer.Tilemap`), and a layered grid or an external map into a `tilemap.Tilemap` (`BuiltLayer.Map`). The map carries the tileset's flags, properties, shapes, animations and terrains, so in the layer's coordinates it answers `FlagsAt`, `TileProperties`, `TilesInRect` and `Raycast` and serves `tileshape.New` for slopes and occluders. `BuiltScene.Draw` draws only the map tiles inside the virtual viewport, seen through the camera for a world-space layer, unless the scene has no virtual size or `DrawAllTiles` is set, as the editor sets it for its edit view; `BuiltScene.Update(dt)` advances animated tiles.

`Build` reads YAML tilesets, Tiled maps with their external tilesets and LDtk projects through the resolver's `FileResolver` (`ReadFile`), with paths in a Tiled or LDtk file taken from that file's directory, and loads their images through an `ImageResolver` (`Image`) when the resolver has one, or names them by path (`core.ImageSource{URI: path}`) otherwise. `FSAssets` reads a project's files from an `fs.FS`, so a scene that names a `.tmj`, `.ldtk` or `.tileset.yaml` builds with no resolver of the game's own. A resolver that implements `TilemapResolver` still loads every `source` itself into a `TilemapDoc`, as before, except a source that names an LDtk `level`: `Tilemap(source)` cannot receive the level, so `Build` reads that one through the `FileResolver`.

### Layers and Objects

Each layer has:

- `name` (required, unique within the scene)
- `space`: `"world"` (default when omitted, draws under camera) or `"screen"` (draws in canvas coordinates, ignoring camera)
- `order`: integer draw order (lowest first)
- `visible`: boolean, defaults to `true` when omitted
- `tilemap`: optional tilemap drawn beneath the layer's objects
- `objects`: placed objects in the layer

Each object has:

- `id` (required, unique across the entire scene)
- `name`, `prefab` (optional)
- Transform: `x`, `y`, `originX`, `originY`, `rotation` (radians, clockwise), `scaleX` (default `1`), `scaleY` (default `1`), `flipX`, `flipY`, `alpha` (`[0, 1]`, default `1`), `z` (layer-local draw order, lowest first)
- `sprite`: optional `{atlas, frame}` referencing a `sprite.Atlas` and frame name
- `material`: optional slash-separated path from the project root to a shared material file, such as `materials/stone.material.yaml`, resolved at build time through `MaterialResolver` (see [resource](resources.md))
- 3D look: `mesh`, one of the built-in primitives `box`, `sphere`, `plane`, `capsule` and `cylinder`, or `model` (version 2), the slash-separated path from the project root to a glTF 2.0 file (`.gltf` or `.glb`), optionally narrowed to one of its nodes by `modelNode` or one of its meshes by `modelMesh`; an object's `material` replaces the model's materials or gives the primitive its surface. `scenemodel.Build` turns them into geometry ([scenemodel](scenemodel.md)). `CheckObjectModel` checks these fields on one object, and the editor's `PlaceObject` refuses an object it refuses; the inspector's Mesh field lists the primitives and is disabled on an object that draws a model
- `components`: optional mapping from a component type name to its fields; free-form unless a component registry is given (see [Component types](#component-types))
- `properties`: optional `map[string]any` for custom metadata
- `scene`: optional slash-separated path from the project root to a scene file this object instances (version 2, see Instancing and inheritance below)
- `overrides`: optional changes to the instanced scene's objects by ID (version 2, requires `scene`)
- `children`: optional list of objects attached to this one (see Node tree below)

### Node tree (#484)

An object can hold child objects under `children`, nested up to `MaxObjectDepth` (64) levels. A child's transform is local to its parent: `x`, `y`, `rotation`, `scaleX` and `scaleY` (and in 3D `posZ`, `rotX`, `rotY`, `rotZ` and `scaleZ`) compose with every ancestor's, and its `alpha` multiplies theirs. `originX`, `originY`, `flipX` and `flipY` stay the object's own, and `z` orders the whole layer, children included, with ties drawing each parent before its children. Children belong to their parent's layer, and their IDs share the scene-wide namespace, so an ID used anywhere in the tree is refused anywhere else.

```yaml
layers:
  - name: ships
    objects:
      - id: ship
        x: 100
        y: 50
        rotation: 1.5707964
        scaleX: 2
        scaleY: 2
        sprite: { atlas: actors, frame: hull }
        children:
          - id: turret
            x: 10        # lands at (100, 70): a quarter turn and twice the size of ship
            sprite: { atlas: actors, frame: turret }
            children:
              - id: barrel
                x: 5     # lands at (100, 80)
```

The same tree loads three ways:

- `Build` links each `BuiltObject` to its `Parent` and `Children`, sets `World` to the composed 2D affine transform and `WorldAlpha` to the composed alpha, and draws each child's sprite at its world position, rotation and scale. A sprite whose composed transform is sheared (a rotated child under a non-uniformly scaled parent) cannot be drawn as a sprite, so `Validate` refuses it, and with it `Parse`, `Encode` and `Build`; `CheckObjectTree` makes the same check, and the nesting limit, for a single tree. A parent collapsed to zero size along one axis (such as `scaleX: 0`) is not a shear: its child sprites draw at zero scale. Objects without a sprite keep the exact matrix in `World`. `BuiltLayer.Objects` lists a layer's top-level objects, and `BuiltScene.Objects` indexes every object by ID. After changing a built object's document fields, `BuiltObject.Sync` recomposes its `World` and `WorldAlpha` and its sprite, and its descendants', and reports false for a sprite the change would shear.
- `SpawnEntities(doc, world)` spawns one entity per object into an `entities.World`, parented in its `Hierarchy2D` as the objects nest, with the object's local `Transform2D` and an `ObjectRef` component naming the object and its layer. The top-level objects of a hidden layer spawn hidden, so their subtrees are not world-visible.
- `BuildNodes3D(doc, scene.Root())` builds a `scene3d` node tree: one group node per layer (carrying the layer's visibility), and one node per object named by its ID, with `UserData` pointing at its `ObjectDoc`. `ObjectLocalTransform3D` gives each node's local transform; `rotX`, `rotY` and `rotZ` apply in that order about fixed axes, the same order the editor's 3D viewport uses.
- `scenemodel.Build(doc, scene.Root(), assets)` builds the same tree and attaches to each object's node the geometry and materials of its `mesh` or `model`, with the node's `LocalBounds` set from the geometry, ready for `render3d.Queue` ([scenemodel](scenemodel.md)).

```go
spawned, err := sceneformat.SpawnEntities(doc, world)
if err != nil {
	return err
}
world.Hierarchy2D().Sync()
wt, _ := world.Hierarchy2D().WorldTransform(spawned["barrel"]) // wt.X == 100, wt.Y == 80

nodes, err := sceneformat.BuildNodes3D(doc, scene.Root())
if err != nil {
	return err
}
pos := nodes.Objects["barrel"].WorldTransform().Pos
```

Moving, deleting or undoing an object in the editor carries its children along, and the editor never hands out an ID that a nested child already uses. It refuses to place a tree deeper than `MaxObjectDepth` or holding a sheared sprite, and refuses a scale or rotation that would shear a sprite, the object's own or a descendant's, under the transforms of its ancestors. Both viewports draw, pick and edit nested children, and the [scene tree](#scene-tree-861) selects, reparents, reorders and renames them.

### Instancing and inheritance (#483)

A version 2 scene reuses other scene files the way Godot's scenes do. Every reference is a slash-separated path from the project root, like a material's, and `Parse` and `Encode` keep references and overrides exactly as written: a level of instances saves as the same instances, never as copies of their objects.

An object with `scene` instances that file. The top-level objects of every layer of the instanced scene, in document order, become the object's first children, ahead of its own `children`, so the object's transform places the whole instance. Their IDs are prefixed with the object's ID and a slash, so ten instances of one enemy hold `enemy_1/body` to `enemy_10/body`. `overrides` changes the instanced objects, keyed by their IDs inside the instanced scene; a scene instanced inside it is reached by its prefixed ID, such as `gun/barrel`. The instanced scene's layer settings, camera, background and virtual size do not carry over, and a scene whose layers hold a tilemap cannot be instanced.

```yaml
# scenes/enemy.scene.yaml
version: 2
name: Enemy
layers:
  - name: root
    objects:
      - id: body
        sprite: { atlas: actors, frame: hero }
        properties: { health: 3, speed: 40 }
        children:
          - id: torch
            x: 6
            sprite: { atlas: actors, frame: torch }

# level.scene.yaml
version: 2
layers:
  - name: actors
    objects:
      - id: enemy_1
        x: 32
        "y": 40
        scene: scenes/enemy.scene.yaml
        overrides:
          body:
            alpha: 0.5
      - id: enemy_2
        x: 64
        "y": 40
        scene: scenes/enemy.scene.yaml
        overrides:
          torch:
            x: -6
```

A scene with `inherits` starts as a copy of its base and changes only what it declares:

- `name`, `backgroundColor`, the virtual size and `camera` replace the base's when set, and `tilemaps` entries are added or replaced by name.
- `overrides` changes inherited objects by ID, nested children and instanced objects (`enemy_1/body`) included.
- `layerOverrides` changes inherited layers by name.
- A layer named like one of the base's adds its objects to that layer and may set nothing else; any other layer is added after the base's layers.

An inherited object or layer cannot be removed, and its ID cannot change.

```yaml
# scenes/fast_enemy.scene.yaml: the enemy above, twice as fast
version: 2
inherits: scenes/enemy.scene.yaml
overrides:
  body:
    properties:
      speed: 80
```

An override sets any object field except `id`, `children`, `scene` and `overrides`; a field it leaves out keeps the base's value, and a value of `0` or `false` is still a change. `components` and `properties` are replaced key by key, keeping the object's other keys.

`Resolve(doc, scenes)` loads the referenced files through a `SceneResolver`, such as `FSScenes{FS: os.DirFS(root)}`, and returns a self-contained copy in which no object instances anything and nothing is inherited. It loads each file once per call and never changes `doc` or the documents the resolver returns. It refuses a cycle of scenes that instance or inherit each other, a chain deeper than `MaxSceneNesting` files, a result of more than `MaxResolvedObjects` objects, an override that names no object or layer of the scene it changes, and a result that does not validate, such as one whose prefixed IDs collide with other IDs. `Build` resolves a scene itself when its `AssetResolver` also implements `SceneResolver`, and `BuiltScene.Doc` is then the resolved copy; `SpawnEntities` and `BuildNodes3D` refuse a scene that still refers to others, so resolve it first.

```go
type assets struct {
	sceneformat.AtlasMap
	sceneformat.FSScenes
}

built, err := sceneformat.Build(level, assets{atlases, sceneformat.FSScenes{FS: os.DirFS("project")}})
```

In the editor, `PlaceScene(layer, path, x, y)` places an instance of a scene file as an undoable command, with an ID named after the file. The editor's `Resolver` must implement `SceneResolver`; `PlaceScene`, and `PlaceObject` for any object tree that names a `scene`, resolve the scene as it would be with the instance placed and refuse it when that fails: a missing or invalid file, an override that names no object or holds a bad value, or IDs that collide once instances are expanded. Set the editor's `ScenePath` to the open scene's path from the project root (`OpenYAML` clears it) so that an instance leading back to the open scene, directly or through other files, is refused as a scene cycle before the saved file instances itself. Placing an instance raises a version 1 scene to version 2 (which Undo restores). New IDs are checked against the IDs the scene gets from its base and its instances as well as its own. The viewports select and move an instance by its root object, and Play runs a resolved copy of the scene, so changes made through `BuiltObject.Doc` show in the editor's document until Stop restores it.

### Example

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/scene.v2.json
version: 2
name: Cave Room
virtualWidth: 320
virtualHeight: 180
backgroundColor: "#141824"
camera:
  x: 160
  y: 90
  zoom: 1
  bounds:
    x: 0
    y: 0
    width: 320
    height: 180
tilemaps:
  floor_grid:
    tileWidth: 16
    tileHeight: 16
    width: 4
    height: 2
    tileset: cave_tiles
    data: [1, 2, 2, 1, 1, 0, 0, 1]
layers:
  - name: floor
    space: world
    order: 0
    tilemap:
      ref: floor_grid
  - name: actors
    space: world
    order: 10
    objects:
      - id: hero
        name: Hero
        prefab: prefabs/hero
        x: 48
        y: 40
        originX: 8
        originY: 8
        z: 5
        sprite:
          atlas: actors
          frame: hero
        components:
          collider: { w: 12, h: 14 }
        properties:
          spawn: true
  - name: hud
    space: screen
    order: 100
    objects:
      - id: heart_1
        x: 8
        y: 8
        sprite:
          atlas: ui
          frame: heart
```

### Loading, building and drawing in Go

```go
doc, err := sceneformat.Parse(yamlBytes)
if err != nil {
	return err
}

built, err := sceneformat.Build(doc, sceneformat.FSAssets{
	FS: os.DirFS(projectDir), // YAML tilesets, Tiled and LDtk maps
	Atlases: sceneformat.AtlasMap{
		"cave_tiles": tilesAtlas,
		"actors":     actorsAtlas,
		"ui":         uiAtlas,
	},
})
if err != nil {
	return err
}

// Each update step:
built.Update(dt)
// In a core.Canvas's Draw callback:
built.Draw(d)
```

`Parse` rejects unknown YAML keys, trailing documents, unsupported versions, version 2 fields in a version 1 scene, scene references that are not slash-separated paths from the project root, overrides without the `scene` or `inherits` they change, duplicate layer names or object IDs (nested children included), object trees deeper than `MaxObjectDepth`, invalid spaces, malformed colors, and non-finite or out-of-range transforms. `Encode` validates and emits deterministic 2-space YAML that round-trips through `Parse` without loss.

### Schema

The scene format's JSON Schemas are published at `https://game.nsgo.dev/schemas/scene.v1.json` (`sceneformat.SchemaURL`) and, for version 2 scenes that instance or inherit other scene files or hold layered grids or LDtk levels, `https://game.nsgo.dev/schemas/scene.v2.json` (`sceneformat.SchemaURLV2`). `Encode` starts every file with the `# yaml-language-server: $schema=…` line for its version (`sceneformat.SchemaCommentFor`), so VS Code's YAML extension and other editors with a YAML language server validate and complete a scene as it is written. See [Schemas for YAML files](schemas.md).

### Component types

A `ComponentRegistry` declares, in Go and without reflection, the component types scene files may use: each type's name and documentation, and each field's name, kind (`FieldBool`, `FieldInt`, `FieldFloat`, `FieldString` or `FieldStringList`), optional allowed `Enum` values (for `FieldString` fields), default, optional inclusive range, optional `Step` for the inspector and documentation. `Register` refuses empty or duplicate names, ranges or steps on non-numeric fields, `Enum` on non-`FieldString` fields, a minimum above the maximum, a fractional range or step on a `FieldInt`, a negative step, and a default of the wrong kind or out of range.

```go
reg := sceneformat.NewComponentRegistry()
reg.MustRegister(sceneformat.ComponentType{
	Name: "body",
	Doc:  "A moving body that collides with the level.",
	Fields: []sceneformat.Field{
		{Name: "solid", Kind: sceneformat.FieldBool, Default: true, Doc: "Blocks other solid bodies."},
		{Name: "speed", Kind: sceneformat.FieldFloat, Default: 32.0, Min: sceneformat.Float64(0), Max: sceneformat.Float64(500), Doc: "Top speed in points per second."},
		{Name: "layer", Kind: sceneformat.FieldInt, Default: 1, Min: sceneformat.Float64(0), Max: sceneformat.Float64(31), Doc: "Collision layer."},
	},
})

doc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{File: "scenes/room.scene.yaml", Components: reg})
```

With a registry, `ParseWith` checks the components of every object, nested children included, and refuses an unregistered component type, a misspelled or unknown field, a value of the wrong kind (a `FieldInt` refuses `3.0`; a `FieldFloat` accepts an integer), a non-finite number and a value out of range. The error is a `*ComponentError` carrying the file, line and column of the offending key or value, the object ID, component and field, and lists the registered names:

```text
scenes/room.scene.yaml:11:13: sceneformat: object "hero" component "body" field "sped": unknown field (registered: solid, speed, layer)
```

Defaults are not written into the document, so loading and saving a scene changes nothing; `reg.Resolve("body", fields)` returns every field with its default filled in. `ValidateComponents` checks a document built in Go (without positions), and `CheckField` checks one value and normalizes it to `bool`, `int`, `float64` or `string`. `Parse` without a registry keeps `components` free-form.

In the editor, `Editor.SetComponentRegistry(reg)` checks the open scene's components and, as `OpenFile` does, its connections, then makes `OpenFile` report component errors with the file and line, and makes `SetComponentField` and `PlaceObject` refuse fields the registry does not allow. Changing the registry clears undo/redo history, so an undone edit the new registry refuses cannot be redone. The inspector lists every registered field of a component in declaration order, including fields the scene leaves unset, which show their default; the label's tooltip gives the field's documentation, kind, range and default, enum fields show a selector, string-list fields edit comma-separated items, and numeric inputs are bounded by the range and step by the field's `Step` (1 for an int, 0.1 for a float when unset). Floats are shown at full precision, and an edit the registry refuses is reported in the status bar. Components without a registered type, or every component when no registry is set, are shown from the values in the file as before (with the built-in physics component schemas available automatically in the inspector).

### Built-in physics components (#817)

`sceneformat` ships six versioned built-in physics components (`PhysicsComponentVersion = 1`), registered together with `RegisterPhysicsComponents(reg)` or `PhysicsComponentRegistry()`:

| Component | Dimension | Key fields |
| --- | --- | --- |
| `body2d` | 2D | `version` (`1`), `kind` (`static`, `kinematic`, `dynamic`), `mass`, `gravityScale`, `vx`, `vy`, `friction`, `restitution`, `linearDamping`, `angularDamping`, `material` |
| `collider2d` | 2D | `version` (`1`), `shape` (`box`, `circle`), `width`, `height`, `radius`, `offsetX`, `offsetY`, `layer`, `mask` (`[]string`), `isTrigger`; declares events `enter`, `stay`, `exit` |
| `trigger2d` | 2D | `version` (`1`), `shape` (`box`, `circle`), `width`, `height`, `radius`, `offsetX`, `offsetY`, `layer`, `mask` (`[]string`); declares events `enter`, `stay`, `exit` |
| `body3d` | 3D | `version` (`1`), `kind` (`static`, `kinematic`, `dynamic`), `mass`, `gravityScale`, `vx`, `vy`, `vz`, `friction`, `restitution`, `linearDamping`, `angularDamping`, `lockRotation`, `material` |
| `collider3d` | 3D | `version` (`1`), `shape` (`box`, `sphere`, `capsule`), `width`, `height`, `depth`, `radius`, `offsetX`, `offsetY`, `offsetZ`, `layer`, `mask` (`[]string`), `isTrigger`; declares events `enter`, `stay`, `exit` |
| `trigger3d` | 3D | `version` (`1`), `shape` (`box`, `sphere`, `capsule`), `width`, `height`, `depth`, `radius`, `offsetX`, `offsetY`, `offsetZ`, `layer`, `mask` (`[]string`); declares events `enter`, `stay`, `exit` |

- **Named collision layers**: `layer` and `mask` reference human-readable layer names declared under `physics.layers` in [`game.yaml`](project.md) instead of raw bitmasks. Pass `ParseOptions{File: ..., PhysicsLayers: proj.PhysicsLayerTable()}` to `sceneformat.ParseWith` (done automatically by `project.File.LoadAutoloads` and `Editor.OpenProject`): any misspelled or unknown layer name fails to load with a `*loaderr.Error` carrying the exact file path, 1-based line, column, field path, and allowed layer names.
- **Building and stepping (`BuildPhysics`)**: `ps, err := sceneformat.BuildPhysics(built, proj.PhysicsSettings(), lib)` constructs the matching `physics.World`, `physics.RigidWorld`, and `physics3d.World` (applying project gravity, solver iterations, named `Layer`/`Mask` bitmasks, and optional `*.physics.yaml` files via `PhysicsMaterialResolver`). Calling `ps.Step(dt)` steps 2D kinematic and rigid bodies, 2D triggers, and 3D bodies and triggers, writes updated local poses back onto `BuiltObject.Doc`, and dispatches `PhysicsTriggerEvent` (`TriggerEnter`, `TriggerStay`, `TriggerExit`) to `ps.OnTrigger` and any attached `ps.Signals` (`enter` / `stay` / `exit`).
- **Play-in-editor & viewport debug outlines**: `Editor.PlayIn` automatically builds and steps `PhysicsScene` (`Editor.PlayPhysics`, `Editor.PlayTriggerEvents`, `Editor.OnPlayTrigger`) so a scene with a player body, wall colliders, and a coin trigger plays in the editor with collision and trigger events without writing game collision code. While `Editor.ShowPhysicsDebug` is enabled (the default; toggle with `SetShowPhysicsDebug`), both the 2D and 3D viewports render wireframe outlines for every `collider2d`, `trigger2d`, `collider3d`, and `trigger3d`.

### Connections (#490)

A scene file connects an event one object emits to a handler of another object, as Godot's `[connection]` entries in a `.tscn` file do, so a button opens a door with no Go code that names either of them:

```yaml
layers:
  - name: props
    objects:
      - id: button
        components:
          button: {}
      - id: door
        components:
          door: {}
connections:
  - from: button
    event: pressed
    to: door
    handler: open
    args:
      speed: 2.5
```

| Field | Type | Description |
| --- | --- | --- |
| `from` | `string` | Required ID of the object that emits the event. |
| `event` | `string` | Required event name, such as `pressed`, or `button.pressed` to say which component when several components of the object declare it. |
| `to` | `string` | Required ID of the object whose handler runs; it may be `from` itself. |
| `handler` | `string` | Required handler name, such as `open`, or `door.open`. |
| `args` | map | Optional values for the handler's params by name, each a bool, a number or a string; a param left out takes its default. |
| `once` | `bool` | Disconnects after the first time the event fires, as Godot's one-shot connections do. |

The events and handlers come from the component registry: a `ComponentType` lists the `Events` an object with that component emits and the `Handlers` it can run, each handler with typed `Params` (declared and checked like fields) and the `Func` that runs it. The game registers what each kind of component does once; the scene files decide which objects are wired together.

```go
reg.MustRegister(sceneformat.ComponentType{
	Name:   "button",
	Events: []sceneformat.Event{{Name: "pressed", Doc: "The player pressed the button."}},
})
reg.MustRegister(sceneformat.ComponentType{
	Name:   "door",
	Fields: []sceneformat.Field{{Name: "open", Kind: sceneformat.FieldBool}},
	Handlers: []sceneformat.Handler{{
		Name:   "open",
		Params: []sceneformat.Field{{Name: "speed", Kind: sceneformat.FieldFloat, Default: 1.0, Min: sceneformat.Float64(0)}},
		Func: func(c sceneformat.Call) {
			c.Target.Components["door"].(map[string]any)["open"] = true
		},
	}},
})

doc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{File: "scenes/hall.scene.yaml", Components: reg})
signals, err := sceneformat.Connect(doc, reg, game)
// Where the game handles a press on an object with a button component:
err = signals.Emit(objectID, "pressed", nil)
```

`Connect(doc, reg, ctx)` checks the scene's components and connections and returns a `*Signals`. `Signals.Emit(from, event, payload)` calls the handler of every connection from that event in the order the file lists them, skipping a `once` connection that has run. Each handler gets a `Call`: the `Signals` (so it can emit further events, nested at most `MaxEmitDepth` levels), the `ctx` given to `Connect` (such as the game's state or a `*BuiltScene`), the source and target `*ObjectDoc` in the scene, the components and names of the event and handler, `Args` holding every param (the connection's value or the default) and the `payload`. `Emit` refuses an object the scene does not hold, an event none of its registered components declares, and a bare name two of its components declare (write `component.event`). `Signals` is a snapshot of the scene's objects as `Connect` finds them: after adding or removing objects in `doc`, which may also move the others in memory, call `Connect` again. A scene that instances or inherits other scene files is connected after `Resolve` (or through `BuiltScene.Doc`), and `Signals` is not safe for concurrent use.

`Parse` checks a connection's form: all four names set, `event` and `handler` written as a name or `component.name`, arguments that are bools, finite numbers or strings, no connection repeated, and, in a scene that refers to no other scene file, `from` and `to` naming its objects. With a registry, `ParseWith` also checks the names against it and reports a `*ConnectionError` with the file, line and column: an event or handler no registered component of the object declares, a bare name two of its components declare, an argument the handler has no param for, and an argument of the wrong kind or out of range. The error lists what the object does declare:

```text
scenes/hall.scene.yaml:16:14: sceneformat: connection 0 ("button" pressed -> "door" opne): no registered component of object "door" declares handler "opne" (it has: door.open)
```

`ValidateConnections` makes the same checks on a resolved document without positions. `Register` refuses an event or handler with an empty name, a dot in its name or a name its type already uses, and handler params on the same terms as fields; a registry used only to check files, such as an editor's, may leave `Func` unset, which `Connect` refuses.

`Resolve` carries connections along: an instanced scene's connections come with the instance, the objects they name prefixed like its objects' IDs (`door_1/button`), so every copy of a prefab keeps its own wiring; an inheriting scene keeps its base's connections and adds its own after them. A scene may connect objects of the scenes it instances or inherits, and those names are checked once it is resolved. In the editor, `DeleteObject` also removes the connections from or to the deleted object and its descendants, and undoing the delete puts them back.

#### Queued events

`Emit` runs the handlers before it returns, so a handler that emits runs the next handlers inside itself. To keep a step's order plain, emit with `Signals.Enqueue(from, event, payload)` instead and call `Signals.Flush()` once per step, such as at the end of `game.Loop`'s `Update`:

```go
loop.Update = func(dt time.Duration) {
	updateSystems(dt) // the game's own input and systems, which call signals.Enqueue
	if err := signals.Flush(); err != nil {
		log.Print(err)
	}
}
```

`Enqueue` refuses what `Emit` refuses before it holds anything, so a misspelled event fails where it is raised. `Flush` runs the events enqueued before it began, in the order they were enqueued, each as `Emit` runs it; an event a handler enqueues, its own included, waits for the next `Flush`, so one `Flush` always ends and a handler never re-enters itself. `Flush` from a handler is refused, and `Pending` says how many events wait. A handler's panic passes through `Flush`, and the events after the one that panicked stay queued for the next `Flush`. Once the queue has grown to a step's events, a step allocates nothing more than emitting them would.

#### Spawning connected entities

`SpawnConnected(doc, world, reg, ctx)` checks the scene as `Connect` does and spawns its objects as `SpawnEntities` does, then returns each object ID's entity and the `*Signals` that connects those entities. Every check runs before the first entity is spawned, so a scene whose connection names a missing object, event, handler or `Func`, or whose components break their types, spawns nothing. `EmitEntity(e, event, payload)` and `EnqueueEntity` address an object by its entity, `Entity(id)` returns an object's entity, and each `Call` carries `SourceEntity` and `TargetEntity`, so a handler reaches the target's components with `entities.Get`. A destroyed entity emits nothing: `Emit`, `Enqueue` and their entity forms refuse it, `Flush` drops the events it enqueued, and connections to it are skipped. A scene that instances or inherits others is spawned after `Resolve`, its instances' connections reaching the instances' entities (`west/door`).

#### Checking connections

`go tool game check` resolves each scene that instances or inherits others. It fails on a scene that does not resolve, such as a cycle or an override naming no object, at the instance or `inherits` line, and on a connection whose `from` or `to` names no object of the resolved scene, at the line that names it. The events and handlers are declared in Go, which the command cannot load, so a game checks them in its own tests with `assetcheck.CheckWith` and its registry. That reports, at the file and line, a connection naming an event, handler or argument its objects do not declare and a component that breaks its type; in a scene that instances or inherits others, it also checks the components its overrides write and every connection of the resolved scene, so an override that breaks a connection the instanced or inherited scene brings, such as one adding a second component that declares the same event, fails at the override:

```go
func TestScenesCheck(t *testing.T) {
	report, err := assetcheck.CheckWith(os.DirFS("."), assetcheck.Options{Components: Components})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range report.Problems {
		t.Error(p)
	}
}
```

```text
scenes/level.scene.yaml:12: sceneformat: connection 0 ("lever" held -> "west/door" open): no registered component of object "lever" declares event "held" (it has: button.pressed)
```

A fault that a scene brings unchanged from a scene it instances or inherits is reported on that file, and one that its overrides cause on the override, so each fault is reported once. Problems carry the line, not the column.

### Inspector (#865)

The inspector sets up an object without editing YAML. Every edit it makes is one undoable step, and each has a method on `Editor` that does the same in Go; while Play is on, the methods return `ErrPlaying` and the controls are disabled. An edit the editor refuses is reported in the status bar.

- **Sprite**: the Sprite field picks an atlas frame from those the asset browser lists (`Assets`), grouped by atlas and searchable, and its clear button unbinds it; the button beside it takes the frame selected in the asset browser. `SetObjectSprite(id, ref)` needs both an atlas and a frame, and with a resolver set the atlas must resolve and hold the frame; a nil `ref` unbinds the sprite.
- **Origin and flip**: `originX` and `originY`, the pivot inside the sprite's frame, are set with `SetObjectOrigin(id, x, y)`, which refuses values that are not finite, and `flipX` and `flipY` with `SetObjectFlip(id, flipX, flipY)`.
- **Material**: the Material field picks one of the open project's `*.material.yaml` files and its clear button names none; `SetObjectMaterial(id, path)` takes a slash-separated path from the project root, or an empty one. A material must load through the editor's resolver, which therefore has to implement `sceneformat.MaterialResolver` (as `resource.Library` does), since the scene could not be built otherwise; with any other resolver the field lists no files and stays enabled only to clear a material the object already names.
- **Properties**: Add Property takes a key and a kind (`string`, `int`, `float` or `bool`) and adds the property holding that kind's zero value (`AddProperty(id, key, kind)`); the field trims the key, and an empty key, one with leading or trailing whitespace or one the object has, is refused. A whole-number float property is saved with a decimal point (`2.0`), so it opens again as a float. Each property's delete button removes it (`DeleteProperty`).
- **Components**: the Add Component field filters the registered types the object lacks, and pressing one adds it holding every registered field at its default, so the file shows what the component does (`AddComponent(id, name)`). Enter adds the type the field names, or the one type it matches. A component the object already has, a name the registry does not know, and a name with leading or trailing whitespace or a `.` (which connections use in `component.event`) are refused; without a registry the field adds a component of any name with no fields. Adding a component is refused, too, when it would declare an event or handler that a connection names bare and another of the object's components declares as well, since the connection would no longer say which it means: write it as `component.event` first. In a scene that instances or inherits others, the check runs on the scene as resolved through the editor's resolver, the connections of the scenes it refers to included; it is skipped when the resolver cannot load scene files or the scene does not resolve and bind even without the new component. Each component's remove button removes it (`RemoveComponent(id, name)`) with every connection that needs it, from the object with an event the component declares or to the object with a handler it declares, named bare or as `component.name`; a bare name is attributed through the component's registered type, so without a registry, or for a type the registry does not know, only connections written as `component.name` are removed. `ComponentConnections(id, name)` lists them, and Undo puts the component and its connections back where they were. A component without a registered type keeps its fields editable from the values in the file; having no schema, a whole-number float in one of its fields is saved as `2` and opens again as an int.
- **Connections (#872)**: the Connections section lists the events declared by the selected object's registered components (`ObjectEvents`) and every outgoing and incoming connection touching it (`ObjectConnections`). Pressing **Connect** opens an inline form to pick an event, choose a target object from the searchable object list (`SceneObjectIDs`) or pick it directly from the 2D/3D viewport or scene tree (`BeginConnectionPick` / `CancelConnectionPick` without losing the current selection), pick one of the target's registered handlers (`ObjectHandlers`), fill the handler's params with typed controls from the component registry, and toggle `once`. Each connection row lets you jump to its other end (`JumpToConnectionOtherEnd`), edit it (`UpdateConnection`), or remove it (`RemoveConnection`). Adding, editing and removing connections (`AddConnection`, `UpdateConnection`, `RemoveConnection`) are undoable commands checked with `sceneformat.Validate` and `sceneformat.ValidateConnections` before reaching `History`: an invalid connection (such as naming a handler the target lacks, a duplicate connection, or a wrong param type or out-of-range value) is refused with the reason shown in the status bar, leaving the scene and undo history unchanged. Without a component registry, existing connections remain visible, jumpable, editable and removable.

### Projects

The editor also opens a game's [project file](project.md): `OpenProject` reads `game.yaml` and opens its main scene, `EditProject` changes it as an undoable step, and `SaveProject` writes it with the `nsgo.yaml` beside it in step. `go tool game editor <dir>` opens a project directory. `MoveFile(from, to)` moves a file or directory of the project and rewrites every reference to it, on disk and in the open scene and project, as `go tool game mv` does ([Stable references](project.md#stable-references)), as one step that Undo takes back and Redo makes again. Undo puts each file the move rewrote back byte for byte, unless it changed since, in which case only its references are rewritten back.

While a project is open, the file browser below the asset browser (`FilesView`) lists its files and directories (`ProjectFiles`), leaving out the directories that `game index` and `game mv` skip too: those whose names start with `.` or `_` and those named `testdata`. Pressing one selects it and puts its path in the field below the list; edit the path and press Move to rename or move it through `MoveFile`, press Open Table on a `.yaml`/`.yml` file to open it in the Data Table editor (`OpenDataTableProjectFile`), or press Find Refs to list every reference to it across the project (`ShowReferences`). Refresh lists the files again and rescans the project search and references index (`RefreshSearch`) after something outside the editor changed them.

## Data Table editor (#918)

The Data Table panel (`DataTablePanelView`) opens versioned YAML data tables ([`tooling/datatable`](datatable.md)) from the file browser as a spreadsheet-style grid below the viewport:

- **Columns and kinds**: columns are the union of every row's keys in deterministic first-appearance order (plus any fields declared by a registered Go row type that no row currently sets). Each column's value kind (`string`, `number`, `bool`, `duration`, or `list`) is inferred from its cell values or taken from the registered Go row type; boolean cells render as toggle buttons and scalar/list cells commit on Enter or blur.
- **Row operations and view controls**: add (`AddDataTableRow`), delete (`DeleteDataTableRow`), and reorder (`MoveDataTableRow`) rows, sort by clicking any column header (`SetDataTableSort`), and filter visible rows by case-insensitive substring match (`SetDataTableFilter`). Sorting and filtering affect only the view and never reorder or modify the underlying YAML document.
- **Undo and lossless round-trip**: opening, closing, adding, deleting, reordering, and editing cells all go through the editor's shared `History`. Opening and saving a table without edits leaves the file byte-for-byte identical, and editing a single cell changes only that line while preserving comments, formatting, and each row's key order.
- **Go row-type validation**: a game's own editor command can bind Go struct row types to slash-separated path or filename patterns with `editor.RegisterDataTableType[T](ed, pattern)` (or `Editor.RegisterDataTableRowType`). Matching tables are checked on open and on every cell or row edit with the exact same rules `datatable.LoadYAML` applies, refusing unknown columns, duplicate fields, and invalid cell values in the grid and showing the `*datatable.CellError` in the panel and status bar.

```go
if err := editor.RegisterDataTableType[Enemy](ed, "data/enemies/*.yaml"); err != nil {
	log.Fatal(err)
}
```

### 2D viewport navigation (#864)

The 2D viewport's canvas fills its panel and shows the scene through the editor's own view (`View2D`: the world point at the canvas's center, a zoom and a rotation), separate from the scene's `camera`, so a level larger than one screen can be seen and edited anywhere. The view is never saved to the scene nor recorded in the undo history.

- A mouse wheel, a pinch, or a touchpad scroll with Control or Command held zooms about the pointer, keeping the world point under it fixed (`ZoomView2DAt`), between `MinView2DZoom` and `MaxView2DZoom`.
- A touchpad's two-finger scroll, a mouse wheel's sideways scroll (Shift+wheel or a tilting wheel), a middle-button drag, a drag with Space held or a two-finger drag on a touch screen pans (`PanView2D`).
- F, or the Frame Selection button, frames the selected object (`FrameSelection2D`, which frames everything when nothing is selected); Home, or Frame All, frames every object of the visible layers, their inline tilemaps and the camera's frame (`FrameAll2D`); Mod+0, or Scene Camera View, returns to the scene camera's view (`ResetView2D`).

Until the author pans or zooms, and again after `ResetView2D` or opening another scene, the view is the scene camera's view: the virtual screen centered in the canvas at the camera's zoom and rotation, following the camera as the scene changes (`View2DFollowsCamera`). In edit mode the whole canvas takes the scene's `backgroundColor`, so objects far from the camera are seen against the game's clear color; it outlines the camera's frame, the virtual screen as the player sees it, and draws the camera's `bounds` dashed. Screen-space layers draw inside that frame. In Play mode only the frame shows, centered at the camera's view whatever view the author left, as in the game; the view does not pan or zoom until Stop brings the author's view back.

Hit testing, placing, moving, tile painting, the selection outline and plugin tools all go through the view. `CanvasToWorld` and `WorldToCanvas` convert between canvas points and the world; `CanvasToScreen` and `ScreenToCanvas` between canvas points and the scene's virtual screen, where screen-space layers live; `ScreenToWorld` and `WorldToScreen` apply the scene's camera alone, as the game does. `CanvasSize` is the canvas's laid-out size, the scene's virtual size until the viewport lays out (`SetCanvasSize`); the 3D viewport fills the same canvas.

### 2D transform gizmo (#863)

With the Select or Move tool, the 2D viewport edits an object's transform by dragging, as Godot's and Unity's 2D transform tools do:

- A drag on any object selects it and moves it, keeping the offset from where it was grabbed, so the object does not jump to the pointer.
- The selected object carries a gizmo drawn at a fixed size on the canvas: a square at its origin moves it freely; a red and a green arrow move it along its own X and Y axes, which turn with its rotation; a ring around it turns it about its origin; and, once its outline is at least 24 points on a side on the canvas (zoom in on a small object), handles on its corners and edges scale it about its origin, the edges one axis and the corners both. A handle that sits on or next to the origin's line along an axis it scales is left out, as the top and left handles of a sprite whose origin is its top left corner are, and where the move square and a handle overlap, the square is taken. Holding Shift while scaling keeps the aspect ratio. Dragging a scale handle through the origin mirrors the object with a negative scale.
- Each drag is one undoable step, recorded when it ends ("Move hero", "Rotate hero", "Scale hero"); Undo restores the object's fields exactly, an unset `scaleX` or `scaleY` staying unset, and an edge handle leaves the other axis's field alone. A drag that ends where it began records nothing.
- Escape, a gesture the system cancels or a second finger coming down puts the object back as it was before the drag and records nothing; the rest of that gesture does nothing. Undo, Redo, Play, Save, Delete, Duplicate, or changing the tool or the view mode during a drag cancels it first.
- A scale that would be zero or nearly so (below 0.001), or a scale or rotation that would shear a sprite in the object's tree, its ancestors included (a rotated child under a non-uniformly scaled parent, which `ScaleObject` and `RotateObject` refuse too), is not taken: the object keeps its last valid transform and the status bar says why.
- A nested child carries the gizmo too and is dragged in its parent's space, so it follows the pointer under a turned or scaled parent. An object hidden or locked in the scene tree, or under one that is, has no gizmo and is not dragged, even while it is selected.

Drags map through the object's layer: a world-space layer's objects move by the world distance the pointer travels, whatever the 2D view's or the scene camera's zoom and rotation, and a screen-space layer's by the distance on the virtual screen. A tap still selects, and with the Move tool still puts the selected object where it is pressed. A drag with Space held, or a middle-button drag, pans the view instead.

In Go, `BeginDrag2D(x, y)` starts a drag at a canvas point and returns the handle it took (`GizmoHandle2D`), `UpdateDrag2D(x, y, keepAspect)` follows the pointer, and `EndDrag2D` or `CancelDrag2D` finishes it; `PickGizmo2D` names the handle under a canvas point, `Dragging2D` the one held, and `Gizmo2DShown` says whether the gizmo is drawn.

## Tile palette (#874)

While the Paint, Erase, Fill or Pick tool is active in 2D, the tile palette shows right of the viewport. It shows the tiles of the active layer's tileset as a grid in tile ID order, each at the tilemap's tile size (a tile wider than the panel is scaled down to fit it). The tiles are the tileset atlas's frames whose names are tile IDs (`"1"`, `"2"`, …), as the scene format looks tiles up; other frames are left out. Pressing a tile, or moving between tiles with the arrow keys, makes it the tile Paint and Fill write (`SetActiveTileID`); every arrow key steps through the tiles in ID order, Left and Up to the previous tile and Right and Down to the next, not by rows of the grid. The selected tile is ringed, and with Paint or Fill the status bar shows its ID. Choosing a tile is not an edit and does not go into the undo history; the paint that follows is one undoable step.

The palette follows the active layer: when it changes, the palette shows the new layer's tileset, and if that tileset has no tile with the selected ID, its first tile is selected. A layer with no inline tilemap (none, or an external Tiled or LDtk map), a tileset the resolver cannot resolve and a tileset without tile frames each show a message saying so instead of an empty grid. In Go, `TilePalette()` returns what the palette shows for the active layer: its tileset, tile size and tiles, or the message.

For a layered grid the palette shows the tiles of its YAML tileset, read through the resolver's `sceneformat.FileResolver` (such as `FSAssets`), with IDs from 1 as `PaintTile` takes them: tile ID `n` paints the tileset's local tile `n - 1`. The palette's labels and the status bar name each tile by its local ID, the number the rows and the tileset's `tiles[].id` write (`PaletteTile.Number`). Above the tiles, a toggle per tile layer picks the one Paint, Erase and Fill change (`SetActiveTileLayer`, `ActiveTileLayer`, the first by default), and the H, V and D toggles set the flips Paint and Fill place tiles with (`SetActiveTileFlip`, `ActiveTileFlip`). The edits write the layer's rows in the form `Encode` saves, one undoable step each, and Fill joins cells whose tile and flips both match.

## Painting, erasing and picking tiles (#875)

With Paint or Erase, a drag with the mouse or one finger paints or erases every cell it crosses, however fast it moves, and the whole stroke is one undoable step, so one Undo takes back a row painted in one drag. A click or tap paints or erases its one cell. With Paint, a right-button drag erases. A stroke that starts off the map paints once it reaches it, a stroke that changes nothing records nothing, and a drag with Space held pans the view instead. A second finger cancels a one-finger stroke and puts its cells back, so a two-finger pan never paints.

While a tile tool is active, the cell under the mouse is outlined, snapped to the grid: with Paint and Fill it shows the selected tile faded in the cell, with Erase it is tinted red, and when a press would pick it is outlined in amber.

Picking takes the tile in a cell as the tile to paint, and the palette selects it: Alt+click (or Alt+drag) with any tile tool, or the Pick tool, which then hands over to Paint. An empty cell leaves the selected tile as it was. Picking is not an edit and does not go into the undo history.

In Go, `BeginTileStroke(mode, x, y)`, `ContinueTileStroke` and `EndTileStroke` draw a stroke (`TileStrokePaint`, `TileStrokeErase` or `TileStrokePick`) at canvas points, `CancelTileStroke` puts it back and `TileStrokeActive` reports one under way; painting and erasing refuse locked or hidden layers, and choosing a tool or layer, changing layers, switching views, saving, Undo, Redo and Play end a stroke under way first (while Revert cancels it). `PickTile(layer, col, row)` picks, `SetTileHover` and `ClearTileHover` move the outline and `HoveredTile` returns the cell it is on.

## Scene tree (#861)

The scene tree panel (`SceneTreeView`), at the left of the editor, lists the open scene's layers and, under each, its objects with their children nested, as Godot's Scene dock and Unity's Hierarchy do. It follows every edit and every undo. Pressing an object's row selects it, and selecting an object in either viewport selects its row and opens the rows above it; pressing a layer's row makes it the active layer. Every object edit works on a nested child as on a top-level object: `FindObject` and `SelectObject` find an object by ID anywhere in the tree, the inspector edits the selected child, and its transform is edited in its parent's space, so the Move tool puts its origin under the press and the 3D gizmo moves it along the world axis.

- **Moving in the tree**: drag a row onto another to make it that object's last child, or near the top or bottom edge of the row to put it before or after it; drop it on a layer's row to move it to the top level of that layer. A row's context menu moves it up or down among its siblings, makes it a child of the sibling above, moves it out of its parent, or moves it to another layer. In Go, `ReparentObject(id, parentID, index)`, `MoveObjectToLayer(id, layer, index)`, `ReorderObject(id, index)` and `DropObject(id, targetID, place)` do the same, each as one undoable step; an index below 0 or past the end puts the object last.
- **World transform**: a moved object keeps its world transform. Its local transform is computed again under its new parent in 2D (`x`, `y`, `rotation`, `scaleX`, `scaleY`), and Undo puts every field back exactly; a flipped object keeps its negative scale on the axis it had. When the object or one of its old or new ancestors sets a 3D field, its depth (`posZ`, `scaleZ`) is kept too. When one of them sets a 3D rotation (`rotX`, `rotY`, `rotZ`), the 2D and 3D views no longer agree on where `x` and `y` lead, and only the 3D world transform is kept: the object stays in place in the 3D viewport, and may move in the 2D one. The move is refused when it would put an object under itself or one of its descendants, nest deeper than `MaxObjectDepth`, or leave a transform that cannot be written as a translation, rotation and scale under the new parent, as a sprite under a turned parent stretched along one axis would be. A move between a world and a screen layer keeps the numbers, not where the object appears on screen.
- **Rename**: F2, a double press or the context menu's Rename edits the selected object's name in its row; Return or leaving the field keeps it, and Escape drops it. Change ID edits its ID through `SetObjectID(oldID, newID)`, which rewrites the scene's connections that name the object, and the objects of a scene it instances (`enemy_1/body` becomes `boss/body`), in the same step, as well as the open animation clip's target and the targets of its tracks, and refuses an ID the scene already uses, including one an instanced or inherited scene adds. Another scene file that instances or inherits this one and overrides the object by its old ID is not rewritten; move such references with `game mv` or edit them by hand.
- **Eye and lock**: the eye button of a row hides the object, and with it its descendants, in both viewports, and the lock button stops a press in either viewport from picking it, so the press reaches what lies under it (`SetObjectHidden`, `SetObjectLocked`). Neither viewport moves a hidden or locked object: its 3D gizmo is not shown and the Move tool leaves it where it is. Both belong to the editing session: they are not edits, Undo does not touch them, they are never written to the scene file, Play draws every object, and opening another scene clears them. A hidden or locked object can still be selected from the tree and edited in the inspector.

The 3D viewport draws and picks every object, nested children included: `Objects3D` lists them depth first, each with its parent's ID, and a child's box is placed by its ancestors' transforms.

## Scene files and unsaved changes (#866)

The editor works on one scene at a time and switches between the project's scenes without a restart. Select a scene in the file browser (a `*.scene.yaml` or `*.scene.yml` file, or one listed under `autoload.scenes`) and press Open, or press it twice, to open it (`OpenScene`). Opening a scene, from the file browser, the Problems panel, the Search and References panel, the Command Palette or `OpenScene`, clears `OnSave`, which persisted the scene open before. `OpenFile`, `SaveFile` and `OpenProject` set `ScenePath` when the scene's file lies in the open project and clear it otherwise. The toolbar's File menu holds New Scene, which writes a minimal valid scene (named after its file, as large as the project's display or window, with one world layer) to a path from the project's root that the author gives (`NewScene`), Save As, which saves the scene under another path and makes that file the scene's (`SaveSceneAs`), and Revert, which puts back the scene as its file holds it (`RevertScene`). A path without a `.yaml` or `.yml` extension gets `.scene.yaml` (one ending in `.scene` gets `.yaml`), the name `game check` and `game index` find scenes by. Neither New Scene nor Save As overwrites another file or writes a scene under the project's own file names, `game.yaml` and `nsgo.yaml`. Revert is one undoable step, so Undo brings the reverted edits back.

`SceneDirty` reports whether the scene differs from what it was opened from or last saved to, comparing encodings as `ProjectDirty` does for `game.yaml`, so undoing back to the saved state makes the scene clean again. A `MoveFile` that rewrites the open scene's file leaves it as clean or dirty as it was; one that rewrites references in the open scene but not in its file, as for a scene `game mv` does not read (no `.scene.yaml` name and nothing names it), leaves the scene dirty so that saving it keeps the new paths. The toolbar shows the open scene's path (`SceneLabel`) and a `*` while the scene or project has unsaved edits (`HasUnsavedChanges`), and the window title follows (`WindowTitle`). `EditorView` owns the window title, so an editor built on the package, such as a game's own, names itself with `Editor.AppName` rather than setting the title, and the title ends with that name ("NSGo Scene Editor" while it is empty). The Save button, File > Save in the menu bar, the Command Palette's Save and Mod+S (`Save`, the `CmdSave` command) save what was edited, the project, the scene or both; an unedited file is left as written, comments included. Opening another scene (from the file browser, the Problems panel, a search or reference result or the Command Palette), New Scene, quitting the editor and closing its window ask first while edits are unsaved: Save saves them and goes on, Discard drops them, the project's too, and goes on, and Cancel keeps them and stays. Discarding the project's edits puts back what follows from the project, as undoing them does: an audition through an audio bus the edits added stops, for example. A forced quit, as at logout, cannot wait for an answer and does not ask.

## Layers panel (#862)

The layers panel (`LayersView`), under the scene tree, lists the open scene's layers in draw order, the first drawn at the top, each with its space (`world` or `screen`) and its tilemap: the tileset and size of an inline grid (`tiles 16x12`), the name of the top-level tilemap it refers to, or the path of an external Tiled map. Pressing a layer's row makes it the active layer, which the Place tool and the tile tools work on, the tile palette shows and the status bar names, with `hidden` and `locked` after it when they apply; pressing a layer's row in the scene tree does the same. In Go, `Layers()` returns what the panel shows.

- **Add and remove**: the + button adds a world layer named `layer_N` that draws over every other layer and makes it active (`AddLayer(name, space)`, which takes the highest order of the layers the scene draws, those of a scene it inherits included, and puts the layer last in the file). The bin button removes the active layer (`RemoveLayer(name)`), with its objects and tilemap and with the scene's connections from or to its objects; Undo puts all of them back where they were, and the scene is saved byte for byte as before. When the active layer is removed, the layer that takes its place in the file, or the one before it, becomes active.
- **Rename**: a double press or the context menu's Rename edits the name in the row; Return or leaving the field keeps it, and Escape drops it (`RenameLayer(oldName, newName)`). An empty name and one another layer has are refused. The active layer and the layer's lock follow the new name.
- **Order**: drag a row onto the top or bottom half of another to put the layer before or after it, or use the context menu's Move Layer Up and Move Layer Down (`MoveLayer(name, index)`, where index is the position in the draw order). The layer moves in the file next to the layer it is to draw after or before, and its `order` changes only when the value it has would not draw it there, to that neighbour's value; the other layers keep their place and their orders.
- **Space**: the context menu's World Space and Screen Space set the layer's `space` (`SetLayerSpace(name, space)`); its objects keep their coordinates.
- **Visibility**: the eye button hides or shows the layer by writing its `visible` field (`SetLayerVisible(name, visible)`). Hiding writes `visible: false`; showing leaves the field unset, its default, so hiding and showing a layer gives back the same file. This is the scene's own setting, which the game reads too, and an undoable edit. A hidden layer is not picked in either viewport, and its objects are not moved there even when they are selected (no 3D gizmo, no Move-tool drag), as for an object hidden in the scene tree.
- **Lock**: the lock button locks the layer against the viewports for the session (`SetLayerLocked(name, locked)`): a press passes over its objects to what lies under them, its objects are not moved there (no 3D gizmo, no Move-tool drag), and the Place, Paint, Erase and Fill tools change nothing while it is the active layer and say so in the status bar. Like the scene tree's object lock, it is not an edit, is not undone, is never written to the scene file, and opening another scene clears it; the panels and the inspector still edit the layer and its objects.

In a scene that inherits another (`inherits`), a layer named like one of the base's extends it, so its name, order, space and visibility belong to the base and to the scene's `layerOverrides`: renaming, moving, hiding or setting the space of such a layer, renaming a layer to a base layer's name and adding one with it are refused with an error that says so, and the panel turns off the eye button, dragging and those menu items on its row, while removing it removes only the objects the scene adds to it. Other scene files that inherit the open one and name its layers are not rewritten by a rename. Every layer edit that would leave a scene that does not validate, or does not resolve with the scenes it instances or inherits, is refused before it reaches the history.

## Animation editor

The editor's animation panel keys any registered property on a timeline and saves the clip as an [`animation` resource file](resources.md#animation), played in the game by [animclip](animclip.md) (an AnimationPlayer equivalent). The panel shows below the viewport while a clip is open.

- **Opening a clip**: the toolbar's Animate button starts a new one-second clip on the selected object. In Go, `NewClip(target, name, length)` does the same, `NewClipFromAsepriteTag(target, tag)` builds a clip matching an Aseprite tag's frames, exact durations and loop setting from the target object's atlas, `NewClipFromFrames(target, name, frames, fps, loop)` builds a flipbook clip from a run of atlas frames at a fixed frame rate, `OpenClipFile(path, target)` and `OpenClipYAML(data, target)` open a clip file, checked against the component registry (a misspelled property fails to open with the file name), and `CloseClip` closes it. The panel's Flipbook row also lets you pick an Aseprite tag (From Tag) or a frame rate (From Frames) directly when the target object has a sprite atlas. `go tool game editor scene.yaml clip.animation.yaml [object-id]` opens a scene and selects the object `object-id` (the scene's first object when it is left out), opens the clip on it when the file exists, and the panel's Save Clip button writes that file.
- **Keying**: pick a property (an object field such as `x`, `alpha` or `sprite.frame`, or a registered component field such as `body.speed`) and press Key to key the value it has in the scene at the playhead; `KeyProperty(target, property, at)` does the same. Choosing `sprite.frame` reveals a frame picker listing the target object's atlas frames (`ClipTargetFrames`) so you can key any frame directly (`KeySpriteFrame(target, frame, at)`). With another object selected, the key goes to a track that targets it. A track that names the clip's own object is the same track as one with an empty target, and a clip with two tracks driving the same property of one object is refused. A bool key holds its value until the next key, and `sprite.frame` always steps discretely without interpolation.
- **Editing keys**: each track is a lane of its keys; a `sprite.frame` track displays each keyed frame as a thumbnail cell spanning its duration (`ClipFrameEntry`). Press a key to select it and move the playhead to it, then edit its time and value/interpolation (`linear`, `smooth` or `step`) for numeric tracks, or its atlas frame (`SetKeyFrame`) and duration (`-10ms` / `+10ms`, `FrameDuration`, `SetFrameDuration`) for `sprite.frame` tracks, or delete it; `SetKeyValue`, `MoveKey`, `SetKeyInterp`, `DeleteKey` and `RemoveClipTrack` do the same in Go. Values are checked against the property: 0 or 1 for a bool, a whole number for an int, inside the field's range, and a frame name present in the target's atlas for `sprite.frame`.
- **Clip settings and events**: the panel edits the clip's name, length and loop (`SetClipName`, `SetClipLength`, `SetClipLoop`), and adds named events at the playhead (`AddClipEvent`, `DeleteClipEvent`).
- **Scrubbing and preview**: the scrubber moves the playhead (`SeekClip`; at the end of a looping clip it shows the last keys) and Play plays the clip on a `game.Loop` (`PlayClip`, `PauseClip`, or `StepClip` to step it by hand). The viewport, in 2D and 3D, then shows a copy of the scene posed by the clip with the same `animclip.Player` and `BindScene` the game uses, so the scene being edited never takes animated values and a clip plays the same in the game. A clip the scene cannot play, such as one whose track targets a deleted object, stays open and `ClipError` says why.
- **Undo**: every clip edit, opening and closing included, goes through the same undo/redo history as scene edits.

`SaveClipYAML` and `SaveClipFile` write deterministic YAML, so a clip that is opened and saved again does not change.

## Editor plugins (#492)

A plugin extends the scene editor with its own panels, an inspector for its own component types and 2D viewport tools, as Godot's `EditorPlugin` does. Go cannot load code at run time on every platform, so a plugin is Go code built into the editor binary: the binary registers it with `Editor.RegisterPlugin`, and a project turns it on by naming it in its [project file](project.md):

```yaml
editor:
  plugins: [health]
```

A registered plugin the open project does not name stays off, and the order of `editor.plugins` is the order its panels and tools appear in. `OpenProject` names in the status bar the plugins the project lists that this binary was built without (`MissingPlugins`). `go tool game editor` registers no plugins, so a game that has its own builds its own editor command:

```go
// cmd/editor/main.go: the game's own editor command.
package main

func main() {
	ed, err := editor.New(nil, atlases) // the game's sprite atlases, as a sceneformat.AssetResolver
	if err != nil {
		log.Fatal(err)
	}
	if err := ed.SetComponentRegistry(health.Components()); err != nil {
		log.Fatal(err)
	}
	if err := ed.RegisterPlugin(healthplugin.Plugin()); err != nil {
		log.Fatal(err)
	}
	if err := ed.OpenProject("."); err != nil {
		log.Fatal(err)
	}
	nsgo.Run(func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgo.WindowSize(1280, 800))
}
```

An `editor.Plugin` has a `Name`, which follows the rule of other names in `game.yaml` (no spaces or control characters), and three lists:

- **Panels** (`Panel`): a view docked left of the viewport (`DockLeft`, after the asset browser), right of the inspector (`DockRight`) or below the viewport (`DockBottom`), under its `Title`. `View(cx, ed, rev)` builds it once when it appears, and the panel stays mounted, with its state, while its plugin is on, however other panels come and go; it reads `rev` inside reactive functions, as the editor's own panels do, to follow every change to the editor's state. A dock without panels takes no space.
- **Inspectors** (`Inspector`): the inspector draws the component `Component` names with `View(cx, ed, rev, objectID)` below the component's title, in place of its generic fields. `Editor.ComponentField` reads a field, with its registered default when the scene leaves it unset, and reports false for an object without the component. Like the generic fields, the inspector's controls should be disabled while `Playing` reports true, and an edit the editor refuses should go to the status bar with `Editor.Report`. When two plugins that are on draw the same component, the one listed first in `editor.plugins` wins.
- **Tools** (`PluginTool`): a button in the tool bar, beside the built-in 2D tools. While it is the active tool (`SetPluginTool`; `Tool` is then `ToolPlugin`), a press on the viewport calls `Press(ed, x, y)` with canvas coordinates (`CanvasToWorld` converts them to the world, `CanvasToScreen` to a screen-space layer), and `Draw`, when set, draws over the scene in edit mode. Choosing another tool, or turning the plugin off, ends it.

A plugin changes the scene only through the editor's methods, such as `SetComponentField`, `PlaceObject` and `MoveObject`, or by executing its own `Command` on `Editor.History`, so every edit it makes is undone and redone with the rest. `History.Group(label, fn)` records the edits `fn` makes as one step that one Undo takes back, and when `fn` fails it undoes them and records nothing; the editor's MCP server ([mcp](mcp.md#editor-tools)) applies each agent request that way. `RegisterPlugin` refuses a plugin without a name or with a name already registered, a panel, inspector or tool missing its ID, title, label, component or function, an ID or component name with control characters, an unknown dock and two panels, inspectors or tools with the same ID or component. Test IDs join the plugin name and the ID with `-` (`plugin-panel-<plugin>-<id>`, `tool-<plugin>-<id>`, `inspector-plugin-<plugin>-<component>`), so plugins used together should have names and IDs that do not join to the same string.

`SetPluginEnabled(name, on)` adds a registered plugin to `editor.plugins` or removes it, as one undoable edit of the project that the toolbar's Save button writes; removing the last plugin removes the `editor` section when nothing else is left in it, including an `editor: {}` the author wrote, and Undo puts it back. `RegisteredPlugins`, `ActivePlugins` and `PluginEnabled` report which plugins the binary has and which are on.

### Shipping an editor with plugins

A game ships its editor as one more command in its own module, beside the game, and runs it with `go run ./cmd/editor` from the project directory. Keep the plugin in a package of its own that only the editor command imports, so the shipped game does not link the editor, and the component types in a package that both import, so the game and the editor check scenes against the same fields:

| Package | Imports | Holds |
| --- | --- | --- |
| the component package | `sceneformat` | the component types (`ComponentType`), a registry for `ParseWith` and `SetComponentRegistry`, and helpers that read a component with its defaults (`ComponentRegistry.Resolve`) |
| the plugin package | `editor` and the component package | `Plugin()`, returning the `editor.Plugin` |
| the game (`main`) | the component package | loads its scenes with `sceneformat.ParseWith` and the registry |
| `cmd/editor` | `editor`, the component and plugin packages | registers the component types and the plugin, then opens the project |

`game.yaml` names the plugin under `editor.plugins`, so the plugin is on whenever the game's editor opens the project. An editor command that should not start without its plugins checks `MissingPlugins` after `OpenProject` and fails when it is not empty; `go tool game editor` on the same project still opens it, names the plugins it lacks in the status bar and shows the generic fields.

[Field Medic](https://github.com/nsgo-dev/game/tree/main/examples/fieldmedic) (`examples/fieldmedic`) is a runnable game laid out this way. Its patients carry a `health` component (package `health`), and its `cmd/editor` registers the `health` plugin (package `healthplugin`): a Ward panel docked right of the inspector that lists every patient's hit points, with a Heal All button that heals them all in one undoable step it executes on `Editor.History`; an inspector for the `health` component with hit point and max fields and a bar of what is left; and an Admit tool that places a patient where the viewport is pressed and outlines every patient while it is active. Its tests open the game's own project in the editor and drive the panel, the inspector and the tool. The editor's own tests hold a smaller `health` plugin as well (`packages/game/editor/plugin_test.go`).

## Library panel (#493)

With `Editor.Library` set to a pack library (`library.Open`), as `go tool game editor -library <index>` sets it, a panel beside the asset browser lists every pack in the library, with its kind, version, description and the version the open project has installed. Its Install, Update and Remove buttons call `InstallPack` and `RemovePack`, which write the pack's files beside `game.yaml`, record the pack and the autoload entries and plugins it adds in the project, and save the project; a registered plugin a plugin pack names turns on at once. They refuse while playing, without a project and while the project has unsaved edits (`ErrProjectDirty`), and they empty the undo history, since the files they write cannot be undone. A pack from a library over https is fetched off the event loop, with its button busy until it arrives (`FetchingPack`). The library format and what an install writes are in [Asset and Plugin Library](library.md).

## Project Settings panel (#907)

While a project is open, the toolbar's **Settings** button (`ProjectSettingsOpen` / `SetProjectSettingsOpen`) toggles `ProjectSettingsPanelView` below the viewport to edit `game.yaml` exclusively through `EditProject`, so every change is validated by `project.Validate`, participates in Undo/Redo, and writes both `game.yaml` and the synced `nsgo.yaml` beside it on **Save** (`SaveProject`):

- **General**: edits `name`, `id`, and `appVersion` (`SetProjectName`, `SetProjectID`, `SetProjectAppVersion`).
- **Window**: edits `window.title`, `width`/`height`, `minWidth`/`minHeight`/`maxWidth`/`maxHeight` limits, `resizable`, and `appearance` (`SetProjectWindowTitle`, `SetProjectWindowSize`, `SetProjectWindowLimits`, `SetProjectWindowResizable`, `SetProjectWindowAppearance`).
- **Display**: toggles the optional `display` section (`SetProjectDisplayEnabled`) and edits its virtual `width`/`height`, `mode` (`fit`, `fill`, `stretch`, `expand`), `integerScale`, `pixelSnap`, and `barColor` (`SetProjectDisplaySize`, `SetProjectDisplayMode`, `SetProjectDisplayIntegerScale`, `SetProjectDisplayPixelSnap`, `SetProjectDisplayBarColor`).
- **Autoload**: selects `autoload.mainScene` (`SetProjectMainScene`) and adds, updates, or removes `autoload.scenes` and `autoload.resources` entries (`AddProjectAutoloadScene`, `UpdateProjectAutoloadScene`, `RemoveProjectAutoloadScene`, `AddProjectAutoloadResource`, `UpdateProjectAutoloadResource`, `RemoveProjectAutoloadResource`) with searchable file pickers populated from the open project's files (`ProjectSceneFiles`, `ProjectResourceFiles`).
- **Physics**: edits `physics.gravity2D`, `physics.gravity3D`, `physics.solverIterations2D` / `solverIterations3D`, and named collision `physics.layers` (`SetProjectPhysicsGravity2D`, `SetProjectPhysicsGravity3D`, `SetProjectPhysicsSolverIterations`, `AddProjectPhysicsLayer`, `RemoveProjectPhysicsLayer`).
- **Editor plugins**: shows every registered plugin (`RegisteredPlugins`) with a toggle switch backed by `SetPluginEnabled`, plus any plugins listed in `game.yaml` that this editor binary lacks (`MissingPlugins`) with an option to remove them.

Input, audio, and export settings have their own panels and are not duplicated here. Any value `project.Validate` refuses displays the validation error next to the offending field without mutating the project or history. Before the first save of the open project (`ProjectCanonicalSavePending`), the panel displays a notice explaining that saving writes `game.yaml` in canonical YAML form, which does not preserve comments.

## Audio bus mixer (#909)

While a project is open, the toolbar's **Mixer** button (`AudioMixerOpen` / `SetAudioMixerOpen`) toggles `AudioMixerPanelView` below the viewport to edit `game.yaml`'s `audio` section directly through `EditProject`, so every edit validates against `project.Validate`, participates in the editor's Undo/Redo history, and writes cleanly on **Save** (`SaveProject`):

- **Master and buses**: adjust the master volume slider (`SetAudioMasterVolume`), add new buses (`AddAudioBus`), rename (`RenameAudioBus`), delete (`DeleteAudioBus`), reorder (`MoveAudioBus`), and adjust each bus's volume slider (`SetAudioBusVolume`). Deleting the last bus when no master volume is set removes the empty `audio` section, and Undo restores it.
- **Per-bus DSP effect chains**: add (`AddAudioBusEffect` / `AddAudioBusEffectKind`), remove (`RemoveAudioBusEffect`), reorder (`MoveAudioBusEffect`), and edit (`SetAudioBusEffect`) all six `project.Effect` types (`lowPass`, `highPass`, `delay`, `reverb`, `gain`, `bitcrush`). Numeric inputs bound parameters to `project.Validate`'s ranges (with `ClampAudioEffect`), and Go API calls refuse out-of-range values without altering the project or history.
- **Live audition**: choose any audio file from the open project (`ProjectAudioFiles`, `SelectAudioFile`) and press **Audition** (`AuditionAudioBus`, `StopAudition`, `SetAuditionLoop`) to play it through the selected bus on an editor-owned `sound.Mixer` configured with `project.File.ConfigureMixer`. Volume changes apply live to the playing stream without restarting it, and effect chain edits or Undo/Redo immediately re-process through `sound.Mixer.ProcessSource` so changes are heard identically to runtime game playback.

## Input map editor (#908)

While a project is open, the toolbar's **Input** button (`InputMapOpen` / `SetInputMapOpen`) toggles `InputMapPanelView` below the viewport to edit `game.yaml`'s `input` section directly through `EditProject` with full Undo/Redo and validation against `project.Validate`:

- **Actions and bindings**: add (`AddInputAction`), rename (`RenameInputAction`), and delete (`DeleteInputAction`) actions, and add (`AddInputBinding`), edit (`SetInputBinding`), or remove (`RemoveInputBinding`) bindings of every `game/input` kind: `key`, `keyCode`, `gamepadButton`, `mouseButton`, `touchRegion`, and `mouseRegion`. Renaming an action atomically updates every axis in `input.axes` that reads it so references stay consistent across Rename, Undo, and Redo.
- **Key capture**: pressing **Press a key** (`BeginKeyCapture` with `project.KindKey`) or **Press code** (`project.KindKeyCode`) enters capture mode and binds the next hardware key press (`CaptureKeyEvent`) directly to the selected action, with **Cancel** (`CancelKeyCapture`) to back out without changing the project.
- **Regions over virtual display size**: the region canvas scales to the project's `display` resolution (falling back to `window` when `display` is unset; `InputMapDisplaySize`), draws all `touchRegion` and `mouseRegion` bounds, and lets you drag existing regions to move them, drag a selected region's bottom-right handle to resize it, or drag on empty space to draw a new region (`BeginRegionDrag` / `UpdateRegionDrag` / `EndRegionDrag` / `SetInputRegion`). Each drag commits as one undoable `EditProject` step.
- **Axes, motion axes and gamepad sticks**: the **Axes** tab edits action axes (`AddInputAxis`, `SetInputAxis`, `DeleteInputAxis`), device motion sensor axes (`AddInputMotionAxis`, `SetInputMotionAxis`, `DeleteInputMotionAxis`), and controller analog stick axes (`AddInputGamepadAxis`, `SetInputGamepadAxis`, `DeleteInputGamepadAxis`). `BuildInputActions` binds the open project's map into a live `*input.Actions` via `project.File.BindInput`.

## Localization panel (#917)

The Localization panel (`LocalizationPanelView`) edits per-locale YAML string tables side by side below the viewport, with message keys as rows and project locales as columns.

- **Opening**: press the toolbar's **Strings** button (which opens `<project>/locales` when a project is open, or starts an in-memory `"en"` table otherwise), or call `OpenLocalizations(dir)` / `NewLocalizations(dir, locales...)` in Go. `CloseLocalizations` closes the panel.
- **Keys and locales**: add or remove locale columns (`AddLocalizationLocale`, `RemoveLocalizationLocale`) and message key rows (`AddLocalizationKey`, `RemoveLocalizationKey`), normalizing locale tags to canonical BCP-47 form (for example `zh_hans` -> `zh-Hans`).
- **Simple strings and CLDR plurals**: toggle a key as plural (`SetLocalizationKeyPlural`) to edit all six CLDR categories (`zero`, `one`, `two`, `few`, `many`, `other`) via `SetLocalizationPlural` or `SetLocalizationPluralForm`, or edit simple strings via `SetLocalizationString`.
- **Missing translations**: cells whose translation is empty (or whose plural `other` fallback is empty) are highlighted with a warning border and `Missing` indicator; `LocalizationMissing`, `LocalizationMissingCount`, and `LocalizationIsMissing` report them programmatically. Untranslated keys are omitted when saved so runtime `l10n.Catalog` lookups fall back along the BCP-47 chain.
- **Undo and saving**: every localization edit goes through `Editor.History` (Undo/Redo). **Save Strings** (`SaveLocalizations` / `SaveLocalizationsTo`) writes each locale's canonical YAML file to `<dir>/<locale>.yaml` using `l10n.EncodeYAML` and removes files for locales that were deleted from the workspace.

## Problems panel (#913)

While a project is open, the Problems panel (`ProblemsView`) below the viewport runs `assetcheck.CheckWith` (using the editor's component registry) and `project.Check` over the project directory off the UI goroutine, and reports the live problem count in the status bar (`Problems: N`). It re-checks automatically when a project opens (`OpenProject`), after saving a scene (`SaveFile`), project (`SaveProject`), or animation clip (`SaveClipFile`), after moving files (`MoveFile`, including undo and redo), and when the component registry changes (`SetComponentRegistry`). The panel's Check button (`CheckProblems`) re-runs the check on demand after edits made outside the editor.

Each row displays a problem's file, 1-based line (when known) and message, matching `go tool game check` on the same project plus any project-level issues reported by `project.Check` (such as an `nsgo.yaml` out of sync with `game.yaml`, a missing installed pack file, or an invalid non-`.scene.yaml` autoloaded scene). Pressing a row (`OpenProblem`) opens the scene when the file is a scene (`*.scene.yaml`, `*.scene.yml`, or listed under `autoload.scenes`), or selects the file in the file browser (`SelectedFile`, `SelectFile`) when it is another project file or a scene that fails to parse. A problem in the scene already open selects it without reloading it, and opening another scene while edits are unsaved asks to save, discard or cancel first, as the file browser does. In Go, `Problems`, `ProblemCount`, `ProblemErr`, `CheckingProblems` and `WaitForProblems` inspect and wait on background checks safely from any goroutine.

## Export and release builds (#916)

While a project is open, the toolbar's **Export** button (`ExportPanelOpen` / `SetExportPanelOpen`) toggles `ExportPanelView` below the viewport to edit `game.yaml`'s `export` section and run release builds directly from the editor:

- **Presets and output directory**: edit `export.output` (`SetExportOutput`) and add, remove, or reorder per-platform presets (`AddExportPreset`, `RemoveExportPreset`, `MoveExportPreset`) with targets chosen strictly from each platform's valid `project.TargetsOf(platform)` (`SetExportPresetTargets`, `ToggleExportPresetTarget`). Every change goes through `EditProject` (`project.Validate` + Undo/Redo), clears an empty `export` section automatically, and writes canonical YAML plus the synced `nsgo.yaml` on **Save** (`SaveProject`), matching `gameproject targets`.
- **Credits pre-flight**: whenever the panel opens, refreshes (`CheckExportCredits`), or starts a build, it runs the same `tooling/credits` scan that `game release` uses. Any asset without a credit is listed with its expected `*.import.yaml` sidecar path and blocks building until credited; once credited, refreshing clears the warning and successful builds write `CREDITS.md` beside the packages.
- **Builds, dry runs, and Output stream**: build all configured presets or toggle specific targets (`ExportSelectedTargets`), optionally selecting a distribution channel (`SetExportChannel`), release notes file (`SetExportReleaseNotes`), or **Dry Run** (`SetExportDryRun`). `StartExportBuild` / `RunExportBuild` share the release command planner with `go tool game release`, so dry-run builds stream the exact same `nsgo build` command line into the panel's Output view off the UI goroutine, and successful non-dry-run builds open the resolved output folder (`OpenOutputFolder` / `OpenExportOutputFolder`).
## Project search and Find References (#914)

While a project is open, the file browser's **Search** button (`SearchOpen` / `SetSearchOpen`) toggles the Search & References panel (`SearchPanelView`) below the viewport to jump anywhere in the project or answer "what uses this file?":

- **Project-wide search (`Search`, `SetSearchQuery`, `SearchResults`)**: indexes every project file (following the same walk rules as `ProjectFiles` and `refs.Scan`) and parses every scene file (`*.scene.yaml`, `*.scene.yml`, autoloaded scenes, and scenes referenced by other files). Case-insensitive queries (including space-separated multi-term queries) match file names and paths (`SearchFile`), file UIDs from file headers or `game.index.yaml` (`SearchUID`), scene titles, and inside scenes: object IDs and display names (`SearchObject`, including nested children), component types (`SearchComponent`, including overrides), prefab names (`SearchPrefab`), and instanced or inherited scene paths (`SearchSceneRef`).
- **Jump-to-result (`OpenSearchResult`)**: activating a result for a non-scene file selects it in the file browser (`SelectedFile`). Activating a result for a scene file opens that scene (setting `ScenePath` and `SelectedFile`) and selects the matching object (`SelectedID`), nested children included, falling back to its top-level ancestor when the child cannot be selected. If the scene is already open, it selects the object in place without reloading from disk or discarding unsaved edits and undo history.
- **Find References (`FindReferences`, `ShowReferences`, `ShowSelectedFileReferences`, `References`, `OpenReference`)**: selecting any file or directory in the file browser and pressing **Find Refs** queries `refs.Scan` for every incoming reference, listing each referring file, 1-based line, field, and written value in the exact order `refs.Scan` reports them. Pressing a reference row opens the referring scene (and selects the top-level object owning that line when applicable) or selects a non-scene referring file in the file browser.
- **Live index updates (`RefreshSearch`)**: edits to the currently open scene (`PlaceObject`, `SetObjectName`, `DeleteObject`, Undo/Redo) immediately update active search results in memory; saving scenes, projects, clips, data tables, or localizations, installing/removing packs, and moving files (`MoveFile`, including Undo/Redo, which also follows `ReferencesTarget`) automatically invalidate and refresh the index; external edits on disk are detected via a lightweight directory fingerprint or refreshed explicitly via **Refresh** (`RefreshSearch`).
## Running the game and Output panel (#910)

Alongside the existing in-editor **Play** preview (`Play`, `PlayIn`, `Stop`), the editor can launch the open project itself on desktop via `go tool game dev` and stream its logs into a docked Output panel (`OutputPanelView`) below the viewport:

- **Run and Stop**: press the toolbar's **Run** button (or **F5**) to launch `go tool game dev` in the open project directory (`RunGame` / `RunGameWith`). Enable the **Current Scene** toggle (`SetRunStartAtOpenScene`)—or pass `RunGameOptions{StartAtOpenScene: true}`—to pass `-scene <path>` and `NSGO_GAME_SCENE` (`hotreload.SceneEnv`) for the currently open scene. While the game is running, the button switches to **Stop Game (Running)** (**Shift+F5** / `StopGame`), which terminates the child process and its entire process tree cleanly (via its own process group on Unix and `taskkill /T /F` on Windows). Unmounting `EditorView`, switching projects (`OpenProject`), or calling `Close()` also stops any running game cleanly. Custom tools and tests can inject `Editor.RunGameCommand`.
- **Output / Log panel**: docked below the viewport (`OutputPanelView`, toggleable via the toolbar's **Output** button or `SetOutputPanelOpen`), streaming the running game's standard output (`OutputStdout`) and standard error (`OutputStderr`) live off the UI goroutine alongside a timestamped history of the editor's own status and error messages (`OutputEditor`). Consecutive editor errors stay in the log instead of overwriting one another.
- **Filter, Clear, and `file:line` navigation**: toggle **Errors** (`SetOutputErrorsOnly`) to show only error entries, press **Clear** (`ClearOutput`) to empty the log, and click any log line carrying a `file:line` location (`OpenOutputEntry`) to open that file in the editor when it is a scene (`*.scene.yaml`, `*.scene.yml`, or listed under `autoload.scenes`) or select it in the file browser otherwise.
## Keyboard shortcuts, menu bar, and command palette (#920)

Every common editor action lives in a single command registry (`Editor.Commands`, `Editor.RegisterCommand`, `Editor.ExecuteCommand`) that drives keyboard shortcuts, the desktop application menu bar, and the Command Palette from one source of truth:

- **Platform shortcuts and text-field guard**: shortcuts use NSGo's `"mod"` modifier (Command on macOS, Ctrl on Windows and Linux; formatted via `FormatShortcut`) and are bound with `core.KeyNotEditing` and `core.MenuNotEditing` so typing or pressing Delete/Backspace/Undo/Save while any text field has focus (such as the inspector's Name field or a search box) never triggers editor shortcuts:
  - File & Edit: `mod+s` (Save), `mod+z` (Undo), `mod+shift+z` (Redo), `mod+d` (Duplicate), `delete` / `backspace` (Delete selected object).
  - 2D Tools: `v` (Select), `m` (Move), `p` (Place), `b` (Paint Tile), `e` (Erase Tile), `g` (Fill Tile).
  - 3D Gizmo & View: `w` (Translate), `r` (Rotate), `s` (Scale), `alt+2` (2D View), `alt+3` (3D View), `f` (Frame Selection, framing the independent 2D `View2D` via `FrameSelection2D` without mutating `Doc.Camera` and `Camera3D` in 3D), `mod+k` (Command Palette).
  - Project: `f5` (Play / Stop), `mod+r` (Run Game via `Editor.OnRunGame`, falling back to play-in-editor when a scope is attached), `mod+,` (Project Settings), `mod+shift+e` (Export & Release), `mod+shift+b` (Check Problems).
- **Desktop menu bar**: `EditorView` registers the native application menu bar (`File`, `Edit`, `View`, `Project`) through `desktop.SetApplicationMenu` (`Editor.ApplicationMenu`), exposing the same registered commands with live enabled/checked states, formatted shortcut hints, active plugin tools under `View > Tools`, and active plugin commands under `Project > Plugins`.
- **Command Palette (`ui.CommandDialog`)**: press `mod+k` (or the toolbar's **Commands** button / `View > Command Palette...`) to open `CommandPaletteView`, which lists every built-in and custom command, active plugin commands, active plugin tools (activating them via `SetPluginTool`), active plugin panels (focusing them via `FocusPluginPanel`), and the open project's scenes and files (opening scenes or selecting files via `OpenProjectFile`) with ranked substring and fuzzy matching.
- **Plugin commands**: plugins can declare `Commands []PluginCommand` with an optional `Shortcut`. `RegisterPlugin` normalizes each shortcut (`NormalizeShortcut`) and refuses any shortcut that clashes with a built-in, custom, or other registered plugin command across macOS and Windows/Linux before mutating the editor.

## Asset import settings and rebuilding (#915)

Selecting an image (`.png`, `.jpg`, `.jpeg`, `.webp`), sound (`.wav`, `.ogg`, `.mp3`, `.flac`), font (`.ttf`, `.otf`), or 3D model (`.gltf`, `.glb`) in the file browser (`SelectedFile`, excluding generated target outputs and `*.import.yaml` sidecars) displays its import settings and credit metadata in the Inspector (`InspectorView`):

- **Sidecar and target defaults**: `InspectAssetImport` reads the asset's `*.import.yaml` sidecar when present and overlays it on the matching target's defaults in `game.yaml`'s `assets` section (image `filter`, `format`, `mipmaps`, `srgb`, and sprite-sheet `slice`; audio `format`, `rate`, `channels`, `bitrate`, `peakDB`/`rmsDB` normalization, and `loop`; font `size`, `padding`, and `chars`; model `scale` and `animations`), plus the shared `credit` section (`author`, `license` as an SPDX expression, `source`, and `notice`).
- **Validated undoable edits and field reset**: every change writes the sidecar atomically through `importsettings.Encode` as one undoable `Editor.History` step (`EditAssetImportSettings`, typed `SetImageImport*` / `SetAudioImport*` / `SetFontImport*` / `SetModelImport*` setters, and `SetAssetImportCredit`). **Undo** restores the exact previous sidecar bytes verbatim (or removes a newly created sidecar file), and **Reset** (`ResetAssetImportField`) clears an override back to the target's default, removing the sidecar once no overrides or credit remain.
- **Reimport, Build Assets, and live reload**: the inspector's **Reimport** button (`ReimportAsset`) and the toolbar's **Build Assets** button (`BuildAssets` / `BuildAssetsWith`) run `go tool game assets` off the UI goroutine, streaming stdout and stderr into the docked **Output** panel (`OutputPanelView`). On completion, `ReloadAssets` reloads changed atlases and textures from disk into `Editor.Resolver` (updating `sceneformat.AtlasMap` or existing `*sprite.Atlas` values in place and invoking `AssetReloader` / `OnReloadAssets` when set), refreshes the Asset Browser and Viewport immediately without restarting the editor, and re-runs the Problems check. Custom tools and tests can inject `Editor.AssetsCommandRunner`.
