# Agent Note: Template agent guidance files

Status: implemented

## Problem

Projects created from templates gave coding agents no project rules, command conventions or Claude Code compatibility file. The repository's agent workflow was therefore absent from the first run.

## Decision

Every template and its internal CLI copy receives the same short `AGENTS.md`. Each `CLAUDE.md` imports it with `@AGENTS.md`, avoiding duplicated guidance. A repository check verifies both files exist for every template. The guide points to `go tool game mcp install`, the version-matched docs and the required nsgo checks.

## Alternatives considered

- Copy only `AGENTS.md`. Claude Code reads `CLAUDE.md` as its project instruction file, so omitting it leaves one major client without the rules.
- Maintain separate AGENTS and CLAUDE text. Duplicated instructions drift, so CLAUDE imports the single source.

## Consequences

New projects start with consistent agent guidance and repository checks catch missing files. MCP attachment inside every game is a separate runtime integration and remains dependent on each template's root component design.
