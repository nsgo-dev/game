# 无障碍指南

本指南将 [Game Accessibility Guidelines](https://gameaccessibilityguidelines.com/)（GAG）与 [Xbox Accessibility Guidelines](https://learn.microsoft.com/en-us/gaming/accessibility/guidelines)（XAG）映射到 NSGo Game 的各个包，说明如何端到端串联玩家无障碍偏好，提供发布前核查清单，并将 Apple App Store 的无障碍营养标签映射到引擎能够如实支撑的能力。

`pkg/a11y` 的完整 API 参考见 [a11y](a11y.md)。下方每一项都会写明当前已实现的能力、在 [`gameui.SettingsMenu`](gameui.md#开箱即用的设置菜单settingsmenu) 中对应的设置行、仍属于**游戏自身职责**的部分，以及引擎尚缺能力对应的跟踪 issue。

## 端到端串联：`save`、`SettingsMenu`、`a11y`、`sound` 与 `effects`

所有无障碍、音频、显示与按键偏好都保存在同一份类型化的 [`save.Settings`](save.md#玩家设置settingsstore-与-accessibilitysettings) 存储中（`save.DefaultSettingsStore`），并与存档槽完全分离。通过 `a11y.NewAssistFromSettings` 将 [`a11y.Assist`](a11y.md#持久化辅助选项assist) 绑定到同一份存储，即可让 `gameui.SettingsMenu`、`a11y.CaptionsView`、`a11y.AccessibleMenu`、`sound.Mixer` 与 `postfx.Flash` / `postfx.Shake` 自动保持同步：

```go
store := save.NewStorageSettingsStore(cx.Storage(), "mygame")
assist := a11y.NewAssistFromSettings(cx, store)
mixer := sound.NewMixer(cx, "mygame")
captions := a11y.NewCaptionTrack(cx)

// 在第一声音效或第一帧画面前加载并应用已保存的设置。
_, _ = store.LoadAndApply(save.SettingsTargets{
	Sound:              mixer,
	ApplyDisplay:       video.Apply,
	ApplyBindings:      func(b map[string][]string) error { gameui.ApplyBindingsToActions(actions, b, true); return nil },
	ApplyLocale:        setLocale,
	ApplyAccessibility: assist.ApplySettings,
})

// 将 SettingsMenu 接到同一份 store 与 Env，使未修改的项自动跟随系统无障碍默认值。
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

// 全屏闪烁在 ReduceMotion 下停止，在 ReduceFlashes 下变为低对比度染色；震屏幅度按 ScreenShake 缩放。
flash := postfx.NewFlash(cx, postfx.FlashConfig{
	Color:         core.RGB(0xffffff),
	ReduceMotion:  assist.ReduceMotion,
	ReduceFlashes: assist.ReduceFlashes,
})
shake := postfx.NewShake(cx, postfx.ShakeConfig{
	Seed:         42,
	ReduceMotion: assist.ReduceMotion,
})

// 在 game.Loop.Update 中：
sim.Step(assist.ScaleDT(dt))
captions.Update(dt)

// 触发屏幕震动时乘以 assist.ScreenShake()（0..1）：
if amp := 6.0 * assist.ScreenShake(); amp > 0 {
	shake.Trigger(amp, 400*time.Millisecond)
}

// 在 core.Stack 中把原生 CaptionsView 叠放在游戏 Canvas 之上：
a11y.CaptionsView(cx, captions, a11y.CaptionStyle{Assist: assist})
```

### 自动跟随的系统无障碍偏好

NSGo 在 `cx.Env()` 上以响应式 signal 形式暴露操作系统的无障碍偏好：

| 系统信号（`core.Env`） | 引擎集成方式 |
| --- | --- |
| `Env().ReduceMotion()` | 在玩家显式覆盖前，作为 `save.AccessibilitySettings.EffectiveReduceMotion`（`assist.ReduceMotion()`）、`SettingsMenu`、`postfx.Flash` 与 `postfx.Shake` 的默认值。 |
| `Env().IncreaseContrast()` | 在玩家显式覆盖前，作为 `save.AccessibilitySettings.EffectiveHighContrast`（`assist.HighContrast()`）、`assist.Palette()`（`VisionHighContrast`）、`CaptionsView` 与 `SettingsMenu` 自动 `HighContrastTheme()` 的默认值。 |
| `Env().FontScale()` | 在玩家显式覆盖前，作为 `save.AccessibilitySettings.EffectiveCaptionScale`（`assist.TextScale()`，裁剪到 `0.5`–`3.0`）的默认值，由 `CaptionsView` 与 `AccessibleMenu` 读取。 |
| `Env().ScreenReader()` | 可在任意位置读取，用于调整播报行为或将焦点引导到原生视图（`a11y.AccessibleMenu`、`cx.Announce`）。 |
| `Env().Dark()` / `Env().Appearance()` | 可读取以适配深色外观界面。 |
| `Env().RTL()` | 由 [`l10n.EnvDirection`](l10n.md#rtl-视觉排序与布局镜像) 读取，用于 RTL 文本排序和 HUD 镜像。 |
| `Env().SafeArea()` | 由 [`screen.Viewport.SafeGameRectForEnv`](screen.md#移动端-hud-安全区)、[`input.TouchControls.Resize`](input.md#屏幕触控控件) 与 [`gameui`](gameui.md#界面屏幕文件screenyaml) 屏幕文件读取。 |
| `Env().Pointer()` | 当为 `core.PointerCoarse` 时，`input.NewTouchControls` 自动显示屏幕触控控件。 |

调用 `Assist.Reset()`（或在 `SettingsMenu` 中点击 **Reset**）会清除显式覆盖，让 `ReduceMotion`、`HighContrast` 与 `TextScale` 重新跟随系统设置。

## 运动与操作（Motor）

对应 GAG Motor（Basic 与 Intermediate）以及 XAG 106（触控）、107（输入）、108（时机与长按）和 117（设置）。

| 准则（GAG / XAG） | 等级 | 引擎支持与 SettingsMenu 设置行 | 缺口与游戏自身职责 |
| --- | --- | --- | --- |
| **可改键控制**（GAG / XAG 107） | 基础 | [`input.Actions`](input.md#定义动作与轴)（`Bind`、`Save`/`Load`）、[`gameui.SettingsMenu`](gameui.md#开箱即用的设置菜单settingsmenu) **控制** 标签页（`save.Settings.Bindings`、`ApplyBindingsToActions`）。 | 将所有玩法与菜单动作都走 `input.Actions`，避免在逻辑中硬编码具体按键或手柄按钮。 |
| **多输入方式与无缝切换**（GAG / XAG 107） | 基础 | [`input.Actions`](input.md) 用同一套动作名统一键盘、鼠标、手柄、触屏与运动传感器；`input.DeviceTracker` 与 `input.PromptTable` 自动切换按键提示；[`Players.SetDisconnectPolicy`](input.md#手柄断开重连与回退模式) 在手柄断开时自动暂停或回退到键盘/触屏。 | 避免在单一输入设备即可完成操作时强制同时使用两种设备（例如必须同时用键盘和鼠标）。 |
| **单一输入方式操作全部菜单与玩法**（GAG / XAG 107） | 基础 | [`gameui.SpatialNavigator`](gameui.md#空间动作焦点导航spatialnavigator)（`StepActions`、`*.screen.yaml` 中的 `focusOrder`）、`SettingsMenu`（指针、触摸、键盘与 `StepGamepad`）、[`a11y.AccessibleMenu`](a11y.md#屏幕阅读器菜单accessiblemenu)（键盘、点击与屏幕阅读器动作）。 | **引擎缺口**：面向 `gameui` 屏幕文件与 `SettingsMenu` 的单开关扫描（one-switch scanning）跟踪于 [#1778](https://github.com/nsgo-dev/game/issues/1778)。 |
| **触控目标尺寸、安全区、左手镜像与自定义布局**（GAG / XAG 106、107） | 基础 / 进阶 | [`input.TouchControls`](input.md#屏幕触控控件) 强制执行 `input.MinTouchTarget`（`44 pt`）、保持在 `SafeArea` 内、支持 `SetMirrored(true)` 左手镜像，并通过 `Save`/`Load` 持久化玩家编辑后的布局。[`gameui.CheckScreen`](gameui.md#静态检查game-check与安全重构toolingrefsgame-mv)（`go tool game check`）会检查手机和平板上小于 `44 pt` 的触控热区。 | 若游戏依赖虚拟摇杆与按钮，请在界面中提供左手镜像开关或布局调整入口。 |
| **长按转切换与避免高频连打**（GAG / XAG 108） | 基础 / 进阶 | 触控按钮支持 `mode: toggle` 与 `mode: tap`（`TouchModeToggle`、`TouchModeTap`）。`Assist.HoldToToggle()` 与 `SettingsMenu` **无障碍 → 长按转切换** 持久化玩家选择。 | **引擎缺口**：`input.Actions` 上的内置运动辅助修饰器（长按转切换、单击代替连打、最短按压时长、连按防抖）跟踪于 [#1777](https://github.com/nsgo-dev/game/issues/1777)。**游戏职责**：在持续动作（冲刺、瞄准、蹲伏、格挡）中读取 `assist.HoldToToggle()`，避免强制快速连打 QTE。 |
| **灵敏度、轴反转、死区与触觉反馈**（GAG / XAG 107、108） | 基础 / 进阶 | 逐轴 `GamepadAxisOptions`（`DeadZone`、`Axial`、`Sensitivity`、`Invert`）、`PointerAxisOptions`（`Sensitivity`、`Invert`）、`MotionAxisOptions`（`DeadZone`、`Smoothing`、`Calibrate`），以及 `input.Haptics.SetEnabled(false)`（可用 `Haptics.Save`/`Load` 保存）。 | **引擎缺口**：作为标准玩家设置应用到输入与 3D 相机的视角灵敏度、轴反转、摇杆死区、视野（FOV）、相机晃动与震动强度跟踪于 [#1780](https://github.com/nsgo-dev/game/issues/1780)；原生手柄马达震动后端跟踪于 [nsgo#1049](https://github.com/nsgo-dev/nsgo/issues/1049)。 |
| **可调游戏速度与操作容错窗口**（GAG / XAG 108、117） | 进阶 | `Assist.GameSpeed()` / `Assist.ScaleDT(dt)`（`0.25`–`2.0`），`SettingsMenu` **无障碍 → 游戏速度**；[`platformer.Config`](platformer.md#计时)（`coyote_time`、`jump_buffer`、`wall_coyote_time`）；[`rhythm.CalibrationScreen`](rhythm.md#校准) 与 `OffsetStore`。 | **游戏职责**：允许玩家通过 `assist.ScaleDT(dt)` 放慢实时节奏而不惩罚通关进度，并提供宽容的时机窗口或可跳过的 QTE。 |
| **瞄准辅助**（GAG / XAG 108） | 进阶 | `Assist.AutoAim()` 与 `SettingsMenu` **无障碍 → 自动瞄准** 持久化玩家开关。 | **引擎缺口**：面向摇杆、触屏与鼠标的 2D/3D 内置瞄准辅助（减速、磁吸、吸附及玩家强度设置）跟踪于 [#1779](https://github.com/nsgo-dev/game/issues/1779)。 |

在 [#1777](https://github.com/nsgo-dev/game/issues/1777) 落地前读取 `assist.HoldToToggle()` 的最小写法：

```go
if assist.HoldToToggle() {
	if actions.JustPressed("crouch") {
		crouching = !crouching
	}
} else {
	crouching = actions.Pressed("crouch")
}
```

## 认知（Cognitive）

对应 GAG Cognitive（Basic 与 Intermediate）以及 XAG 112（阅读与节奏）、113–115（引导与难度）和 117（设置）。

| 准则（GAG / XAG） | 等级 | 引擎支持与 SettingsMenu 设置行 | 缺口与游戏自身职责 |
| --- | --- | --- | --- |
| **保存并记住全部设置**（GAG / XAG 117） | 基础 | [`save.DefaultSettingsStore`](save.md#玩家设置settingsstore-与-accessibilitysettings) 独立于存档槽持久化音频、视频、键位、语言与无障碍设置，并提供原子备份恢复。 | **引擎缺口**：为所有入门模板内置带无障碍标签页的设置界面，并在首次启动发声前提供入口，跟踪于 [#1783](https://github.com/nsgo-dev/game/issues/1783)。 |
| **随时暂停与撤销误操作**（GAG / XAG 115） | 基础 | [`game.Loop.SetPaused`](loop.md#暂停与同步子系统)（`Loop.OnPause = mixer.SetPaused`）、进入 `core.LifecycleBackground` 时自动暂停，以及面向解谜/回合制游戏的 [`undo.History`](undo.md)（`Undo`/`Redo`/`Seek`，可用 `undo.EncodeLog` 持久化）。 | **游戏职责**：允许在玩法与过场中随时暂停；单人模式避免无法暂停的实时倒计时提示。 |
| **自主控制阅读节奏、对话回看与可跳过过场**（GAG / XAG 112） | 基础 / 进阶 | [`vn.Presenter`](vn.md#presenter)（`CharsPerSecond` 设为负值可整句立即显示、`Advance`、`SetAuto`、`SetSkip`、`Backlog` 与 `ReplayVoice`）；[`cutscene.Cutscene`](cutscene.md#跳过)（`skippable: true`、`skipAfter`）。 | **游戏职责**：默认由玩家手动推进对话，避免剧情文本在短计时后自动消失。 |
| **可调速度与无敌辅助**（GAG / XAG 115） | 基础 / 进阶 | `Assist.GameSpeed()` / `ScaleDT`（`SettingsMenu` **无障碍 → 游戏速度**）与 `Assist.Invincibility()`（**无障碍 → 无敌模式**）。 | **游戏职责**：提供可选难度档位（如剧情 / 轻松 / 标准）、跳过卡关战斗的选项与充足的检查点。 |
| **清晰目标提示与匹配设备的按键指引**（GAG / XAG 113、114） | 基础 / 进阶 | [`quest.Log`](quest.md#运行任务) 追踪进行中的任务与目标以驱动日志或 HUD；`input.PromptTable` + `DeviceTracker` 显示与当前设备一致的按键提示；[`l10n.Catalog`](l10n.md) 负责本地化文本。 | **游戏职责**：允许随时查看当前目标与重玩教学，使用简明无黑话的文案，并允许关闭非必要倒计时。 |

## 视觉（Vision）

对应 GAG Vision（Basic 与 Intermediate）以及 XAG 101（文本）、102（对比度）、103（色彩）、104（视觉舒适与布局）和 105（屏幕朗读）。

| 准则（GAG / XAG） | 等级 | 引擎支持与 SettingsMenu 设置行 | 缺口与游戏自身职责 |
| --- | --- | --- | --- |
| **清晰可读且可缩放至 200% 的文本**（GAG / XAG 101） | 基础 / 进阶 | `cx.Env().FontScale()` 与 `Assist.TextScale()`（`0.5`–`3.0`，`SettingsMenu` **无障碍 → 文本缩放**）自动缩放 `CaptionsView` 与 `AccessibleMenu`。[`text.SDFFont`](text.md#有向距离场字体) 与 [`text.DrawPlatform`](text.md#使用-drawplatform-绘制平台文本) 在任意尺寸下保持清晰。`gameui.CheckScreen`（`go tool game check`）会在手机、平板、桌面与超宽屏四个参考尺寸下检查多语言文本溢出。[`Root.TextScale`](gameui.md#文本缩放textscale) 可把 `gameui` 控件与 `*.screen.yaml` 文本放大至 200%，`ScreenCheckOptions.TextScale` 会在该比例下检查溢出。 | **引擎缺口**：自动跟随 `Env.FontScale()` 与玩家设置，以及独立的 HUD 缩放，跟踪于 [#1772](https://github.com/nsgo-dev/game/issues/1772)。**游戏职责**：选择清晰易读的字体，关键界面避免使用极小像素字。 |
| **充足对比度与高对比度模式**（GAG / XAG 102） | 基础 | `cx.Env().IncreaseContrast()`、`Assist.HighContrast()`（`SettingsMenu` **无障碍 → 高对比度**）、`a11y.PaletteFor(VisionHighContrast)`（`21:1` 前景/背景对比度）、`gameui.HighContrastTheme()`（未自定义 `Theme` 时 `SettingsMenu` 自动启用）、`a11y.ContrastRatio`、`a11y.EnsureContrast`，以及 `gameui.CheckScreen`（`go tool game check`）对 `*.screen.yaml` 文本强制检查 ≥ `4.5:1` WCAG 对比度。 | **游戏职责**：在复杂玩法画面上方绘制 HUD 文本时加半透明深色底衬，并用 `a11y.ContrastRatio(fg, bg) >= 4.5` 检查自定义画布文本。 |
| **色盲友好调色板与 CVD 模拟测试**（GAG / XAG 103） | 基础 | `a11y.PaletteFor(mode)`（基于 Okabe-Ito / Wong 方案，覆盖 `VisionNormal`、`VisionProtanopia`、`VisionDeuteranopia`、`VisionTritanopia`、`VisionHighContrast`）、`Assist.ColorVision()` / `Assist.Palette()`（`SettingsMenu` **无障碍 → 色盲模式**）、`a11y.Simulate(c, mode)` 以及 [`postfx.PaletteSwap`](postfx.md#调色板替换)。 | **引擎缺口**：按色盲模式作用于整帧画面的后处理滤镜以及编辑器/测试中的色觉预览，跟踪于 [#1776](https://github.com/nsgo-dev/game/issues/1776)。**游戏职责**：**绝不单靠颜色传递信息**——始终用不同轮廓、图标、花纹、描边或文字标签配合颜色。 |
| **屏幕阅读器与界面朗读**（GAG / XAG 105） | 进阶 | `cx.Env().ScreenReader()`、`cx.Announce(text)`、`cx.Speak(text, voice, done)`（`cx.Env().CanSpeak()`）、原生 NSGo 无障碍属性（`Role`、`Label`、`Hint`、`AccessibilityDescription`、`AccessibilityValue`、`LiveRegion`）、[`a11y.AccessibleMenu`](a11y.md#屏幕阅读器菜单accessiblemenu)（支持 `MenuText` / `l10n` 本地化）、[`a11y.CaptionsView`](a11y.md#实时字幕captiontrack-与-captionsview) 以及 `input.TouchControls.Draw`（输出带标签与角色的 `core.CanvasNode`）。 | **引擎缺口**：将画布绘制的 `gameui` 控件、`SettingsMenu` 与 `*.screen.yaml` 暴露给屏幕阅读器并播报焦点与数值，跟踪于 [#1425](https://github.com/nsgo-dev/game/issues/1425)。在 `#1425` 落地前，需要完整 VoiceOver/TalkBack 支持的菜单请使用 `a11y.AccessibleMenu` 或原生 NSGo 视图。 |
| **安全区、深色界面与 RTL 布局**（GAG / XAG 102、104） | 进阶 | `cx.Env().Dark()` / `Env().Appearance()`、[`screen.Viewport.SafeGameRectForEnv`](screen.md#移动端-hud-安全区)、`*.screen.yaml` 中的 `safeArea`、[`l10n.MirrorX`](l10n.md#rtl-视觉排序与布局镜像) / `LayoutAlignment` 以及 [`screen.Video`](screen.md#视频设置) 窗口/全屏模式。 | **游戏职责**：确保 HUD 不被刘海、底部指示条或曲面屏遮挡，并让前景玩法元素与背景有清晰分层。 |
| **3D 相机视野（FOV）与镜头晃动**（GAG / XAG 104） | 进阶 | `scene3d.Camera` 支持在游戏代码中调整视野角（FOV）。 | **引擎缺口**：将 FOV 与镜头晃动作为标准玩家设置自动应用到 3D 相机，跟踪于 [#1780](https://github.com/nsgo-dev/game/issues/1780)。 |

在单元测试中用 `a11y.Simulate` 和 `a11y.ContrastRatio` 守住自定义调色板的对比度：

```go
for _, mode := range []a11y.ColorVision{a11y.VisionProtanopia, a11y.VisionDeuteranopia, a11y.VisionTritanopia} {
	pal := a11y.PaletteFor(mode)
	if ratio := a11y.ContrastRatio(pal.Foreground, pal.Background); ratio < 4.5 {
		t.Errorf("%s foreground contrast %.2f < 4.5", mode, ratio)
	}
	_ = a11y.Simulate(pal.Enemy, mode)
}
```

## 听觉（Hearing）

对应 GAG Hearing（Basic 与 Intermediate）以及 XAG 109（字幕）和 110–111（音频与替代线索）。

| 准则（GAG / XAG） | 等级 | 引擎支持与 SettingsMenu 设置行 | 缺口与游戏自身职责 |
| --- | --- | --- | --- |
| **对话字幕与方向性音效字幕**（GAG / XAG 109） | 基础 / 进阶 | [`a11y.CaptionTrack`](a11y.md#实时字幕captiontrack-与-captionsview) 与 `CaptionsView`（`ShowDialogue`、`ShowSFX`、`ShowSFXAt`、`ShowImportant`、`CaptionMusic`）、`CaptionStyleFromLocalizer`、`Assist.CaptionsEnabled()` 与 `Assist.SFXCaptionsEnabled()`、`SettingsMenu` **无障碍 → 字幕**、**音效字幕** 与 **文本缩放**。当 `|Direction| >= 0.25` 时，`Caption.Format()` 会自动加上 `[<]` 或 `[>]`。 | **引擎缺口**：玩家可调的字幕背景与透明度、说话人颜色、按类型开关、两行上限与设置页实时预览跟踪于 [#1773](https://github.com/nsgo-dev/game/issues/1773)；音效、过场与对话自动接入 `CaptionTrack` 及屏幕方向性声音指示器跟踪于 [#1774](https://github.com/nsgo-dev/game/issues/1774)。 |
| **分通道音量与静音**（GAG / XAG 110） | 基础 | [`sound.Mixer`](sound.md#mixer-与持久化音量)（`Master` 与按总线分组的音量，如 `"music"`、`"sfx"`、`"voice"`、`"ui"`）、`save.AudioSettings`（`Master`、`Groups`、`Muted`）、`SettingsMenu` **音频** 标签页。 | **游戏职责**：将音乐、音效、语音与界面提示分别路由到不同总线，方便玩家压低背景音乐而保留人声。 |
| **单声道音频与左右平衡**（GAG / XAG 110） | 基础 / 进阶 | [`sound.Mixer`](sound.md#单声道音频与左右平衡)（`SetMono`、`FollowSystemMono`、`SetBalance`）、`save.AudioSettings`（`Mono`、`Balance`）、`SettingsMenu` **音频 → 单声道音频** 与 **左右平衡**。`Listener2D.Pan` 与 `Listener3D.Pan` 仍保留真实空间方向，单声道下方向性字幕依然准确。 | **NSGo v0.3.4 限制**：立体声音频资源保留各自声道，直至 NSGo 支持输出混音为单声道（[nsgo#2676](https://github.com/nsgo-dev/nsgo/issues/2676)）；自动跟随系统单声道开关将在 NSGo 暴露 `Env.MonoAudio`（[nsgo#2677](https://github.com/nsgo-dev/nsgo/issues/2677)）后生效。 |
| **声音线索的视觉与触觉替代**（GAG / XAG 109、111） | 基础 / 进阶 | [`input.Haptics`](input.md#屏幕触控控件)（`Play`、`Rumble`、`PlayPattern` 以及可用于触发视觉提示的 `OnHaptic` 回调）；`CaptionTrack.ShowImportant` 与 `ShowSFXAt`。 | **游戏职责**：为所有影响玩法的声音线索（敌人攻击前摇、低血量警报、画外威胁、节奏节拍）提供清晰的视觉或触觉对应，确保静音状态下仍可完整游玩。 |

## 语音（Speech）

对应 GAG Speech（Basic 与 Intermediate）以及 XAG 118–119（沟通）。

| 准则（GAG / XAG） | 等级 | 引擎支持与 SettingsMenu 设置行 | 缺口与游戏自身职责 |
| --- | --- | --- | --- |
| **不要求语音输入**（GAG / XAG 118） | 基础 | NSGo Game 的任何包都不依赖麦克风或语音输入。原生语音合成（`cx.Speak`、`cx.Env().CanSpeak()`）与语音识别（`cx.StartSpeech`、`cx.Env().CanRecognizeSpeech()`）作为可选钩子提供在 `core.Scope` 上。 | **游戏职责**：确保所有玩法与菜单都不以说话为前提。 |
| **多人沟通的文字替代方案**（GAG / XAG 119） | 基础 / 进阶 | 原生 NSGo 文本输入与叠加视图（`core.TextInput`、`gameui.AnchorOverlay`）可在游戏画布之上承载文字聊天与快捷短语轮盘。 | **游戏职责**：若游戏提供语音聊天，请同时提供文字聊天或预设信号/快捷短语、按玩家单独静音，以及可选的文字转语音 / 语音转文字桥接。 |

## 光敏安全与动态舒适度（Photosensitivity & Motion Comfort）

对应 GAG General/Vision（光敏与晕动症）以及 XAG 104（视觉舒适）和 120（光敏安全）。

| 准则（GAG / XAG） | 等级 | 引擎支持与 SettingsMenu 设置行 | 缺口与游戏自身职责 |
| --- | --- | --- | --- |
| **减少动态效果与屏幕震动控制**（GAG / XAG 104） | 基础 | `cx.Env().ReduceMotion()`、`Assist.ReduceMotion()` 与 `Assist.ScreenShake()`（`0.0`–`1.0`）、`SettingsMenu` **无障碍 → 减少动态效果** 与 **屏幕震动**、[`postfx.Shake`](postfx.md#暗角闪屏与震屏)（`ShakeConfig.ReduceMotion`）。 | **游戏职责**：根据 `assist.ReduceMotion()` 与 `assist.ScreenShake()` 门控或缩放 [`tilemap.Camera.Shake`](tilemap.md#跟随边界限制与震动)、视差滚动、界面待机弹跳与镜头过渡。 |
| **全屏闪烁与光敏癫痫安全**（GAG / XAG 120） | 基础 | `Assist.ReduceFlashes()`、`SettingsMenu` **无障碍 → 减弱闪烁**、[`postfx.Flash`](postfx.md#光敏安全)（`FlashConfig.ReduceFlashes`、`FlashConfig.Limiter`）。`postfx.Flash` 在任意滚动的一秒内最多运行 3 次，共享的 `postfx.FlashLimiter` 可为多个闪屏统一限频，高饱和红色闪屏会被去饱和；把 `assist.ReduceFlashes` 传给 `FlashConfig.ReduceFlashes`，闪屏即变为低对比度染色。 | **引擎缺口**：粒子与精灵闪烁尚不读取 `ReduceFlashes`，在测试中检查录制帧的闪烁与高对比图案违规跟踪于 [#1775](https://github.com/nsgo-dev/game/issues/1775)。**游戏职责**：同一画面的全屏闪屏共享一个 `FlashLimiter`，让其他闪烁内容保持低于每秒 3 次，并避免 `postfx.Flash` 之外的大面积高亮或高饱和红色频闪。 |
| **高对比重复图案与画面畸变**（GAG / XAG 104、120） | 进阶 | [`postfx.CRT`](postfx.md#crt-与扫描线) 扫描线与曲面畸变为可选着色器通道，可在开启 `ReduceMotion()` 或 `ReduceFlashes()` 时关闭。 | **引擎缺口**：3D FOV 与镜头晃动的标准玩家设置跟踪于 [#1780](https://github.com/nsgo-dev/game/issues/1780)。**游戏职责**：避免在大面积区域使用高对比静止或滚动条纹/棋盘格图案，并为后处理畸变或泛光提供开关。 |

将 `tilemap.Camera.Shake` 与 `postfx.Shake` 一同受无障碍偏好控制：

```go
if !assist.ReduceMotion() {
	if intensity := 6.0 * float32(assist.ScreenShake()); intensity > 0 {
		cam.Shake(intensity, 250*time.Millisecond)
	}
}
```

## 发布前无障碍核查清单

在构建发布包之前逐项核查（构建流程见[构建与发布](shipping.md)）：

- [ ] **设置已持久化并能跨启动记住**：在 `SettingsMenu` 中修改音频、视频、控制、语言与无障碍各项并重启应用，确认所有设置独立于存档槽生效（`save.DefaultSettingsStore`）；点击 **Reset** 后确认未显式设置的 `ReduceMotion`、`HighContrast` 与 `TextScale` 重新跟随 `cx.Env()`。
- [ ] **在第一声声音前提供字幕与无障碍选项**：确认玩家在片头过场或标题音乐播放之前就能开启字幕、音效字幕、单声道音频和减少动态/闪烁（模板默认接入跟踪于 [#1783](https://github.com/nsgo-dev/game/issues/1783)）。
- [ ] **200% 文本缩放下可读**：将系统字号调至 200%（`Env.FontScale() == 2.0`）并将 `Assist.TextScale()` 设为 `2.0`，检查 `CaptionsView`、`AccessibleMenu` 与原生 HUD 文本是否截断或重叠（在 [#1772](https://github.com/nsgo-dev/game/issues/1772) 落地前，同时手工核查 `gameui` 屏幕）。
- [ ] **对比度与非颜色线索检查**：运行 `go tool game check`，确认所有 `*.screen.yaml` 满足 `4.5:1` 对比度、移动端 `44 pt` 触控热区且各语言无文本溢出；用 `a11y.ContrastRatio` 与 `a11y.Simulate` 检查自定义画布配色，并在灰度模式下试玩确认没有任何机制仅靠颜色区分。
- [ ] **闪烁与动态舒适度检查**：开启 **减少动态效果** 与 **减弱闪烁**，触发所有受击闪白、爆炸、闪电、震屏、Boss 转阶段与过场动画，确认无意外闪烁、无屏幕震动，且无超过 3 Hz 的全屏亮度或高饱和红光频闪（自动化帧检测跟踪于 [#1775](https://github.com/nsgo-dev/game/issues/1775)）。
- [ ] **单一输入方式走完全流程**：分别**仅用**键盘、**仅用**手柄、**仅用**触屏（确认 `44 pt` 热区位于 `SafeArea` 内）和**仅用**鼠标，走完标题页 → 设置 → 游玩 → 暂停 → 结算全流程，并验证手柄断开与重连恢复（`Players.SetDisconnectPolicy`）。
- [ ] **iOS 与 Android 屏幕阅读器实测**：在真机上开启 **VoiceOver**（iOS）与 **TalkBack**（Android）遍历每个原生视图、`AccessibleMenu`、`CaptionsView` 与 `TouchControls` 画布节点，验证焦点顺序、朗读标签与数值，确认画布绘制的 `gameui` 界面不会阻断导航（见 [#1425](https://github.com/nsgo-dev/game/issues/1425)）。

## Apple App Store 无障碍营养标签对照表

Apple App Store 允许开发者在 App Store Connect 中声明九项[无障碍营养标签](https://developer.apple.com/help/app-store-connect/manage-app-accessibility/overview-of-accessibility-nutrition-labels)（Accessibility Nutrition Labels）。只有当你的**完整**核心体验满足 Apple 的要求时才应勾选，而不能仅凭引擎存在某个辅助 API 就声明。

| App Store 标签 | 引擎支持该项的能力 | 当前限制与跟踪 issue | 声明前必须完成的测试 |
| --- | --- | --- | --- |
| **旁白（VoiceOver）** | 原生 NSGo 视图（`Role`、`Label`、`Hint`、`AccessibilityValue`、`LiveRegion`）、`a11y.AccessibleMenu`、`a11y.CaptionsView`、`input.TouchControls.Draw`（`core.CanvasNode`）、`cx.Announce`、`cx.Env().ScreenReader()`。 | 画布绘制的 `gameui` 控件、`SettingsMenu` 与 `*.screen.yaml` 尚未暴露给 VoiceOver（[#1425](https://github.com/nsgo-dev/game/issues/1425)）；自定义画布或 3D 场景需游戏自行提供 `CanvasNode` 或原生替代界面。 | 在 iOS 真机开启 VoiceOver（含幕帘模式）走完引导、菜单、设置与核心玩法，确认所有可交互控件都有准确的标签、角色、数值与合理焦点顺序。 |
| **语音控制（Voice Control）** | 带唯一清晰 `Label` 的原生 NSGo 视图、`a11y.AccessibleMenu` 与 `input.TouchControls.Draw`（`CanvasNode` 标签）会向 iOS 语音控制暴露命名目标。 | 画布绘制的 `gameui` 控件与 `*.screen.yaml` 尚未暴露（[#1425](https://github.com/nsgo-dev/game/issues/1425)）；高频实时连续移动/瞄准通常难以单靠语音命令完成。 | 在 iOS 语音控制中开启“显示名称”与“显示编号”，确认所有常见操作与菜单项均可通过语音可靠触发而不会卡死。 |
| **更大字体（Larger Text）** | `cx.Env().FontScale()`、`Assist.TextScale()`（`0.5`–`3.0`）、`CaptionsView`、`AccessibleMenu`、原生 `core.Text`、`text.SDFFont`、`text.DrawPlatform`。 | `gameui` 控件、`SettingsMenu` 与 `*.screen.yaml` 尚未按 `Env.FontScale()` 自动缩放到 200%（[#1772](https://github.com/nsgo-dev/game/issues/1772)）。 | 启动前将 iOS 动态字体调至无障碍特大号（≥ 200%），验证菜单、HUD、对话与字幕中的所有可读文本均能清晰放大且无裁切或重叠。 |
| **深色界面（Dark Interface）** | `cx.Env().Dark()` 与 `cx.Env().Appearance()`、默认深色的 `a11y.PaletteFor` 预设、`gameui.DefaultTheme()` / `HighContrastTheme()`。 | 游戏美术与自定义主题需自行适配深色外观。 | 在 iOS 浅色与深色模式之间切换，验证所有界面、弹窗、设置页与叠加层在深色模式下可读舒适且无刺眼白屏闪烁。 |
| **不单靠颜色区分（Differentiate Without Color Alone）** | `a11y.PaletteFor(...)`（Okabe-Ito / Wong 调色板）、`a11y.Simulate(...)`、`SettingsMenu` 色盲模式。 | 整帧色盲后处理滤镜跟踪于 [#1776](https://github.com/nsgo-dev/game/issues/1776)；若玩法状态仅靠颜色表达，单有调色板并不满足此标签。 | 开启 iOS **色彩滤镜 → 灰度**并游玩全部核心机制（敌人、拾取物、危险区域、解谜块、状态条），确认形状、图标、花纹或文字始终能独立传达状态。 |
| **足够对比度（Sufficient Contrast）** | `gameui.CheckScreen` / `go tool game check`（检查 `*.screen.yaml` ≥ `4.5:1`）、`a11y.ContrastRatio`、`a11y.EnsureContrast`、`CaptionsView`（强制 `7.0:1`）、跟随 `cx.Env().IncreaseContrast()` 的 `Assist.HighContrast()`、`gameui.HighContrastTheme()`。 | 在 `*.screen.yaml` 之外自绘的 `Canvas` / 3D HUD 文本与图标不会由 `game check` 自动核查。 | 运行 `go tool game check`，开启 iOS **增强对比度**，并核查所有自定义 HUD/菜单前景色与背景色，确保常规文本 ≥ `4.5:1`、大号文本与关键控件 ≥ `3:1`。 |
| **减弱动态效果（Reduced Motion）** | `cx.Env().ReduceMotion()`、默认跟随系统的 `Assist.ReduceMotion()`、`Assist.ScreenShake()`、`Assist.ReduceFlashes()`、`SettingsMenu` 无障碍标签页、`postfx.ShakeConfig.ReduceMotion`、`postfx.FlashConfig.ReduceMotion`。 | `postfx.Flash` 需显式接入 `FlashConfig.ReduceFlashes`；`tilemap.Camera.Shake`、补间动画、视差与粒子需在游戏代码中检查 `assist.ReduceMotion()`。 | 在启动游戏前开启 iOS **减弱动态效果**（不改动游戏内设置），验证震屏、全屏闪烁、页面过渡动画、视差与界面待机动画立即减弱或关闭。 |
| **字幕（Captions）** | `a11y.CaptionTrack`、`a11y.CaptionsView`、`Assist.CaptionsEnabled()`、`Assist.SFXCaptionsEnabled()`、`Caption.Format()`（说话人名称与 `[<]`/`[>]` 方向标记）、`SettingsMenu` 无障碍标签页。 | 游戏目前需自行将对话、音效与过场提示推送到 `CaptionTrack`（[#1774](https://github.com/nsgo-dev/game/issues/1774)）；玩家可调字幕底衬、透明度与说话人颜色跟踪于 [#1773](https://github.com/nsgo-dev/game/issues/1773)。 | 在 iOS 上全程静音游玩整款游戏（含片头、过场、对话与实机音效），验证所有语音台词与关键音效均准确同步显示字幕。 |
| **音频描述（Audio Descriptions）** | `pkg/cutscene` 与 `pkg/sound` 暂未内置音频描述轨播放器；游戏可通过 `sound.Mixer` 叠加同步旁白总线，或用 `cx.Speak`（`Env.CanSpeak()`）为纯画面过场朗读描述。 | 纯视觉过场与媒体的音频描述完全由游戏自行制作与接入。 | 仅当所有纯视觉视频或剧情段落都提供可选且同步的音频描述轨（解释关键视觉动作且不与台词冲突）时方可声明。 |
