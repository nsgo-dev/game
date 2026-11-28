# Agent Note: Local agent model and effort arguments

Status: implemented

## Problem

`AgentRequest.Model` existed but external agent runners ignored it, so local Claude Code and Codex processes always selected their own defaults. A task's model choice could not be honored without changing the launch policy.

## Decision

The desktop CLI runner validates optional model and reasoning-effort identifiers before constructing an `os/exec` argument list. Claude Code receives `--model`, while Codex receives `--model` and its `model_reasoning_effort` configuration override. Arguments are inserted before the existing `--` delimiter and policy flags, and empty values preserve current defaults.

## Alternatives considered

Passing selections through environment variables would depend on agent-specific environment conventions and could leak into child tools. Embedding them in the prompt would not select a model and would make the setting advisory, so both approaches were rejected.

## Consequences

Per-request model selection now reaches both supported local CLIs while launch isolation remains unchanged. The accepted identifier alphabet is intentionally conservative, so future provider-specific IDs may require an explicit validator extension.

## Verification

Desktop runner tests verify Claude and Codex argument lists, effort configuration, prompt delimiter placement, and rejection of shell metacharacters, whitespace and overlong model IDs. `go tool nsgo test ./pkg/editor` and `go tool nsgo check ./pkg/editor` pass.
