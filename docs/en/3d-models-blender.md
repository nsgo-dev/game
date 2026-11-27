# 3D Models from Blender

Prepare static, skinned, and animated 3D models in Blender 4.x or convert FBX and OBJ assets into glTF 2.0 (`.glb`/`.gltf`) matching NSGo's 3D conventions, verified against two committed round-trip sample models.

> NSGo's 3D rendering layer is Stage 2 of the [overview](index.md) ([nsgo#1059](https://github.com/nsgo-dev/nsgo/issues/1059)). The conventions and glTF 2.0 asset contract on this page let you author, convert, and validate 3D assets ahead of time so they load cleanly without re-exporting.

## NSGo 3D conventions

NSGo adopts the standard glTF 2.0 coordinate system and physical units across every 3D package:

| Property | Convention | Notes |
| --- | --- | --- |
| Handedness | Right-handed | Matches glTF 2.0 and OpenGL/Metal/Vulkan view math |
| Axes | `+X` right, `+Y` up, `-Z` forward | Characters and vehicles face `-Z` in local space |
| Units | Meters (`1.0` = 1 m) | Human character ≈ `1.7`–`1.8` units tall; physics and lighting assume meters |
| Front-face winding | Counter-clockwise (CCW) | Outward normals satisfy `(v1 - v0) × (v2 - v0) · n > 0` |
| Texture UVs | Origin `(0, 0)` at top-left | Standard glTF 2.0 UV layout; Blender's glTF exporter flips V automatically |
| PBR workflow | Metallic-Roughness | `Principled BSDF` base color, metallic, roughness, normal, occlusion, emissive |
| Runtime format | Binary glTF 2.0 (`.glb`) | Single self-contained file with aligned binary buffers; use `.gltf` for JSON inspection |
| Skinning limit | 4 bone influences per vertex | One `JOINTS_0` / `WEIGHTS_0` `VEC4` attribute per vertex, normalized to sum to `1.0` |

### Project layout for 3D assets

Keep runtime `.glb` files inside `resources/` so `nsgo build` bundles them on all six targets and your game loads them via `"asset:..."`. Keep heavy authoring files (`.blend`, `.fbx`, `.obj`, `.psd`) outside `resources/` (for example in `art/`) so they never inflate release packages. A `.gltf` export with its `.bin` buffer and texture files can stay in `art/` too: a model target of [`game assets`](cli.md#assets) packs it into one `.glb` under `resources/`, scaled and with or without its animations as the model's import settings sidecar says:

```text
mygame/
├─ art/                    # authoring sources, not shipped in builds
│  ├─ crate.blend
│  └─ robot_arm.blend
├─ resources/              # packaged with every target ("asset:...")
│  └─ models/
│     ├─ crate.glb
│     └─ robot_arm.glb
└─ nsgo.yaml
```

See [Building and Shipping](shipping.md) for using `mobile.exclude_resources` when desktop and mobile builds ship different LODs or texture sizes.

## Blender 4.x export checklist

Blender uses a right-handed **Z-up** viewport (`+X` right, `+Y` into screen, `+Z` up). When you export with **+Y Up** enabled, Blender's glTF 2.0 exporter rotates the scene `-90°` around X so Blender's `+Z` becomes `+Y` and Blender's `-Y` (Front View, Numpad `1`) becomes **`-Z` forward** in glTF.

### Before exporting (every asset)

1. **Set metric units**: in *Scene Properties → Units*, set *Unit System* to `Metric` and *Unit Scale* to `1.0`. Verify the object's dimensions in meters in the *N* sidebar (`Item → Dimensions`).
2. **Face `-Y` in Blender**: orient characters, weapons, and vehicles so their front faces `-Y` (looking toward you in Front Orthographic View, Numpad `1`). After `+Y Up` export, they face `-Z` forward.
3. **Place the pivot cleanly**: put static props and characters with their base or feet resting at `(0, 0, 0)` so spawning at `y = 0` places them flush on the floor.
4. **Apply transforms**: select the mesh (and armature for rigged assets) and press `Ctrl+A → Rotation & Scale` (plus `Location` for origin-anchored props). Never export with negative scale (`-1`), which flips triangle winding inside out.
5. **Check face orientation**: enable *Viewport Overlays → Face Orientation*. Every visible outside surface must be blue. If any faces appear red, enter Edit Mode, select all (`A`), and run `Mesh → Normals → Recalculate Outside` (`Shift+N`).
6. **Use `Principled BSDF`**: wire textures into `Base Color`, `Metallic`, `Roughness`, `Normal` (via a `Normal Map` node), and `Emission`. Bake procedural Blender shader nodes (Noise, Voronoi, ColorRamp) to image textures before exporting; procedural Blender nodes do not exist in glTF.

### Static meshes (`.glb`)

Open `File → Export → glTF 2.0 (.glb/.gltf)` and verify these settings:

- **Format**: `glTF Binary (.glb)`.
- **Include**: enable `Selected Objects` (or `Active Collection`) so cameras, lights, and guide empties stay out of the asset.
- **Transform**: check **`+Y Up`**.
- **Data → Mesh**:
  - Check `Apply Modifiers` (bakes Subdivision, Mirror, Bevel, and Weighted Normal modifiers).
  - Check `UVs` and `Normals`.
  - Check `Tangents` if the material uses a normal map (avoids tangent-space mismatches across renderers).
  - Uncheck `Vertex Colors` unless your shader reads them.
- **Data → Material**: `Materials: Export`, images as `Automatic` (PNG/JPEG) or `WebP` where supported.
- **Data → Compression**: leave **Draco mesh compression unchecked** while iterating, so the file stays easy to inspect; turn it on for shipped assets when download size matters. `game/gltf` decodes Draco in pure Go (see [Compressed geometry](#compressed-geometry)).

### Skinned meshes and animations

Rigged characters and props need a few extra checks so GPU skinning and animation blending work cleanly:

1. **Single Armature root with unit scale**: apply `Scale = (1, 1, 1)` to both the Armature and the skinned Mesh before exporting. Keep the Armature modifier on the mesh (do **not** apply the Armature modifier).
2. **Enforce the 4-bone influence limit**:
   - Select the mesh, switch to *Weight Paint* mode, and run `Weights → Limit Total` with **Limit = `4`**.
   - Immediately run `Weights → Normalize All` (with *Lock Active* unchecked) so every vertex's weights sum strictly to `1.0`.
   - In the glTF exporter under *Data → Skinning*, check `Skinning` and leave **`Include All Bone Influences` unchecked** so only `JOINTS_0` and `WEIGHTS_0` (4 joints per vertex) are emitted.
3. **Export multiple clips via Actions / NLA**:
   - Give each clip a clean Action name (`Idle`, `Walk`, `Jump`, `Wave`).
   - Push each Action down to an NLA strip in the *Non-Linear Animation* editor so Blender preserves every clip in the file.
   - In the exporter under *Animation*, set *Animation Mode* to **`Actions`** (or `NLA Tracks`), check **`Always Sample Animations`** if the rig uses IK, Copy Rotation, or drivers, and set *Sampling Rate* to `30` or `60` fps (matching your fixed step).
   - Enable `Optimize Animation Size` to drop redundant flat channels while keeping keyframe curves accurate.
4. **Root motion vs in-place locomotion**:
   - Structure your rig with a ground-level `Root` bone at `(0, 0, 0)` parenting `Hips`/`Pelvis`.
   - For **in-place** clips (where Go code moves the entity transform), keep `Root` stationary at `(0, 0, 0)` and strip horizontal `X`/`Z` drift from `Hips`.
   - For **root-motion** clips (such as dodge rolls or lunge attacks), author horizontal translation and yaw rotation exclusively on the `Root` bone. Runtime playback can then sample per-step root deltas and hand them to your movement controller without double-applying displacement.
5. **Clip events (`extras.events`)**:
   - NSGo reads per-clip event markers from `animations[i].extras.events`, an array of `{ "time": <seconds>, "name": "<event>" }` objects:
     ```json
     "extras": {
       "events": [
         { "time": 0.0, "name": "wave_start" },
         { "time": 0.5, "name": "wave_apex" },
         { "time": 1.0, "name": "wave_end" }
       ]
     }
     ```
   - In Blender, add **Pose Markers** to an Action in the *Dope Sheet → Action Editor* (`M` to add a marker, `Ctrl+M` to rename it), then copy them onto `action["events"]` before exporting with **`Include → Custom Properties`** enabled:

```python
import bpy

fps = bpy.context.scene.render.fps / bpy.context.scene.render.fps_base

for action in bpy.data.actions:
    if not action.pose_markers:
        continue
    start = action.frame_range[0]
    events = [
        {"time": round((m.frame - start) / fps, 6), "name": m.name}
        for m in sorted(action.pose_markers, key=lambda item: item.frame)
    ]
    action["events"] = events
```

## Converting FBX and OBJ from other tools

Assets from Maya, 3ds Max, ZBrush, Mixamo, or third-party marketplaces frequently arrive as `.fbx` or `.obj`, often in **centimeters** (`100×` too large) or **Z-up** orientation. Convert them to `.glb` during asset preparation instead of parsing FBX or OBJ at runtime.

### Headless Blender CLI conversion

You can batch-convert `.fbx` and `.obj` files reproducibly in CI or a local script using Blender's headless CLI (`blender --background`). Save the following helper as `tools/convert_to_glb.py`:

```python
import os
import sys
import bpy

argv = sys.argv[sys.argv.index("--") + 1 :]
src_path, dst_path = argv[0], argv[1]
scale_factor = float(argv[2]) if len(argv) > 2 else 1.0  # pass 0.01 for cm -> m

bpy.ops.wm.read_factory_settings(use_empty=True)

ext = os.path.splitext(src_path)[1].lower()
if ext == ".fbx":
    bpy.ops.import_scene.fbx(
        filepath=src_path,
        use_anim=True,
        ignore_leaf_bones=True,
        automatic_bone_orientation=True,
    )
elif ext == ".obj":
    bpy.ops.wm.obj_import(
        filepath=src_path,
        forward_axis="NEGATIVE_Z",
        up_axis="Y",
    )
else:
    raise SystemExit(f"unsupported input format: {ext}")

# Apply centimeter -> meter scale when source tools authored in cm (Maya, Unreal, Mixamo).
if scale_factor != 1.0:
    bpy.ops.object.select_all(action="SELECT")
    for obj in bpy.context.selected_objects:
        if obj.parent is None:
            obj.scale = tuple(s * scale_factor for s in obj.scale)
    bpy.ops.object.transform_apply(location=False, rotation=True, scale=True)

# Normalize skinning weights to 4 bone influences per vertex on any skinned mesh.
for obj in bpy.data.objects:
    if obj.type == "MESH" and obj.vertex_groups:
        bpy.context.view_layer.objects.active = obj
        bpy.ops.object.mode_set(mode="WEIGHT_PAINT")
        bpy.ops.object.vertex_group_limit_total(limit=4)
        bpy.ops.object.vertex_group_normalize_all(lock_active=False)
        bpy.ops.object.mode_set(mode="OBJECT")

bpy.ops.export_scene.gltf(
    filepath=dst_path,
    export_format="GLB",
    export_yup=True,
    export_apply=True,
    export_texcoords=True,
    export_normals=True,
    export_tangents=True,
    export_skins=True,
    export_all_influences=False,
    export_animations=True,
    export_force_sampling=True,
    export_extras=True,
    export_draco_mesh_compression_enable=False,
)
```

Run it from the command line:

```sh
# Convert an OBJ prop authored in meters
blender --background --python tools/convert_to_glb.py -- art/barrel.obj resources/models/barrel.glb

# Convert a Maya / Mixamo FBX authored in centimeters (0.01x -> meters)
blender --background --python tools/convert_to_glb.py -- art/character.fbx resources/models/character.glb 0.01
```

### Handling common source-tool quirks

- **Maya / 3ds Max / Unreal FBX (`cm` → `m`)**: Blender's FBX importer respects the FBX `UnitScaleFactor` header when present, but many marketplace exports leave `UnitScaleFactor = 1` while modeling `180` units tall. Always verify bounding-box height in meters after conversion and pass `0.01` when needed.
- **Z-up OBJ files** (CAD tools, older 3ds Max exports): pass `up_axis="Z"` to `bpy.ops.wm.obj_import(...)` so Blender rotates the mesh upright before exporting with `export_yup=True`.
- **Mixamo multi-clip FBX downloads**: download the character mesh once (`With Skin`), then download additional animations `Without Skin`, import their Armature actions into the same `.blend` file, push each Action to the NLA stack, and export a single `.glb` containing all clips.
- **Standalone inspection & optimization**: after exporting, you can inspect node trees, accessor bounds, and texture sizes with `gltf-transform inspect model.glb` or validate strict spec compliance with the Khronos `gltf_validator`.

## Compressed geometry

`game/gltf` decodes both common glTF geometry compression extensions in pure Go, with no C++ or Wasm decoder, so compressed files load through the same `gltf.Load` and `gltf.LoadFS` calls as plain ones:

| Extension | What it compresses | Typical producer |
| --- | --- | --- |
| `KHR_draco_mesh_compression` | Whole triangle primitives: connectivity plus quantized attributes | Blender's exporter (*Data → Compression*), `gltf-pipeline -d`, `gltf-transform draco` |
| `EXT_meshopt_compression`, `KHR_meshopt_compression` | Individual buffer views: vertex attributes, triangle and index lists, animation keyframes | `gltfpack -c`, `gltf-transform meshopt` |
| `KHR_mesh_quantization` | Integer positions, normals and texture coordinates, usually paired with meshopt | `gltfpack`, `gltf-transform quantize` |

The decoded data is what the readers return: `Primitive.Positions`, `Indices` and the other attributes, `ReadVec3` and friends, animation channels and skins all see uncompressed values. A buffer tagged as a meshopt fallback is never fetched, so a `.glb` that ships its uncompressed fallback as a separate optional `.bin` loads without that file.

Choosing between them:

- **Draco** gives the smallest files for dense static meshes, but decoding is slower and it may change the order and number of vertices, so tools that compare against the uncompressed file must match by value, not by index. Quantization is lossy: Blender's default 14-bit positions are enough for props; check close-ups of large meshes.
- **meshopt** decodes much faster, keeps vertex order, also compresses animation data, and suits skinned characters and scenes with many small meshes. Its filters (octahedral, quaternion, exponential, color) are lossy; plain attribute, triangle and index streams are lossless.

Both decoders charge their output and working memory to the same per-load budget as uncompressed data before allocating it, so a small stream that declares huge counts or many wide attributes fails fast, and malformed streams return an error instead of panicking. An `EXT_meshopt_compression` file may use only what that extension defines: version 0 of the attribute codec and no `COLOR` filter. Other required extensions, such as `KHR_texture_basisu`, are still refused at load time.

## Texture transforms and material variants

`game/gltf` supports both `KHR_texture_transform` and `KHR_materials_variants` (whether listed in `extensionsUsed` or `extensionsRequired`):

- **`KHR_texture_transform`**: parsed on every PBR and material-extension texture slot (`BaseColorTexture`, `MetallicRoughnessTexture`, `NormalTexture`, `OcclusionTexture`, `EmissiveTexture`, `Clearcoat.Texture`, `Clearcoat.RoughnessTexture`, `Clearcoat.NormalTexture`, `Transmission.Texture`) into `TextureRef.Transform` (`*TextureTransform`).
  - Defaults match the Khronos specification (`offset: [0, 0]`, `rotation: 0`, `scale: [1, 1]`), and transforms apply in exact `T * R * S` order in glTF UV space (`+U` right, `+V` down, positive rotation counter-clockwise) via `TextureTransform.Apply`, `TextureTransform.ApplyUV`, `TextureTransform.Matrix`, or `TextureRef.ApplyUV`.
  - When `KHR_texture_transform.texCoord` is present, `TextureRef.EffectiveTexCoord()` returns the overridden UV set instead of `TextureRef.TexCoord`.
  - `Document.PrimitiveTexCoords(prim, set)` returns `TEXCOORD_0`/`TEXCOORD_1` directly and decodes higher sets (`TEXCOORD_2`, …) on demand; `Document.TransformTexCoords(prim, ref, out)` evaluates the final transformed UVs for a primitive and texture slot into a reusable slice.
  - Malformed transforms (non-object payloads, `offset` or `scale` arrays with length other than 2, non-finite numbers, or negative/non-integer `texCoord`) are rejected at load time with `*loaderr.Error` locations.
- **`KHR_materials_variants`**: top-level variant names load into `Document.Variants` (`[]MaterialVariant`), and per-primitive mappings load into `Primitive.VariantMappings` (`map[int]int`).
  - Look up variants by name with `Document.VariantIndex(name)` and resolve materials without mutating `Primitive.Material` via `Document.MaterialForPrimitive(prim, variantIndex)`, `Document.MaterialForVariant(prim, name)`, `Document.MeshMaterials(meshIndex, variantIndex)`, and `Document.ResolveVariantMaterials(variantIndex)`. Any unmapped primitive or out-of-range/negative variant falls back to `Primitive.Material`.
  - Spec constraints are enforced at load time: non-empty `variants` and `mappings` arrays, required variant `name`, valid material and variant indices, and at most one mapping per variant on any primitive.

## Material extensions

`game/gltf` accepts four material extensions in `extensionsUsed` and `extensionsRequired`:

- **`KHR_materials_unlit`**: sets `Material.Unlit`.
- **`KHR_materials_emissive_strength`**: `Material.EmissiveStrength()` returns the multiplier for `EmissiveFactor`, 1 when the extension or its `emissiveStrength` is omitted, and an error when the payload is not an object or the value is not a finite number >= 0.
- **`KHR_materials_clearcoat`**: populates `Material.Clearcoat` (`*Clearcoat`) with `Factor`, `RoughnessFactor`, `Texture`, `RoughnessTexture`, and `NormalTexture` (including `Scale`, `TexCoord`, and `KHR_texture_transform`), rejecting out-of-range factors or invalid texture references at load time.
- **`KHR_materials_transmission`**: populates `Material.Transmission` (`*Transmission`) with `Factor` and `Texture` (including `TexCoord` and `KHR_texture_transform`), rejecting out-of-range factors or invalid texture references at load time.

## Baked lightmaps

Bake lighting in Blender (or another baker) into a second UV map, export it as `TEXCOORD_1`, and point the material at the baked texture with `MOZ_lightmap` (what the Hubs Blender add-on writes), `EXT_lightmap`, or an `extras.lightmap` object. Both extensions are accepted in `extensionsUsed` and `extensionsRequired`.

- `Document.MaterialLightmap(materialIndex)` returns a `*LightmapRef` (nil when the material has no lightmap) with the texture, `Intensity`, the `Encoding` hint (`linear`, `srgb`, `rgbm`, `rgbd`), `RGBMMaxRange`, and the atlas `UVScale` / `UVOffset`.
- `UVScale` / `UVOffset` are the authoritative atlas transform. They come from, in increasing precedence, the lightmap's `KHR_texture_transform` scale and offset, a `scaleOffset` `[sx, sy, ox, oy]` field, and separate `uvScale` / `uvOffset` fields. A rotated lightmap transform is refused. The transform is not left in `LightmapRef.Texture.Transform` (which keeps only a `texCoord` override), so `TransformTexCoords` on it never applies the atlas transform twice.
- `Document.LightmapTexCoords(prim, ref)` returns the lightmap UV set, falling back to `TEXCOORD_0` when `TEXCOORD_1` is missing, and fails when the primitive has neither.

[render3d](render3d.md#baked-lightmaps-lightmapmesh-lightmaprenderer) shows how to draw the result.

## Morph targets

`game/gltf` decodes glTF 2.0 morph targets (`POSITION`, `NORMAL`, and `TANGENT` displacement deltas, including sparse accessors), default `mesh.weights` and `node.weights`, and animated `weights` channels (`STEP`, `LINEAR`, and `CUBICSPLINE`). In Blender, author morph targets as **Shape Keys** on the mesh, keyframe their *Value* sliders in an Action, and enable *Data → Shape Keys* (plus *Shape Key Normals* and *Shape Key Tangents* when needed) in the glTF 2.0 exporter.

At runtime, evaluate morph targets without per-frame heap allocations by reusing weight and `MorphResult` buffers:

```go
weights := gltf.ResolveWeights(doc, nodeIdx) // node.Weights > mesh.Weights > zeros
var morphed gltf.MorphResult

// Each frame:
if sampled, ok := gltf.SampleMorphWeights(doc.Animations[0], nodeIdx, animTime, weights); ok {
	weights = sampled
}
gltf.ApplyMorphWeights(prim, weights, &morphed)
// morphed.Positions, morphed.Normals (renormalized), morphed.Tangents (XYZ renormalized, W preserved), morphed.Bounds
```

- `ResolveWeights` and `ResolveWeightsInto` apply glTF 2.0 weight precedence (`node.Weights` overrides `mesh.Weights`; missing weights default to zeros matching the primitive's morph target count).
- `SampleMorphWeights` unpacks `N` target weights per keyframe (`3 * N` floats per keyframe in `[inTangent, value, outTangent]` order for `CUBICSPLINE`), clamping outside the channel's keyframe time range.
- `EvaluateMorphTangents` blends tangent `XYZ` displacements, renormalizes `XYZ`, and preserves `W` handedness unchanged, complementing `EvaluateMorphPositions` and `EvaluateMorphNormals`.
- To draw an animated morph mesh in an NSGo `gpu.View`, upload the base primitive and targets into `render3d.GPUMorphMesh` (see [`render3d`](render3d.md#morph-target-meshes-morphmesh-gpumorphmesh)). The test suite verifies full animation playback and pixel readback against the Khronos `AnimatedMorphCube` sample model.

## Collision nodes (`-colonly` and `extras.collider`)

Author level colliders directly alongside visual geometry in Blender using Godot-style node name suffixes or Custom Properties (`extras.collider`), and keep collider-only nodes out of visual draws:

| Name suffix | Collider shape | Drawn visually? |
| --- | --- | --- |
| `-col` / `-colonly` | Triangle mesh (`ColliderTriMesh`) | Yes / No |
| `-boxcol` / `-boxcolonly` | Oriented box (`ColliderBox`) | Yes / No |
| `-spherecol` / `-spherecolonly` | Sphere (`ColliderSphere`) | Yes / No |
| `-capsulecol` / `-capsulecolonly` | Capsule (`ColliderCapsule`) | Yes / No |
| `-convcol` / `-convcolonly` | Convex (`ColliderConvex`, currently built as trimesh) | Yes / No |

Suffix matching is case-insensitive and strips Blender's trailing duplicate numbers (such as `Wall-colonly.001`). Nodes can also declare `extras.collider` (as a shape string, `true`/`false`, or an object with `shape`/`type`, `draw`/`only`/`visible`, `trigger`, `tag`, `center`, `halfExtents`/`size`, `radius`, `height`, `friction`, `restitution`, `layer`, and `mask`) — including on meshless Empty nodes.

- **Filter visual draws**: call `doc.DrawableNodes()` (or `doc.SceneDrawableNodes(sceneIdx)` / `node.IsDrawable()`) so `-colonly` and `draw: false` nodes are excluded from rendering.
- **Inspect or build physics colliders**: call `doc.ColliderNodes()` / `gltf.ParseNodeCollider(node)` for raw metadata, or pass `doc` directly to [`physics3d.World.AddGLTFColliders`](physics3d.md#gltf-gridmap-and-heightmap-colliders).

## Round-trip sample models

The [placeholder pack](placeholder.md) holds three minimal, deterministic glTF 2.0 sample models under [`pkg/placeholder/pack/models/`](../../pkg/placeholder/pack/models/), generated and round-trip verified in Go by [`internal/samplegltf/`](../../internal/samplegltf/samplegltf.go). Like the rest of the pack they are dedicated to the public domain under CC0 1.0, and `placeholder.Model` loads them.

### 1. Static binary crate (`crate.glb`)

File: [`models/crate.glb`](../../pkg/placeholder/pack/models/crate.glb) (1,920 bytes, GLB 2.0)

- **Geometry**: `1 m × 1 m × 1 m` cube with pivot at bottom center (`X ∈ [-0.5, 0.5]`, `Y ∈ [0, 1]`, `Z ∈ [-0.5, 0.5]`), right-handed `+Y` up, `-Z` forward.
- **Mesh data**: 24 vertices (4 per face for crisp hard normals), 12 triangles (36 `UNSIGNED_SHORT` indices), strict CCW outward winding, `POSITION` (with exact `min`/`max`), unit `NORMAL`, and per-face `TEXCOORD_0`.
- **Material**: `CrateMaterial` using `pbrMetallicRoughness` (`baseColorFactor: [0.72, 0.52, 0.32, 1]`, `metallicFactor: 0.05`, `roughnessFactor: 0.78`, `doubleSided: false`).
- **Container layout**: 12-byte GLB header (`glTF` magic `0x46546C67`, version `2`, 4-byte-aligned total length), space-padded `JSON` chunk (`0x4E4F534A`), and zero-padded `BIN` chunk (`0x004E4942`).

### 2. Skinned & animated 2-bone arm (`robot_arm.gltf`)

File: [`models/robot_arm.gltf`](../../pkg/placeholder/pack/models/robot_arm.gltf) (embedded base64 buffer, human-readable JSON)

- **Hierarchy**:
  ```text
  ArmRoot (node 0)
  ├─ ArmMesh (node 1: mesh 0, skin 0)
  └─ UpperArm (node 2: skeleton root at 0, 0, 0)
     └─ Forearm (node 3: translation 0, 0.5, 0)
  ```
- **Geometry & skinning**: `0.25 m × 1.0 m × 0.25 m` segmented arm (`X ∈ [-0.125, 0.125]`, `Y ∈ [0, 1]`, `Z ∈ [-0.125, 0.125]`), 32 vertices, 20 triangles (60 indices). The shared elbow ring at `y = 0.5` blends smoothly across both joints (`JOINTS_0 = [0, 1, 0, 0]`, `WEIGHTS_0 = [0.5, 0.5, 0, 0]`), while base and tip rings bind `1.0` to `UpperArm` and `Forearm` respectively.
- **Skin (`ArmSkin`)**: `joints: [2, 3]`, `skeleton: 2`, and `inverseBindMatrices` containing identity `I` for `UpperArm` and translation `T(0, -0.5, 0)` for `Forearm`, satisfying `worldBind × IBM = I`.
- **Animation (`Wave`)**: 1.0-second clip (`times = [0, 0.5, 1]`) with two `LINEAR` quaternion rotation channels (`UpperArm` +15° Z, `Forearm` +60° Z) and three `extras.events` markers (`wave_start` at `0.0 s`, `wave_apex` at `0.5 s`, `wave_end` at `1.0 s`).

### 3. Flat-shaded gem (`gem.glb`)

File: [`models/gem.glb`](../../pkg/placeholder/pack/models/gem.glb) (1,924 bytes, GLB 2.0)

- **Geometry**: a hexagonal bipyramid `0.8 m` across and `1 m` tall with its pivot at its bottom point (`Y ∈ [0, 1]`), its girdle at `y = 0.6`, right-handed `+Y` up.
- **Mesh data**: 36 vertices and 12 triangles, three vertices per face so each face has its own unit `NORMAL`, with CCW outward winding.
- **Material**: `GemMaterial` using `pbrMetallicRoughness` (`baseColorFactor: [0.13, 0.77, 0.45, 1]`, `metallicFactor: 0`, `roughnessFactor: 0.2`).
- **Reproducibility**: the generator uses only exactly rounded arithmetic, so it writes the same bytes on every architecture.

### Running the round-trip verifier

The Go test suite in [`internal/samplegltf/samplegltf_test.go`](../../internal/samplegltf/samplegltf_test.go) unpacks the committed models and verifies:

- GLB magic, version `2`, 4-byte alignment, JSON/BIN chunk bounds, and zero-byte BIN padding
- Embedded data URI base64 decoding and exact buffer byte lengths
- Accessor alignment, `POSITION` `min`/`max` bounds, unit normals, and CCW outward triangle winding on every triangle
- 4-bone `JOINTS_0`/`WEIGHTS_0` bounds and per-vertex weight sums equal to `1.0`
- Exact `worldBind × inverseBindMatrix == Identity` for every joint in `skins`
- Strictly increasing animation timestamps, unit rotation quaternions, and in-range `extras.events`
- Byte-for-byte reproducibility against the deterministic Go generator

Run the tests from the repository root:

```sh
go tool nsgo test ./internal/samplegltf
```

If you edit the generator, regenerate the committed models and re-verify in one command:

```sh
go test ./internal/samplegltf -update
```

## Troubleshooting

| Symptom | Likely cause | Fix |
| --- | --- | --- |
| Model is 100× too large or tiny | Source authored in centimeters or unapplied Object Scale | Set Blender units to Metric (`1.0`), apply `Ctrl+A → Scale`, or convert FBX with `0.01` scale |
| Model lies on its back or faces away from the camera | `+Y Up` disabled on export, or model built facing `+Y` instead of `-Y` | Rotate model so front faces `-Y` in Blender, apply `Ctrl+A → Rotation`, and enable **`+Y Up`** |
| Inside-out or missing faces | Inverted normals or negative scale (`-1`) flipped triangle winding | Apply `Ctrl+A → Scale`, check *Face Orientation* overlay, and run `Shift+N` (*Recalculate Outside*) |
| Skinned mesh explodes or stretches weirdly | Unapplied Armature/Mesh scale, >4 bone weights, or unnormalized weights | Apply scale to mesh and armature, run `Limit Total (4)` + `Normalize All`, uncheck `Include All Bone Influences` |
| Only one animation exports (or none) | Actions are not pushed to NLA tracks, or IK constraints are unbaked | Push every Action to an NLA strip, set *Animation Mode* to `Actions`, and check `Always Sample Animations` |
| Material looks flat white, black, or loses textures | Blender-only procedural nodes or unpacked external image paths | Bake procedural nodes into PBR maps wired to `Principled BSDF` and export as `.glb` |
| Character snaps back to origin when a walk cycle loops | Root motion baked into a clip meant to loop in place | Zero horizontal translation on `Root`/`Hips` for in-place loops, or consume root deltas in gameplay code |
| `extras.events` missing from exported glTF | Custom properties omitted or stored on the Object instead of the Action | Write `action["events"]` on each Blender Action and enable `Include → Custom Properties` on export |
| `gltf.Load` refuses the file | The exporter wrote data the spec forbids: an index past the vertex count, attributes or morph targets with different counts, NaN/Inf vertex data, a `JOINTS_0` value past the skin's joint list, unordered sparse indices, a second GLB JSON chunk, or an `asset.minVersion` above 2.0 | Fix the source mesh (or re-export) and check it with the Khronos `gltf_validator`; the error names the mesh, primitive and element |
| A node's `Translation`/`Rotation`/`Scale` do not match its `LocalMatrix` | The node's authored `matrix` has shear, which TRS cannot express; `Node.MatrixNotTRS` is set | Use `LocalMatrix`, or apply the transform in Blender so the node exports as plain TRS |
