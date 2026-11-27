# 资源与插件库

`github.com/nsgo-dev/game/pkg/tooling/library` 在库中查找共享的资源包、编辑器插件包和模板，并把它们安装到游戏项目里。库是一份任何人都能通过拉取请求添加包的文本索引；安装一个包会把它的文件写入项目，并把这些文件以及它们新增的引用记录在 `game.yaml` 中。`go tool game library` 从命令行运行它，场景编辑器的库面板从编辑器里运行它。

## 模组、插件与包（#1268）

引擎有三种扩展游戏的方式，它们不能互相替代：

| | 编辑器（Go）插件 | 脚本模组 | 库包 |
| --- | --- | --- | --- |
| 用什么写 | Go | TSX | YAML 与媒体文件 |
| 谁来写 | 游戏的开发者、第三方工具作者 | 玩家 | 任何人 |
| 何时加载 | 编译进编辑器程序 | 运行时加载，支持热重载（`Manager.Reload`） | 安装进项目 |
| 信任级别 | 完全信任 | 沙箱隔离，按能力授权 | 不含代码 |
| 平台 | 全部，包括 iOS 和 Web | 原生平台；Web 仅在设置 `ManagerOptions.WebUnisolated` 后可用 | 全部 |
| 扩展编辑器 | 可以 | 不能（目前） | 只能启用内置插件 |

编辑器插件是编辑器程序用 `Editor.RegisterPlugin` 注册、项目再在 `game.yaml` 的 `editor.plugins` 下按名字启用的 `editor.Plugin`；见[编辑器插件](editor.md#编辑器插件492)。脚本模组是一份 `mod.yaml` 清单加一个 TSX 入口脚本，由 `modding.Manager` 在沙箱中运行，能做什么仅限于清单申请、宿主授予的能力；见 [modding](modding.md)。库包是 `library.Install` 写入项目并记录在 `game.yaml` 中的一组文件，本页详细说明。包不得包含 Go 源码或模块文件（`.go`、`go.mod` 等都会被拒绝），所以插件包只能启用编辑器程序编译时已包含的插件，而不会携带它们的代码。

该选哪一种？

- 给编辑器加新的面板、检查器、视口工具或 3D 操纵杆，或让编辑器按某些组件类型检查场景：用编辑器插件，见[编辑器插件](editor.md#编辑器插件492)。
- 让玩家在游戏设定的限度内改动已发布的游戏：用脚本模组，见 [modding](modding.md)。
- 分享精灵图、场景、声音或起步模板：用库包，见下文。

Go 插件编译进程序而不是在运行时加载，是因为 Go 没有跨平台的运行时加载代码的手段——iOS 和 Web 都不能在运行时加载原生代码——所以插件随它所扩展的编辑器命令一起发布。

## 查找与安装包

```sh
go tool game library list
go tool game library list tiles
go tool game library -kind plugin list
go tool game library install dungeon-tiles
go tool game library remove dungeon-tiles
```

`list` 列出名称、描述或标签包含所有给定词的包，并显示当前目录（或 `-project` 指定目录）中的项目已安装的版本：

```text
PACK           KIND   VERSION  INSTALLED  DESCRIPTION
dungeon-tiles  asset  1.0.0    -          A 16x16 stone floor tile and brick wall tile with a TexturePacker atlas.
```

`install` 获取每个指定的包，按索引校验后安装到项目中；`-dir` 把包的文件放到项目里另一个目录，而不是包清单指定的目录；之后再次安装该包以更新它时，除非 `-dir` 指定了别的目录，否则仍保留在这个目录中。`remove` 把已安装的包移除。除非 `-index` 指定了其他库，否则使用本仓库 [`library`](https://github.com/nsgo-dev/game/tree/main/library) 目录中的共享库：`-index` 可以是索引文件的 https URL、索引文件，或包含 `index.yaml` 的目录。

## 安装做了什么

把示例资源包安装到项目中，会把它的文件写到 `packs/dungeon-tiles` 下（包括包的 `pack.yaml`，这样许可证随文件一起保留），并在 `game.yaml` 中加入：

```yaml
autoload:
  resources:
    - name: dungeon-tiles
      path: packs/dungeon-tiles/tiles.json
packs:
  - name: dungeon-tiles
    kind: asset
    packVersion: 1.0.0
    sha256: 69d89c016fc8eaf19a83ea7bf3f14800fc4b6fff1384cd4d9f2db29f6e5ab177
    dir: packs/dungeon-tiles
    files:
      - pack.yaml
      - tiles.json
      - tiles.png
    resources: [dungeon-tiles]
```

`packs` 条目是[项目文件](project.md)对这次安装的记录：包的版本和摘要、写入的目录和文件，以及它新增的自动加载 `scenes`、`resources` 和编辑器 `plugins`。插件包会把它列出的编辑器插件加入 `editor.plugins`，在内置了这些插件的编辑器中启用它们（见[编辑器插件](editor.md#编辑器插件492)）；项目原本已启用的插件仍归项目所有，不会记为包的插件。

安装不会覆盖项目中已有的文件，除非该文件是这个包写入的，或内容与包要写入的完全相同；也不会覆盖其他包写入的文件；自动加载的场景或资源名称与项目已有名称冲突时会拒绝安装。它在写入任何内容之前完成全部检查，写入失败时会恢复已改动的文件。随后 `game.yaml` 以规范格式写出，与编辑器保存时一样，不保留注释。

包只能写到资源应在的位置。它的文件写入的目录和它列出的每个文件都必须位于项目内、且不在隐藏目录中（路径的任何一段都不能以 `.` 开头，因此包无法写入 `.github` 或 `.git`），名称必须是所有平台都能保存的：不含控制字符或 `<>:"|?*` 中的任何字符，任何一段都不以点或空格结尾，不是 `con`、`aux` 这类 Windows 设备名，也不能有两个文件的名称只有大小写不同。包不能包含 Go 源码或 Go 工具链会编译或读取的其他文件（`.go`、`.s`、`.c`、`.h`、`.cc`、`.cpp`、`.m`、`.syso` 等，以及 `go.mod`、`go.sum`、`go.work`），因此安装包永远不会改变编译进游戏的内容；编辑器插件包启用的是编辑器构建时已包含的插件，而不是附带插件的代码。

## 更新与移除

安装一个项目中已有的包就是更新它，更新发生在已安装版本所在的目录中：新版本没有的文件被删除，其余文件被替换（两个版本之间文件可以变成目录，目录也可以变成文件），它新增的自动加载条目和插件被替换为新版本的，并保持在 `game.yaml` 中原来的位置。再次安装同一个版本不会有任何改动，也不写任何文件。移除包会删除它写入的文件（无论是否被修改过）、包目录内因此变空的目录（对于位于默认 `packs/<name>` 的包，还包括 `packs` 本身），但绝不删除包目录之上、项目在安装前可能就已有的目录，以及它新增的自动加载条目和插件；如果主场景是它的场景，会取消主场景设置。`game project check` 和 `project.Check` 会报告已安装包写入但已缺失的文件。

## 在编辑器中

`go tool game editor -library <索引> <项目>` 会在资源浏览器旁显示库面板，列出库中每个包的类型、版本、描述以及打开的项目已安装的版本。Install（安装）、Update（更新）和 Remove（移除）与命令行的作用相同，之后会保存 `game.yaml`，因此项目有未保存的修改时会被拒绝，请先保存或撤销这些修改。安装写入的文件无法通过撤销历史恢复，所以安装后会清空撤销历史，与打开项目时一样。从 https 库获取包时不会阻塞编辑器；包到达之前面板的按钮不可用，打开另一个项目会取消这次获取。在 Go 中，把 `Editor.Library` 设为 `*library.Library`，并调用 `Editor.InstallPack` 和 `Editor.RemovePack`。

## 索引

```yaml
version: 1
packs:
  - name: dungeon-tiles
    kind: asset
    packVersion: 1.0.0
    description: A 16x16 stone floor tile and brick wall tile with a TexturePacker atlas.
    tags: [2d, tiles, dungeon, pixel-art]
    license: CC0-1.0
    source: packs/dungeon-tiles
    sha256: 69d89c016fc8eaf19a83ea7bf3f14800fc4b6fff1384cd4d9f2db29f6e5ab177
```

| 字段 | 内容 |
| --- | --- |
| `version` | 索引 schema 版本，`1`。 |
| `name` | 包名：1 到 64 个小写 ASCII 字母、数字和 `-`，以字母或数字开头；在索引中唯一。 |
| `kind` | `asset`、`plugin` 或 `template`。 |
| `packVersion` | 索引提供的包版本：不带前导 `v` 的严格语义化版本（`MAJOR.MINOR.PATCH[-prerelease][+build]`），例如 `1.0.0`。 |
| `description` | 一行文字说明包的内容。 |
| `tags` | 搜索也会匹配的小写词。 |
| `license` | 包中文件的许可证，以 SPDX 表达式书写，如 `CC0-1.0` 或 `MIT OR Apache-2.0`。 |
| `source` | 包的目录：相对于索引、不离开库的斜杠分隔路径，或 https URL（纯 http 只接受回环地址）。 |
| `sha256` | 包的摘要（见下文）。 |

`library.ParseIndex` 拒绝未知字段、其他版本和重复名称，`EncodeIndex` 写出 `ParseIndex` 读回后不变的格式。获取到的包如果清单中的名称、类型或版本与索引条目不同，或摘要与 `sha256` 不同，会被拒绝。

## 包

包是一个目录，包含清单 `pack.yaml` 以及清单列出的文件：

```yaml
version: 1
name: dungeon-tiles
kind: asset
packVersion: 1.0.0
description: A 16x16 stone floor tile and brick wall tile with a TexturePacker atlas.
license: CC0-1.0
files:
  - tiles.json
  - tiles.png
autoload:
  resources:
    - name: dungeon-tiles
      path: tiles.json
```

| 字段 | 内容 |
| --- | --- |
| `version` | 清单 schema 版本，`1`。 |
| `name`、`kind`、`packVersion`、`description`、`license` | 与索引相同，索引会重复这些字段。 |
| `into` | 包的文件写入的项目目录；省略时为 `packs/<name>`。 |
| `files` | 除 `pack.yaml` 以外的包文件，以包目录内的斜杠分隔路径书写。未列出的文件不属于这个包。 |
| `autoload` | `files` 中安装时加入 `game.yaml` 自动加载部分的 `scenes`（`.yaml` 或 `.yml`）和 `resources`，每项带有自动加载时使用的 `name`。 |
| `plugins` | 插件包启用的编辑器插件；插件包至少列出一个，其他类型的包不能列出。 |

包的摘要是对其文件（包括 `pack.yaml`）按字节顺序排列后 `sha256sum` 输出的清单再取 SHA-256：每个文件一行，依次是小写十六进制的 SHA-256、两个空格和路径。`go tool game library digest <包目录>` 会打印它。

## 向共享库添加包

1. 在 `library/packs/<name>` 下添加包的目录，包括 `pack.yaml` 和文件。每个文件都必须是你有权按清单所写许可证共享的。
2. 在仓库根目录运行 `go run ./cmd/game library digest library/packs/<name>`，并在 `library/index.yaml` 中添加带有该 `sha256` 的条目。
3. 提交拉取请求。库的测试会获取索引中的每个包，因此摘要或清单与条目不一致会让 CI 失败。

团队也可以在任何能通过 https 提供文件的地方，或在自己仓库的某个目录中维护自己的库，并用 `-index` 指向它。纯 http 只接受回环地址（如本地测试服务器），索引和包的 `source` 都是如此，每个文件限制为 64 MiB。服务器 30 秒内连不上或 1 分钟内没有响应，或某个文件 10 分钟内没有下载完，都会放弃。

## 在 Go 中使用

`library.Open(ctx, location)` 读取索引，`Library.Fetch(ctx, name)` 获取并校验包；`Index.Search(kind, query)` 查找包。`library.Install(projectDir, pack, dir)` 和 `library.Remove(projectDir, name)` 修改项目目录。`PlanInstall` 和 `PlanRemove` 根据 `fs.FS` 和 `project.File` 计算同样的改动而不写入，`Change.Apply` 负责写入。
