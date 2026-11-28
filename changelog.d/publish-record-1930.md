### Added

- `game/pkg/editor`: each successful upload from the Build and Release screen writes a versioned, credential-free publish record to `.game/publish/<destination>.yaml` with the version, `nsgo.yaml` build number, artifact name, SHA-256, size, time and result URL, and keeps the store-console steps a person confirmed, clearing the previous version's confirmations on a new upload (#1930).
