# Agent Note: Editor Build and Release screen with readiness, pre-build checks, and release size comparison

Status: implemented

## Problem

The existing docked Export panel ([editor export presets](2026-11-25-editor-export-presets.md)) lets authors edit `game.yaml`'s `export` presets and stream a single `nsgo build` invocation, but it cannot tell an author before building whether a platform (such as iOS) is ready to build, cannot isolate a signing failure on one target while finishing the other checked targets, cannot pause and resume per-preset build steps without restarting completed steps, and cannot compare finished artifact sizes or frame-time budgets against `game.yaml` thresholds and the last release (#1048). At the same time, preserving `ExportPanelView`, the compact 2D `editor-toolbar`, and byte-for-byte parity with `game release -dry-run` is mandatory.

## Decision

1. **Optional `export.budgets` in `game.yaml` (`packages/game/project`)**:
   - Add `Budgets *Budgets` (`qualityProfile`, `downloadSizeMB`, `frameTimeP95Ms`) to `project.Export` with validation in `validateExport`, deep copy in `File.Clone`, acceptance in `cmd/game/release.go`'s strict `knownFields` check, and regenerated `apps/website/public/schemas/game.v1.json`.
   - When `export.budgets` is omitted, the Build and Release screen displays and evaluates against defaults (`Low` profile, `80 MB`, `16.7 ms`) without mutating `game.yaml`.
2. **Sequential per-preset release orchestrator (`packages/game/editor/build_release.go`)**:
   - Add `OpenBuildReleaseScreen`, `CloseBuildReleaseScreen`, `BuildReleaseScreenOpen`, `StartReleaseBuild`, `StopReleaseBuild`, `ResumeReleaseBuild`, `SetBuildPresetIncluded`, `SetBuildPresetFixProposal`, `ApplyBuildPresetFixAndRebuild`, `RefreshBuildPreflight`, `EvaluateBuildReadiness`, `RunBuildReadinessAction`, and `RunBuildReadinessAgentSetup`.
   - Reuse `packages/game/internal/releaseplan` (`releaseplan.Build` and `releaseplan.WriteDryRun`) for both the combined included-presets plan and each individual preset's `Compile Go` / `Pack assets` steps, guaranteeing byte-for-byte parity with `game release -dry-run`.
   - Execute included, ready presets strictly one at a time through the 6 ordered steps (`Check project`, `Compile Go`, `Pack assets`, `Sign`, `Smoke playtest`, `Measure`) off the UI goroutine, streaming into `build/<platform>.log` (on disk for real builds and in memory for all runs).
   - **Stop and Resume**: cancelling an active run preserves `BuildStepDone` statuses and `NextStepIdx` on the interrupted preset; `ResumeReleaseBuild` skips already-`Done` presets and resumes the interrupted preset at `NextStepIdx`.
   - **Failure isolation and agent fix diffs**: a failing step (such as a missing signing keystore at `Sign`) marks only that preset `Failed`, records `BuildFailureNotice` (`"Other platforms keep building. Nothing was uploaded."`), and continues building every remaining included preset. Applying an attached `BuildPresetFixProposal` updates the preset's signing config / targets (and `export/<platform>.export.yaml` if present) and rebuilds that preset.
   - **Platform readiness**: `EvaluateBuildReadiness` delegates to the shared `internal/releaseplan.EvaluateReadiness` checks against `nsgo.yaml` (`id`, `icon.apple` / `icon.png`, `sign.identity`, `sign.profile`, `apple.privacy`), `resources/PrivacyInfo.xcprivacy`, and `*.touch.yaml` / `touchRegion` touch inputs ([iOS release readiness checks](../../implemented/bug-fix/2026-11-28-editor-ios-readiness-checklist.md)), marks platforms with blocking failures as `Set up` and unchecked by default, handles agent-capable fixes directly (syncing `nsgo.yaml`, writing `resources/PrivacyInfo.xcprivacy`, or generating `controls.touch.yaml` from `game.yaml` actions), and delegates person-required steps (provisioning profile and Apple signing setup) to `OnReleaseReadinessPersonAction`.
3. **Full-window Spec-tokenized screen (`packages/game/editor/build_release_ui.go`)**:
   - Render `BuildReleaseScreenView` in `EditorView` alongside `StartScreenView`, `MergeConflictView`, and `HUDLayoutView` using exclusively Spec design tokens (`specBg`, `specPanel`, `specRaised`, `specLine`, `specLineRaised`, `specAccent`, `specAgent`, `specAdded`, `specChanged`, `specProblem`, `specWarning` and their Spec alpha derivatives).
   - Register `CmdBuildRelease` (`project.buildRelease`, `mod+shift+r`) in the command registry, Command Palette, and `Project` menu bar without adding buttons to the 2D `editor-toolbar` and without breaking or removing `ExportPanelView`.

## Alternatives considered

- **Replacing `ExportPanelView` in place with the full-window screen**: Simpler wiring with a single command. Rejected because existing toolbar Export tests (`TestExportPanel*`) and workflows rely on `ExportPanelView` docked below the viewport, and keeping `ExportPanelView` intact avoids breaking those tests while exposing the full screen cleanly via `CmdBuildRelease` / `OpenBuildReleaseScreen`.
- **Adding a toolbar button for Build & Release**: Faster one-click discovery from the 2D workspace. Rejected because the 2D `editor-toolbar` must stay compact (`<= 2` rows at 1120×680), and full-window workflows like `MergeConflictView` already live in the `Project` menu and Command Palette.
- **Running all selected presets concurrently in `StartReleaseBuild`**: Shorter wall-clock build time on multi-core hosts. Rejected because `Build.dc.html` and issue #1048 explicitly require builds to run one preset at a time so the editor stays responsive and Stop/Resume semantics stay predictable.

## Consequences

- **Benefits**:
  - Authors see `nsgo check`, smoke playtest, `game.yaml` budgets, and platform readiness (including agent vs person setup split) before launching a release build.
  - Dry-run builds exercise all 6 ordered steps and emit the exact commands `game release -dry-run` prints.
  - Signing or step failures isolate cleanly to the failing platform while remaining checked platforms finish, and agent fix diffs apply and rebuild in one action.
  - Finished builds surface artifact paths, size deltas against the last release, `code`/`textures`/`audio`/`other` breakdowns, and budget Met/Missed badges.
- **Costs and known limits**:
  - Real notarization and emulator-backed mobile smoke playtests rely on injectable hooks (`ReleaseStepRunner`, `ReleasePlaytestRunner`, `ReleaseMeasureRunner`) until dedicated platform runners land; default store uploads now run `go tool nsgo publish` with environment-only keychain credentials ([editor review uploads publish](../bug-fix/2026-11-28-editor-review-uploads-publish.md)).
  - Adding `export.budgets` updates `apps/website/public/schemas/game.v1.json`, so any downstream schema consumer picks up the optional `budgets` object under `export`.

## Verification

- `(cd packages/game && env -u GOROOT go tool nsgo check ./project && env -u GOROOT go tool nsgo check ./cmd/game && env -u GOROOT go tool nsgo check ./editor && env -u GOROOT go tool nsgo check -target ios ./editor)`
- `(cd packages/game && env -u GOROOT go tool nsgo test ./project && env -u GOROOT go tool nsgo test ./cmd/gameschema && env -u GOROOT go tool nsgo test ./cmd/game -- -run TestRelease)`
- `(cd packages/game && TMPDIR=/tmp env -u GOROOT go tool nsgo test ./editor -- -run 'TestBuildReleaseScreen|TestExportPanel|TestOpenFromTheFileBrowserOpensTabs|TestDiscardFollowsTheProjectBack|TestLightingHandlesAlignOnParallaxLayer|TestDesktopMenuBarMatchesRegistryAndExecutesActions|TestLocaleCatalogsComplete')`
- `(cd packages/game && TMPDIR=/tmp env -u GOROOT go tool nsgo test -target macos ./editor/editorcmd -- -run TestBuildReleaseScreenMacOSScreenshots)`
- `env -u GOROOT go run ./tools/changelog check && bun run verify-notes`
