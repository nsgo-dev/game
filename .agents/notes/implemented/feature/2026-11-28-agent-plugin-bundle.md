# Agent Note: Agent plugin bundle and connection proof

Status: implemented

## Problem

Installing the skill, editor MCP, and running-game MCP required separate host-specific steps, and configuration files alone did not prove the editor was connected.

## Decision

The repository ships Claude Code and Codex plugin manifests bundling the same skill, onboarding skill, and two local MCP servers. `game mcp prompt` prints a host-specific setup prompt that preserves unrelated servers and requires a read-only connection proof via `selection_get` before onboarding continues.

## Alternatives considered

Keeping only manual install documentation would retain host drift and would not provide a machine-readable bundle. Claiming success after writing `.mcp.json` was rejected because the editor may be closed or the command unavailable.

## Consequences

Hosts have one-step manifests and a shared onboarding contract. The prompt is local and does not authenticate or contact model providers. Full GUI screenshot coverage remains host-dependent.
