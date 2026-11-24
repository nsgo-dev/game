### Added

- `game/gltf`: full glTF 2.0 morph target evaluation (`MorphResult`, `ApplyMorphWeights`, `EvaluateMorphTangents`), glTF weight precedence (`ResolveWeights`, `ResolveWeightsInto`), and zero-allocation animated weight sampling (`SampleMorphWeights` for `STEP`, `LINEAR`, and `CUBICSPLINE`) verified against the Khronos `AnimatedMorphCube` model (#273).
- `game/render3d`: `MorphTarget`, `MorphMesh`, `UploadMorphMesh`, and `GPUMorphMesh` for dirty-tracked morph target vertex buffer updates, live object-space bounds, `Retained` device-loss restoration, and `MeshVertexLayout` drawing across all GPU view backends (#273).
