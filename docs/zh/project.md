# 项目文件

`github.com/nsgo-dev/game/packages/game/project` 负责读取、检查、写入并应用 `game.yaml`。它是游戏根目录下唯一保存项目设置的文件：游戏名称、标识与版本，窗口与虚拟分辨率，输入映射，音频总线，启动时加载的场景与资源，各平台的导出预设，它启用的编辑器插件，以及 `game assets` 构建的资源目标。游戏在启动时读取它，`gameproject` 命令检查并应用它，编辑器打开、编辑并保存它，`game release` 读取其中的导出预设，`game assets` 读取其中的资源目标。

## 文件内容

```yaml
version: 1
name: Cave Run
id: dev.nsgo.caverun
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
        - kind: touchRegion
          region: {"y": 116, width: 72, height: 64}
        - kind: mouseRegion
          button: left
          region: {"y": 116, width: 72, height: 64}
    - name: right
      bindings:
        - kind: touchRegion
          region: {x: 72, "y": 116, width: 72, height: 64}
    - name: fire
      bindings:
        - kind: mouseButton
          button: right
  axes:
    - name: move
      negative: left
      positive: right
  motionAxes:
    - name: steer
      axis: tiltX
      deadZone: 0.1
audio:
  master: 0.9
  buses:
    - name: music
      volume: 0.8
      effects:
        - lowPass: {cutoffHz: 1200, q: 0.7}
        - reverb: {roomSize: 0.5, damping: 0.4, wet: 0.25}
    - name: sfx
      effects:
        - delay: {time: 250ms, feedback: 0.3, wet: 0.2}
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
| `window` | 主窗口内容区的初始尺寸（`width`、`height`，单位为点，必填），可调整的尺寸范围（`minWidth`、`minHeight`、`maxWidth`、`maxHeight`，0 表示不限制），`resizable`（省略时为 true），可选的 `title`，以及 `appearance`（`light` 或 `dark`，省略时跟随系统）。 |
| `display` | `screen.Viewport` 映射到窗口上的虚拟分辨率：`width`、`height`、`mode`（省略时为 `fit`，也可为 `fill`、`stretch` 或 `expand`）、`integerScale`、`pixelSnap`，以及黑边的 CSS 颜色 `barColor`。省略时游戏按窗口尺寸绘制。 |
| `input` | 具名的 `actions`，每个动作带有 `bindings`，类型为 `touchRegion`（需要 `region`）、`mouseButton`（需要 `button`）或 `mouseRegion`（两者都要）；`button` 取 `left`、`right`、`middle`、`back` 或 `forward`，区域使用虚拟坐标。`axes` 在 `negative` 动作按下时读数为 -1，在 `positive` 动作按下时为 +1；`motionAxes` 读取 `tiltX`、`tiltY`、`tiltZ`、`gyroX`、`gyroY` 或 `gyroZ`，选项与 `input.MotionAxisOptions` 的字段相同；运动轴可以与某个轴同名，`Actions.Axis` 会把两者相加。没有绑定的动作只做声明，供代码绑定或供轴读取。 |
| `audio` | 默认的 `master` 音量与各条 `buses`。每条总线是 `sound.Mixer` 的一个分组，带默认 `volume` 和按顺序应用的 DSP `effects`：`lowPass` 与 `highPass`（`cutoffHz`、`q`）、`delay`（`time`、`feedback`、`wet`）、`reverb`（`roomSize`、`damping`、`wet`）、`gain`（`factor`）和 `bitcrush`（`bits`、`downsample`）。 |
| `autoload` | 启动时加载的 `scenes`（场景格式的 YAML 文件）和其他 `resources`，每项都有唯一的 `name` 和项目内以斜杠分隔的 `path`；`mainScene` 指定游戏启动时进入的场景。 |
| `export` | 构建输出目录 `output`，以及每个平台一个预设（`macos`、`windows`、`linux`、`ios`、`tvos`、`watchos`、`android`、`harmonyos`、`web`、`miniprogram`），每个预设列出该平台要构建的 NSGo `targets`；不列目标的预设直接构建平台名，由 NSGo 展开为它的默认目标。 |
| `editor` | 场景编辑器的设置：`plugins` 列出它为本项目启用的[编辑器插件](editor.md#编辑器插件492)，顺序即插件面板和工具出现的顺序。插件是编译进编辑器程序的 Go 代码，这个列表只负责启用它们。 |
| `assets` | `game assets` 构建的目标，每个列表都可省略：从一个 PNG 图片目录打包的 `atlases`（`name`、`in`、`out` 以及 `padding`、`extrude`、`maxWidth`、`maxHeight`、`scale`、`trim`），编码为 KTX2 的 `textures`（`in`、`out`、`format` 取 `rgba8`、`bc1`、`bc3` 或 `etc2`、`mipmaps`、`srgb`），从 WAV 转换的 `audio`（`in`、`out`、`format` 取 `wav`、`aac` 或 `mp3`、`rate`、`channels`、`peakDB` 或 `rmsDB`、`bitrate`），最后生成的 `manifest`（`in`、`out`、`package`、`prefix`、`embed`、`embedDir`），以及构建缓存路径 `cache`，省略时为 `.game/assets-cache.yaml`。路径以斜杠分隔且位于项目内。各设置的作用见[命令行](cli.md#assets)。 |

`Parse` 会拒绝未知字段、其他版本、多余的 YAML 文档、重复名称、轴读取了却未声明的动作、未被自动加载的主场景、跳出项目目录的路径、属于其他平台的目标、越界的值，以及写入同一文件或写入其他目标扫描目录的资源目标，并在错误里指明出错的字段。`Encode` 先校验 `File`，再按固定字段顺序输出 2 空格缩进的 YAML，`Parse` 读回后内容不变。手写时可以使用任意 YAML 风格并加注释；`gameproject fmt` 会把文件改写为规范形式，规范形式不保留注释。

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
- `ConfigureMixer(mixer)` 为 `sound.Mixer` 设置默认主音量、各总线的默认音量（`SetDefaultMaster`、`SetDefaultVolume`）和 DSP 链。玩家设置过并由混音器保存在存储中的音量优先于默认值。没有效果的总线保留游戏在代码中设置的效果链，就像没有绑定的动作保留其绑定一样。
- `LoadAutoloads(fsys)` 从以项目根目录为根的 `fs.FS`（例如 `embed.FS`）读取每个自动加载的场景和资源，场景会按场景文档解析并校验；遇到第一个缺失或无效的文件即返回错误。`Loaded.Main()` 是主场景。

## game.yaml 与 nsgo.yaml

NSGo 自己的 `nsgo.yaml` 与 `game.yaml` 放在同一目录，其中部分设置与 `game.yaml` 重复。这些设置由 `game.yaml` 负责，`nsgo.yaml` 跟随：

| nsgo.yaml | game.yaml |
| --- | --- |
| `name`、`id`、`version` | `name`、`id`、`appVersion` |
| `appearance` | `window.appearance` |
| `build.output`、`build.targets` | `export.output`、按顺序排列的各预设目标 |

`SyncNSGo` 把这些字段写入 `nsgo.yaml`，保留它的其他字段（`dev`、`icon` 等）和注释；`CheckNSGo` 报告每个不一致的字段。两者都与 NSGo 一样，把空的 `build:` 字段视为没有构建设置。`nsgo.yaml` 的其余内容仍由 NSGo 负责。

## gameproject 命令

```sh
go run github.com/nsgo-dev/game/packages/game/cmd/gameproject check [dir]
go run github.com/nsgo-dev/game/packages/game/cmd/gameproject fmt [-check] [dir]
go run github.com/nsgo-dev/game/packages/game/cmd/gameproject sync [-check] [dir]
go run github.com/nsgo-dev/game/packages/game/cmd/gameproject targets [dir]
```

- `check` 一次报告项目的全部问题（`project.Check`）：无法解析的 `game.yaml`、与它不一致的 `nsgo.yaml`，以及缺失或无效的自动加载文件。
- `fmt` 把 `game.yaml` 改写为规范形式；加 `-check` 时不改写，文件不规范则报错。
- `sync` 把 `game.yaml` 负责的字段写入 `nsgo.yaml`，文件不存在时会创建；加 `-check` 时不改写，两者不一致则报错。修改 `game.yaml` 后运行它。
- `targets` 以逗号分隔输出各导出预设的构建目标，可直接传给 `nsgo build -target`。

`dir` 是项目根目录，省略时为当前目录。

## 在编辑器中

`Editor.OpenProject(path)` 打开一个 `game.yaml` 或包含它的目录，并打开其中指定的主场景，同时清空撤销历史。`EditProject(label, edit)` 修改项目的副本，校验通过后才采用，并作为一个可撤销的步骤。`SaveProject()` 以规范形式写入项目，并同步旁边的 `nsgo.yaml`。工具栏的“Save”按钮在项目被编辑过时会先于场景保存项目，即使场景没有可保存到的文件；未编辑的 `game.yaml` 保持原样，注释也会保留。`SetPluginEnabled(name, on)` 以同样的可撤销步骤把插件加入 `editor.plugins` 或从中移除。在命令行中运行 `gameeditor <dir>` 即可打开一个项目。

## 起手模板与示例

起手模板和四个示例游戏（`caverun`、`cryptquest`、`slidegem`、`starburst`）都把设置放在 `game.yaml` 中：每个项目都有窗口设置，用到时还包括虚拟分辨率、输入映射、音频总线和导出预设。Cave Run 还通过它自动加载关卡。每个项目都有一个测试，检查 `game.yaml` 是规范形式，并且 `nsgo.yaml` 与它一致。
