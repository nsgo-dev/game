# Agent Note: Git scene merge driver discovery, CLI check, and editor setup banner

Status: implemented

## Problem

Every project template ships a `.gitattributes` file routing `*.scene.yaml` and `*.scene.yml` to `merge=nsgo-scene`, while the driver definition itself lives in `.git/config`, which Git never shares across clones. Previously, `game new` only checked `os.Stat(<dir>/.git)`, so projects created inside a parent Git repository (such as a monorepo) or initialized later with `git init` never received `merge.nsgo-scene.driver`, and every clone silently fell back to Git's built-in 3-way line merge because Git does not warn when a named merge driver is unconfigured. Nothing in the CLI or the scene editor checked for this gap.

## Decision

Share Git merge-driver detection and configuration through a small stdlib-only package (`internal/scenemerge`) imported by both `internal/gamecli` and `pkg/editor`:

- **`game new`**: locate any enclosing repository with `git rev-parse --show-toplevel` from the new project directory instead of stat'ing `<dir>/.git`. When inside a repository (including a parent directory or worktree), configure `merge.nsgo-scene.name` and `merge.nsgo-scene.driver` automatically; otherwise print `After git init, run: go tool game merge -setup` right after the `cd … && go tool nsgo dev` hint.
- **`game merge -check [dir]`**: inspect the enclosing repository, `.gitattributes` chain from `repoRoot` down to `dir` (plus `git check-attr` for `.git/info/attributes` and cross-pattern overrides), and effective Git config (`git config --get merge.nsgo-scene.driver`, honoring local, worktree, global, and system config). Exit 1 with the exact `go tool game merge -setup` fix when `.gitattributes` names `nsgo-scene` without a configured driver (or when outside Git); exit 0 when configured or when `.gitattributes` does not reference `nsgo-scene`.
- **Editor project-open check and banner**: `Editor.OpenProject` calls `RefreshSceneMergeDriverStatus()` via `Editor.runGit`. When `status.NeedsSetup()` is true, `mergeDriverBannerView` renders a non-blocking warning bar directly below the toolbar (memoized like `diskWatchBannerView` so background problem checks never remount its buttons) with a one-click `Enable merge driver` button that calls `SetupSceneMergeDriver()` and a `Dismiss` action.
- **Template READMEs and docs**: document the per-clone `go tool game merge -setup` step in every template `README.md` (and its embedded copy under `internal/gamecli/_templates/projects/`) and update `docs/{en,zh}/cli.md`, `docs/{en,zh}/cli-reference.md`, `docs/{en,zh}/editor.md`, and `skills/nsgo-game/references/cli-and-formats.md`.

## Alternatives considered

1. **Duplicate the Git config and `.gitattributes` check separately in `internal/gamecli` and `pkg/editor`**: avoids adding an `internal/` package, but `pkg/editor` cannot import `internal/gamecli` (cycle via `internal/gamecli/editor.go`), and two separate implementations would drift on ancestor `.gitattributes` precedence, symlink canonicalization (`/var` vs `/private/var` on macOS), and `git check-attr` edge cases.
2. **Require `merge.nsgo-scene.driver` in `git config --local` only**: simpler than `git config --get`, but rejects clones where a developer intentionally configured `merge.nsgo-scene.driver` in `~/.gitconfig` (`--global`), even though Git uses it during merges.
3. **Fail `game merge -check` or show the editor banner even when `.gitattributes` does not name `nsgo-scene`**: catches repos that never adopted `.gitattributes`, but produces false positives for repositories that do not use the scene merge driver and for minimal editor test fixtures without `.gitattributes`.

## Consequences

- **Benefits**: `game new` works in parent repositories and tells standalone projects the exact post-`git init` step; teams and CI can enforce driver setup with `game merge -check`; opening any fresh clone in the editor surfaces the missing local driver immediately with one-click remediation.
- **Costs**: adds one lightweight Git check (`rev-parse`, `check-attr`, `config --get`) on `Editor.OpenProject` beside the existing Changes count check, and adds a small shared `internal/scenemerge` package.
- **Verification**: covered by `internal/scenemerge`, `internal/gamecli` (`TestMergeSetupAndNewInGitRepo`, `TestMergeUsageErrors`, `TestMergeEndToEndWithGit`, `TestReference`, `TestTemplatesMatchTemplateModules`), `pkg/editor` (`TestMergeDriverBannerAndOneClickSetup`, `TestMergeDriverBannerNestedParentRepoAndDismiss`, `TestMergeDriverBannerNotShownWhenNotNeededAndShowsErrorOnFailure`, `TestLocaleCatalogsComplete`), and real macOS before/after `nsgotest` screenshots in `pkg/editor/editorcmd/merge_driver_screenshot_test.go`.
