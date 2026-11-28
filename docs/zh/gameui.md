# gameui

`game/gameui` 用于在 `core.Drawing` 中直接绘制风格化的游戏内菜单与 HUD 控件，支持盒式布局、虚拟分辨率缩放、基于动作的 2D 空间焦点导航，以及锚定到 2D 世界坐标的原生 NSGo 叠加视图。

```go
import "github.com/nsgo-dev/game/pkg/gameui"
```

## 布局容器、控件与 `Root`

用布局容器（`Panel`、`VBox`、`HBox`）和控件（`Label`、`Button`、`Toggle`、`Slider`、`List`、`ProgressBar`、`ImageProgress`、`Spacer`）组装控件树，再用 `Root` 包装：

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

### 图像填充进度条（`ImageProgress`）

`ImageProgress` 是一个只读进度表，由最多三层图像按 under → progress → over 的顺序叠加绘制，适合扁平 `ProgressBar` 无法表达的 Boss 血条和华丽仪表。每一层都是 `NineSlice`，因此进度图像在任意数值、任意控件尺寸下都能保持四角盖帽不变形；某一层为 nil 时退回纯色填充（under 层用 `TrackColor`，progress 层用 `FillColor`）。`Fill` 决定数值如何显露 progress 层：`left_to_right`（默认）、`right_to_left`、`top_to_bottom`、`bottom_to_top`、`center_h`、`center_v`，以及径向扇形的 `clockwise` 与 `counter_clockwise`——扇形从 `StartAngle` 度开始（0 指向右侧，正值为顺时针），扫过 `Span` 度（0 默认 360）。

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

## 开箱即用的设置菜单（`SettingsMenu`）

`SettingsMenu` 提供一套可直接接入的五标签设置界面，覆盖 **音频**（静音、主音量、可配置音频总线音量、单声道音频与左右平衡）、**视频**（窗口/全屏模式、桌面端窗口分辨率、缩放模式、整数缩放、画质预设与帧率上限；由于 NSGo 尚未提供交换间隔控制，垂直同步默认隐藏，可通过 `ShowVSync` 显式开启）、**控制**（支持按键与手柄按键捕获和重置的动作改键、视角灵敏度、反转、死区、扳机阈值、震动强度、视野、镜头摇晃、镜头摆动与运动模糊）、**语言**（地区/语言选择）以及 **无障碍**（字幕、音效字幕、文本缩放、高对比度、减少动态效果、屏幕震动、减弱闪烁、色盲模式、游戏速度、无敌模式、自动瞄准与长按转切换）。

菜单通过 `save.DefaultSettingsStore` 持久化玩家设置（与 `a11y.NewAssistFromSettings(cx, store)` 直接共享同一份存储），并通过 `save.SettingsTargets`（`save.Apply`）与 `SettingsMenuConfig.Video`（`screen.Video.ApplyResolution`）将校验后的设置派发到各子系统：

```go
store := save.NewStorageSettingsStore(cx.Storage(), "mygame")
assist := a11y.NewAssistFromSettings(cx, store)
menu := gameui.NewSettingsMenu(gameui.SettingsMenuConfig{
	Store: store,
	Env:   cx.Env(), // 自动隐藏当前平台不支持的窗口模式/分辨率项，并同步全屏状态与系统无障碍默认值
	Video: video,    // 在 Apply/Save 时通过 video.ApplyResolution 应用窗口分辨率
	Targets: save.SettingsTargets{
		Sound:              mixer,
		ApplyDisplay:       video.Apply,
		ApplyBindings:      func(b map[string][]string) error { gameui.ApplyBindingsToActions(actions, b, true); return nil },
		ApplyLocale:        setLocale,
		ApplyAccessibility: assist.ApplySettings,
	},
	OnBack: closeSettings,
})

// 在 Canvas.Draw 中绘制：
menu.Draw(d, bounds)

// 转发鼠标、触摸、键盘与手柄输入：
// - 指针：menu.OnPointerMove / OnPointerDown / OnPointerUp / OnPress
// - 触摸：menu.OnTouch(ev)
// - 键盘：menu.OnKeyDown(ev)
// - 手柄：menu.StepGamepad(gamepad.Pads(cx))（十字键/左摇杆导航，A 确认，B 返回，LB/RB 切换标签页，并支持按键改键捕获）
```

- 使用 `SettingsResolutionFrom` / `SetSettingsResolution`、`ParseSettingsResolution` 和 `ApplySettingsResolution` 读写并应用保存在 `save.Settings.Custom` 中的分辨率选项。减弱闪烁现已迁入 `save.AccessibilitySettings.ReduceFlashes`（加载旧文件时自动从 `Custom["reduce_flashes"]` 迁移；`SettingsReduceFlashesFrom` 与 `SetSettingsReduceFlashes` 保留为已弃用的兼容封装）。传入 `Env: cx.Env()` 可在手机、平板和 Web 上自动隐藏分辨率项（或通过 `HideResolution: true` 手动隐藏），仅在游戏将 `save.DisplaySettings.VSync` 接到真实的底层开关时才设置 `ShowVSync: true`。
- 传入 `Env: cx.Env()` 后，未手动修改过的“减少动态效果”、“高对比度”和“文本缩放”会跟随系统无障碍设置（`Env.ReduceMotion()`、`Env.IncreaseContrast()`、`Env.FontScale()`），点击重置后再次恢复跟随系统。关于无障碍设置的完整端到端串联（包括 `postfx.Flash`/`Shake`、字幕、触控热区与发布前核查清单），见[无障碍指南](accessibility-guide.md)。
- 使用 `FormatBindingToken`、`ParseBindingToken`、`FormatBindingLabel`、`GamepadButtonToken` 与 `ApplyBindingsToActions` 在 `save.Settings.Bindings` 和 `input.Actions` 之间双向转换。
- 在玩家切换语言后调用 `SetText` 传入翻译后的 `SettingsText`；未自定义 `Theme` 时（或调用 `HighContrastTheme()`），有效高对比度开启时会自动切换到高对比度配色。
- 打开菜单时调用 `SyncGamepad`，避免用于打开设置界面的按键按住状态在菜单内立即触发。
- 在 `SettingsMenuConfig` 中传入 `Env: cx.Env()`，可在无法进入全屏的平台（`screen.CanFullScreen`，如 iOS、Android 和手机浏览器）上隐藏“窗口模式”行，并在 Web 以及按 Escape 或系统控件切换全屏后使窗口模式与 `Env.FullScreen()` 保持一致。

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

## 界面屏幕文件（`*.screen.yaml`）

可将 HUD 与菜单编写为带版本号的纯文本 YAML 文件（`*.screen.yaml`，已发布 Schema 为 `https://game.nsgo.dev/schemas/screen.v1.json`），而不必在 Go 代码中手写所有控件。`ParseScreen`、`LoadScreenFS` 与 `EncodeScreen` 会拒绝未知字段、重复的节点 ID 或焦点顺序、YAML 锚点/别名以及不支持的版本号，并以规范的 2 空格缩进 YAML 无损往返。

屏幕文件声明 `designWidth` × `designHeight` 设计画布尺寸（例如 `640 × 360`）、可选的 `scaleMode`（`fit`、`fill`、`stretch`、`integer`）以及节点树（`vbox`、`hbox`、`label`、`button`、`sprite_row`、`color_rect`、`touch_stick`、`touch_button`、`container`、`image_progress`）：

- **锚点与安全区**：通过 `anchor` 指定九宫格锚点（`top_left`、`top_center`、`top_right`、`center_left`、`center`、`center_right`、`bottom_left`、`bottom_center`、`bottom_right`）或 `fill`，通过 `offset`（右侧/底部锚点的正值向内缩进）设置偏移，并用 `safeArea` 控制是否保持在安全区内（非 `fill` 默认为 `true`，`fill` 默认为 `false`）。
- **尺寸与本地化文本**：在 `textKey` 中存放本地化键而非字面文本，在 `label` 与 `button` 节点上使用 `sizeMode: fixed` 搭配 `width` 或 `sizeMode: fit_text` 随翻译文本自适应宽度，并按节点类型支持的属性（见下表）设置样式。
- **焦点顺序、触控热区与设备类别**：在 `button` 与 `touch_button` 上使用从 1 开始的 `focusOrder` 声明键盘/手柄焦点遍历顺序，在 `button`、`touch_stick` 与 `touch_button` 上使用 `touchHitSize` 设置扩展触控热区，并使用 `deviceClasses`（`phone`、`tablet`、`desktop`、`ultrawide`）按设备类别条件显示控件。嵌套节点的 `deviceClasses` 会与父节点取交集，且必须与父节点至少有一个重叠的设备类别；`ParseScreen` 与 `ValidateScreen` 会拒绝无交集的嵌套设备类别以及各节点类型不支持的字段。
- **图像填充进度条**：`image_progress` 节点将 `under`、`progress`、`over` 三个图像路径叠加为 `ImageProgress`，支持 `fill`（`left_to_right`、`right_to_left`、`top_to_bottom`、`bottom_to_top`、`center_h`、`center_v`、`clockwise`、`counter_clockwise`）、径向填充的 `startAngle` 与 `span`，以及初始量程 `min`/`max`/`value`。游戏代码在运行时更新控件的 `Value`。

#### 各节点类型支持的字段

所有节点均支持 `id`、`type`、`anchor`、`offset`、`safeArea`、`width`、`height` 与 `deviceClasses`；除 `image_progress` 外的节点类型均支持 `background`（盒容器/容器/标签/按钮/纯色矩形的填充色、`sprite_row` 的图标回退色、`touch_stick` 的底环颜色）。节点类型未使用的字段会在校验时被拒绝：

| 节点类型 | 额外支持的字段 |
| --- | --- |
| `vbox`、`hbox` | `children`、`padding`、`spacing`、`cornerRadius` |
| `container` | `children`、`cornerRadius` |
| `label` | `sizeMode`、`padding`、`textKey`、`fontSize`、`textColor`、`cornerRadius` |
| `button` | `sizeMode`、`padding`、`textKey`、`fontSize`、`textColor`、`cornerRadius`、`focusOrder`、`touchHitSize` |
| `sprite_row` | `spacing`、`sprite`、`spriteFrame`、`spriteCount` |
| `color_rect` | `cornerRadius` |
| `touch_stick` | `touchHitSize`、`textColor`（摇杆旋钮颜色） |
| `touch_button` | `textKey`、`fontSize`、`textColor`、`focusOrder`、`touchHitSize` |
| `image_progress` | `under`、`progress`、`over`、`fill`、`startAngle`、`span`、`min`、`max`、`value` |

运行时调用 `BuildScreen` 将屏幕文件构建为 `gameui.Root` 与 `SpatialNavigator`：

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

在 `Canvas.Draw` 中调用 `screen.Draw(d, bounds)`，并可通过 `screen.ApplyCatalog(catalog, locale)` 动态切换语言、通过 `screen.SetSafeArea(insets)` 更新安全区边距、通过 `screen.SetDeviceClass(class)` 切换设备类别过滤。

### 静态检查（`game check`）与安全重构（`tooling/refs`、`game mv`）

`game check`（及 `gameui.CheckScreen`）会检查项目中的所有 `*.screen.yaml` 文件，并按文件、行号和节点 ID 报告以下问题：

- 手机和平板上的触控热区小于 `44 pt`；
- 本地化文本（含全角中文字符宽度）在四个参考尺寸（`Phone 19.5:9 (notch)`、`Tablet 4:3`、`Desktop 16:9`、`Ultrawide 21:9`）下超出固定框或安全区；
- `textKey` 在项目的任意语言字符串表中缺失；
- 文本与其填充背景的 WCAG 对比度低于 `4.5:1`；
- 引用的 `sprite` 图片或图集帧缺失；
- `image_progress` 节点引用的 `under`、`progress` 或 `over` 图片缺失。

`tooling/refs` 与 `game mv` 会追踪 `*.screen.yaml` 中的 `sprite` 路径、`image_progress` 图层路径和 `textKey` 引用：`game mv art/heart.png art/ui/heart.png` 会原地更新 `sprite:`，而 `game mv -text-key hud.hint.lever hud.tip.lever` 会同步重命名所有 `*.screen.yaml` 与 YAML 本地化字符串表中的键。
