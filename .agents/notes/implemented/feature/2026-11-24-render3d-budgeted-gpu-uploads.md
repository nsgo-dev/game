# Agent Note: Budgeted cross-frame GPU uploads for render3d

Status: implemented

## Problem

`render3d.UploadMesh` validates, packs, and uploads an entire mesh synchronously on the engine goroutine, and direct `gpu.Device.WriteTexture*` calls upload full images in one frame. When a 3D game streams multi-megabyte models and textures (for example glTF meshes with >65k vertices and mipmapped textures), decoding on the main thread and issuing unbounded GPU transfers in a single `OnFrame` causes visible frame-time spikes (#387). We need a way to decode and pack 3D assets concurrently off-thread and pace GPU buffer and texture writes across frames under a configurable per-frame byte budget without leaking goroutines or partially created GPU resources.

## Decision

`packages/game/render3d/uploader.go` introduces `Uploader`, a dedicated asynchronous decode-and-stream coordinator bound to a `*gpu.Device` and `*core.Scope`:

- **Two-stage pipeline (background decode, engine-thread GPU step)**:
  - `NewUploader(cx, dev, opts)` spawns a fixed worker pool (`Workers`, defaulting to `min(runtime.GOMAXPROCS(0), 4)`, at least 1) fed by a priority min-heap (`decodeHeap`).
  - Worker goroutines run caller decode callbacks plus all CPU-heavy preparation: `Mesh.Validate()`, vertex/index buffer packing (`AppendVertexData` / `AppendIndexData`), image conversion through `PixelBufferFromImage` (straight alpha, as `UploadTexture`), the CPU mip chain through the same `packPixelRows` / `downsamplePixelsBox(..., format.IsSRGB())` path `UploadTexturePixels` uses, and raw texture validation/repacking.
  - The queue methods take the synchronous uploads' own options (`MeshUpload`, `TextureUpload`) plus an `UploadPriority`, and `QueueTexture` / `QueueTexturePixels` deliver a `*GPUTexture`, so a streamed asset is a drop-in replacement for `UploadMesh` / `UploadTexture` (binds with `BindTexture`, counts in `GPUTexture.ByteSize`).
  - Completed CPU staging payloads move into a second priority heap (`uploadHeap`). Calling `Uploader.Step()` once per frame on the engine goroutine pops ready items, lazily allocates GPU resources on first touch, streams chunks up to `BytesPerFrame` (default `DefaultUploadBytesPerFrame` = 4 MiB) in increments of `ChunkBytes` (default `DefaultUploadChunkBytes` = 256 KiB), and invokes `onReady` only once all bytes are uploaded.
- **Deterministic chunked writes across all NSGo GPU backends**:
  - Buffers (`GPUMesh` vertex/index buffers and `RawBufferUpload`) write 4-byte aligned intermediate slices via `Device.WriteBuffer` so WebGPU alignment requirements hold on every chunk.
  - Uncompressed textures write full row bands via `Device.WriteTextureRegion`, and fall back to horizontal sub-row spans (`Height: 1`) when `maxWrite < bytesPerRow`, so even sub-row budgets never exceed `BytesPerFrame`.
  - Block-compressed raw textures write whole block-row bands via `Device.WriteTexture` with block-aligned `OriginY`.
- **Priority preemption, cancellation, and lifecycle**:
  - `UploadPriority` (`PriorityHigh`, `PriorityNormal`, `PriorityLow`) orders both `decodeHeap` and `uploadHeap` with FIFO tie-breaking (`seq`). Because partially uploaded jobs return to `uploadHeap` when a frame's budget runs out, a newly decoded `PriorityHigh` asset preempts an in-progress `PriorityLow` asset on the next frame.
  - `UploadHandle` provides lock-free atomic reads (`State`, `Progress`, `Err`, `Done`) and thread-safe `Cancel()`, which cancels the job's derived `context.Context` immediately and releases any partially allocated GPU resources on the next `Step()` or `Close()`.
  - `SetBytesPerFrame` and `SetChunkBytes` adjust pacing atomically at runtime without restarting workers.
  - When `cx != nil`, `cx.Cleanup` calls `Uploader.Close()`, which cancels the root context, broadcasts to wake idle workers, waits on `wg.Wait()` for zero goroutine leaks, and closes all partially created GPU resources. Already-delivered resources remain owned by `cx` / the caller.

### Invariants

- All `gpu.Device` calls (`CreateBuffer`, `CreateTexture`, `WriteBuffer`, `WriteTexture`, `WriteTextureRegion`) and all asynchronous `onReady` callbacks run on the goroutine calling `Uploader.Step()`, which must also be the one calling `Close` (scope disposal does). Errors a `Queue*` call reports at once (nil decode, closed uploader, full queue) call `onReady` on the caller's goroutine before the call returns.
- Every queued upload's `onReady` is called exactly once: on completion, on failure or cancellation in `Step`, or with `ErrUploaderClosed` from `Close`.
- `UploadFrameStats.BytesUploaded` never exceeds `BytesPerFrame` unless `BytesPerFrame` is smaller than a single minimum GPU transfer unit (4 bytes for intermediate buffer chunks, 1 texel for uncompressed textures, or 1 block row for compressed textures) on a frame where zero bytes have been written yet, which guarantees forward progress without livelock.
- After `Uploader.Close()` returns (or the owning `*core.Scope` is disposed), zero worker goroutines remain running and every `UploadHandle.Done()` channel is closed.

## Alternatives considered

### Why not call `gpu.Device.WriteBuffer` / `WriteTexture` directly from worker goroutines?

Uploading directly from decode goroutines would eliminate the `uploadHeap` handoff and `Step()` loop. However, NSGo's `gpu.Device` contract binds device mutations to the engine goroutine (especially for OpenGL/WebGL backends and frame submission ordering), and uncoordinated background writes cannot enforce a strict per-frame byte budget synchronized with `OnFrame`. Keeping GPU writes inside an explicit `Step()` call preserves single-threaded GPU safety, deterministic frame pacing, and clean `nsgotest.ManualFrames()` testing.

### Why byte-budgeted chunking (`BytesPerFrame`) instead of a wall-clock time budget per `Step()`?

A time budget (for example "stop stepping after 1.5 ms") adapts to faster or slower hardware directly. However, GPU command encoding is asynchronous — `WriteBuffer` and `WriteTextureRegion` copy into driver staging queues quickly while the real GPU/driver transfer cost scales with bytes uploaded — and wall-clock budgets are flaky across CI runners and headless tests. A byte budget (`BytesPerFrame` + `ChunkBytes`) directly bounds staging memory pressure and driver upload work, is 100% deterministic in tests, and still lets games adapt at runtime via `SetBytesPerFrame` based on `FrameProfiler` or loading-screen state.

### Why a separate `Uploader` instead of adding async options to `UploadMesh`?

Extending `UploadMesh` with a callback or channel would keep the API surface smaller for single meshes. However, frame pacing must coordinate budgets and priorities across *all* concurrent mesh, texture, and raw-buffer uploads together rather than per call. A stateful `Uploader` owns the shared worker pool, priority heaps, per-frame budget counter, and scope-bound shutdown cleanly without altering `UploadMesh`'s synchronous contract.

### Why split uncompressed textures horizontally when a single row exceeds the remaining budget?

Restricting texture uploads to whole-row minimum units is simpler and avoids tracking `curX`. However, a 4K RGBA8 texture has a 16 KiB row stride, and small per-frame budgets or leftover budget tails would either stall or overshoot `BytesPerFrame`. Tracking `curX` and issuing `Height: 1` `WriteTextureRegion` spans keeps uncompressed uploads strictly within any `BytesPerFrame >= 4` budget.

## Consequences

- **Benefit:** Large multi-megabyte meshes and mipmapped textures decode off the main thread and stream across frames with strictly bounded per-frame GPU transfer volume and no `OnFrame` spikes.
- **Benefit:** Existing `render3d` types (`GPUMesh`, `MeshVertexLayout`, `Mesh.Validate`, `UploadMesh`) remain untouched; streamed `GPUMesh` and `*gpu.Texture` values render identically to synchronously uploaded ones across both full and `NoGPUCompute` (WebGL 2 / GLES 3) profiles.
- **Cost:** Decoded CPU staging slices stay resident in memory between decode completion and final GPU chunk upload (released immediately once complete, canceled, or closed). Games streaming many huge assets should bound memory with `UploaderOptions.MaxPending`.
- **Cost:** Callers must invoke `Uploader.Step()` every frame on the engine goroutine; forgetting to call `Step()` leaves decoded payloads queued in `uploadHeap`.
- **Cost:** `QueueTexture` supports what `UploadTexture` supports (RGBA8 linear or sRGB, RG8, R8). BGRA or other formats go through `QueueRawTexture`.
- **Limit:** the byte budget bounds the upload work per frame, not its wall-clock time; tests assert the byte cap, not durations.

## Testing

- `packages/game/render3d/uploader_test.go` verifies under `-race`:
  - Pixel-identical rendering of a multi-megabyte streamed mesh (>65k vertices, `uint32` indices) and 512x512 texture vs synchronous `UploadMesh` / `UploadTexture` with strict `BytesUploaded <= BytesPerFrame` per frame.
  - Byte-for-byte parity of every mip level with `UploadTexture` for a translucent `*image.RGBA`, an `RGBA64` image, a paletted image with transparency and sRGB mip chains.
  - A BC1 mip chain streamed in block-row bands under a budget smaller than one mip, and a six-face cube map, read back against single writes.
  - End-to-end `gpu.View` streaming under both full and `nsgotest.NoGPUCompute` (WebGL 2 / GLES 3) capability profiles.
  - Large model (~6.7 MiB mesh + full mipmap chain) multi-frame pacing under the byte cap, with every mip level matching `UploadTexture`.
  - Sub-row horizontal texture splitting when `BytesPerFrame` is a quarter of one row.
  - `NRGBA`, `R8` and padded `RG8` pixel buffers, `QueueRawBuffer`, and padded-stride `QueueRawTexture` uploads.
  - Priority ordering (`High` > `Normal` > `Low` + FIFO) and mid-upload preemption, synchronized on `InFlightDecodes` rather than sleeps.
  - Cancellation while queued, decoding, and mid-upload, plus pre-canceled caller contexts, decode/validation errors, `MaxPending` backpressure, runtime `SetBytesPerFrame` / `SetChunkBytes`, nil-device handling, and mid-upload device loss with retained-mesh recovery.
  - Scope disposal cleanup with every `onReady` called with `ErrUploaderClosed`, idempotent `Close()`, zero leaked goroutines, and high-concurrency producer/canceler/stepper stress testing.
- Verified with `go tool nsgo test ./render3d -- -race`, `go tool nsgo check -target macos,windows,linux,android,web`, `go tool nsgo test -run TestStabilityTable .`, `go run ./cmd/gameschema -check`, `go run ./tools/changelog check`, and `bun run verify-notes`.
