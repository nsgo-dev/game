### Added

- `game/mcp`: add `game_loop_status`, `game_loop_pause`, `game_loop_resume`, `game_loop_step`, `game_loop_time_scale`, and `game_loop_seed` MCP tools to inspect and drive `game.Loop` and deterministic randomness (#462).
- `game`: add `Loop.Steps`, `Loop.Elapsed`, `Loop.ElapsedSeconds`, `Loop.StepRate`, `Loop.StepDuration`, `Loop.EffectiveTimeScale`, and synchronous `Loop.StepNow` helpers (#462).
- `game/random`: add `Stream.SetSeed` and `Streams.SetSeed` to re-seed deterministic streams in place (#462).
