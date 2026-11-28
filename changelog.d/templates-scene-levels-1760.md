### Added

- `template/platformer2d` and `template/rpg2d`: each template ships an entry scene (`scenes/level1.scene.yaml`, `scenes/town.scene.yaml`) that `game.yaml` autoloads as its main scene, so the editor opens the project on it; the scene marks where the level's start, lamp and beacon or the town's people, items and exits are, and a test keeps it in step with `data/level.yaml` and `data/world.yaml`, which the game still runs from (part of #1760).
