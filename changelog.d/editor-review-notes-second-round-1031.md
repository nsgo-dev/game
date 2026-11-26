### Added

- `game/editor`, `game/editor/editormcp`: add change-scoped review notes (`C` / `CmdReviewNote`, inline note composer, viewport note pins, Notes filter chip, paused Agent panel unsent-note counts, and Revert all dropped-note warning), multi-round review tabs (`Round 1` read-only / `Round 2` latest with round banner and `"Redone from your note: …"` Why), and MCP tools `review_notes_get` and `review_redo` (with privacy, permission, You-only, and noted-only isolation enforcement) so redoing noted changes stays within one turn and one undo step (#1031).
