### Added

- `game/render3d`: HDR offscreen render target (`HDRTarget` using `gpu.FormatRGBA16Float` with automatic fallback detection), exposure helper (`ExposureFromEV`), tone mapping operators (`ToneMapNone`, `ToneMapReinhard`, `ToneMapACES`, `ToneMapNeutral`, `ToneMapAgX`), exact sRGB/power-law gamma conversion (`LinearToSRGBExact`, `SRGBToLinearExact`, `ApplyToneMap`), and full-screen GPU pass (`ToneMapPass`, `ToneMapWGSL`, zero-allocation `AppendToneMapUniforms`, and `Retained` device-loss support) (#250).
