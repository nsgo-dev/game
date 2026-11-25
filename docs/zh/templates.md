# 项目模板

新项目从一个模板开始：模板是一个小而完整的游戏，在你改动任何一行之前就能构建、运行并通过测试。`game new` 把模板写入一个新目录，NSGo 自带的 `nsgo init -template` 则从模板的 Go 模块写出同样的文件。

## 现有模板

| 模板 | 内容 | `nsgo init -template` 模块 |
| --- | --- | --- |
| `starter` | 一个点击得分的 2D 游戏：固定步长循环上的动画精灵、原生 HUD 和无头测试，是最小的起步项目。默认模板。 | `github.com/nsgo-dev/game/template` |
| `fighting2d` | 一对一的 2D 格斗游戏：两名原创角色，有搓招触发的必杀技、连段、上下段防御、回合和计时，可对战电脑或第二名玩家，用键盘、手柄或触屏操作。详见[格斗游戏模板](template-fighting2d.md)。 | `github.com/nsgo-dev/game/template/fighting2d` |
| `puzzle2d` | Spark Slide，一个 2D 网格解谜游戏：让火花在棋盘上滑行、点亮每一盏灯，关卡用 YAML 定义，支持撤销和重做、步数上限、每关星级，以及保存进度的选关界面；键盘、手柄、触屏和鼠标都能玩。 | `github.com/nsgo-dev/game/template/puzzle2d` |
| `racing3d` | 一个 3D 赛车游戏：在封闭赛道上与沿赛车线行驶的对手比赛两圈，包含射线车辆、检查点、倒计时、最佳圈幽灵车和结算画面，可用键盘、手柄或触屏操作。[3D 赛车模板](template-racing3d.md) | `github.com/nsgo-dev/game/template/racing3d` |
| `rpg2d` | 一个俯视角 RPG：可以行走的小镇和草地、带分支对话的人物、带装备的物品栏、一个任务、回合制战斗和三个存档槽。[指南](template-rpg2d.md)。 | `github.com/nsgo-dev/game/template/rpg2d` |

`go tool game new -list` 由命令自身打印同样的列表，默认模板排在最前。

### puzzle2d

火花一直滑行到被墙挡住为止，并点亮途经的每一盏灯；在步数用完之前点亮一关的所有灯即过关，滑进虚空或步数用完时仍有灯未亮则失败。这个模板展示解谜游戏如何成长：规则在 `board.go` 中，每一步都是 `undo.History` 的一个操作，可以精确地撤销和重做；关卡在 `levels.yaml` 中，这是一个带版本号、拒绝未知字段并有规范形式的 YAML 文件，由 `levels.go` 读写；输入映射在 `game.yaml` 中，绑定到 `input.Actions`，滑动手势来自 `input.GestureRecognizer`；移动由固定步长 `game.Loop` 上的 `tween` 播放动画；每关的星级用 `save` 保存。要修改游戏，可以在 `levels.yaml` 中增改关卡，测试会检查每一关都能在标准步数内完成；在 `game.yaml` 中重新绑定操作；在 `board.go` 中添加新的格子类型并在 `draw.go` 中绘制它。模板的 README 列出了全部操作、关卡格式和每个文件。

## 用模板创建项目

```sh
go run github.com/nsgo-dev/game/packages/game/cmd/game@latest new -list
go run github.com/nsgo-dev/game/packages/game/cmd/game@latest new -template starter mygame
```

不加 `-template` 时，`game new` 使用 `starter`。不在列表中的名称会被拒绝，并列出现有的模板名。`go tool game` 的 shell 补全（见[命令行](cli.md#completion)）会用同样的名称补全 `-template`。其他参数见[命令行](cli.md#new)。

NSGo 自带的命令不需要 `game` 工具，也能从模板的模块写出项目，这需要支持 [Go 模块模板](https://nsgo.dev/zh/docs/getting-started/#来自-go-模块的模板)的 NSGo 命令，例如 `go install github.com/nsgo-dev/nsgo/cmd/nsgo@v0.3.0`：

```sh
nsgo init -template github.com/nsgo-dev/game/template -module example.com/mygame mygame
```

每个模板使用上表列出的模块，加上 `@<version>` 可以固定版本。这样写出的 `game.yaml` 和 `nsgo.yaml` 保留模板自己的游戏名称和标识符，需要你自己修改；而 `game new` 会按目录名或 `-name` 设置它们。

## 模板的目录结构

每个模板都是本仓库中一个独立的 Go 模块，因为 `nsgo init -template` 写出的是所给模块根目录下的 `template` 目录。`starter` 就是 `template/` 模块本身；其他每个模板都是 `template/<name>/` 中的模块 `github.com/nsgo-dev/game/template/<name>`：

```text
template/
├─ go.mod                     module github.com/nsgo-dev/game/template
├─ template/                  starter
└─ <name>/
   ├─ go.mod                  module github.com/nsgo-dev/game/template/<name>
   └─ template/               <name> 模板
      ├─ go.mod.tmpl
      ├─ go.sum
      ├─ game.yaml
      ├─ nsgo.yaml
      ├─ main.go
      ├─ …_test.go
      ├─ README.md
      └─ .gitignore
packages/game/cmd/game/
├─ templates.go               注册表：每个模板一条
└─ _templates/projects/
   ├─ starter/                template/template 的副本
   └─ <name>/                 template/<name>/template 的副本
```

`game new` 嵌入 `_templates/projects` 下的副本，所以它在模块缓存中、没有本仓库时也能工作。仓库的 `go.work` 使用每个模板模块，因此在检出的仓库中，`go tool nsgo dev ./template/<name>/template` 运行某个模板，`go tool nsgo test ./template/<name>/template` 用本地的游戏层测试它。

## 新增模板

新增一个模板用一个单独的 pull request：

1. 创建模块：`template/<name>/go.mod` 声明 `module github.com/nsgo-dev/game/template/<name>`，`go` 行与 `template/go.mod` 相同，项目放在 `template/<name>/template/`。名称只能用小写字母、数字和 `-`，以字母开头，且不能是 `template`。
2. 把 `./template/<name>` 加入 `go.work` 的 `use` 列表。
3. 在 `packages/game/cmd/game` 中把项目复制进 `game` 命令：`cp -R ../../../../template/<name>/template _templates/projects/<name>`。
4. 在 `packages/game/cmd/game/templates.go` 的 `projectTemplates` 中加一条：`newProjectTemplate("<name>", templateModuleRoot+"/<name>", "<一行英文，说明这是什么游戏>")`。
5. 在 `docs/en/templates.md` 和 `docs/zh/templates.md` 上面的表格中为模板各加一行，并在 `changelog.d/` 下加一个片段。

`template/<name>/template/` 中的项目遵循以下规则，测试会尽可能检查它们：

- `go.mod.tmpl` 以 `module {{.Module}}` 开头，依赖一个已发布的、包含模板所用全部包的 `github.com/nsgo-dev/game/packages/game` 版本，并带有 `tool github.com/nsgo-dev/nsgo/cmd/nsgo`。它是唯一含模板动作的文件：`game new` 和 `nsgo init` 也会渲染 `nsgo.yaml`、`main.go` 和其他 `.tmpl` 文件，但 CI 只渲染 `go.mod.tmpl`，模板还必须在工作区中原样构建，所以这些文件渲染后必须与原文相同，`main.go` 中也不能出现 `{{`（例如 `[]T{{...}}` 这样的复合字面量），这类代码放到其他 Go 文件中。其余文件按原样复制，内容不限。名为 `go.mod` 的文件会让该目录成为独立模块，`go:embed` 和工作区都无法包含它。
- `go.sum` 记录所固定模块的校验和。生成方法：在仓库外复制一份模板，把其中的 `go.mod.tmpl` 变成 `go.mod`，然后在那里运行 `GOWORK=off go mod tidy`。
- `game.yaml` 是规范格式（`go run github.com/nsgo-dev/game/packages/game/cmd/gameproject check` 会检查），`nsgo.yaml` 与它一致；`game new` 会替换两者的 `name` 和 `id`。
- 包含 `main.go`、`README.md`、`.gitignore` 和测试，并且在 `packages/game` 中运行 `go run ./cmd/game index ../../template` 无事可做。
- 模板作者编写的每个文件都是文本，与本仓库中的任何项目一样。

缺少上述任何一步时，`packages/game/cmd/game` 的测试都会失败：`_templates/projects` 下的副本与模块的 `template` 目录有任何一个字节不同；某个 `template/*/template` 目录没有注册；某条注册没有副本、模块或 `go.work` 条目；本页或其英文版中没有写明模板及其模块的那一行。测试还会用每个模板创建项目，用本地游戏层构建并运行它的测试。

新增模板无需修改 CI：`templates` 任务列出 `template/template` 和每个 `template/*/template` 目录，并以开发者拿到项目的方式测试每个模板：把模板复制到工作区之外，为 `example.com/<name>game` 渲染 `go.mod`，并在 `GOWORK=off` 下运行 `go tool nsgo test ./...`，因此项目依赖的是 `go.mod.tmpl` 固定的游戏层版本，而不是当前检出的代码。如果模板需要尚未发布的包，等该包合入 `main` 后再提升这个固定版本。
