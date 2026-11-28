# Agent Note: gamedebug visual log and its JSON Lines format

Status: implemented

## Problem

Debug shapes from `pkg/tooling/gamedebug` are drawn for the current frame only, so when an AI or combat decision needs investigating several seconds later, nothing of what the systems logged along the way is left. Issue #1732 asks for a gameplay debugger (per-entity categories) and an Unreal-style visual log (per-entity, per-step messages, snapshots and shapes that can be saved and scrubbed later).

## Decision

`pkg/tooling/gamedebug` gains `VLog`, a bounded, mutex-guarded log of `VLogEntry` (message or `Category` snapshot) and `VLogShape` (kind plus world-space points) records, each tagged with the fixed step and the entity:

- **Bounded memory**: entries and shapes each live in a fixed-capacity ring (`DefaultVLogCapacity` = 4096 when `NewVLog` gets 0 or less); once full a push overwrites the oldest record, with no per-record copying of the rest. Mobile sessions need predictable memory, and the editor's rolling replay keeps a bounded window too.
- **Category** is an interface (`Name`, `Snapshot(entity) []Field`) so AI, stats or navigation packages can describe an entity without `gamedebug` importing them. `VLog.Snapshot` records a category's fields as an `info` entry under the category's name, which is how status snapshots reach the log.
- **Queries**: `Entries(entity, category, from, to)` and `Shapes(entity, from, to)` return records in recorded order; empty strings match any and `to == 0` means no upper bound, the shape an editor timeline or an MCP tool needs.
- **Release builds**: like every other `gamedebug` feature, `vlog_release.go` replaces `VLog` with no-ops under `-tags release`; `Snapshot` does not call the category, and `ReadVLog` returns an error instead of pretending a file is empty.

## File format

`WriteJSONL` writes JSON Lines: a header `{"format":"gamedebug.vlog","version":1}` (`VLogFormat`, `VLogVersion`), then every entry and then every shape in recorded order, each line an object tagged `"type": "entry"` or `"type": "shape"` with lowercase keys. Output is deterministic (struct field order, no timestamps or IDs), so a log read back and saved again gives the same bytes. `ReadVLog` must refuse a missing or unknown header, an unknown record type or level, and unknown fields, as AGENTS.md requires of formats this repository defines. The entry is `tooling/gamedebug#vlog` on the File Formats page, checked by `TestFormatsPage`.

## Alternatives considered

- **Unbounded in-memory log**: simplest and loses nothing, but a long play session on a phone grows it without limit; a ring with a configurable capacity keeps the recent history that matters for a bug.
- **Dispatching records on the presence of a `message` key** (the first version of this change): needs no extra field, but an entry with an empty message or a snapshot with only fields is misread as a shape, and the file has no version to evolve under. An explicit `type` tag plus a versioned header costs a few bytes per line.
- **A binary capture owned by the renderer**: compact, but logs must be readable and diffable as text and portable back from a device, so JSON Lines, one record per line, streams and greps well.

## Consequences

- **Benefits**: games can record why an entity acted, filter by entity, category and step, and carry the recording off the device as a text file that a future editor panel can load; release binaries link none of it.
- **Costs and gaps**: the overlay does not yet draw the log or show categories for a picked entity, there is no editor Visual Log panel, timeline scrubbing or MCP query tool, no built-in categories for `ability`, `stats`, `ai` perception or navigation agents, no shape helpers (`Line`, `Arrow`, `Circle`, `Box`, `Text`) beyond the generic kind plus points, and no automatic saving beside the rolling replay; those remain open in #1732. Entries and shapes are saved in two runs rather than interleaved by step, so a reader that wants one timeline merges them by `step`.
