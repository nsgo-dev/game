### Added

- `game/physics`: add `MoveAndSlideInto`, `World.MoveAndSlideInto`, `World.MoveAndSlideWithInto`, `CharacterBody.MoveInto`, and `CharacterBody.MoveWorldInto` to reuse contact and collision slices across fixed steps (#1660).

### Fixed

- `game/physics`, `game/platformer`: eliminate per-step contact, collision, and closure allocations in `MoveAndSlideInto`, `World.MoveAndSlideWith`, `CharacterBody.Move`, `platformer.Controller.Step`, Cave Run, and Crypt Quest (#1660).
