# gameui

`game/gameui` builds stylized in-canvas menus and HUD widgets drawn directly into `core.Drawing`, with box layout, virtual-resolution scaling, 2D spatial action focus navigation, and world-anchored native NSGo overlays.

```go
import "github.com/nsgo-dev/game/pkg/gameui"
```

## Layout containers, widgets, and `Root`

Compose a widget tree from layout containers (`Panel`, `VBox`, `HBox`) and widgets (`Label`, `Button`, `Toggle`, `Slider`, `List`, `ProgressBar`, `ImageProgress`, `Spacer`), then wrap it in a `Root`:

```go
theme := gameui.DefaultTheme()

resumeBtn := &gameui.Button{Text: "Resume", OnClick: resumeGame}
volumeSlider := &gameui.Slider{Label: "Volume", Min: 0, Max: 1, Value: 0.8, Step: 0.1, OnChange: setVolume}
muteToggle := &gameui.Toggle{Label: "Mute", Style: gameui.ToggleSwitch, OnChange: setMuted}

menu := &gameui.Panel{
	Width:   320,
	Padding: core.EdgesAll(16),
	Child: &gameui.VBox{
		Spacing: 10,
		Align:   gameui.AlignStretch,
		Children: []gameui.Widget{
			&gameui.Label{Text: "Paused", TextAlign: core.TextAlignCenter},
			resumeBtn,
			volumeSlider,
			muteToggle,
		},
	},
}

uiRoot := gameui.NewRoot(menu)
uiRoot.Theme = &theme
```

Inside your `Canvas.Draw`, call `uiRoot.Draw(d, bounds)` (which runs `Layout` and draws inside `Save`/`Restore` so caller canvas state stays untouched). Route pointer and keyboard events with `OnPointerMove`, `OnPointerDown`, `OnPointerUp`, `OnPress`, and `OnKeyDown`.

Customize visuals with `Theme` fields, `NineSlice` frames (`NineSlice.FrameDrawer()`, `NineSliceStateFrame`), or custom `TextDrawer` and `TextMeasurer` hooks for bitmap fonts.

### Image-filled progress (`ImageProgress`)

`ImageProgress` is a read-only meter built from up to three layered images, drawn under → progress → over, for boss bars and ornate meters a flat `ProgressBar` cannot express. Each layer is a `NineSlice`, so a progress image keeps its corner caps at any value and any widget size; a nil layer falls back to a flat fill (`TrackColor` for the under layer, `FillColor` for the progress layer). `Fill` picks how the value reveals the progress layer: `left_to_right` (the default), `right_to_left`, `top_to_bottom`, `bottom_to_top`, `center_h`, `center_v`, or a radial pie with `clockwise` and `counter_clockwise`, which starts at `StartAngle` degrees (0 points right, positive is clockwise) and sweeps `Span` degrees (0 defaults to 360).

```go
bossBar := &gameui.ImageProgress{
	Under:      &gameui.NineSlice{Image: core.Src("asset:ui/bar_back.png"), Src: core.Bounds{Size: core.Size{Width: 96, Height: 24}}, Left: 8, Right: 8},
	Progress:   &gameui.NineSlice{Image: core.Src("asset:ui/bar_fill.png"), Src: core.Bounds{Size: core.Size{Width: 96, Height: 24}}, Left: 8, Right: 8},
	Over:       &gameui.NineSlice{Image: core.Src("asset:ui/bar_frame.png"), Src: core.Bounds{Size: core.Size{Width: 96, Height: 24}}, Left: 8, Right: 8},
	Fill:       gameui.FillLeftToRight,
	Min:        0,
	Max:        100,
	Value:      bossHP,
}
```

## Virtual-resolution scaling (`Viewport`)

Attach a `Viewport` to `Root.Viewport` (or call `Root.DrawWithViewport`) so menus author against a fixed virtual resolution and scale cleanly across aspect ratios:

```go
vp := gameui.NewViewport(1280, 720, gameui.ScaleFit) // ScaleFit, ScaleFill, ScaleStretch, ScaleInteger
vp.LetterboxColor = core.RGB(8, 10, 16)
uiRoot.Viewport = &vp

// In Canvas.Draw:
vp.SetWindowSize(canvasBounds.Size)
uiRoot.Draw(d, canvasBounds)
```

`Root` automatically converts window-space pointer coordinates through `Viewport.WindowToVirtual` during hit-testing and draws letterbox bars when `ScaleFit` or `ScaleInteger` leaves margins.

## Spatial action focus navigation (`SpatialNavigator`)

For gamepad and keyboard menus with grids or multi-column layouts, pair `Root` with `SpatialNavigator`:

```go
nav := gameui.NewSpatialNavigator(uiRoot)
nav.WrapAround = true
nav.OnBack = closeMenu
nav.SetOverrides(resumeBtn, gameui.NavOverrides{Up: muteToggle})

// Inside game.Loop.Update, bridge game/input actions:
nav.StepActions(func(a gameui.NavAction) bool {
	switch a {
	case gameui.NavUp:
		return actions.JustPressed("ui_up")
	case gameui.NavDown:
		return actions.JustPressed("ui_down")
	case gameui.NavLeft:
		return actions.JustPressed("ui_left")
	case gameui.NavRight:
		return actions.JustPressed("ui_right")
	case gameui.NavConfirm:
		return actions.JustPressed("ui_confirm")
	case gameui.NavBack:
		return actions.JustPressed("ui_back")
	default:
		return false
	}
})
```

Customize focus outlines with `FocusRingStyle` on `Root.FocusRing`, `Theme.FocusRing`, or `SpatialNavigator.FocusRing` (including dashed outlines or a custom `Draw` callback).

## Ready settings menu (`SettingsMenu`)

`SettingsMenu` provides a drop-in five-tab settings screen covering **Audio** (mute, master, configurable bus volumes, mono audio, and left/right balance), **Video** (window mode, desktop windowed resolution, scale mode, integer scale, quality preset, and frame cap; VSync is hidden by default until NSGo exposes swap-interval control and can be enabled with `ShowVSync`), **Controls** (keyboard and gamepad action rebinding with capture and reset), **Language** (locale picker), and **Accessibility** (subtitles/captions, SFX captions, text scale, high contrast, reduced motion, screen shake, flash reduction, color-blind mode, game speed, invincibility, auto-aim, and hold-to-toggle).

Templates can gate the first launch with `gameui.FirstLaunchSettings`. It renders a one-action accessibility prompt before the first sound, enables captions by default, and persists the completion marker through the same `save.DefaultSettingsStore` used by `SettingsMenu`:

```go
first := gameui.NewFirstLaunchSettings(gameui.FirstLaunchSettingsConfig{
	Store: save.NewStorageSettingsStore(cx.Storage(), "game"),
	OnMoreOptions: func() { openSettingsAccessibility() },
})
// Mount first.View(cx) last among the root's children; it removes itself after Continue.
```

The prompt is skippable in one action and can be localized with `FirstLaunchSettingsConfig.Text`. The More options button appears only when `OnMoreOptions` is set. Continue keeps the player's other settings, including existing `Custom` keys, and stores completion under the `first_launch_accessibility_complete` key, so a later launch renders nothing.

It persists through `save.DefaultSettingsStore` (shared directly with `a11y.NewAssistFromSettings(cx, store)`) and dispatches validated updates through `save.SettingsTargets` (`save.Apply`) and `SettingsMenuConfig.Video` (`screen.Video.ApplyResolution`):

```go
store := save.NewStorageSettingsStore(cx.Storage(), "mygame")
assist := a11y.NewAssistFromSettings(cx, store)
menu := gameui.NewSettingsMenu(gameui.SettingsMenuConfig{
	Store: store,
	Env:   cx.Env(), // hides unsupported Window Mode / Resolution rows and syncs FullScreen & OS accessibility defaults
	Video: video,    // applies windowed Resolution via video.ApplyResolution on Apply/Save
	Targets: save.SettingsTargets{
		Sound:              mixer,
		ApplyDisplay:       video.Apply,
		ApplyBindings:      func(b map[string][]string) error { gameui.ApplyBindingsToActions(actions, b, true); return nil },
		ApplyLocale:        setLocale,
		ApplyAccessibility: assist.ApplySettings,
	},
	OnBack: closeSettings,
})

// In Canvas.Draw:
menu.Draw(d, bounds)

// Forward mouse, touch, keyboard, and gamepad input:
// - Pointer:  menu.OnPointerMove / OnPointerDown / OnPointerUp / OnPress
// - Touch:    menu.OnTouch(ev)
// - Keyboard: menu.OnKeyDown(ev)
// - Gamepad:  menu.StepGamepad(gamepad.Pads(cx)) (D-pad / left stick, A confirm, B back, LB/RB tabs, button capture)
```

- Use `SettingsResolutionFrom` / `SetSettingsResolution`, `ParseSettingsResolution`, and `ApplySettingsResolution` to read, write, and apply the resolution entry stored in `save.Settings.Custom`. Flash reduction now lives on `save.AccessibilitySettings.ReduceFlashes` (migrated automatically from legacy `Custom["reduce_flashes"]`; `SettingsReduceFlashesFrom` and `SetSettingsReduceFlashes` are kept as deprecated wrappers). Pass `Env: cx.Env()` so Resolution is hidden automatically on phones, tablets, and the web (or set `HideResolution: true`), and set `ShowVSync: true` only when wiring `save.DisplaySettings.VSync` to a real backend control.
- Pass `Env: cx.Env()` so unset Reduced Motion, High Contrast, and Text Scale controls follow the OS accessibility settings (`Env.ReduceMotion()`, `Env.IncreaseContrast()`, `Env.FontScale()`) until the player explicitly changes them, and return to following the OS on Reset. For the full end-to-end accessibility wiring (including `postfx.Flash`/`Shake`, captions, touch targets, and the pre-release checklist), see the [Accessibility Guide](accessibility-guide.md).
- Use `FormatBindingToken`, `ParseBindingToken`, `FormatBindingLabel`, `GamepadButtonToken`, and `ApplyBindingsToActions` to bridge `save.Settings.Bindings` with `input.Actions`.
- Call `SetText` with a translated `SettingsText` struct when the player switches language, and omit `Theme` (or call `HighContrastTheme()`) to automatically switch to high-contrast styling when effective high contrast is enabled.
- Call `SyncGamepad` when opening the menu so any button held to open it does not immediately trigger inside the screen.
- Pass `Env: cx.Env()` in `SettingsMenuConfig` so the Video tab hides Window Mode on platforms that cannot enter full screen (`screen.CanFullScreen`, such as iOS, Android, and phone web browsers) and keeps Window Mode synchronized with `Env.FullScreen()` on the web and after Escape or system window toggles.

## World-anchored native views (`AnchorOverlay`)

Pin ordinary native NSGo views (name tags, speech bubbles, chat inputs) to 2D world coordinates over your game canvas using `AnchorOverlay`:

```go
overlay := gameui.NewAnchorOverlay(cx)
overlay.Add(&gameui.WorldAnchor{
	ID:      "npc-1",
	WorldX:  240,
	WorldY:  120,
	OffsetY: -28,
	AnchorX: 0.5,
	AnchorY: 1,
	Visible: true,
	View: func(cx *core.Scope) core.View {
		return core.Text(cx, "Blacksmith")
	},
})

// Each frame after camera update:
proj := gameui.NewCameraViewportProjection(cam.X, cam.Y, cam.Zoom, 1280, 720, vp)
overlay.Sync(proj)
```

Mount `overlay.View(cx)` above your `Canvas` inside a `core.Stack`. Anchors that project outside `ScreenBounds` are hidden automatically, and positions update via `SetWorldPosition` and `Sync` without rebuilding the surrounding view tree.

## Screen files (`*.screen.yaml`)

Author HUDs and menus as plain-text versioned YAML files (`*.screen.yaml`, published schema `https://game.nsgo.dev/schemas/screen.v1.json`) rather than hand-coding every widget in Go. `ParseScreen`, `LoadScreenFS`, and `EncodeScreen` refuse unknown fields, duplicate node IDs or focus orders, YAML anchors/aliases, and unsupported versions, and round-trip losslessly in canonical 2-space YAML.

A screen defines a `designWidth` × `designHeight` reference canvas (such as `640 × 360`), an optional `scaleMode` (`fit`, `fill`, `stretch`, or `integer`), and a tree of nodes (`vbox`, `hbox`, `label`, `button`, `sprite_row`, `color_rect`, `touch_stick`, `touch_button`, `container`, `image_progress`):

- **Anchors and safe areas:** set `anchor` to one of nine points (`top_left`, `top_center`, `top_right`, `center_left`, `center`, `center_right`, `bottom_left`, `bottom_center`, `bottom_right`) or `fill`, `offset` (`x`, `y` inset inward from right/bottom edges), and `safeArea` (defaults to `true` for non-fill anchors and `false` for `fill`).
- **Sizing and localized text:** store localization keys in `textKey` (never literal strings), use `sizeMode: fixed` with `width` or `sizeMode: fit_text` to grow with localized text on `label` and `button` nodes, and style nodes with the fields their type supports (listed below).
- **Focus order, touch areas, and device classes:** declare explicit 1-based `focusOrder` for keyboard/gamepad traversal (`button`, `touch_button`), `touchHitSize` for expanded touch targets (`button`, `touch_stick`, `touch_button`), and `deviceClasses` (`phone`, `tablet`, `desktop`, `ultrawide`) to show controls only on matching devices. Nested `deviceClasses` intersect with their parent's allowed classes and must share at least one class with the parent; `ParseScreen` and `ValidateScreen` refuse disjoint combinations and unsupported fields per node type.
- **Image-filled progress:** an `image_progress` node layers `under`, `progress`, and `over` image paths into an `ImageProgress`, with `fill` (`left_to_right`, `right_to_left`, `top_to_bottom`, `bottom_to_top`, `center_h`, `center_v`, `clockwise`, `counter_clockwise`), `startAngle` and `span` for the radial fills, and `min`/`max`/`value` as the starting range. Game code updates the widget's `Value` at runtime.

#### Supported fields by node type

Every node supports `id`, `type`, `anchor`, `offset`, `safeArea`, `width`, `height`, and `deviceClasses`; every node type except `image_progress` also supports `background` (fill color on boxes/containers/labels/buttons/color rects, icon fallback color on `sprite_row`, ring base color on `touch_stick`). Type-specific fields are refused on node types that do not read them:

| Node type | Additional supported fields |
| --- | --- |
| `vbox`, `hbox` | `children`, `padding`, `spacing`, `cornerRadius` |
| `container` | `children`, `cornerRadius` |
| `label` | `sizeMode`, `padding`, `textKey`, `fontSize`, `textColor`, `cornerRadius` |
| `button` | `sizeMode`, `padding`, `textKey`, `fontSize`, `textColor`, `cornerRadius`, `focusOrder`, `touchHitSize` |
| `sprite_row` | `spacing`, `sprite`, `spriteFrame`, `spriteCount` |
| `color_rect` | `cornerRadius` |
| `touch_stick` | `touchHitSize`, `textColor` (knob color) |
| `touch_button` | `textKey`, `fontSize`, `textColor`, `focusOrder`, `touchHitSize` |
| `image_progress` | `under`, `progress`, `over`, `fill`, `startAngle`, `span`, `min`, `max`, `value` |

Build a parsed screen into a `gameui.Root` and `SpatialNavigator` at runtime with `BuildScreen`:

```go
doc, err := gameui.LoadScreenFS(assetsFS, "screens/hud.screen.yaml")
if err != nil {
	return err
}

phone := gameui.ReferenceDevicesFor(doc.DesignWidth, doc.DesignHeight)[0]
screen, err := gameui.BuildScreen(doc, gameui.ScreenBuildOptions{
	SafeArea:       phone.SafeArea,
	DeviceClass:    phone.Class,
	ViewportWidth:  phone.Width,
	ViewportHeight: phone.Height,
	Catalog:        catalog,
	Locale:         "zh",
	OnClick: map[string]func(){
		"pause_button": openPauseMenu,
		"jump":         triggerJump,
	},
})
```

Call `screen.Draw(d, bounds)` inside `Canvas.Draw`, switch languages dynamically with `screen.ApplyCatalog(catalog, locale)`, update safe-area insets with `screen.SetSafeArea(insets)`, or switch device filters with `screen.SetDeviceClass(class)`.

### Validation (`game check`) and safe refactoring (`tooling/refs`, `game mv`)

`game check` (and `gameui.CheckScreen`) checks every `*.screen.yaml` file in a project and reports the file, line, and node for:

- touch areas under `44 pt` on phones and tablets;
- localized text that overflows its box in any project locale at the four reference sizes (`Phone 19.5:9 (notch)`, `Tablet 4:3`, `Desktop 16:9`, `Ultrawide 21:9`), accounting for full-width CJK glyphs;
- a `textKey` missing from any project locale string table;
- text under `4.5:1` WCAG contrast against its fill;
- missing `sprite` images or atlas frames;
- missing `under`, `progress`, or `over` images on `image_progress` nodes.

`tooling/refs` and `game mv` follow `sprite` paths, `image_progress` layer paths, and `textKey` references in `*.screen.yaml` files: `game mv art/heart.png art/ui/heart.png` rewrites `sprite:` in place, and `game mv -text-key hud.hint.lever hud.tip.lever` renames a localization key across both `*.screen.yaml` files and YAML string tables.
