# 教程 9：发布

让 Star Catcher 准备好交到玩家手里：给它商店需要的名称、版本和图标，应用失去焦点时暂停，每次发布前在测试里玩完整局，检查所有目标平台，并构建发布包。

完成后的游戏在 [`examples/tutorial/ch09-ship`](../../examples/tutorial/ch09-ship/)。下面每一步的更多细节，包括签名和上传商店，见[构建与发布](shipping.md)。

## 名称、版本和图标

`nsgo.yaml` 现在写明商店里显示的内容：

```yaml
name: Star Catcher
id: dev.nsgo.starcatcher
version: 1.0.0
build_number: 1
copyright: "© 2026 Star Catcher authors"

icon:
  png: assets/Icon.png

dev:
  target: native

build:
  output: dist
  targets: [macos, windows, linux, android, web, ios]
```

- `id` 在每次发布中都必须保持不变：App Store、Google Play 和 Microsoft Store 都靠它识别应用。请使用你拥有的域名的反向 DNS 名称。
- `version` 是玩家看到的版本；`build_number` 每次上传商店加一。
- `icon.png` 是边长 1024 像素的正方形 PNG，`nsgo build` 会把它转换成各平台需要的所有尺寸。若要 macOS 和 iOS 的分层图标，再加上 `icon.apple`，一个 Icon Composer `.icon` 文件。

## 用代码画图标

图标和精灵一样由代码绘制，代码在 `internal/icon`，由一个小工具写出：

```go
//go:generate go run ./tools/genicon assets/Icon.png
```

修改后运行 `go generate ./...` 重新绘制它。PNG 会提交到仓库，因为 `nsgo build` 从 `assets/` 读取它；一个测试防止两者不一致：

```go
func TestIconIsUpToDate(t *testing.T) {
	data, err := os.ReadFile("assets/Icon.png")
	if err != nil {
		t.Fatal(err)
	}
	got, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("assets/Icon.png: %v", err)
	}
	if b := got.Bounds(); b.Dx() != 1024 || b.Dy() != 1024 {
		t.Fatalf("icon is %dx%d, want 1024x1024", b.Dx(), b.Dy())
	}
	want := icon.Render(1024)
	// Compare every pixel of got and want…
}
```

使用手绘美术的游戏把图标直接放在 `assets/` 里，不需要生成器。

## 失去焦点时暂停

应用在后台时，`game.Loop` 本来就不运行更新；但应用只是处于非活动状态时，循环会继续前进：来电响铃时、打开应用切换器时，或者桌面上另一个窗口拿到焦点时。离开一下的玩家回来时应该看到暂停菜单，而不是发现星星在他离开时已经落下。一个副作用监视应用的生命周期，只要应用不处于活动状态，就暂停正在进行的游戏：

```go
cx.Effect(func() {
	if cx.Env().Lifecycle() != core.LifecycleActive && s.phase.GetUntracked() == phasePlaying {
		s.setPhase(phasePaused)
	}
})
```

只要它读过的信号变化，`cx.Effect` 就会再次运行它的函数。这里它只读取生命周期：阶段是用 untracked 方式读取的，所以开始或暂停游戏不会触发它。

`TestLosingTheFocusPausesTheGame` 用 `app.SetEnvironment` 先让应用进入非活动状态、再把它送到后台，每次都把它带回来，然后找到暂停菜单。

## 发布前玩完整局

单元测试一次只检查一条规则。发布前值得确认这些规则合起来是一个游戏：能玩、会越来越难、最终会结束。`TestAWholeGame` 用一个简单的机器人玩三个种子的完整局，机器人朝最低的星星移动，并避开快要落进篮子的石头：

```go
func TestAWholeGame(t *testing.T) {
	for _, seed := range []uint64{1, 2, 3} {
		w := newWorld(seed)
		steps := 0
		for !w.over && steps < 60*60*20 {
			w.step(step, bot(w))
			steps++
		}
		if !w.over {
			t.Fatalf("seed %d: a game lasted over 20 minutes", seed)
		}
		if w.score < 20 {
			t.Fatalf("seed %d: the bot caught only %d stars", seed, w.score)
		}
	}
}
```

因为世界是确定性的，每个种子每次都玩出同一局，所以这个测试从不时好时坏；又因为它直接推进世界，二十分钟的游戏只需几毫秒。如果某次规则改动让游戏赢不了或者永远结束不了，这个测试会比玩家先发现。

## 检查所有目标平台

`nsgo check` 为每个目标平台检查代码，找出在某个平台上无法构建的调用：

```sh
go tool nsgo check -target macos,windows,linux,android,web
go tool nsgo check -target ios,ios_simulator   # on a Mac with Xcode
```

仓库的 CI 在每次改动时为每一章运行这两条检查，并在 Linux、macOS 和 Windows 上运行每一章的测试，见 `.github/workflows/examples.yml`。

## 构建发布包

`nsgo build` 构建 `nsgo.yaml` 列出的目标，在 `dist/` 下为每个目标生成一个目录，旁边是安装器和压缩包：

```sh
go tool nsgo build
go tool nsgo build -target web
go tool nsgo build -channel store -target macos,windows,ios,android
```

Web 构建是一个可以放到任何静态托管上的文件夹。在本地用 `go tool nsgo build -target web -run` 试一试，它会启动服务并打开浏览器。`store` 渠道生成 Mac App Store、Microsoft Store、App Store 和 Google Play 接受的包；签名和上传见[构建与发布](shipping.md)。

## 接下来

Star Catcher 完成了：它能在六个平台上打开，在任何帧率下玩起来都一样，支持触控、鼠标和键盘，有声音，会记忆，能暂停，并且从规则到完整的一局都有测试。要继续扩展它，或者开始你自己的游戏：

- 用 `sprite.LoadAtlas` 加载手绘美术，通过 `"asset:"` URI 加载录制的声音，见[快速开始](getting-started.md)。
- 用 [particles](particles.md) 在接住星星时迸发粒子，用 [tween](tween.md) 让分数弹跳一下。
- 用 [input](input.md) 的 `Save` 和 `Load` 让玩家重新映射按键并保存。
- 用 [replay](replay.md) 录下一局的输入，在测试里回放。
