### Fixed

- `pkg/tilemap`: parallax factor `0` on `TileLayer`, `ImageLayer`, and `ObjectLayer` (including group-multiplied `0`) now keeps the layer fixed to the screen and round-trips through `EncodeTiled`/`LoadTiled`, with `HasParallax`, `NewImageLayer`, and `NewObjectLayer` preserving default `1` on unset struct literals (#1582).
