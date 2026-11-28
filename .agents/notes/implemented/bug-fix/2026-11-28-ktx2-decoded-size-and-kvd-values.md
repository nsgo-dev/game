# Agent Note: Bound KTX2 decoded sizes and keep binary KVD values exact

Status: implemented

## Problem

`pkg/ktx2` capped each Zstandard or ZLIB level's decompressed payload at `maxUncompressedLevelBytes` (256 MiB) and each dimension at 16384, but nothing capped the RGBA image `Texture.DecodeLevelRGBA` builds from a level, the sum of a texture's levels, or the output of `Transcode`. Zstandard RLE blocks turn four input bytes into 128 KiB, so a file of a few kilobytes could:

- declare a 16384x16384 BC1 level, 128 MiB inflated, and make `DecodeRGBA`, `DecodeLevelRGBA` or `Transcode` allocate a 1 GiB `image.NRGBA`, about 2 GiB in total;
- declare a full 16384x16384 BC7 or UASTC mip chain, every level within the per-level cap, and make `Decode` alone allocate about 1.9 GiB, since a frame without a content size grew its buffer one RLE byte at a time and every frame was then copied into a second output buffer; `render3d.LoadKTX2` then cloned every level again through `Transcode(t.Format)`;
- declare a full 8192x8192 UASTC chain, every level within the RGBA cap, and make `Transcode(FormatRGBA8)` allocate about 1.2 GiB across its levels;
- make `DecodeRGBA(data, 1)` pass a header check on level 1 while `Decode` inflated level 0 and every other level.

`DecodeKVD` stripped every trailing zero byte from every value with `bytes.TrimRight`, so binary values the KTX2 specification defines without a terminator (`KTXdxgiFormat__`, `KTXglFormat` and the like), which often end in zeros, read back truncated.

## Decision

- `maxUncompressedLevelBytes` bounds the RGBA image of a level as well as its payload. `checkRGBALevelSize` refuses a level whose `w*h*4` exceeds it, that is one larger than 8192x8192. `DecodeLevelRGBA` checks before any decoding, which covers every format including BasisLZ/ETC1S, and `DecodeRGBA` checks from the header alone before anything is decompressed.
- `maxDecodedTextureBytes` (256 MiB) bounds a whole texture twice. `Decode` sums `LevelByteSize` over the levels of a Zstandard or ZLIB file from its header and refuses the file before decompressing anything. `Transcode` to another format sums `target.LevelByteSize` over the levels and checks every level's RGBA size before decoding the first one. Payloads `Decode` copies from an uncompressed or BasisLZ file are not counted: they are bounded by the file the caller already holds.
- `decompressZstd` allocates one buffer of the level's exact size and decodes every frame in place at its end, a frame's slice capped at the budget the earlier frames left, so match offsets still cannot reach into an earlier frame. An RLE block fills its span in one step. `decompressZLIB` reads into one buffer of the level's size plus one byte instead of growing through `io.ReadAll`.
- `DecodeRGBA` de-supercompresses only the level it returns (an internal `decode(data, only)`), so the budget counts only that level.
- `render3d.LoadKTX2` uploads levels in their own format straight from the decoded texture instead of cloning them through `Transcode(t.Format)`; the texture is local to the call, so nothing else sees the shared slices.
- `kvdBinaryKeys` lists the specification's binary keys (`KTXcubemapIncomplete`, `KTXglFormat`, `KTXdxgiFormat__`, `KTXmetalPixelFormat`, `KTXanimData`). `buildKVD` writes them without a NUL and `DecodeKVD` keeps them byte for byte. Every other value is a string: `buildKVD` appends one NUL and `DecodeKVD` drops at most one, so zero bytes inside or before the terminator survive a round trip.

## Alternatives considered

- **A compression-ratio limit per level**: rejects the bomb shape directly, but a uniform or mostly flat texture compresses with zstd to exactly the RLE shape the attack uses, so any ratio low enough to matter refuses legitimate files, and a ratio only raises the input size an attacker needs while the absolute caps bound the memory either way.
- **No texture-wide budget, relying on the per-level cap**: the payload of a texture is at most about 4/3 of the per-level cap, but that is still 341 MiB from a few kilobytes, and the review showed the real peak was several times that once buffer growth, the frame copy and the same-format clone were counted. A texture budget equal to the per-level cap makes the bound one number a caller can plan for.
- **A larger texture budget (512 MiB)**: would keep a full 16384x16384 BC7 chain and a full 8192x8192 RGBA8 fallback loading, but puts the worst case of `Decode` plus a different-format `Transcode` plus one RGBA intermediate past 1 GiB, the magnitude the advisory reported, on the phones and browser tabs it is about.
- **Stripping one NUL from every KVD value, binary or not**: simpler, but still corrupts a binary value another tool wrote without a terminator whenever it ends in zero.

## Consequences

- From a small file, `Decode` allocates at most 256 MiB beyond the file, `Transcode` to another format at most 256 MiB of output plus one RGBA image of at most 256 MiB, and `DecodeRGBA` one level plus its RGBA image. `limits_test.go` asserts each refusal with `runtime.MemStats` (`TestDecodeRefusesRGBAOverLimit`, `TestDecodeRefusesTextureOverBudget`, `TestTranscodeRefusesOutputOverBudget`, `TestDecodeRGBADecompressesOnlyItsLevel`) and that a level is allocated once (`TestDecodeAllocatesLevelsOnce`); `FuzzDecodeKTX2` carries the 16384x16384 BC7 chain as a seed, which `Decode` refuses from the header so the seed stays cheap to mutate.
- A full 16384x16384 BC7 or UASTC mip chain (341 MiB) no longer decodes. A single 16384x16384 level in a block format still decodes and uploads where the device supports the format. A UASTC or BasisLZ/ETC1S texture larger than 8192x8192 never loads, since it is always transcoded on the CPU. A CPU fallback to RGBA8 is limited to 256 MiB of output, so a full 8192x8192 chain on a device without its block format no longer loads; 8192x8192 universal textures still transcode to BC7, BC1, BC3, ETC2 or ASTC.
- `Decode` still allocates up to 256 MiB for a texture a small file declares: that is the documented budget, and a compression-ratio limit was rejected above.
- A custom (non-specification) KVD key holding binary data that ends in a zero byte loses that one byte on read, since the reader cannot tell it from a string terminator; writing such data with `Options.KVD` round-trips because `buildKVD` adds the terminator.
