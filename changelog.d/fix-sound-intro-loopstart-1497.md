### Added

- `pkg/sound`: added `Music.PlayLooped(src, loopStart, fade)` using NSGo's native `audio.PlayerOptions.LoopStart` for single-file intro-and-loop music without a two-player handoff seam, and documented intro-and-loop playback in `docs/en/sound.md` and `docs/zh/sound.md` (#1497).

### Fixed

- `pkg/sound`: two-file `Music.PlaySections` and `Music.PlayWithIntro` now schedule the preloaded loop player slightly ahead of the intro's known duration from `OnProgress` (with `OnEnd` as fallback) instead of waiting until after `OnEnd` fires a turn late (#1497).
