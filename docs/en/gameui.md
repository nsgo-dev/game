# gameui

`game/gameui` builds stylized in-canvas menus and HUD widgets drawn directly into `core.Drawing`, with box layout, virtual-resolution scaling, 2D spatial action focus navigation, and world-anchored native NSGo overlays.

```go
import "github.com/nsgo-dev/game/packages/game/gameui"
```

## Layout containers, widgets, and `Root`

Compose a widget tree from layout containers (`Panel`, `VBox`, `HBox`) and widgets (`Label`, `Button`, `Toggle`, `Slider`, `List`, `ProgressBar`, `Spacer`), then wrap it in a `Root`:

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
