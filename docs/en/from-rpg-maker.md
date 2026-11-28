# Coming from RPG Maker

NSGo supports the data-first RPG workflow while leaving the runtime in Go.

## Maps and events

Use `tilemap` and scene files for maps, layers and object placement. Use `pkg/flow` event documents for pages, switches, variables, triggers and resumable commands. `pkg/dialogue`, `pkg/quest` and `pkg/inventory` provide focused runtime pieces that can share the same saved state.

## RPG data

Use the versioned YAML database in `pkg/rpg` for actors, classes, skills, enemies and troops. Keep formulas and battle rules deterministic, and use `pkg/save` to persist slots and settings with migrations.

## UI, localization and tools

Use `gameui` for menus, `l10n` for string tables and `text` for fonts and text layout. The editor opens maps and scenes directly, while `go tool nsgo check` validates formats and references.

