### Added

- `game/tooling/importsettings`: import settings sidecars, versioned YAML files such as `hero.png.import.yaml` beside an image or a sound, with a published JSON Schema (`import.v1.json`). `game assets` reads them: an image's sidecar sets its texture's compression format, mipmaps, sRGB and sampling filter (stored in the KTX2 key/value data as `game.filter`) or slices a sprite sheet in an atlas's folder into frames on a grid, and a sound's sidecar sets its output format, rate, channels, loudness, bitrate and loop points. A sidecar's settings override its target's in `game.yaml`, and editing one rebuilds only the targets that read its asset. An image read by both an atlas and a texture target may carry both kinds of settings, and a sidecar whose asset no target reads, such as a mistyped or differently cased name, is refused. Fonts and models have no section yet, since `game assets` has no target for them, and nothing at run time reads `game.filter` yet (#487).
- `game/tooling/audioconv`: `Buffer.Loop` holds a loop region, which `EncodeWAV` writes in a `smpl` chunk, `DecodeWAV` reads back and `Resample` scales to the new rate (#487).

### Changed

- `game/tooling/manifest`: `Scan` skips import settings sidecars (`*.import.yaml`), so they never appear in a generated manifest (#487).
- `game assets` and `game audio` keep the loop of a source WAV's `smpl` chunk in a WAV output, which they used to drop (#487).
