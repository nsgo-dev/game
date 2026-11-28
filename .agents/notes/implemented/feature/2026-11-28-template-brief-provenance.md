# Agent Note: Template brief provenance

Status: implemented

## Problem

Template `playtest.yaml` files named `source: brief.yaml` for their goals, yet no template shipped a `brief.yaml`, so a new project opened with an empty Game brief page and nothing told an agent which numbers were the author's decisions and which were defaults to tune. Playtest reports also listed only the metrics a playtest measured, so a brief target nobody measured was simply absent rather than visibly unverified (#1963).

## Decision

Every registered template ships a canonical version 1 `brief.yaml`, byte-identical in its module (`template/<name>/template`) and in the copy `game new` embeds (`internal/gamecli/_templates/projects/<name>`). Its pitch is the template's registry summary, and its feel targets are exactly the goals its `playtest.yaml` takes from the brief, with the same bounds, summary and unit. `TestTemplatesShipBriefsAndResolveBriefPlaytestGoals` fails when a brief is missing or not canonical, or when a `source: brief.yaml` goal has no target for the playtest's scene or differs from it.

`brief.v1` gains two optional, additive fields rather than a `brief.v2`: `basis` (`locked`, `target` or `assumption`) on the brief, `art`, `feel` and each feel target, and a `changes` list of `project.BriefChange` (date as `YYYY-MM-DD`, field, from, to, reason). Old files stay valid; unknown basis values and malformed dates are refused with their line. Templates mark the brief `assumption` and measured targets `target`; a target added on the Game brief page is `target`.

`Editor.EditBrief` compares the feel targets before and after each edit, keyed by scene and metric, and appends one `BriefChange` per added, removed or changed target, with `describeBriefTarget`'s one-line bounds and basis as `from` and `to` and the edit's label as `reason`. The entry is part of the same undoable step, so undo removes it.

The verification record is the playtest report's `briefTargets` list, not a second file. `playtest.RunTest` loads the `brief.yaml` beside the playtest config (the project root) and calls `ApplyBriefTargetStatuses`: a target whose metric the report does not contain is `pending`; a measured one is `met` or `missed` by the brief target's own bounds (`GoalFromBriefTarget(...).Evaluate`), not by the metric's status, because `playtest.yaml` may give the metric a different goal that `ApplyBriefTargets` deliberately leaves alone. `playtest.Run` itself does not read files, since `scope.project` is a project name, not a path.

## Alternatives considered

- A separate verification file beside the brief, as opengame's `design/verification.md` does. It keeps verification readable on its own, but it duplicates what the playtest report already records per run and the two drift; folding it into `playtest.report.yaml` keeps one source.
- Provenance in YAML comments. It needs no schema change, but comments do not survive `EncodeBrief` and are invisible to the editor and `project_context`, so the basis is a typed field.
- Deriving `briefTargets` status from the metric's `met`/`missed` status. Simpler, but wrong whenever `playtest.yaml` keeps its own goal for a metric the brief also targets, so the report evaluates the brief bounds directly.
- Recording one change entry per edit with a fingerprint of all targets. It captures everything, but the `from`/`to` strings become unreadable as targets grow; per-target entries say what changed.

## Consequences

New projects open with a filled Game brief and an honest list of which targets were measured. The brief schema and report gain optional fields only, so existing files load unchanged. Each template carries one more text file whose embedded copy must stay byte-identical, and a template's `playtest.yaml` goal and brief target now have to change together. Templates pin a released game layer, so their own playtest reports gain `briefTargets` only once the pin moves past this change. `changes` entries carry the UTC day of the edit, the one value in a saved brief that depends on when it was written; it changes only when a target is edited, never on a plain save. Editing the basis of an existing target on the Game brief page, and agent instructions that `locked` values are changed only by the person, are not part of this change.
