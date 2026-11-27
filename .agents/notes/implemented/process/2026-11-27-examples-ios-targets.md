# Agent Note: Examples declare all six default platforms and vet iOS in CI

Status: implemented

## Problem

Although the README promises iOS for every game and `releaseplan.DefaultPlatforms` lists six platforms (`macos`, `windows`, `linux`, `web`, `ios`, `android`), most examples omitted `ios` from `nsgo.yaml` `build.targets` and `game.yaml` `export.presets` (and `cryptquest` and `starburst` had no `build` or `export` sections at all). Because `.github/workflows/examples.yml` only ran iOS checks for `caverun` and `tutorial`, new or edited examples could silently omit iOS or stop compiling for iOS without failing CI.

## Decision

Every example under `examples/` that carries an `nsgo.yaml` (except the eight work-in-progress Star Catcher tutorial chapters `tutorial/ch01-empty-project` through `tutorial/ch08-save`, which omit `build` until `ch09-ship`) declares `build.output: dist` and `build.targets: [macos, windows, linux, android, web, ios]`. Every example with a `game.yaml` declares matching `export.output: dist` and `export.presets` for all six platforms so `project.Check` keeps `game.yaml` and `nsgo.yaml` in sync. `examples/dedicatedserver` remains without `nsgo.yaml` because it is a plain headless Go server process.

Two automated guardrails enforce this contract:

- `internal/repocheck/examples_test.go` (`TestExamplesDeclareAllDefaultPlatforms`) walks `examples/` and fails whenever a non-exempt example's `nsgo.yaml` `build.targets` or `game.yaml` `export.presets` misses any platform in `releaseplan.DefaultPlatforms`. `TestCheckExampleTargetsDetectsDrift` tests the scanner against synthetic missing-platform fixtures.
- `.github/workflows/examples.yml` adds an `examples-ios` job on `depot-macos-15` that loops over every `examples/*/go.mod` other than `caverun` and `tutorial` (which keep their own macOS test + iOS check matrix entries in `apple`) and runs `go tool nsgo check -target ios,ios_simulator` in each module.

## Alternatives considered

1. **Add every example module to the `apple` matrix job in `.github/workflows/examples.yml`**: each example would also run `go tool nsgo test ./...` on macOS in parallel. Rejected because every example already runs `go tool nsgo test ./...` on `depot-ubuntu-24.04-4` in the `examples` matrix, and spinning up one `depot-macos-15` runner per example multiplies macOS runner usage instead of sharing one warm Go/Xcode build cache across the other examples.
2. **Put the drift test in `internal/gamecli` or a shell script in CI**: `internal/gamecli` already checks `template/`, and a shell script avoids Go code. Rejected in favor of `internal/repocheck`, which is the dedicated home for repository-wide consistency tests (`formats_test.go`, `stability_test.go`, `conformance_test.go`, `skill_test.go`) and runs from the repository root via `main_test.go`.
3. **Add `build.targets` to `cryptquest` and `starburst` `nsgo.yaml` without adding `export` to their `game.yaml`**: keeps `game.yaml` shorter. Rejected because `project.Check` (`pkg/project/nsgo.go`) enforces that `nsgo.yaml`'s `build.output` and `build.targets` match `game.yaml`'s `export.output` and `export.presets`, and both examples' `project_test.go` run `project.Check`.

## Consequences

- **Benefits**: Copying any example's `nsgo.yaml` or `game.yaml` starts with all six platforms including iOS. New examples added under `examples/` are automatically checked by both `TestExamplesDeclareAllDefaultPlatforms` and the dynamic `examples-ios` CI loop without editing workflow files.
- **Costs**: Adding a new platform to `releaseplan.DefaultPlatforms` will fail `TestExamplesDeclareAllDefaultPlatforms` until every non-exempt example declares it. `examples-ios` adds one `depot-macos-15` job to the Examples workflow, taking roughly two minutes to vet all non-`caverun`/`tutorial` example modules sequentially on a shared build cache.
