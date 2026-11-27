### Fixed

- `template`, `cmd/game`, `pkg/project`: every project template's `game.yaml` now targets iOS devices (`ios` instead of `ios_simulator`) and includes a `linux` preset (`linux_x64`, `linux_arm64`), `nsgo.yaml` keeps `build.targets` to cross-host targets so plain `nsgo build` succeeds on Linux and Windows, and `game release` skips presets the current host cannot build with an explanation while reporting unsigned iOS builds and failing on explicitly requested unavailable `-target` values (#1669).
