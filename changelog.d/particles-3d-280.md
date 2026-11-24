### Added

- `game/particles`: GPU-friendly 3D particle emitter (`Emitter3D`), 3D spawn shapes (`Point3D`, `Sphere`, `Hemisphere`, `Box`, `Cone`, `Ring3D`), billboard orientation modes (`BillboardSpherical`, `BillboardCylindrical`, `BillboardStretched`), deterministic back-to-front radix depth sorting (`SortByDepth`), instanced GPU uploader and WGSL pipelines (`GPUParticles3D`, `NewParticlePipeline`), and versioned YAML/JSON config serialization for 10k+ particles in a single draw call (#280).
