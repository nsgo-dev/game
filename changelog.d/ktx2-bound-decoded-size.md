### Security

- `pkg/ktx2`: decoding a level to RGBA (`DecodeRGBA`, `Texture.DecodeLevelRGBA`, and `Texture.Transcode` to another format, which `render3d.LoadKTX2` uses for its CPU fallback) now refuses a level whose RGBA image would exceed 256 MiB, that is one larger than 8192x8192, before allocating it, and a Zstandard level is no longer held twice while it decompresses, so a KTX2 file of a few kilobytes from a mod, pack or model can no longer make the decoder allocate gigabytes. Textures up to 8192x8192 decode as before, and a larger block-compressed texture still uploads in its own format on a device that supports it; update to this release if your game loads KTX2 files it did not build itself (GHSA-9mh9-5gjq-r7h8).

### Fixed

- `pkg/ktx2`: binary key/value values the KTX2 specification defines, such as `KTXdxgiFormat__` and `KTXglFormat`, are written without a terminator and read back byte for byte, and a string value loses only its one terminating NUL, where `DecodeKVD` used to strip every trailing zero byte (GHSA-9mh9-5gjq-r7h8).
