### Changed

- `game/modding`: mod manifests are `mod.yaml` files with a `manifestVersion` field, read strictly (unknown fields, duplicate keys and unknown or repeated capabilities are refused) and written back in one canonical form by `EncodeManifest`; manifests built in Go may leave `ManifestVersion` at 0, so `Validate` and `ResolveOrder` callers are unaffected; `mod.json` is no longer read, and a mod that has only one is refused with a message asking for `mod.yaml` (#474).
