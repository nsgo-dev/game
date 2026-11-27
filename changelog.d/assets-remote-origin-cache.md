### Added

- `pkg/assets`, `pkg/project`, `pkg/tooling/assetbuild`: load loose remote assets by content-hashed URL (`remote.yaml`, `assets.WithRemote`, `DirRemoteCache` with a 200 MiB default quota and loaded-group-safe LRU eviction, pluggable `Fetch` transport seam, and `assets.remote` publishing in `go tool game assets`) (#1736).
