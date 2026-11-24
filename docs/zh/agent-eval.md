# 智能体评测套件

位于 `tools/agenteval/` 下的智能体评测套件是面向贡献者的工具，包含一组固定的游戏开发任务。每个任务有一个起始工作区、一份手写的参考解，以及一套评分标准：用 YAML 检查、`assetcheck`、`nsgo check` 和无头实时 `game/mcp` 工具给工作区打 0 到 100 分。定时 CI 任务评分的是参考解，因此它是针对这些参考解所用游戏 API、YAML 格式与 MCP 工具的回归套件。让编程智能体在起始工作区上作答是另一种需要显式启用的模式，CI 不运行智能体。

## 任务列表

每个任务是 `tools/agenteval/tasks/` 下带版本号（`version: 1`）的纯文本 YAML 文件，拒绝未知字段并支持确定性往返编码：

| 任务 ID | 目标 | 评分检查内容 |
| --- | --- | --- |
| `platformer-level` | 构建平台跳跃关卡 | 有效的 `game.yaml` 与 `scenes/level1.scene.yaml`、`assetcheck.Check`、从 `player_spawn` 到 `goal` 的平台链（水平间隙不超过 `max_gap`、垂直落差不超过 `max_rise`）、`nsgo check`，以及实时 MCP 校验：通过 `game_input_action` 按住 `right` 动作后，玩家在 60 个固定步长内向终点移动 |
| `add-enemy-type` | 添加一种敌人类型 | 有效的 `game.yaml` 与 `scenes/arena.scene.yaml`、`assetcheck.Check`、带 `enemy` 组件的敌人对象（`prefab: enemy`、`enemy` 组件或 `tag: enemy`）、`nsgo check`，以及实时 MCP 校验带 `"enemy"` 标签的实体在 60 个固定步长内移动 |
| `add-menu` | 添加菜单 | 有效的 `game.yaml`、`scenes/menu.scene.yaml` 与 `scenes/gameplay.scene.yaml`、`assetcheck.Check`、含至少两个按钮（`prefab: button` 或 `button` 组件）的可见屏幕空间图层、`nsgo check`，以及实时 MCP 校验 `game_scenes_list` 与 `game_layers_list`，再通过 `game_input_pointer` 点击 Play，必须切换到 `gameplay` |

## 评分标准

每个任务在 `checks.weights` 中把 100 分分配到五项检查：

1. **`yaml_valid`**（15 分）：列出的 `game.yaml` 与 `*.scene.yaml` 均可解析，并经 `project.Encode` 或 `sceneformat.Encode` 确定性往返。
2. **`project_check_passes`**（15 分）：`assetcheck.Check` 在场景、地图与引用上报告零问题。
3. **领域检查**（`level_geometry_valid`、`enemy_definition_present` 或 `menu_scene_valid`，25 分）：任务的场景几何、敌人定义或菜单按钮。
4. **`builds_cleanly`**（15 分）：工作区针对本仓库检出的 `packages/game` 执行 `go tool nsgo check` 成功。
5. **`mcp_verify_passes`**（30 分）：生成的测试在 `nsgotest.Start` 下无头启动游戏，通过 `nsgo/automation.Call` 调用实时 `game/mcp` 工具，并且必须报告通过；进程正常退出但没有通过记录不得分。

任务在 `context_docs` 中列出的文档不计分。它们会被复制到工作区旁供智能体阅读，缺失或为空时整次运行失败。

`min_score` 设置任务的及格分；不写时为 100，显式写 `0` 会保留为 0。

## 本地运行

### 参考模式（默认）

用本仓库检出评分每个任务的参考解，不需要模型或 API 密钥：

```sh
go run ./tools/agenteval
```

输出 JSON 报告与 Markdown 摘要：

```sh
go run ./tools/agenteval -out agent-eval-report.json -summary summary.md
```

只运行部分任务或调整及格分：

```sh
go run ./tools/agenteval -task platformer-level,add-menu -min-score 80
```

### 命令模式（运行编程智能体）

把每个任务的起始工作区与上下文文档复制到临时目录，把提示词写入 `$AGENTEVAL_PROMPT_FILE`，在工作区中执行 `-agent-cmd`，再用同一套标准为智能体留下的结果评分：

```sh
go run ./tools/agenteval \
  -mode=command \
  -min-score=80 \
  -agent-timeout=20m \
  -agent-cmd='my-agent --prompt "$AGENTEVAL_PROMPT_FILE"'
```

智能体会拿到一个临时 `go.work`（在 `GOWORK` 中），它把 `packages/game` 解析到本仓库检出，因此智能体工作时可以运行 `go tool nsgo check` 与 `go tool nsgo test .`。智能体拿不到指向仓库的路径，因为仓库的 fixtures 里有参考解。`-agent-timeout`（默认 15 分钟）限制智能体，`-timeout`（默认 5 分钟）限制评分，因此智能体超时会在报告中显示为 `agent_timed_out`，而不是构建失败。

传给 `-agent-cmd` 的环境变量：

- `AGENTEVAL_TASK_ID`：任务标识，例如 `platformer-level`。
- `AGENTEVAL_TASK_TITLE`：任务标题。
- `AGENTEVAL_PROMPT_FILE`：Markdown 提示词文件。
- `AGENTEVAL_WORKSPACE`：暂存的起始工作区。
- `AGENTEVAL_CONTEXT_DIR`：存放复制来的上下文文档的目录，文档保持仓库中的路径，例如 `docs/en/mcp.md`。
- `AGENTEVAL_DOCS`：逗号分隔的上下文文档路径。
- `GOWORK`：临时 `go.work`。

## 定时 CI

`.github/workflows/agent-eval.yml` 每天在 Depot（`depot-ubuntu-24.04-4`）上运行，也可通过 `workflow_dispatch` 手动触发，并在套件变更时运行。它先运行套件自身的测试，再执行 `go run ./tools/agenteval -mode=reference -out agent-eval-report.json`，把评分表追加到 `$GITHUB_STEP_SUMMARY`，并上传 `agent-eval-report.json`。它不运行智能体。
