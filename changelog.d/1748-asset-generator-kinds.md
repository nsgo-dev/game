### Added

- `game/editor`: generator servers in Settings › Tools accept the `textures`, `models`, `animation` and `tiles` kinds besides `images` and `sound`; `GenerateAsset` refuses a kind that has no in-memory validator yet (`AssetGeneratorKind.Generatable`, so far only `images` and `sound`) before calling the server (#1748).
- `game/importsettings`: the `.source.yaml` sidecar (`AssetSource`) gains optional `kind` and `files` fields, and Pick records the generated kind; version-1 sidecars without them stay valid (#1748).
