# Agent Note: Standalone desktop editor and signed release packaging

Status: implemented

## Problem

Tagged releases of `nsgo-dev/game` previously created text-only GitHub Releases with changelog notes and no prebuilt desktop editor or CLI binaries. Users who wanted to open the visual 2D/3D scene editor had to invoke it through Go (`go run github.com/nsgo-dev/game/cmd/game@latest editor` or `nsgo game editor`), and there was no standalone desktop `nsgo.yaml` application entrypoint for packaging native installers.

## Decision

Add a thin desktop application entrypoint in `cmd/editor` (`cmd/editor/main.go` + `cmd/editor/nsgo.yaml`) and extend `.github/workflows/release.yml` to build, sign, verify, and attach full desktop installers for **NSGo Game Editor** alongside prebuilt `nsgo-game` CLI archives on every release.

### Packaging and signing contract

- `cmd/editor` delegates directly to `internal/gamecli.Run(editorArgs(os.Args[1:]))`, stripping macOS Finder's `-psn_*` flag so double-clicking the app opens the Start Screen while CLI invocations still accept project/scene/clip arguments.
- `cmd/editor/nsgo.yaml` declares `id: dev.nsgo.game.editor`, `release.github: nsgo-dev/game`, and `updates.key` matching the organization-wide `NSGO_UPDATES_KEY` secret so `go tool nsgo build` emits signed `appcast-<target>.xml` feeds, `install.sh`, `install.ps1`, and `.AppImage.zsync` files alongside every installer.
- `.github/workflows/release.yml` builds all six desktop targets on Depot runners (`depot-macos-15` for `macos_arm64`/`macos_x64` `.dmg`, `depot-ubuntu-24.04-4` for `linux_x64`/`linux_arm64` `.deb`/`.pkg.tar.gz`/`.AppImage` and `windows_x64`/`windows_arm64` `-setup.exe`), plus cross-compiled `nsgo-game` CLI archives for all six desktop OS/arch pairs.
- macOS builds import the organization's Developer ID certificate (`MACOS_CERTIFICATE`, `MACOS_CERTIFICATE_PASSWORD`, `vars.MACOS_SIGNING_IDENTITY`) and App Store Connect API key (`APPSTORE_API_KEY_P8`, `APPSTORE_API_KEY_ID`, `APPSTORE_API_ISSUER_ID`), passing them to `go tool nsgo build` for hardened-runtime signing, notarization, and stapling, then verify every `.app` and `.dmg` with `codesign`, `xcrun stapler validate`, and `spctl --assess`. Non-release fork PRs without secrets fall back to ad hoc signing while tagged releases and `workflow_dispatch` uploads fail fast if signing or update secrets are missing.
- `workflow_dispatch` with an optional existing `tag` input (`mode=upload`) allows building and attaching signed editor and CLI assets onto an already-published release (such as `v0.1.0`) via `gh release upload --clobber` without moving or recreating the immutable Go module tag.

## Alternatives considered

1. **Build `cmd/game` directly as a desktop app**: Rejected because `cmd/game` requires a subcommand (`editor`) and exits with CLI usage text when launched with no arguments from Finder/Start Menu/desktop launchers.
2. **Move the existing `v0.1.0` tag to re-trigger a tag push release**: Rejected because `github.com/nsgo-dev/game@v0.1.0` is already indexed by `proxy.golang.org` and the Go checksum database; moving a published Go module tag breaks module verification.
3. **Require Windows Authenticode signing via Certum immediately**: Deferred because no `nsgo-dev` repository currently ships a Certum SimplySign runner integration; Windows installers are still built and signed for WinSparkle updates via `NSGO_UPDATES_KEY`, and Authenticode signing can be added later through `nsgo build -sign-command`.

## Consequences

- **Benefits**: Every GitHub Release ships ready-to-install macOS (signed & notarized `.dmg`), Linux (`.deb`, `.pkg.tar.gz`, `.AppImage`), and Windows (`-setup.exe`) desktop editors with one-line `install.sh`/`install.ps1` scripts, signed update appcasts, and prebuilt `nsgo-game` CLI binaries. Pull requests modifying `cmd/editor` or `release.yml` dry-run the entire packaging and signing pipeline before merge.
- **Costs**: Tagged releases wait on macOS notarization (typically a few minutes, bounded by a 60-minute job timeout) and carry ~200 MB of release assets per version.
- **Verification**: `go tool nsgo test ./cmd/editor`, `go tool nsgo test . -- -run TestStabilityTable`, `go tool nsgo build -target macos,windows,linux ./cmd/editor`, `actionlint`, `go run ./tools/changelog check`, `bun run verify-notes`, website `bun run test && bun run build`, and live PR CI execution of `.github/workflows/release.yml`.
