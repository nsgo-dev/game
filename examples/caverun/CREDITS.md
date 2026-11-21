# Cave Run — Asset Credits & Licenses

All art, tilemap, font, and audio assets in `examples/caverun/assets/` are **100% original works** created specifically for the `github.com/nsgo-dev/game` repository. They are deterministically generated from scratch using only the Go standard library by [`tools/genassets/main.go`](tools/genassets/main.go), with zero third-party artwork, fonts, or audio samples.

## Asset Inventory

| File | Kind | Author | License |
| --- | --- | --- | --- |
| `assets/sprites.png` | 128×64 RGBA pixel-art sprite sheet (hero, gems, slimes, portal, sparks, HUD icons) | NSGo Game Contributors (`tools/genassets`) | [CC0-1.0](#cc0-10-universal) |
| `assets/sprites.json` | Aseprite JSON-hash sprite atlas descriptor (`sprite.LoadAtlas` compatible) | NSGo Game Contributors (`tools/genassets`) | [CC0-1.0](#cc0-10-universal) |
| `assets/tileset.png` | 64×32 RGBA cave tileset (stone, moss, wooden bridge, cave walls, crystals, lava) | NSGo Game Contributors (`tools/genassets`) | [CC0-1.0](#cc0-10-universal) |
| `assets/level1.tmj` | 40×14 Tiled JSON level map (background, terrain, object layers) | NSGo Game Contributors (`tools/genassets`) | [CC0-1.0](#cc0-10-universal) |
| `assets/font.png` | 128×48 RGBA 8×8 pixel bitmap font sheet (ASCII 32–126) | NSGo Game Contributors (`tools/genassets`) | [CC0-1.0](#cc0-10-universal) |
| `assets/font.fnt` | AngelCode BMFont text descriptor for `font.png` | NSGo Game Contributors (`tools/genassets`) | [CC0-1.0](#cc0-10-universal) |
| `assets/sfx_jump.wav` | 16-bit mono 22050 Hz PCM WAV — jump sound effect | NSGo Game Contributors (`tools/genassets`) | [CC0-1.0](#cc0-10-universal) |
| `assets/sfx_gem.wav` | 16-bit mono 22050 Hz PCM WAV — gem collection chime | NSGo Game Contributors (`tools/genassets`) | [CC0-1.0](#cc0-10-universal) |
| `assets/sfx_hurt.wav` | 16-bit mono 22050 Hz PCM WAV — damage sound effect | NSGo Game Contributors (`tools/genassets`) | [CC0-1.0](#cc0-10-universal) |
| `assets/sfx_win.wav` | 16-bit mono 22050 Hz PCM WAV — level-clear victory fanfare | NSGo Game Contributors (`tools/genassets`) | [CC0-1.0](#cc0-10-universal) |
| `assets/music_cave.wav` | 16-bit mono 22050 Hz PCM WAV — seamless looping cavern theme | NSGo Game Contributors (`tools/genassets`) | [CC0-1.0](#cc0-10-universal) |

## Regenerating Assets

All files in `examples/caverun/assets/` can be reproduced identically from the repository root with:

```sh
go run ./examples/caverun/tools/genassets/main.go
```

## Licenses

### CC0-1.0 Universal

All generated files under `examples/caverun/assets/` are dedicated to the public domain under the **Creative Commons Zero v1.0 Universal** (`CC0-1.0`) dedication:

> To the extent possible under law, the authors have waived all copyright and related or neighboring rights to the Cave Run assets in `examples/caverun/assets/`. You can copy, modify, distribute, and perform the work, even for commercial purposes, all without asking permission.
>
> Full text: <https://creativecommons.org/publicdomain/zero/1.0/legalcode>

### Generator Source Code

The Go asset generator source code in `examples/caverun/tools/genassets/` is licensed under the repository's [Apache License, Version 2.0](../../LICENSE) (`Apache-2.0`).
