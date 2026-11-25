# 教程：Star Catcher

分九章从一个空项目做出一款完整、可发布的游戏 Star Catcher。每一章给游戏加一个新概念，每一章完成后的代码都在仓库里，CI 会为所有目标平台构建它并运行它的测试。

Star Catcher 里，星星和石头沿五条通道落下。玩家把篮子在通道之间移动，接住星星、躲开石头。漏掉一颗星星或接到一块石头都会失去三条命中的一条，分数越高，游戏越快。

## 章节

| 章节 | 新增内容 | 用到的包 | 代码 |
| --- | --- | --- | --- |
| [1. 空项目](tutorial-01-empty-project.md) | 模块、`nsgo.yaml`、`Canvas` 和第一个测试 | `core`、`nsgotest` | [`ch01-empty-project`](../../examples/tutorial/ch01-empty-project/) |
| [2. 游戏循环](tutorial-02-game-loop.md) | 固定步长循环、虚拟分辨率，以及测试可以逐步推进的世界 | `game`、`screen` | [`ch02-game-loop`](../../examples/tutorial/ch02-game-loop/) |
| [3. 输入](tutorial-03-input.md) | 通过命名动作，用触控、鼠标和键盘移动篮子 | `input` | [`ch03-input`](../../examples/tutorial/ch03-input/) |
| [4. 精灵](tutorial-04-sprites.md) | 用代码绘制的精灵图集、精灵、批次和动画 | `sprite` | [`ch04-sprites`](../../examples/tutorial/ch04-sprites/) |
| [5. 规则](tutorial-05-rules.md) | 随机生成、碰撞、分数、生命和游戏结束 | `random`、`physics` | [`ch05-rules`](../../examples/tutorial/ch05-rules/) |
| [6. 界面](tutorial-06-screens.md) | 用原生视图做的标题界面、暂停菜单和结束界面 | `core`、`base` | [`ch06-screens`](../../examples/tutorial/ch06-screens/) |
| [7. 声音](tutorial-07-sound.md) | 用 Go 合成的音效、混音器，以及会被记住的静音开关 | `sound`、`audio` | [`ch07-sound`](../../examples/tutorial/ch07-sound/) |
| [8. 存档](tutorial-08-save.md) | 用带版本的存档槽跨启动保存最高分 | `save` | [`ch08-save`](../../examples/tutorial/ch08-save/) |
| [9. 发布](tutorial-09-ship.md) | 图标、商店元数据、失去焦点时暂停，以及发布构建 | `nsgo build` | [`ch09-ship`](../../examples/tutorial/ch09-ship/) |

每一章都是完整的程序。它的目录里是这一章结束时整个游戏的样子，所以每一章都能单独运行，也可以比较两章，看清某一章到底改了什么：

```sh
git diff --no-index examples/tutorial/ch04-sprites examples/tutorial/ch05-rules
```

## 开始之前

你需要 Go 1.27 和 NSGo v0.3.4 或更新版本，见[快速开始](getting-started.md)。教程会边做边解释用到的每个 API；各个包的其余功能见包指南，例如 [Loop](loop.md) 和 [input](input.md)。

如果想在自己的模块里跟着做，从第 1 章开始，它会创建一个模块。如果想直接运行本仓库里的各章，进入 `examples/tutorial`，这个 Go 模块包含全部九章：

```sh
cd examples/tutorial
go tool nsgo dev ./ch05-rules
go tool nsgo test ./ch05-rules
go tool nsgo test ./...
```

`nsgo dev` 在本机运行一章，保存时自动重新加载；加上 `-target web`、`-target ios_simulator` 或 `-target android` 可以在其他平台运行。`nsgo test` 运行一章的测试，用 `./...` 则运行所有章节的测试。

## 游戏的结构

每一章都保持同样的拆分，开始前值得先了解：

- **`world.go`** 保存游戏的状态和规则，只有它的 `step` 方法会改变它们。它对窗口一无所知，所以测试可以直接推进它，同样的种子和操作总是玩出同样的一局。
- **`game.go`** 把世界和屏幕连起来。`game.Loop` 每秒带着玩家的输入调用 `step` 六十次，`Canvas` 在两次之间绘制世界。
- **`screens.go`** 从第 6 章开始出现，在画布上方构建原生 NSGo 视图：分数、菜单和按钮。
- **`*_test.go`** 两边都测：通过 `world` 测规则，通过 `nsgotest` 测整个应用。`nsgotest` 不开窗口就能运行应用，测试可以点按钮、按键、触摸屏幕和推进时间。

从[第 1 章](tutorial-01-empty-project.md)开始。
