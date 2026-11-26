# Agent Note: Project automations that run the agent after a change

Status: implemented

## Problem

Authors want repetitive post-change tasks—checking a level after it saves, updating the game brief after a review is kept, scaffolding an example scene when a prefab is added, running a mobile profile check before export, or running a nightly balance playtest—to run automatically without manually prompting the agent every time. However, unattended agent work introduces two hazards: an automation could silently mutate project files without a safe rollback point, or could bypass human review and apply You-only settings or drafts.

## Decision

Add a shared, versioned YAML automation configuration (`automations.yaml`), a pre-run checkpoint and background task engine (`packages/game/editor/automations_format.go`, `automations.go`), and the Settings › Automations page (`automations_ui.go`, integrated into the unified `SettingsView` from [unified Settings pages](2026-11-26-editor-settings-project-pages.md) and the existing `AgentPanelView` / `AgentTurn` in `agent.go` / `agent_ui.go`):

- **Tracked `automations.yaml` vs git-ignored `.nsgo/` runtime state**: `Settings.dc.html` marks Automations as `Project · shared`, while starter templates ignore `.nsgo/`. Storing declarative rules in `automations.yaml` beside `game.yaml` (version 1, `KnownFields(true)`, strict trigger validation, deterministic 2-space YAML, published JSON Schema `https://game.nsgo.dev/schemas/automations.v1.json`) keeps the file reviewable and diff-clean in git. Mutable local runtime state—weekly run timestamps, daily trigger watermarks, pause-for-today date, and pre-run checkpoints—lives under `.nsgo/automations-state.yaml` and `.nsgo/checkpoints/<id>/`, so running an automation never dirties `automations.yaml`.
- **Five deterministic triggers**:
  - `file_changed`: slash-normalized project-relative glob (`*`, `**`, `?`) fired from scene/project saves, autosave commits, and tab saves (skipping internal `.nsgo/`, `.game/`, `.git/`, `dist/`, and `automations.yaml` paths to prevent self-trigger loops).
  - `review_kept`: fired when reviews are kept (`KeepReview`, `EndReview(true)`, `KeepTileReview`, `KeepCutsceneReview`, `KeepAgentTurnReview`), skipping the originating automation ID to avoid recursion.
  - `file_added`: file-kind classifier (`prefab`, `scene`, `tileset`, `cutscene`, `material`, `particles`, `rig`, `screen`, `datatable`, `script`, `asset`, `any`) plus optional glob, fired on `NewScene` and project-tree additions in `PollDiskChanges`.
  - `before_export`: optional platform filter (`ios`, `android`, `web`, `macos`, `windows`, `linux`) fired before `StartExportBuild` and `StartReleaseBuild`.
  - `daily`: 24-hour `HH:MM` clock trigger evaluated periodically while a UI scope is attached and synchronously via `StepAutomations()`, firing at most once per calendar day while the editor is open.
- **Checkpoint first, Ready to review always, You-only enforced**: Every automation run calls `takeAutomationCheckpoint` before invoking `AgentRunner` (writing `manifest.yaml` and bounded, non-symlink text file snapshots under `.nsgo/checkpoints/<id>/`), wraps execution in `WithAgentActor` with `AgentRequest.Permissions` set to `Editor.AgentPermissions()`, and lands its `AgentTurn` in `AgentReviewReady` (`Ready to review`) with **Keep**, **Dismiss**, and **Restore checkpoint** actions. All You-only mutations (`KeepAgentTurnReview`, `DismissAgentTurnReview`, `KeepAutomationDraft`, `SetAutomationEnabled`, `AddAutomationRule`, `RemoveAutomationRule`, `SetAutomations`, `PauseAutomationsForToday`, `ResumeAutomations`, `RestoreAutomationCheckpoint`) refuse `e.AgentActorActive()` with `ErrAutomationYouOnly`.
- **Pause all for today**: `PauseAutomationsForToday` records the editor clock's calendar date (`YYYY-MM-DD`); `AutomationsPausedToday` suppresses every trigger while `today <= pausedDate` and resumes automatically on the next calendar day (or immediately via `ResumeAutomations`).
- **Settings › Automations UI**: Adds the `AGENT` sidebar group in `SettingsView` with an `Automations` entry (`<N> on` or `Paused`), renders `WHEN → THE AGENT` cards with weekly run counts and `SpecSwitch` (`core.RoleSwitch`, `aria-checked`, 2 px accent focus ring), **+ New automation, in words** (agent drafts a reviewable YAML card via `DraftAutomationFromWords`, kept only by the human via `KeepAutomationDraft`), **Pause all automations for today**, and the **Agent on this page** card, strictly using `SpecPalette` tokens in both Spec Dark and Spec Light.

## Alternatives considered

- **Storing `automations.yaml` inside `.nsgo/automations.yaml`** — Keeps the project root uncluttered. Rejected because starter `.gitignore` files ignore all of `.nsgo/`, which would prevent teams from sharing automations in version control despite `Settings.dc.html` specifying `Project · shared`.
- **Storing weekly run counts and `pausedDate` inside `automations.yaml`** — Fewer files on disk. Rejected because every automation run or pause toggle would mutate `automations.yaml` with timestamps and counters, breaking deterministic round-trips and creating noisy git diffs.
- **Building a separate `AutomationPanelView` or `AutomationTask` type instead of extending `AgentTurn` / `AgentPanelView`** — Isolates automation UI from chat turns. Rejected because `Tasks.dc.html` and `Settings.dc.html` explicitly require automation results to land in the Agent panel alongside interactive requests as Ready to review, and duplicating `AgentPanelView` or `AgentTurn` would fragment review controls.

## Consequences

- **Benefits**:
  - Teams can review, version, and schema-validate project automations in plain YAML (`automations.yaml`), while local checkpoints and run counts stay git-ignored under `.nsgo/`.
  - Every automation run guarantees a pre-run checkpoint and a human-only review gate before any result is kept.
  - Headless tests can drive every trigger, daily clock rollover, and pause-until-tomorrow transition deterministically using `Editor.Now` and `StepAutomations()` without real sleeps.
- **Costs and known limits**:
  - `defaultDraftAutomationFromWords` uses a deterministic English/Chinese keyword parser when `Editor.AutomationDrafter` is nil; projects that want full LLM-based drafting plug in `Editor.AutomationDrafter`.
  - Checkpoints snapshot bounded regular text/YAML/Go files (up to 256 files, 2 MiB each, skipping symlinks) rather than large binary media assets.

## Verification

- `(cd packages/game && env -u GOROOT go run ./cmd/gameschema -check)`
- `(cd packages/game && env -u GOROOT go tool nsgo check ./editor)`
- `(cd packages/game && env -u GOROOT go tool nsgo check -target ios ./editor)`
- `(cd packages/game && env -u GOROOT go tool nsgo check -target ios ./editor/editorcmd)`
- `(cd packages/game && env -u GOROOT TMPDIR=/tmp go tool nsgo test . -run TestFormatsPage)`
- `(cd packages/game && env -u GOROOT TMPDIR=/tmp go tool nsgo test ./editor -run 'TestAutomations|TestLocaleCatalogsComplete')`
- `(cd packages/game && env -u GOROOT TMPDIR=/tmp go tool nsgo test -target macos -run TestSettingsAutomationsMacOSScreenshots ./editor/editorcmd)`
- `env -u GOROOT go run ./tools/changelog check && bun run verify-notes`
