# Agent Note: KTX2 UASTC transcoding and compressed formats in LoadKTX2

Status: implemented

## Problem

`render3d.LoadKTX2` (#487) uploaded only uncompressed-container RGBA8, BC1, BC3 and ETC2 KTX2 files. Issue #271 asks for KTX2 with Basis Universal transcoded to each GPU's format, so one texture file draws on desktop (BC), Android and WebGL 2 (ETC2) and Apple GPUs (ASTC). That needs a UASTC decoder, block encoders for each target, Zstandard supercompression (the usual packing of UASTC files) and a device-aware choice of target, all in Go, as this repository allows no C and no new third-party modules.

## Decision

`tooling/ktx2` gains the codecs and `render3d.LoadKTX2` stays the one entry point:

- `ktx2.Decode` validates a 2D container, undoes Zstandard (`zstd.go`, an in-tree RFC 8878 decoder) or ZLIB (`compress/zlib`) supercompression with the output capped at the level's exact size, and resolves the format from the vkFormat or, for vkFormat 0, from the DFD (UASTC or ETC1S). `Texture.Transcode` returns every level in a target format: unchanged when it already is one, otherwise decoded to RGBA and encoded again.
- The UASTC decoder (`uastc.go`) implements all 19 modes. Its partition, anchor and Huffman tables, per-mode parameters, hint layout, integer-sequence order, endpoint unquantization and interpolation were checked line by line against basisu's `unpack_uastc` (`basisu_transcoder.cpp`); the unquantization tables were recomputed from the ASTC specification's formula and match. The encoder writes modes 8, 5, 18 and 14.
- The BC7 decoder handles all eight modes, with the partition and anchor tables taken from the specification as bc7enc's decoder spells them; its test places every field at the absolute bit offsets that decoder reads. The BC7 encoder writes mode 6.
- The ASTC 4x4 decoder initially handled only the blocks the ASTC encoder writes; [BasisLZ/ETC1S KTX2 decoder, full CPU ASTC 4x4 LDR decoder, and reference-encoder fixtures](2026-11-25-ktx2-etc1s-and-astc-decoder.md) (#750) upgraded it to decode all valid ASTC 4x4 LDR blocks.
- `LoadKTX2` keeps its signature, `*GPUTexture` result, `game.filter` sampler and `ErrInvalidTexture`. A block format the device supports uploads as it is and any other decodes to RGBA8, as before; UASTC (and BasisLZ/ETC1S after #750) transcodes to the first of BC7, BC1 (opaque), BC3, ETC2 RGB8 or RGBA8 (linear), ASTC 4x4 the device supports when the base size is a multiple of 4, and decodes to RGBA8 otherwise. `KTX2Upload.ForceRGBA8` forces RGBA8.
- Refused: BC1 without alpha (`VK_FORMAT_BC1_RGB_*`, whose punch-through texels an RGBA BC1 upload draws transparent), and in `ParseFormat` the new format names, so `game tex -format`, sidecars and `project.TextureFormats` keep the four formats the asset pipeline documents. BasisLZ/ETC1S was initially refused here and later added in [BasisLZ/ETC1S KTX2 decoder, full CPU ASTC 4x4 LDR decoder, and reference-encoder fixtures](2026-11-25-ktx2-etc1s-and-astc-decoder.md) (#750).

## Alternatives considered

### A second API (`UploadKTX2`, `GPUCompressedTexture`, `ErrInvalidKTX2`) next to LoadKTX2

The first version of this change added one. It was dropped because its texture type fits none of the material texture slots, which take `*GPUTexture`, and it ignored the `game.filter` key, so import-settings filters were lost.

### `github.com/klauspost/compress/zstd`

A mature decoder, but this repository takes no new third-party modules. The in-tree decoder verifies the XXH64 content checksum, scopes match offsets to their frame, and is tested against frames the zstd 1.5.7 CLI wrote, checked in under `testdata/zstd` so the tests do not need the CLI.

### Transcoding foreign block formats (BC3 on an ETC2 device) to the device's block format

It compresses twice and costs a decode and encode per block at load, so a BC, ETC2 or ASTC file the device lacks decodes to RGBA8 as `LoadKTX2` already did. Only UASTC, which no GPU samples, is transcoded.

### Direct UASTC-to-BC7 or UASTC-to-ASTC transcoding, as basisu does

Faster and without a second loss, but it needs per-mode bit-level mapping tables for every target. Decode and encode keeps one encoder per target; its cost is measured by `BenchmarkTranscodeUASTC` and documented in `render3d.md`.

## Consequences

- One UASTC file loads on every backend; BC7, ETC2 RGBA8 and ASTC files and supercompressed files load too.
- Transcoding runs on the goroutine calling `LoadKTX2`: about 0.5 s to BC7, 0.8 s to ETC2 and 0.16 s to ASTC for a 1024x1024 level on a Ryzen 9 3900X, four times that at 2048x2048. The benchmark is not in `benchmarks/baseline.yaml`, whose CI job gates sprite, tilemap and bullets on one runner.
- No UASTC file written by basisu or toktx is checked in, as no reference encoder was available while writing this; the decoder is verified against basisu's source, not against its output. ASTC files from other encoders load only on devices with ASTC. Both are part of #750.
- NSGo's software renderer decodes only BC7 mode 6 and void-extent ASTC blocks exactly, so render tests use images whose 4x4 blocks are one color.

## Verification

- `go tool nsgo test ./tooling/ktx2`: all BC7 modes against reference bit offsets, ASTC refusals, UASTC modes and round trips, zstd CLI fixtures with and without checksums and as two frames, XXH64 reference values, malformed inputs and `FuzzDecodeKTX2`.
- `go tool nsgo test ./render3d`: UASTC drawn on BC, ETC2, ASTC and uncompressed software devices, BC7, ETC2 RGBA8 and ASTC native and decoded, `ForceRGBA8`, odd sizes, ZLIB, refusals and `FuzzLoadKTX2`.
