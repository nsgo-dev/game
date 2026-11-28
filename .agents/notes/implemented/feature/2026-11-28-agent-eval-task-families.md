# Agent Note: Agent evaluation task families

Status: implemented

## Problem

The evaluation catalog had only create-task metadata, so reports could not distinguish editor, debugging, 3D, data, code, and safety workflows or state which MCP servers a task expected.

## Decision

Tasks may declare a constrained `family` and an `mcp_servers` list of distinct names. Both fields stay optional, so a task without them still loads; the three existing tasks are tagged `create` with `[nsgo]`, the one server the staged `mcp.json` configures. The metadata is intentionally independent of grading so new families can be added without changing old rubrics.

## Alternatives considered

Inferring families from task IDs would be brittle and would hide the classification from reviewers. Requiring the fields immediately would invalidate the existing catalog, so both remain optional during migration.

## Consequences

Grading and reports do not read the fields yet; future runners and reports can group task outcomes by family and check MCP coverage. `mcp_servers` is not checked against the staged `mcp.json`, because tasks for servers the runner does not stage yet (such as an editor server) need to declare them first. Adding a new family requires extending the explicit allow-list and adding task fixtures.

