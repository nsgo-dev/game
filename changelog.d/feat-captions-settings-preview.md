### Added

- `game/save`, `game/a11y`, `game/gameui`: add player-configurable caption styling (`CaptionSettings`: background color, opacity 0–100%, text color, speaker colors, size preset, per-kind dialogue/chatter/sound toggles), `CaptionChatter` kind, `Caption.Qualifier`, `CaptionTrack.ShowDialogue` speaker-repeat suppression with configurable gap, 40-char / two-line caption wrapping in `CaptionsView`, and a live light/dark swatch caption preview in `SettingsMenu` (#1773).

### Changed

- `a11y.CaptionsView` shows at most two wrapped lines of 40 characters at once by default, older lines leaving first (set `CaptionStyle.MaxLines` and `CaptionStyle.MaxChars` for other limits), and music cues now follow the sound captions toggle (`Assist.SFXCaptionsEnabled`) instead of the speech captions toggle. `gameui.SettingsMenu`'s "SFX Captions" row is now "Sound Captions" (#1773).
