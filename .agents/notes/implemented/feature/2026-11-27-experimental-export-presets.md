# Agent Note: Experimental export preset warnings and editor badges

Status: implemented

## Problem

`game.yaml` export presets, `game release`, and the scene editor accept all ten NSGo platforms, including `tvos`, `watchos`, `harmonyos`, and `miniprogram`. None of those four are built or vetted by `.github/workflows/check.yml`, and NSGo `v0.3.4` disables audio and GPU views on all four. Without a warning, developers selecting one of those targets encounter late `nsgo build` failures or silent builds with empty GPU views, while removing the platforms outright would block experimentation and create churn as upstream NSGo closes those gaps.

## Decision

Keep accepting `tvos`, `watchos`, `harmonyos`, and `miniprogram` across `game.yaml`, `game release`, and the scene editor, and mark them experimental from a single source of truth in `internal/releaseplan`:

1. **Single authoritative set (`internal/releaseplan/plan.go`)**: `releaseplan.Experimental` sits beside `releaseplan.DefaultPlatforms` and maps each experimental platform (`tvos`, `watchos`, `harmonyos`, `miniprogram`) to an `ExperimentalInfo` naming what is missing and its upstream `nsgo-dev/nsgo` tracking issue (`#1317`, `#1318`, `#1521`, `#1505`), alongside `PlatformSupportURL` / `PlatformSupportURLZH` pointing to the `#experimental-targets` section created by [platform support matrix](../../implemented/process/2026-11-27-platform-support-matrix.md). `ExperimentalPlatforms` deduplicates sub-targets (such as `tvos_simulator` or `miniprogram_wechat`) to one entry per platform in first-seen order.
2. **CLI warnings and `-strict` (`internal/gamecli/release.go`)**: `releaseplan.Build` resolves targets and populates `Plan.Experimental`. By default, `game release` prints one formatted warning to `stderr` per experimental platform built (including under `-dry-run`, keeping `stdout` clean). Passing `-strict` sets `Options.Strict`, causing `releaseplan.Build` to return `*releaseplan.ExperimentalTargetsError` (wrapping `ErrExperimentalTarget`) so `game release` exits 1 before invoking `nsgo build`.
3. **Non-blocking editor markings (`pkg/editor/export_panel.go`, `pkg/editor/build_release.go`, `pkg/editor/build_release_ui.go`)**: `Editor.ExportPresetExperimental` and `Editor.OpenExperimentalPlatformDocs` expose the metadata without leaking `internal/releaseplan` into `pkg/editor`'s public API. Both the Export settings panel and the Build & Release screen display an `Experimental` badge and clickable platform-support link (opening `OpenBrowserURL` with locale-aware docs URLs) without marking presets unready or blocking builds, and the Export panel's output log prepends the same warning lines as `game release`. This extends [editor export presets](../../implemented/feature/2026-11-25-editor-export-presets.md).
4. **Promotion contract**: When NSGo enables audio and a GPU view on one of the four platforms, remove it from `releaseplan.Experimental` and add it to `.github/workflows/check.yml`'s target list in the same pull request.

## Alternatives considered

- **Reject experimental platforms in `project.Parse` / `parseExportSettings`**: prevents users from hitting unsupported targets at all. Rejected because it blocks developers who want to test 2D or early experimental builds and would have to be reverted as NSGo closes `#1521`, `#1317`, `#1318`, and `#1505`.
- **Maintain separate experimental lists in `internal/gamecli` and `pkg/editor`**: avoids touching `internal/releaseplan`. Rejected because the CLI warning, `-strict` check, and editor badges would drift when a platform graduates from experimental status.
- **Make `-strict` the default and require `-allow-experimental` to build**: prevents accidental experimental builds even without flags. Rejected because existing projects with experimental presets in `game.yaml` would break unexpectedly; warning by default and opting into `-strict` for CI matches Go and NSGo tooling conventions.

## Consequences

- **Benefits**:
  - One table (`releaseplan.Experimental`) drives CLI warnings, `-strict` failures, editor Export panel badges, and Build & Release screen banners.
  - Sub-targets (`tvos_simulator`, `watchos_simulator`, `miniprogram_*`) deduplicate cleanly to one warning per platform.
  - CI pipelines can enforce primary-target-only releases with `game release -strict` without removing experimental support for local experimentation.
- **Costs**:
  - `releaseplan.Experimental` must be kept in sync with `docs/{en,zh}/platforms.md` and `.github/workflows/check.yml` whenever NSGo upgrades close platform gaps.
