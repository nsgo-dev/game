# Agent Note: Agent evaluation suite and scheduled CI regression harness

Status: implemented

## Problem

As NSGo's game layer adds agent-facing documentation, YAML formats, project checks (`assetcheck`), and live runtime tools (`game/mcp`), regressions can silently break the end-to-end workflow an agent relies on: a doc page gets renamed, a YAML schema breaks round-trip determinism, a platformer or scene API changes signature, or an MCP tool stops reporting scenes, layers, entities, or loop steps. Without fixed, automatically graded tasks running on a schedule, those cross-cutting breakages are only discovered when a user or coding agent fails on a real game project (#506).

## Decision

Add a standalone Go module under `tools/agenteval` (registered in `go.work`, mirroring `tools/changelog`) with plain-text versioned YAML task definitions (`version: 1`, strict `KnownFields(true)` decoding, deterministic round-trip encoding), isolated starter and reference game workspaces under `tools/agenteval/testdata/fixtures/`, a six-criterion 0–100 rubric grader, two execution modes (`reference` and `command`), and a scheduled Depot CI workflow (`.github/workflows/agent-eval.yml`).

## Suite architecture and invariants

- **Isolated tooling module (`tools/agenteval`)**: Keeping the evaluation runner outside `packages/game` avoids adding test-harness dependencies to library consumers and keeps `packages/game/stability_test.go` and `packages/game/formats_test.go` focused on shipped game packages.
- **Versioned YAML task format (`tools/agenteval/tasks/*.yaml`)**: Every task (`platformer-level`, `add-enemy-type`, `add-menu`) declares `version: 1`, prompt text, `context_docs`, starter and reference fixture paths, domain checks (`platformer_level`, `enemy_spec`, or `menu_spec`), live `mcp_verify` assertions, and six positive `weights` that must sum to `100`.
- **Two execution modes**:
  - `reference` (default in scheduled CI): stages each task's reference workspace into a temporary directory, writes a temporary `go.work` plus local `replace` pointing at this checkout's `packages/game`, and grades all six criteria deterministically with zero external LLM cost, API keys, or flakiness.
  - `command` (`-mode=command -agent-cmd=...`): stages each task's starter workspace, writes the prompt and doc paths to `$AGENTEVAL_PROMPT_FILE`, invokes the configured external agent command in `$AGENTEVAL_WORKSPACE`, and grades the produced workspace with the exact same rubric.
- **Hidden headless MCP verification (`mcp_verify_passes`)**: The grader generates `agenteval_mcp_verify_test.go` from the task's `mcp_verify` YAML spec, runs `go tool nsgo test . -- -v -run ^TestAgentEvalMCPVerify$`, boots `Root` via `nsgotest.Start`, and calls live `game/mcp` tools (`game_scenes_list`, `game_layers_list`, `game_entities_list`, `game_entity_inspect`, `game_component_get`, `game_loop_pause`, `game_loop_step`) through `nsgo/automation.Call`. Any workspace `go.mod` patch and generated test file are cleaned up on exit.
- **Scheduled CI reporting (`.github/workflows/agent-eval.yml`)**: Runs daily on `depot-ubuntu-24.04-4` (and on `workflow_dispatch` or changes to `tools/agenteval/**`), appends the per-task Markdown score table to `$GITHUB_STEP_SUMMARY`, and uploads `agent-eval-report.json`.

## Alternatives considered

### Placing the evaluation runner inside `packages/game/mcp` or `packages/game/tooling`

Strongest argument: colocating the suite inside `packages/game` avoids adding another `go.work` module and lets tests import internal helpers directly.

Rejected because `packages/game` is the shipped game library whose packages are audited by `stability_test.go` and vetted across mobile/web targets (`android`, `ios`, `web`). A CLI harness that invokes `os/exec` (`go tool nsgo check` / `go tool nsgo test`) and manages temporary workspaces does not belong in the game runtime module; `tools/agenteval` matches `tools/changelog`.

### Calling a live hosted LLM API directly in scheduled CI

Strongest argument: running a live model every night measures model quality alongside API regressions.

Rejected as the default CI mode because live LLM calls require repository secrets, incur recurring API cost, fail on provider outages, and introduce non-deterministic pass/fail flakiness unrelated to code regressions. Separating deterministic `reference` mode (CI default) from pluggable `-mode=command` (for evaluating any external coding agent on demand) keeps scheduled CI fast and reliable while supporting real agent benchmarks with the exact same grading harness.

### Checking MCP behavior via static AST inspection instead of running `nsgotest`

Strongest argument: parsing Go ASTs avoids invoking `nsgo test` on staged workspaces and runs in milliseconds.

Rejected because static AST checks miss runtime regressions in `game/mcp`, `nsgo/automation`, `platformer.Controller`, `entities.World`, `scene.Manager`, and `game.Loop.StepNow`. Running a generated `nsgotest` harness exercises the actual compiled game and MCP wire handlers end to end.

## Consequences

- **Benefits**: Daily scheduled CI verifies docs, YAML round-trips, `assetcheck`, builds, and live MCP behavior across three representative game-development tasks (`platformer-level`, `add-enemy-type`, `add-menu`) with clear per-task 0–100 scores and criterion breakdowns; maintainers can also benchmark any external coding agent via `-mode=command`.
- **Costs**: Adds one workspace module (`tools/agenteval`) and three fixture pairs under `tools/agenteval/testdata/fixtures/` that must be updated if breaking changes are intentionally made to the referenced docs or Experimental `game/mcp` APIs.

## Verification

Verified with `gofmt`, `go tool nsgo test ./...` in `tools/agenteval`, `go run ./tools/agenteval -out ...`, `actionlint .github/workflows/agent-eval.yml`, `go run ./cmd/gameschema -check`, `go tool nsgo check -target macos,windows,linux,android,web`, `go tool nsgo test . -- -run 'TestFormatPackagesRunConformance|TestFormatsPage|TestStabilityTable'`, `go run ./tools/changelog check`, and `bun run verify-notes`.
