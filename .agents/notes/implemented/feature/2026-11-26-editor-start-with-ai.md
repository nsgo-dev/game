# Agent Note: Editor Start with AI first-run flow (#1051)

Status: implemented

## Problem

The Start Screen ([2026-11-25-editor-start-screen.md](2026-11-25-editor-start-screen.md)) offered a **Start with AI** placeholder that displayed a static notice instead of guiding a first-time user from one sentence to a reviewable first scene. Implementing `docs/design/editor/FirstRun.dc.html` introduces cross-file contracts and safety invariants that code alone does not make obvious:

1. No project folder or `.mcp.json` may be written to disk until the author clicks **Approve** on the first plan.
2. **Stop and delete the folder** during Building is a destructive filesystem operation that must never touch a pre-existing directory, a sibling directory, or the user's home directory.
3. First-review onboarding tips must show one at a time, once per person across restarts (stored outside any project), and come back from **Help › Replay tips**.

## Decision

The Start with AI journey lives in `packages/game/editor` (`first_run.go`, `first_run_ui.go`) integrated with `StartScreenView`, `EditorView`, `EditorSettings`, the unified command registry, and a new top-level **Help** menu:

- **Five-stage state machine (`FirstRunStage`)**:
  - Step 1 (Start Screen) keeps the idea and project name in `startScreenState`, shows agent connection status beside **Start with AI**, and renders **WHAT HAPPENS NEXT** (Connect an agent, Approve its plan, Review the first scene).
  - Step 2 (`FirstRunStageConnect`) renders **in place** inside `StartScreenView` (`StartScreenOpen()` remains `true`) so **Edit** and **Later** return directly to Step 1 without losing the idea or name. Local CLI agents (`claude`, `codex`) are probed on `PATH` (or via `Editor.FirstRunDetectAgents`), MCP configuration is staged in memory rather than written into the target directory, and pasted API keys are validated immediately via `Editor.FirstRunValidateAPIKey` and kept only in memory (never written to YAML or diagnostics).
  - Step 3 (`FirstRunStagePlan`) switches to a full-window view with a read-only template preview, planned dashed-violet ghosts (`FirstRunGhost`), optional clarifying question with a suggested answer, **Just do it**, and **Approve ⌘↵**. No directory exists on disk until `ApproveFirstRunPlan` runs.
  - Step 4 (`FirstRunStageBuilding`) verifies `os.Stat(targetDir)` returns `os.ErrNotExist` before creating `targetDir`, then records `createdByFlow = true` and the canonicalized `createdDir` path. The UI renders the 5-stage progress bar (`Template`, `Project`, `First scene`, `Check`, `Playtest`) and concrete object counts (`19 of 31 objects`). Pressing **Cancel** or `Esc` pauses the builder via a condition-like channel (`WaitIfPaused`) and opens **Stop building?**. `StopFirstRunDeleteFolder` is offered and permitted only when `CanDeleteFirstRunFolder()` is true (`createdByFlow && createdDir != ""` and `isSafeOwnedProjectDir`), detaches the project if open inside `createdDir`, removes only `createdDir`, and clears ownership. A failed `Check` stage pauses before review with `FirstRunBuildFixProposal` (**Fix it** / **Review as is**).
  - Step 5 (`FirstRunStageReview`) displays the plan summary with amber deviations (`FirstRunPlanDeviation`), **Before / After** toggle plus a stable-scope `core.KeyHeld(cx, "b")` hold-`B` peek (`Before: an empty folder`), per-change revert/restore toggles, **Revert all**, **Keep N of M**, and three one-at-a-time onboarding tips.

- **Per-user tip persistence (`EditorSettings.SeenTips`)**:
  - `EditorSettings` (schema v1, `apps/website/public/schemas/editor-settings.v1.json`) gains an optional `seenTips` string list (`yaml:"seenTips,omitempty"`), validated against empty/whitespace/duplicate IDs and canonically sorted on encode.
  - The three first-run tip IDs (`first_review.keep_revert`, `first_review.hold_b`, `first_review.ask_selection`) are recorded one by one on **Next** / **Got it**, or all at once on **Skip tips**.
  - A new top-level **Help** desktop menu (`menubar.go`, localized in `en.yaml` and `zh.yaml`) exposes **Help › Replay tips** (`CmdReplayTips`), which removes only the first-run tip IDs from `SeenTips`, persists `editor.yaml`, and immediately restores tip 1 if a first review is open.

- **Injectable hooks for deterministic tests and future agent issues**:
  - `FirstRunDetectAgents`, `FirstRunConnectAgent`, `FirstRunValidateAPIKey`, `FirstRunPlanner`, and `FirstRunBuilder` keep #1051 decoupled from the still-open #1009, #1035, and #1014 issues while allowing those issues to plug directly into the same first-run flow without rewriting the Start-to-Review UX.

## Alternatives considered

1. **Create the project folder and `.mcp.json` during Connect (Step 2)**: matches a literal reading of the static Connect copy in `FirstRun.dc.html`, which shows "Wrote .mcp.json in ~/games/dungeon-run". Rejected because issue #1051's acceptance criteria explicitly require that no project folder exists on disk until **Approve**, and writing files before approval breaks the promise "Nothing is written before."
2. **Store seen tips in a separate YAML file (such as `tips.yaml`)**: avoids adding a field to `EditorSettings`. Rejected because issue #1051 specifically specifies keeping what was seen in the editor settings file, and `EditorSettings` v1 already stores optional per-user UI preferences (`viewportOverlays`, `shortcuts`) with strict schema validation and JSON Schema generation.
3. **Wait for #1009, #1035, and #1014 to land before wiring Connect/Plan/Review**: would reuse their final panels directly. Rejected because #1009, #1035, and #1014 are open, and defining clean hook boundaries (`FirstRun*` fields on `Editor`) lets #1051 ship end-to-end now while giving those issues explicit attachment points.

## Consequences

- **Benefits**: First-time users can go from one sentence to a reviewed scene with zero premature disk writes, strict safety around folder deletion, full Dark/Light Spec token compliance, and persistent one-time review tips resettable via **Help › Replay tips**.
- **Costs**: Adding the top-level **Help** menu changed the desktop menu bar from 4 menus to 5 (`File`, `Edit`, `View`, `Project`, `Help`), requiring updates to desktop menu-bar tests. When #1009, #1035, and #1014 land, their implementations should wire into the `FirstRun*` hooks rather than duplicating Start-to-Review transitions.
- **Related notes**: Partially overlaps with [2026-11-25-editor-start-screen.md](2026-11-25-editor-start-screen.md) (Start Screen foundation), [2026-11-25-editor-i18n-and-appearance.md](2026-11-25-editor-i18n-and-appearance.md) (`EditorSettings` YAML format), and [2026-11-26-editor-spec-light-theme.md](2026-11-26-editor-spec-light-theme.md) (`SpecPalette` Dark/Light tokens).
