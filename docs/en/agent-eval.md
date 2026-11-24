# Agent Evaluation Suite

The agent evaluation suite under `tools/agenteval/` runs a fixed set of game-development tasks against NSGo's documentation, YAML schemas, `game check` (`assetcheck`), `nsgo check`, and live headless MCP tools (`game/mcp`) to catch regressions in docs and APIs on a schedule and report a 0–100 score per task.

## Tasks

Each task lives as a plain-text versioned YAML document (`version: 1`) under `tools/agenteval/tasks/` with unknown fields refused and deterministic round-trip encoding:

| Task ID | Goal | What the rubric grades |
| --- | --- | --- |
| `platformer-level` | Build a platformer level | Context docs, valid `game.yaml` and `scenes/level1.scene.yaml`, `assetcheck.Check`, reachable platform geometry from `player_spawn` to `goal`, `nsgo check`, and live MCP stepping (`game_scenes_list`, `game_layers_list`, `game_entities_list`, `game_loop_pause`, `game_loop_step`, `game_entity_inspect`, `game_component_get`) |
| `add-enemy-type` | Add an enemy type | Context docs, valid `game.yaml` and `scenes/arena.scene.yaml`, `assetcheck.Check`, enemy object carrying an `enemy` component, `nsgo check`, and live MCP verification that the `"enemy"`-tagged entity moves deterministically across 60 fixed updates |
| `add-menu` | Add a menu | Context docs, valid `game.yaml`, `scenes/menu.scene.yaml` and `scenes/gameplay.scene.yaml`, `assetcheck.Check`, visible screen-space menu layer with interactive items, `nsgo check`, and live MCP verification of `game_scenes_list` (`menu`, `gameplay`) and `game_layers_list` (`space: screen`) |

## Scoring rubric

Every task defines 100 points across six criteria in `checks.weights`:

1. **`docs_context_present`** (10 pts): every documentation file listed in `context_docs` exists and is non-empty.
2. **`yaml_valid`** (15 pts): every listed `game.yaml` and `*.scene.yaml` parses cleanly and round-trips deterministically through `project.Encode` or `sceneformat.Encode`.
3. **`project_check_passes`** (15 pts): `assetcheck.Check` reports zero problems across scenes, maps, and references.
4. **Domain check** (`level_geometry_valid` / `enemy_definition_present` / `menu_scene_valid`, 20 pts): verifies task-specific scene geometry, reachability, enemy definitions, or screen-space menu items.
5. **`builds_cleanly`** (15 pts): `go tool nsgo check` succeeds on the workspace against the current checkout's `packages/game`.
6. **`mcp_verify_passes`** (25 pts): boots the game headless under `nsgotest.Start` and invokes live `game/mcp` tools through `nsgo/automation.Call`.

## Running locally

### Reference mode (default, deterministic, no LLM API key)

Grades each task's reference workspace against the current repository checkout:

```sh
go run ./tools/agenteval
```

Write a machine-readable JSON report and a Markdown summary:

```sh
go run ./tools/agenteval -out agent-eval-report.json -summary summary.md
```

Run a subset of tasks or adjust the passing threshold:

```sh
go run ./tools/agenteval -task platformer-level,add-menu -min-score 80
```

### Command mode (evaluating an external coding agent)

Stages each task's starter fixture into an isolated temporary workspace, writes the task prompt and required docs list to `$AGENTEVAL_PROMPT_FILE`, invokes `-agent-cmd` inside `$AGENTEVAL_WORKSPACE`, and grades the result with the same rubric:

```sh
go run ./tools/agenteval \
  -mode=command \
  -min-score=80 \
  -agent-cmd='my-agent --prompt "$AGENTEVAL_PROMPT_FILE"'
```

Environment variables passed to `-agent-cmd`:

- `AGENTEVAL_TASK_ID`: task identifier (for example `platformer-level`).
- `AGENTEVAL_TASK_TITLE`: human-readable task title.
- `AGENTEVAL_PROMPT_FILE`: path to the generated Markdown prompt file.
- `AGENTEVAL_WORKSPACE`: path to the staged starter workspace directory.
- `AGENTEVAL_REPO_ROOT`: path to the repository root.
- `AGENTEVAL_DOCS`: comma-separated list of context doc paths.

## Scheduled CI

`.github/workflows/agent-eval.yml` runs daily on Depot (`depot-ubuntu-24.04-4`) and on demand via `workflow_dispatch`. It runs `tools/agenteval`'s own test suite, executes `go run ./tools/agenteval -mode=reference -out agent-eval-report.json`, appends the per-task score table to `$GITHUB_STEP_SUMMARY`, and uploads `agent-eval-report.json` as a workflow artifact.
