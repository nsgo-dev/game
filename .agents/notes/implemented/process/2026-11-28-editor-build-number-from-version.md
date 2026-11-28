# Agent Note: Editor releases derive the build number from the version, and the editor checks for updates on request

Status: implemented

## Problem

No released NSGo Game Editor offered an update. `cmd/editor/nsgo.yaml` sets `build_number: 1`, and `.github/workflows/release.yml` built every release with it, so the appcast of 0.1.0, 0.2.0 and 0.3.0 all carry `sparkle:version` 1. NSGo's updater compares an appcast item's `sparkle:version` with the running copy's build (`internal/platform/updates/updates.go` in nsgo v0.3.4: `Newer(items[0].Build, c.build())`, the build being `CFBundleVersion` from `BundleVersion` in `cmd/nsgo/project.go`), as Sparkle and WinSparkle do, so every release compared equal to the installed copy and none was newer. Users could not ask either: the editor never called `updater`, so it had no **Check for Updates...** item, and the daily background check, which Sparkle and WinSparkle run on their own, found nothing.

## Decision

- The `meta` job of `release.yml` derives the build number from the release version and passes it to `go tool nsgo build -build-number`: `X.Y.Z` is `X*1000000 + Y*10000 + Z*100 + 99`, and a prerelease `X.Y.Z-<name>.N`, with `N` from 1 to 98, ends in `N` instead of 99, so a release candidate sorts below its release and above the release before it (0.3.0 is 30099, 0.3.1-rc.1 is 30101, 0.3.1 is 30199). A version outside that shape, a minor or patch number over 99, or a prerelease number outside 1–98 fails the job before anything builds. `nsgo.yaml` keeps `build_number: 1` for local builds.
- The editor registers `CmdCheckForUpdates` (`help.checkForUpdates`), listed last in the Help menu and in the Command Palette, which calls `updater.Check` on the editor's scope. NSGo shows the answer: Sparkle's and WinSparkle's windows, an alert that installs the signed package on Linux, or the reason a development copy does not update.

Every number is above 1, so installed copies of 0.1.0 to 0.3.0, whose build is 1 and which already read the latest release's appcast (`SUFeedURL` is `https://github.com/nsgo-dev/game/releases/latest/download/appcast-<target>.xml`), update to the first release built this way without reinstalling.

## Alternatives considered

1. **Remove `build_number` so the build follows the version**: `CFBundleVersion` would be `0.3.1`, and Sparkle's comparison runs by run of digits, so `0.3.1` sorts below the installed copies' `1` and they would never update; it also leaves release candidates and releases of one version equal.
2. **Use the workflow's run number**: always grows on pushes, but a `workflow_dispatch` rebuild of an older tag would get a larger number than a newer release, and the number would not be reproducible from the tag.
3. **Bump `build_number` in `nsgo.yaml` with each release**: one more edit for the release pull request to forget, which is how every release so far shipped with 1.

## Consequences

- **Benefits**: each release's build number is larger than every earlier release's and follows from its tag alone, so a rebuild of the same tag gets the same number; users can check for an update from the Help menu.
- **Costs**: minor and patch numbers are capped at 99 and prereleases at 98 per version, and the Windows version resource leaves out the build number once it no longer fits its fourth 16-bit field, from 0.7.0 on; Sparkle and WinSparkle compare the appcast, not that resource.
- **Verification**: the derivation run on `0.3.0`, `0.3.1-rc.1`, `0.3.1`, `1.0.0` and `1.0.0-beta.2` gives 30099, 30101, 30199, 1000099 and 1000002, and refuses `0.3`, `1.2.3-rc` and `0.100.0`; the published 0.3.0 appcast carries `sparkle:version` 1, the cause above.
