# Agent Note: Run the test suite on Windows in CI

Status: implemented

## Problem

Windows is one of the six targets the README promises, but no CI job ran any test on it: `test.yml`, `examples.yml` and `release.yml` tested only on `depot-ubuntu-24.04-4` and `depot-macos-15`, while Windows was covered only by cross-compiled `go tool nsgo check`, which vets but runs nothing. Path handling, line endings in the YAML round-trip tests, file locking in saves and the asset cache, and process spawning in the CLI can all regress on Windows unnoticed (#1305). `.github/actionlint.yaml` already declared `depot-windows-2025-4` and no job used it.

## Decision

Add `depot-windows-2025-4` to the existing test jobs, changing no test code:

- `test.yml`'s `test` job gains a third matrix entry. The `Install fish` step stays Linux-only; the completion tests already skip with a reason (`<shell> is not installed`) via `exec.LookPath` for any shell a runner lacks, so no Windows-specific skip is introduced.
- `examples.yml` gains a `windows` job mirroring the `apple` job's shape: a fixed matrix of `caverun` and `tutorial` running `go tool nsgo test ./...` on `depot-windows-2025-4`. The per-example `examples` matrix job stays Linux-only, as does `check -target` cross-vetting, which already covers compilation for Windows.
- `release.yml`'s tag-time `test` job gains the same third matrix entry.
- The Postgres store jobs and the template jobs stay on Linux: they exercise code paths that are not platform specific.
- `timeout-minutes` stays 20 for every job; it is raised only if a measured Windows run needs it.
- CONTRIBUTING.md's CI summary and the tutorial's ship chapter (English and Chinese) now say tests run on Linux, macOS and Windows.

## Alternatives considered

- **Extend the per-example `examples` matrix job in `examples.yml` with an os dimension**: covers every example on Windows, but the matrix comes from `fromJSON(needs.example-list.outputs.matrix)`, so adding an os axis means restructuring `example-list`'s output, and it doubles a matrix whose non-Cave-Run, non-tutorial examples exercise no platform-specific code the root module's own Windows run does not already cover. The `apple` job's fixed caverun/tutorial matrix is the established pattern for testing the playable examples on a second OS.
- **Skip the change until Windows failures are triaged locally**: rejected; there is no local Windows host, and the PR's own CI run is the triage run the issue asks for.

## Consequences

- **Benefits**: a Windows regression in path handling, line endings, file locking or process spawning now fails CI before a release ships; the previously unused `depot-windows-2025-4` label in `actionlint.yaml` is exercised, and `check.yml`'s Depot-runner guard still passes because the label is self-hosted, not GitHub-hosted.
- **Costs**: each push and pull request runs one more root-module test job and two more example test jobs on a Windows runner, and the release tag run grows by one job.
- **Verification**: `go tool nsgo check` and the workflow YAML parse locally; whether `go tool nsgo test ./...` passes on `depot-windows-2025-4` (and whether any test needs a reasoned `t.Skip` or a raised timeout) can only be proven by the PR's own CI run, which is the triage pass #1305 prescribes. Any failure whose cause turns out to be in NSGo itself is recorded as an nsgo-dev/nsgo issue and linked from the skip.
