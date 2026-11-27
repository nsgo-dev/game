# Khronos sample models

Unmodified copies of models from [KhronosGroup/glTF-Sample-Assets](https://github.com/KhronosGroup/glTF-Sample-Assets/tree/main/Models), used by the Draco, meshopt, `KHR_texture_transform`, and `KHR_materials_variants` tests to verify decoded geometry, transformed UVs, and variant material selection against Khronos references, and by `render3d`'s skinning tests (`Cesium Man`, `Rigged Simple`), unlit reference render (`Unlit Test`), and metallic-roughness vertex-color test (`Vertex Color Test`). Only the `.gltf` and `.bin` files are kept except for `TextureTransformTest` (`UV.png`, `Arrow.png`) and `VertexColorTest` (`VertexColorChecks.png`, `VertexColorTestLabels.png`), which also keep their textures to verify texture sampling directly. Each directory keeps the model's `LICENSE.md`.

| Model | Variants | Credit | License |
| --- | --- | --- | --- |
| Animated Morph Cube | `glTF` | Microsoft, 2017 | CC0-1.0 |
| Avocado | `glTF`, `glTF-Draco` | Microsoft, 2017 | CC0-1.0 |
| Box | `glTF`, `glTF-Draco` | Cesium, 2017 | CC-BY-4.0 |
| Cesium Man | `glTF` (texture `CesiumMan_img0.jpg` left out) | Cesium, 2017 | CC-BY-4.0 |
| Cesium Milk Truck | `glTF`, `glTF-Draco` | Cesium, 2017 | CC-BY-4.0 |
| Emissive Strength Test | `glTF` (`KHR_materials_emissive_strength`, with `PlainGrid.png`) | Ed Mackey, Analytical Graphics, Inc., 2022 | CC-BY-4.0 |
| GlamVelvetSofa | `glTF` (`KHR_materials_variants`, `KHR_texture_transform`) | Eric Chadwick, Wayfair, LLC, 2021 | CC-BY-4.0 |
| Meshopt Cube Test | `glTF` (optional extension with fallback buffer), `glTF-Meshopt` | Arseny Kapoulkine, 2025 | CC0-1.0 |
| Morph-Primitives Test | `glTF`, `glTF-Draco` | ft-lab, 2018; Frank Galligan, 2020 | CC-BY-4.0 |
| Rigged Simple | `glTF`, `glTF-Draco` | Cesium, 2017 | CC-BY-4.0 |
| Sunglasses Khronos | `glTF`, `glTF-Draco` | Eric Chadwick, Darmstadt Graphics Group GmbH, 2024 | CC-BY-4.0 |
| Texture Transform Multi Test | `glTF` (`KHR_texture_transform` across PBR slots & UV sets) | Ed Mackey, Analytical Graphics, Inc., 2020 | CC-BY-4.0 |
| Texture Transform Test | `glTF` (`KHR_texture_transform` offset, rotation, scale) | Microsoft, 2018 | CC0-1.0 |
| Unlit Test | `glTF` (`KHR_materials_unlit`) | Ed Mackey, Analytical Graphics, Inc., 2019 | CC-BY-4.0 |
| Vertex Color Test | `glTF` (`COLOR_0` on metallic-roughness materials) | Ed Mackey, Analytical Graphics, Inc., 2018 | CC-BY-4.0 |
