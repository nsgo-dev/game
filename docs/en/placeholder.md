# Placeholder Assets

`game/placeholder` is a small asset pack to prototype with: sprite sheets, a tileset with a Tiled tileset and a sample level, a pixel font, sound effects and a music loop, and three glTF models, each image and sound with its [import settings sidecar](cli.md#assets). Every file is an original work drawn, synthesized or modeled in Go for this repository, and all of them are dedicated to the public domain under [CC0 1.0](https://creativecommons.org/publicdomain/zero/1.0/): a game may use, change and ship them, commercially or not, without asking and without giving credit.

```go
import "github.com/nsgo-dev/game/pkg/placeholder"
```

The examples and the tutorial use only this pack or assets of their own whose license is written down; see [Credits in this repository](#credits-in-this-repository).

## What it holds

The files live in `pkg/placeholder/pack`, which the package embeds and `placeholder.FS()` returns:

| Folder | Files | What they are |
| --- | --- | --- |
| `sprites/` | `player`, `slime`, `bat`, `coin`, `items`, `effects`, `ui`, each `.png`, `.json` and `.png.import.yaml` | Rows of 16×16 pixel-art frames, an Aseprite JSON array export of each with its animations as frame tags, and a sidecar that slices the sheet on the same grid |
| `tiles/` | `tileset.png`, `tileset.tsj`, `level.tmj`, `tileset.png.import.yaml` | 32 tiles of 16×16, their Tiled tileset with each tile's type, properties and animation, and a 40×15 sample level |
| `fonts/` | `pixel.png`, `pixel.fnt`, `pixel.png.import.yaml` | Placeholder Pixel, a proportional font of printable ASCII, as an AngelCode BMFont |
| `sounds/` | `jump`, `coin`, `hit`, `explosion`, `shoot`, `powerup`, `click`, `select`, `music_loop`, each `.wav` and `.wav.import.yaml` | 22050 Hz mono WAV effects and a four-bar music loop |
| `models/` | `crate.glb`, `robot_arm.gltf`, `gem.glb` | glTF 2.0 models, +Y up, -Z forward, standing on the origin |
| | `CREDITS.md` | Every file with its kind and license |

The sprite sheets and their animations:

| Sheet | Frames | Animations (frame tags) |
| --- | --- | --- |
| `player` | 8 | `idle` (0–1), `run` (2–5), `jump` (6), `hurt` (7) |
| `slime` | 4 | `idle` (0–3) |
| `bat` | 4 | `fly` (0–3) |
| `coin` | 4 | `spin` (0–3) |
| `items` | 8 | `gem`, `heart`, `key`, `star`, `potion`, `bomb`, `shield`, `flag`, one frame each |
| `effects` | 8 | `explosion` (0–4, plays once), `sparkle` (5–6), `bullet` (7) |
| `ui` | 4 | `panel`, `button`, `button_pressed`, `cursor`, one frame each; the panel and both buttons are also nine-slices with 4-pixel borders |

Frame `i` of a sheet is named `<sheet>_<i>`, such as `player_2`. Every frame's pivot is its middle.

The tileset's tiles, by id (Tiled's tile id, one less than the map's global id):

| Ids | Tiles | Properties |
| --- | --- | --- |
| 0–4 | grass, dirt, stone, brick, wood | `solid` |
| 5 | platform | `oneWay` |
| 6 | ladder | `ladder` |
| 7 | spikes | `hazard` |
| 8–11 | water, animated through 8–11 | `water` |
| 12–15 | lava, animated through 12–15 | `hazard` |
| 16, 17 | ice, crate | `solid` |
| 18–23 | bush, flower, sign, door top, door bottom, wall | |
| 24–27 | metal, bonus block, sand, snow | `solid` |
| 28, 29 | torch, animated through 28–29 | |
| 30, 31 | chain, window | |

Package `tilemap` turns the `solid` and `oneWay` properties into the tile flags `TileSolid` and `TileOneWay`; a game reads the others with `Tilemap.TileProperties`. The level has a `decor` and a `ground` tile layer and an `objects` layer: points of type `spawn` (named `player`), `coin`, `gem`, `slime` and `bat`, and an `exit` rectangle over the door.

## Loading it in code

Each function reads the embedded files and returns what the matching package loads:

```go
player, err := placeholder.Sheet("player")   // *sprite.Atlas with Animations["run"] and the rest
font, err := placeholder.Font()              // *text.Font
level, err := placeholder.Level()            // *tilemap.Tilemap with its tileset loaded
crate, err := placeholder.Model("crate.glb") // *gltf.Document
jump, err := placeholder.Sound("jump")       // core.AudioSource for audio.LoadSound
```

- `Sheet`, `Font` and `Level` give their images as `core.ImageSource` values holding the PNG's bytes and its size in points, one point a pixel. `Image` does the same for any PNG in the pack.
- `FS()` returns the pack as a read-only `fs.FS`, for anything else, such as `fs.ReadFile(placeholder.FS(), "CREDITS.md")`.
- `License` is `"CC0-1.0"`, the SPDX identifier of every file.

The pack is embedded in a game's binary only when the game imports the package, and adds about 430 KB, two thirds of it the music.

## Copying it into a project

To change the art, or to build it into a game's own atlases, copy `pkg/placeholder/pack` into the project, for instance as `art/placeholder`, and build it with [`game assets`](cli.md#assets) like any other art. This assets section of `game.yaml` uses every sidecar as it is meant to be read:

```yaml
assets:
  atlases:
    - name: sprites
      in: art/placeholder/sprites
      out: assets
  textures:
    - in: art/placeholder/tiles/tileset.png
      out: assets/tileset.ktx2
    - in: art/placeholder/fonts/pixel.png
      out: assets/pixel.ktx2
  audio:
    - in: art/placeholder/sounds/jump.wav
      out: assets/jump.wav
    - in: art/placeholder/sounds/music_loop.wav
      out: assets/music_loop.wav
    # and the same for each other sound you use
```

- Each sprite sheet's sidecar slices it into 16×16 frames, which the atlas names `<sheet>_<i>.png`. The atlas holds frames only; the sheets' animations stay in their JSON files.
- The sidecars of the tileset and the font page set the `nearest` filter, which keeps pixel art sharp when it is scaled.
- Each effect's sidecar normalizes its peak to -3 dBFS; the music's normalizes it to -6 dBFS, keeps it WAV and loops all of it, which game assets writes into the output's `smpl` chunk.
- `game assets` bakes fonts from TrueType and OpenType files, and the pack's font is already a bitmap font, so the font's `pixel.fnt`, like the Tiled `tileset.tsj` and `level.tmj` and the sheets' JSON files, is not a build target. Copy them into the game's bundled assets yourself, beside the PNG pages they name (`pixel.png`, `tileset.png`), and load them with `text.LoadBMFontImage` and `tilemap.LoadTiled`, whose `TilesetLoader.LoadImage` maps the tileset's image path to an image source, as `Font` and `Level` do. The KTX2 textures carry the `nearest` filter, which [`render3d.LoadKTX2`](render3d.md) applies to a texture it loads for 3D; `text` and `tilemap` draw from PNG pages, so a 2D game draws the font and the level from them.
- A sidecar beside an image or sound that no target reads is refused, so a project that uses only some of the pack copies only those files and their sidecars.

## Regenerating it

The generator in `pkg/placeholder/internal/gen` (with the models from `pkg/placeholder/internal/samplegltf`) draws every file but `CREDITS.md`, and writes the same bytes on every run and architecture. A test compares the committed pack with it; after changing the generator, rewrite the pack from the repository root with:

```sh
go tool nsgo test ./placeholder -run TestPackMatchesGenerator -update
```

and list any new file in `pack/CREDITS.md`, which a test also checks.

## Credits in this repository

The examples, the tutorial, the docs and the project template use only this pack or assets of their own with a written license. A test in `game/placeholder` enforces it: every image, sound, font, model, map or tileset exported by another tool, and atlas exported as JSON beside its image, under `examples/`, `docs/` and `template/` must be listed, with its license, in a `CREDITS.md` in its folder or a folder above it, in a Markdown table with a `License` column. The license is an SPDX identifier such as `CC0-1.0` or `Apache-2.0`, written as it is or as the text of a link, and every file a `CREDITS.md` lists must exist:

```markdown
| File | Kind | Author | License |
| --- | --- | --- | --- |
| `assets/hero.png` | 64×16 sprite sheet | Your name | CC0-1.0 |
```

An example that needs art it cannot draw in code uses the pack instead, or adds its files with such a table, as Cave Run's [`CREDITS.md`](../../examples/caverun/CREDITS.md) does.
