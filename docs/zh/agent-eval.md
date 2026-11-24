# 智能体评测套件

位于 `tools/agenteval/` 下的智能体评测套件针对 NSGo 的文档、YAML 格式、`game check`（`assetcheck`）、`nsgo check` 以及无头实时 MCP 工具（`game/mcp`）运行一组固定游戏开发任务，在定时 CI 中尽早发现文档与 API 回归，并为每个任务输出 0–100 分的评分报告。

## 任务列表

每个任务以纯文本带版本号的 YAML 文件（`version: 1`）保存在 `tools/agenteval/tasks/` 下，拒绝未知字段并支持确定性往返编码：

| 任务 ID | 目标 | 评分检查内容 |
| --- | --- | --- |
| `platformer-level` | 构建平台跳跃关卡 | 上下文文档、有效的 `game.yaml` 与 `scenes/level1.scene.yaml`、`assetcheck.Check`、从 `player_spawn` 到 `goal` 的可达平台几何、`nsgo check`，以及实时 MCP 步进校验（`game_scenes_list`、`game_layers_list`、`game_entities_list`、`game_loop_pause`、`game_loop_step`、`game_entity_inspect`、`game_component_get`） |
| `add-enemy-type` | 添加一种敌人类型 | 上下文文档、有效的 `game.yaml` 与 `scenes/arena.scene.yaml`、`assetcheck.Check`、带 `enemy` 组件的敌人对象、`nsgo check`，以及实时 MCP 验证带 `"enemy"` 标签的实体在 60 个固定步长内确定性移动 |
| `add-menu` | 添加菜单 | 上下文文档、有效的 `game.yaml`、`scenes/menu.scene.yaml` 与 `scenes/gameplay.scene.yaml`、`assetcheck.Check`、含交互项的可见屏幕空间菜单图层、`nsgo check`，以及实时 MCP 校验 `game_scenes_list`（`menu`、`gameplay`）与 `game_layers_list`（`space: screen`） |

## 评分标准

每个任务在 `checks.weights` 中将 100 分分配到六项检查：

1. **`docs_context_present`**（10 分）：`context_docs` 中列出的每个文档文件均存在且非空。
2. **`yaml_valid`**（15 分）：列出的 `game.yaml` 与 `*.scene.yaml` 均可正常解析，并经 `project.Encode` 或 `sceneformat.Encode` 确定性往返编码。
3. **`project_check_passes`**（15 分）：`assetcheck.Check` 对场景、地图和引用报告零问题。
4. **领域检查**（`level_geometry_valid` / `enemy_definition_present` / `menu_scene_valid`，20 分）：校验任务专属的关卡可达性、敌人定义或屏幕空间菜单项。
5. **`builds_cleanly`**（15 分）：工作区针对当前检出的 `packages/game` 通过 `go tool nsgo check`。
6. **`mcp_verify_passes`**（25 分）：在 `nsgotest.Start` 下无头启动游戏，并通过 `nsgo/automation.Call` 调用实时 `game/mcp` 工具验证运行时状态。

## 本地运行

### 参考模式（默认，确定性，无需 LLM API 密钥）

使用当前仓库检出的代码与文档，对每个任务的参考工作区打分：

```sh
go run ./tools/agenteval
```

生成机器可读的 JSON 报告和 Markdown 摘要：

```sh
go run ./tools/agenteval -out agent-eval-report.json -summary summary.md
```

只运行部分任务或调整及格线：

```sh
go run ./tools/agenteval -task platformer-level,add-menu -min-score 80
```

### 命令模式（评测外部编程智能体）

将每个任务的初始工作区复制到隔离的临时目录，把任务提示词和所需文档列表写入 `$AGENTEVAL_PROMPT_FILE`，在 `$AGENTEVAL_WORKSPACE` 内执行 `-agent-cmd`，并用同一套评分标准对结果打分：

```sh
go run ./tools/agenteval \
  -mode=command \
  -min-score=80 \
  -agent-cmd='my-agent --prompt "$AGENTEVAL_PROMPT_FILE"'
```

传递给 `-agent-cmd` 的环境变量：

- `AGENTEVAL_TASK_ID`：任务标识符（例如 `platformer-level`）。
- `AGENTEVAL_TASK_TITLE`：可读的任务标题。
- `AGENTEVAL_PROMPT_FILE`：生成的 Markdown 提示词文件路径。
- `AGENTEVAL_WORKSPACE`：准备好的初始工作区目录路径。
- `AGENTEVAL_REPO_ROOT`：仓库根目录路径。
- `AGENTEVAL_DOCS`：逗号分隔的上下文文档路径列表。

## 定时 CI

`.github/workflows/agent-eval.yml` 每天在 Depot（`depot-ubuntu-24.04-4`）上定时运行，也支持通过 `workflow_dispatch` 手动触发。它会运行 `tools/agenteval` 自身测试、执行 `go run ./tools/agenteval -mode=reference -out agent-eval-report.json`，将各任务得分表追加到 `$GITHUB_STEP_SUMMARY`，并把 `agent-eval-report.json` 上传为工作流产物。
