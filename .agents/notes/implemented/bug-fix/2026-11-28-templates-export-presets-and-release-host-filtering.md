# Agent Note: Template release export presets, portable nsgo.yaml targets, and host-aware game release

Status: implemented

## Problem

Every template's `game.yaml` listed `targets: [ios_simulator]` for its `ios` preset, and 13 of 16 templates omitted a `linux` preset. Because `pkg/project.File.SyncNSGo` copied every preset target verbatim into `nsgo.yaml`'s `build.targets`, plain `go tool nsgo build` and `go tool game release` failed on Linux and Windows hosts with missing `xcrun` / macOS tools, while on macOS `game release` shipped an iOS simulator bundle (rejected by devices, TestFlight, and App Store Connect) and produced no Linux release (`nsgo-dev/game#1669`).

## Decision

Separate full release presets (`game.yaml`, built by `game release` with host capability filtering) from portable everyday build targets (`nsgo.yaml`, built by plain `nsgo build` on every host):

1. **Template export presets (`template/template/`, `template/*/template/`, synced to `internal/gamecli/_templates/projects/`)**:
   - Every template's `game.yaml` declares all six primary platforms: `macos` (`macos_arm64`), `windows` (`windows_x64`), `linux` (`linux_x64`, `linux_arm64`), `ios` (`ios`), `android` (`android_arm64`), and `web`. `ios_simulator` stays documented for `nsgo dev` rather than release export presets.
   - Every template's `nsgo.yaml` sets `build.targets` to `[windows_x64, linux_x64, linux_arm64, android_arm64, web]` so a plain `go tool nsgo build` succeeds on Linux, Windows, and macOS without macOS-only tools (`codesign`, `ditto`, `hdiutil`) or Xcode (`xcrun`).
   - `TestTemplateExportPresets` in `internal/gamecli/templates_test.go` enforces that every template has an `ios` device preset (never `ios_simulator`), a `linux` preset (`linux_x64`, `linux_arm64`), all six primary platforms, and portable-only `nsgo.yaml` `build.targets`.
2. **Portable `nsgo.yaml` syncing (`pkg/project/nsgo.go`)**:
   - `IsPortableTarget` and `(*File).PortableBuildTargets` identify targets every host can build with plain Go (excluding Apple platforms `macos`, `ios`, `tvos`, `watchos` and NDK-requiring `android_x64`).
   - `SyncNSGo` writes `PortableBuildTargets()` into `nsgo.yaml`'s `build.targets` whenever at least one preset target is portable (falling back to `BuildTargets()` for Apple-only projects), so `gameproject sync` and editor saves preserve cross-host buildability.
   - `CheckNSGo` accepts either `PortableBuildTargets()` or the full `BuildTargets()` list so existing projects (such as `examples/*`, which declare all six default platforms in `nsgo.yaml` per [examples iOS targets](../process/2026-11-27-examples-ios-targets.md)) continue to pass unchanged.
   - Template `project_test.go` filters Apple presets on a `Project.Clone()` before calling `CheckNSGo(nsgoYAML)` so templates stay compatible with their pinned `github.com/nsgo-dev/game v0.1.0` module under `GOWORK=off` CI until template pins advance past `v0.1.0`.
3. **Host-aware `game release` and unsigned iOS reporting (`internal/releaseplan/plan.go`, `internal/gamecli/release.go`, `pkg/editor/export_panel.go`)**:
   - `releaseplan.Host` (`CurrentHost()` in production, `AllTargetsHost()` for cross-OS unit tests) probes `runtime.GOOS` and, lazily on `darwin`, `xcrun --sdk <sdk> --show-sdk-path`.
   - When targets come from `game.yaml` presets or defaults, `releaseplan.Build` skips targets the host cannot build (`macos*` off macOS; `ios`/`tvos`/`watchos` and their simulators off macOS or without the matching Xcode SDK), records them in `Plan.Skipped`, and prints `game release: skipped <target>: <reason>` on stderr while building the remaining targets (failing with `ErrNoBuildableTargets` if nothing remains).
   - When the user explicitly passes `-target`, any host-unavailable target fails before `nsgo build` runs with `*UnavailableTargetsError` wrapping `ErrHostUnavailableTarget`.
   - When the `ios` device target is included in the build plan and `nsgo.yaml`'s `sign.profile` is empty, `Plan.Unsigned` records it and `game release` prints `game release: note: ios is unsigned (...)` rather than silently dropping `ios`.

## Alternatives considered

- **Keep `macos_arm64` in template `nsgo.yaml` `build.targets`**: rejected because NSGo `v0.3.4`'s `buildMacApp` runs `codesign` and `buildDMG` runs `ditto` and `hdiutil`, so a plain `go tool nsgo build` would still fail on Linux and Windows.
- **Bump every template's `go.mod.tmpl` to an unreleased `main` pseudo-version so `project_test.go` can call the new `CheckNSGo` directly**: rejected because templates pin published releases (`v0.1.0`) and `GOWORK=off` CI tests against the pinned module before a PR merges to `main`. Cloning `Project` and filtering Apple presets in `project_test.go` verifies every field on both `v0.1.0` and `main` without changing module pins.
- **Apply `CurrentHost()` filtering unconditionally inside `releaseplan.Build` even when `Options.Host` is nil**: rejected because `pkg/editor` callers and tests construct `macos_arm64` plans on Linux CI with mocked runners; keeping `Options.Host` opt-in (always set by `game release`) preserves host-independent editor tests while enforcing real host filtering on the CLI.

## Consequences

- **Benefits**:
  - `go tool nsgo build` and `go tool game release` both succeed out of the box from any template on Linux, Windows, and macOS (with or without Xcode).
  - macOS releases with Xcode produce real iOS device IPAs (clearly noting when `sign.profile` is unset) alongside Linux x64/arm64 packages.
  - Explicit `game release -target ios` off macOS or without Xcode fails fast with a clear host error before invoking `nsgo build`.
- **Costs**:
  - `SyncNSGo` no longer mirrors Apple targets into `nsgo.yaml` when portable targets exist, so users running plain `go tool nsgo build` on macOS pass `-target macos,ios` or use `go tool game release` (which also sets `GOFLAGS=-tags=release`; see [release build tag](2026-11-28-release-build-tag.md)).
  - Once templates bump their pinned `github.com/nsgo-dev/game` version past `v0.1.0`, the `Project.Clone()` Apple-preset filter in `template/*/template/project_test.go` can be simplified back to `Project.CheckNSGo(nsgoYAML)`.
  - The user-facing ownership and synchronization workflow, its documentation test, and the checked-in store-listing validation are recorded in [Shipping guide follows game.yaml and validates store listings](../process/2026-11-28-shipping-guide-gameyaml-store-workflow.md).
