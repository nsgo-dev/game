### Added

- `flow`: map events in YAML (File Formats entry `flow#mapevent`): `flow.ParseMapEvent` and `flow.EncodeMapEvent` read and write a versioned list of pages with `when` conditions on self switches, switches and variables, a trigger (`action`, `touch`, `autorun`, `parallel`) and `say`, `wait`, `set` and `self` commands; `MapEventDoc.ActivePage` picks the last page whose conditions hold, and `flow.EventRunner` runs a page on fixed steps without blocking, with an `EventSnapshot` that saves and restores it mid-command (part of #1755).
