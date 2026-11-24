# 快速开始

创建一个 NSGo 游戏项目，在桌面端、Web、iOS 和 Android 上运行它，在固定步长循环中绘制带动画的精灵、处理触控和鼠标输入，并用混音器播放音效与音乐。

## 环境要求

- **Go 1.27** 或更高版本。
- **NSGo `v0.3.0`** 或更新版本，新项目依赖它，并把它作为 `nsgo` 工具运行。
- 构建 iOS 目标：需要 macOS 和 Xcode。
- 在 Android 真机或模拟器上运行：需要 `adb`（Android SDK Platform-Tools）。仅构建 `android_arm64` 的 APK 只需要 Go。

在项目目录中运行 `go tool nsgo doctor`，可以检查当前机器已安装的工具链。

## 创建项目

用 `game` 命令的 `new` 子命令创建项目。它把起手模板写入一个新目录，再让 Go 把游戏层和 `game` 工具加入它的 `go.mod`，与模板自带的 `nsgo` 工具并列：

```sh
go run github.com/nsgo-dev/game/packages/game/cmd/game@latest new mygame
cd mygame
go tool nsgo dev
```

起手模板包含精灵动画、点击得分和测试。它的设置都在[项目文件](project.md) `game.yaml` 中，`nsgo.yaml` 与之保持一致。`new` 用目录名作为这两个文件里的 `name` 和 `id`，以及模块路径；`-name` 和 `-module` 可以另行指定，例如 `-module example.com/mygame`。所有参数见[命令行](cli.md#new)。发布游戏之前，把应用标识改成你自己的：在 `game.yaml` 中修改游戏名称和应用标识，然后运行 `go run github.com/nsgo-dev/game/packages/game/cmd/gameproject sync` 把它们写入 `nsgo.yaml`。

NSGo 自带的命令也能从模板模块写出同一个起手模板，但不包含 `game` 工具：`nsgo init -template github.com/nsgo-dev/game/template -module example.com/mygame mygame`。这需要支持 [Go 模块模板](https://nsgo.dev/zh/docs/getting-started/#来自-go-模块的模板)的 NSGo 命令，例如 `go install github.com/nsgo-dev/nsgo/cmd/nsgo@v0.3.0`。它写出的 `game.yaml` 和 `nsgo.yaml` 把游戏命名为 `Starter Game`，需要你自己修改。

如果你已经检出了本仓库，`go run ./packages/game/cmd/game new ../mygame` 会用 `template/template/` 中的起手模板创建项目；从检出的仓库构建的命令没有自己的版本号，所以除非用 `-version` 指定，项目会依赖最新发布的游戏层。也可以在仓库根目录运行 `go tool nsgo dev ./template/template`，直接启动模板游戏。

把游戏在运行时加载的文件放入 `resources/`，把应用图标等构建时需要的文件放入 `assets/`：

```text
mygame/
├─ go.mod
├─ game.yaml
├─ nsgo.yaml
├─ main.go
├─ project.go
├─ game.go
├─ game_test.go
├─ resources/
│  ├─ hero.png
│  ├─ hero.json
│  ├─ jump.wav
│  └─ theme.m4a
└─ assets/
   └─ Icon.png
```

`resources/` 下的所有文件都会随各平台安装包一起打包，并在代码中通过 `"asset:<路径>"` 访问（例如 `"asset:hero.png"` 或 `"asset:sounds/jump.wav"`）。图集 JSON 可以直接用 `//go:embed` 嵌入到 Go 二进制中。

## 在各平台运行

`nsgo dev` 会构建并启动应用，保存代码时自动热重载。

桌面端（使用当前机器的原生后端，也是 `nsgo.yaml` 的默认目标）：

```sh
go tool nsgo dev
```

Web（在 8080 端口启动本地开发服务器并自动打开浏览器）：

```sh
go tool nsgo dev -target web
```

iOS（模拟器，或已连接的 iPhone；真机可用 `-team` 指定开发者团队，默认沿用 Xcode 上次使用的团队）：

```sh
go tool nsgo dev -target ios_simulator
go tool nsgo dev -target ios
```

Android（已连接的真机或运行中的模拟器；连接多台设备时用 `-device` 指定）：

```sh
go tool nsgo dev -target android
```

给任意 `nsgo dev` 命令加上 `-fps`，可以在运行中的游戏画面上叠加 NSGo 的性能监视器。

如果还想在保存 `resources/` 下的图集、图片和数据文件时让运行中的游戏重新加载它们，用 `go tool game dev` 运行游戏（先执行 `go get -tool github.com/nsgo-dev/game/packages/game/cmd/game`），并用 `hotreload.NewDevWatcher` 在这些文件上启动监视器，做法见[保存时重新加载](assets.md#保存时重新加载)。

## 用 Loop 和 sprite.Batch 绘制精灵

游戏就是一个 NSGo 组件。它在自己的作用域上启动 `game.Loop`，在 `Update` 中以固定频率（默认每秒 60 次）推进状态，在 `Draw` 中更新 `core.Signal`，让 `core.Canvas` 重绘而不必重新提交整个视图树。

用 TexturePacker 或 Aseprite 导出图集（关闭旋转），把 PNG 放到 `resources/hero.png`，用 `//go:embed` 嵌入 JSON。如果还没有导出图集，也可以按 [sprite 指南](sprite.md)中的方式手写 `*sprite.Atlas` 字面量。

```go
package main

import (
	_ "embed"
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/core"
)

//go:embed resources/hero.json
var heroJSON []byte

func Game(cx *core.Scope) core.Node {
	atlas, err := sprite.LoadAtlas(core.Src("asset:hero.png"), heroJSON)
	if err != nil {
		panic(err)
	}

	walk := atlas.Animations["walk"]
	hero := sprite.NewSprite(walk.Frame())
	hero.OriginX = hero.Frame.Width / 2
	hero.OriginY = hero.Frame.Height / 2
	hero.X, hero.Y = 160, 120

	batch := &sprite.Batch{
		Atlas:   atlas,
		Sprites: []*sprite.Sprite{&hero},
	}

	x, y := hero.X, hero.Y
	lastX, lastY := x, y
	targetX, targetY := x, y
	alpha := core.NewSignal(cx, 0.0)

	loop := &game.Loop{
		Rate: 60,
		Update: func(dt time.Duration) {
			lastX, lastY = x, y
			dx, dy := targetX-x, targetY-y
			if dist := float32(math.Hypot(float64(dx), float64(dy))); dist > 1 {
				step := min(dist, 180*float32(dt.Seconds()))
				x += dx / dist * step
				y += dy / dist * step
				hero.FlipX = dx < 0
				walk.Update(dt)
				hero.Frame = walk.Frame()
			}
		},
		Draw: func(a float64) {
			alpha.Set(a)
		},
	}
	loop.Run(cx)

	return core.Canvas{
		Style: core.Style{FlexGrow: 1, FlexShrink: 1, Background: core.RGB(0x181c24)},
		OnPress: func(e core.PressEvent) {
			targetX = float32(e.Position.X)
			targetY = float32(e.Position.Y)
		},
		Draw: func(d *core.Drawing, _ core.Size) {
			a := float32(alpha.Get())
			hero.X = lastX + (x-lastX)*a
			hero.Y = lastY + (y-lastY)*a
			batch.Draw(d)
		},
	}
}

func main() { nsgo.Run(Game) }
```

因为 `Draw` 读取了 `alpha.Get()`，NSGo 每个显示帧只重绘 `Canvas`。在 `(lastX, lastY)` 与 `(x, y)` 之间按 `alpha` 插值，可以让画面在 60 Hz、120 Hz 或可变刷新率显示器上都保持顺滑，同时逻辑模拟固定在 60 Hz。

## 处理触控与鼠标输入

`core.Canvas.OnPress` 同时处理手机上的点击和桌面端、Web 上的鼠标点击，提供的 `e.Position` 以画布自身的点（points）为单位。

如果需要连续追踪指针、拖拽或多点触控，可以用一个 `core.View` 包裹 `Canvas`，监听 NSGo 的指针与触控事件（`OnPressDown`、`OnPressUp`、`OnMouseMove`、`OnTouch`、`OnPan`），并用 `core.Absolute` 在上面叠加原生 HUD 视图：

```go
return core.View{
	Style: core.Style{FlexGrow: 1, FlexShrink: 1},
	OnMouseMove: func(e core.MouseEvent) {
		if e.PressedButton == core.MouseButtonLeft {
			targetX, targetY = float32(e.Position.X), float32(e.Position.Y)
		}
	},
	OnTouch: func(e core.TouchEvent) {
		if len(e.Touches) > 0 {
			targetX = float32(e.Touches[0].Position.X)
			targetY = float32(e.Touches[0].Position.Y)
		}
	},
	Children: []core.Node{
		canvas,
		core.View{
			Style: core.Style{
				Position:   core.Absolute,
				Top:        core.Pt(12),
				Left:       core.Pt(12),
				PaddingEnv: core.EnvAll(core.EnvSafeArea),
			},
			Children: []core.Node{
				core.Text{Content: "Score: 0", Style: core.Style{Color: core.RGB(0xffffff)}},
			},
		},
	},
}
```

键盘和手柄的输入动作映射计划在 `game/input` 包中提供（[#3](https://github.com/nsgo-dev/game/issues/3)）。

## 用 sound.Mixer 播放声音

在组件作用域上创建一个 `sound.Mixer`。它会把主音量和各分组音量保存在 `core.Storage` 中，应用切入后台时自动静音，并统一管理短音效（`Effect`）和流式背景音乐（`Music`）。

将音频以 WAV、AAC（`.m4a`）或 MP3 格式放入 `resources/`：

```go
mixer := sound.NewMixer(cx, "sound")
music := mixer.Music("music")
music.Loop = true

var jump *sound.Effect
audio.LoadSound(cx, audio.Source{URI: "asset:jump.wav"}, func(s *audio.Sound, err error) {
	if err != nil {
		return
	}
	jump = mixer.Effect(s, "effects", 4)
})
```

在玩家按下时播放音效并启动背景音乐（在 Web 上，浏览器要求在首次用户手势之后才允许播放音频）：

```go
started := false
// 在 OnPress 中：
if !started {
	started = true
	music.Play(audio.Source{URI: "asset:theme.m4a"}, 2*time.Second)
}
if jump != nil {
	jump.Play(audio.Voice{Volume: 0.8})
}
```

## 下一步

- 跟着[教程](tutorial.md)分九章从空项目做出一款完整的游戏 Star Catcher，直到构建发布包。
- 阅读各个包的详细指南：[Loop](loop.md)、[sprite](sprite.md) 和 [sound](sound.md)，或查阅汇总的[包](packages.md)参考。
- 试玩并阅读示例平台跳跃游戏 [Cave Run](https://github.com/nsgo-dev/game/tree/main/examples/caverun)：它用到了 Loop、键盘、游戏手柄、触控和鼠标上的输入动作、精灵批次、带镜头的 Tiled 地图、场景、声音分组以及原生 HUD 和暂停菜单，带有 1000 个精灵的压力模式，以及在无界面环境中把关卡一直玩到终点的测试。
- 探索 3D glTF 场景示例 [Lantern 3D](https://github.com/nsgo-dev/game/tree/main/examples/lantern3d)：在 NSGo 的 `gpu.View` 上通过 `game/gltf` 加载内嵌的二进制 glTF 2.0 庭院场景，使用 `render3d.UploadMesh` 与 `render3d.UploadTexture` 上传网格与纹理，通过 `render3d.LightRig`（定向主光、环绕暖色点光源、神龛光晕与环境光）和 render3d 的金属度-粗糙度 PBR 着色照亮场景，并配有 `scene3d.OrbitController` 轨道相机与原生 `scene.HUD3D` 界面叠层。
- 准备好检查所有目标平台并构建 macOS、Windows、Linux、Web、iOS 和 Android 发布包时，阅读[构建与发布](shipping.md)。
