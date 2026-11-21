# gameui

`game/gameui` 用于在 `core.Drawing` 中直接绘制风格化的游戏内菜单与 HUD 控件，支持盒式布局、虚拟分辨率缩放、基于动作的 2D 空间焦点导航，以及锚定到 2D 世界坐标的原生 NSGo 叠加视图。

```go
import "github.com/nsgo-dev/game/packages/game/gameui"
```

## 布局容器、控件与 `Root`

用布局容器（`Panel`、`VBox`、`HBox`）和控件（`Label`、`Button`、`Toggle`、`Slider`、`List`、`ProgressBar`、`Spacer`）组装控件树，再用 `Root` 包装：

```go
theme := gameui.DefaultTheme()

resumeBtn := &gameui.Button{Text: "继续游戏", OnClick: resumeGame}
volumeSlider := &gameui.Slider{Label: "音量", Min: 0, Max: 1, Value: 0.8, Step: 0.1, OnChange: setVolume}
muteToggle := &gameui.Toggle{Label: "静音", Style: gameui.ToggleSwitch, OnChange: setMuted}

menu := &gameui.Panel{
	Width:   320,
	Padding: core.EdgesAll(16),
	Child: &gameui.VBox{
		Spacing: 10,
		Align:   gameui.AlignStretch,
		Children: []gameui.Widget{
			&gameui.Label{Text: "已暂停", TextAlign: core.TextAlignCenter},
			resumeBtn,
			volumeSlider,
			muteToggle,
		},
	},
}

uiRoot := gameui.NewRoot(menu)
uiRoot.Theme = &theme
```

在 `Canvas.Draw` 中调用 `uiRoot.Draw(d, bounds)`（内部自动执行 `Layout` 并包裹在 `Save`/`Restore` 中，不会污染调用方的画布状态）。指针与键盘事件通过 `OnPointerMove`、`OnPointerDown`、`OnPointerUp`、`OnPress` 和 `OnKeyDown` 派发。

可通过 `Theme`、`NineSlice` 九宫格边框（`NineSlice.FrameDrawer()`、`NineSliceStateFrame`）以及自定义的 `TextDrawer` / `TextMeasurer` 钩子接入位图字体与自定义皮肤。

## 虚拟分辨率缩放（`Viewport`）

将 `Viewport` 赋给 `Root.Viewport`（或调用 `Root.DrawWithViewport`），即可按固定虚拟分辨率排版菜单并在各种屏幕比例下稳定缩放：

```go
vp := gameui.NewViewport(1280, 720, gameui.ScaleFit) // ScaleFit、ScaleFill、ScaleStretch、ScaleInteger
vp.LetterboxColor = core.RGB(8, 10, 16)
uiRoot.Viewport = &vp

// 在 Canvas.Draw 中：
vp.SetWindowSize(canvasBounds.Size)
uiRoot.Draw(d, canvasBounds)
```

`Root` 会在命中测试时自动通过 `Viewport.WindowToVirtual` 转换窗口坐标，并在 `ScaleFit` 或 `ScaleInteger` 存在留边时绘制信箱黑边。

## 空间动作焦点导航（`SpatialNavigator`）

面向手柄与键盘驱动的网格或多列菜单，将 `Root` 与 `SpatialNavigator` 搭配使用：

```go
nav := gameui.NewSpatialNavigator(uiRoot)
nav.WrapAround = true
nav.OnBack = closeMenu
nav.SetOverrides(resumeBtn, gameui.NavOverrides{Up: muteToggle})

// 在 game.Loop.Update 中桥接 game/input 动作：
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

通过 `Root.FocusRing`、`Theme.FocusRing` 或 `SpatialNavigator.FocusRing` 上的 `FocusRingStyle` 可自定义焦点框（包括虚线样式或自定义 `Draw` 回调）。

## 世界坐标锚定原生视图（`AnchorOverlay`）

使用 `AnchorOverlay` 将普通原生 NSGo 视图（头顶名牌、对话气泡、聊天输入框）钉在游戏画布上的 2D 世界位置：

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
		return core.Text(cx, "铁匠")
	},
})

// 每帧相机更新后同步：
proj := gameui.NewCameraViewportProjection(cam.X, cam.Y, cam.Zoom, 1280, 720, vp)
overlay.Sync(proj)
```

在 `core.Stack` 中将 `overlay.View(cx)` 叠放在游戏 `Canvas` 之上即可。投射到 `ScreenBounds` 之外的锚点会自动隐藏，位置通过 `SetWorldPosition` 与 `Sync` 更新而无需重建外层视图树。
