### Added

- `tooling/ktx2`, `render3d`: decode BasisLZ/ETC1S KTX2 files (including alpha slices and mipmap chains) in pure Go and transcode them in `Texture.Transcode` and `render3d.LoadKTX2` to each GPU's supported block format (BC7, BC1, BC3, ETC2, ASTC 4x4, or RGBA8) (#750).
- `tooling/ktx2`: decode every valid ASTC 4x4 LDR block on the CPU (void-extent, all 2D block modes, BISE trits/quints/bits, 1–4 partitions, dual-plane weights, bilinear weight grid infill, and all LDR color endpoint modes) so `astcenc`-encoded KTX2 files load on devices without ASTC (#750).
- `tooling/ktx2`: verify the ETC1S, UASTC (uncompressed and Zstandard-supercompressed), BC7, and ASTC 4x4 decoders against checked-in `basisu` and `astcenc` reference fixtures and unpacked RGBA PNGs (#750).
