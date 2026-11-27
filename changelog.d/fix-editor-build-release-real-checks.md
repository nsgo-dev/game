### Fixed

- `game/pkg/editor`: Build and Release no longer invents smoke playtest summaries, frame times, or padded package sizes when no runner or built artifact exists. It measures the packages `nsgo build` wrote with `game/pkg/tooling/sizecheck`, runs a headless main-scene smoke playtest on host desktop targets that were built, reports "not run" / "Not measured" without marking budgets met otherwise, and skips writing `build/last-release.yaml` when nothing was measured (#1618).

### Added

- `game/pkg/tooling/sizecheck`: `MeasureBuiltArtifact` finds the package `nsgo build` wrote for a platform under an output directory (with `PackageDir`, `ReleaseName` and `PackageSlug` following NSGo's naming) and measures its real size and code, textures, audio and other bytes (#1618).
