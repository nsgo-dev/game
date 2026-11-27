### Changed

- `cmd/game release` and `pkg/editor`: mark `tvos`, `watchos`, `harmonyos`, and `miniprogram` export presets as experimental in `internal/releaseplan.Experimental`; `game release` warns once per experimental platform built with links to the platform support matrix and upstream NSGo tracking issue, `-strict` fails with exit code 1 instead for CI use, and the scene editor's export panel and build screen show an Experimental badge and link without blocking builds (#1309).
