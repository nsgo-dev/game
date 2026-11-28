# Agent Note: Canonical generator parameter vocabulary

Status: implemented

## Problem

Asset generators received one private argument shape, so provider tools with aliases, enums, or bounds required wrapper programs and discovered invalid values only after a call.

## Decision

The editor defines a fixed canonical vocabulary, parses the connected MCP tool's object JSON Schema, maps aliases such as `aspect_ratio` and `num_images`, applies `GeneratorServerConfig.Params` overrides, and validates types, enums, and numeric bounds before `CallTool`. Existing `target`, `prompt`, `count`, and `kind` arguments remain in the request for legacy servers.

## Alternatives considered

Keeping one private schema would preserve the current implementation but force every provider through a wrapper. Guessing capabilities from model names would expose controls that a server does not declare, so the connected tool schema remains authoritative.

## Consequences

Providers can be connected by configuration and invalid paid calls are rejected locally. Nested provider-specific schemas and UI controls for every mapped field remain follow-up work, while explicit wire-key overrides cover names outside the alias table.
