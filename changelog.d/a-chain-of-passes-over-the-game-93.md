### Added

- `game/effects`: a chain of Kage shader passes over the game's frame, fused into one program with no texture between passes, with per-effect uniforms animated on the frame clock (`Clock`), an `effects.View` over a pixel-buffer frame and CPU `Reference` pictures for headless golden tests (#93).
