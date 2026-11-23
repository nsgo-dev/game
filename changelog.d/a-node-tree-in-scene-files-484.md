### Added

- `game/sceneformat`: objects hold `children` with local transforms, built into composed 2D world transforms by `Build`, into the entity hierarchy by `SpawnEntities` and into a 3D node tree by `BuildNodes3D`; `Validate` refuses a child sprite whose composed transform is sheared, and `CheckObjectTree` checks one tree's depth and shear (#484).
