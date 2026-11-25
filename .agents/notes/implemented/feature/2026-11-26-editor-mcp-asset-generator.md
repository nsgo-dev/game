# Agent Note: Editor MCP asset generation, candidate picking, and source license gating

Status: implemented

## Problem

Authors using MCP image and sound generator servers need a safe way inside the editor to inspect candidate outputs against the game's `brief.yaml` style rules, compare them on real level backdrops or audition sound loudness, pick a candidate without leaving stray unapproved files on disk, record deterministic provenance and licensing metadata beside every picked asset, and guarantee that generated files with an unknown license cannot ship in a release build (`nsgo-dev/game#1053`).

Without a dedicated sidecar contract and untrusted decoding boundary:

- Generator servers could write arbitrary or non-media files into the project before a human approves them.
- Provenance (`generator`, `model`, `prompt`, `seed`, `references`, `approvedBy`) and license terms (`license`, `commercialUse`) would be lost or ad-hoc, making export-time license auditing impossible.
- Release builds (`game release` and the editor's Export panel) would only check `*.import.yaml` credits and would miss generated assets whose license was left unknown.

## Decision

Add a versioned `<file>.source.yaml` sidecar format in `packages/game/tooling/importsettings`, enforce unknown-license blocking in `packages/game/tooling/credits` and `packages/game/internal/releaseplan`, and implement an MCP generator client and full-window **Generated Assets** screen (`AssetGeneratorView`) in `packages/game/editor` matching `docs/design/editor/Assets.dc.html` Screen 2 and `docs/design/editor/Spec.dc.html`.

### `<file>.source.yaml` format and sidecar boundary

- `packages/game/tooling/importsettings/source.go` defines `AssetSource` (`AssetSourceVersion = 1`, `AssetSourceSuffix = ".source.yaml"`, `AssetSourceSchemaURL = "https://game.nsgo.dev/schemas/source.v1.json"`), `ParseAssetSource`, `EncodeAssetSource`, `ValidateAssetSource`, `AssetSourcePath`, `IsAssetSource`, and `IsUnknownLicense`.
- `ParseAssetSource` refuses unknown keys (`KnownFields(true)`), duplicate keys, YAML anchors/aliases/merge keys, trailing documents, and missing or unsupported versions, while accepting empty or `"unknown"` `license` values so unresolved generated assets can be represented on disk and rejected by release checks.
- `EncodeAssetSource` writes deterministic 2-space YAML prefixed with `AssetSourceSchemaComment` and is verified by `formattest.Run` (`tooling/importsettings#source`), `TestFormatsPage`, and `cmd/gameschema -check`.
- `packages/game/tooling/manifest.Scan` and `editor.asset_import.go` skip `*.source.yaml` files alongside `*.import.yaml` so sidecars are never treated as runtime game assets.

### Export-time license gate (`LICENSES · CHECKED AT EXPORT`)

- `packages/game/tooling/credits/source_licenses.go` adds `ScanGeneratedLicenses`, `CheckGeneratedLicenses`, and `*UnknownLicenseError`.
- `credits.Collect` runs `CheckGeneratedLicenses(root)` first and fails with `*UnknownLicenseError` naming every generated asset path (and its `.source.yaml` path) whose license is empty or unknown (`""`, `"unknown"`, `"none"`, `"noassertion"`, `"tbd"`, `"unspecified"`). When a generated asset has a known license in `*.source.yaml` and no `*.import.yaml` credit, `credits.Collect` credits it from `*.source.yaml`.
- `packages/game/internal/releaseplan.Build` runs `credits.CheckGeneratedLicenses(absDir)` even when `game.yaml` has no `assets:` section, ensuring both `cmd/game release` and `editor.CheckExportCredits` block any release build containing an unverified generated asset.

### Untrusted MCP generator client, style checks, and atomic undoable Pick

- `packages/game/editor/asset_gen.go` connects MCP generator servers over `github.com/modelcontextprotocol/go-sdk/mcp` (`ConnectAssetGenerator` accepting any `mcp.Transport` for tests and `ConnectAssetGeneratorCommand` using stdio `CommandTransport` on desktop, stubbed on mobile/web for iOS check parity).
- **Untrusted decoding rule**: `GenerateAsset` decodes returned image candidates strictly through `image.DecodeConfig` + `image.Decode` with dimension/byte bounds and re-encodes fresh in-memory PNG bytes (`decodeUntrustedImage`), refusing SVGs, HTML, scripts, or malformed payloads. Sound candidates are strictly decoded through `audioconv.DecodeWAV` or validated OGG headers in memory (`decodeUntrustedAudio`). Nothing is ever written to disk before a pick.
- **Style and audio checks**: `LoadAssetStyleBrief` reads `brief.yaml` (falling back to `DefaultAssetStyleBrief`) and evaluates each image candidate for palette count, pixel-grid alignment (anti-aliasing and scale straddling), size against the reference asset, and WCAG luminance contrast against the active `PREVIEW ON` backdrop; sound candidates report duration, RMS dBFS loudness, peak dBFS, loudness check against the reference sound or brief target, and in-memory audition (`PlayGeneratedSoundCandidate`).
- **Atomic Pick / Undo**: `PickGeneratedCandidate` executes one `fallibleCommand` (`pickGeneratedAssetCmd`) in `Editor.History` that atomically writes `<targetPath>` and `<targetPath>.source.yaml` (rolling back the first file if the second fails). A single `Editor.Undo()` removes both files cleanly (or restores prior bytes), and `Editor.Redo()` restores both. `VaryGeneratedCandidate`, `RefineGeneratedAsset`, and `RegenerateAsset` re-invoke the generator with the same references without writing to disk.
- **UI and toolbar budget**: `AssetGeneratorView` (`asset_gen_ui.go`) is wired as a full-window view in `EditorView` (`ui.go`), opened via `CmdGeneratedAssets` (`Project > Generated Assets` and Command Palette) rather than adding a button to the 2D `editor-toolbar`, keeping the toolbar compact (<= 2 rows at 1120×680). Configured generator servers are kept on `Editor` (`SetGeneratorServerConfigs`) rather than adding a slice field to `EditorSettings`, preserving `EditorSettings` struct comparability (`i18n_settings_test.go`).

## Alternatives considered

1. **Place `AssetSource` in `packages/game/editor` instead of `packages/game/tooling/importsettings`** — Strongest argument: `*.source.yaml` is written by the editor's picker, so co-locating it in `editor` keeps editor-authored types together. Rejected because `tooling/credits`, `tooling/manifest`, and `internal/releaseplan` all inspect or skip `*.source.yaml` without importing `editor` (which imports them); placing `AssetSource` in `tooling/importsettings` keeps the dependency graph acyclic and aligns both asset sidecar formats (`*.import.yaml` and `*.source.yaml`) in one package.
2. **Write candidate previews to a temporary project folder before Pick** — Strongest argument: allows reusing existing file-based asset loaders and thumbnails. Rejected because the untrusted-output contract requires showing candidates strictly as in-memory pixels/audio and writing zero bytes to disk until the author explicitly picks a candidate.
3. **Store configured MCP generator servers inside `EditorSettings`** — Strongest argument: persists generator server configs in the existing per-user `editor.yaml`. Rejected because `EditorSettings` is tested and used as a comparable Go value type (`*parsed != def`), and adding a slice field breaks value comparability for existing callers.
4. **Add a Generate button directly to the 2D `editor-toolbar`** — Strongest argument: one-click discoverability from the main viewport. Rejected to avoid expanding the 2D toolbar past two rows at 1120×680; `Project > Generated Assets`, Command Palette (`CmdGeneratedAssets`), and `Settings › Tools` provide clean entry points without toolbar bloat.

## Consequences

- **Benefits**: Untrusted MCP generator output never touches disk before Pick; picking writes both the media file and a schema-validated, deterministic `<file>.source.yaml` in one undoable step; one `Undo` removes both files cleanly; unknown generated licenses reliably block `credits.Collect`, `releaseplan.Build`, `game release`, and the editor Export panel with the exact offending asset path; verified with unit tests, conformance tests, iOS cross-checks, and native macOS `nsgotest` screenshots.
- **Costs**: Configured MCP generator servers stored via `SetGeneratorServerConfigs` are currently session-scoped rather than persisted in `editor.yaml` until a dedicated per-user tools configuration file is introduced; OGG candidates without a built-in Vorbis PCM decoder estimate loudness from container metadata rather than decoded PCM samples (WAV candidates compute exact RMS and peak dBFS via `audioconv`).
