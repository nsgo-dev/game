### Added

- `game new` and every template module ship a canonical `brief.yaml` with the template's pitch, genre and platforms and, where `playtest.yaml` takes its goals from the brief, those goals as feel targets (#1963).
- `game/project`: `brief.yaml` takes an optional `basis` (`locked`, `target` or `assumption`) on the brief, its `art` and `feel` sections and each feel target, and a `changes` list of `project.BriefChange` entries (date, field, from, to, reason); `brief.v1.json` describes both (#1963).
- `game/editor`: adding, removing or changing a feel target on the Game brief page or through `EditBrief` appends a `changes` entry that undo removes, and each target's badge shows its basis (#1963).
- `game/tooling/playtest`: `playtest.RunTest` lists the feel targets of the `brief.yaml` beside `playtest.yaml` in the report's `briefTargets` (`ApplyBriefTargetStatuses`, `BriefTargetReport`), `pending` when the playtest did not measure the metric and otherwise `met` or `missed` by the brief's own bounds (#1963).
