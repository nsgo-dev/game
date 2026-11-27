# Agent Note: Player-Configurable Captions, Two-Line Wrapping, and Settings Preview

Status: implemented

## Problem

`a11y.CaptionsView` previously rendered captions with a fixed near-black 90% pill, fixed white/cyan/yellow colors, no speaker coloring or delivery qualifiers, no background-chatter caption kind, no speaker-name repetition suppression, and up to four multi-line captions stacked vertically (`nsgo-dev/game#1773`). `gameui.SettingsMenu` exposed only the master Subtitles toggle, SFX Captions toggle, and shared Text Scale slider, with no live preview over bright and dark scenes.

## Decision

Extend the unified `save.AccessibilitySettings` model ([Unify Accessibility Preferences Across save, a11y, and gameui](../bug-fix/2026-11-28-a11y-settings-assist-sync.md)) with a value-type `save.CaptionSettings` struct, update `a11y.CaptionTrack` / `a11y.CaptionsView` for XAG 104 presentation rules, and add a dedicated Captions column with a split light/dark swatch live preview to `gameui.SettingsMenu` ([Ready Settings Menu in game/gameui](2026-11-24-gameui-settings-menu.md)).

### Persisted model (`pkg/save`)

- `save.CaptionSettings` lives on `save.AccessibilitySettings.Caption` using only comparable value types (`string`, `float64`, `bool`) so `AccessibilitySettings` remains comparable (`==`) and compatible with `pkg/save`'s `Stable` contract.
- Preset normalizers (`NormalizeCaptionBackground`, `NormalizeCaptionTextColor`, `NormalizeCaptionSize`) accept canonical presets (`black`/`gray`/`blue`/`white`, `white`/`yellow`/`cyan`/`green`/`black`, `small`/`medium`/`large`/`xlarge`) plus `#rrggbb` hex colors.
- Explicit-override flags (`BackgroundOpacitySet`, `SpeakerColorsSet`, `DialogueSet`, `SoundsSet`) and `CaptionSettings.UnmarshalJSON` preserve intentional `0` opacity or `false` toggles across `Validate()` and JSON round-trips while keeping zero-value structs at readable defaults (`0.9` opacity, speaker colors on, dialogue on).
- `AccessibilitySettings.SetSoundCaptions` / `EffectiveSoundCaptions` bridge legacy `SFXCaptions` with `Caption.Sounds` without mutating one from the other inside `Validate()`, preserving exact struct equality in callers that mutate `SFXCaptions` directly.
- `AccessibilitySettings.SetCaptions` / `ClearCaptions` / `EffectiveCaptions(env)` mirror `EffectiveReduceMotion(env)` so `nsgo-dev/nsgo#2677`'s platform caption preference can be picked up automatically once NSGo exposes it on `core.Env`.

### Caption tracking, wrapping, and rendering (`pkg/a11y`)

- `CaptionChatter` is appended to `CaptionKind` (`"chatter"`) alongside `CaptionTrack.ShowChatter`. `Caption.Qualifier` formats as `"Speaker (qualifier): text"`, and `CaptionTrack.ShowDialogue` / `ShowDialogueWithQualifier` track a logical clock advanced by `Update(dt)` (even while `Active()` is empty) to set `OmitSpeaker: true` on consecutive lines from the same speaker/qualifier within `SpeakerRepeatGap` (default `5s`; `< 0` disables suppression) while retaining `Caption.Speaker`.
- `WrapCaptionLines(text, maxChars)` wraps at word boundaries for space-separated text and by character for CJK/fullwidth runes (default `DefaultCaptionMaxChars = 40`). `CaptionsView` limits total simultaneous visible lines across active captions to `CaptionStyle.MaxLines` (default `DefaultCaptionMaxLines = 2`), dropping older lines first.
- `CaptionsView` renders speaker prefixes via `core.Text.SpansFunc` tinted with `SpeakerColor` (derived from `PaletteFor(mode)` for CVD safety, or `CaptionStyle.SpeakerColors` overrides). `EnsureContrast` protects legibility whenever pill background opacity is `>= 0.5` or `HighContrast` is active; below `0.5` opacity it preserves the player's chosen text color and adds a subtle drop shadow.

### Settings menu Captions section and live preview (`pkg/gameui`)

- `SettingsMenu`'s Accessibility tab uses a two-column layout that fits inside `960×540` without overlap: the left column groups all 9 caption controls above a non-focusable `Panel` (`CaptionPreviewPanel`) drawing split light (`#E8EEF5`) and dark (`#141923`) swatches behind live sample captions computed via `CaptionPreviewState()`; the right column holds the 10 display and gameplay-assist controls.
- `pkg/gameui` (Experimental) imports `pkg/a11y` (Experimental) directly so the live preview shares `ResolveCaptionBackground`, `ResolveCaptionTextColor`, `SpeakerColor`, `FormatCaptionParts`, `WrapCaptionLines`, and `EnsureContrast` with `CaptionsView`.

## Alternatives considered

- **Single vertical column in `SettingsTabAccessibility`**: adding 7 new caption controls plus a preview box to the existing 12-row vertical stack would compress rows below 16 pt at `960×540` or require introducing a scroll container into `gameui`. A two-column layout keeps every control >= 22 pt and leaves ~130 pt for the live swatch preview while preserving linear `Up`/`Down` gamepad navigation across all 19 controls.
- **Splitting speaker name and body into sibling `core.Text` nodes**: breaks single-node text lookup (`app.Find("text:Speaker: Body")`) and screen-reader atomic announcements. Using `core.Text.SpansFunc` styles the speaker prefix independently inside one `core.Text` node.
- **Counting CJK runes as width 2 in `WrapCaptionLines`**: rejected because `MaxChars` is documented and tested as a character count; counting runes directly ensures `len([]rune(line)) <= MaxChars` for both Latin and CJK scripts.

## Consequences

- **Benefits**: Players can customize caption backdrop color, opacity (0–100%), text color, speaker colors, size preset, and per-kind toggles (dialogue, chatter, sounds) and preview readability over light and dark backgrounds before saving. Captions now conform to XAG 104's 40-char / two-line guidance and avoid noisy repeated speaker prefixes.
- **Costs**: `SettingsTabAccessibility` grows from 12 to 19 focusable controls, requiring gamepad-traversal tests (`pkg/gameui/settings_menu_test.go`, `examples/caverun/settings_test.go`) to step through the new rows, and `a11y.CaptionsView` now defaults to `MaxLines: 2` (callers wanting more simultaneous lines set `CaptionStyle.MaxLines`).
- **Deferred dependency**: Platform caption preference fallback in `EffectiveCaptions(env)` waits on `nsgo-dev/nsgo#2677` adding the signal to `core.Env`; first-launch caption prompt is tracked separately in `nsgo-dev/game#1783`.
