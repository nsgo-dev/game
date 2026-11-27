# Agent Note: Pass GOFLAGS=-tags=release and verify desktop binaries in release builds

Status: implemented

## Problem

`pkg/debug`, `pkg/tooling/hotreload`, `pkg/tooling/devstate`, `pkg/physics`, `pkg/physics3d`, `pkg/ai`, and `pkg/ai/navbake3d` compile out their development-only overlays, console, watchers, state persistence, and debug drawing behind `//go:build release` / `//go:build !release`. However, nothing invoking a release build set the `release` tag: `releaseplan.Args` emitted `go -C <dir> tool nsgo build ...` with no tags, `nsgo build` (NSGo `v0.3.4`) only adds `-trimpath -buildvcs=false` and its own internal tags (`nsgo_bundled`, `nsgo_steam`, `nsgo_android_dns`), and `nsgo.yaml`'s `build.tags` applies to every `go` command including `nsgo dev` and `nsgo test`. As a result, `game release`, the editor's Export and Build and Release screens, and plain `nsgo build` all shipped development builds of those packages.

## Decision

Propagate the `release` build tag through `GOFLAGS=-tags=release` from a single source of truth in `internal/releaseplan`, and verify built desktop executables after `nsgo build`:

1. **Single build-tag contract (`internal/releaseplan/plan.go`)**: `releaseplan.ReleaseTag` (`"release"`), `ReleaseEnv()`, `GOFLAGSWithReleaseTag`, `MergeGOFLAGS`, `WithReleaseEnv`, and `ApplyEnv` define how release invocations pass `-tags=release`. NSGo `v0.3.4`'s `cmd/nsgo/tags.go` reads `go env GOFLAGS`, parses `-tags=` entries via `goFlagTags`, and merges them with `nsgo.yaml`'s `build.tags` and NSGo's own per-target tags (`nsgo_bundled`, `nsgo_steam`, `nsgominiprogram`, `nsgo_android_dns`) before passing `-tags` to `go list` and `go build`. Merging into existing `GOFLAGS` preserves caller flags such as `-mod=mod` or custom tags without duplicating `release`.
2. **Unified CLI and editor propagation (`internal/gamecli/release.go`, `pkg/editor/export_panel.go`)**: `releaseplan.Plan` carries `Env` (`[]string{"GOFLAGS=-tags=release"}`) and formats `CommandLine` as `GOFLAGS=-tags=release go -C ... tool nsgo build ...`, keeping `game release -dry-run`, the editor's Export panel, and the Build and Release screen byte-for-byte identical. `game release`'s `runNSGo` applies `plan.Env` via `releaseplan.ApplyEnv`, and the editor's shared `defaultExportCommandRunner` (used by both the Export panel and Build and Release's Compile Go step) applies `releaseplan.WithReleaseEnv`.
3. **Post-build desktop binary verification (`internal/releaseplan/verify.go`)**: Because `nsgo build` strips symbol tables with `-ldflags "-s -w"`, `go tool nm` reports `no symbols` on built desktop binaries. `VerifyDesktopRelease` instead reads Go build info via `debug/buildinfo` (including every slice of universal macOS fat binaries via `debug/macho.OpenFat`) to require `-tags` containing `release`, and extracts Go function names from `.gopclntab` / `__gopclntab` via `debug/elf`, `debug/macho`, `debug/pe`, and `debug/gosym` to fail (`ErrDebugSymbolLinked`) if any `!release`-only symbol from `pkg/debug`, `pkg/tooling/hotreload`, `pkg/tooling/devstate`, `pkg/physics`, `pkg/physics3d`, or `pkg/ai` was linked. `game release` runs this check after `nsgo build` and before writing `CREDITS.md` whenever desktop targets were built.
4. **Size budgets, templates, docs, and CI**: `cmd/sizecheck` passes `-tags=release` for WebAssembly builds, `.github/workflows/quality.yml` and `.github/workflows/examples.yml` pass `GOFLAGS=-tags=release` for direct `nsgo build` steps, `.github/workflows/examples.yml`'s `release-build-tag` job builds `examples/fieldmedic` (which imports `pkg/debug`) for the host desktop target via `game release` and runs `pkg/debug` and tooling tests under `-tags=release`, and template READMEs plus `docs/{en,zh}/{debug,shipping,cli}.md` document both `game release` and `GOFLAGS=-tags=release go tool nsgo build`.

Native release-only tags in `nsgo.yaml` are tracked upstream in [nsgo-dev/nsgo#2695](https://github.com/nsgo-dev/nsgo/issues/2695). Once NSGo ships release-only tag configuration, templates and `game release` can migrate to it while keeping `VerifyDesktopRelease` as the safety net.

## Alternatives considered

- **Set `build.tags: [release]` in `nsgo.yaml`**: requires no CLI wrapper changes. Rejected because NSGo applies `build.tags` to `nsgo dev`, `nsgo test`, and `nsgo check`, stripping debug overlays during development and breaking tests that exercise `!release` paths.
- **Switch packages from `//go:build release` to `//go:build !nsgodev`**: works with NSGo `v0.3.4` without `GOFLAGS`. Rejected because plain `nsgo build` local playtests and `nsgo test` do not set `nsgodev`, which would disable debug paths in tests and local non-release builds and contradict the documented `-tags release` contract.
- **Inspect symbols with `go tool nm` alone**: simple subprocess call. Rejected because `nsgo build` links release binaries with `-s -w`, stripping the ELF/Mach-O symbol tables so `go tool nm` fails with `no symbols`; reading `debug/buildinfo` plus `.gopclntab`/`__gopclntab` via Go's standard library works reliably on stripped binaries across macOS (thin and universal), Linux, and Windows.

## Consequences

- **Benefits**:
  - Every `game release`, editor Export, Build and Release, and size-budget build now compiles with `-tags=release` and links the zero-cost release stubs of `pkg/debug`, `hotreload`, `devstate`, and debug drawing.
  - `game release -dry-run` prints a copy-pasteable command that includes `GOFLAGS=-tags=release`.
  - Post-build verification catches missing `release` tags or leaked debug symbols immediately on desktop release builds even on `-s -w` stripped binaries.
- **Costs**:
  - Direct `go tool nsgo build` invocations outside `game release` still require `GOFLAGS=-tags=release` until `nsgo-dev/nsgo#2695` lands upstream.
  - `go -C <dir> tool nsgo build` also inherits `GOFLAGS=-tags=release` when compiling the `nsgo` tool itself on a cold Go build cache; `nsgo` uses no `release` build constraints, so it compiles identically, at the cost of a separate cached tool binary per tag set.
  - `ForbiddenDebugSymbols` in `internal/releaseplan/verify.go` must be kept in sync if unexported `!release` helper names in `pkg/debug` or tooling packages are renamed or removed.
