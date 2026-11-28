# Agent Note: Editor MCP field change reports

Status: implemented

## Problem

Editor MCP edit responses only described an operation and revision. Agents could not verify a field's previous value or tell whether an inherited or instanced edit was recorded as an override.

## Decision

`scene_edit` returns one `FieldChange` per changed field and `object_set_field` returns its single `FieldChange`. Records contain before and after values, a deterministic changed flag, and override metadata from `Editor.ObjectOverrideInfo`.

## Alternatives considered

Returning the whole scene YAML would expose unrelated data and force agents to diff YAML. Reporting only the revision would not identify no-op writes or override routing.

## Consequences

Responses are larger and values use JSON-compatible representations. Existing text summaries remain unchanged, while structured consumers can explain each edit and its storage location.
