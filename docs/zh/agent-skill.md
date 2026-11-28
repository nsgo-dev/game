# 智能体技能

本仓库在 [`skills/nsgo-game`](../../skills/nsgo-game/SKILL.md) 发布了一个智能体技能（Agent Skill），让编程智能体了解如何通过 `go tool game` 命令行工具、纯文本 YAML 格式和 `game/mcp` 工具集来创建、构建、测试、检查并驱动 NSGo 游戏。

## 安装

使用 `bunx skills add` 安装该技能：

```sh
bunx skills add nsgo-dev/game
```

建议同时安装 NSGo 自身的 `nsgo` 技能，以涵盖核心 UI 组件、signal、`nsgotest` 与 `go tool nsgo`：

```sh
bunx skills add nsgo-dev/nsgo
```

如需让智能体实时检查并驱动运行中的游戏，还需注册 NSGo 的 MCP 服务器：

```sh
claude mcp add nsgo -- go tool nsgo mcp
```

## 技能内容

- **[`SKILL.md`](../../skills/nsgo-game/SKILL.md)**：项目核心规则（纯 Go、带版本号的纯文本 YAML 格式、`go tool nsgo check`/`test`、稳定性分级）、快速上手的 CLI 流程、最小 `game.Loop` + `mcp.Attach` 接入示例，以及确定性 MCP 验证循环。
- **[`references/packages.md`](../../skills/nsgo-game/references/packages.md)**：`github.com/nsgo-dev/game` 下所有包的分组目录、导入路径与稳定性状态，与[API 稳定性](stability.md)保持一致。
- **[`references/cli-and-formats.md`](../../skills/nsgo-game/references/cli-and-formats.md)**：全部 `go tool game` 子命令（[命令参考](cli-reference.md)）、配套模块工具、YAML 格式契约（[文件格式](formats.md)）、JSON Schema（[YAML 文件的 Schema](schemas.md)）及直接读取的外部工具格式（Tiled、LDtk、Aseprite、TexturePacker、BMFont、glTF、Yarn）。
- **[`references/mcp-tools.md`](../../skills/nsgo-game/references/mcp-tools.md)**：`mcp.Bridge` 接入方法（[mcp](mcp.md)）与完整工具目录，覆盖场景/实体检查、固定步长循环控制、游戏坐标输入、实时生成与组件编辑、带诊断叠加层的截图、调试控制台可调参数、输入录制与回放、性能读数以及资源热重载。

## 典型智能体工作流

1. 使用 `go run github.com/nsgo-dev/game/cmd/game@latest new` 创建项目，并用 `go tool game generate`、`go tool game assets` 与 `go tool game check` 搭建组件并校验资源和引用。
2. 在游戏的 `*core.Scope` 上挂载 `mcp.Bridge`，并运行 `go tool nsgo dev -automation`（或在测试中使用 `nsgotest.Start`）。
3. 通过 MCP 执行可复现验证：调用 `game_loop_pause` 暂停、`game_loop_seed` 设定随机种子、注入输入或生成实体、调用 `game_loop_step` 同步推进步数、通过 `game_entities_list` / `game_entity_inspect` / `game_component_get` 检查实体状态，并用 `game_screenshot` 截取叠加层画面。

处理商店媒体时，让 `store/capture.yaml`（[商店截图计划](shipping.md#商店截图计划)）始终记录每张截图的场景、录像和帧，用 `game_screenshot` 按计划写明的尺寸截取每张图片，并运行 `go tool game capture`，直到它不再报告缺失或尺寸不对的文件。起草商店文案时只使用 `game.yaml`、输入绑定和 `changelog.d` 片段中的事实。

编辑器 MCP 服务器还通过 `skill://nsgo-game/...` 提供当前版本的技能和参考文档；客户端无法读取 MCP 资源时使用 `skill_get`。服务器副本优先于本地较旧的安装副本。
