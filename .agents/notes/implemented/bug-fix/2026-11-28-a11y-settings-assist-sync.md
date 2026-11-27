# Agent Note: Unify Accessibility Preferences Across save, a11y, and gameui

Status: implemented

## Problem

Accessibility and gameplay-assist preferences previously lived in two disconnected stores (`nsgo-dev/game#1771`):

- `gameui.SettingsMenu`'s Accessibility tab wrote `save.Settings.Accessibility` (`Captions`, `CaptionScale` 0.5–3.0, `ColorBlindMode` starting at `"none"`, `ReduceMotion`, `HighContrast`, `ScreenShake`, `GameSpeed` 0.25–2.0) plus `Custom["reduce_flashes"]`, with `DefaultSettings()` hardcoding `ReduceMotion: false` and `HighContrast: false`.
- `a11y.Assist` read and wrote ten raw `core.Storage` keys (`a11y.assist.*`) with conflicting ranges (`TextScale` 1.0–2.0, `GameSpeed` 0.25–1.0), `"normal"` instead of `"none"` for `VisionNormal`, four extra flags (`SFXCaptionsEnabled`, `Invincibility`, `AutoAim`, `HoldToToggle`) missing from `SettingsMenu`, and OS environment fallbacks (`Env.ReduceMotion()`, `Env.IncreaseContrast()`, `Env.FontScale()`).

Because `CaptionsView`, `AccessibleMenu`, `effects.Flash`, and `effects.Shake` read `a11y.Assist`, turning on Reduced Motion or Subtitles in `SettingsMenu` and saving had no effect on captions or flashes.

## Decision

Make `save.AccessibilitySettings` the single persisted model and back `a11y.Assist` with `*save.DefaultSettingsStore` (updating [Ready Settings Menu in game/gameui](../feature/2026-11-24-gameui-settings-menu.md)).

### Schema, ranges, and follow-platform contract (`pkg/save`)

- `save.AccessibilitySettings` (`pkg/save/settings.go`) adds typed fields `SFXCaptions`, `ReduceFlashes`, `Invincibility`, `AutoAim`, and `HoldToToggle` (all `bool` with `omitempty`), plus explicit-override markers `CaptionScaleSet`, `ReduceMotionSet`, and `HighContrastSet` (`bool` with `omitempty`). Keeping all fields value types preserves `pkg/save`'s `Stable` API compatibility, struct comparability (`==`), and unchanged default JSON size.
- Shared range constants `save.MinCaptionScale` (`0.5`), `save.MaxCaptionScale` (`3.0`), `save.MinGameSpeed` (`0.25`), and `save.MaxGameSpeed` (`2.0`) are the single source of truth used by `pkg/save`, `pkg/a11y`, and `pkg/gameui`.
- `save.NormalizeColorBlindMode` canonicalizes `""`, `"none"`, and `"normal"` to `"none"`, and `"highcontrast"` / `"high_contrast"` to `"high-contrast"`. `a11y.ParseColorVision` accepts both `"normal"` and `"none"` as `VisionNormal`.
- `EffectiveReduceMotion(env)`, `EffectiveHighContrast(env)`, and `EffectiveCaptionScale(env)` fall back to `env.ReduceMotion()`, `env.IncreaseContrast()`, and `env.FontScale()` when the corresponding `*Set` flag is false and the value is at its default (`false` or `0`/`1.0`), while respecting direct struct field assignments (`ReduceMotion: true`, `CaptionScale: 1.5`) and explicit `Set*`/`Clear*` helpers (including overriding OS `true` with `false` or OS font scale with `1.0`).
- `Settings.Validate()` migrates legacy `Custom["reduce_flashes"]` into `Accessibility.ReduceFlashes` and deletes the legacy `Custom` key (avoiding `Settings.UnmarshalJSON`, which would break embedded `save.Settings` structs in generic `SettingsStore[T]` callers).
- `SettingsStore[T].Subscribe(fn func()) func()` notifies registered listeners in registration order after `Save`, `Reset`, or `Delete` succeeds, enabling reactive consumers on both `FileBackend` and `StorageBackend`.

### Reactive `Assist` bridge and legacy migration (`pkg/a11y`)

- `a11y.NewAssistFromSettings(cx, store)` backs `*Assist` with the game's shared `*save.DefaultSettingsStore`, subscribes to `store.Subscribe` (cleaned up on `cx.Cleanup`), and tracks a `core.Signal[uint64]` so `CaptionsView`, `AccessibleMenu`, and `effects.Flash` / `effects.Shake` observe saves from `SettingsMenu` automatically on both file and storage backends.
- `Assist.ApplySettings(save.AccessibilitySettings) error` matches `save.SettingsTargets.ApplyAccessibility` for live preview on `SettingsMenu.Apply()` before saving, and `Assist.Reset()` resets only `Settings.Accessibility` so Audio, Display, Bindings, Locale, and Custom sections in a shared store remain intact.
- `a11y.NewAssist(cx, key)` is deprecated in favor of `NewAssistFromSettings` and now wraps `save.NewStorageSettingsStore(cx.Storage(), key)`. Both constructors run `MigrateLegacyAssist` once to move legacy `"<key>.*"` raw `core.Storage` keys into `store` and remove them only after `store.Save` succeeds.

### Accessibility tab expansion (`pkg/gameui`)

- `SettingsMenu`'s Accessibility tab adds rows for `SFXCaptions`, `Invincibility`, `AutoAim`, and `HoldToToggle` (12 rows at 26 pt height with 4 pt spacing, fitting within the 960×540 test viewport without overlap).
- `SettingsMenu` binds directly to `save.AccessibilitySettings.ReduceFlashes` (deprecating `SettingsCustomReduceFlashes`, `SettingsReduceFlashesFrom`, and `SetSettingsReduceFlashes`), displays effective platform values via `Effective*` when `Env` is supplied, and only marks `*Set` when the player edits those controls so unrelated saves preserve follow-platform behavior.

## Alternatives considered

### Changing `ReduceMotion`, `HighContrast`, and `CaptionScale` to pointer types (`*bool`, `*float64`)

Pointer fields represent tri-state naturally in JSON (`null` = follow system). Rejected because `pkg/save` is a `Stable` package: changing field types breaks existing Go callers, breaks struct equality (`==`) used in tests, and requires nil checks for every caller reading `AccessibilitySettings`. Companion `*Set` booleans with `Effective*` methods preserve full Go and JSON compatibility.

### Implementing `Settings.UnmarshalJSON` to migrate `Custom["reduce_flashes"]`

A custom `UnmarshalJSON` on `save.Settings` could inspect raw JSON keys directly. Rejected because Go promotes `UnmarshalJSON` onto structs that embed `save.Settings` (such as `customGameSettings` in `SettingsStore[T]`), which silently drops the outer struct's fields during `json.Unmarshal`. Migrating in `Settings.Validate()` runs on every `Load`, `Save`, and `Apply` without interfering with struct embedding.

### Keeping `a11y.Assist` on `core.Storage` and mirroring writes from `SettingsMenu`

Writing to both `save.DefaultSettingsStore` and `a11y.assist.*` keys would avoid importing `pkg/save` from `pkg/a11y`. Rejected because it preserves two conflicting schemas, breaks `FileBackend` games unless every call site manually syncs both stores, and risks drift whenever a new assist option is added. `pkg/a11y` (`Experimental`) importing `pkg/save` (`Stable`) obeys the stability layering boundary.

## Consequences

- **Benefits**: Saving preferences in `SettingsMenu` immediately updates `a11y.Assist`, `CaptionsView`, `AccessibleMenu`, and `effects.Flash` / `effects.Shake` on both file and storage backends; old `settings.json` files and legacy `a11y.assist.*` keys upgrade automatically; unset motion, contrast, and text-scale preferences follow OS accessibility settings.
- **Costs**: `pkg/a11y` now depends on `pkg/save` (`Stable`), and `SettingsMenu`'s Accessibility tab grows from 8 to 12 controls (slightly tighter row height at 26 pt).
- **Trade-off**: Callers that directly write `AccessibilitySettings{ReduceMotion: false}` without `SetReduceMotion(false)` leave `ReduceMotionSet == false`, so `EffectiveReduceMotion(env)` treats it as follow-platform; explicit `false` overrides use `SetReduceMotion(false)` (or `Assist.SetReduceMotion(false)`).
