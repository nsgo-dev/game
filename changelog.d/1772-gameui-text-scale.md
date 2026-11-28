### Added

- `game/pkg/gameui`: `Theme.TextScale`, `Root.TextScale` and `Root.SetTextScale` multiply every widget font size, the theme's default font and per-widget or screen-file `fontSize` alike, clamped to `MinTextScale`–`MaxTextScale` (1–2); `SettingsMenu` now applies the full persisted `TextScale` range (0.5–3.0); `ScreenCheckOptions.TextScale` makes `CheckScreen` check text overflow at that largest scale as well as the theme's own (#1772).
