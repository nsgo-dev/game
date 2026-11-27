### Changed

- `pkg/project`, `pkg/tooling/library`: `project.ValidatePackVersion` now requires a strict Semantic Versioning 2.0.0 string (`MAJOR.MINOR.PATCH[-prerelease][+build]`) without a leading `v`, so `game.yaml`, `pack.yaml`, and library `index.yaml` reject non-semver values such as `1.2`, `latest`, or `v1.0.0` (#1269).

### Fixed

- `pkg/modding`: version constraints now accept partial versions such as `^1.2`, `~1.2`, and `>=1.2`, matching the manifest validation fix hint, and accept optional leading `v` prefixes (#1269).
