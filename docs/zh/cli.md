# 命令行

`game` 是游戏层的命令行工具：一个命令，由子命令打包精灵图集、转换音频、生成资源清单、编码纹理、打开场景编辑器、构建发布包、无界面回放输入录像、对照存储的基线检查基准测试、对照体积预算检查构建好的包、创建项目、检查、格式化并应用项目的 `game.yaml`、运行游戏并热重载资源、检查游戏的场景、地图和图集，从 `game.yaml` 构建游戏全部资源并只重建有改动的部分，向项目中写入新的组件、系统、场景和预制体，移动文件并保持指向它们的引用有效，维护记录稳定文件 ID 的项目索引，把 pixelfx 特效渲染为精灵表，以及生成 shell 补全脚本。子命令的调用方式一致：参数标志写在位置参数之前，读取的内容用 `-in`，写出的内容用 `-out`，帮助也一样。

## 安装

在项目之外创建新项目时，直接用 `go run` 运行，无需预先安装：

```sh
go run github.com/nsgo-dev/game/cmd/game@latest new mygame
```

在项目内，把该命令添加为 Go 模块工具（用 [`game new`](#new) 创建的项目已经包含这一行），然后通过 `go tool game` 运行各个子命令：

```sh
go get -tool github.com/nsgo-dev/game/cmd/game
go tool game help
```

`go.mod` 里的 `tool` 一行把命令固定在模块所用的游戏层版本上，参与这个游戏的每个人运行的都是同一个命令。每个模板的 `go.mod` 和游戏层本身同时固定 `github.com/nsgo-dev/nsgo` v0.3.4，这是第一个软件渲染器和窗口深度缓冲都能正确绘制 3D 场景的版本。

通过 NSGo 的通用扩展机制以 `nsgo game` 运行命令（以及在项目外使用 `cmd/nsgo-game`）需要高于 v0.3.4 的 NSGo 发布版本；NSGo v0.3.4 内置的 `nsgo game` 命令硬编码了旧的 `github.com/nsgo-dev/game/packages/game` 模块路径，因此在模板升级到更新的 NSGo 发布版本之前，项目外请使用 `go run github.com/nsgo-dev/game/cmd/game@latest`，项目内请使用 `go tool game`。

## 命令

| 命令 | 作用 |
| --- | --- |
| `pack` | 把一个文件夹的 PNG 图片打包成精灵图集页面和供 `sprite.LoadAtlas` 读取的 TexturePacker JSON-hash 元数据 |
| `audio` | 对 WAV 文件做归一化、重采样，并转换为 WAV、AAC 或 MP3 |
| `manifest` | 从资源目录生成带类型的 Go 资源清单，缺少资源时构建失败 |
| `tex` | 把 PNG 或 JPEG 纹理编码为 BC1、BC3、ETC2 或 RGBA8 格式的 KTX2 容器，可选生成 mipmap |
| `editor` | 在场景编辑器中打开一个项目或 YAML 场景，未指定时打开开始界面 |
| `mcp` | 通过标准输入输出向智能体提供编辑器的 MCP 工具，把每次调用转发给打开了该项目的编辑器 |
| `release` | 通过一次 `nsgo build` 为 macOS、Windows、Linux、Web、iOS 和 Android，或为 `game.yaml` 的导出预设，构建游戏的发布包，并附上列出各资源许可证的署名文件 |
| `replay` | 检查各目录下的输入录像，并通过它们的回放测试无界面回放，结果不一致时失败 |
| `bench` | 无界面运行基准测试，有一项退化超过存储的基线时失败，或者更新基线 |
| `size` | 测量 WebAssembly 二进制、Android AAB 或 APK 包或 iOS IPA 包的体积，超出体积预算时失败 |
| `new` | 从模板在一个目录中创建游戏项目，其 `go.mod` 依赖游戏层，并包含 `game` 和 `nsgo` 工具 |
| `project` | 检查项目的 `game.yaml` 及其自动加载文件，把它改写为规范形式，把它负责的字段写入 `nsgo.yaml`，或输出各导出预设的构建目标 |
| `dev` | 用 `nsgo dev` 运行游戏，资源保存后在运行中的游戏里重新加载 |
| `check` | 检查一个目录下的场景、Tiled 和 LDtk 地图、精灵图集以及它们引用的文件，每个问题都报告所在的文件和行 |
| `assets` | 从 `game.yaml` 打包图集、编码纹理、转换音频、烘焙字体、打包模型并生成资源清单，只重建有改动的部分 |
| `generate` | 向游戏项目写入新的组件、系统、场景或预制体：它的 Go 代码和测试、场景或预制体的场景文件，以及它的注册 |
| `mv` | 移动或重命名项目中的文件或目录，改写所有指向它的引用，并保持项目索引最新 |
| `index` | 给每个场景文件和资源文件分配 uid，把引用指向未经工具移动的文件的新位置，并写出项目索引 |
| `library` | 在库中查找资源、编辑器插件和模板包并安装到项目中，更新或移除它们，或打印包的摘要 |
| `credits` | 写出 `release` 随发布附带的署名文件：每个资源的作者和许可证，取自它旁边的导入设置附属文件，有资源缺少署名时失败 |
| `pixelfx` | 把 pixelfx 特效或其表演版本渲染为 PNG 精灵表和供 `sprite.LoadAtlas` 读取的 Aseprite JSON |
| `navmesh` | 把场景的 glTF 关卡几何体为 `game.yaml` 声明的每种代理类型烘焙成导航网格，写入场景的 `navMesh` 指定的文件 |
| `merge` | 为 Git 按对象合并场景 YAML 文件，或在项目中配置与检查 `nsgo-scene` Git 合并驱动 |
| `schema` | 输出项目的场景 JSON Schema（含项目注册的组件与插件），项目没有自己的编辑器命令时输出内置场景 Schema |
| `completion` | 为 zsh、bash 或 fish 生成补全脚本 |
| `help` | 列出所有命令，或显示某个命令的参数 |

`go tool game help` 列出所有命令，`go tool game help <command>` 或 `go tool game <command> -h` 显示一个命令的用法行、作用和参数。[命令参考](cli-reference.md)在一页中列出每个命令的参数及其默认值，由命令定义生成。

命令成功或显示帮助时退出码为 0；执行失败（读不到文件、找不到编码器、清单或资源已过期、构建失败、回放的结束状态不一致、基准测试退化、包超出体积预算、`check` 发现了失效的引用、`generate` 要写的文件已存在、有资源缺少署名）时为 1；调用方式有误（未知参数、缺少必需参数、互相冲突的参数、多余的位置参数）时为 2，并同时打印该命令的用法。错误信息以命令名开头，例如 `game pack: -in directory is required`；未知或格式错误的参数例外，由 Go 的 flag 包用它自己的措辞报告，例如 `flag provided but not defined: -nope`。未知命令同样以 2 退出，但不打印用法，而是提示运行 `game help`。

### pack

```sh
go tool game pack -in art/ui -out assets -name ui -padding 2
```

把 `ui.png` 和 `ui.json` 写到 `-out`（帧放不下一页时改为 `ui-0.png`、`ui-0.json`、`ui-1.png` 等），并逐行打印路径。`-padding` 和 `-extrude` 设置每帧四周的透明间隔和重复的边缘像素，`-max-width` 和 `-max-height` 限制单页尺寸，`-scale` 写入 `meta.scale`，默认裁掉透明边框并记录每帧在未裁剪尺寸中的偏移，`-trim=false` 时保留边框。

`-in` 中的 PNG 旁边有[导入设置附属文件](#资源旁的导入设置)时，它的 `image.slice` 和 `image.pivot` 设置与在 `assets` 中一样生效：按网格切分精灵表，并把每帧的归一化 TexturePacker `pivot` 写入图集 JSON 供 `sprite.LoadAtlas` 读取。

### audio

```sh
go tool game audio -in sfx/jump.wav -out assets/jump.wav -rate 22050 -channels 1 -peak-db -1
go tool game audio -in music/theme.wav -out assets/theme.m4a -format aac -rms-db -14 -bitrate 192
```

读取 WAV 文件，重采样到 `-rate`，混合为 `-channels` 个声道，并按峰值（`-peak-db`）或响度（`-rms-db`，带软削波）归一化，两者只能选一，同时指定属于调用方式有误。WAV 输出由 Go 编写；MP3 需要 `PATH` 上有 `ffmpeg`，AAC 使用 `ffmpeg`，在 macOS 上缺少 `ffmpeg` 时改用 `afconvert`；都没有时报错并指出缺少的编码器。

输入旁边有[导入设置附属文件](#资源旁的导入设置)时（例如 `sfx/jump.wav.import.yaml`），它的 `audio` 设置与在 `assets` 中一样生效，包括循环点；命令行上给出的参数优先于附属文件中的同名设置：`-peak-db` 或 `-rms-db` 会替换附属文件中的电平，不论它是哪一种。循环需要 `wav` 输出，附属文件含有其他种类资源的设置时会被拒绝。

### manifest

```sh
go tool game manifest -resources -out assets/manifest_gen.go -pkg assets
go tool game manifest -in assets -out assets/manifest_gen.go -pkg assets -check
```

为 `-in` 下的每个资源（跳过名字以点开头的文件和文件夹、Go 文件和导入设置附属文件 `*.import.yaml`）写出一个带类型引用的 Go 文件，`-out` 为空时打印到标准输出。`-resources` 默认扫描 `resources/`（未显式传入 `-in` 时）并生成面向应用资源的 `"asset:"` URI 引用而非 `//go:embed`，使资源在各目标平台上按需加载而不增大二进制体积；不能与 `-embed` 或 `-embed-dir` 同时使用。`-embed` 生成 `//go:embed` 指令而不是 `asset:` URI，`-embed-dir` 加在这些指令的模式之前，并会转义通配符元字符（`[`、`*`、`?`），使 `hero[1].png` 等文件名只匹配自身。在 `-embed` 模式下，每个资源路径及 `-embed-dir` 都必须满足 Go `//go:embed` 的文件名规则（仅允许 ASCII 字母、数字、空格、Unicode 字母及 `!#$%&()+,-.=@[]^_{}~`，不能以点结尾，也不能使用 `.git` 等保留版本库目录名或 `CON` 等 Windows 设备名）；包含 `*`、`?`、引号、冒号或其他不允许字符的名称会在扫描阶段直接报错，而不会生成无法编译的代码。`-check` 不修改 `-out`，只在它与将要生成的内容不同时失败，供 CI 使用；用一行 `go:generate` 运行第一种写法即可让文件保持最新。生成文件的头部注明写出它的命令（`// Code generated by game manifest. DO NOT EDIT.`），因此由旧的 `gamemanifest` 命令生成的清单需要重新生成一次，`-check` 才会通过。

### tex

```sh
go tool game tex -format bc1 -mipmaps -srgb -out assets/albedo.ktx2 art/albedo.png
```

把位置参数（或 `-in`）指定的图片编码为 `-out`（简写 `-o`）处的 KTX2 文件，默认路径是输入路径换成 `.ktx2` 扩展名。`-format` 可选 `bc1`、`bc3`、`etc2` 或 `rgba8`，`-mipmaps` 写出直到 1x1 的完整 mipmap 链，`-srgb` 把纹理标为 sRGB，并在线性光空间中过滤 mipmap。

输入旁边有[导入设置附属文件](#资源旁的导入设置)时（例如 `art/albedo.png.import.yaml`），它的 `image` 设置与对 `assets` 的纹理目标一样生效：`format`、`mipmaps` 和 `srgb` 在命令行给出的参数之下生效（参数优先），`filter` 以 `game.filter` 写入 KTX2 文件。`slice` 和 `pivot` 是图集的设置，这里不读取，因为纹理包含整张图片；附属文件含有其他种类资源的设置时会被拒绝。

### editor

```sh
go tool game editor scenes/level1.yaml
go tool game editor .
```

在窗口中用 NSGo 场景编辑器打开指定的场景文件，未指定时打开开始界面（包含最近打开的项目、目录选择、从 `game new` 模板新建项目以及打开演示房间）。指定目录或 `game.yaml` 时打开游戏的[项目文件](project.md)及其主场景；位于某个项目目录中的场景文件会连同该项目一起打开。项目中的场景用项目自己的精灵图集绘制，图集按名称查找，方式与 [`game check`](#check) 相同，资源浏览器列出这些图集，项目的场景文件可以作为实例放置，材质、物理材质、分层瓦片地图的 YAML 图块集以及 Tiled 和 LDtk 源也从项目中加载，并填充瓦片调色板；组件是自由格式的，因为只有游戏的 Go 代码才会注册组件类型。不在任何项目中的场景文件从它自己的目录读取它引用的文件，并用演示房间的图集绘制。打开项目后，编辑器可以从文件浏览器打开项目中的其他场景、新建场景、把场景另存到其他路径，并在退出或打开另一个场景、项目会丢失未保存的修改时先询问（[场景文件与未保存的修改](editor.md#场景标签页场景文件与未保存的修改866873)）。`-library` 会添加一个面板，把该[库](library.md)中的包安装到打开的项目中。它没有内置任何[编辑器插件](editor.md)，也不知道游戏的任何组件类型。项目有自己的编辑器命令时，无论是 [`game generate editor`](#generate) 写出的，还是像 [Field Medic](https://github.com/nsgo-dev/game/tree/main/examples/fieldmedic) 那样导入了 `editorcmd` 的 `cmd/editor` main 包，都会在项目目录中用 `go run` 运行该命令来打开项目，并把同样的参数（其中场景和动画片段路径转换为绝对路径）和 `-library` 传给它；该命令按游戏的组件类型和内置物理组件检查每个组件，并内置游戏的插件。没有导入 `editorcmd` 的 `cmd/editor` 不会被运行；`game editor` 会说明这一点，并自己打开项目。场景格式见[编辑器与场景格式](editor.md)。编辑器，以及基于 `editorcmd` 的游戏自己的编辑器命令，会在本地端点上把打开的项目提供给智能体，`mcp` 通过它连接。

### mcp

```sh
go tool game mcp install
claude mcp add game-editor -- go tool game mcp
```

一个基于标准输入输出的 MCP 服务器，由 Claude Code、Codex 等智能体在项目目录（或其中任一子目录）里启动，或用 `-project` 指定项目。它找到打开了该项目的编辑器，把 [mcp](mcp.md#编辑器工具) 列出的全部编辑器工具以及变更流转发给智能体；没有编辑器打开该项目时，每次调用都会说明如何打开它。在项目中运行一次 `go tool game mcp install`（可附加 `-project`）会在项目根目录的 `.mcp.json`（供 Claude Code 使用）与 `.codex/config.toml`（供 Codex 使用）中写入或更新 `nsgo-game-editor` 项，以及当前用户在**设置 › 工具**中启用的每个 MCP 服务器，并删除与那里已禁用服务器同名的项目项。已启用服务器的命令和参数会从当前用户的编辑器设置复制到这些项目文件中，因此提交前请检查文件内容。无关服务器和其他 Codex 配置会保留，现有文件权限保持不变，内容相同的文件不会重写。工具与连接流程见 [mcp](mcp.md#编辑器工具)。

### release

```sh
go tool game release
go tool game release -channel store -release-notes notes/0.2.0.md
go tool game release -target macos_arm64,web -output build -dry-run
```

在指定目录（默认是当前目录，其中有游戏的 `nsgo.yaml`）里，通过一次 `nsgo build` 为所有平台构建游戏：未另行指定时，构建两种架构的 macOS、Windows 和 Linux，以及 Web、iOS 和 arm64 的 Android。每个目标的包写到 `<output>/<name>-<version>-<target>`，发布时上传的文件（磁盘映像、安装程序、Debian 和 Arch 包、AppImage、IPA、APK 和网站的 zip，各带一个 `.sha256`）写到 `<output>/releases`，目录结构与 `nsgo build` 相同。任一目标失败都会终止本次运行，退出码为 1。游戏的模块必须把 NSGo 作为工具（`go.mod` 里有 `tool github.com/nsgo-dev/nsgo/cmd/nsgo`，起始模板已带上），因为命令运行的是 `GOFLAGS=-tags=release go -C <dir> tool nsgo build`，构建使用模块自己的 NSGo 版本并裁剪仅限开发期的包（`pkg/tooling/gamedebug`、`pkg/tooling/hotreload`、`pkg/tooling/devstate` 以及物理与 AI 调试绘制）；`-dry-run` 只打印这条命令而不运行。构建包含桌面目标时，`game release` 会在构建后校验生成的桌面可执行文件的 Go 构建信息与符号表，缺少 `release` 标签或链接了调试符号时会失败。

目标取自 `-target`；没有时取 `nsgo.yaml` 旁边 `game.yaml` 的 `export` 预设，按顺序，没有 `targets` 的预设构建其平台名；再没有时取上面的六个平台，但渠道（`-channel`，没有时取 `nsgo.yaml` 的 `updates.channel`）为 `steam` 时只取 macOS、Windows 和 Linux，因为 `nsgo build` 只用桌面目标制作 Steam depot。命令不使用 `nsgo.yaml` 的 `build.targets`：它保存的是在所有宿主都能直接运行 `nsgo build` 的跨平台目标列表（省略 macOS 和 iOS，使普通 `nsgo build` 在 Linux 和 Windows 上也能成功），完整的发布目标列表写在 `game.yaml` 的导出预设里。当目标来自预设或默认值时，`game release` 会跳过当前宿主无法构建的目标（非 macOS 上的 macOS 目标，以及缺少 macOS 与 Xcode 时的 iOS、tvOS 和 watchOS 目标），打印跳过原因并继续构建其余目标；用 `-target` 显式指定的目标若当前宿主无法构建，则在运行 `nsgo build` 前直接报错。在装有 Xcode 的 Mac 上，未在 `nsgo.yaml` 配置 `sign.profile` 的 `ios` 真机预设仍会构建，并在标准错误上说明其为未签名包，而不会悄悄丢弃。输出目录取自 `-output`；没有时取 `game.yaml` 的 `export.output`；再没有时取 `nsgo.yaml` 的 `build.output`：

```yaml
version: 1
export:
  output: dist
  presets:
    - platform: macos
    - platform: windows
      targets: [windows_x64]
    - platform: linux
    - platform: web
    - platform: ios
    - platform: android
```

命令读取 `game.yaml` 的 `version`（必须是整数 1）、`export` 部分，以及为署名文件读取的 `assets` 部分。它拒绝项目文件字段（`version`、`name`、`id`、`appVersion`、`window`、`display`、`input`、`audio`、`autoload`、`export`、`editor`、`assets` 和 `navigation`）以外的顶层字段，因此拼错的 `export` 不会被悄悄跳过；在 `export` 里则拒绝未知字段、空的 `presets` 列表、未知的平台和目标、属于其他平台的目标、重复项以及位于项目之外的输出目录；文件的其余部分归项目文件管理。`-channel` 即 `nsgo build` 的同名参数：`store` 构建商店接受的包（Mac App Store 安装包，同时构建两种 macOS 架构时为通用包，以及 Windows MSIX、Flatpak 包和 Android App Bundle），取代直接分发的磁盘映像、安装程序和 APK；`-release-notes` 和 `-jobs` 也原样传给它。签名、公证和上传商店遵循 `nsgo.yaml` 和 `nsgo publish`。

四个实验性平台（`tvos`、`watchos`、`harmonyos` 和 `miniprogram`，以及它们的子目标如 `tvos_simulator`、`watchos_simulator` 和各小程序宿主）仍可在 `game.yaml` 预设与 `-target` 中使用，但本仓库 CI 不会为它们执行构建检查，且 NSGo 在这些平台上禁用了音频与 GPU 视图（见[平台支持](platforms.md#实验性目标平台)）。`game release` 会为本次构建涉及的每个实验性平台在标准错误上打印一次警告，列出缺失能力并附上平台支持页面与上游 NSGo issue 链接；传入 `-strict` 会将实验性平台警告转为错误（在运行 `nsgo build` 前以退出码 1 失败），便于在 CI 中使用。

`game.yaml` 有 `assets` 部分时（此时整个文件都必须通过[项目文件](project.md)的校验），发布会附带一份署名文件：`nsgo build` 成功后，命令把 `CREDITS.md` 写入输出目录、与各个包放在一起，列出该部分涉及的每个资源的作者、许可证和来源，以及许可证要求随资源附带的每段声明，内容取自每个资源的[导入设置附属文件](#资源旁的导入设置)中的 `credit` 部分，与 [`game credits`](#credits) 写出的相同。这些资源是每个图集打包的图片、每个纹理、音频、字体和模型目标的输入、每个模型以相对路径引用并嵌入其 GLB 的缓冲区和图片，以及资源清单在其文件夹里列出的每个文件（例如字体、地图和模型），但不含各目标写出的文件，因为它们由这些输入生成。只要有资源缺少署名，发布就会在构建任何东西之前停止，并列出需要写的附属文件；资源清单列出的文件如果是某个目标在输出改变之前写出的，在 `game assets` 删除它之前仍算作资源。`-dry-run` 同样检查署名，并给出它会写出的文件。没有 `assets` 部分的游戏不生成署名文件，命令会在标准错误上提示这一点。游戏从别处加载的文件，例如 `autoload` 资源或嵌入在资源清单文件夹之外的文件，不会被列出，需要自行署名。

署名文件写在各个包旁边，而不是包里面，因为包里装什么由 `nsgo build` 决定：它供发布者放到商店页面或下载页面上。若还要让每个包内都带上它，在发布之前把它写进 NSGo 的 `resources` 文件夹，`nsgo build` 会把该文件夹复制进每个包：`go tool game credits -out resources/CREDITS.md`。

一台机器能构建哪些目标，以 `nsgo doctor` 的显示为准：iOS 需要装有 Xcode 的 Mac，macOS 磁盘映像需要在已登录的会话里运行 `hdiutil`，所以为所有平台发布要在 Mac 上运行。

### replay

```sh
go tool game replay ./...
go tool game replay -run '^TestBossReplay$' ./levels
```

无界面回放游戏的[输入录像](replay.md)，供 CI 使用。录像要靠游戏自己的模拟才能回放，所以命令通过游戏的测试来回放：它先加载各目录下的每个 `*.replay.yaml` 文件，只要有一个无法回放（未知的键、表头中没有的名字、乱序的步），或者某个目录里一个录像也没有，就直接失败而不运行任何测试，这样写错的路径不会蒙混通过。随后用 `go tool nsgo test` 无界面运行名字匹配 `-run` 的测试，默认是名字中含 `Replay` 的所有测试，有一个失败命令就失败；一次回放的结束状态与测试期望的不同时，`replay.AssertEndState` 正是让测试失败。某个目录里没有任何测试通过时命令也会失败，例如 `-run` 一个测试也没匹配到，这样命名不对的测试或写错的模式不会在一个录像也没回放的情况下蒙混通过。请据此给回放录像的测试命名，例如 `TestLevel1ClearReplay`。

每个目录都按 `nsgo test` 的方式当作一棵树：无论末尾有没有 `/...`，其子目录都会被搜索和测试，跳过名字以 `.` 或 `_` 开头的目录和嵌套模块，但不跳过 `testdata`。默认是 `./...`。命令会运行 `go tool nsgo`，因此模块需要像 NSGo 应用那样把 NSGo 的命令加为工具。

### playtest

```sh
go tool game playtest
go tool game playtest -config playtest.yaml -out playtest.report.yaml -recordings playtest-recordings
go tool game playtest -check
```

校验 `playtest.yaml`（或 `-config` 指定的文件），并通过匹配 `-run`（默认 `Playtest`，在测试中调用 `playtest.RunTest`）的测试按每个种子无头运行配置的机器人。每次运行都会把输入录制到 `<recordings>/run_NNN_seed_<seed>.replay.yaml`，便于回放观看或转为 `game replay` 回归测试，并向 `-out`（默认 `playtest.report.yaml`）写出确定性的 YAML 报告，包含每次运行的指标、各目标的达成状态（`met`、`missed` 或 `info`）以及用于热力图的坐标数据。命令会打印报告汇总，若有任何目标未达成或未匹配到任何测试则以退出码 1 失败。可用 `-count` 或 `-seeds` 覆盖 `playtest.yaml` 中的运行次数或种子，用 `-check` 仅校验配置而不运行测试。见 [playtest](playtest.md)。

### bench

```sh
go tool game bench ./sprite ./tilemap
go tool game bench -update ./sprite ./tilemap
go tool game bench -in bench.txt
```

用 `go tool nsgo test -bench` 无界面运行各目录下的基准测试，共运行 `-count` 次（默认 3 次），每次运行 `-benchtime`（默认 200ms），取每个基准测试每次操作的耗时、字节数和分配次数的中位数，与 `-baseline` 指定的基线文件比较，默认是 `benchmarks/baseline.yaml`。它为基线中的每个基准测试打印一行，有一项超过限度或者没有测到，或者基线中没有任何基准测试时失败；测到了但基线中没有的基准测试列为新增，不算失败。`-bench` 像 `go test -bench` 那样缩小运行的范围。`-in` 从一个 `go test -bench -benchmem` 输出文件读取结果，不运行任何东西。

`-update` 改为把测得的中位数写入基线，基线不存在时创建它，并保留这次没有测到的基准测试。基线是 YAML 文本文件，可以像其他文件一样审阅和编辑：格式版本、一段备注、各项指标允许退化的百分比，以及按名字排序、每个基准测试一行。加载后再保存不会改变它，未知的键和缺少的指标会被拒绝。

```yaml
version: 1
note: Update with go tool game bench -update ./sprite ./tilemap
max_regression_pct: {ns_per_op: 25, bytes_per_op: 25, allocs_per_op: 25}
benchmarks:
  BenchmarkBatchDraw1000: {ns_per_op: 155412, bytes_per_op: 72232, allocs_per_op: 754}
  BenchmarkRaycast: {ns_per_op: 827.3, bytes_per_op: 0, allocs_per_op: 0}
```

基准测试的名字不带 `-GOMAXPROCS` 后缀，因此在一台机器上记录的基线可以在另一台机器上读取，但每次操作的耗时取决于机器：请在 CI 检查基线所用的同类机器上记录它。这个格式由 `tooling/bench` 包读写。

### size

```sh
go tool game size -kind aab path/to/app.aab
go tool game size -kind wasm -budget benchmarks/web-budget.json -markdown
```

测量一个包的体积并对照体积预算检查：Android App Bundle（`.aab`）、Android APK（`.apk`）、iOS App Store Package（`.ipa`）或 WebAssembly 二进制（`.wasm`）。包类型取自 `-kind`，否则根据包的后缀推断，再否则为 `wasm`。移动端包对照 `-budget` 指定的预算文件检查，未指定时使用平台内置预算；WebAssembly 二进制对照 `-budget` 检查，默认为 `benchmarks/web-budget.json`，`-wasm` 未指定二进制时先以 `GOOS=js GOARCH=wasm` 构建预算的 target。`-budget-mb` 和 `-abi-budget-mb` 覆盖总体积上限和单 ABI 上限。报告默认为文本，加 `-markdown` 为 Markdown，加 `-json` 为 JSON；包超出预算时报告仍会完整写出，命令随后失败。[包体积预算](package-size-budgets.md)列出了分类、默认预算和预算文件，预算文件由 `tooling/sizecheck` 包读取。

### new

```sh
go run github.com/nsgo-dev/game/cmd/game@latest new mygame
go run github.com/nsgo-dev/game/cmd/game@latest new -name "Space Rocks" -module example.com/rocks rocks
go run github.com/nsgo-dev/game/cmd/game@latest new -list
```

用一个模板生成项目并写入指定目录，目录不存在时会创建，并打印每个文件的路径。随后在该目录运行 `go get -tool github.com/nsgo-dev/game/cmd/game@<version>` 和 `go mod tidy`，这两步需要访问 Go 模块代理。这样项目依赖该版本的游戏层，`game` 工具与 `nsgo` 工具并列，无需修改即可用 `go tool nsgo dev` 构建运行。

游戏名称默认取目录名，`-name` 可以另行指定（字母、数字、空格、`-` 和 `_`，以字母开头）。名称写入 `game.yaml` 和 `nsgo.yaml` 的 `name`，`id` 由它派生为 `dev.nsgo.<去掉空格和标点的小写名称>`。模块路径默认是小写名称、单词之间用 `-` 连接，`-module` 可以另行指定。与标准库包同名的模块路径（例如名为 Time 的游戏得到的 `time`）会被拒绝，因为这样的项目无法构建，此时需要用 `-module` 指定，例如 `-module example.com/time`。`-version` 是要依赖的游戏层版本，例如 `v0.4.0`、某个提交或 `latest`；默认是正在运行的命令自身的版本，所以 `go run …@v0.4.0 new` 或以 `v0.4.0` 安装的 `nsgo-game` 创建的项目使用 `v0.4.0`，而从本仓库的检出构建的命令则使用 `latest`。`-template` 选择模板，默认是 `starter`；未知的模板名会被拒绝，并列出现有的模板名。`-list` 打印每个模板的名称（标出默认模板）和一行说明，不创建任何文件。[项目模板](templates.md)列出了所有模板以及 `nsgo init -template` 对应的模块，并说明模板的目录结构和如何新增模板。

只要目录中已经存在任何一个将要写入的文件，`new` 就什么也不写，并以 1 退出。每个模板都包含设置 `* text=auto eol=lf`、将二进制媒体扩展名标记为 `binary` 并把 `*.scene.yaml` 和 `*.scene.yml` 注册给 `nsgo-scene` 合并驱动的 `.gitattributes`，以及忽略 `.nsgo/`、`.game/` 和 `dist/` 的 `.gitignore`；`new` 会在新建目录中通过 `git rev-parse --show-toplevel` 查找仓库，只要位于已有 Git 仓库内（包括上级目录或 monorepo）就自动写入本地 Git 驱动配置，否则会打印在 `git init` 后需运行的 `go tool game merge -setup`。由于 Git 不会共享 `.git/config`，每个克隆都需运行一次 `go tool game merge -setup`（或在编辑器横幅中一键启用）。`go get` 或 `go mod tidy` 失败时（例如没有网络），已写入的文件会保留，错误信息会列出还需在该目录运行的命令以完成项目：`go get` 失败时是 `go get -tool …` 和 `go mod tidy` 两条，`go mod tidy` 失败时只有它一条。

### project

```sh
go tool game project check
go tool game project fmt
go tool game project -check sync path/to/game
go tool game project targets
```

处理指定目录（默认为当前目录）中项目的[项目文件](project.md) `game.yaml`。`check` 一次报告项目的全部问题：无法解析的 `game.yaml`、与它不一致的 `nsgo.yaml`、缺失或无效的自动加载文件，以及已安装包写入但已缺失的文件。`fmt` 把 `game.yaml` 改写为规范形式（规范形式不保留注释），有改动时打印其路径。`sync` 把 `game.yaml` 负责的字段写入 `nsgo.yaml`，文件不存在时会创建，有改动时打印其路径；修改 `game.yaml` 后运行它。`targets` 以逗号分隔输出各导出预设的构建目标，可直接传给 `nsgo build -target`。加 `-check` 时，`fmt` 和 `sync` 不写任何文件，文件不是规范形式或两个文件不一致时以 1 退出，供 CI 使用。对 `check` 或 `targets` 使用 `-check`，或把 `-check` 写在动词之后，属于调用错误。

### dev

```sh
go tool game dev
go tool game dev -target native -fps
go tool game dev -scene scenes/level2.scene.yaml
go tool game dev ./mygame -lang fr
```

运行 `go tool nsgo dev`：它构建游戏、启动游戏，并在 Go 代码改动时重新构建；同时把资源所在位置告诉运行中的游戏，即 `-assets` 的绝对路径（默认是项目目录下的 `resources`），放在环境变量 `NSGO_GAME_ASSETS` 里，`hotreload.DevAssetsDir` 返回的就是它。传入 `-scene <路径>` 会设置环境变量 `NSGO_GAME_SCENE`（`hotreload.SceneEnv`，可通过 `hotreload.DevScene` 读取），供运行中的游戏直接从指定场景启动（编辑器在“当前场景”模式下运行游戏时也使用同一机制）。它必须是游戏的 `assets.Manager` 读取 `"asset:"` URI 的目录：用 `assets.WithFS(os.DirFS(hotreload.DevAssetsDir()))` 创建的 Manager，因为没有 `WithFS` 的 `Manager` 会通过 `core.Scope.ReadAsset` 读取应用打包的资源，而不是磁盘上的实时编辑。游戏用 `hotreload.NewDevWatcher` 在这个目录上启动监视器后，它已加载的图集、图片、数据文件、着色器或材质在保存后一秒内就会在游戏里更新，无需重启；见[保存时重新加载](assets.md#保存时重新加载)和[着色器与材质热重载](render3d.md#着色器与材质热重载)。第一个不是参数标志的位置参数是项目目录，省略时为 `.`，其后的参数原样传给 `nsgo dev`；`-target`、`-fps` 和 `-automation` 也会传过去（`-automation` 启用 NSGo 的自动化服务，供编辑器的 Remote 标签页或 `go tool nsgo mcp` 实时检查运行中的游戏）；目录之后的参数里如果有 `-target`，就以它为准，与 `nsgo dev` 的处理一致。`nsgo dev` 的输出就是 game dev 的输出，Control+C 会同时停止两者。`nsgo dev` 失败时 `go tool game dev` 以 1 退出。

资源在 `native` 和 `tui` 目标上重新加载，带 `-android-host` 的 `android` 目标也可以，这些目标的应用的 Go 代码运行在本机并读取这些文件。在网页和手机上，应用访问不到资源目录，game dev 会提示这一点，游戏要等 `nsgo dev` 重新构建后才拿到资源改动。没有资源目录时，game dev 照常运行 `nsgo dev`，并提示资源不会重新加载。游戏用 `go:embed` 嵌入的资源属于二进制的一部分：这类文件改动时 `nsgo dev` 会重新构建并重启游戏，而监视器的重新加载会在那之前先显示改动。

### check

```sh
go tool game check
go tool game check examples/caverun
```

检查所指目录（或 `-in`；两者都没给时为工作目录）下的每个场景、地图、图块集和图集，以及它们之间的引用，让失效的引用在玩家碰到之前就报错。每个问题以 `file:line: message` 的形式打印到标准错误，文件名从工作目录算起，编辑器可以直接打开，最后打印问题数，命令以 1 退出；没有问题时打印每类文件各检查了多少个，以 0 退出：

```text
examples/caverun/assets/level1.tmj:1397: tileset image "tileset.png": not found
game check: 1 problem
```

| 文件 | 检查内容 |
| --- | --- |
| 场景（`*.scene.yaml`、`*.scene.yml`） | 按[场景格式](editor.md)解析并校验通过，没有未知字段；瓦片地图的 `source` 是存在的 Tiled 地图（`.tmj` 或 `.tmx`，须检查通过）或 LDtk 项目（`.ldtk`，须检查通过且含有 `level` 指定的关卡）；分层瓦片地图的 `tilesetFile` 是存在且检查通过的 YAML 图块集，并含有单元格放置的每个瓦片；每个精灵的 `atlas` 存在且含有它的 `frame`；内联瓦片地图的 `tileset` 图集含有它用到的每个瓦片 ID 的帧；`material` 文件存在、是有效的材质，且它的 `texture` 存在；`model` 是存在的 `.gltf` 或 `.glb` 文件，能用 `gltf.LoadFS` 加载，并含有 `modelNode` 指定的节点或 `modelMesh` 指定的网格，`mesh` 是内置基本形体（[scenemodel](scenemodel.md)）；在实例化或继承其他场景的场景中，每个连接的 `from` 和 `to` 都指向解析后场景中的对象；项目拥有自己的编辑器命令时，还会按项目的组件注册表一并检查组件字段以及每条连接的事件、处理函数和参数（[检查连接](editor.md#检查连接)） |
| Tiled 地图（`.tmj`）和图块集（`.tsj`） | 外部图块集是存在的 JSON 图块集（`.tsj`，或旧版 Tiled 使用的 `.json`；不是 `.tsx`）；图块集图片和图像图层的图片存在，组图层里的也算；地图能用 `tilemap.LoadTiled` 加载。场景引用的 Tiled XML 地图（`.tmx`）连同其外部图块集（`.tsx` 或 `.tsj`）能用 `tilemap.LoadTiled` 加载，且它绘制的每张图块集图片和图像图层图片都存在 |
| YAML 图块集（`*.tileset.yaml`、`*.tileset.yml`） | 按[图块集格式](tilemap.md#yaml-图块集)解析并校验通过；网格图块集的 `image` 存在，且大小足以容纳它的 `columns`、`tileCount`、`spacing` 和 `margin`；图集图块集的 `atlas` 存在，且含有它的每个 `frames` |
| LDtk 项目（`.ldtk`） | 图块集图片和关卡背景存在；没有关卡保存在单独的 `.ldtkl` 文件中，因为 `tilemap.LoadLDtkAll` 不读取这些文件（在 LDtk 中关闭“Save levels to separate files”）；项目能用 `tilemap.LoadLDtkAll` 加载 |
| 精灵图集（顶层有 `frames` 和 `meta` 的 JSON，即 TexturePacker 和 Aseprite 导出的格式） | `meta.image` 存在；每一帧都落在图片之内；图集能用 `sprite.LoadAtlas` 加载 |
| `game.yaml` 以及文件之间的其他所有引用 | `game.yaml` 按[项目文件](project.md)解析并校验通过；每个自动加载的场景和资源都存在，每个 `assets` 目标的 `in`（图集的是文件夹）也存在，上面各行没有覆盖到的其他引用所指的文件同样必须存在：场景 `inherits` 的场景和对象实例化的场景（`scene`，不论其文件名是什么）、覆盖项中以及不以 `*.scene.yaml` 命名的场景中的材质、模型和瓦片地图 `source`，材质的 `texture`（无论该材质在哪里被使用），以及 `.gltf` 模型存放在单独文件中的缓冲区和图片。`game assets` 写出的路径（`out`、`cache`、`manifest.in`）以及场景 `navMesh` 指定、由 `game navmesh` 写出的导航网格文件可以暂不存在 |
| 项目索引（`game.index.yaml`） | 能够解析，且同一项目中没有两个场景或资源文件共用同一个 `uid`（复制出的文件在 `game index` 给它分配新 uid 之前就是这种情况）；不同的项目（例如由同一模板创建的两个游戏）可以共用 |

地图、图块集和图集里写的路径相对于该文件本身，与 Tiled、LDtk 和纹理打包工具的写法一致。场景的瓦片地图 `source`、`tilesetFile`、`material` 和 `model`，YAML 图块集的 `image`，以及材质的 `texture`，相对于项目根目录：从场景往上最近一个含有 `game.yaml`、`nsgo.yaml` 或 `go.mod` 的目录，都没有时为被检查的目录。场景按游戏的 `sceneformat.AssetResolver` 的方式用名字指代图集，所以检查按文件名找它：同一项目中去掉 `.json` 后的文件名等于这个名字的图集 JSON（`actors` 即 `art/actors.json`），或者从项目根目录算起的路径等于这个名字的（`art/actors`）。有多个匹配时取离场景最近的一个，距离按从场景往上走到共同目录、再往下走到图集所经过的目录数计算，所以场景旁边的图集优先于子目录里的；两个同样近时报告为有歧义，直到场景用路径指明其中一个。没有图集匹配、但有同名的 JSON 文件无法解析或不是图集时，报告该文件的问题。路径必须是相对路径、使用正斜杠，且不能指到被检查的目录之外。PNG、JPEG 和 GIF 图片必须能解码；其他图片只要存在即可。

名字以 `.` 或 `_` 开头的目录以及名为 `testdata` 的目录会被跳过，与 go 命令的做法一致。其他 JSON 和 YAML 文件不会被读取。地图或图集的加载器报告的问题不带行号，因此打印为 `file: message`；只有当文件的引用都能解析时，检查才会运行这个加载器，同一个错误不会被报告两次。

如果引用指向的文件已不存在，而项目索引知道它移到了哪里，问题信息会说明这一点，[`game index`](#index) 会更新该引用：

```text
scenes/room.scene.yaml:10: material "materials/stone.material.yaml": not found; it moved to "materials/rock.material.yaml"; run game index to update the reference
```

当被检查的目录是拥有自己编辑器命令的项目时（`cmd/editor` 中导入 `pkg/editor/editorcmd` 的 `main` 包，或由 `game generate editor` 写出、用 `-tags gameeditor` 构建的 `main`），`game check` 会以 `-check <dir>` 运行该命令，使用项目的 `editorcmd.Options.Components` 加上内置物理和骨骼组件调用 `assetcheck.CheckWith`，一并检查组件字段以及连接的事件、处理函数与参数（包括由实例化或继承场景带来的连接）。没有编辑器命令时，`game check` 只检查连接的对象；只要场景里存在连接，就会在标准错误打印一条提示，说明事件和处理函数未被检查，以及如何用 `game generate editor` 开启。

Examples 工作流会对示例游戏运行 `game check`。要在自己游戏的 CI 中运行它，按上文把命令作为工具加入模块，然后在游戏目录下运行 `go tool game check`。这些检查也是一个包，`github.com/nsgo-dev/game/pkg/tooling/assetcheck`，它的 `Check` 接受一个 `fs.FS`，以值的形式返回问题；它的 `CheckWith` 还接受游戏的组件注册表，便于在测试中直接检查事件和处理函数（例如 `action3d` 模板中的 `TestScenesCheck`），无需编辑器命令。

### assets

```sh
go tool game assets
go tool game assets -check
```

一次构建 `game.yaml`（或 `-in` 指定的文件）声明的全部资源目标：像 `pack`、`tex` 和 `audio` 那样打包图集、编码纹理、转换音频，把字体烘焙为位图字体、把模型打包为单个 GLB 文件，最后像 `manifest` 那样重新生成资源清单。每个目标只在有改动时重建：没有任何改动时再运行一次不会写任何文件，改动一张图片只会重新打包它所属的图集。命令为每个构建了的目标打印一行，最后打印计数，例如 `1 built, 4 up to date`。

这些目标是项目文件 `game.yaml` 的 `assets` 部分。命令像游戏和编辑器一样经由 `project` 包读取它，`release` 也接受它，所以同一个 `game.yaml` 里既有这些目标，也有窗口、输入、自动加载和导出设置（见[项目文件](project.md)）。文件是带版本号的 YAML，命令会拒绝不认识的字段、没有 `assets` 部分的文件，以及项目模式拒绝的文件。这一部分的路径用斜杠分隔，相对于文件所在目录，且不能跳出该目录：

```yaml
version: 1
name: Cave Run
id: dev.nsgo.game.examples.caverun
appVersion: 0.1.0
window: {width: 480, height: 640}
assets:
  atlases:
    - name: ui            # 页面为 ui.png 和 ui.json
      in: art/ui          # PNG 图片，递归查找
      out: assets/atlases
      padding: 2          # 省略时为 1
      extrude: 1
      maxWidth: 2048
      maxHeight: 2048
      scale: 1
      trim: true          # 省略时为 true
  textures:
    - in: art/albedo.png
      out: assets/albedo.ktx2
      format: bc1         # rgba8、bc1、bc3 或 etc2；省略时为 rgba8
      mipmaps: true
      srgb: true
  audio:
    - in: sfx/jump.wav
      out: assets/jump.wav
      format: wav         # wav、aac 或 mp3；省略时为 wav
      rate: 22050
      channels: 1
      peakDB: -1          # 或 rmsDB，二者只能取一
      bitrate: 128        # 用于 aac 和 mp3
  fonts:
    - in: fonts/Inter.ttf # TrueType 或 OpenType 字体
      out: assets/fonts/hud.fnt  # 页面 hud.png，或 hud-0.png、hud-1.png 等，写在它旁边
      size: 24            # 像素；省略时为 32
      chars: " 0123456789:" # 省略时为可打印 ASCII
      padding: 1          # 省略时为 1
    - in: art/fonts/score # 字形图片目录：0.png ... 9.png、plus.png、times.png
      out: assets/fonts/score.fnt
      glyphs:             # 把不是单个字符的文件名映射到对应字符
        plus.png: "+"
        times.png: "×"
      spacing: 2          # 额外的前进量像素；省略时为 0
      align: bottom       # top、center 或 bottom；省略时为 bottom
  models:
    - in: models/ship.gltf  # .gltf 或 .glb，它引用的缓冲区和图片放在它旁边
      out: assets/ship.glb
      scale: 0.01         # 省略时为 1
      animations: true    # 省略时为 true
  manifest:
    in: assets
    out: assets/manifest_gen.go
    package: assets
    prefix: "asset:"
    embed: false
    embedDir: ""
  remote:
    out: dist/cdn
    base: https://cdn.example.com/game/
    include: [music/]
```

每个列表和清单都可省略，每项设置的含义与单个命令中同名参数相同。两个目标不能写同一个文件：名为 `ui` 的图集写出 `ui.png` 和 `ui.json`，需要多页时写出 `ui-0.png`、`ui-1.png` 等及对应的 `.json` 文件，因此同一文件夹里的其他目标不能写这些文件名，那里的其他图集也不能命名为 `ui` 或 `ui-1`。任何目标和缓存都不能覆盖 `game.yaml` 或某个目标读取的文件，否则下次运行会读到自己的输出。图集不能把页面写进任何图集读取图像的文件夹，其他目标和缓存也不能在那里写 `.png` 文件，否则下次运行时图集会把它们打包进去；缓存也不能放在资源清单的文件夹里，否则清单会把它列出来。名字以点开头的文件夹会被这两种扫描跳过，所以这些文件都可以放在那里。字体的页面像图集的页面以图集名命名那样，以它的 `.fnt` 文件命名，即 `hud.fnt` 旁边的 `hud.png` 或 `hud-0.png`、`hud-1.png` 等，并遵守同样的规则。任何目标也不能读取字体的页面：字体在纹理和模型之后构建，用页面生成的纹理会编码上一次运行的页面。

字体目标把 TrueType 或 OpenType 字体烘焙为 `text.LoadBMFont` 读取的 [AngelCode BMFont](text.md)：`chars` 中字体有字形的每个字符都以 `size` 像素（最大 2048）绘制，颜色为白色、覆盖率作为 alpha，字形以 `padding` 像素的间隔打包到每边最多 2048 像素的 PNG 页面上，`.fnt` 文本文件记录每个字形的位置、偏移和前进量、行高和基线，以及字体在这些字符之间的字距调整。字体没有字形的字符会被略过；一个字形都没有时报错。

`in` 也可以是一个 PNG 字形图片目录（如记分数字、伤害数字），从图片本身烘焙出同样的 `.fnt` 和页面：文件名主干是单个字符的图片（如 `7.png`）绘制该字符，其余名字由 `glyphs` 映射到字符，如 `plus.png` 映射到 `+`。未映射的名字、两张图片对应同一字符、`glyphs` 条目指向目录里不存在的文件、以及目录中出现非 PNG 文件都会被拒绝；`size` 和 `chars` 同样被拒绝，它们是为字体文件挑选字形的设置。每个字形按图片自身的像素绘制，因此 `lineHeight` 和 `base` 取最高图片的高度，前进量是图片宽度加 `spacing`，`align` 把字形放在行的顶部、居中或底部；手绘图片没有字距信息，`.fnt` 里也就没有。目录自己的附属文件只能设置 `padding`。

模型目标把 glTF 模型打包成一个[无需再解析任何外部文件](3d-models-blender.md)的二进制 glTF 文件：模型包含或引用的缓冲区（包括 `.glb` 文件的二进制块）合并进 GLB 的二进制块，它通过文件或 data URI 引用的每张图片连同媒体类型嵌入其中。其他属性全部保留，包括扩展和 extras，结果会通过加载来检查。`scale` 在模型各场景的每个根节点之上加一个按它缩放的新节点，同一根节点被多个场景列出时也只加一个，例如以厘米导出的模型用 `0.01`；`animations: false` 不保留动画。模型引用的缓冲区和图片必须是项目内的相对路径；它们是目标的输入，所以编辑 `.gltf` 文件旁的纹理会重新打包它。

配置 `remote` 后，`go tool game assets` 会把散装资源复制到 `remote.out` 下的内容哈希文件名（`<name>.<hash8>.<ext>`），并写出按 URI 排序且带版本号的 `remote.yaml` 清单，将每个 URI（默认前缀 `"asset:"`，或 `remote.prefix` 加相对路径）映射到其哈希路径、字节大小与 SHA-256，并将 `remote.base` 记为默认源站地址供 [`assets.WithRemote`](assets.md#远程资源) 使用。它扫描 `remote.in`，省略时使用 `manifest.in`（两者都未设置时取其余构建目标的输出），跳过以点开头的文件、`.go` 文件与 `*.import.yaml` 附属文件，并可用 `remote.include` 按前缀或通配符筛选。内容变动后旧的哈希文件会自动清理；`-resources` 会重定向 `remote.in` 而保持 `remote.out` 不变。

每个目标由什么构建而来记录在缓存里，默认是 `.game/assets-cache.yaml`，`assets.cache` 可指定其他路径。缓存是 YAML：为每个目标记录一个 SHA-256 键，覆盖它的设置、构建它的游戏层版本、每个输入（包括模型的缓冲区和图片）及其旁边导入设置附属文件的路径和内容，以及 AAC 和 MP3 所用的外部编码器及其版本，并记录它写出的每个文件的路径和 SHA-256。键相同且输出仍以同样内容留在磁盘上时目标才算最新，因此修改或删除某个输出会让它重建，换装另一个编码器也会。图集需要的页面比上次少时，不再写出的页面会被删除，它文件夹里同名的其他页面文件也会被删除。目标被改名、改到别的输出位置或从 `game.yaml` 中删除时，它上次写出的文件会在生成资源清单之前被删除。资源清单不需要缓存条目：它每次都会生成，生成时不读取任何文件内容，只有与现有文件不同时才写入。缓存缺失或损坏只会导致一次完整重建。请把 `.game` 文件夹加入 `.gitignore`。

`-resources` 会把所有构建目标的输出以及清单扫描目录重定向到 `resources/`（如有 `assets/` 前缀则替换之），并在关闭 `embed` 的情况下于清单中生成 `"asset:"` URI，使构建产物随 NSGo 应用资源分发而不打入嵌入式目录。`-force` 重建所有目标。`-check` 不在项目里写任何文件：它列出过期的目标，有过期目标时以 1 退出，供 CI 使用。缓存不能证明是最新的目标（例如在没有 `.game` 文件夹的全新检出中）会在临时文件夹里构建，项目中的文件与它写出的完全一致时即算最新，因此只要提交了输出，CI 就不需要缓存。AAC 和 MP3 是例外：不同的编码器或同一编码器的不同版本会为同一输入写出不同的字节，所以没有缓存条目时，只要它们的输出存在就算最新，CI 上也不需要编码器。图集的文件夹里若有它不再写出的同名页面文件（例如之前需要更多页面时留下的 `ui-1.png`），在某次构建删除它之前该图集都算过期。某个目标失败时，命令会保留它之前已构建目标的缓存，下次运行从那里继续。

#### 资源旁的导入设置

资源可以带一个附属文件（sidecar）：放在它旁边、在它的文件名后加上 `.import.yaml` 的 YAML 文件，例如 `art/albedo.png.import.yaml`，说明如何导入它（与 Godot 的 `.import` 文件相同），以及它由谁制作、以什么许可证提供。附属文件里的设置会覆盖读取该文件的目标的同名设置，因此改一个资源的导入方式只需编辑它旁边的文件，下次运行就会重建读取它的目标。附属文件是带版本号的 YAML，由 `github.com/nsgo-dev/game/pkg/importsettings` 包读写；它含有 `image`、`audio`、`font` 或 `model` 部分之一、一个 `credit` 部分，或者署名加上前四者之一，会拒绝不认识的字段，并可用[已发布的 Schema](schemas.md) `https://game.nsgo.dev/schemas/import.v1.json` 校验。

```yaml
# art/albedo.png.import.yaml，由纹理目标读取
version: 1
image:
  filter: nearest     # linear 或 nearest，以 game.filter 写入 KTX2 的键值数据
  format: rgba8       # rgba8、bc1、bc3 或 etc2
  mipmaps: false
  srgb: true
```

```yaml
# art/hero/run.png.import.yaml，由文件夹里有 run.png 的图集读取
version: 1
image:
  slice: {width: 16, height: 16, margin: 1, spacing: 2, count: 6}
  pivot:
    default: [0.5, 1]
    cells:
      - cell: 2
        pivot: [0.25, 0.75]
```

```yaml
# music/theme.wav.import.yaml，由音频目标读取
version: 1
audio:
  format: wav         # wav、aac 或 mp3
  rate: 32000
  channels: 2
  rmsDB: -14          # 或 peakDB；设置任一项都会替换目标的归一化设置
  bitrate: 128
  loop: {start: 44100, end: 1323000}  # 以源文件的采样帧计；end 为 0 或省略表示声音结尾
```

```yaml
# fonts/Inter.ttf.import.yaml，由字体目标读取
version: 1
font:
  size: 24
  chars: " 0123456789:"  # 每个字符一次，顺序不限
  padding: 2
```

```yaml
# models/ship.gltf.import.yaml，由模型目标与 scenemodel.Build 读取
version: 1
model:
  scale: 0.01
  animations: false
  lodHysteresis: 0.05
  lods:
    - { node: Ship_LOD0, minScreenRatio: 0.25 }
    - { node: Ship_LOD1, minScreenRatio: 0.0 }
```

```yaml
# assets/fonts/pixel.ttf.import.yaml，没有目标导入的资源旁的署名
version: 1
credit:
  author: Ana Souza
  license: OFL-1.1    # SPDX 许可证表达式
  source: https://example.com/pixel-font
  notice: |-
    Copyright 2026 Ana Souza.
    Licensed under the SIL Open Font License, Version 1.1.
```

- 纹理目标读取 `filter`、`format`、`mipmaps` 和 `srgb`。`filter` 存放在 KTX2 文件键值数据的 `game.filter` 键下，[`render3d.LoadKTX2`](render3d.md) 加载纹理时据此设置采样器的过滤方式：`nearest` 让它绘制得清晰锐利，适合像素画；`linear` 混合相邻纹素。`slice` 和 `pivot` 会被拒绝，因为纹理包含整张图片，除非还有图集读取这张图片。
- 图集目标从它文件夹里每张图片的附属文件中读取 `slice` 和 `pivot`：`slice` 把图片切成 `width` × `height` 像素的格子，距上边和左边 `margin` 像素，格子之间相隔 `spacing` 像素，逐行取出，`run.png` 的第 `i` 个格子作为帧 `run_i.png` 打包；`count` 只保留前若干个格子，省略时每个放得下的格子都是一帧。`pivot` 以未裁剪帧尺寸的比例设置归一化 `[x, y]` 枢轴坐标（`[0, 0]` 为左上角，`[0.5, 1]` 为底部中心），写入打包后每帧的 TexturePacker `pivot` 字段，由 `sprite.LoadAtlas` 读入 `Frame.PivotX` 和 `Frame.PivotY`：`default` 适用于未切分图片以及切分精灵表中未单独覆盖的每个格子，`cells` 按 0 起始格子序号升序列出单格 `{cell, pivot}` 覆盖（需同时设置 `slice`）。纹理设置（包括 `filter`）在这里会被拒绝，因为图集页面是打包进去的所有图片共用的一张纹理，除非还有纹理目标读取这张图片：同时被两者读取的图片可以同时带两类设置，每个目标各取所需。与文件夹里另一张图片同名的帧会被拒绝。
- 字体目标读取全部 `font` 设置：`size`、`chars` 和 `padding`。以另一种顺序列出同样的字符不会改变任何东西。
- 模型目标读取全部 `model` 设置：`scale` 和 `animations`。
- `game release` 和 `game credits` 读取 `credit`。任何资源的附属文件都可以带它，可以与 `image`、`audio`、`font` 或 `model` 部分并存，也可以单独出现；`game assets` 不根据它构建任何东西。`author` 和 `license` 是必填项。`license` 是一个 [SPDX 许可证表达式](https://spdx.github.io/spdx-spec/v2.3/SPDX-license-expressions/)：SPDX 许可证列表中的标识符，例如 `CC0-1.0`、`CC-BY-4.0` 或 `OFL-1.1`，用 `AND` 和 `OR` 连接、用括号分组，`+` 表示“或任何更新版本”，`WITH` 表示例外条款，例如 `MIT OR Apache-2.0`；没有标识符的许可证（例如商店或工作室自己的许可证）写作 `LicenseRef-` 加上自己的名字，条款写在 `notice` 里。这里只检查表达式的形式，不对照列表检查标识符，并拒绝 `NONE` 和 `NOASSERTION`。`source` 是资源的来源，通常是一个 URL；`notice` 是许可证要求随资源附带的文字，例如版权声明，会逐字保留，需要时可以写成多行。
- 音频目标读取全部 `audio` 设置。`loop` 以无限重复的正向循环写入 WAV 输出的 `smpl` 块，并按输出采样率缩放；AAC 和 MP3 不带循环点，所以带循环的声音必须转换为 `wav`。源 WAV 的 `smpl` 块里已有的循环也会照此保留。

附属文件中的设置是读取其资源的每个目标的键的一部分，因此新增、编辑或删除这些设置只会重建这些目标，在下次运行之前 `-check` 会报告它们已过期。它的 `credit` 部分、注释和排版不属于键，因为它们不改变任何输出：修正作者的名字不会重建任何东西。附属文件不是资源：资源清单会跳过名为 `*.import.yaml` 的文件，图集也只打包 `.png` 文件。`game pack`、`game tex` 和 `game audio` 也读取输入旁的附属文件，命令行参数优先。目标读取的文件夹（图集的文件夹及其下的文件夹，或纹理、音频、字体、模型输入所在的文件夹）里，如果某个附属文件对应的资源没有任何目标读取，它会被拒绝；因此拼错的文件名、图集文件夹里 JPEG 旁边的附属文件、或者与资源大小写不同的名字（例如 `hero.png` 旁边的 `Hero.PNG.import.yaml`）都会报错，而不是让设置悄无声息地不起作用。只含 `credit` 的附属文件不影响构建，所以它可以放在这些文件夹里任何文件旁边，例如 JPEG 或模型嵌入的图片，只要该文件确实以完全相同的名字存在。

### generate

```sh
go tool game generate component player_health
go tool game generate system movement
go tool game generate -main scene level_one
go tool game generate prefab enemy
go tool game generate editor
```

把一个新的组件、系统、场景或预制体的代码和文件，或者游戏的编辑器命令，写入 `-in`（默认为当前工作目录）中的游戏，布局与 [`game new`](#new) 创建的项目一致，并打印写出的每个文件的路径。Go 代码写进该目录中的包，使用该包的包名，每个 Go 文件旁边都附带一个测试。名称由 ASCII 字母和数字组成、以字母开头，可以用驼峰写法，也可以用 `-` 或 `_` 连接单词，并按用到它的每个地方写成相应的形式：`player_health`、`player-health` 和 `PlayerHealth` 都得到 Go 类型 `PlayerHealth`、键名 `playerHealth` 以及名为 `player_health_*` 的文件。

| 种类 | 写出 | 注册到 |
| --- | --- | --- |
| `component` | `<name>_component.go`：一个结构体（`PlayerHealth`），以及场景文件用来设置它的 `sceneformat.ComponentType`（`playerHealth`），两者都还没有字段 | `Components`，加载场景时用来检查其对象组件的 `sceneformat.ComponentRegistry` |
| `system` | `<name>_system.go`：一个函数 `MovementSystem(w *entities.World, dt time.Duration)`，以 `movement` 为名加入调度器 | `Systems`，游戏每个固定步长推进一次的 `entities.Scheduler` |
| `scene` | `scenes/<name>.scene.yaml`：采用场景格式当前版本的场景，含一个名为 `main` 的空图层，虚拟分辨率取自 `game.yaml`（没有时取其窗口大小）；以及 `<name>_scene.go`，它嵌入该文件，并用 `LoadLevelOneScene` 解析，按 `Components` 检查其组件 | 加 `-autoload` 时注册到 `game.yaml` 的 `autoload` 场景，加 `-main` 时同时设为其 `mainScene` |
| `prefab` | `prefabs/<name>.scene.yaml`：只含一个对象的场景，该对象的 `prefab` 就是这个名称，供其他场景用 `scene: prefabs/enemy.scene.yaml` 实例化；以及 `<name>_prefab.go`，同名的 `entities.Prefab`（`EnemyPrefab`） | `Prefabs`，按名称索引的游戏预制体 |
| `editor`（不带名称） | `editor.go`：游戏的场景编辑器，一个带 `gameeditor` 构建标签的 `main` 函数，它用 `Components` 和 `editorPlugins`（游戏的编辑器插件，声明时为空）运行 `editorcmd.Main`；以及 `editor_test.go`，用它打开项目 | 给声明游戏自身 `main` 的文件加上 `//go:build !gameeditor`，使带该标签构建时由编辑器取而代之 |

这些注册表各只声明一次，分别在 `components.go`、`systems.go` 和 `prefabs.go` 中，由第一个需要它的组件、系统、场景或预制体写出。每个生成的文件在 `init` 函数中把自己加入注册表，因此 `generate` 从不修改已经存在的文件。游戏在加载场景和运行循环的地方使用它们：`sceneformat.ParseWith(data, sceneformat.ParseOptions{Components: Components})` 拒绝场景中设置了却没有任何组件描述的组件或字段，`game.Loop` 的 `Update` 中调用 `Systems.Step(world, dt)` 按顺序运行各个系统，顺序由加入每个系统时的 `entities.After` 和 `entities.Before` 决定。之后在生成的代码上继续填写：组件的字段以及场景可设置的每个字段对应的 `sceneformat.Field`、系统的查询、用 `entities.With` 给预制体添加的组件。

编辑器命令就是带 `gameeditor` 标签构建的游戏自身的包，因为 `game new` 创建的游戏把代码放在 `main` 包中，而其他包无法导入 `main` 包：这样构建出的编辑器链接了游戏的组件类型，不带该标签构建的游戏则不链接编辑器。[`game editor`](#editor) 以 `go run -tags gameeditor .` 在项目上运行它，因此项目的场景打开时，其组件和字段都按 `Components` 检查，检查器也会列出这些字段。要添加插件，把插件的 `editor.Plugin` 追加到 `editorPlugins`，只有编辑器用到的代码放在带该标签构建的文件中，或放在只有这些文件导入的包中；`game.yaml` 在 `editor.plugins` 下启用它（[编辑器插件](editor.md#编辑器插件492898)）。用 `go tool nsgo test . -tags gameeditor` 运行它的测试。以下情况 `generate editor` 会拒绝：包不是 `main` 包或没有 `main` 函数；声明 `main` 的文件的构建约束是旧式的 `// +build` 行，或者已经提到 `gameeditor`（再次运行 `generate editor` 时就是这种情况）；声明 `main` 的文件还声明了包的其他部分需要的东西，而编辑器构建会整个去掉这个文件、丢失它们：其他文件或测试用到的名称、`init` 函数、空白 `var`，或者其他文件所声明类型的方法（把它移到另一个文件中）；包已经声明了它要声明的标识符。改写该文件失败时，它会删除已经写出的文件。

`generate` 写出的内容原样就能构建、通过 `go tool nsgo test` 运行的测试，并通过 `game check`。要写的文件已经存在，或者包中（包括测试和其他平台的文件）已经声明了它要声明的标识符时，它什么也不写，以 1 退出；这时换一个名称即可。包中把 `Components`、`Systems` 或 `Prefabs` 声明成变量以外的东西，或者声明了生成的文件导入包时使用的名称（例如 `entities` 或 `time`）时，它同样失败；名称 `components`、`systems` 和 `prefabs` 也会被拒绝，因为它们的 Go 名称就是注册表的名称。`-autoload` 和 `-main` 会按 `game project fmt` 的格式重写 `game.yaml`，因此对于不是这种格式的 `game.yaml`（例如带注释的，重写会丢掉注释），在 `game project fmt` 重写它之前一律拒绝；`game.yaml` 已经自动加载了同名场景时也会拒绝。起始模板的 `TestAutoloadsLoad` 会检查 `game.yaml` 自动加载的文件，因此自动加载的场景丢失时，游戏的测试会失败；它从项目目录读取这些文件，所以在手机、模拟器或 Web 上用 `nsgo test -target` 运行时会跳过，而生成的测试只读取其代码嵌入的场景文件。

### mv

```sh
go tool game mv materials/stone.material.yaml materials/rock.material.yaml
go tool game mv -dry-run art gfx
```

移动或重命名一个文件或目录，并改写所有指向被移动内容的引用：包括 `game.yaml`（含已安装包在 `packs[].dir` 中的目录）、场景、资源文件以及 Tiled 和 LDtk 文件；Tiled 和 LDtk 文件本身被移动时，其中相对自身书写的路径也会改写。每个引用都原地修改，保留引号，文件其余部分（包括注释）逐字节不变。项目索引 `game.index.yaml` 会随之更新。路径从工作目录算起，且必须位于项目之内：即 `-in` 指定的目录，未指定时为从工作目录往上最近一个含有 `game.yaml`、`nsgo.yaml` 或 `go.mod` 的目录。每项修改都会打印出来；`-dry-run` 只打印而不执行，与 `release` 相同。会导致引用失效的移动会在做任何修改之前被拒绝，移动 `game.yaml` 或索引、移动到已存在的文件、移出项目，以及在不移动包目录的情况下单独移动已安装包 `files` 列表中的文件或子目录，也会被拒绝。见[稳定引用](project.md#稳定引用)。

### index

```sh
go tool game index
go tool game index -dry-run examples/caverun
```

给每个还没有 `uid` 的场景文件和资源文件加上 uid（写在 `version` 之后的一行），把指向未经工具移动的文件的引用改到文件的新路径，给保留了原文件 uid 的副本换一个新 uid，并在每个项目根目录写出 `game.index.yaml`，其中列出每个场景和资源文件的路径与 uid，以及其他被引用文件的路径与 SHA-256（对文本文件在计算哈希前会把 CRLF 规范为 LF，使索引在不同操作系统间保持一致）。命令会打印每项修改，或者打印索引已是最新；`-dry-run` 只打印而不执行。目录为所指定的目录或 `-in`，否则为从工作目录往上最近一个含有 `game.yaml`、`nsgo.yaml` 或 `go.mod` 的目录。`check` 检查的是工作目录及其下的内容，而 `mv` 和 `index` 默认作用于上述整个项目，因为指向被移动文件的引用可能出现在项目的任何位置，而索引位于项目根目录：在子目录中运行时，`check` 只检查该子目录，`index` 则更新整个项目。首次使用时运行一次以开始维护索引，之后每次手动或用 Git 移动文件后再运行；把索引和文件一起提交。

### library

```sh
go tool game library list tiles
go tool game library install dungeon-tiles
go tool game library remove dungeon-tiles
go tool game library digest library/packs/dungeon-tiles
```

列出库中与给定词匹配的包（`-kind` 只保留一种类型），并显示项目已安装的版本；把包安装到当前目录（或 `-project`）的项目中，写入包的文件，并在 `game.yaml` 中记录它们以及它们新增的自动加载条目和编辑器插件；用新版本覆盖旧版本；移除已安装的包；以及为库索引打印包目录的摘要。除非 `-index` 指定了其他库，否则使用本仓库中的共享库。索引和包的格式、安装可以覆盖和不能覆盖什么，以及如何添加包，见[资源与插件库](library.md)。

### credits

```sh
go tool game credits
go tool game credits -in game.yaml -out CREDITS.md
```

从 `-in`（默认是 `game.yaml`）的 `assets` 部分写出 `game release` 在各个包旁边写出的署名文件，写到 `-out`，`-out` 为空时直接打印，不构建任何东西。它列出的资源与 `release` 相同，每个资源的作者、许可证和来源取自它旁边[导入设置附属文件](#资源旁的导入设置)中的 `credit` 部分；只要有资源没有署名，就以 1 退出，并给出每个缺少署名的资源需要的附属文件，因此 CI 可以运行它来确保每个资源都有署名。文件是 Markdown，只取决于署名内容，相同的附属文件总是生成相同的字节：

````markdown
# Credits

<!-- Generated from the credit section of each asset's import settings sidecar (*.import.yaml). DO NOT EDIT. -->

| Asset | Author | License | Source |
| --- | --- | --- | --- |
| `assets/fonts/pixel.ttf` | Ana Souza | OFL-1.1 | <https://example.com/pixel-font> |
| `sfx/jump.wav` | Kenney | CC0-1.0 |  |

## Licenses

| License | Assets |
| --- | --- |
| [CC0-1.0](https://spdx.org/licenses/CC0-1.0.html) | 1 |
| [OFL-1.1](https://spdx.org/licenses/OFL-1.1.html) | 1 |

## Notices

### `assets/fonts/pixel.ttf`

```text
Copyright 2026 Ana Souza.
Licensed under the SIL Open Font License, Version 1.1.
```
````

资源按路径排序。许可证表统计每个许可证表达式下的资源数，并把单个 SPDX 标识符链接到它在 SPDX 许可证列表中的页面；随后逐字列出每段声明。`github.com/nsgo-dev/game/pkg/tooling/credits` 包负责收集署名并写出文件，游戏若想在自己的界面里展示署名也可以使用它。

### pixelfx

```sh
go tool game pixelfx -out assets/fx fire/torch
go tool game pixelfx -set seed=4 -set palette=#102,#215,#43a,#8cf,#fff -flip -name frost-torch -out assets/fx fire/torch
go tool game pixelfx -act -fps 30 -out assets/fx moves/ground-pound
go tool game pixelfx -list
```

把命令内置的某个 [pixelfx](pixelfx.md) 特效渲染为 PNG 精灵表及其 Aseprite JSON，即 `-out` 下的 `<name>.png` 和 `<name>.json`，可供 `sprite.LoadAtlas` 读取：每一帧按特效自身的帧率（或 `-fps`）渲染并排成网格，带有每帧的时长和位于特效原点的轴心，以及一个动画标签（循环特效循环播放，一次性特效只播放一次）。名字默认取特效名（`torch`），可用 `-name` 指定，但必须是不带目录的文件名。`-set name=value` 设置特效的一个选项，值可以是数字、`true` 或 `false`、颜色（如 `#4a3f69`，可写作 `#rgb`、`#rrggbb`，或忽略透明度的 `#rrggbbaa`）、用逗号分隔的一串颜色（调色板）或文本；`-flip` 镜像特效。`-act` 改为渲染表演版本：内置角色带着稻草靶表演该特效，除非用 `-fps` 指定，否则帧率为 60，JSON 中带有表演的时间信息，供有自己角色的游戏使用。`-list` 列出所有特效及其尺寸、时长和选项。命令不认识的特效、选项或取值以 2 退出。它能渲染的类别是目前已移植的类别，每个类别移植时会把自己加进来。

### navmesh

```sh
go tool game navmesh scenes/level1.scene.yaml
go tool game navmesh -out build/level1.navmesh.yaml scenes/level1.scene.yaml
```

烘焙一个 3D 场景的[导航网格](navbake3d.md)：它解析该场景实例化和继承的场景，从项目根目录加载每个对象的 `mesh` 指定的 glTF 模型（`.gltf` 或 `.glb`），放在对象的 3D 变换处，再按[项目文件](project.md)中 `navigation` 部分的设置，为其中的每种代理类型各烘焙一次；没有该部分时，用默认设置烘焙一种名为 `default` 的代理类型。它把[导航网格文件](formats.md#3d-导航网格)写到 `-out`，或写到场景的 `navMesh` 从项目根目录指定的文件，并输出该文件以及每种代理类型的多边形数。项目目录为 `-in`，或工作目录及其上级目录中最近一个含有 `game.yaml`、`nsgo.yaml` 或 `go.mod` 的目录，场景必须位于其中。同样的场景和设置总是写出同样的字节，因此该文件应与场景一起纳入版本控制，未改变任何东西的烘焙不会产生差异。场景没有 `navMesh` 且未给出 `-out` 时退出码为 2，没有可烘焙的几何体时退出码为 1。

### merge

```sh
go tool game merge -setup
go tool game merge -check
go tool game merge %O %A %B %P
go tool game merge -out merged.scene.yaml base.scene.yaml ours.scene.yaml theirs.scene.yaml scenes/level1.scene.yaml
```

面向 YAML 场景文件（`*.scene.yaml`、`*.scene.yml`）的三路 Git 合并驱动。它不按行合并场景，而是按身份匹配：图层按 `name`，对象在整个场景树中按 `id`（包括跨父节点和跨图层移动），瓦片地图与覆盖按主键，连接按 `(from, event, to, handler)`；字段逐一合并，使两个分支对不同对象或同一对象不同字段的修改无冲突地组合，并以规范的 `sceneformat.Encode` 字节写出。

Git 用 `<base> <ours> <theirs> [path]` 调用驱动时，无冲突合并会用规范的场景 YAML 覆盖 `<ours>`（或写入 `-out`）并以 0 退出。空的 `<base>` 文件（Git 的双侧新增冲突）会以空祖先为基准合并 `<ours>` 与 `<theirs>`。当双方把同一字段改为不同值、一方删除而另一方修改、双方以相同 ID 新增不同对象、重设父节点冲突或形成环，或合并结果无法通过 `sceneformat.Validate` 时，驱动会写出带版本号、确定性的[场景合并冲突报告](formats.md#场景合并冲突报告)（`version: 1` YAML，列出每个冲突的对象、字段以及 `base`、`ours` 和 `theirs` 三个值）到 `<ours>`（或 `-conflict-out` / `-out`），并以 1 退出，让 Git 将该路径标记为未合并。

`-setup [dir]` 在仓库的本地 Git 配置中注册 `merge.nsgo-scene.name` 和 `merge.nsgo-scene.driver`（`go tool game merge %O %A %B %P`），并确保 `.gitattributes` 设置 `* text=auto eol=lf`、将二进制媒体扩展名标记为 `binary`，以及将 `*.scene.yaml` 和 `*.scene.yml` 指向 `merge=nsgo-scene`。由于 Git 从不共享 `.git/config`，且当 `.gitattributes` 指定的驱动未配置时会静默回退到按行文本合并，每个克隆都需运行一次 `go tool game merge -setup`。用 `game new` 创建的新项目已包含 `.gitattributes`，只要位于已有 Git 仓库内（包括上级目录）就会自动完成本地驱动配置；否则会打印在 `git init` 后运行的配置命令。

`-check [dir]` 检查当 `.gitattributes`（或 Git 生效的属性）将文件路由到 `merge=nsgo-scene` 时，Git 是否已配置 `merge.nsgo-scene.driver`：缺少驱动（或目录不在 Git 仓库内）时以 1 退出并打印修复命令，否则以 0 退出。场景编辑器在打开位于 Git 仓库中的项目时也会运行同样的检查，并在工具栏下方通过横幅提供一键配置。

### schema

```sh
go tool game schema
go tool game schema -out schemas/scene.json
go tool game schema -out schemas/scene.json path/to/project
```

把项目的 draft-07 场景 JSON Schema 输出到标准输出或写入 `-out`，组件名按升序排列、属性顺序稳定、输出完全确定（见 [YAML 文件的 Schema](schemas.md#项目场景-schema)）。由于 `game` CLI 无法直接看到项目的 Go 类型，当项目拥有自己的编辑器命令时（`cmd/editor` 中导入 `pkg/editor/editorcmd` 的 `main` 包，或由 `game generate editor` 写出、用 `-tags gameeditor` 构建的 `main`），`game schema` 会以 `-schema` 运行该命令，直接输出 `editorcmd.Options.Components`（包含插件以及内置物理和骨骼组件）的场景 Schema 并退出，不打开窗口。没有自己编辑器命令的项目会得到内置场景 Schema，其中内置物理、骨骼和 2D 导航区域组件带有完整类型定义，而自定义组件保持自由格式。

### net

```sh
go tool game net decode session.capture.jsonl
go tool game net decode --schema ./proto --format json --resolve-deltas session.capture.jsonl
echo "230100000001" | go tool game net decode -
go tool game net stats session.capture.jsonl
```

由 `pkg/online/netplay/wire` Schema 驱动，检查 `netplay.NewCaptureTransport` 写出的 `pkg/online/netplay` 抓包日志（`*.capture.jsonl`）或标准输入上的单帧十六进制/Base64 线协议数据（见 [netplay](netplay.md#抓包与协议检查)、[netplay/wire](netplay-wire.md)）。

- `decode` 将每一帧按 YAML（默认）或 JSON（`-format json`）逐层打印：传输帧头（`channel`、`seq`）、可选的中继帧与控制体，以及由线协议判别字段选出的复制、锁步/回滚、成员变更、握手或游戏自定义消息。传入 `-schema <目录或文件>` 可解码游戏自定义的 `*.wire.yaml` 消息（包括 `EntityState.Props`）；传入 `-resolve-deltas` 会跟踪每个对等端的复制基线，让 `SnapshotDelta` 同时打印合并后的完整实体状态；传入 `-` 可从标准输入读取 JSONL 抓包流或单帧十六进制/Base64 数据。
- `stats` 读取 JSONL 抓包（文件或 `-`），按对齐文本（默认）、YAML 或 JSON（`-format text|yaml|json`）打印总包数、总字节数、时长、平均 `SnapshotDelta` 大小，以及按消息类型和按对等端的明细统计。

### wire

```sh
go tool game wire -in ./netmsg -out ./netmsg
go tool game wire -lang ts -in ./netmsg -out ./netmsg/ts
go tool game wire -in ./netmsg -out ./netmsg -check
```

从 `*.wire.yaml` Schema 生成确定性、零分配的 Go 编解码器，或在 `-lang ts` 下生成无依赖的 TypeScript 编解码器（见 [netplay/wire](netplay-wire.md)）：处理 `-in` 目录中的每个 Schema，或作为参数给出的 Schema 文件，以及它们导入的 Schema。`-out` 是一个目录，或在只有一个 Schema 时是一个 `.go` 或 `.ts` 文件，默认是输入目录；`-pkg` 覆盖 Go 包名。`-check` 不写任何文件，在生成的文件缺失或与其 Schema 的生成结果不同时失败，CI 就是这样保证 `pkg/online/netplay/wire` 中提交的编解码器保持最新。

### completion

```sh
mkdir -p ~/.zfunc
go tool game completion zsh > ~/.zfunc/_game
```

把 `zsh`、`bash` 或 `fish` 的补全脚本写到标准输出。脚本补全子命令（附带其简介）、每个子命令的参数（附带其说明）、`-format` 和 `release` 的 `-channel` 可取的值、`generate` 能生成的种类（由于命令的参数在每个位置的补全方式相同，补全名称时也会提供这些种类）、`pixelfx` 能渲染的特效，以及 `-in`、`-out` 和其他命令接受的文件与目录路径。脚本由命令定义生成，因此升级游戏层后重新生成一次，就能补全升级新增的子命令和参数。

脚本同时补全 `game` 和 `nsgo game`（需要高于 v0.3.4 的 NSGo 发布版本，见[安装](#安装)）：在 `nsgo` 之后提供 `game` 和 `nsgo` 自己的命令，在 `nsgo game` 之后提供与 `game` 之后相同的补全。`go tool game` 不会被补全，因为这时 shell 补全的是 `go`。`nsgo` 自身没有补全，所以 zsh 和 bash 脚本只在 `nsgo` 还没有补全时才为它注册：在脚本之前注册的、compinit 在 `$fpath` 中更靠前的位置找到的、或 bash-completion 能够加载的 `nsgo` 补全会继续补全 `nsgo`；`$fpath` 中更靠后的 `_nsgo` 仍然补全 `nsgo` 的命令，而 `nsgo game` 补全游戏层的命令。fish 脚本为 `nsgo` 添加的补全只在 `nsgo game` 之后生效，与 `nsgo` 自己的补全并存。生成脚本要运行命令，所以把脚本保存为文件一次即可，不要在每次启动 shell 时生成。

zsh：把脚本保存为 `$fpath` 中某个目录下的 `_game`，compinit 会为两个命令都注册它，并在 `~/.zshrc` 中写：

```zsh
fpath=(~/.zfunc $fpath)
autoload -Uz compinit && compinit
```

替换掉在支持 `nsgo game` 之前生成的脚本后，删除 `~/.zcompdump`，让 compinit 读取新的注册。

bash：保存到 bash-completion 的目录。它在第一次补全某个命令时加载该命令的补全，所以脚本要以两个名字保存；没有安装 bash-completion 时，在 `~/.bashrc` 中 source 这个文件：

```sh
mkdir -p ~/.local/share/bash-completion/completions
go tool game completion bash > ~/.local/share/bash-completion/completions/game
ln -s game ~/.local/share/bash-completion/completions/nsgo
```

fish：保存到它的补全目录，fish 从以命令命名的文件中加载该命令的补全：

```sh
go tool game completion fish > ~/.config/fish/completions/game.fish
ln -s game.fish ~/.config/fish/completions/nsgo.fish
```

如果那里已有 `nsgo` 自己的补全，就不要创建 `nsgo` 的链接。想单独输入 `game` 时，用 `go install github.com/nsgo-dev/game/cmd/game@latest` 安装命令，或者把 `game` 定义为运行 `go tool game` 的 shell 函数，这样在模块内它沿用 `go.mod` 固定的版本：zsh 和 bash 中写 `game() { go tool game "$@"; }`，fish 中写 `function game; go tool game $argv; end`。
# 模板残留警告

当 `game.yaml` 记录了 `template` 类型的来源时，`game check` 会将项目与内置模板基线比较。如果规则或数据文件、项目名称或 `README.md` 仍与模板完全相同，就会报告建议性警告。只有警告时命令仍以成功状态退出。若确实要原样复制，可将发现名称（`rules`、`data`、`name`、`readme` 或 `credits`）加入 `origin.keep`。
