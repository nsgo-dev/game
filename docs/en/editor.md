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
2. **Tile painting**: paint inline tile grids or preview referenced Tiled `.tmj` tilemaps.
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
| `inherits` | `string` | Optional slash-separated path from the project root to a base scene this one inherits (version 2, see Instancing and inheritance below). |
| `name` | `string` | Optional human-readable scene name. |
| `virtualWidth`, `virtualHeight` | `float32` | Virtual viewport size in points. Must both be `0` or both positive. |
| `backgroundColor` | `string` | Optional CSS color string parsed by `core.ParseColor` (for example `"#141824"`). Filled over `(0, 0, virtualWidth, virtualHeight)` before layers draw. |
| `camera` | object | Optional default 2D camera for world-space layers (`x`, `y`, `zoom`, `rotation`, optional `bounds`). |
| `tilemaps` | map | Optional named tilemaps referenced from layers via `tilemap.ref`. |
| `layerOverrides` | map | Changes to inherited layers by name: `space`, `order`, `visible`, `tilemap` (version 2, requires `inherits`). |
| `overrides` | map | Changes to inherited objects by ID (version 2, requires `inherits`). |
| `layers` | list | Ordered list of layers drawn lowest `order` first, preserving document order on ties. |

### Camera

- `x`, `y`: world coordinates placed at the center of the virtual viewport `(virtualWidth/2, virtualHeight/2)` (or `(0, 0)` when virtual dimensions are `0`).
- `zoom`: positive zoom factor; defaults to `1` when omitted.
- `rotation`: camera rotation in radians (clockwise on screen); world layers rotate by `-rotation` around `(x, y)`.
- `bounds`: optional `{x, y, width, height}` world rectangle clamping the visible area (`width > 0`, `height > 0`).
- When `camera` is omitted, world layers draw with the identity transform.

### Tilemaps

A tilemap entry (in top-level `tilemaps` or inline on a layer) uses exactly one mode:

- **Reference** (`ref`, layers only): names an entry in top-level `tilemaps`.
- **External Tiled map** (`source`): path to a `.tmj` file resolved at build time through `TilemapResolver`.
- **Inline grid**: `tileWidth`, `tileHeight`, `width`, `height`, `tileset` (atlas name whose frames are keyed `"1"`, `"2"`, …), and row-major `data` of length `width * height` (`0` is an empty tile).

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
- `BuildNodes3D(doc, scene.Root())` builds a `scene3d` node tree: one group node per layer (carrying the layer's visibility), and one node per object named by its ID, with `UserData` pointing at its `ObjectDoc`. `ObjectLocalTransform3D` gives each node's local transform; `rotX`, `rotY` and `rotZ` apply in that order about fixed axes, the same order the editor's 3D viewport uses for a top-level object.

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

Moving, deleting or undoing an object in the editor carries its children along, and the editor never hands out an ID that a nested child already uses. It refuses to place a tree deeper than `MaxObjectDepth` or holding a sheared sprite, and refuses a scale that would shear a child's sprite. The 2D viewport draws nested children through `Build`; the 3D viewport draws and picks top-level objects only, and selecting or editing a nested child in either viewport is not supported yet.

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

In the editor, `PlaceScene(layer, path, x, y)` places an instance of a scene file as an undoable command, with an ID named after the file. The editor's `Resolver` must implement `SceneResolver`; the file is resolved before it is placed, and placing an instance raises a version 1 scene to version 2 (which Undo restores). The viewports select and move an instance by its root object.

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

built, err := sceneformat.Build(doc, sceneformat.AtlasMap{
	"cave_tiles": tilesAtlas,
	"actors":     actorsAtlas,
	"ui":         uiAtlas,
})
if err != nil {
	return err
}

// In a core.Canvas's Draw callback:
built.Draw(d)
```

`Parse` rejects unknown YAML keys, trailing documents, unsupported versions, version 2 fields in a version 1 scene, scene references that are not slash-separated paths from the project root, overrides without the `scene` or `inherits` they change, duplicate layer names or object IDs (nested children included), object trees deeper than `MaxObjectDepth`, invalid spaces, malformed colors, and non-finite or out-of-range transforms. `Encode` validates and emits deterministic 2-space YAML that round-trips through `Parse` without loss.

### Schema

The scene format's JSON Schemas are published at `https://game.nsgo.dev/schemas/scene.v1.json` (`sceneformat.SchemaURL`) and, for version 2 scenes that instance or inherit other scene files, `https://game.nsgo.dev/schemas/scene.v2.json` (`sceneformat.SchemaURLV2`). `Encode` starts every file with the `# yaml-language-server: $schema=…` line for its version (`sceneformat.SchemaCommentFor`), so VS Code's YAML extension and other editors with a YAML language server validate and complete a scene as it is written. See [Schemas for YAML files](schemas.md).

### Component types

A `ComponentRegistry` declares, in Go and without reflection, the component types scene files may use: each type's name and documentation, and each field's name, kind (`FieldBool`, `FieldInt`, `FieldFloat` or `FieldString`), default, optional inclusive range, optional `Step` for the inspector and documentation. `Register` refuses empty or duplicate names, ranges or steps on non-numeric fields, a minimum above the maximum, a fractional range or step on a `FieldInt`, a negative step, and a default of the wrong kind or out of range.

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

In the editor, `Editor.SetComponentRegistry(reg)` checks the open scene, then makes `OpenFile` report component errors with the file and line, and makes `SetComponentField` and `PlaceObject` refuse fields the registry does not allow. Changing the registry clears undo/redo history, so an undone edit the new registry refuses cannot be redone. The inspector lists every registered field of a component in declaration order, including fields the scene leaves unset, which show their default; the label's tooltip gives the field's documentation, kind, range and default, and numeric inputs are bounded by the range and step by the field's `Step` (1 for an int, 0.1 for a float when unset). Floats are shown at full precision, and an edit the registry refuses is reported in the status bar. Components without a registered type, or every component when no registry is set, are shown from the values in the file as before.

### Projects

The editor also opens a game's [project file](project.md): `OpenProject` reads `game.yaml` and opens its main scene, `EditProject` changes it as an undoable step, and `SaveProject` writes it with the `nsgo.yaml` beside it in step. `go tool game editor <dir>` opens a project directory.

## Animation editor

The editor's animation panel keys any registered property on a timeline and saves the clip as an [`animation` resource file](resources.md#animation), played in the game by [animclip](animclip.md) (an AnimationPlayer equivalent). The panel shows below the viewport while a clip is open.

- **Opening a clip**: the toolbar's Animate button starts a new one-second clip on the selected object. In Go, `NewClip(target, name, length)` does the same, `OpenClipFile(path, target)` and `OpenClipYAML(data, target)` open a clip file, checked against the component registry (a misspelled property fails to open with the file name), and `CloseClip` closes it. `go tool game editor scene.yaml clip.animation.yaml [object-id]` opens a scene and selects the object `object-id` (the scene's first object when it is left out), opens the clip on it when the file exists, and the panel's Save Clip button writes that file.
- **Keying**: pick a property (an object field such as `x` or `alpha`, or a registered component field such as `body.speed`) and press Key to key the value it has in the scene at the playhead; `KeyProperty(target, property, at)` does the same. With another object selected, the key goes to a track that targets it. A track that names the clip's own object is the same track as one with an empty target, and a clip with two tracks driving the same property of one object is refused. A bool key holds its value until the next key.
- **Editing keys**: each track is a lane of its keys. Press a key to select it and move the playhead to it, then edit its time, value and interpolation (`linear`, `smooth` or `step`) or delete it; `SetKeyValue`, `MoveKey`, `SetKeyInterp`, `DeleteKey` and `RemoveClipTrack` do the same in Go. Values are checked against the property: 0 or 1 for a bool, a whole number for an int, inside the field's range.
- **Clip settings and events**: the panel edits the clip's name, length and loop (`SetClipName`, `SetClipLength`, `SetClipLoop`), and adds named events at the playhead (`AddClipEvent`, `DeleteClipEvent`).
- **Scrubbing and preview**: the scrubber moves the playhead (`SeekClip`; at the end of a looping clip it shows the last keys) and Play plays the clip on a `game.Loop` (`PlayClip`, `PauseClip`, or `StepClip` to step it by hand). The viewport, in 2D and 3D, then shows a copy of the scene posed by the clip with the same `animclip.Player` and `BindScene` the game uses, so the scene being edited never takes animated values and a clip plays the same in the game. A clip the scene cannot play, such as one whose track targets a deleted object, stays open and `ClipError` says why.
- **Undo**: every clip edit, opening and closing included, goes through the same undo/redo history as scene edits.

`SaveClipYAML` and `SaveClipFile` write deterministic YAML, so a clip that is opened and saved again does not change.
