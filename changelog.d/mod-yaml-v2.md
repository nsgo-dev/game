### Added

- `pkg/modding`: added `mod.yaml` version 2 (`ManifestVersion = 2`, `MinManifestVersion = 1`) with `optionalDependencies`, `conflicts` (`ErrConflictingMods`), `platforms` (`ErrUnsupportedPlatform`, `SupportedPlatforms`, `ManagerOptions.Platform`), and `contributes` (`ModContributes` with `assets`, `scenes`, `l10n`, and `datatables`), including content-only v2 mods without an `entry` script and published `mod.v1.json` / `mod.v2.json` JSON Schemas (#1285).
- `pkg/modding`: added `Manager.ContentFS()` read-only content overlay in load order (later mods win per path) with strict mod-directory path and symlink confinement (`ErrUnsafePath`), plus a content-only sample mod under `pkg/modding/testdata/mods/slime-pack` (#1285).

### Changed

- `pkg/modding`: `Discover` and `DiscoverFS` now isolate failures per mod (`StateFailed` with `Error` and `Dir` on `ModInfo`) for bad manifests, missing entries or `contributes` directories, incompatible game versions, unsupported platforms, duplicate IDs (failing both mods), conflicts (failing both mods), unsatisfied or failed dependencies, and cycles, so healthy sibling mods still register and load (#1285).
- `pkg/modding`: `ResolveOrder` now orders present `optionalDependencies` before dependents, ignores absent ones, and refuses conflicting pairs in either direction with `ErrConflictingMods` (#1285).
