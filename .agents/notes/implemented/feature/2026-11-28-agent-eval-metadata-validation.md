# Agent Note: Agent-eval task metadata validation

Status: implemented

## Problem

The evaluation catalog introduced editor workflow tasks, but task metadata had no validated family or MCP-server contract. Typos could silently misclassify coverage.

## Decision

Task YAML accepts an optional family from the fixed coverage vocabulary and a list of non-empty, unique MCP server names. Loading rejects unknown families, whitespace-containing names, and duplicates. The editor-workflow task declares the `editor` family and `nsgo` server.

## Alternatives considered

Keeping metadata as unchecked strings would preserve flexibility but make coverage reports unreliable. Inferring families from task IDs would hide intent and fail for future task names.

## Consequences

Evaluation reports can group tasks by explicit family and server expectations with deterministic validation. The current suite still stages only the runtime `nsgo` server, so editor-mode bootstrapping remains follow-up work.
