### Added

- `game/l10n`: per-language asset maps are versioned YAML (`version: 1`) loaded strictly with `LoadAssetManifestYAML` and saved deterministically with `MarshalAssetManifestYAML` (#473).

### Removed

- `game/l10n`: `AssetCatalog.LoadAssetManifestJSON`, with its direct-map shape and `baseLocale` alias. To migrate, write the manifest as YAML with `version: 1`, put the entries under `assets:` and rename `baseLocale` to `base_locale` (#473).
