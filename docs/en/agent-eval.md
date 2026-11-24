# Agent Evaluation Suite

The agent evaluation suite under `tools/agenteval/` is contributor tooling with fixed game-development tasks. Each task has a starter workspace, a hand-written reference solution and a rubric that scores a workspace from 0 to 100 with YAML checks, `assetcheck`, `nsgo check` and live headless `game/mcp` tools. The scheduled CI job grades the reference solutions, which makes it a regression suite for the game APIs, YAML formats and MCP tools they use. Running a coding agent on the starter workspaces is a separate, opt-in mode, and CI does not run an agent.

## Tasks

Each task is a plain-text versioned YAML document (`version: 1`) under `tools/agenteval/tasks/`, with unknown fields refused and deterministic round-trip encoding:

| Task ID | Goal | What the rubric grades |
| --- | --- | --- |
| `platformer-level` | Build a platformer level | Valid `game.yaml` and `scenes/level1.scene.yaml`, `assetcheck.Check`, platforms from `player_spawn` to `goal` with no horizontal gap above `max_gap` and no vertical step above `max_rise`, `nsgo check`, and live MCP checks that holding the `right` action through `game_input_action` moves the player toward the goal over 60 fixed steps |
| `add-enemy-type` | Add an enemy type | Valid `game.yaml` and `scenes/arena.scene.yaml`, `assetcheck.Check`, an enemy object (`prefab: enemy`, an `enemy` component or `tag: enemy`) carrying an `enemy` component, `nsgo check`, and live MCP checks that the `"enemy"`-tagged entity moves across 60 fixed steps |
| `add-menu` | Add a menu | Valid `game.yaml`, `scenes/menu.scene.yaml` and `scenes/gameplay.scene.yaml`, `assetcheck.Check`, a visible screen-space layer with at least two buttons (`prefab: button` or a `button` component), `nsgo check`, and live MCP checks of `game_scenes_list` and `game_layers_list`, then a click on Play through `game_input_pointer` that must switch to `gameplay` |

## Scoring rubric

Every task spreads 100 points across five criteria in `checks.weights`:

1. **`yaml_valid`** (15 pts): every listed `game.yaml` and `*.scene.yaml` parses and round-trips deterministically through `project.Encode` or `sceneformat.Encode`.
2. **`project_check_passes`** (15 pts): `assetcheck.Check` reports zero problems across scenes, maps and references.
3. **Domain check** (`level_geometry_valid`, `enemy_definition_present` or `menu_scene_valid`, 25 pts): the task's scene geometry, enemy definition or menu buttons.
4. **`builds_cleanly`** (15 pts): `go tool nsgo check` succeeds on the workspace against the checkout's `packages/game`.
5. **`mcp_verify_passes`** (30 pts): a generated test boots the game headless under `nsgotest.Start`, calls live `game/mcp` tools through `nsgo/automation.Call`, and must report a pass; a clean exit without it scores nothing.

The documents a task lists in `context_docs` are not scored. They are copied next to the workspace for the agent, and a missing or empty one fails the run.

`min_score` sets a task's passing score; when it is absent the threshold is 100, and an explicit `0` is kept.

## Running locally

### Reference mode (default)

Grades each task's reference solution against the checkout, with no model or API key:

```sh
go run ./tools/agenteval
```

Write a JSON report and a Markdown summary:

```sh
go run ./tools/agenteval -out agent-eval-report.json -summary summary.md
```

Run some tasks or change the passing score:

```sh
go run ./tools/agenteval -task platformer-level,add-menu -min-score 80
```

### Command mode (running a coding agent)

Copies each task's starter workspace and context docs into a temporary directory, writes the prompt to `$AGENTEVAL_PROMPT_FILE`, runs `-agent-cmd` in the workspace, and grades what the agent leaves with the same rubric:

```sh
go run ./tools/agenteval \
  -mode=command \
  -min-score=80 \
  -agent-timeout=20m \
  -agent-cmd='my-agent --prompt "$AGENTEVAL_PROMPT_FILE"'
```

The agent gets a temporary `go.work` (in `GOWORK`) that resolves `packages/game` to the checkout, so it can run `go tool nsgo check` and `go tool nsgo test .` while it works. It gets no path into the repository, whose fixtures hold the reference solutions. `-agent-timeout` (default 15 minutes) bounds the agent and `-timeout` (default 5 minutes) bounds grading, so a slow agent shows up as `agent_timed_out` in the report rather than as build failures.

Environment variables passed to `-agent-cmd`:

- `AGENTEVAL_TASK_ID`: the task identifier, such as `platformer-level`.
- `AGENTEVAL_TASK_TITLE`: the task title.
- `AGENTEVAL_PROMPT_FILE`: the Markdown prompt.
- `AGENTEVAL_WORKSPACE`: the staged starter workspace.
- `AGENTEVAL_CONTEXT_DIR`: the directory holding the copied context docs at their repository paths, such as `docs/en/mcp.md`.
- `AGENTEVAL_DOCS`: the context doc paths, comma-separated.
- `GOWORK`: the temporary `go.work`.

## Scheduled CI

`.github/workflows/agent-eval.yml` runs daily on Depot (`depot-ubuntu-24.04-4`), on demand through `workflow_dispatch`, and when the suite changes. It runs the suite's own tests, then `go run ./tools/agenteval -mode=reference -out agent-eval-report.json`, appends the score table to `$GITHUB_STEP_SUMMARY`, and uploads `agent-eval-report.json`. It does not run an agent.
