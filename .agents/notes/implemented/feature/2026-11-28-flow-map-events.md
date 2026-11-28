# Agent Note: YAML map events and their resumable runner in pkg/flow

Status: implemented

## Problem

Map events such as one-time chests, signs and touch triggers were hard-coded by each template (rpg2d's `interact` and `arrive`), and `flow` had no file form for a command list that pauses and resumes. Issue #1755 asks for RPG Maker style map events; this change delivers the format and the interpreter core, not the full vocabulary or the rpg2d port.

## Decision

`pkg/flow/events.go` defines map-event format version 1 (`flow.MapEventVersion`), following the repository's YAML format rules: `ParseMapEvent` decodes with unknown fields refused, refuses duplicate keys, a missing or future version, an empty document and a trailing document, and returns `loaderr.Error` values; `EncodeMapEvent` writes the canonical form, which always spells out each page's trigger because a page without one reads as `action`; `TestMapEventFormatConformance` checks `testdata/mapevent` and the `flow#mapevent` entries of both `formats.md` pages.

- **Page selection**: `MapEventDoc.ActivePage` returns the last page whose conditions hold, as RPG Maker does and as the issue specifies, so a later page overrides an earlier one when both match. Self switches and switches compare for equality; a variable condition holds when the variable is at least the value, RPG Maker's variable condition.
- **Triggers are data, not behavior**: the runner does not watch input or movement. The game decides when `action`, `touch`, `autorun` or `parallel` fires and calls `EventRunner.Start(page)`, which keeps the runner free of scene and input dependencies.
- **Runner**: `EventRunner.Update(dt)` runs commands until a `wait` or the end of the page and returns whether the runner is idle. Time left over when a wait ends carries into the next commands, so two one-second waits end after two seconds of steps regardless of step size.
- **Snapshot**: `EventSnapshot` (version 1) holds the page (-1 when idle), the next command index, the remaining wait and the runner's self switches and variables. `Restore` validates the snapshot against the runner's document (page and command in range, a remaining wait only right after a wait command) and leaves the runner unchanged on error.

## Alternatives considered

- **Keeping event commands in Go**: rejected because the editor, agents and mods cannot read or author them.
- **Reusing dialogue scripts**: rejected because map events need page conditions and self switches, while dialogue has a different lifetime and presentation.
- **First matching page**: simpler to explain, but it inverts RPG Maker's rule that authors coming from it rely on, where the "after" page is listed after the "before" page.
- **Wiring the runner to `flow.Vars` now**: the runner keeps its own `Self` and `Variables` maps so the format could land without deciding how self switches map to `Vars.Self` object IDs; that integration is follow-up work under #1755.

## Consequences

- **Benefits**: a chest, a sign or a one-time trigger is a reviewed text file with a canonical form, and a save mid-event resumes at the same command and wait.
- **Costs**: the command set is small (`say`, `wait`, `set`, `self`). Choices, conditional branches, item, transfer, sound and move commands, game-registered commands, integration with `Vars` and the dialogue runner, and turning rpg2d's items and exits into map events remain open in #1755, with movement routes waiting on #1752.
