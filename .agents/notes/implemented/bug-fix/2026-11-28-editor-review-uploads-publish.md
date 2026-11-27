# Agent Note: Editor Review Uploads Default NSGo Publish Uploader

Status: implemented

## Problem

On the Build and Release screen ([Editor Build and Release screen](../feature/2026-11-26-editor-build-release-screen.md)), confirming store uploads in **Review uploads** always failed when `Editor.ReleaseUploader` was nil because `defaultReleaseUploader` returned `"editor: no store uploader runner configured..."` even though NSGo v0.3.4 already ships `nsgo publish itch`, `nsgo publish -target android -track internal`, and `nsgo publish ios` (#1762). Two adjacent mismatches also misled authors in the dialog:

- Desktop itch.io channel labels were hard-coded as `osx`, `windows`, and `linux`, whereas `nsgo publish itch` pushes to `osx-<arch>`, `windows-<arch>`, and `linux-<arch>` unless overridden in `nsgo.yaml`'s `itch.channels`.
- Desktop builds built with the `steam` channel were still offered for `itch.io`.

## Decision

Wire `defaultReleaseUploader` in `pkg/editor/build_release_publish.go` to invoke `go tool nsgo publish` (falling back to `nsgo` on `PATH` when no enclosing `go.mod` declares `tool github.com/nsgo-dev/nsgo/cmd/nsgo`) in the project directory, resolve exact itch.io channel labels from `nsgo.yaml` and `github.com/nsgo-dev/nsgo/targets`, and mark `steam`-channel desktop builds as manual `"upload with SteamPipe"` rows.

### Cross-package contract (`internal/releaseplan`, `pkg/editor`)

- **`internal/releaseplan/plan.go`**: `NSGoSettings` parses `itch.project` and `itch.channels` from `nsgo.yaml` alongside `updates.channel` and `build.output`.
- **Channel and destination mapping (`pkg/editor/build_release_publish.go`, `pkg/editor/build_release.go`)**:
  - `BuildPresetEntry.Channel` records the effective channel used during `StartReleaseBuild` (`opt.Channel` else `nsgo.yaml`'s `updates.channel`).
  - `buildReviewUploadItems` resolves per-preset targets via `targets.Expand`, derives itch.io channel labels from `nsgo.yaml`'s `itch.channels` or NSGo's defaults (`html5`, `osx-<arch>`, `windows-<arch>`, `linux-<arch>`), and marks desktop presets (`macos`, `windows`, `linux`) on the `steam` channel with `ManualOnly: true`, `Destination: ""`, `DestinationLabel: "upload with SteamPipe"`, and `Included: false` so they are never offered for itch.io or selectable for automated upload.
- **Default uploader (`defaultReleaseUploader`)**:
  - `PublishDestItchIO`: runs `publish itch -target <targets> [-output <dir>] [-version <ver>]` with `BUTLER_API_KEY` set only in `cmd.Env`.
  - `PublishDestGooglePlay`: runs `publish -target android -track <track> [bundle.aab]` (normalizing `"internal track"` to `"internal"`) with `NSGO_PLAY_SERVICE_ACCOUNT` set only in `cmd.Env`.
  - `PublishDestTestFlight`: runs `publish ios <file.ipa>` with `NSGO_APP_STORE_KEY` (and optional `NSGO_APP_STORE_KEY_ID` / `NSGO_APP_STORE_ISSUER` parsed from JSON `{key, key_id, issuer}` or `key_id:issuer` account metadata) set only in `cmd.Env`.
  - Streams stdout/stderr line-by-line (splitting on `\n` and `\r`) as `ReleaseUploadProgress`, strips ANSI escapes, redacts secret values before updating UI state or returning errors, and surfaces printed store URLs (or derived `https://<owner>.itch.io/<game>` URLs) and processing state messages on `ReleaseUploadResult`.

## Alternatives considered

### Keeping a single raw-token environment variable for TestFlight

Passing only `NSGO_APP_STORE_KEY` without parsing optional JSON or `key_id:issuer` account metadata would be simpler, because `Keychain.Set` stores a single secret string per destination. Rejected because `nsgo publish ios` requires `NSGO_APP_STORE_KEY`, `NSGO_APP_STORE_KEY_ID`, and `NSGO_APP_STORE_ISSUER` together unless already set in the environment; parsing optional JSON `{key, key_id, issuer}` (and `key_id:issuer` in `Credential.Account`) preserves full backwards compatibility with single-string keychain entries while allowing all three Apple credentials to live in the system keychain without changing the `Keychain` interface.

### Always invoking `go tool nsgo` without checking `go.mod`

Unconditionally executing `go tool nsgo` matches template projects that declare `tool github.com/nsgo-dev/nsgo/cmd/nsgo`, but fails in minimal directories without `go.mod` or when testing with a fake `nsgo` binary on `PATH`. Checking the nearest enclosing `go.mod` for the `nsgo` tool directive uses `go tool nsgo` in every real Go module project while cleanly supporting standalone `nsgo` binaries on `PATH`.

## Consequences

- **Benefits**: Confirming uploads in **Review uploads** works out of the box for itch.io, Google Play internal testing, and TestFlight without custom Go wiring, displays the exact channels `nsgo publish itch` pushes to, keeps Steam-channel desktop builds out of itch.io, and guarantees credentials stay in the environment alone and redacted from all UI output and errors.
- **Costs and known limits**: Automated Steam uploads still require SteamPipe externally until a Steam destination is implemented; `pkg/editor` now imports `github.com/nsgo-dev/nsgo/targets` to share NSGo's target expansion table directly.
