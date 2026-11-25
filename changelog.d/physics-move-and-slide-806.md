### Added

- `game/physics`, `game/platformer`: added `World.MoveAndSlide` and `World.MoveAndSlideWith` for multi-iteration shape-sweep sliding against static/kinematic bodies, solid and one-way tiles, and static `RigidBody` colliders with configurable up direction, floor angle and `MotionFloating` top-down mode, added `Body.OneWay` (with `WorldSnapshot`/`Checksum` support), and wired `CharacterBody.Move`/`MoveWorld` and `platformer.Controller.StepWorld` to use it alongside slopes and moving platforms (#806).
