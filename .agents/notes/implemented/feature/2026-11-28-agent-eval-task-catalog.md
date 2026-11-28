# Agent Note: Agent evaluation task catalog

Status: implemented

## Problem

The agent evaluation suite had only creation tasks and did not record which editor or runtime MCP tools a task was intended to exercise.

## Decision

Task checks accept a deterministic `required_tools` list validated against a known catalog. A new `editor-workflow` task uses the existing menu fixture while declaring scene, layer, edit and game-check tools.

## Alternatives considered

Leaving tool coverage only in prose was rejected because it cannot detect stale or misspelled tool names. Adding a separate unversioned manifest was rejected because task YAML already carries the rubric and travels with its starter and reference fixtures.

## Consequences

Task review can identify intended MCP coverage, and unknown tools fail at task load. Running an editor server in a new mode and adding 3D, datatable and prompt-injection fixtures remain follow-up work.
