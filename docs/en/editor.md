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

## Scene Format (`version: 1`) (#160)

```go
import "github.com/nsgo-dev/game/packages/game/sceneformat"
```

### Top-level fields

| Field | Type | Description |
| --- | --- | --- |
| `version` | `int` | Required schema version. Must be `1`. |
| `name` | `string` | Optional human-readable scene name. |
| `virtualWidth`, `virtualHeight` | `float32` | Virtual viewport size in points. Must both be `0` or both positive. |
| `backgroundColor` | `string` | Optional CSS color string parsed by `core.ParseColor` (for example `"#141824"`). Filled over `(0, 0, virtualWidth, virtualHeight)` before layers draw. |
| `camera` | object | Optional default 2D camera for world-space layers (`x`, `y`, `zoom`, `rotation`, optional `bounds`). |
| `tilemaps` | map | Optional named tilemaps referenced from layers via `tilemap.ref`. |
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
- `components`: optional `map[string]any` for structured component payloads
- `properties`: optional `map[string]any` for custom metadata

### Example

```yaml
version: 1
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

`Parse` rejects unknown YAML keys, trailing documents, unsupported versions, duplicate layer names or object IDs, invalid spaces, malformed colors, and non-finite or out-of-range transforms. `Encode` validates and emits deterministic 2-space YAML that round-trips through `Parse` without loss.
