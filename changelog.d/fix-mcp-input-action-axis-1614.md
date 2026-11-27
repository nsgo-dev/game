### Fixed

- `pkg/mcp`: `game_input_action` now declares `"axis"` in its schema enum, error message and docs (defaulting to `"tap"` when `value` is omitted and `"axis"` when `value` is provided) and rejects `"axis"` without `value` with an error instead of panicking (#1614).
