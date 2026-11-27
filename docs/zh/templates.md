# 项目模板

新项目从一个模板开始：模板是一个小而完整的游戏，在你改动任何一行之前就能构建、运行并通过测试。`game new` 把模板写入一个新目录，NSGo 自带的 `nsgo init -template` 则从模板的 Go 模块写出同样的文件。

## 现有模板

| 模板 | 内容 | `nsgo init -template` 模块 |
| --- | --- | --- |
| `starter` | 一个点击得分的 2D 游戏：固定步长循环上的动画精灵、原生 HUD 和无头测试，是最小的起步项目。默认模板。 | `github.com/nsgo-dev/game/template` |
| `action3d` | 第三人称 3D 动作冒险：环绕相机、由状态机混合的奔跑、跳跃和攻击动画、近战敌人，以及打开通往目标的大门的钥匙（[说明](template-action3d.md)）。 | `github.com/nsgo-dev/game/template/action3d` |
| `cards2d` | [Suit Ladders](template-cards2d.md)，一个单人纸牌游戏：在抽牌堆、手牌、弃牌堆和四座梯子之间拖放卡牌，规则写在代码里，牌组和牌桌写在 YAML 里，带编号的发牌和存档。 | `github.com/nsgo-dev/game/template/cards2d` |
| `fighting2d` | 一对一的 2D 格斗游戏：两名原创角色，有搓招触发的必杀技、连段、上下段防御、回合和计时，可对战电脑或第二名玩家，用键盘、手柄或触屏操作。详见[格斗游戏模板](template-fighting2d.md)。 | `github.com/nsgo-dev/game/template/fighting2d` |
| `fps3d` | 一个 3D 第一人称射击竞技场：即时命中和弹体两种武器，用 `ai` 和 `physics3d` 实现会巡逻、搜寻和射击的敌人，生命和弹药拾取物，以及三波要清除的敌人，可用键盘鼠标、游戏手柄或触屏游玩。详见 [fps3d 模板](template-fps3d.md)。 | `github.com/nsgo-dev/game/template/fps3d` |
| `idle` | 一个放置游戏：生产器、升级、大数、回来时结算离线进度和转生，有胜有负；详见[放置游戏模板](template-idle.md)。 | `github.com/nsgo-dev/game/template/idle` |
| `platformer2d` | 一个横版平台跳跃游戏（[说明](template-platformer2d.md)）：带土狼时间和跳跃缓冲的控制器、三个各有相机边界的房间、尖刺、深坑、可收集的火花、一个检查点和一个终点，可用键盘、手柄或触屏操作。 | `github.com/nsgo-dev/game/template/platformer2d` |
| `puzzle2d` | Spark Slide，一个 2D 网格解谜游戏：让火花在棋盘上滑行、点亮每一盏灯，关卡用 YAML 定义，支持撤销和重做、步数上限、每关星级，以及保存进度的选关界面；键盘、手柄、触屏和鼠标都能玩。 | `github.com/nsgo-dev/game/template/puzzle2d` |
| `racing3d` | 一个 3D 赛车游戏：在封闭赛道上与沿赛车线行驶的对手比赛两圈，包含射线车辆、检查点、倒计时、最佳圈幽灵车和结算画面，可用键盘、手柄或触屏操作。[3D 赛车模板](template-racing3d.md) | `github.com/nsgo-dev/game/template/racing3d` |
| `rhythm` | Pulse Lanes，一个四轨道节奏游戏：谱面用 YAML 写在节拍图上，音乐时钟让音符与音乐保持同步，有 Perfect、Great、Good 和 Miss 判定、连击、分数、评级，以及决定通关或失败的能量条，音频和画面延迟校准按设备保存，最佳成绩也会保存；键盘、手柄、触屏和鼠标都能玩。 | `github.com/nsgo-dev/game/template/rhythm` |
| `roguelike2d` | 一个回合制 roguelike（[指南](template-roguelike2d.md)）：五层由本局种子生成的 BSP 房间与洞穴、撞击即攻击的战斗、带保底的加权掉落、带装备与限时药剂的属性、按回合冷却的饮药与横扫两个技能、永久死亡和本局总结，可用键盘、手柄或触屏游玩。 | `github.com/nsgo-dev/game/template/roguelike2d` |
| `rpg2d` | 一个俯视角 RPG：可以行走的小镇和草地、带分支对话的人物、带装备的物品栏、一个任务、回合制战斗和三个存档槽。[指南](template-rpg2d.md)。 | `github.com/nsgo-dev/game/template/rpg2d` |
| `rts2d` | 一场 2D 即时战略遭遇战：采集水晶、建造兵营、训练三种单位并摧毁电脑对手的总部，带战争迷雾、框选、编队以及键盘、手柄和触屏操作（[指南](template-rts2d.md)）。 | `github.com/nsgo-dev/game/template/rts2d` |
| `shmup2d` | 一个纵向卷轴射击游戏：YAML 写的敌机波次和关卡、弹幕、强化道具、炸弹、三个阶段的 Boss、残机和分数，可用键盘、鼠标、手柄或触屏操作。 | `github.com/nsgo-dev/game/template/shmup2d` |
| `towerdefense2d` | 一个 2D 塔防游戏：在敌人沿着行进的道路旁建造可升级的防御塔，波次写在 YAML 里，有金钱和生命，可用键盘、手柄或触摸操作；[塔防模板](template-towerdefense2d.md)介绍了它。 | `github.com/nsgo-dev/game/template/towerdefense2d` |
| `visualnovel` | 一部分支短篇视觉小说：两个角色、代码绘制的背景、改变结局的选项、对话记录、自动与快进模式、存档槽和两种语言。 | `github.com/nsgo-dev/game/template/visualnovel` |

`go run github.com/nsgo-dev/game/cmd/game@latest new -list` 由命令自身打印同样的列表，默认模板排在最前。

### puzzle2d

火花一直滑行到被墙挡住为止，并点亮途经的每一盏灯；在步数用完之前点亮一关的所有灯即过关，滑进虚空或步数用完时仍有灯未亮则失败。由于 HUD、棋盘和底部操作栏纵向堆叠，根作用域在手机上请求竖屏方向（`cx.RequestOrientation(core.OrientationPortrait)`）。这个模板展示解谜游戏如何成长：规则在 `board.go` 中，每一步都是 `undo.History` 的一个操作，可以精确地撤销和重做；关卡在 `levels.yaml` 中，这是一个带版本号、拒绝未知字段并有规范形式的 YAML 文件，由 `levels.go` 读写；输入映射在 `game.yaml` 中，绑定到 `input.Actions`，滑动手势来自 `input.GestureRecognizer`；移动由固定步长 `game.Loop` 上的 `tween` 播放动画；每关的星级用 `save` 保存。要修改游戏，可以在 `levels.yaml` 中增改关卡，测试会检查每一关都能在标准步数内完成；运行 `go tool game playtest` 可通过 `playtest.yaml` 与 `playtest_test.go` 验证可解性、难度顺序、意外捷径和超出标准步数；在 `game.yaml` 中重新绑定操作；在 `board.go` 中添加新的格子类型并在 `draw.go` 中绘制它。模板的 README 列出了全部操作、关卡格式和每个文件。

### rhythm

音符随着歌曲的节拍沿四条轨道落下，玩家在音符到达判定线时敲击对应的轨道；命中会累积连击并充满能量条，漏掉则消耗能量，在能量耗尽之前坚持到歌曲结束即通关，能量耗尽则失败。由于四条纵向轨道和堆叠的菜单需要竖屏高度，根作用域在手机上请求竖屏方向（`cx.RequestOrientation(core.OrientationPortrait)`）。这个模板展示节奏游戏如何成长：歌曲列表、判定窗口和能量规则在 `songs.yaml` 中，这是一个带版本号、拒绝未知字段并有规范形式的 YAML 文件，由 `songs.go` 读写，每首歌的速度信息放在旁边的 `rhythm` 节拍图文件中；规则在 `run.go` 中，`Run` 按音轨位置判定命中，不依赖任何时钟，因此测试可以精确地驱动它；音乐通过 `audio.Player` 播放，并由 `rhythm.Clock` 跟随，比歌曲短的音轨循环播放时，模板先把播放器报告的位置展开成歌曲位置（`unwrapLoop`），再传给 `rhythm.Clock.Sync`；命中按按键或手指按下的时刻判定，而不是按读取输入的游戏步；延迟校准使用 `rhythm.CalibrationScreen`，与谱面走同一套输入，并由 `rhythm.OffsetStore` 按设备保存；输入映射在 `game.yaml` 中，绑定到 `input.Actions`，每条轨道所在的列还绑定了触屏和鼠标；每张谱面的最佳成绩用 `save` 保存。要修改游戏，可以在 `songs.yaml` 中为新歌制谱，并把它的节拍图和音乐放在 `songs/` 下，测试会检查文件是规范形式、每张谱面都公平；判定窗口和能量也在那里调整；在 `game.yaml` 中重新绑定操作；在 `draw.go` 中修改游戏区域的外观。模板的 README 列出了全部操作、歌曲格式和每个文件。

### shmup2d

`shmup2d` 是一个纵向射击游戏的一关，场地为 270×480 的竖屏。敌机波次沿俯冲、摆动、横穿和悬停路径飞入，发射 [`bullets`](bullets.md) 弹幕；击毁整波敌机会掉落强化、炸弹或残机道具；关底是一个有三个阶段的 Boss。击败 Boss 即胜利，失去最后一条命即失败。可用方向键或 WASD、手柄的摇杆、十字键和按键操作，也可以用手指或鼠标拖动，触屏上另有一个炸弹按钮。

- `data/stage.yaml` 是关卡：自机的速度、残机、炸弹和武器，敌机种类及其路径和弹幕，各波次的时间、位置和掉落，以及 Boss 和它的各阶段。格式由 `stage.go` 定义，带版本号、拒绝未知字段、编码规范，测试和模糊测试都会检查。
- `data/patterns.yaml` 以 `bullets` 格式保存全部弹幕，`data/explosion.yaml` 以 [`particles`](particles.md) 格式定义爆炸。
- `world.go` 是一局关卡：自机、敌机、波次、道具和 Boss，以及推进它们的各个系统；它接收每一步的操作，并报告需要播放音效的事件。`controls.go` 把输入映射和拖动转换成这些操作，`game.go` 和 `hud.go` 负责循环、标题、游戏和结束画面以及原生 HUD。
- 精灵在代码中绘制，音效来自[占位资源包](placeholder.md)，`CREDITS.md` 列出了它们的来源。
- 测试通过 `nsgotest` 游玩整关：会躲避和射击的自动驾驶用手柄通关，什么都不做的自机则在键盘操作下被击落。其他测试覆盖触屏、鼠标、暂停、关卡的每个系统和数据文件。`playtest.yaml` 和 `playtest_test.go` 接入了 `shooter` 机器人配置，运行 `go tool game playtest` 即可按种子无头试玩关卡并输出通关率、各波受击数和道具拾取率。

模板的 `README.md` 列出了操作方式，以及修改关卡、弹幕、路径、道具、美术和音效的位置。

### visualnovel

《提灯夜渡》（*Lantern Crossing*）：一位制图师要在天亮前把地图带过雾中的河，摆渡人决定对她信任到什么程度。前两个选择赢得或失去他的信任，第三个选择在河流分岔处决定是抵达港口还是迷失雾中，因此故事有一个好结局和一个迷失结局。

它建立在以下包之上：[`vn`](vn.md) 负责呈现剧本——带表情的立绘、带名牌和打字机效果的文本框、选项、对话记录，以及自动与快进模式；[`flow`](flow.md) 的对话运行器播放 [Yarn Spinner](https://www.yarnspinner.dev/) 剧本并保存其变量；[`save`](save.md) 提供三个存档槽和玩家设置；[`l10n`](l10n.md) 提供英文和简体中文字符串表。背景和立绘都用代码绘制，因此不需要图片文件。键盘、手柄和触屏都能玩完整个游戏，按键来自 `game.yaml` 中的输入映射。

| 文件 | 内容 |
| --- | --- |
| `story/story.yarn` | 故事本身。说话人写角色 id，每句台词和每个选项都有 `#line:` id，`<<bg name>>` 切换背景，`<<ending name>>` 指明结局，每个节点的 `chapter:` 头指明存档槽显示的章节。 |
| `story/cast.yaml` | 角色：名牌颜色、站位和表情，采用 `vn` 的角色表格式。 |
| `strings/en.yaml`、`strings/zh-Hans.yaml` | 各语言的界面字符串，以及角色名、章节名和结局。英文台词就是剧本自身的文字；其他语言以 `line.<id>` 翻译每一句。 |
| `story.go` | 加载故事文件，并以玩家选择的语言显示台词和名牌。 |
| `novel.go` | 游戏本身：标题画面、故事及其菜单、输入、存档槽和设置。 |
| `ui.go`、`stage.go` | 舞台上方的按钮和菜单，以及代码绘制的背景和立绘。 |
| `story_test.go`、`game_test.go` | 检查故事文件彼此一致（每句都有翻译、每个背景和结局都存在、每条路径都以结局收尾），并分别用键盘、手柄和触屏玩到两个结局，途中存档和读档。 |

存档槽保存从开头起做出的选择、当前台词的 id，以及这句台词自上一次选择以来出现过几次，读档时重放这条路径，因为对话运行器保存的状态只有变量和访问过的节点，不包括停在节点中的哪个位置。覆盖已有存档前会先询问。已读台词和已发现的结局属于玩家设置，与存档槽分开保存，并在应用退到后台或退出时再保存一次，因此快进模式在每一轮游戏中都认得读过的台词。根作用域在手机上请求横屏方向（`cx.RequestOrientation(core.OrientationLandscape)`），使两位角色的立绘并排立在文本框上方；文本框会避开手机的刘海和主屏幕指示条，对话记录的“较早”和“较新”按钮让触屏和鼠标也能滚动它。

要把模板改成自己的游戏，在 `story/story.yarn` 中写故事并运行测试，测试会指出每一句缺少翻译的台词，以及游戏不认识的背景、结局或章节；在 `story/cast.yaml` 中添加角色并在 `stage.go` 中为其设定外观，在 `stage.go` 的 `backgrounds` 中添加背景，在 `story.go` 的 `endings` 中添加结局，并以一张字符串表加上 `story.go` 中的登记添加一种语言。模板的 `README.md` 列出了操作方式。

## 用模板创建项目

```sh
go run github.com/nsgo-dev/game/cmd/game@latest new -list
go run github.com/nsgo-dev/game/cmd/game@latest new -template starter mygame
```

在创建好的项目内，使用 `go tool game <command>` 运行各个子命令（见[命令行](cli.md#安装)）。

不加 `-template` 时，`game new` 使用 `starter`。不在列表中的名称会被拒绝，并列出现有的模板名。`game` 的 shell 补全（见[命令行](cli.md#completion)）会用同样的名称补全 `-template`。其他参数见[命令行](cli.md#new)。

NSGo 自带的命令不需要 `game` 工具，也能从模板的模块写出项目，这需要支持 [Go 模块模板](https://nsgo.dev/zh/docs/getting-started/#来自-go-模块的模板)的 NSGo 命令，例如安装好的 v0.3.4 或更新版本的 `nsgo`（[命令行](cli.md#安装)）：

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
      ├─ .gitattributes
      └─ .gitignore
internal/gamecli/
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
3. 在 `internal/gamecli` 中把项目复制进 `game` 命令包：`cp -R ../../template/<name>/template _templates/projects/<name>`。
4. 在 `internal/gamecli/templates.go` 的 `projectTemplates` 中加一条：`newProjectTemplate("<name>", templateModuleRoot+"/<name>", "<一行英文，说明这是什么游戏>")`。
5. 在 `docs/en/templates.md` 和 `docs/zh/templates.md` 上面的表格中为模板各加一行，并在 `changelog.d/` 下加一个片段。

`template/<name>/template/` 中的项目遵循以下规则，测试会尽可能检查它们：

- `go.mod.tmpl` 以 `module {{.Module}}` 开头，依赖一个已发布的、包含模板所用全部包的 `github.com/nsgo-dev/game` 版本，并带有 `tool github.com/nsgo-dev/nsgo/cmd/nsgo`。它是唯一含模板动作的文件：`game new` 和 `nsgo init` 也会渲染 `nsgo.yaml`、`main.go` 和其他 `.tmpl` 文件，但 CI 只渲染 `go.mod.tmpl`，模板还必须在工作区中原样构建，所以这些文件渲染后必须与原文相同，`main.go` 中也不能出现 `{{`（例如 `[]T{{...}}` 这样的复合字面量），这类代码放到其他 Go 文件中。其余文件按原样复制，内容不限。名为 `go.mod` 的文件会让该目录成为独立模块，`go:embed` 和工作区都无法包含它。
- `go.sum` 记录所固定模块的校验和。生成方法：在仓库外复制一份模板，把其中的 `go.mod.tmpl` 变成 `go.mod`，然后在那里运行 `GOWORK=off go mod tidy`。
- `game.yaml` 是规范格式（`go run github.com/nsgo-dev/game/cmd/gameproject check` 会检查），并声明全部六个主要平台的导出预设（`macos_arm64`、`windows_x64`、`linux_x64` 与 `linux_arm64`、面向真机的 `ios`——`ios_simulator` 留在 `dev` 中使用——`android_arm64` 和 `web`）。`nsgo.yaml` 与它一致，且 `build.targets` 仅保留所有操作系统都能构建的跨宿主目标（`windows_x64`、`linux_x64`、`linux_arm64`、`android_arm64`、`web`），使普通 `go tool nsgo build` 在 Linux 和 Windows 上也能直接成功，而 `go tool game release` 构建当前宿主支持的所有预设；`game new` 会替换两者的 `name` 和 `id`。
- 包含 `main.go`、`README.md`、`.gitattributes`、`.gitignore` 和测试，并且在仓库根目录运行 `go run ./cmd/game index template` 无事可做。`.gitattributes` 设置 `* text=auto eol=lf` 使文本文件在不同操作系统下都保持 LF（即使 Windows 版 Git 默认开启 `core.autocrlf=true`），随后将二进制媒体扩展名标记为 `binary`，并把 `*.scene.yaml` / `*.scene.yml` 指向 `merge=nsgo-scene`；`.gitignore` 忽略 `.nsgo/`、`.game/` 和 `dist/`。
- 模板作者编写的每个文件都是文本，与本仓库中的任何项目一样。

缺少上述任何一步时，`internal/gamecli` 的测试都会失败：`_templates/projects` 下的副本与模块的 `template` 目录有任何一个字节不同；某个 `template/*/template` 目录没有注册；某条注册没有副本、模块或 `go.work` 条目；某个模板的 `.gitattributes` 或 `.gitignore` 缺少必需规则；本页或其英文版中没有写明模板及其模块的那一行。测试还会用每个模板创建项目，用本地游戏层构建并运行它的测试。

新增模板无需修改 CI：`templates` 任务列出 `template/template` 和每个 `template/*/template` 目录，并以开发者拿到项目的方式测试每个模板：把模板复制到工作区之外，为 `example.com/<name>game` 渲染 `go.mod`，并在 `GOWORK=off` 下运行 `go tool nsgo test ./...`，因此项目依赖的是 `go.mod.tmpl` 固定的游戏层版本，而不是当前检出的代码。Windows CI 任务（`templates-windows`）还会在开启 `core.autocrlf=true` 的情况下克隆模板并运行其测试，以验证 `.gitattributes` 的 LF 规则能让规范 YAML 检查持续通过。如果模板需要尚未发布的包，等该包合入 `main` 后再提升这个固定版本。
