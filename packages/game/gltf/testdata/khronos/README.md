# Khronos sample models

Unmodified copies of models from [KhronosGroup/glTF-Sample-Assets](https://github.com/KhronosGroup/glTF-Sample-Assets/tree/main/Models), used by the Draco, meshopt, `KHR_texture_transform`, `KHR_materials_variants`, and `render3d` glTF material tests to verify decoded geometry, transformed UVs, variant material selection, and metallic-roughness PBR rendering against Khronos references. Only the `.gltf` and `.bin` files are kept except for `TextureTransformTest` (which keeps `UV.png` and `Arrow.png`) and `DamagedHelmet` (which keeps its unmodified `.gltf` and `.bin` plus its five JPEG textures downscaled to 256×256 for repository size and fast headless tests). Each directory keeps the model's `LICENSE.md`.

| Model | Variants | Credit | License |
| --- | --- | --- | --- |
| Animated Morph Cube | `glTF` | Microsoft, 2017 | CC0-1.0 |
| Avocado | `glTF`, `glTF-Draco` | Microsoft, 2017 | CC0-1.0 |
| Box | `glTF`, `glTF-Draco` | Cesium, 2017 | CC-BY-4.0 |
| Cesium Milk Truck | `glTF`, `glTF-Draco` | Cesium, 2017 | CC-BY-4.0 |
| Damaged Helmet | `glTF` (unmodified `.gltf`/`.bin`, 256×256 JPEG maps) | theblueturtle_, 2016; ctxwing, 2018 | CC-BY-NC-4.0 / CC-BY-4.0 |
| GlamVelvetSofa | `glTF` (`KHR_materials_variants`, `KHR_texture_transform`) | Eric Chadwick, Wayfair, LLC, 2021 | CC-BY-4.0 |
| Meshopt Cube Test | `glTF` (optional extension with fallback buffer), `glTF-Meshopt` | Arseny Kapoulkine, 2025 | CC0-1.0 |
| Morph-Primitives Test | `glTF`, `glTF-Draco` | ft-lab, 2018; Frank Galligan, 2020 | CC-BY-4.0 |
| Rigged Simple | `glTF`, `glTF-Draco` | Cesium, 2017 | CC-BY-4.0 |
| Sunglasses Khronos | `glTF`, `glTF-Draco` | Eric Chadwick, Darmstadt Graphics Group GmbH, 2024 | CC-BY-4.0 |
| Texture Transform Multi Test | `glTF` (`KHR_texture_transform` across PBR slots & UV sets) | Ed Mackey, Analytical Graphics, Inc., 2020 | CC-BY-4.0 |
| Texture Transform Test | `glTF` (`KHR_texture_transform` offset, rotation, scale) | Microsoft, 2018 | CC0-1.0 |
