# 命令行

`game` 是游戏层的命令行工具：一个命令，由子命令打包精灵图集、转换音频、生成资源清单、编码纹理和打开场景编辑器。子命令的调用方式一致：参数标志写在位置参数之前，读取的内容用 `-in`，写出的内容用 `-out`，帮助也一样。

## 安装

把命令作为工具加入你的模块，然后用 `go tool game` 运行：

```sh
go get -tool github.com/nsgo-dev/game/packages/game/cmd/game
go tool game help
```

`go.mod` 里的 `tool` 一行把命令固定在模块所用的游戏层版本上，参与这个游戏的每个人运行的都是同一个命令。

## 命令

| 命令 | 作用 |
| --- | --- |
| `pack` | 把一个文件夹的 PNG 图片打包成精灵图集页面和供 `sprite.LoadAtlas` 读取的 TexturePacker JSON-hash 元数据 |
| `audio` | 对 WAV 文件做归一化、重采样，并转换为 WAV、AAC 或 MP3 |
| `manifest` | 从资源目录生成带类型的 Go 资源清单，缺少资源时构建失败 |
| `tex` | 把 PNG 或 JPEG 纹理编码为 BC1、BC3、ETC2 或 RGBA8 格式的 KTX2 容器，可选生成 mipmap |
| `editor` | 在场景编辑器中打开一个 YAML 场景，未指定时打开演示场景 |
| `help` | 列出所有命令，或显示某个命令的参数 |

`go tool game help` 列出所有命令，`go tool game help <command>` 或 `go tool game <command> -h` 显示一个命令的用法行、作用和参数。

命令成功或显示帮助时退出码为 0；执行失败（读不到文件、找不到编码器、清单已过期）时为 1；调用方式有误（未知的命令或参数、缺少必需参数、多余的位置参数）时为 2，并同时打印该命令的用法。错误信息以命令名开头，例如 `game pack: -in directory is required`。

### pack

```sh
go tool game pack -in art/ui -out assets -name ui -padding 2
```

把 `ui.png` 和 `ui.json` 写到 `-out`（帧放不下一页时改为 `ui-0.png`、`ui-0.json`、`ui-1.png` 等），并逐行打印路径。`-padding` 和 `-extrude` 设置每帧四周的透明间隔和重复的边缘像素，`-max-width` 和 `-max-height` 限制单页尺寸，`-scale` 写入 `meta.scale`，默认裁掉透明边框并记录每帧在未裁剪尺寸中的偏移，`-trim=false` 时保留边框。

### audio

```sh
go tool game audio -in sfx/jump.wav -out assets/jump.wav -rate 22050 -channels 1 -peak-db -1
go tool game audio -in music/theme.wav -out assets/theme.m4a -format aac -rms-db -14 -bitrate 192
```

读取 WAV 文件，重采样到 `-rate`，混合为 `-channels` 个声道，并按峰值（`-peak-db`）或响度（`-rms-db`，带软削波）归一化，两者只能选一。WAV 输出由 Go 编写；AAC 和 MP3 需要 `PATH` 上有 `ffmpeg`（macOS 上 AAC 也可用 `afconvert`），否则报错并指出缺少的编码器。

### manifest

```sh
go tool game manifest -in assets -out assets/manifest_gen.go -pkg assets
go tool game manifest -in assets -out assets/manifest_gen.go -pkg assets -check
```

为 `-in` 下的每个资源写出一个带类型引用的 Go 文件，`-out` 为空时打印到标准输出。`-embed` 生成 `//go:embed` 指令而不是 `asset:` URI，`-embed-dir` 加在这些指令的模式之前。`-check` 不修改 `-out`，只在它与将要生成的内容不同时失败，供 CI 使用；用一行 `go:generate` 运行第一种写法即可让文件保持最新。

### tex

```sh
go tool game tex -format bc1 -mipmaps -srgb -out assets/albedo.ktx2 art/albedo.png
```

把位置参数（或 `-in`）指定的图片编码为 `-out`（简写 `-o`）处的 KTX2 文件，默认路径是输入路径换成 `.ktx2` 扩展名。`-format` 可选 `bc1`、`bc3`、`etc2` 或 `rgba8`，`-mipmaps` 写出直到 1x1 的完整 mipmap 链，`-srgb` 把纹理标为 sRGB，并在线性光空间中过滤 mipmap。

### editor

```sh
go tool game editor scenes/level1.yaml
```

在窗口中用 NSGo 场景编辑器打开指定的场景文件，未指定时打开一个小型演示房间。场景格式见[编辑器与场景格式](editor.md)。
