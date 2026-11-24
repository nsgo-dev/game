### Added

- `game/render3d`: local reflection probes (`ReflectionProbe`, `ReflectionProbeSet`, `ProbeBlendResult`, `BoxProjectDirection`, `BakeReflectionProbe`, async `BakeReflectionProbeGPU`) backed by `Cubemap`/`GPUCubemap` and `PrefilterEnvironment`, plus light probes (`LightProbe`, `LightProbeGrid`, `LightProbeVolume`, `ComputeIrradianceSHFromFunc`) backed by `IrradianceSH`, wired into `LitPBRWGSL` at `IBLGroup` via `ProbeIBLUniform`, `GPUProbeIBLBuffer`, `ProbeIBLWGSL`, `LitPBRProbeIBLWGSL`, `NewLitPBRProbeIBLPipeline`, and CPU reference `EvaluateProbeIBL` (#384).
