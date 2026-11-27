# 插件

Go 插件是用于扩展游戏运行时、场景编辑器或 `game` 命令行工具的 Go 模块。由于 iOS 和 Web 目标要求所有原生代码在编译期静态链接，Go 插件是以普通 Go 包的形式通过 `go.mod` 接入游戏项目的，而不是在运行时动态加载共享库。

```go
import (
	"github.com/nsgo-dev/game/pkg/plugin"
	"github.com/nsgo-dev/game/pkg/plugin/manifest"
)
```

## 运行时插件（`pkg/plugin`）

插件的运行时 Go 包导出一个返回 `plugin.Plugin` 的构造函数（约定为 `func Plugin() plugin.Plugin`），游戏通过 `plugin.Install` 将所有启用的插件安装到 `plugin.Host` 上。`pkg/plugin` 不导入 `pkg/editor`、`pkg/mcp` 或 `pkg/playtest`，确保发布的二进制只链接实际用到的运行时包。

```go
func Plugin() plugin.Plugin {
	return plugin.Plugin{
		Name:   "health",
		Module: "example.com/healthkit",
		API:    plugin.APIVersion,
		Components: []sceneformat.ComponentType{
			Type(), // 包含 Fields、Events、Handlers 及可选的 Spawn
		},
		Systems: []plugin.System{
			{Name: "health.regen", Stage: plugin.StageUpdate, Fn: regenSystem},
		},
		Codecs: func(w *entities.World) error {
			return entities.RegisterCodec[Stats](w, "health")
		},
	}
}
```

### 安装到 `Host`

`plugin.Host` 聚合游戏希望插件填充的注册表；任意为 `nil` 的字段都会跳过对应类别的贡献：

- `Host.Components`（`*sceneformat.ComponentRegistry`）：按插件参数顺序与切片顺序合并每个插件的 `Components`（保留 `ComponentType.Spawn`）。
- `Host.Scheduler`（`*entities.Scheduler`）：为每个调度器一次性添加四组标准阶段锚点，随后按插件与切片顺序注册各插件的 `Systems`。
- `Host.DataTables`（`*datatable.Registry`）：按文件路径模式排序依次注册每个插件的 `RowTypes`。
- `Host.Console`（`*debug.Console`）：按切片顺序注册每个插件的 `Console` 命令，拒绝与内置命令或已有命令重名。
- `Host.OnWorld(w)` 按安装顺序在 `w` 上执行每个已安装插件的 `Codecs` 回调。每次游戏创建新的 `entities.World`（包括从存档恢复的新世界）时都应调用 `OnWorld`。
- `Host.Plugins()` 返回已安装插件的元数据（`Name`、`Module` 以及通过 `runtime/debug.ReadBuildInfo` 读取的 `Version`；本地构建或无版本号的 `replace` 指令返回 `"(devel)"`）。`Host.Has(name)` 报告插件是否已安装。

```go
host := &plugin.Host{
	Components: sceneformat.NewComponentRegistry(),
	Scheduler:  entities.NewScheduler(),
	DataTables: datatable.NewRegistry(),
	Console:    debug.NewConsole(),
}
if err := plugin.Install(host, health.Plugin()); err != nil {
	return err
}

world := entities.NewWorld()
if err := host.OnWorld(world); err != nil {
	return err
}
```

### 校验、确定性与进程全局注册表

- **注册前校验**：`Install` 会先检查 `Plugin.API == plugin.APIVersion`、校验插件名与模块路径、拒绝重复插件名，并确认所有组件、系统、像素特效、数据表模式与控制台命令名均未被占用，之后才会写入任何注册表。出现任何冲突或校验错误时，不会产生半成品注册。
- **确定性顺序**：按参数顺序处理插件、按声明顺序处理切片字段（`Components`、`Systems`、`Console`）、按排序后的键顺序处理映射（`PixelFX`、`RowTypes`）。`Plugin.Install` 逃生舱（`func(*Host) error`）在所有声明式贡献提交之后最后执行。
- **进程全局幂等（`PixelFX`）**：`pixelfx` 特效保存在进程全局注册表中。`Install` 会记录每个插件注册特效的归属 `(Name, Module)`，因此在第二个测试或编辑器重载后重复安装同一插件时会自动跳过而不会 panic；若其他插件或内置特效已占用该 ID，则在写入任何状态前明确报错。
- **不设 `Version` 字段**：与 `plugin.yaml` 一致，`plugin.Plugin` 不包含 `Version` 字段，版本号统一通过 `plugin.ModuleVersion(modulePath)` 从 `go.mod` 构建信息读取。`plugin.APIVersion` 仅在插件契约发生破坏性变更时递增。

### 标准调度阶段

第三方系统通常希望在不了解宿主游戏内部系统名称的前提下插入到确定阶段。`plugin.Install`（或 `plugin.EnsureStages(sched)`）会为每个 `*entities.Scheduler` 一次性添加四组固定顺序的阶段锚点：

1. `plugin.StageInput`（`"input"`）——读取输入并转换玩家意图。
2. `plugin.StageUpdate`（`"update"`）——常规玩法逻辑系统。
3. `plugin.StagePhysics`（`"physics"`）——物理、移动与碰撞解算。
4. `plugin.StageLate`（`"late"`）——相机跟随、变换同步与收尾清理。

设置 `System.Stage` 会自动为系统添加 `entities.After(stage.Start())` 与 `entities.Before(stage.End())` 约束。同一阶段内的系统默认按注册顺序执行，也可通过 `Before` 和 `After` 进一步约束。游戏自身的 `Scheduler.AddSystem` 调用同样可以引用 `stage.Start()` 与 `stage.End()`。

### 开发期贡献（`pkg/plugin/devtools`）

MCP 桥接预制体（`pkg/mcp`）与自动试玩机器人（`pkg/playtest`）依赖较重的检查与回放包，发布版游戏二进制不应链接这些代码（见[包体积预算](package-size-budgets.md)）。这些贡献拆分在独立的 `github.com/nsgo-dev/game/pkg/plugin/devtools` 包中：

```go
import "github.com/nsgo-dev/game/pkg/plugin/devtools"

devHost := &devtools.Host{Bridge: bridge}
if err := devtools.Install(devHost, devtools.Plugin{
	Name:   "healthkit",
	Module: "example.com/healthkit",
	API:    plugin.APIVersion,
	Prefabs: []devtools.PrefabSpec{
		{Prefab: patientPrefab},
	},
	Bots: map[string]playtest.BotFactory{
		"healthkit-medic": newMedicBot,
	},
}); err != nil {
	return err
}
```

`devtools.Install` 遵循同样的 API 版本校验、原子化注册前冲突检查、确定性顺序，以及 `playtest` 机器人注册表的进程全局归属规则（阻止覆盖内置或其他插件的机器人，同时允许同一插件幂等重装），且不导入 `pkg/editor`。

## `plugin.yaml` 清单

每个 Go 插件都在其 Go 模块根目录放置一份 `plugin.yaml`。命令行工具、资源与插件库索引以及编辑器会在编译任何代码之前读取 `plugin.yaml`，以获知插件提供的 Go 入口包、兼容的引擎版本与目标平台，以及所依赖或冲突的其他插件模块。

```yaml
manifestVersion: 1
name: healthkit
module: example.com/healthkit
description: Health component, ward panel and admit tool
license: MIT
authors: [Jane Doe]
repository: https://github.com/example/healthkit
engine: '>=0.4.0 <0.5.0'
api: 1
platforms: [all]
entries:
  runtime: example.com/healthkit/health
  editor: example.com/healthkit/healtheditor
  cli:
    - name: healthkit
      package: example.com/healthkit/cmd/game-healthkit
requires:
  example.com/navkit: ^0.3.0
conflicts: [example.com/oldhealth]
contributes:
  components: [health]
  editor:
    panels: [ward]
    tools: [admit]
    commands: [heal-all]
  agentTools: [plugin.healthkit.heal_all]
```

### 为什么没有 `version` 字段

插件的版本就是其 Go 模块版本，由游戏项目的 `go.mod` 解析并由 `go.sum` 记录。如果在 `plugin.yaml` 中再写一份版本号，必然会与 Git 标签和 `go.mod` 产生漂移，因此 `plugin.yaml` 刻意不设 `version` 字段，并将其作为未知字段拒绝。

出于同样原因，`requires` 和 `conflicts` 以 Go 模块路径（例如 `example.com/navkit`）而非短名称作为键，使依赖解析与冲突检查能够直接对应 `go.mod` 中解析出的模块。

## 字段说明

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `manifestVersion` | 是 | `plugin.yaml` 的模式版本。必须写为纯十进制整数 `1`（拒绝 `1.0` 和 `0x1`）。 |
| `name` | 是 | 插件短名称：1 到 64 个小写 ASCII 字母、数字或 `-`，且以字母或数字开头（与资源包名称规则一致）。 |
| `module` | 是 | 插件的 Go 模块路径，必须与插件 `go.mod` 中的 `module` 行一致。 |
| `description` | 否 | 单行简介，不含控制字符。 |
| `license` | 否 | 单行许可证标识，例如 `MIT` 或 `Apache-2.0`。 |
| `authors` | 否 | 非空单行作者名称列表，不允许重复。 |
| `repository` | 否 | 插件源码仓库的 `http://` 或 `https://` URL。 |
| `engine` | 是 | 对 `github.com/nsgo-dev/game` 的语义化版本约束，例如 `>=0.4.0 <0.5.0` 或 `^0.4.0`。 |
| `api` | 是 | 插件所针对的正整数 `plugin.APIVersion`（例如 `1`）。 |
| `platforms` | 否 | 单独的 `[all]`，或 `macos`、`windows`、`linux`、`ios`、`android`、`web` 的子集。省略时表示支持全部平台。 |
| `entries` | 是 | 位于 `module` 内部的 Go 包导入路径。`entries.runtime`、`entries.editor` 与 `entries.cli` 至少须设置一项。 |
| `entries.runtime` | 否 | 提供运行时入口（`func Plugin() plugin.Plugin`）的 Go 包，必须等于 `module` 或位于 `module/` 之下。 |
| `entries.editor` | 否 | 提供编辑器入口（`func Plugin() editor.Plugin`）的 Go 包，必须等于 `module` 或位于 `module/` 之下。 |
| `entries.cli` | 否 | 通过 `go.mod` `tool` 指令扩展 `game` 命令行工具的 `{name, package}` 条目列表。每个 `name` 遵循插件名称规则且不可重复；每个 `package` 必须等于 `module` 或位于 `module/` 之下。 |
| `requires` | 否 | 所依赖插件的 Go 模块路径到语义化版本约束（如 `^0.3.0`）的映射。键必须是合法 Go 模块路径，不能填写自身 `module`，且与 `conflicts` 互不相交。 |
| `conflicts` | 否 | 不能与本插件同时安装的插件 Go 模块路径列表。条目必须是合法且不重复的 Go 模块路径，不能填写自身 `module`，且与 `requires` 互不相交。 |
| `contributes` | 否 | 插件注册符号的索引（`components`、`editor.panels`、`editor.tools`、`editor.commands` 和 `agentTools`），用于目录索引并与实际代码比对。 |

## 严格解析与规范编码

`pkg/plugin/manifest` 不依赖 `pkg/editor`，可供命令行工具、运行时代码和索引器直接导入：

- `manifest.Parse(data)` 严格解码单个 YAML 文档，拒绝未知字段、重复键和尾随文档，要求 `manifestVersion` 与 `api` 为纯十进制整数，并校验所有字段。返回的错误可通过 `errors.Is` 匹配 `manifest.ErrInvalid`，同时为 `*loaderr.Error`，包含文件、行号、字段路径与修复建议。
- `manifest.ParseFS(fsys, dir)` 从 `fsys` 的 `dir` 目录读取并解析 `plugin.yaml`，并将文件路径附加到任何 `*loaderr.Error` 上。
- `manifest.Encode(m)` 校验 `m`（若 `ManifestVersion` 为 `0` 则填入 `1`）并写出确定性的 2 空格缩进 YAML：所有列表及 `requires` 映射键均按字节序排序，且不会修改调用方传入的切片。
- `(Manifest).Validate()` 校验代码中构造的清单。
- `(Manifest).SupportsPlatform(platform)` 报告清单是否支持指定平台（`macos`、`windows`、`linux`、`ios`、`android`、`web`）。

```go
m, err := manifest.ParseFS(os.DirFS(pluginDir), ".")
if err != nil {
	return err
}
if !m.SupportsPlatform("web") {
	return fmt.Errorf("plugin %s does not support web", m.Name)
}
```

## JSON Schema

`plugin.yaml` 的已发布 JSON Schema 位于 `https://game.nsgo.dev/schemas/plugin.v1.json`（`manifest.SchemaURL`）。可在 `plugin.yaml` 首行添加 Schema 注释（`manifest.Parse` 会忽略注释），或在编辑器的 YAML 设置中将 `plugin.yaml` 映射到该 Schema，以获得补全与即时校验：

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/plugin.v1.json
```
