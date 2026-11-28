
## Gameplay visual log

`debug.VLog` records bounded per-step entries for an entity and category, including messages and numeric debug shapes. `WriteJSONL` and `ReadJSONL` provide deterministic JSON Lines interchange, while `Entries` filters by entity and inclusive step range for timeline scrubbing. Keep the recorder opt-in and omit it from release builds until a platform debugger integrates it.
