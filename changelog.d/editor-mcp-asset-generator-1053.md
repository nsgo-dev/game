### Added

- `game/editor`, `game/tooling/importsettings`, `game/tooling/credits`: added the MCP Generated Assets screen (`AssetGeneratorView`), strict versioned `<file>.source.yaml` provenance/license sidecar format (`importsettings.ParseAssetSource` / `EncodeAssetSource`, `source.v1.json` schema), untrusted in-memory image/audio candidate decoding with `brief.yaml` style checks, single-step undoable Pick/Undo, and release-blocking unknown license checks in `credits` and `game release` (#1053).
