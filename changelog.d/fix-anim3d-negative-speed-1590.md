### Fixed

- `pkg/anim3d`: entering a `StateMachine` state or calling `Player.Play` / `Player.Crossfade` with a negative speed on a non-looping clip now starts at `Duration` and plays backward to `0` (firing events in reverse order), and flipping speed sign at a clamped end jumps to the opposite end (#1590).
