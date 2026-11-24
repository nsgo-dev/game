### Added

- `game/render3d`: GPU skeletal skinning (`SkinnedMesh`, `GPUSkinnedMesh`, `SkinnedVertexLayout`, `SkinnedMeshFromGLTF`, `SkinnedMeshesFromGLTFNode`, `GPUSkinPalette`, `SkinJointMatrices`, `AppendJointMatrices`, `SkinVertex`, `SkinWGSL`, and `NewSkinnedMeshPipeline`) with up to 128 joints in a portable 8 KiB uniform buffer, weight normalization, joint bounds validation, `Retained` device-loss recovery, and Khronos CesiumMan / RiggedSimple GPU readback verification (#121).
