# Agent Note: Release MCP self-test evidence

Status: implemented

## Problem

Release smoke tests did not have a user-runnable MCP self-test or a structured YAML artifact recording whether the headless editor tool registration was available.

## Decision

`game mcp selftest` performs a headless editor MCP registration check and writes deterministic `release-check.yaml` evidence with version, project/template, OS, check name, status, duration, and error. The same command is suitable for release workflows to upload without contacting a model provider.

## Alternatives considered

Parsing release logs would be fragile and would not provide a reusable local check. A browser-based test would require a display and platform packaging, so the first slice uses the existing scope-less editor and software-capable MCP server.

## Consequences

The report gives release automation a stable artifact and lets users run the smoke check locally. Full per-template scene-edit and viewport round trips remain follow-up checks as the release matrix grows.
