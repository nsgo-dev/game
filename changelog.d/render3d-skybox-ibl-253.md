### Added

- `game/render3d`: cubemap skybox (`Cubemap`, `CubemapFromImages`, `CubemapFromEquirectangular`, `GradientSkyCubemap`, `GPUCubemap`, `UploadCubemap`, `SkyboxPipeline`, `SkyboxWGSL`) and low dynamic range image-based lighting (`IrradianceSH`, `ComputeIrradianceSH`, `PrefilterEnvironment`, `GenerateBRDFLUT`, `EnvBRDFApprox`, `EvaluateIBL`, `GPUIBLBuffer`, `IBLWGSL`, `LitPBRIBLWGSL`, `NewLitPBRIBLPipeline`) with retained device-loss recovery and CPU/GPU reference tests (#253).
