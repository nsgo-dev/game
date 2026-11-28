# 项目文件

`github.com/nsgo-dev/game/pkg/project` 负责读取、检查、写入并应用 `game.yaml`。它是游戏根目录下唯一保存项目设置的文件：游戏名称、标识与版本，窗口与虚拟分辨率，输入映射，音频总线，启动时加载的场景与资源，各平台的导出预设，它启用的编辑器插件、`game assets` 构建的资源目标，以及安装到项目中的库包。游戏在启动时读取它，[`game project`](cli.md#project) 命令检查并应用它，编辑器打开、编辑并保存它，`game release` 读取其中的导出预设，`game assets` 读取其中的资源目标。

## 文件内容

```yaml
version: 1
name: Cave Run
id: dev.nsgo.game.examples.caverun
appVersion: 0.1.0
window:
  width: 960
  height: 540
  minWidth: 480
  minHeight: 270
  appearance: dark
display:
  width: 320
  height: 180
  mode: fit
  integerScale: true
  pixelSnap: true
  barColor: '#0b0d17'
input:
  actions:
    - name: left
      bindings:
        - kind: key
          key: left
        - kind: keyCode
          code: KeyA
        - kind: gamepadButton
          button: dpadLeft
        - kind: touchRegion
          region: {"y": 116, width: 72, height: 64}
        - kind: mouseRegion
          button: left
          region: {"y": 116, width: 72, height: 64}
    - name: right
      bindings:
        - kind: key
          key: right
        - kind: keyCode
          code: KeyD
        - kind: gamepadButton
          button: dpadRight
        - kind: touchRegion
          region: {x: 72, "y": 116, width: 72, height: 64}
    - name: fire
      bindings:
        - kind: mouseButton
          button: right
        - kind: gamepadButton
          button: south
  axes:
    - name: move
      negative: left
      positive: right
  motionAxes:
    - name: steer
      axis: tiltX
      deadZone: 0.1
  gamepadAxes:
    - name: move
      axis: leftX
      deadZone: 0.15
audio:
  master: 0.9
  maxVoices: 28
  buses:
    - name: music
      volume: 0.8
      effects:
        - lowPass: {cutoffHz: 1200, q: 0.7}
        - reverb: {roomSize: 0.5, damping: 0.4, wet: 0.25}
    - name: sfx
      effects:
        - delay: {time: 250ms, feedback: 0.3, wet: 0.2}
  concurrency:
    - name: impacts
      max: 4
      resolve: quietest
      scope: global
      retrigger: 40ms
      stealFade: 30ms
      duck: {amount: 0.25, attack: 20ms, release: 150ms}
  duck:
    - bus: music
      by: dialogue
      amount: 0.4
      attack: 100ms
      release: 500ms
physics:
  gravity2D:
    "y": 980
  gravity3D:
    "y": -9.81
  solverIterations2D: 8
  solverIterations3D: 8
  layers:
    - name: player
      bit: 0
    - name: walls
      bit: 1
    - name: coins
      bit: 2
autoload:
  mainScene: title
  scenes:
    - name: title
      path: scenes/title.yaml
  resources:
    - name: level1
      path: assets/level1.tmj
export:
  output: dist
  presets:
    - platform: macos
    - platform: windows
      targets: [windows_x64]
    - platform: web
editor:
  plugins: [health]
assets:
  atlases:
    - name: ui
      in: art/ui
      out: assets/atlases
  textures:
    - in: art/albedo.png
      out: assets/albedo.ktx2
      format: bc1
      mipmaps: true
  audio:
    - in: sfx/jump.wav
      out: assets/jump.wav
      rate: 22050
      peakDB: -1
  manifest:
    in: assets
    out: assets/manifest_gen.go
```

| 字段 | 内容 |
| --- | --- |
| `version` | 模式版本，固定为 `1`。 |
| `name`、`id`、`appVersion` | 游戏的显示名称、反向域名形式的应用标识和发布版本。 |
| `origin` | 项目复制自何处：`kind`（`game new` 内置的模板为 `template`，此外还有 `example`、`directory` 或 `pack`）、`source`（模板的 Go 模块，或被复制的目录），以及模板的 `path`，即模板名称，例如 `cards2d`。`keep` 列出 `game check` 在有意原样复制时不再报告的[模板残留警告](cli.md#check)：`rules`、`data`、`name`、`readme` 和 `credits`；其他值会被拒绝，并指出所在行。 |
| `window` | 主窗口内容区的初始尺寸（`width`、`height`，单位为点，必填），可调整的尺寸范围（`minWidth`、`minHeight`、`maxWidth`、`maxHeight`，0 表示不限制），`resizable`（省略时为 true），可选的 `title`，以及 `appearance`（`light` 或 `dark`，省略时跟随系统）。 |
| `display` | `screen.Viewport` 映射到窗口上的虚拟分辨率：`width`、`height`、`mode`（省略时为 `fit`，也可为 `fill`、`stretch` 或 `expand`）、`integerScale`、`pixelSnap`，以及黑边的 CSS 颜色 `barColor`。省略时游戏按窗口尺寸绘制。 |
| `input` | 具名的 `actions`，每个动作带有 `bindings`，类型为 `touchRegion`（需要 `region`）、`mouseButton`（需要鼠标 `button`）、`mouseRegion`（两者都要）、`key`（跟随键盘布局的键名 `key`，如 `space`、`left`）、`keyCode`（物理键位 `code`，如 `KeyW`、`ArrowLeft`）或 `gamepadButton`（W3C 标准手柄按键 `button`：`south`、`east`、`west`、`north`、`leftShoulder`、`rightShoulder`、`leftTrigger`、`rightTrigger`、`back`、`start`、`leftStick`、`rightStick`、`dpadUp`、`dpadDown`、`dpadLeft`、`dpadRight` 或 `home`），以及可选的 `assist` 配置（`toggle`、`mash`、每秒连打频率 `mashRate`，默认 10，设置 `mashRate` 时要求 `mash: true`），声明该动作允许哪些玩家运动辅助，防止菜单动作被意外锁存。鼠标 `button` 取 `left`、`right`、`middle`、`back` 或 `forward`，区域使用虚拟坐标。`axes` 在 `negative` 动作按下时读数为 -1，在 `positive` 动作按下时为 +1；`motionAxes` 读取 `tiltX`、`tiltY`、`tiltZ`、`gyroX`、`gyroY` 或 `gyroZ`，选项与 `input.MotionAxisOptions` 的字段相同；`gamepadAxes` 读取手柄摇杆（`leftX`、`leftY`、`rightX` 或 `rightY`），选项与 `input.GamepadAxisOptions` 的字段相同（`deadZone`、`axial`、`sensitivity`、`invert`）。运动轴或手柄摇杆轴都可以与某个动作轴同名，`Actions.Axis` 会把它们相加并限制在 `[-1, 1]` 内。没有绑定的动作只做声明，供代码绑定或供轴读取。 |
| `audio` | 默认的 `master` 音量、可选的全局发声预算 `maxVoices`（0 使用 `sound.DefaultMaxVoices`）、各条 `buses`（每条总线是 `sound.Mixer` 的一个分组，带默认 `volume` 和按顺序应用的 DSP `effects`：`lowPass` 与 `highPass`（`cutoffHz`、`q`）、`delay`（`time`、`feedback`、`wet`）、`reverb`（`roomSize`、`damping`、`wet`）、`gain`（`factor`）和 `bitcrush`（`bits`、`downsample`））、共享并发分组 `concurrency`（`name`、`max`、`resolve` 取 `refuse`/`oldest`/`farthest`/`quietest`/`lowestPriority`、`scope` 取 `global`/`owner`、`retrigger`、`stealFade` 及组内旧声闪避 `duck` `{amount, attack, release}`），以及总线闪避规则 `duck`（`bus`、`by`、`amount`、`attack`、`release`）。 |
| `physics` | 项目级 2D 与 3D 物理默认设置：`gravity2D`（`x`、`y`）、`gravity3D`（`x`、`y`、`z`）、`solverIterations2D`、`solverIterations3D`、具名碰撞层 `layers`（将人类可读的 `name` 如 `player`、`walls`、`coins` 映射到唯一的位索引 `0..31` / `LayerMask`），以及 `ldtkIntGrid` 规则（将 LDtk IntGrid 标识符/分组名称 `name` 或数值 `value` 映射到 `solid`、`oneWay` 与自定义 `flags` 位；通过 `File.LDtkOptions` 供 `tilemap.LoadLDtkWith` 使用，并由 `project.Resolver` 在 `sceneformat.Build` 加载 `.ldtk` 地图时自动应用）。 |
| `autoload` | 启动时加载的 `scenes`（场景格式的 YAML 文件）和其他 `resources`，每项都有唯一的 `name` 和项目内以斜杠分隔的 `path`；`mainScene` 指定游戏启动时进入的场景。 |
| `export` | 构建输出目录 `output`，以及每个平台一个预设：主要目标（`macos`、`windows`、`linux`、`ios`、`android`、`web`）和实验性目标（`tvos`、`watchos`、`harmonyos`、`miniprogram`；见[平台支持](platforms.md#实验性目标平台)），每个预设列出该平台要构建的 NSGo `targets`；不列目标的预设直接构建平台名，由 NSGo 展开为它的默认目标。`game release` 会为每个构建的实验性平台打印一次警告（`-strict` 将警告转为错误以供 CI 使用），场景编辑器也会为实验性预设显示“实验性”标记而不阻止构建。 |
| `editor` | 场景编辑器的设置：`plugins` 列出它为本项目启用的[编辑器插件](editor.md#编辑器插件492898)，顺序即插件面板和工具出现的顺序。插件是编译进编辑器程序的 Go 代码，这个列表只负责启用它们。 |
| `assets` | `game assets` 构建的目标，每个列表都可省略：从一个 PNG 图片目录打包的 `atlases`（`name`、`in`、`out` 以及 `padding`、`extrude`、`maxWidth`、`maxHeight`、`scale`、`trim`），编码为 KTX2 的 `textures`（`in`、`out`、`format` 取 `rgba8`、`bc1`、`bc3` 或 `etc2`、`mipmaps`、`srgb`），从 WAV 转换的 `audio`（`in`、`out`、`format` 取 `wav`、`aac` 或 `mp3`、`rate`、`channels`、`peakDB` 或 `rmsDB`、`bitrate`），从 TrueType 或 OpenType、或从 PNG 字形图片目录烘焙为 AngelCode BMFont 的 `fonts`（`in`、`out` 为 `.fnt` 文件、`size`、`chars`、`padding`，字形目录另有 `glyphs`、`spacing`、`align`），从 glTF 打包为单个 GLB 文件的 `models`（`in`、`out`、`scale`、`animations`），最后生成的 `manifest`（`in`、`out`、`package`、`prefix`、`embed`、`embedDir`），以及构建缓存路径 `cache`，省略时为 `.game/assets-cache.yaml`。路径以斜杠分隔且位于项目内。图片、声音、字体或模型还可以在旁边的导入设置附属文件（`hero.png.import.yaml`）里带上自己的设置，覆盖其目标的设置；任何资源都可以在其中记录作者和许可证，`game release` 会把它们列入随发布附带的署名文件。各设置的作用见[命令行](cli.md#assets)。 |
| `navigation` | [`game navmesh`](cli.md#navmesh) 烘焙场景[导航网格](navbake3d.md)时使用的设置和代理类型：体素的 `cellSize` 和 `cellHeight`、`minRegionArea`、`maxEdgeLength`、`maxSimplificationError`、`maxVertsPerPoly`（3 到 12）、`detailSampleDistance`（不小于 `cellSize`）和 `detailSampleMaxError`，以及 `agents`，每种代理类型有唯一的 `name` 及其 `radius`、`height`、`maxSlope`（角度，小于 90）和 `maxClimb`。省略的尺寸取 navbake3d 的默认值，没有代理类型时烘焙一种名为 `default` 的代理类型。 |
| `packs` | 从[资源与插件库](library.md)安装的包，每项包含 `name`、`kind`（`asset`、`plugin` 或 `template`）、`packVersion`（不带前导 `v` 的严格语义化版本，如 `1.0.0`）、`sha256` 摘要、写入 `files` 的目录 `dir`，以及安装时新增的自动加载 `scenes`、`resources` 和编辑器 `plugins`，这些条目必须仍在文件中。`game library` 和编辑器的库面板负责写入它；两个包不能占用同一个文件、条目或插件。 |

`Parse` 会拒绝未知字段、其他版本、多余的 YAML 文档、重复名称、轴读取了却未声明的动作、未被自动加载的主场景、跳出项目目录的路径、属于其他平台的目标、越界的值，以及写入同一文件、覆盖 `game.yaml` 或某个目标读取的文件、或写入其他目标扫描目录的资源目标，并在错误里指明出错的字段。`Encode` 先校验 `File`，再按固定字段顺序输出 2 空格缩进的 YAML，`Parse` 读回后内容不变。手写时可以使用任意 YAML 风格并加注释；`game project fmt` 会把文件改写为规范形式，规范形式不保留注释。

## 在游戏中读取

游戏把 `game.yaml` 嵌入程序，并在启动时解析一次：

```go
//go:embed game.yaml
var gameYAML []byte

var gameProject = project.MustParse(gameYAML)

func main() {
	nsgo.Run(Root,
		nsgo.WindowSize(gameProject.WindowSize()),
		nsgo.WindowConstraints(gameProject.WindowConstraints()),
		nsgo.Appearance(gameProject.Appearance()),
	)
}

func Root(cx *core.Scope) core.Node {
	desktop.SetTitle(cx, gameProject.Title())
	// ...
}
```

`nsgo.Run` 没有设置标题的选项，因此由根组件用 `desktop.SetTitle` 设置标题：`Title()` 是 `window.title`，没有时为 `name`。

文件的其余部分应用到游戏自己的对象上：

- `Viewport()` 返回 `display` 对应的 `screen.Viewport`，没有 `display` 时返回 false。
- `BindInput(actions)` 把输入映射绑定到 `input.Actions`，替换同名动作的绑定，保留其他绑定，例如随代码计算出的布局而定的绑定。`Region(action)` 返回绑定到某个动作的第一个区域，游戏在那里绘制屏幕上的控件。
- `ConfigureMixer(mixer)` 为 `sound.Mixer` 设置默认主音量、各总线的默认音量（`SetDefaultMaster`、`SetDefaultVolume`）和 DSP 链。玩家设置过并由混音器保存在存储中的音量优先于默认值。没有效果的总线保留游戏在代码中设置的效果链，就像没有绑定的动作保留其绑定一样。运行时效果链只应用于通过 `Music.Play` 播放或用 `Mixer.LoadEffect` 加载的内存中 16-bit PCM WAV 来源；其他来源不加效果播放，并按分组输出一条日志（见 [sound](sound.md)）。
- `ConfigurePhysicsWorld(world)`、`ConfigureRigidWorld(world)` 与 `ConfigurePhysics3DWorld(world)` 将 `physics.gravity2D`、`physics.gravity3D` 和求解器迭代次数应用到 `physics.World`、`physics.RigidWorld` 与 `physics3d.World`。`PhysicsLayerTable()` 和 `PhysicsSettings()` 将 `physics:` 转换为 `sceneformat.ParseWith` 与 `sceneformat.BuildPhysics` 使用的类型，`Physics.LayerBit(name)`、`Physics.LayerMask2D(names...)`、`Physics.LayerMask3D(names...)` 则便于在 Go 代码中解析具名碰撞层。
- `NewResolver(fsys)` 返回以项目根目录为根的 `fs.FS` 上的 `Resolver`，它从项目自己的文件中加载场景引用的内容：精灵图集、场景文件、材质、物理材质，以及瓦片地图引用的 YAML 图块集和 Tiled、LDtk 地图。它就是 `sceneformat.Build` 和 `sceneformat.BuildPhysics` 接受的 `sceneformat.AssetResolver`、`SceneResolver`、`MaterialResolver`、`PhysicsMaterialResolver` 和 `FileResolver`（`ReadFile`），并为编辑器的资源浏览器列出项目的图集（`AtlasNames`）。场景按 `game check` 的方式指定图集（`NearestAtlases`）：图集 JSON 文件去掉 `.json` 后、从项目根目录算起的路径，或其基本名；有多个匹配时离场景最近的那个胜出（`ForScene` 返回针对某个场景的解析器），图像按其 `meta.image` 从项目中该文件旁边读取。图块集和地图通过 `ReadFile` 读取，与 `sceneformat.FSAssets` 的读取方式相同。图集只列出和加载一次，直到 `Refresh`；场景文件和 `ReadFile` 读取的文件每次请求都重新读取。`AtlasNamed`、`SkipDir` 和 `IsAtlasJSON` 是遍历和按名称查找与 `game check` 共用的规则：哪些名称指向某个图集文件、跳过哪些目录、哪些 JSON 文件是图集。
- `LoadAutoloads(fsys)` 从以项目根目录为根的 `fs.FS`（例如 `embed.FS`）读取每个自动加载的场景和资源，场景会按场景文档解析并校验（包括对照 `physics.layers` 校验具名物理层）；遇到第一个缺失或无效的文件即返回错误。`Loaded.Main()` 是主场景。

## game.yaml 与 nsgo.yaml

NSGo 自己的 `nsgo.yaml` 与 `game.yaml` 放在同一目录，其中部分设置与 `game.yaml` 重复。这些设置由 `game.yaml` 负责，`nsgo.yaml` 跟随：

| nsgo.yaml | game.yaml |
| --- | --- |
| `name`、`id`、`version` | `name`、`id`、`appVersion` |
| `appearance` | `window.appearance` |
| `build.output`、`build.targets` | `export.output`、按顺序排列的各预设中的跨宿主目标（仅含 Apple 目标的项目则保留全部目标） |

`SyncNSGo` 把这些字段写入 `nsgo.yaml`，在存在跨宿主目标时将 `build.targets` 过滤为所有宿主都能构建的目标（省略 Apple 目标和 `android_x64`），使普通 `nsgo build` 在 Linux 和 Windows 上也能成功，同时保留它的其他字段（`dev`、`icon` 等）和注释；`CheckNSGo` 报告每个不一致的字段，对 `build.targets` 同时接受跨宿主子集和完整的预设目标列表。两者都与 NSGo 一样，把空的 `build:` 字段视为没有构建设置。`nsgo.yaml` 的其余内容仍由 NSGo 负责。

## game project 命令

```sh
go tool game project check [dir]
go tool game project [-check] fmt [dir]
go tool game project [-check] sync [dir]
go tool game project targets [dir]
```

- `check` 一次报告项目的全部问题（`project.Check`）：无法解析的 `game.yaml`、与它不一致的 `nsgo.yaml`、缺失或无效的自动加载文件，以及已安装包写入但已缺失的文件。
- `fmt` 把 `game.yaml` 改写为规范形式；加 `-check` 时不改写，文件不规范则报错。
- `sync` 把 `game.yaml` 负责的字段写入 `nsgo.yaml`，文件不存在时会创建；加 `-check` 时不改写，两者不一致则报错。修改 `game.yaml` 后运行它。
- `targets` 以逗号分隔输出各导出预设的构建目标，可直接传给 `nsgo build -target`。

`dir` 是项目根目录，省略时为当前目录。在项目中，`go tool game project` 无需安装 `nsgo` 即可运行同一命令。检查不通过时以 1 退出；调用方式错误时（例如对 `check` 或 `targets` 使用 `-check`，或把 `-check` 写在动词之后）以 2 退出。

## 在编辑器中

`Editor.OpenProject(path)` 打开一个 `game.yaml` 或包含它的目录，并打开其中指定的主场景，同时清空撤销历史。`EditProject(label, edit)` 修改项目的副本，校验通过后才采用，并作为一个可撤销的步骤。`SaveProject()` 以规范形式写入项目，并同步旁边的 `nsgo.yaml`。工具栏的“Save”按钮在项目被编辑过时会先于场景保存项目，即使场景没有可保存到的文件；未编辑的 `game.yaml` 保持原样，注释也会保留。`SetPluginEnabled(name, on)` 以同样的可撤销步骤把插件加入 `editor.plugins` 或从中移除。设置了 `Editor.ProjectAssets` 时，打开项目会把编辑器的解析器换成项目目录上的 `project.Resolver`，使项目的场景用它自己的图集绘制；`OpenProjectScene(path, scene)` 打开项目时用它的另一个场景代替主场景。在命令行中运行 `go tool game editor <dir>` 就是这样打开项目的，项目有自己的编辑器命令时则通过该命令打开（`game generate editor`，见[命令行](cli.md#generate)）。`MoveFile(from, to)` 移动项目中的文件并改写指向它的引用（见下文“稳定引用”）。

## 稳定引用

文件之间通过路径互相引用：`game.yaml` 自动加载场景和资源、从文件和文件夹构建资源，并记录已安装库包的目录（`packs[].dir`）；场景继承或实例化其他场景并引用材质、glTF 模型、Tiled 和 LDtk 地图以及 YAML 图块集，YAML 图块集引用它的图片，材质引用纹理，Tiled 和 LDtk 文件引用各自的图块集和图片（Tiled 图块集可以是 `.tsj` 或 `.json`），`.gltf` 模型引用它存放在单独文件中的缓冲区和图片（按 glTF 的写法做百分号编码），精灵图集在 `meta.image` 中引用它的页面图片。`game/tooling/refs` 包让这些引用在文件改名或移动后依然有效。它通过 `project` 包（`project.PathRefs`）读取 `game.yaml` 中的路径，因此无法通过校验的 `game.yaml` 会被报告，修好之前不会移动任何文件。

每个场景文件和资源文件的文件头里都有一个稳定 ID，即 `uid`：只写入一次、此后不再改变，因此会跟着文件一起移动：

```yaml
version: 2
uid: 4kq0w8m2c7d9x
name: Cave
```

uid 由 13 个字符组成，取自 `0`-`9` 和去掉 `i`、`l`、`o`、`u` 的 `a`-`z`（`resource.NewUID`），`sceneformat` 和 `resource` 会拒绝其他取值；空的或为 null 的 `uid` 视为没有 uid，`game index` 会就地填上。uid 在同一项目内唯一：两个项目（例如由同一模板创建的两个游戏）可以共用同一个 uid，因为它们各有自己的索引。项目索引 `game.index.yaml` 位于 `game.yaml` 旁边，记录工具上一次看到的各文件位置：每个场景文件和资源文件记录路径和 uid，引用指向的其他文件（如图片或 Tiled 地图）记录路径和内容的 SHA-256。索引由工具维护，按路径排序，只有文件变化时才会变化；请把它和它描述的文件一起纳入版本控制。

```yaml
# The stable IDs of this project's files, kept by game index, game mv and the editor. Keep it in version control.
version: 1
files:
  - path: art/stone.png
    sha256: 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08
  - path: materials/stone.material.yaml
    uid: 4kq0w8m2c7d9x
```

- `go tool game mv <from> <to>` 移动或重命名一个文件或目录，改写所有指向被移动内容的引用（包括被移动的 Tiled 或 LDtk 文件中相对自身书写的路径），并更新索引。`-dry-run` 只打印将要做的修改而不实际执行。编辑器中的 `Editor.MoveFile(from, to)` 做同样的事，并同时更新当前打开的场景、项目和动画片段，未保存的编辑也会保留；编辑器文件浏览器中的“Move”按钮调用的就是它。编辑器中的一次移动是一个可撤销的步骤：撤销会把文件移回原处，把移动改写过的每个文件（包括索引）逐字节恢复原样，删除移动时新建的索引，移动时新建的目录如果因此变空也会被删除；重做会再移动一次。移动之后又被改过的文件只把引用改写回去；移动没有碰过的文件（例如原本就指向目标路径的文件）保持不变。如果磁盘上新出现的文件挡住了这一步，这一步保持原样，状态栏会说明原因。
- `go tool game index` 给每个还没有 uid 的场景文件和资源文件加上 uid（写在 `version` 之后的一行）；把指向“未经工具移动的文件”的引用改到文件的新路径；给保留了原文件 uid 的副本换一个新 uid；然后写出索引。首次使用时运行一次，之后每次手动或用 Git 移动文件后再运行。
- `go tool game check` 会对指向已不存在文件的引用报错，给出引用所在的文件和行号；如果索引能重新找到该文件，还会说明它移到了哪里：即现在持有索引中旧路径对应 uid 的场景或资源文件，或者扩展名和内容都相同的唯一另一个文件。

```
scenes/room.scene.yaml:10: material "materials/stone.material.yaml": not found; it moved to "materials/rock.material.yaml"; run game index to update the reference
```

引用是原地改写的：保留原有的引号，文件其余部分（包括注释和格式）逐字节不变，因此一次移动在代码审查中只显示它改动的那几行。会导致引用失效的移动会在做任何修改之前被拒绝：引用会指到项目根目录之外、引用跨多行书写、引用所在的场景无法解析，或在不移动包目录（`packs[].dir`）的情况下单独移动已安装包 `packs[].files` 中记录的文件或子目录。场景中的图集、图块集和预制体名称是由游戏的解析器查找的名称而不是路径，因此不会被改写；对象的 `mesh` 也不会被改写，它指代内置的基本形体；对象的 `modelNode` 和 `modelMesh` 同样不会被改写，它们指代模型文件内部的部件（[scenemodel](scenemodel.md)）。热重载着色器材质（`hotreload.MaterialDef`）中的着色器和纹理路径同样不会被改写，因为它们相对于游戏在运行时交给重载器的资源目录（在 `game dev` 下默认是 `resources`），而不是项目根目录。`game assets` 写出的路径（`assets` 目标的 `out`、`cache` 以及清单的 `in` 文件夹）在移动时会被改写，但可以暂不存在，也不会列入索引；文件夹（包括已安装包的目录）同样不列入。只改变大小写的重命名（例如把 `a.png` 改为 `A.png`）在不区分大小写的文件系统上同样可用。

## 起手模板与示例

起手模板和四个示例游戏（`caverun`、`cryptquest`、`slidegem`、`starburst`）都把设置放在 `game.yaml` 中：每个项目都有窗口设置，用到时还包括虚拟分辨率、输入映射、音频总线和导出预设。Cave Run 还通过它自动加载关卡。每个项目都有一个测试，检查 `game.yaml` 是规范形式，并且 `nsgo.yaml` 与它一致。它们也都建立了索引：Cave Run 的 `game.yaml` 和地图引用了文件，因此带有 `game.index.yaml`；`game/tooling/refs` 中的一个测试会在 `game index` 要对示例或起手模板做任何修改时失败，所以移动或编辑它们引用的文件后，请在仓库根目录运行 `go run ./cmd/game index <dir>`。其余示例和起手模板没有场景文件或资源文件，也不引用任何文件，因此在游戏加入这类文件之前没有需要索引的内容。
# 资源交付

`export.presets[].delivery` 在项目 YAML 中保存平台包布局。省略 delivery 时保持现有单包行为。
