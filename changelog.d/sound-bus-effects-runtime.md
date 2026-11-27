### Fixed

- `game/sound`: bus effect chains configured in `game.yaml` now apply at run time: `Music.Play` and the new `Mixer.LoadEffect` process in-memory 16-bit PCM WAV sources through the group's chain, and sources of other formats play without it, with a diagnostic logged once per group (#1397).
