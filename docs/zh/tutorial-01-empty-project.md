# 教程 1：空项目

为 Star Catcher 创建 Go 模块，在 `nsgo.yaml` 里描述应用，打开一个用 `Canvas` 绘制场地的窗口，并在没有窗口的情况下测试它。

完成后的代码在 [`examples/tutorial/ch01-empty-project`](../../examples/tutorial/ch01-empty-project/)。

## 创建模块

游戏就是一个普通的 Go 模块，依赖游戏库和 NSGo，并把 `nsgo` 命令作为它的工具之一：

```sh
mkdir starcatcher && cd starcatcher
go mod init example.com/starcatcher
go get github.com/nsgo-dev/game
go get -tool github.com/nsgo-dev/nsgo/cmd/nsgo@v0.3.4
```

之后 `go tool nsgo` 会运行模块锁定版本的 `nsgo` 命令，所以每个构建这个游戏的人用的都是同一个版本。

## 描述应用

`go.mod` 旁边的 `nsgo.yaml` 给应用命名，并说明 `nsgo dev` 如何运行它：

```yaml
name: Star Catcher 1
id: dev.nsgo.starcatcher.ch01
version: 0.1.0

dev:
  target: native
```

`id` 是应用在 Apple 平台上的 bundle identifier，也是它在 Android 上的 application ID。每一章都有自己的 `id`，所以两章同时装在一台手机上也不会互相覆盖。[第 9 章](tutorial-09-ship.md)会设置商店里显示的名称、版本和图标。

## 打开窗口

`main.go` 把根组件交给 NSGo：

```go
package main

import "github.com/nsgo-dev/nsgo"

func main() { nsgo.Run(Root) }
```

`Root` 是一个 NSGo 组件：一个接收 `*core.Scope`、返回要显示的视图的函数。Star Catcher 在铺满窗口的 `core.Canvas` 里绘制场地，并在它上方用原生 `core.Text` 显示标题：

```go
func Root(cx *core.Scope) core.Node {
	return core.View{
		Style: core.Style{FlexGrow: 1, FlexShrink: 1, Background: skyColor},
		Children: []core.Node{
			core.Canvas{
				TestID: "game-canvas",
				Label:  "Star Catcher playfield",
				Style:  fill(),
				Draw:   drawPlayfield,
			},
			core.View{
				PointerEvents: core.PointerEventsNone,
				Style: core.Style{
					Position:   core.Absolute,
					Top:        core.Pt(0),
					Left:       core.Pt(0),
					Right:      core.Pt(0),
					AlignItems: core.AlignCenter,
					Padding:    core.All(24),
					PaddingEnv: core.EnvAll(core.EnvSafeArea),
				},
				Children: []core.Node{
					core.Text{
						TestID:  "title",
						Content: "Star Catcher",
						Style:   core.Style{Color: textColor, FontSize: 28, FontWeight: core.FontWeightBold},
					},
				},
			},
		},
	}
}
```

- `fill()` 返回一个把子视图固定到父视图四条边的样式，所以无论窗口多大，画布都铺满它。
- `PaddingEnv: core.EnvAll(core.EnvSafeArea)` 让标题避开手机的刘海和底部横条。
- `PointerEventsNone` 让触摸穿过标题落到场地上，[第 3 章](tutorial-03-input.md)会开始监听这些触摸。
- `TestID` 给测试用的视图命名，`Label` 给屏幕阅读器用的视图命名。

画布用一个 `*core.Drawing` 和它自己的尺寸（以点为单位）调用 `Draw`，尺寸变化时会重新绘制：

```go
func drawPlayfield(d *core.Drawing, size core.Size) {
	w, h := float32(size.Width), float32(size.Height)
	d.SetFillColor(skyColor).FillRect(0, 0, w, h)
	d.SetFillColor(groundColor).FillRect(0, h-groundHeight, w, groundHeight)
}
```

运行它：

```sh
go tool nsgo dev
```

深色的夜空铺满窗口，底部是地面，顶部是标题。调整窗口大小，地面始终在底部。

## 测试

NSGo 的 `nsgotest` 包可以在没有窗口的情况下运行应用：`nsgotest.Start` 把应用挂载到测试指定大小的窗口里，`Find` 按 `TestID` 或文字查找视图、找不到就让测试失败，画布的 `Ops` 是它记录下来的绘制操作。

```go
func TestMain(m *testing.M) { nsgotest.Main(m) }

func TestShowsTitleOverPlayfield(t *testing.T) {
	app := nsgotest.Start(t, Root, nsgotest.Size(360, 640))
	app.Find("text:Star Catcher")

	ops := app.Find("game-canvas").Ops
	if !hasFillColor(ops, skyColor) {
		t.Fatal("canvas did not paint the sky")
	}
	if !hasFillColor(ops, groundColor) {
		t.Fatal("canvas did not paint the ground")
	}
}
```

当测试在真实平台上运行时（例如 `go tool nsgo test -target macos`），`TestMain` 把主线程交给平台。这一章的第二个测试用 `app.Resize` 调整窗口大小，检查地面移到了新的底边。用下面的命令运行测试：

```sh
go tool nsgo test ./...
```

## 下一章

场地现在还只是一张图。[第 2 章](tutorial-02-game-loop.md)用固定步长的游戏循环让它动起来。
