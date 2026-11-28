# Agent Note: Agent evaluation task families

Status: implemented

## Problem

The evaluation catalog had only create-task metadata, so reports could not distinguish editor, debugging, 3D, data, code, and safety workflows or state which MCP servers a task expected.

## Decision

Tasks may declare a constrained `family` and `mcp_servers` list. Existing tasks omit both fields and remain valid. The metadata is intentionally independent of grading so new families can be added without changing old rubrics.

## Alternatives considered

Inferring families from task IDs would be brittle and would hide the classification from reviewers. Requiring the fields immediately would invalidate the existing catalog, so both remain optional during migration.

## Consequences

Future runners and reports can group task outcomes and verify MCP coverage. Adding a new family requires extending the explicit allow-list and adding task fixtures.

