### Added

- `game/render3d`: local cubemap reflection probes (`ProbeCubemap`, `ReflectionProbe`, `BoxProjectDirection`, `ReflectionProbeSet`, on-request CPU/GPU baking via `BakeReflectionProbe` and `BakeReflectionProbeGPU`), L2 9-band spherical-harmonic light probes (`SH9`, `BakeSH9FromCubemap`, `LightProbeGrid`, `LightProbeVolume`), and portable GPU probe blending/shading (`ProbeWGSL`, `ProbeRenderer`, `ProbeBinding`) (#384).
