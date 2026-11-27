### Fixed

- `pkg/render3d`: `DecodeHDR` now checks remaining payload size and grows the pixel buffer scanline by scanline instead of pre-allocating `width * height` pixels from the header, stopping truncated or crafted `.hdr` files from allocating up to 768 MiB before failing (#1552).
