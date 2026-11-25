# scenemodel

`game/scenemodel` gives the objects of a [scene file](editor.md) their 3D look, so a 3D level is a text file a person or an AI can edit and review, as a 2D one is, instead of being assembled in Go. An object names a built-in primitive in `mesh`, a glTF 2.0 model in `model`, or a modular 3D grid map in `gridMap`, and `scenemodel.Build` turns the scene into a `scene3d` node tree whose nodes carry their geometry, materials and bounds, ready for `render3d.Queue` or the package's own `Renderer`.

```go
import "github.com/nsgo-dev/game/packages/game/scenemodel"
```

## Fields

```yaml
version: 2
name: Courtyard
layers:
  - name: level
    objects:
      - id: floor
        scaleX: 6
        scaleZ: 6
        mesh: plane
        material: materials/stone.material.yaml
      - id: crate
        x: -1
        "y": 0.5
        rotY: 0.4
        model: models/crate.glb
      - id: lid
        x: 1.5
        "y": 0.1
        model: models/crate.glb
        modelNode: Lid
      - id: ball
        x: 0.9
        "y": 0.5
        mesh: sphere
        material: materials/teal.material.yaml
```

| Field | Meaning |
| --- | --- |
| `mesh` | A built-in primitive: `box` (1 on each side), `sphere` (1 across), `plane` (1 by 1 in the XZ plane, facing +Y), `cylinder` (1 across and 1 tall) or `capsule` (1 across and 2 tall), centered on the object's origin with the round axis along Y, sized like Godot's default meshes so that the object's scale reads as meters. Any other value is refused. |
| `model` | Version 2. The glTF 2.0 model the object draws: a `.gltf` file, whose separate buffers and images are read from beside it, or a `.glb`, named by its slash-separated path from the project root. Without `modelNode` or `modelMesh` the object draws every node of the model's default scene (or of its first scene, or every parentless node when it has none), each as a child node of the object with its glTF transform. It cannot be combined with `mesh`. |
| `modelNode` | Version 2, with `model`. The first node of the model with this name, drawn on the object's node with its descendants as child nodes; the object's transform replaces the node's own. |
| `modelMesh` | Version 2, with `model`, not with `modelNode`. The first mesh of the model with this name, drawn at the object's origin. |
| `gridMap` | Version 2. A modular 3D [`*.gridmap.yaml` file](world3d.md#modular-3d-gridmap), named by its slash-separated path from the project root. `Build` loads the grid map and its glTF `meshLibrary`, calls `world3d.GridMap.BuildInstances`, creates child nodes for each tile instance sharing `MeshID` and `MaterialID`, and sets the object node's `LocalBounds` from `GridMap.Bounds()`. Cannot be combined with `mesh` or `model`. |
| `material` | A [material file](resources.md). On a primitive it is the surface, white, non-metallic and fully rough without one; on a model or grid map it replaces every material of the model or tile library. Its base color and emissive colors are sRGB and are converted to linear light, its opacity multiplies the base color's alpha and blends the surface when below 1, and its `metallic`, `roughness`, `doubleSided` and `texture` are used as written. Its 2D blend mode and `unlit` do not apply in 3D. |
| `occluder` | Version 2. Marks the object as a 3D occluder for `render3d.OcclusionBuffer`: `{ mode: model }` uses the object's `mesh` or `model` geometry (at its coarsest LOD when available), and `{ mode: box, center: [x, y, z], size: [w, h, d] }` uses an explicit box in the object's local space (defaulting to centered `[1, 1, 1]`). Optional `doubleSided: true` rasterizes both faces. |

An override (`overrides`) can replace each of these fields on an object of an instanced or inherited scene. The merged object must still follow the rules above, so an override that turns a primitive into a model or grid map writes `mesh: ""` beside its `model` or `gridMap`, and one that keeps a `modelNode` or `modelMesh` while it changes `model` needs that part in the new model. `scene.v2.json` describes them for editors and AI tools ([schemas](schemas.md)).

## LOD levels and occluders (#904)

`scenemodel.Build` populates `DrawItem.LODs` and extracts occluder geometry so a level authored in YAML gets both level-of-detail switching and software occlusion culling without Go glue code:

- **Naming convention**: sibling glTF nodes or meshes sharing a base name with `_LOD1`, `_LOD2`, … suffixes (parsed by `gltf.ParseLODNodeName`, case-insensitive and ignoring Blender `.001` suffixes) are paired with the base (`_LOD0` or unsuffixed) mesh automatically. Unconfigured thresholds default to `scenemodel.DefaultLODScreenRatio` (`0.25`, `0.10`, `0.04`, `0.015`, with the coarsest level at `0`).
- **Import sidecar (`*.import.yaml`)**: when the resolver implements `ImportSettingsResolver` (as `scenemodel.Assets` does), `Build` reads `<model>.import.yaml` (`importsettings.Model.LODs` and `LODHysteresis`). Each LOD entry sets `minScreenRatio` (projected bounding-sphere diameter / viewport height) and/or `maxDistance`, and may either override thresholds for discovered `_LODn` siblings or point explicitly at a `node`, `mesh`, or separate `.gltf`/`.glb` `model` file.
- **Occluders**: objects with `occluder` become `Built.Occluders`. Call `Built.AppendOccluders(dst)` or `Built.RasterizeOccluders(buf, viewProj)` to feed a `render3d.OcclusionBuffer` directly; `scenemodel.Renderer` automatically rasterizes `Built.Occluders` into an internal `render3d.OcclusionBuffer` in `Prepare` (unless `RendererOptions.DisableOcclusion` is set) and exposes the resulting `render3d.RenderPlan` (including `Stats.Occluded`) via `Renderer.Plan()`.

## Building

```go
assets := scenemodel.NewAssets(os.DirFS(projectRoot)) // or an embed.FS rooted at the project
doc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{File: "scenes/level.scene.yaml"})
if err != nil {
	return err
}
world := scene3d.NewScene()
built, err := scenemodel.Build(doc, world.Root(), assets)
if err != nil {
	return err
}
```

`Build` builds the layers and objects as `sceneformat.BuildNodes3D` does and then gives each object its look. It loads models through a `ModelResolver`, as `sceneformat.Build` loads materials through a `MaterialResolver`; the resolver must also be a `GridMapResolver` when an object names a `gridMap` file, a `sceneformat.MaterialResolver` when an object names a material file, and an `ImageResolver` when that file names a texture. `Assets` implements all four over a project's files: models load with `gltf.LoadFS`, grid maps with `world3d.ParseGridMapWith`, material files through a `resource.Library`, and images decode as PNG or JPEG, each once. A scene that instances or inherits others must go through `sceneformat.Resolve` first. On any error, such as a model that does not load, a `modelNode` it does not have, or a grid map referencing an unknown tile, `Build` returns it with the layer and object and leaves the parent untouched.

The result, a `Built`, holds the layer and object nodes (`Layers`, `Objects`), any built `*world3d.GridMap` instances keyed by object ID (`GridMaps`), and the `Drawables`: every node that draws, an object's node or a node made for a node of its model or an instanced tile of its grid map. Each drawable lists its `Parts`, one per glTF triangle primitive or one for a primitive shape, with the triangle `Mesh` in the node's space and its `Material`. Parts that draw the same geometry share a `MeshID` and parts with the same material a `MaterialID`, so a renderer uploads each once. Each drawing node's `LocalBounds` is set from its geometry, and its `*Drawable` is stored as the attachment `scenemodel.AttachmentKey`, so picking or an editor can find what a node draws.

A model draws in its bind pose with its meshes' default morph weights applied. Triangle strips and fans are unrolled into lists, a primitive without normals is flat-shaded as glTF says, and the base color texture's UV set and `KHR_texture_transform` are applied to the UVs. Skins, cameras, lights, animations, vertex colors, points and lines are not used.

## Drawing

`Built.AppendDrawItems` appends a `render3d.DrawItem` for every part of every visible, enabled node, with the node's world matrix, the part's world bounds and its mesh, material and pipeline IDs, for a game's own `render3d.Queue`. `Built.Part` maps an item's ID back to its drawable and part.

`Renderer` draws a `Built` on a `gpu.Device`: it uploads every mesh and texture once, then each frame `Prepare` writes the camera, a `render3d.LightRig` and the nodes' current transforms and plans the frame with a `render3d.Queue` (frustum culling, opaque parts front to back, blended ones back to front), and `Draw` records it into a render pass with a depth attachment.

```go
r, err := scenemodel.NewRenderer(cx, dev, built, scenemodel.RendererOptions{})
if err != nil {
	return err
}
// Each frame:
if _, err := r.Prepare(cam, lights); err != nil {
	return err
}
pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
pass.ClearColor(core.RGB(0x10141c))
pass.ClearDepth(1)
r.Draw(pass)
pass.End()
```

Its shader (`ShaderWGSL`) is render3d's metallic-roughness lighting with a base color texture, alpha cutouts, double-sided surfaces lit from the side the viewer sees, and blending. Moving, hiding or showing a node changes what is drawn from the next `Prepare`; to change what an object looks like, edit the scene or material file and build again. A game that needs its own shader, such as one with fog, reads the parts and uploads them itself, as the Lantern 3D example does.

`SetShading` switches how `Draw` shades the parts: `ShadingLit`, the default, or `ShadingUnlit`, which draws each surface at its textured base color plus its emissive color and ignores the lights, as an editor's unlit view does; its pipelines are created on the first switch to it. The editor's 3D viewport draws a scene's build with a `Renderer` ([Editor](editor.md#3d-viewport-890)).

## Checking and moving

- `game check` reports a model or grid map that is missing or does not load, and a `modelNode` or `modelMesh` the model does not have, at the scene file and line that names it, including what an override of an instanced or inherited scene changes, at the override; a model's load error, a buffer or image a `.gltf` file names that is missing, or an invalid `meshLibrary` / unknown tile in a `.gridmap.yaml` file is reported against that asset file ([CLI](cli.md)).
- `game mv`, `game index` and the editor's `MoveFile` treat `model`, `gridMap` and a grid map's `meshLibrary` as references: moving a model or grid map rewrites every scene, override and `.gridmap.yaml` file that names it, and moving a `.gltf` file's buffer or image rewrites the `.gltf` file, percent-encoding the new name as glTF requires ([Project File](project.md)).

## Example

[Lantern 3D](https://github.com/nsgo-dev/game/tree/main/examples/lantern3d) loads its level from `assets/shrine.scene.yaml`, which names its glTF diorama, builds it with `Build`, animates two of the model's nodes through the `scene3d` nodes `Build` made for them and draws the parts with its own fog shader.
