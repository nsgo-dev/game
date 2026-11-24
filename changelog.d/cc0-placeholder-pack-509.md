### Added

- `game/placeholder`: a CC0 placeholder asset pack to prototype with, drawn, synthesized and modeled in Go for this repository: seven 16×16 sprite sheets with Aseprite exports and animations, a 32-tile tileset with a Tiled tileset (types, `solid`/`oneWay` and other properties, animations) and a sample level, the proportional Placeholder Pixel BMFont, eight sound effects and a seamless music loop, and three glTF models, each image and sound with its import settings sidecar. `Sheet`, `Font`, `Level`, `Model` and `Sound` load them, `FS` embeds them with a `CREDITS.md`, and the guide gives the `game.yaml` assets section that builds them with `game assets`. A test keeps the examples, the tutorial, the docs and the project template to this pack or assets listed with their license in a `CREDITS.md` (#509).

### Changed

- The sample glTF models `crate.glb` and `robot_arm.gltf` moved from `docs/assets/3d-sample` into the placeholder pack's `models` folder, now with a stated CC0 license (#509).
