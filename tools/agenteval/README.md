# agenteval

The agent evaluation suite in this directory is contributor tooling with fixed game-development tasks. Each task has a starter workspace, a hand-written reference solution and a rubric that scores a workspace from 0 to 100 with YAML checks, `assetcheck`, `nsgo check` and live headless `game/mcp` tools. It supports three modes: deterministic `reference` grading (API and format regression checks with no model or key), built-in tool-calling `agent` grading (Anthropic Messages API or OpenAI-compatible Chat Completions API), and `command` grading for any external coding agent CLI.

## Tasks

Each task is a plain-text versioned YAML document (`version: 1`) under `tools/agenteval/tasks/`, with unknown fields refused and deterministic round-trip encoding:

| Task ID | Goal | What the rubric grades |
| --- | --- | --- |
| `platformer-level` | Build a platformer level | Valid `game.yaml` and `scenes/level1.scene.yaml`, `assetcheck.Check`, platforms from `player_spawn` to `goal` with no horizontal gap above `max_gap` and no vertical step above `max_rise`, `nsgo check`, and live MCP checks that holding the `right` action through `game_input_action` moves the player toward the goal over 60 fixed steps |
| `add-enemy-type` | Add an enemy type | Valid `game.yaml` and `scenes/arena.scene.yaml`, `assetcheck.Check`, an enemy object (`prefab: enemy`, an `enemy` component or `tag: enemy`) carrying an `enemy` component, `nsgo check`, and live MCP checks that the `"enemy"`-tagged entity moves across 60 fixed steps |
| `add-menu` | Add a menu | Valid `game.yaml`, `scenes/menu.scene.yaml` and `scenes/gameplay.scene.yaml`, `assetcheck.Check`, a visible screen-space layer with at least two buttons (`prefab: button` or a `button` component), `nsgo check`, and live MCP checks of `game_scenes_list` and `game_layers_list`, then a click on Play through `game_input_pointer` that must switch to `gameplay` |
| `editor-workflow` | Build the same menu through the editor workflow | The `add-menu` rubric; `checks.required_tools` declares `game_scenes_list`, `game_layers_list`, `scene_edit` and `game_check` as the intended tools, which is not scored ([catalog](../../docs/en/agent-eval.md)) |

## Scoring rubric

Every task spreads 100 points across five criteria in `checks.weights`:

1. **`yaml_valid`** (15 pts): every listed `game.yaml` and `*.scene.yaml` parses and round-trips deterministically through `project.Encode` or `sceneformat.Encode`.
2. **`project_check_passes`** (15 pts): `assetcheck.Check` reports zero problems across scenes, maps and references.
3. **Domain check** (`level_geometry_valid`, `enemy_definition_present` or `menu_scene_valid`, 25 pts): the task's scene geometry, enemy definition or menu buttons.
4. **`builds_cleanly`** (15 pts): `go tool nsgo check` succeeds on the workspace against the checkout's root module.
5. **`mcp_verify_passes`** (30 pts): a generated test boots the game headless under `nsgotest.Start`, calls live `game/mcp` tools through `nsgo/automation.Call`, and must report a pass; a clean exit without it scores nothing.

The documents a task lists in `context_docs` and the repository's [Agent Skill](../../docs/en/agent-skill.md) (`skills/nsgo-game/SKILL.md` and `references/`) are not scored. They are copied next to the workspace for the agent, and a missing or empty doc or skill fails the run.

`min_score` sets a task's passing score; when it is absent the threshold is 100, and an explicit `0` is kept.

## Running locally

### Reference mode (default)

Grades each task's reference solution against the checkout, with no model or API key. The commands below run from the repository root:

```sh
go run ./tools/agenteval
```

Write a JSON report, a standalone Markdown report, and append to a step summary file:

```sh
go run ./tools/agenteval -out agent-eval-report.json -markdown-out agent-eval-summary.md -summary summary.md
```

Run some tasks or change the passing score:

```sh
go run ./tools/agenteval -task platformer-level,add-menu -min-score 80
```

### Agent mode (built-in coding agent)

Runs a built-in multi-turn tool-calling agent (`-mode=agent`) against each task's starter workspace, staged docs, Agent Skill (`skills/nsgo-game/`), and MCP config (`mcp.json`), then grades what the agent leaves with the same rubric:

```sh
ANTHROPIC_API_KEY=... go run ./tools/agenteval \
  -mode=agent \
  -min-score=80 \
  -out agent-eval-agent-report.json \
  -markdown-out agent-eval-agent-summary.md
```

It supports both the Anthropic Messages API (`-agent-provider=anthropic`) and OpenAI-compatible Chat Completions APIs (`-agent-provider=openai`), or `-agent-provider=auto` (default), which selects a provider from `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, or `AGENT_EVAL_API_KEY` (with optional `-agent-model`, `-agent-base-url`, and `-agent-max-turns`, or the matching `AGENT_EVAL_*` environment variables).

The built-in agent operates strictly inside the staged temporary directories through five tools (`list_files`, `read_file`, `write_file`, `edit_file`, `run_command`), refuses symlinks and path traversal, and scrubs API keys and CI tokens from `run_command`'s environment. Pass `-skip-if-unconfigured` to emit a skipped report and exit `0` when no API key is configured.

### Command mode (external coding agent CLI)

Copies each task's starter workspace, context docs, and Agent Skill into a temporary directory, writes the prompt to `$AGENTEVAL_PROMPT_FILE` and an MCP client config to `$AGENTEVAL_MCP_CONFIG`, runs `-agent-cmd` (or `$AGENT_EVAL_COMMAND`) in the workspace, and grades what the agent leaves with the same rubric:

```sh
go run ./tools/agenteval \
  -mode=command \
  -min-score=80 \
  -agent-timeout=20m \
  -agent-cmd='my-agent --prompt "$AGENTEVAL_PROMPT_FILE" --mcp-config "$AGENTEVAL_MCP_CONFIG"'
```

The agent gets a temporary `go.work` (in `GOWORK`) that resolves `github.com/nsgo-dev/game` to the checkout, so it can run `go tool nsgo check` and `go tool nsgo test ./...` while it works. It gets no path into the repository, whose fixtures hold the reference solutions. `-agent-timeout` (default 15 minutes) bounds the agent and `-timeout` (default 5 minutes) bounds grading, so a slow agent shows up as `agent_timed_out` in the report rather than as build failures.

Environment variables passed to `-agent-cmd` (and `run_command` in `-mode=agent`):

- `AGENTEVAL_TASK_ID`: the task identifier, such as `platformer-level`.
- `AGENTEVAL_TASK_TITLE`: the task title.
- `AGENTEVAL_PROMPT_FILE`: the Markdown prompt.
- `AGENTEVAL_WORKSPACE`: the staged starter workspace.
- `AGENTEVAL_CONTEXT_DIR`: the directory holding the copied context docs and skill at their repository paths, such as `docs/en/mcp.md` and `skills/nsgo-game/SKILL.md`.
- `AGENTEVAL_SKILL_DIR`: the staged `skills/nsgo-game` directory.
- `AGENTEVAL_SKILL_FILE`: the staged `skills/nsgo-game/SKILL.md` file.
- `AGENTEVAL_MCP_CONFIG`: a ready-to-use `mcp.json` pointing at `go tool nsgo mcp`.
- `AGENTEVAL_DOCS`: the context doc paths, comma-separated.
- `GOWORK`: the temporary `go.work`.

## Scheduled CI

`.github/workflows/agent-eval.yml` runs daily on Depot (`depot-ubuntu-24.04-4`), on demand through `workflow_dispatch`, and when `tools/agenteval/**` (apart from this README), the tools module's `tools/go.mod` and `tools/go.sum`, `skills/nsgo-game/**` or the workflow itself changes. It has two jobs:

1. **`eval` (reference)**: runs on pull requests, pushes to `main`, `schedule`, and `workflow_dispatch`. It runs `go tool nsgo test ./agenteval/...` in the `tools` module, grades the reference solutions with `-mode=reference`, appends the per-task score table and criterion breakdown to `$GITHUB_STEP_SUMMARY`, and uploads `agent-eval-reference-report` (`agent-eval-reference-report.json` and `agent-eval-reference-summary.md`).
2. **`eval-agent` (coding agent)**: runs on `schedule` and `workflow_dispatch` (never on untrusted pull requests). When configured with repository secrets or variables, it runs an actual coding agent on the starter workspaces against the staged docs, skill, and MCP config, reports per-task scores, turns/tool counts, and criterion breakdowns to `$GITHUB_STEP_SUMMARY`, and uploads `agent-eval-agent-report` (`agent-eval-agent-report.json` and `agent-eval-agent-summary.md`). In `auto` / scheduled mode, if no credentials are configured yet, it runs with `-skip-if-unconfigured` so CI records a clear `SKIPPED` summary instead of failing.

Repository secrets and variables read by `eval-agent`:

- **Secrets**: `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `AGENT_EVAL_API_KEY` (for `-mode=agent`), or `AGENT_EVAL_COMMAND` (to run a custom agent CLI via `-mode=command`).
- **Variables**: `AGENT_EVAL_COMMAND`, `AGENT_EVAL_PROVIDER` (`auto`, `anthropic`, `openai`), `AGENT_EVAL_MODEL`, `AGENT_EVAL_BASE_URL`, `AGENT_EVAL_MAX_TURNS`, and `AGENT_EVAL_MIN_SCORE`.
