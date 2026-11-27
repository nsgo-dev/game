### Added

- `game/undo`: `History` gains the three tools an in-game editor needs: `Limit` bounds how many moves are kept (the oldest applied moves are forgotten first), `DoMerge` and `EndMerge` fold the steps of one drag into a single move through the new `Merger` interface, and `MarkSaved`, `Dirty` and `Version` track the saved point for an "unsaved changes" mark (#1365).

### Changed

- `game/undo`: the saved-log format is now version 2 with a `forgotten` count of the moves a `Limit` dropped; version 1 logs are still read and save again in the current form, and `Restore` refuses a log with forgotten moves, whose effects the starting state lacks (#1365).
