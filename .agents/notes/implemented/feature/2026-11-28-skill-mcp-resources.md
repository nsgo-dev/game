# Agent Note: Skill served as MCP resources

Status: implemented

## Problem

Agents could read a locally installed skill that was older than the editor MCP server and had no section router for focused workflow guidance.

## Decision

The repository embeds `skills/nsgo-game` in a small `skills` package. The editor MCP server advertises each Markdown file under `skill://nsgo-game/` and adds a read-only `skill_get` tool that extracts an exact heading section with strict arguments.

## Alternatives considered

Embedding the skill in every game binary would increase shipped binaries, so the resource is served by the editor process only. Returning the entire skill for every request would waste context, so `skill_get` returns one heading section.

## Consequences

MCP clients can use the same skill version as the editor binary and avoid stale local copies. The proxy receives the resources through the editor server, while the running-game MCP server remains unchanged.
