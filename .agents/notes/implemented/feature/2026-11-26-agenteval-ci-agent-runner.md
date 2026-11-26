# Agent Note: Built-in coding agent runner and scheduled CI job for agenteval

Status: implemented

## Problem

The initial agent evaluation suite ([agents-eval-suite](2026-11-24-agents-eval-suite.md)) added the task YAML format, rubric grader, deterministic `reference` mode, and external `-mode=command` hook, leaving live coding-agent execution in scheduled CI open under #506. Without a built-in agent driver, skill/MCP staging, and a secret-aware CI job, maintainers had no turnkey way to run a coding agent on schedule against the docs, `skills/nsgo-game`, and `game/mcp`, nor a safe degradation path when repository secrets are absent on forks or unconfigured checkouts.

## Decision

Extend `tools/agenteval` and `.github/workflows/agent-eval.yml` with three tightly scoped additions while keeping deterministic `reference` grading as the always-on baseline:

- **Skill and MCP staging in every run (`tools/agenteval/runner.go`)**: Stage `skills/nsgo-game` (verifying `SKILL.md` is present and non-empty, refusing symlinks) alongside `task.ContextDocs` under the temporary `context/` directory, write a workspace-external `mcp.json` pointing at `go tool nsgo mcp`, expose `AGENTEVAL_SKILL_DIR`, `AGENTEVAL_SKILL_FILE`, and `AGENTEVAL_MCP_CONFIG`, and reference all three in the rendered prompt without exposing any path into the repository fixtures.
- **Zero-dependency built-in coding agent (`-mode=agent`, `tools/agenteval/agent.go`)**: Implement a multi-turn tool-calling loop over Go's `net/http` supporting both the Anthropic Messages API (`anthropic`) and OpenAI-compatible Chat Completions APIs (`openai`), with `auto` provider selection from `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, or `AGENT_EVAL_API_KEY`. The agent acts strictly through five workspace-confined tools (`list_files`, `read_file`, `write_file`, `edit_file`, `run_command`) that refuse symlinks and path traversal and scrub API keys and GitHub tokens from `run_command` subprocess environments.
- **Graceful unconfigured skip and dual-job Depot CI (`.github/workflows/agent-eval.yml`, `tools/agenteval/report.go`)**: Add `-skip-if-unconfigured`, `-markdown-out`, and agent telemetry fields (`provider`, `model`, `skipped`, `skip_reason`, turns, tool calls, token counts, `agent_error`) to `SuiteReport`/`TaskReport`. In `.github/workflows/agent-eval.yml` (on `depot-ubuntu-24.04-4` only), keep `eval` running tests + `-mode=reference` on PRs, pushes, schedule, and dispatch, and add `eval-agent` on `schedule` and `workflow_dispatch` (never on untrusted PRs). `eval-agent` runs `-mode=command` when `AGENT_EVAL_COMMAND` is set or `-mode=agent` when an API key secret is set, passes `-skip-if-unconfigured` in `auto`/scheduled mode so unconfigured repos publish a `SKIPPED` step summary and artifacts instead of failing, and uploads both JSON and Markdown reports with `if: always()`.

## Alternatives considered

### Installing an external npm/CLI coding agent (Claude Code or Codex) directly in `.github/workflows/agent-eval.yml`

Strongest argument: running the exact same third-party CLI end users invoke exercises CLI-specific prompt/tool quirks.

Rejected as the sole CI mechanism because installing Node/npm CLI binaries in CI adds supply-chain and release-churn coupling and locks CI to one vendor CLI. Keeping `-mode=command` (driven by `AGENT_EVAL_COMMAND`) alongside a stdlib Go `-mode=agent` lets CI run out of the box with just an Anthropic or OpenAI-compatible API key secret while still allowing maintainers to plug in any external CLI via `AGENT_EVAL_COMMAND`.

### Adding third-party Go LLM SDK dependencies to `tools/agenteval/go.mod`

Strongest argument: vendor SDKs wrap request/response structs and streaming helpers.

Rejected because `agenteval` only needs non-streaming tool-call POST requests to `/v1/messages` and `/chat/completions`. Writing the two wire adapters with Go's standard `net/http` and `encoding/json` keeps `tools/agenteval/go.mod` dependency-minimal and trivially testable with `httptest.NewServer`.

### Running `eval-agent` on `pull_request` events

Strongest argument: catching doc regressions before merge instead of waiting for the daily schedule.

Rejected because pull requests from forks do not have repository secrets, exposing LLM secrets to untrusted PR code is a security risk, and live model calls on every commit add cost and flakiness. PRs exercise the full `agent.go` driver deterministically through mock-server tests in `eval`, while live runs stay scoped to `schedule` and `workflow_dispatch`.

## Consequences

- **Benefits**: Scheduled CI can now evaluate a real coding agent end to end against the docs, `skills/nsgo-game`, and `game/mcp` whenever `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `AGENT_EVAL_API_KEY`, or `AGENT_EVAL_COMMAND` is configured, publishing per-task 0–100 scores, criterion breakdowns, and turn/token telemetry to `$GITHUB_STEP_SUMMARY` and JSON/Markdown artifacts; unconfigured repositories and PRs stay green without secrets.
- **Costs**: Adds ~550 lines of provider/tool-loop code in `tools/agenteval/agent.go` and a second Depot CI job in `.github/workflows/agent-eval.yml` whose Anthropic/OpenAI wire schemas must be kept in sync if providers deprecate current endpoints.

## Verification

Verified with `gofmt`, `cd tools/agenteval && go tool nsgo test ./...`, `cd tools/agenteval && go tool nsgo check -target macos,windows,linux,web .`, `go run ./tools/agenteval -mode=reference -out ... -markdown-out ...`, `go run ./tools/agenteval -mode=agent -skip-if-unconfigured`, `actionlint .github/workflows/agent-eval.yml`, the Depot runner label check, `go run ./tools/changelog check`, and `bun run verify-notes`.
