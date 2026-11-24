### Added

- `game/render3d`: `Uploader` decodes and packs meshes, textures and raw buffers or textures on background goroutines and streams their GPU writes across frames in `Step` under a `BytesPerFrame` budget that can change at runtime. `QueueMesh`, `QueueTexture` and `QueueTexturePixels` take the `MeshUpload` and `TextureUpload` options of the synchronous uploads and deliver the same `GPUMesh` and `GPUTexture`; uploads run in priority order, and an `UploadHandle` reports progress and cancels (#387).
