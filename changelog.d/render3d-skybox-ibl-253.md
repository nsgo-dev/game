### Added

- `game/render3d`: cubemap skybox (`Cubemap`, `CubemapFromImages`, `CubemapFromEquirectangular`, `GradientSkyCubemap`, `GPUCubemap`, `UploadCubemap`, `SkyboxPipeline`, `SkyboxWGSL`) and image-based lighting (`IrradianceSH`, `ComputeIrradianceSH`, `PrefilterEnvironment`, `GenerateBRDFLUT`, `EnvBRDFApprox`, `EvaluateIBL`, `IBLBuffer`, `IBLWGSL`) with retained device-loss recovery and CPU/GPU reference tests (#253).
