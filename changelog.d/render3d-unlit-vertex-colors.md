### Added

- `game/render3d`: add `KHR_materials_unlit` unlit materials (`UnlitMaterial`, `GPUUnlitMaterial`, `UnlitWGSL`, `NewUnlitPipeline`, `EvaluateUnlit`, `UnlitMaterialFromGLTF`, `UploadGLTFUnlitMaterial`) and `COLOR_0` linear RGBA vertex-color meshes (`ColoredMesh`, `GPUColoredMesh`, `ColoredVertexLayout`, `ColoredMeshFromGLTF`) with sRGB fallback textures, alpha mask/blend modes, double-sided culling, and `Retained` device-loss recovery (#260).
