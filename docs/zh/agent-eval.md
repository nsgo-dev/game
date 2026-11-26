# 智能体评测套件

位于 `tools/agenteval/` 下的智能体评测套件是面向贡献者的工具，包含一组固定的游戏开发任务。每个任务有一个起始工作区、一份手写的参考解，以及一套评分标准：用 YAML 检查、`assetcheck`、`nsgo check` 和无头实时 `game/mcp` 工具给工作区打 0 到 100 分。套件支持三种模式：确定性的 `reference` 评分（无模型与密钥依赖的 API 与格式回归检查）、内置工具调用的 `agent` 评分（支持 Anthropic Messages API 与 OpenAI 兼容的 Chat Completions API），以及用于任意外部编程智能体 CLI 的 `command` 模式。

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

任务在 `context_docs` 中列出的文档以及本仓库的[智能体技能](agent-skill.md)（`skills/nsgo-game/SKILL.md` 与 `references/`）不计分。它们会被复制到工作区旁供智能体阅读，缺失或为空时整次运行失败。

`min_score` 设置任务的及格分；不写时为 100，显式写 `0` 会保留为 0。

## 本地运行

### 参考模式（默认）

用本仓库检出评分每个任务的参考解，不需要模型或 API 密钥：

```sh
go run ./tools/agenteval
```

输出 JSON 报告、独立 Markdown 报告，并追加到摘要文件：

```sh
go run ./tools/agenteval -out agent-eval-report.json -markdown-out agent-eval-summary.md -summary summary.md
```

只运行部分任务或调整及格分：

```sh
go run ./tools/agenteval -task platformer-level,add-menu -min-score 80
```

### 智能体模式（内置编程智能体）

使用内置的多轮工具调用智能体（`-mode=agent`）在每个任务的起始工作区中，结合暂存文档、智能体技能（`skills/nsgo-game/`）与 MCP 配置（`mcp.json`）完成任务，再用同一套标准评分：

```sh
ANTHROPIC_API_KEY=... go run ./tools/agenteval \
  -mode=agent \
  -min-score=80 \
  -out agent-eval-agent-report.json \
  -markdown-out agent-eval-agent-summary.md
```

支持 Anthropic Messages API（`-agent-provider=anthropic`）与 OpenAI 兼容的 Chat Completions API（`-agent-provider=openai`），默认 `-agent-provider=auto` 会根据 `ANTHROPIC_API_KEY`、`OPENAI_API_KEY` 或 `AGENT_EVAL_API_KEY` 自动选择提供商（可通过 `-agent-model`、`-agent-base-url`、`-agent-max-turns` 或对应的 `AGENT_EVAL_*` 环境变量覆盖）。

内置智能体严格限定在暂存临时目录内，通过五个工具（`list_files`、`read_file`、`write_file`、`edit_file`、`run_command`）操作文件与命令，拒绝符号链接与路径穿越，并在 `run_command` 环境中清除 API 密钥与 CI 令牌。传入 `-skip-if-unconfigured` 可在未配置密钥时输出跳过报告并以 `0` 退出。

### 命令模式（外部编程智能体 CLI）

把每个任务的起始工作区、上下文文档与智能体技能复制到临时目录，把提示词写入 `$AGENTEVAL_PROMPT_FILE`，把 MCP 客户端配置写入 `$AGENTEVAL_MCP_CONFIG`，在工作区中执行 `-agent-cmd`（或 `$AGENT_EVAL_COMMAND`），再用同一套标准为智能体留下的结果评分：

```sh
go run ./tools/agenteval \
  -mode=command \
  -min-score=80 \
  -agent-timeout=20m \
  -agent-cmd='my-agent --prompt "$AGENTEVAL_PROMPT_FILE" --mcp-config "$AGENTEVAL_MCP_CONFIG"'
```

智能体会拿到一个临时 `go.work`（在 `GOWORK` 中），它把 `packages/game` 解析到本仓库检出，因此智能体工作时可以运行 `go tool nsgo check` 与 `go tool nsgo test ./...`。智能体拿不到指向仓库的路径，因为仓库的 fixtures 里有参考解。`-agent-timeout`（默认 15 分钟）限制智能体，`-timeout`（默认 5 分钟）限制评分，因此智能体超时会在报告中显示为 `agent_timed_out`，而不是构建失败。

传给 `-agent-cmd`（以及 `-mode=agent` 中 `run_command`）的环境变量：

- `AGENTEVAL_TASK_ID`：任务标识，例如 `platformer-level`。
- `AGENTEVAL_TASK_TITLE`：任务标题。
- `AGENTEVAL_PROMPT_FILE`：Markdown 提示词文件。
- `AGENTEVAL_WORKSPACE`：暂存的起始工作区。
- `AGENTEVAL_CONTEXT_DIR`：存放复制来的上下文文档与技能的目录，保持仓库中的路径，例如 `docs/en/mcp.md` 与 `skills/nsgo-game/SKILL.md`。
- `AGENTEVAL_SKILL_DIR`：暂存的 `skills/nsgo-game` 目录。
- `AGENTEVAL_SKILL_FILE`：暂存的 `skills/nsgo-game/SKILL.md` 文件。
- `AGENTEVAL_MCP_CONFIG`：指向 `go tool nsgo mcp` 的就绪 `mcp.json` 文件。
- `AGENTEVAL_DOCS`：逗号分隔的上下文文档路径。
- `GOWORK`：临时 `go.work`。

## 定时 CI

`.github/workflows/agent-eval.yml` 每天在 Depot（`depot-ubuntu-24.04-4`）上运行，也可通过 `workflow_dispatch` 手动触发，并在 `tools/agenteval/**`、`skills/nsgo-game/**` 或工作流本身变更时运行。它包含两个作业：

1. **`eval`（参考解）**：在 Pull Request、推送到 `main`、`schedule` 与 `workflow_dispatch` 上运行。它先在 `tools/agenteval` 运行 `go tool nsgo test ./...`，再用 `-mode=reference` 评分参考解，把逐任务评分表与细项分解追加到 `$GITHUB_STEP_SUMMARY`，并上传 `agent-eval-reference-report`（`agent-eval-reference-report.json` 与 `agent-eval-reference-summary.md`）。
2. **`eval-agent`（编程智能体）**：仅在 `schedule` 与 `workflow_dispatch` 上运行（不在不受信任的 Pull Request 上运行）。配置了仓库密钥或变量后，它会在起始工作区上针对暂存文档、技能与 MCP 配置运行真实编程智能体，把逐任务得分、轮次/工具调用数与细项分解输出到 `$GITHUB_STEP_SUMMARY`，并上传 `agent-eval-agent-report`（`agent-eval-agent-report.json` 与 `agent-eval-agent-summary.md`）。在 `auto` / 定时模式下，若尚未配置密钥，则会以 `-skip-if-unconfigured` 运行，在摘要中清晰记录 `SKIPPED` 而不导致 CI 失败。

`eval-agent` 读取的仓库密钥与变量：

- **密钥（Secrets）**：`ANTHROPIC_API_KEY`、`OPENAI_API_KEY`、`AGENT_EVAL_API_KEY`（用于 `-mode=agent`），或 `AGENT_EVAL_COMMAND`（通过 `-mode=command` 运行自定义智能体 CLI）。
- **变量（Variables）**：`AGENT_EVAL_COMMAND`、`AGENT_EVAL_PROVIDER`（`auto`、`anthropic`、`openai`）、`AGENT_EVAL_MODEL`、`AGENT_EVAL_BASE_URL`、`AGENT_EVAL_MAX_TURNS` 与 `AGENT_EVAL_MIN_SCORE`。
