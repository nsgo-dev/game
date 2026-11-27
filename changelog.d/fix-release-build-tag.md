### Fixed

- `game release`, the scene editor's Export and Build and Release screens, and `sizecheck`: pass `GOFLAGS=-tags=release` to `nsgo build` so shipped builds link the `release` stubs of `pkg/debug`, `pkg/tooling/hotreload`, `pkg/tooling/devstate`, and physics/AI debug drawing instead of development code, and verify built desktop executables against missing `release` tags or leaked debug symbols (#1495).
