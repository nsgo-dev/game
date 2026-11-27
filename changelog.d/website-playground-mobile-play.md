### Added

- `website`: add mobile play mode to the playground on touch screens with native fullscreen and a `100dvh` overlay fallback, page scroll locking, per-example orientation metadata with a rotate-your-phone prompt, and touch hint copy across all locales (#1298).

### Fixed

- `game/input`: `TouchControls.Attach` claims touches on its controls through a `core.Responder` without `LetNative` so drags on virtual controls block native scrolling (#1298, upstream `nsgo-dev/nsgo#2660`).
