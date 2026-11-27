# Agent Note: undo.History move limit, consecutive-move merging and saved marker

Status: implemented

## Problem

`undo.History` kept every move until `Clear`, which fits a puzzle that undoes to the start of a level but not an in-game editor (level maker, base builder, character creator): a drag of a slider recorded one move per step, a long session grew the history without bound on phones and the web, and "unsaved changes" had to be tracked beside the history, going wrong after undo and redo. Issue #1365, part of #1354.

## Decision

`pkg/undo` gains three tools, all free of the clock, goroutines and package state:

- **Merge runs**: `Merger[M]` is a move with `Merge(next M) bool`. `History.DoMerge` applies the move like `Do` (discarding the redo branch first), then folds it into the latest applied move when that move is a `Merger` that accepts it; a refused merge records the move as usual. `EndMerge` sets a `noMerge` flag so the next `DoMerge` starts a new move. Only `EndMerge` and `MarkSaved` set the flag; plain `Do`, `Undo`, `Redo` and `Seek` do not, so a drag interrupted by an unrelated edit can still continue folding into the top move, and merging after an undo simply finds nothing (or a different move) on top.
- **Limit**: `History.Limit`, when positive, is enforced in `record` after each appended move: the oldest applied moves are dropped from the front (`forgotten` counts them). Their effects stay in the state — forgetting drops the move record, not the change — so the current state is always "the first `forgotten + applied` moves ever recorded", and `Seek` and `Log` count from the oldest kept move. Enforcement only on `Do`/`DoMerge` means a `Limit` lowered mid-session takes effect on the next recorded move.
- **Saved marker and version**: `Version()` returns `forgotten + applied`, an absolute coordinate that counts moves ever recorded, so it changes on every `Do`, `Undo`, `Redo`, `Seek` and `Clear`, while a folded `DoMerge` keeps it (the top move's place is unchanged). `MarkSaved` stores that coordinate and ends the merge run, so the first `DoMerge` after a save cannot rewrite the move the saved point rests on and report clean while the state diverges. `Dirty` compares the current coordinate with the saved one, which makes undoing back to the saved point report clean; two cases keep it dirty forever after: a new move discarding the redo branch the saved coordinate was on invalidates the marker, and a saved coordinate below `forgotten` is unreachable because `forgotten + applied` never goes below `forgotten`. `Clear` resets the coordinate space and invalidates the marker.

**Log format change**: `Log` gains a `forgotten` key between `applied` and `moves` and `LogVersion` is now 2. Version 1 logs (no `forgotten` key, from before `Limit`) are still decoded, and `EncodeLog` normalizes any log to the current version, so a v1 file saves again in v2 form; a v1 log with a nonzero `forgotten` is invalid. `Restore` refuses a log with forgotten moves: their effects are part of the history's state but not of the starting state the log replays from, and the log no longer holds the moves that would replay them — the remedy is the existing `Clear` contract (save the state at the `Clear` with the log). The `undo` entries in `docs/*/formats.md` and the testdata fixture moved to the v2 canonical form.

## Alternatives considered

- **A monotonic operation counter for `Version`**: a counter that only increases would make `Dirty` trivial (compare counters) but could never report clean after undoing back to the saved point, which is the marker's main use in an editor. Godot's `get_version` is a position coordinate for the same reason.
- **Restoring logs with forgotten moves onto the state at `New`**: the replayed state would silently miss the forgotten moves' effects, a wrong state presented as a successful load. Refusing forces the game onto the `Clear` + save-state path that already exists.
- **Keeping version 1 and adding `forgotten` as an optional key**: old builds would refuse new logs anyway (unknown fields are refused), so the version gives the refusal a clear name, and the conformance checks get a multi-version format to exercise.
- **`Do` blocking merges**: making a plain `Do` end the merge run would forbid folding an edit into a move the game made through `Do`, with no gain in predictability; the flag exists only for `EndMerge` (and `MarkSaved`, where folding would corrupt the saved point's meaning).

## Consequences

- **Benefits**: an editor drag is one undo step, history memory is bounded, and the dirty flag survives undo/redo/seek correctly, all in about a hundred lines; the log format stays deterministic and round-trips, with v1 saves still readable.
- **Costs**: `Dirty` compares positions, not states, so a game that mutates state outside the history can report clean wrongly — moves must carry every change, as the package already requires of `Revert`. A history with a `Limit` can no longer be saved mid-session as a `Log` once moves are forgotten; games that persist editor sessions must `Clear` and save the state first. Fuzz coverage of the new operations (`DoMerge`, `Limit`) is not in `FuzzHistory`, which predates them; the unit tests cover the acceptance cases.
