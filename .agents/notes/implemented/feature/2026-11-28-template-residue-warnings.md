# Agent Note: Template residue warnings in game check

Status: implemented

## Problem

A project made from a template can ship with the template's rules, data, name and README unchanged, presented as a new game, and nothing in `game check` says so. Issue #1960, part of #1923; it depends on the `origin` record of #1956, which PR #2029 implements in `game new`.

## Decision

`project.File` gains `origin` (after `appVersion`) with the shape #2029 writes, `kind`, `source` and `path` (the template's name), plus `keep`, the list of warnings to leave out (`rules`, `data`, `name`, `readme`, `credits`; any other value is refused at its line by `project.Validate`). The shape and the `kind`/`source` validation copy #2029's, so whichever of the two merges second resolves a conflict that is a union of the same fields.

`internal/gamecli/check_template.go` finds the template an origin of kind `template` names among the templates the CLI embeds (`projectTemplates`, by `path`, or by `source` module when there is no path) and compares files byte for byte:

- rules: `.go` files other than `main.go` (rendered by `game new`) and `_test.go`; data: `.yaml`, `.yml`, `.json`, `.csv` and `.yarn` files other than `game.yaml`, `nsgo.yaml`, `game.index.yaml`, `playtest.yaml` and `*.touch.yaml`, which are settings, recordings or layouts rather than game data. The unchanged ones are listed together in one warning at the `origin` line of `game.yaml`.
- `name`, and `window.title` when the template sets one, still equal to the template's `game.yaml`, at their lines.
- `README.md` and `CREDITS.md` still equal to the template's.

The warnings are kept out of `assetcheck.Report` and printed by the CLI as `file:line: warning: message` after the problems, also when the project has its own editor command; they never change the exit status, and the summary line counts them.

## Alternatives considered

- A changed-line threshold, as opengame uses: tolerant of whitespace edits but fuzzy and more code; exact comparison is cheap and needs no tuning. Byte comparison instead of SHA-256 digests gives the same answer without hashing, since both sides are in memory.
- A `Warning` field on `assetcheck.Problem` (the first version of this PR): it changed `Problem.String` and made `Report.OK` and `Summary` count advisory findings as problems for every other caller (the editor, agent evaluation). Keeping residue in the CLI leaves the package's contract unchanged; the editor's Problems panel and the editor MCP `game_check` tool do not show these warnings yet.
- Comparing against a template fetched from the module proxy at a recorded version: neither `game new` nor #2029 records a version, so the comparison uses the templates of the running CLI, which can report a file changed upstream as edited, never the reverse.

## Consequences

Projects without an origin, with an origin of another kind, or naming a template this CLI does not embed get no warnings. Until #2029 merges, `game new` does not write `origin`, so the warnings appear only for a project whose `game.yaml` has one written by hand. The editor and MCP surfaces of #1960 remain to be done.
