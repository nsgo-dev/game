# Accessibility Guide

This guide maps the [Game Accessibility Guidelines](https://gameaccessibilityguidelines.com/) (GAG) and [Xbox Accessibility Guidelines](https://learn.microsoft.com/en-us/gaming/accessibility/guidelines) (XAG) to NSGo Game packages, shows how to wire player preferences end to end, provides a pre-release checklist, and maps Apple's App Store Accessibility Nutrition Labels to what the engine lets you claim truthfully.

For the `pkg/a11y` API reference, see [a11y](a11y.md). Each item below states what works today, which row in [`gameui.SettingsMenu`](gameui.md#ready-settings-menu-settingsmenu) controls it, what remains the **game's responsibility**, and the open issue wherever the engine does not yet cover a requirement.

## Wiring settings end to end

All accessibility, audio, display, and control preferences live in one typed [`save.Settings`](save.md#player-settings-settingsstore--accessibilitysettings) store (`save.DefaultSettingsStore`), kept separate from save slots. Backing [`a11y.Assist`](a11y.md#persisted-assist-options-assist) with that same store via `a11y.NewAssistFromSettings` connects `gameui.SettingsMenu`, `a11y.CaptionsView`, `a11y.AccessibleMenu`, `sound.Mixer`, and `postfx.Flash` / `postfx.Shake`:

```go
store := save.NewStorageSettingsStore(cx.Storage(), "mygame")
assist := a11y.NewAssistFromSettings(cx, store)
mixer := sound.NewMixer(cx, "mygame")
captions := a11y.NewCaptionTrack(cx)

// Load and apply saved settings before the first sound or frame.
_, _ = store.LoadAndApply(save.SettingsTargets{
	Sound:              mixer,
	ApplyDisplay:       video.Apply,
	ApplyBindings:      func(b map[string][]string) error { gameui.ApplyBindingsToActions(actions, b, true); return nil },
	ApplyLocale:        setLocale,
	ApplyAccessibility: assist.ApplySettings,
})

// Wire SettingsMenu to the same store and Env so OS defaults apply when unset.
settingsMenu := gameui.NewSettingsMenu(gameui.SettingsMenuConfig{
	Store: store,
	Env:   cx.Env(),
	Video: video,
	Targets: save.SettingsTargets{
		Sound:              mixer,
		ApplyDisplay:       video.Apply,
		ApplyBindings:      func(b map[string][]string) error { gameui.ApplyBindingsToActions(actions, b, true); return nil },
		ApplyLocale:        setLocale,
		ApplyAccessibility: assist.ApplySettings,
	},
	OnBack: closeSettings,
})

// Full-screen flashes stop under ReduceMotion and turn into low-contrast tints under ReduceFlashes; shake scales with ScreenShake.
flash := postfx.NewFlash(cx, postfx.FlashConfig{
	Color:         core.RGB(0xffffff),
	ReduceMotion:  assist.ReduceMotion,
	ReduceFlashes: assist.ReduceFlashes,
})
shake := postfx.NewShake(cx, postfx.ShakeConfig{
	Seed:         42,
	ReduceMotion: assist.ReduceMotion,
})

// Inside game.Loop.Update:
sim.Step(assist.ScaleDT(dt))
captions.Update(dt)

// When triggering camera shake, multiply by assist.ScreenShake() (0..1):
if amp := 6.0 * assist.ScreenShake(); amp > 0 {
	shake.Trigger(amp, 400*time.Millisecond)
}

// Mount native CaptionsView above your Canvas in a core.Stack:
a11y.CaptionsView(cx, captions, a11y.CaptionStyle{Assist: assist})
```

### Platform preferences followed automatically

NSGo exposes the operating system's accessibility settings as reactive signals on `cx.Env()`:

| OS signal (`core.Env`) | Engine integration |
| --- | --- |
| `Env().ReduceMotion()` | Default for `save.AccessibilitySettings.EffectiveReduceMotion` (`assist.ReduceMotion()`), `SettingsMenu`, `postfx.Flash`, and `postfx.Shake` until the player explicitly overrides it. |
| `Env().IncreaseContrast()` | Default for `save.AccessibilitySettings.EffectiveHighContrast` (`assist.HighContrast()`), `assist.Palette()` (`VisionHighContrast`), `CaptionsView`, and `SettingsMenu`'s automatic `HighContrastTheme()`. |
| `Env().FontScale()` | Default for `save.AccessibilitySettings.EffectiveCaptionScale` (`assist.TextScale()`, clamped to `0.5`–`3.0`), read by `CaptionsView` and `AccessibleMenu`. |
| `Env().ScreenReader()` | Readable anywhere to adjust narration or route focus into native views (`a11y.AccessibleMenu`, `cx.Announce`). |
| `Env().Dark()` / `Env().Appearance()` | Readable for dark-mode UI adaptation. |
| `Env().RTL()` | Read by [`l10n.EnvDirection`](l10n.md#rtl-visual-ordering-and-layout-mirroring) for RTL text ordering and HUD mirroring. |
| `Env().SafeArea()` | Read by [`screen.Viewport.SafeGameRectForEnv`](screen.md#safe-area-for-mobile-huds), [`input.TouchControls.Resize`](input.md#on-screen-touch-controls), and [`gameui`](gameui.md#screen-files-screenyaml) screen files. |
| `Env().Pointer()` | `input.NewTouchControls` shows touch controls automatically when `core.PointerCoarse`. |

Calling `Assist.Reset()` (or clicking **Reset** in `SettingsMenu`) clears explicit overrides so `ReduceMotion`, `HighContrast`, and `TextScale` follow the operating system again.

## Motor

Covers GAG Motor (Basic & Intermediate) and XAG 106 (Touch), 107 (Input), 108 (Timing/Holds), and 117 (Settings).

| Guideline (GAG / XAG) | Level | Engine support & SettingsMenu row | Gaps & game's responsibility |
| --- | --- | --- | --- |
| **Remappable controls** (GAG / XAG 107) | Basic | [`input.Actions`](input.md#defining-actions-and-axes) (`Bind`, `Save`/`Load`), [`gameui.SettingsMenu`](gameui.md#ready-settings-menu-settingsmenu) **Controls** tab (`save.Settings.Bindings`, `ApplyBindingsToActions`). | Ensure every gameplay and menu action is bound through `input.Actions` rather than hard-coded keys or buttons. |
| **Multiple input methods & device switching** (GAG / XAG 107) | Basic | [`input.Actions`](input.md) unifies keyboard, mouse, gamepad, touch, and motion sensors under the same action names; `input.DeviceTracker` and `input.PromptTable` update prompts automatically; [`Players.SetDisconnectPolicy`](input.md#controller-disconnect-reconnect-and-fallback) pauses or falls back to keyboard/touch on controller disconnect. | Avoid requiring two devices at once (for example, mouse plus keyboard) when a single device can drive the same actions. |
| **Single input method for menus and play** (GAG / XAG 107) | Basic | [`gameui.SpatialNavigator`](gameui.md#spatial-action-focus-navigation-spatialnavigator) (`StepActions`, `focusOrder` in `*.screen.yaml`), `SettingsMenu` (pointer, touch, keyboard, and `StepGamepad`), [`a11y.AccessibleMenu`](a11y.md#screen-reader-menus-accessiblemenu) (keyboard, press, and screen-reader actions). | **Engine gap**: one-switch scanning for `gameui` screens and `SettingsMenu` is tracked in [#1778](https://github.com/nsgo-dev/game/issues/1778). |
| **Touch target size, spacing, left-handed layout & saved edits** (GAG / XAG 106, 107) | Basic / Interm. | [`input.TouchControls`](input.md#on-screen-touch-controls) enforces `input.MinTouchTarget` (`44 pt`), stays inside `SafeArea`, supports `SetMirrored(true)` for left-handed play, and persists player layout edits via `Save`/`Load`. [`gameui.CheckScreen`](gameui.md#validation-game-check-and-safe-refactoring-refs-game-mv) (`go tool game check`) flags touch targets under `44 pt` on phones and tablets. | Expose a layout editor or left-handed toggle in your game's UI if players rely on virtual sticks and buttons. |
| **Hold-to-toggle & avoiding rapid button mashing** (GAG / XAG 108) | Basic / Interm. | Touch buttons support `mode: toggle` and `mode: tap` (`TouchModeToggle`, `TouchModeTap`). `Assist.HoldToToggle()` and `SettingsMenu` **Accessibility → Hold to Toggle** persist the player's preference. | **Engine gap**: built-in action modifiers on `input.Actions` (hold-to-toggle, press-once instead of mashing, minimum press time, repeat debounce) are tracked in [#1777](https://github.com/nsgo-dev/game/issues/1777). **Game's responsibility**: read `assist.HoldToToggle()` on sustained actions (sprint, aim, crouch, block) and avoid mandatory rapid-press QTEs. |
| **Sensitivity, axis inversion, dead zones & haptics** (GAG / XAG 107, 108) | Basic / Interm. | Per-axis `GamepadAxisOptions` (`DeadZone`, `Axial`, `Sensitivity`, `Invert`), `PointerAxisOptions` (`Sensitivity`, `Invert`), `MotionAxisOptions` (`DeadZone`, `Smoothing`, `Calibrate`), and `input.Haptics.SetEnabled(false)` (persisted with `Haptics.Save`/`Load`). | **Engine gaps**: standard player settings for look sensitivity, invert axes, stick dead zone, FOV, camera bob, and haptics strength applied to input and 3D cameras are tracked in [#1780](https://github.com/nsgo-dev/game/issues/1780); native gamepad rumble hardware support is tracked in [nsgo#1049](https://github.com/nsgo-dev/nsgo/issues/1049). |
| **Adjustable game speed & input leniency** (GAG / XAG 108, 117) | Interm. | `Assist.GameSpeed()` / `Assist.ScaleDT(dt)` (`0.25`–`2.0`), `SettingsMenu` **Accessibility → Game Speed**; [`platformer.Config`](platformer.md#timing) (`coyote_time`, `jump_buffer`, `wall_coyote_time`); [`rhythm.CalibrationScreen`](rhythm.md#calibration) + `OffsetStore`. | **Game's responsibility**: let players slow down real-time play (`assist.ScaleDT(dt)`) without punishing score or locking progression, and provide forgiving timing windows or skippable QTEs. |
| **Aim assist** (GAG / XAG 108) | Interm. | `Assist.AutoAim()` and `SettingsMenu` **Accessibility → Auto-Aim** persist the toggle. | **Engine gap**: built-in 2D/3D aim assist (slowdown near targets, magnetism, snap) with player-set strength is tracked in [#1779](https://github.com/nsgo-dev/game/issues/1779). |

Minimal pattern for reading `assist.HoldToToggle()` until [#1777](https://github.com/nsgo-dev/game/issues/1777) lands:

```go
if assist.HoldToToggle() {
	if actions.JustPressed("crouch") {
		crouching = !crouching
	}
} else {
	crouching = actions.Pressed("crouch")
}
```

## Cognitive

Covers GAG Cognitive (Basic & Intermediate) and XAG 112 (Reading/Timing), 113–115 (Guidance/Difficulty), and 117 (Settings).

| Guideline (GAG / XAG) | Level | Engine support & SettingsMenu row | Gaps & game's responsibility |
| --- | --- | --- | --- |
| **Save and remember all settings** (GAG / XAG 117) | Basic | [`save.DefaultSettingsStore`](save.md#player-settings-settingsstore--accessibilitysettings) persists audio, video, bindings, language, and accessibility settings outside save slots with atomic backup recovery. | **Engine gap**: wiring a Settings screen with the Accessibility tab into every starter template on first launch before any sound is tracked in [#1783](https://github.com/nsgo-dev/game/issues/1783). |
| **Pause at any time & undo mistakes** (GAG / XAG 115) | Basic | [`game.Loop.SetPaused`](loop.md#pausing-and-syncing-subsystems) (`Loop.OnPause = mixer.SetPaused`), automatic pause on `core.LifecycleBackground`, and [`undo.History`](undo.md) (`Undo`/`Redo`/`Seek`, saved via `undo.EncodeLog`) for puzzle and turn-based games. | **Game's responsibility**: allow pausing during gameplay and cutscenes; avoid unpausable real-time prompts in single-player modes. |
| **Self-paced text, backlog & skippable cutscenes** (GAG / XAG 112) | Basic / Interm. | [`vn.Presenter`](vn.md#presenter) (`CharsPerSecond` with negative values for instant reveal, `Advance`, `SetAuto`, `SetSkip`, `Backlog` and `ReplayVoice`); [`cutscene.Cutscene`](cutscene.md#skipping) (`skippable: true`, `skipAfter`). | **Game's responsibility**: require manual confirmation to advance dialogue by default instead of auto-dismissing story text on a short timer. |
| **Adjustable speed & invincibility assists** (GAG / XAG 115) | Basic / Interm. | `Assist.GameSpeed()` / `ScaleDT` (`SettingsMenu` **Accessibility → Game Speed**) and `Assist.Invincibility()` (**Accessibility → Invincibility**). | **Game's responsibility**: provide selectable difficulty levels (such as Story / Relaxed / Standard), skip-encounter options, and generous checkpoints. |
| **Clear objectives, reminders & device-accurate prompts** (GAG / XAG 113, 114) | Basic / Interm. | [`quest.Log`](quest.md#running-quests) tracks active quests and objectives for a HUD/journal; `input.PromptTable` + `DeviceTracker` display prompts matching the player's current device; [`l10n.Catalog`](l10n.md) localizes all text. | **Game's responsibility**: surface the current objective on demand, allow replaying tutorials, keep UI copy concise and jargon-free, and allow disabling optional timers. |

## Vision

Covers GAG Vision (Basic & Intermediate) and XAG 101 (Text), 102 (Contrast), 103 (Color), 104 (Visual Comfort/Layout), and 105 (Screen Narration).

| Guideline (GAG / XAG) | Level | Engine support & SettingsMenu row | Gaps & game's responsibility |
| --- | --- | --- | --- |
| **Legible, scalable text (up to 200%)** (GAG / XAG 101) | Basic / Interm. | `cx.Env().FontScale()` and `Assist.TextScale()` (`0.5`–`3.0`, `SettingsMenu` **Accessibility → Text Scale**) scale `CaptionsView` and `AccessibleMenu` automatically. [`text.SDFFont`](text.md#signed-distance-field-fonts) and [`text.DrawPlatform`](text.md#platform-text-with-drawplatform) stay crisp at any size. `gameui.CheckScreen` (`go tool game check`) checks localized text overflow across Phone, Tablet, Desktop, and Ultrawide reference sizes. | **Engine gap**: automatic text and HUD scaling up to 200% for `gameui` widgets and `*.screen.yaml` screen files (verified by `CheckScreen`) is tracked in [#1772](https://github.com/nsgo-dev/game/issues/1772). **Game's responsibility**: pick clean sans-serif or readable fonts and avoid tiny pixel text for essential UI. |
| **Sufficient contrast & high-contrast mode** (GAG / XAG 102) | Basic | `cx.Env().IncreaseContrast()`, `Assist.HighContrast()` (`SettingsMenu` **Accessibility → High Contrast**), `a11y.PaletteFor(VisionHighContrast)` (`21:1` fg/bg), `gameui.HighContrastTheme()` (auto-applied by `SettingsMenu` when `Theme` is nil), `a11y.ContrastRatio`, `a11y.EnsureContrast`, and `gameui.CheckScreen` (`go tool game check`) enforcing ≥ `4.5:1` WCAG contrast on `*.screen.yaml` text nodes. | **Game's responsibility**: place translucent dark scrims behind HUD text drawn over busy gameplay backgrounds, and audit custom canvas text with `a11y.ContrastRatio(fg, bg) >= 4.5`. |
| **Color-blind-safe palettes & CVD testing** (GAG / XAG 103) | Basic | `a11y.PaletteFor(mode)` (Okabe-Ito / Wong palettes for `VisionNormal`, `VisionProtanopia`, `VisionDeuteranopia`, `VisionTritanopia`, `VisionHighContrast`), `Assist.ColorVision()` / `Assist.Palette()` (`SettingsMenu` **Accessibility → Color-Blind Mode**), `a11y.Simulate(c, mode)`, and [`postfx.PaletteSwap`](postfx.md#palette-swap). | **Engine gap**: full-frame color-blind post-processing filter and editor/test CVD preview are tracked in [#1776](https://github.com/nsgo-dev/game/issues/1776). **Game's responsibility**: **never convey information by color alone** — always pair color with distinct silhouettes, icons, patterns, outlines, or text labels. |
| **Screen reader & UI narration** (GAG / XAG 105) | Interm. | `cx.Env().ScreenReader()`, `cx.Announce(text)`, `cx.Speak(text, voice, done)` (`cx.Env().CanSpeak()`), native NSGo accessibility attributes (`Role`, `Label`, `Hint`, `AccessibilityDescription`, `AccessibilityValue`, `LiveRegion`), [`a11y.AccessibleMenu`](a11y.md#screen-reader-menus-accessiblemenu) (localized via `MenuText` / `l10n`), [`a11y.CaptionsView`](a11y.md#live-captions-captiontrack--captionsview), and `input.TouchControls.Draw` (`core.CanvasNode` labels and roles). | **Engine gap**: exposing canvas-drawn `gameui` widgets, `SettingsMenu`, and `*.screen.yaml` files to screen readers with focus and value announcements is tracked in [#1425](https://github.com/nsgo-dev/game/issues/1425). Until `#1425` lands, use `a11y.AccessibleMenu` or native NSGo views when full VoiceOver/TalkBack menu support is required. |
| **Safe areas, dark interface & RTL layout** (GAG / XAG 102, 104) | Interm. | `cx.Env().Dark()` / `Env().Appearance()`, [`screen.Viewport.SafeGameRectForEnv`](screen.md#safe-area-for-mobile-huds), `safeArea` in `*.screen.yaml`, [`l10n.MirrorX`](l10n.md#rtl-visual-ordering-and-layout-mirroring) / `LayoutAlignment`, and [`screen.Video`](screen.md#video-settings) windowed/full-screen modes. | **Game's responsibility**: ensure HUD elements never clip into notches, home indicators, or curved edges, and keep important gameplay elements separated from cluttered backgrounds. |
| **3D camera FOV & camera bob** (GAG / XAG 104) | Interm. | `scene3d.Camera` accepts configurable field-of-view angles in game code. | **Engine gap**: standard player settings for FOV and camera bob applied to 3D cameras are tracked in [#1780](https://github.com/nsgo-dev/game/issues/1780). |

Use `a11y.Simulate` and `a11y.ContrastRatio` in unit tests to guard custom gameplay palettes:

```go
for _, mode := range []a11y.ColorVision{a11y.VisionProtanopia, a11y.VisionDeuteranopia, a11y.VisionTritanopia} {
	pal := a11y.PaletteFor(mode)
	if ratio := a11y.ContrastRatio(pal.Foreground, pal.Background); ratio < 4.5 {
		t.Errorf("%s foreground contrast %.2f < 4.5", mode, ratio)
	}
	_ = a11y.Simulate(pal.Enemy, mode)
}
```

## Hearing

Covers GAG Hearing (Basic & Intermediate) and XAG 109 (Captions) and 110–111 (Audio/Alternatives).

| Guideline (GAG / XAG) | Level | Engine support & SettingsMenu row | Gaps & game's responsibility |
| --- | --- | --- | --- |
| **Subtitles & directional SFX captions** (GAG / XAG 109) | Basic / Interm. | [`a11y.CaptionTrack`](a11y.md#live-captions-captiontrack--captionsview) + `CaptionsView` (`ShowDialogue`, `ShowSFX`, `ShowSFXAt`, `ShowImportant`, `CaptionMusic`), `CaptionStyleFromLocalizer`, `Assist.CaptionsEnabled()` and `Assist.SFXCaptionsEnabled()`, `SettingsMenu` **Accessibility → Subtitles**, **SFX Captions**, and **Text Scale**. `Caption.Format()` adds `[<]` or `[>]` when `|Direction| >= 0.25`. | **Engine gaps**: player-set caption background/opacity, speaker colors, per-kind toggles, 2-line limit, and live preview in `SettingsMenu` are tracked in [#1773](https://github.com/nsgo-dev/game/issues/1773); automatic caption feeding from sounds, cutscenes, and dialogue plus an on-screen directional sound indicator is tracked in [#1774](https://github.com/nsgo-dev/game/issues/1774). |
| **Separate volume controls & mute** (GAG / XAG 110) | Basic | [`sound.Mixer`](sound.md#mixer-and-persistent-volumes) (`Master` and per-bus groups such as `"music"`, `"sfx"`, `"voice"`, `"ui"`), `save.AudioSettings` (`Master`, `Groups`, `Muted`), `SettingsMenu` **Audio** tab. | **Game's responsibility**: route music, sound effects, voice, and UI sounds through distinct mixer groups so players can turn down background music while keeping speech audible. |
| **Mono audio & stereo balance** (GAG / XAG 110) | Basic / Interm. | [`sound.Mixer`](sound.md#mono-audio-and-leftright-balance) (`SetMono`, `FollowSystemMono`, `SetBalance`), `save.AudioSettings` (`Mono`, `Balance`), `SettingsMenu` **Audio → Mono Audio** and **Balance**. `Listener2D.Pan` and `Listener3D.Pan` still report true spatial direction so directional captions remain accurate under mono. | **NSGo v0.3.4 limits**: stereo audio assets keep their channels until output down-mixing lands ([nsgo#2676](https://github.com/nsgo-dev/nsgo/issues/2676)); automatic OS mono-switch tracking awaits `Env.MonoAudio` ([nsgo#2677](https://github.com/nsgo-dev/nsgo/issues/2677)). |
| **Visual & haptic alternatives to sound cues** (GAG / XAG 109, 111) | Basic / Interm. | [`input.Haptics`](input.md#on-screen-touch-controls) (`Play`, `Rumble`, `PlayPattern`, and `OnHaptic` callback for visual indicators); `CaptionTrack.ShowImportant` and `ShowSFXAt`. | **Game's responsibility**: ensure every gameplay-critical audio cue (enemy attack telegraph, low-health beep, off-screen threat, rhythm beat) has a clear visual or haptic equivalent so the game is fully playable muted. |

## Speech

Covers GAG Speech (Basic & Intermediate) and XAG 118–119 (Communication).

| Guideline (GAG / XAG) | Level | Engine support & SettingsMenu row | Gaps & game's responsibility |
| --- | --- | --- | --- |
| **Never require voice input** (GAG / XAG 118) | Basic | No package in NSGo Game requires microphone or voice input. Native text-to-speech (`cx.Speak`, `cx.Env().CanSpeak()`) and speech recognition (`cx.StartSpeech`, `cx.Env().CanRecognizeSpeech()`) are available as optional hooks on `core.Scope`. | **Game's responsibility**: keep every mechanic and menu operable without speaking. |
| **Text alternatives for multiplayer communication** (GAG / XAG 119) | Basic / Interm. | Native NSGo text inputs and views (`core.TextInput`, `gameui.AnchorOverlay`) can render text chat and quick-phrase wheels over the game canvas. | **Game's responsibility**: if your game includes voice chat, provide text chat or pre-written ping/call-out phrases, individual player mute controls, and optional TTS/STT bridges. |

## Photosensitivity and Motion Comfort

Covers GAG General/Vision (Photosensitivity & Motion Sickness) and XAG 104 (Visual Comfort) and 120 (Photosensitivity).

| Guideline (GAG / XAG) | Level | Engine support & SettingsMenu row | Gaps & game's responsibility |
| --- | --- | --- | --- |
| **Reduced motion & screen shake control** (GAG / XAG 104) | Basic | `cx.Env().ReduceMotion()`, `Assist.ReduceMotion()` and `Assist.ScreenShake()` (`0.0`–`1.0`), `SettingsMenu` **Accessibility → Reduce Motion** and **Screen Shake**, [`postfx.Shake`](postfx.md#vignette-flash-and-shake) (`ShakeConfig.ReduceMotion`). | **Game's responsibility**: gate [`tilemap.Camera.Shake`](tilemap.md#following-clamping-and-shaking), parallax scrolling, idle UI bounce, and camera transitions on `assist.ReduceMotion()` and `assist.ScreenShake()`. |
| **Full-screen flashes & seizure safety** (GAG / XAG 120) | Basic | `Assist.ReduceFlashes()`, `SettingsMenu` **Accessibility → Reduce Flashes**, [`postfx.Flash`](postfx.md#photosensitivity) (`FlashConfig.ReduceFlashes`, `FlashConfig.Limiter`). A `postfx.Flash` runs at most 3 times per rolling second, a shared `postfx.FlashLimiter` budgets several flashes together, saturated-red flash colors are desaturated, and passing `assist.ReduceFlashes` to `FlashConfig.ReduceFlashes` turns flashes into low-contrast tints. | **Engine gap**: particles and sprite flashes do not read `ReduceFlashes`, and automated test checks for flash and high-contrast pattern failures on recorded frames are tracked in [#1775](https://github.com/nsgo-dev/game/issues/1775). **Game's responsibility**: share one `FlashLimiter` between the full-screen flashes of a screen, keep other flashing content below 3 per second, and avoid rapid saturated-red or full-screen luminance strobes outside `postfx.Flash`. |
| **Repeating patterns & screen distortion** (GAG / XAG 104, 120) | Interm. | [`postfx.CRT`](postfx.md#crt-and-scanlines) scanlines and curvature are opt-in shader passes that can be disabled when `ReduceMotion()` or `ReduceFlashes()` is active. | **Engine gap**: 3D FOV and camera-bob settings are tracked in [#1780](https://github.com/nsgo-dev/game/issues/1780). **Game's responsibility**: avoid high-contrast static or scrolling stripe/checkerboard patterns across large screen areas, and offer a toggle for post-processing distortion or bloom. |

Gate `tilemap.Camera.Shake` alongside `postfx.Shake`:

```go
if !assist.ReduceMotion() {
	if intensity := 6.0 * float32(assist.ScreenShake()); intensity > 0 {
		cam.Shake(intensity, 250*time.Millisecond)
	}
}
```

## Pre-release Accessibility Checklist

Run through every item before cutting a release build ([Building and Shipping](shipping.md)):

- [ ] **Settings saved and restored**: change every Audio, Video, Controls, Language, and Accessibility setting in `SettingsMenu`, restart the app, and confirm every value persists separately from save slots (`save.DefaultSettingsStore`); click **Reset** and confirm unset `ReduceMotion`, `HighContrast`, and `TextScale` follow `cx.Env()` again.
- [ ] **Accessibility & captions available before first sound**: confirm players can enable subtitles, SFX captions, mono audio, and reduced motion/flashes before opening cutscenes or title music play (template defaults tracked in [#1783](https://github.com/nsgo-dev/game/issues/1783)).
- [ ] **Text readable at 200%**: set OS text scale to 200% (`Env.FontScale() == 2.0`) and `Assist.TextScale()` to `2.0`; inspect `CaptionsView`, `AccessibleMenu`, and native HUD text for truncation or overlap (also inspect `gameui` screens manually until [#1772](https://github.com/nsgo-dev/game/issues/1772) lands).
- [ ] **Contrast and non-color audit**: run `go tool game check` to verify every `*.screen.yaml` meets `4.5:1` contrast, `44 pt` touch targets on mobile, and zero text overflow across all locales; test custom canvas colors with `a11y.ContrastRatio` and `a11y.Simulate`, and play in greyscale to confirm no mechanic relies on color alone.
- [ ] **Flash and motion check**: turn on **Reduce Motion** and **Reduce Flashes**, then trigger every hit flash, explosion, lightning strike, screen shake, boss transition, and cutscene; confirm zero unexpected flashes, zero screen shake, and no rapid > 3 Hz full-screen luminance or saturated-red flicker (automated frame check tracked in [#1775](https://github.com/nsgo-dev/game/issues/1775)).
- [ ] **One input method end to end**: play from title screen through Settings, gameplay, pause, and game over using each supported input method alone — keyboard-only, gamepad-only, touch-only (checking `44 pt` targets inside `SafeArea`), and mouse-only — and verify controller disconnect recovery (`Players.SetDisconnectPolicy`).
- [ ] **Screen reader pass on iOS and Android**: test with **VoiceOver** (iOS) and **TalkBack** (Android) across every native view, `AccessibleMenu`, `CaptionsView`, and `TouchControls` canvas node; verify focus order, spoken labels/values, and that canvas-only `gameui` screens do not block navigation (see [#1425](https://github.com/nsgo-dev/game/issues/1425)).

## Apple App Store Accessibility Nutrition Labels

Apple's App Store lets developers declare nine [Accessibility Nutrition Labels](https://developer.apple.com/help/app-store-connect/manage-app-accessibility/overview-of-accessibility-nutrition-labels) in App Store Connect. Only claim a label when your **entire** core experience meets Apple's criteria — not merely because an engine helper exists.

| App Store Label | Engine features that support it | Limits & open issues | What to test before claiming |
| --- | --- | --- | --- |
| **VoiceOver** | Native NSGo views (`Role`, `Label`, `Hint`, `AccessibilityValue`, `LiveRegion`), `a11y.AccessibleMenu`, `a11y.CaptionsView`, `input.TouchControls.Draw` (`core.CanvasNode`), `cx.Announce`, `cx.Env().ScreenReader()`. | Canvas-drawn `gameui` widgets, `SettingsMenu`, and `*.screen.yaml` are not yet exposed to VoiceOver ([#1425](https://github.com/nsgo-dev/game/issues/1425)); custom canvas/3D scenes require your own `CanvasNode`s or native UI alternatives. | Complete onboarding, menus, settings, and core gameplay on a real iOS device with VoiceOver enabled (including Screen Curtain); confirm every interactive control has an accurate label, role, value, and logical focus order. |
| **Voice Control** | Native NSGo views with distinct `Label`s, `a11y.AccessibleMenu`, and `input.TouchControls.Draw` (`CanvasNode` labels) expose named elements to iOS Voice Control. | Canvas-drawn `gameui` widgets and `*.screen.yaml` are not yet exposed ([#1425](https://github.com/nsgo-dev/game/issues/1425)); real-time continuous movement/aiming usually cannot be played by voice commands alone. | Enable iOS Voice Control with **Show Names** and **Show Numbers**; confirm every common task and menu action can be operated reliably by voice without getting stuck. |
| **Larger Text** | `cx.Env().FontScale()`, `Assist.TextScale()` (`0.5`–`3.0`), `CaptionsView`, `AccessibleMenu`, native `core.Text`, `text.SDFFont`, `text.DrawPlatform`. | `gameui` widgets, `SettingsMenu`, and `*.screen.yaml` do not yet automatically scale to 200% from `Env.FontScale()` ([#1772](https://github.com/nsgo-dev/game/issues/1772)). | Set iOS Dynamic Type to Accessibility Extra Large (≥ 200%) before launch; verify all readable text across menus, HUD, dialogue, and captions scales cleanly without clipping or overlapping. |
| **Dark Interface** | `cx.Env().Dark()` and `cx.Env().Appearance()`, dark-first `a11y.PaletteFor` presets, `gameui.DefaultTheme()` / `HighContrastTheme()`. | Game art and custom themes must respect dark appearance themselves. | Switch iOS between Light and Dark Mode; verify all screens, dialogs, settings, and overlays adapt to dark styling with comfortable legibility and no sudden bright white flashes. |
| **Differentiate Without Color Alone** | `a11y.PaletteFor(...)` (Okabe-Ito / Wong palettes), `a11y.Simulate(...)`, `SettingsMenu` Color-Blind Mode. | Full-frame post-process color-blind filter is tracked in [#1776](https://github.com/nsgo-dev/game/issues/1776); palettes alone do not satisfy this label if gameplay meaning depends solely on color. | Enable iOS **Color Filters → Grayscale** and play every core mechanic (enemies, pickups, hazards, puzzle pieces, status bars); confirm shape, icon, pattern, or text always communicates state without color. |
| **Sufficient Contrast** | `gameui.CheckScreen` / `go tool game check` (checks ≥ `4.5:1` on `*.screen.yaml`), `a11y.ContrastRatio`, `a11y.EnsureContrast`, `CaptionsView` (`7.0:1` enforced), `Assist.HighContrast()` following `cx.Env().IncreaseContrast()`, `gameui.HighContrastTheme()`. | Custom `Canvas` / 3D HUD text and icons drawn outside `*.screen.yaml` are not checked automatically by `game check`. | Run `go tool game check`, enable iOS **Increase Contrast**, and measure custom HUD/menu foregrounds against their backgrounds to verify ≥ `4.5:1` for regular text and ≥ `3:1` for large text and essential UI controls. |
| **Reduced Motion** | `cx.Env().ReduceMotion()`, `Assist.ReduceMotion()` (follows OS by default), `Assist.ScreenShake()`, `Assist.ReduceFlashes()`, `SettingsMenu` Accessibility tab, `postfx.ShakeConfig.ReduceMotion`, `postfx.FlashConfig.ReduceMotion`. | `postfx.Flash` requires wiring `FlashConfig.ReduceFlashes` explicitly; `tilemap.Camera.Shake`, tweens, parallax, and particles must check `assist.ReduceMotion()` in game code. | Enable iOS **Reduce Motion** before opening the game (without changing in-game settings); verify camera shake, full-screen flashes, animated transitions, parallax, and idle UI motion are reduced or turned off immediately. |
| **Captions** | `a11y.CaptionTrack`, `a11y.CaptionsView`, `Assist.CaptionsEnabled()`, `Assist.SFXCaptionsEnabled()`, `Caption.Format()` (speaker names and `[<]`/`[>]` directional cues), `SettingsMenu` Accessibility tab. | Games must push dialogue, SFX, and cutscene cues to `CaptionTrack` themselves ([#1774](https://github.com/nsgo-dev/game/issues/1774)); player-customizable caption backdrop, opacity, and speaker colors are tracked in [#1773](https://github.com/nsgo-dev/game/issues/1773). | Play the full game muted on iOS (intro, cutscenes, dialogue, and gameplay); verify every spoken line and gameplay-relevant sound is accurately captioned in sync with audio. |
| **Audio Descriptions** | No built-in audio-description track player in `pkg/cutscene` or `pkg/sound`; games can layer a synchronized narration bus through `sound.Mixer` or speak cues with `cx.Speak` (`Env.CanSpeak()`). | Fully the game's responsibility for visual-only cutscenes and media. | Claim only if every visual-only video or narrative sequence includes selectable, synchronized audio descriptions explaining essential visual action without talking over dialogue. |
