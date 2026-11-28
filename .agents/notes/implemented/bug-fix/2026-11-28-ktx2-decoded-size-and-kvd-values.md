# Agent Note: Bound KTX2 decoded RGBA size and keep binary KVD values exact

Status: implemented

## Problem

`pkg/ktx2` capped each Zstandard or ZLIB level's decompressed payload at `maxUncompressedLevelBytes` (256 MiB) and each dimension at 16384, but nothing capped the RGBA image `Texture.DecodeLevelRGBA` builds from a level. Zstandard RLE blocks turn four input bytes into 128 KiB, so a file of about 4 KB could declare a 16384x16384 BC1 level, decompress it to 128 MiB, and then make `DecodeRGBA`, `DecodeLevelRGBA` or `Transcode` allocate a 1 GiB `image.NRGBA`, about 2 GiB in total. `decompressZstd` also copied every frame into a second output buffer, holding a level twice at its peak.

`DecodeKVD` stripped every trailing zero byte from every value with `bytes.TrimRight`, so binary values the KTX2 specification defines without a terminator (`KTXdxgiFormat__`, `KTXglFormat` and the like), which often end in zeros, read back truncated.

## Decision

- `maxUncompressedLevelBytes` bounds the RGBA image of a level as well as its payload. `checkRGBALevelSize` refuses a level whose `w*h*4` exceeds it, that is one larger than 8192x8192. `DecodeLevelRGBA` checks before any decoding, which covers every format including BasisLZ/ETC1S and every `Transcode` that is not a same-format clone, and `DecodeRGBA` checks from the header alone before `Decode` decompresses anything.
- `decompressZstd` uses the first frame's buffer as its output instead of copying it, so a single-frame level is held once.
- `kvdBinaryKeys` lists the specification's binary keys (`KTXcubemapIncomplete`, `KTXglFormat`, `KTXdxgiFormat__`, `KTXmetalPixelFormat`, `KTXanimData`). `buildKVD` writes them without a NUL and `DecodeKVD` keeps them byte for byte. Every other value is a string: `buildKVD` appends one NUL and `DecodeKVD` drops at most one, so zero bytes inside or before the terminator survive a round trip.

## Alternatives considered

- **A compression-ratio limit per level**: rejects the bomb shape directly, but a uniform or mostly flat texture compresses with zstd to exactly the RLE shape the attack uses, so any ratio low enough to matter refuses legitimate files, and a ratio only raises the input size an attacker needs while the absolute caps bound the memory either way.
- **A separate, smaller total-texture budget**: the decoder accepts only one face and one layer, and the level count is capped at the mip chain's length, so the payload of a whole texture is already at most about 4/3 of the per-level cap; a second constant would add a limit without changing the bound.
- **Stripping one NUL from every KVD value, binary or not**: simpler, but still corrupts a binary value another tool wrote without a terminator whenever it ends in zero.

## Consequences

- A tiny supercompressed file can no longer make the decoder allocate more than the per-level cap for any level, and `DecodeRGBA` refuses an oversized level before allocating anything. `TestDecodeRefusesRGBAOverLimit` asserts both with `runtime.MemStats`, and `FuzzDecodeKTX2` carries the file as a seed.
- Decoding a block-compressed, UASTC or ETC1S level larger than 8192x8192 to RGBA on the CPU is now refused, including transcoding a universal texture that large to another block format. Uploading such a level in its own block format (`render3d.LoadKTX2` on a device that supports it) still works.
- `Decode` still allocates up to 256 MiB for a level a small file declares, as before: that cap is the documented level budget, not this change.
- A custom (non-specification) KVD key holding binary data that ends in a zero byte loses that one byte on read, since the reader cannot tell it from a string terminator; writing such data with `Options.KVD` round-trips because `buildKVD` adds the terminator.
