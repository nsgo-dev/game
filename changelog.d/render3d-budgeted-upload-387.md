### Added

- `game/render3d`: `Uploader` decodes meshes, textures (with optional CPU mipmap generation), and raw buffers/textures on background goroutines and streams chunked GPU writes across frames in `Step` under a runtime-adjustable `BytesPerFrame` budget, with priority scheduling, `UploadHandle` progress/cancellation, and scope-disposal cleanup (#387).
