### Added

- `game/editor`: History panel (`⌘Y` / toolbar icon button with tooltip `"History · ⌘Y"`) combining manual edits, agent turns, external reloads, and saved points on one newest-first day-grouped timeline with `All` / `You` / `Agent` filters, read-only point-in-time viewport preview (`"Previewing … · editing is off"`, exited with `Esc` or `Now`), and non-destructive multi-file `Restore` recorded as one new undoable step (#1017).
- `game/editor/editormcp`: read-only `history_list` MCP tool exposing the unified History timeline (with `author` and `limit` filters) to connected agents, and automatic agent-turn attribution for MCP edits (#1017).
