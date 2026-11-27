### Fixed

- `pkg/assets`: default `Manager` reads for `"asset:"` URIs now use `core.Scope.ReadAsset` so atlas JSON, raw bytes, and custom assets resolve the same `resources/` files as images and sounds on every target (including on-demand fetches on the web), while keeping `WithFS` and `WithReadAsset` as overrides (#1480).

### Added

- `cmd/game`: add `-resources` to `game assets` and `game manifest` to write built asset outputs into `resources/` and emit `"asset:"` URIs without `//go:embed` (#1480).

### Changed

- `examples/caverun`: stream the level group (`sprites`, `tileset`, `level1.tmj`, and audio) from `resources/` through `assets.Manager`, keeping only the title bitmap font embedded (#1480).
