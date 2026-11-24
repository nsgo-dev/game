### Added

- `game/render3d`: `LoadKTX2` loads Basis Universal UASTC 4x4 KTX2 files, transcoding them to BC7, BC1, BC3, ETC2 or ASTC 4x4 as the device supports, or decoding them to RGBA8; it also takes BC7, ETC2 RGBA8 and ASTC 4x4 files and Zstandard or ZLIB supercompressed levels, and `KTX2Upload.ForceRGBA8` uploads any file decoded to RGBA8 (#271).
- `game/tooling/ktx2`: `Decode`, `Texture.Transcode`, `Texture.DecodeLevelRGBA` and `FormatFromHeaderDFD` read KTX2 files with Zstandard or ZLIB supercompression and the UASTC, BC7, ETC2 RGBA8 and ASTC 4x4 formats, `Options.Format` encodes them, and `Options.Supercompression` writes ZLIB (#271).
