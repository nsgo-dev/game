### Fixed

- `game/sound`, `game/input`: keep music tracks blocked by `audio.NotAllowed` current with `Music.Blocked()` and retry them on `Mixer.Unblock()` (including via `input.Actions.Mixer` and Lantern 3D's input/HUD handlers) so music started before the first user gesture plays after the first click on the web (#1481).
