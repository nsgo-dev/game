# Agent Note: Real package measurement and headless smoke playtest in Build and Release

Status: implemented

## Problem

The editor's Build and Release screen (`pkg/editor/build_release.go`) previously fell back to fabricated numbers whenever `Editor.ReleasePlaytestRunner` and `Editor.ReleaseMeasureRunner` were unset (which was always true outside tests):

- `defaultSmokePlaytestResult` reported `"0 errors, 0 crashes, main scene loaded and stepped cleanly."` with a constant `11.2 ms` p95 frame time without running the game.
- `measurePresetArtifact` walked the project's source tree counting `.go` files as code bytes, padded every small category with constants (`+6 MiB` web / `+9 MiB` native code, `4 MiB` textures, `1 MiB` audio, `400 KiB` other), fell back to `11.2 ms` / `9.8 ms` p95 frame times, and hardcoded `http://127.0.0.1:8731/` for Web builds.
- `DownloadBudgetMet` and `FrameTimeBudgetMet` were then marked true against `export.budgets` from those invented constants, misleading authors into trusting passing green budget checks when nothing had been measured (`nsgo-dev/game#1618`).

## Decision

1. **Built-package measurement in `pkg/tooling/sizecheck`**:
   - Main already moved `cmd/sizecheck`'s WebAssembly (`MeasureWasm`, `EvaluateBudget`, `LoadBudget`) and mobile archive inspection (`InspectMobilePackage`, `CheckMobilePackage`) into the importable `pkg/tooling/sizecheck` package behind `game size`, so the editor imports that package instead of adding a second copy.
   - Add `MeasureBuiltArtifact`, `PackageSlug`, `ReleaseName`, and `PackageDir` (`pkg/tooling/sizecheck/package.go`) matching NSGo's actual `nsgo build` output layout (`<outDir>/<Name>-<Version>-<target>` and `<outDir>/releases/<slug>-<version>-<target><ext>`), returning `Found: false` with zero bytes whenever no built artifact exists in the output directory.
   - Map real inspected archive and directory entries into `CategoryTotals` (`CodeBytes`, `TexturesBytes`, `AudioBytes`, `OtherBytes`) without reading source trees or adding constant padding; mobile archives fold their `InspectMobilePackage` categories into these totals.

2. **Honest Build and Release measurement and smoke playtest (`pkg/editor/build_release.go`, `pkg/editor/build_release_ui.go`)**:
   - Remove `defaultSmokePlaytestResult` and every constant size, frame time, and fallback URL from `pkg/editor/build_release.go`.
   - Extend `BuildSmokePlaytestResult` with `Ran bool` and `BuildArtifactResult` with `SizeMeasured bool`, `WasmGzipBytes int64`, `FrameTimeMeasured bool`, and `SmokePlaytestRan bool` (normalizing legacy injected runners in tests that populate positive values without setting the explicit flags).
   - For the **Smoke playtest** step when `Editor.ReleasePlaytestRunner` is nil: on non-dry-run builds targeting the current desktop host (`macos` on Darwin, `linux` on Linux, `windows` on Windows) where a built package exists in `plan.OutputDir` and `autoload.mainScene` is configured, parse, build, and step the main scene headlessly for 120 fixed 60 Hz frames (`sceneformat.Build` + `sceneformat.BuildPhysics`), checking for non-finite coordinates and measuring real per-frame wall-clock p95 duration. On dry runs, non-host targets (`web`, `android`, `ios`, cross-compiled targets), or runs with no built package, report `"not run"` with `SmokePlaytestRan: false`, `FrameTimeMeasured: false`, and `FrameTimeBudgetMet: false`.
   - For the **Measure** step when `Editor.ReleaseMeasureRunner` is nil: call `sizecheck.MeasureBuiltArtifact` against `plan.OutputDir`. Only set `DownloadBudgetMet` when `SizeMeasured && SizeBytes > 0`, and only set `FrameTimeBudgetMet` when `FrameTimeMeasured` with a finite positive p95; otherwise both booleans stay `false`.
   - Persist `build/last-release.yaml` after non-dry-run builds only when at least one preset has `SizeMeasured && SizeBytes > 0`, so runs that measure nothing never write or overwrite a baseline.
   - Update `BuildReleaseScreenView` (`buildDetailDoneView`, `buildBudgetCard`) to render a neutral **Not measured** badge when a metric was not measured, hide the synthetic breakdown bar when size was not measured, and show **Smoke playtest · not run** instead of a green checkmark when no smoke playtest ran.

## Alternatives considered

- **Running an in-process scene step on every target (including Web, Android, and iOS)**: gives a frame-time number on every platform without needing a browser or emulator. Rejected because host Go CPU timing of an editor scene does not measure WebAssembly/Chromium or mobile GPU/CPU performance and would falsely mark mobile/web frame budgets as met on an unrun target.
- **A separate `pkg/tooling/sizereport` package**: the first version of this change moved the measuring code into a new package. Rejected after main moved the same code to `pkg/tooling/sizecheck` for `game size`: two packages would hold the same budget and archive code, and `game release` (`nsgo-dev/game#1619`) can share `pkg/tooling/sizecheck` with the editor as it is.

## Consequences

- **Benefits**: Build and Release never fabricates passing smoke tests, frame times, or padded package sizes; real `.wasm`, `.aab`/`.apk`, `.ipa`, and desktop builds report true measured bytes and categories through one shared implementation; host desktop builds with a main scene execute a real headless smoke check and fail on broken scenes.
- **Costs**: Unmeasured dry runs and cross-target builds without an injected playtest runner now display neutral `"Not measured"` / `"not run"` cards instead of green `"Met"` badges.
- **Related**: supersedes the placeholder defaults noted in [editor build release screen](../feature/2026-11-26-editor-build-release-screen.md).
