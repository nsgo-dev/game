# Agent Note: Bound DecodeHDR allocations against crafted or truncated .hdr headers

Status: implemented

## Problem

`render3d.DecodeHDR` in `pkg/render3d/ibl_hdr.go` (introduced in [HDR environment cubemaps, textured glTF material IBL, and DamagedHelmet reference](../feature/2026-11-25-render3d-ibl-hdr-damaged-helmet.md)) previously called `NewHDRImage(width, height)` immediately after parsing the Radiance resolution line and before reading any scanline data. Because `maxHDRImageDim = 16384` and `maxHDRImagePixels = 64 Mi`, a 52-byte header declaring `-Y 4096 +X 16384` followed by EOF allocated and zeroed 768 MiB before returning `scanline 0: EOF`. In addition, unbounded `bufio.Reader.ReadString('\n')` calls on header lines, missing old-style RLE shift bounds, extreme `EXPOSURE=` values producing `+Inf` pixels, and old-style RLE's 16,384:1 expansion ratio (48 KiB expanding to 768 MiB) left untrusted project and mod `.hdr` files able to OOM mobile and web targets.

## Decision

`DecodeHDR` in `pkg/render3d/ibl_hdr.go` validates dimensions and payload feasibility without pre-allocating `width * height` pixels:

- **Bounded header parsing**: `readHDRLine` reads via `bufio.Reader.ReadSlice('\n')` capped at `maxHDRHeaderLineBytes` (8 KiB) per line and `maxHDRHeaderBytes` (64 KiB) across the full header. `EXPOSURE=` products whose reciprocal is non-positive or non-finite are rejected, and `rgbeToLinearRGB` clamps values above `math.MaxFloat32` so every decoded `*HDRImage` satisfies `Validate()`.
- **Known-size pre-check + first-scanline peek**: `minHDRScanlineBytes(width)` returns the exact minimum byte count any valid Radiance scanline (uncompressed, adaptive RLE, or old-style RLE) can occupy: 4 bytes for `width == 1`, 8 bytes for `width <= 256`, and 12 bytes for `width > 256`. When the underlying reader exposes its remaining length (`Len() int`, `*io.SectionReader`, or a regular `fs.File` implementing `io.Seeker`), `DecodeHDR` rejects streams shorter than `height * minHDRScanlineBytes(width)` before allocating the row buffer or pixel slice. For opaque streaming `io.Reader` inputs, `br.Peek(4)` rejects immediate EOF before allocating `scan`.
- **Incremental row-by-row growth and in-place `+Y` flip**: `rgb [][3]float32` starts empty and grows on demand (initial capacity `min(totalPixels, max(width, 4096))`, doubling up to `totalPixels`) only after each scanline decodes cleanly. Rows are written in stream order (applying `flipX` per row), and when `flipY` is set (`+Y`), rows are reversed in place after all `height` scanlines succeed, avoiding any pre-allocation of the final index range.
- **Decompression-bomb ceiling**: `hdrCountingReader` tracks exact bytes consumed (`cr.n - int64(br.Buffered())`). Once decoded output exceeds `hdrExpansionFloorBytes` (1 MiB), `DecodeHDR` rejects streams whose decoded bytes exceed `256 * consumedBytes`. Adaptive RLE's mathematical maximum expansion is `12 * 127 / 8 = 190.5x` and uncompressed is `3x`, so every valid `EncodeHDR` output and real-world HDRI passes while 48 KiB old-style RLE bombs (up to 16,384x) fail near the 1 MiB floor. Old-style RLE also guards `rleShift >= 24` and `count > width-x` against 32-bit integer overflow.
- **CI fuzzing**: `FuzzDecodeHDR` in `pkg/render3d/ibl_hdr_test.go` asserts bounded `runtime.MemStats.TotalAlloc`, error-wrapping (`ErrInvalidCubemap`), `Validate()` success, and pixel-for-pixel parity between sized (`*bytes.Reader`) and opaque streaming readers, and runs in `.github/workflows/quality.yml`.

## Alternatives considered

### Rejecting only via known-size reader checks without incremental allocation

Checking `bytes.Reader.Len()` or `os.File.Stat()` alone is fast and zero-allocation for seekable files, but fails to protect non-seekable `io.Reader` streams (such as compressed archives, HTTP/pipe streams, or custom wrappers). Combining the known-size pre-check with incremental slice growth protects every `io.Reader` while keeping zero-allocation rejection on seekable files.

### Dropping old-style RLE support or lowering `maxHDRImagePixels`

Removing old-style RLE would eliminate its 16,384x worst-case ratio, but breaks compatibility with legacy Radiance `.pic`/`.hdr` files. Lowering `maxHDRImagePixels` below 64 Mi would reject legitimate 8K equirectangular skies (`8192x4096` = 32 Mi pixels, or `16384x4096`). Keeping old-style RLE with a 256x expansion ceiling above 1 MiB preserves legacy files and all adaptive-RLE images while bounding decompression bombs.

## Consequences

- **Benefits**: Crafted or truncated `.hdr` headers allocate ~4 KiB instead of 768 MiB on both seekable files and streaming readers; truncated multi-scanline streams allocate only proportional to decoded rows; old-style RLE bombs fail around 1–3 MiB; and `FuzzDecodeHDR` guards regressions in CI.
- **Costs**: Valid large images decoded from streaming or seekable readers now grow `rgb` geometrically (up to ~2x transient allocation vs a single exact `make`), and `+Y` images perform an in-place row reversal pass after decoding. Synthetic solid-color old-style RLE files larger than 1 MiB whose compression exceeds 256x are rejected.
