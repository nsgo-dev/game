# Agent Note: Typed reusable-scene parameter declarations

Status: implemented

## Problem

Reusable scenes exposed inner object IDs and fields directly through raw overrides. Authors and tools had no typed, stable declaration for the values intended to vary per instance.

## Decision

`sceneformat` adds versioned-YAML fields for scene parameter declarations and instance parameter values. `SceneParameter` carries a name, one of eight scalar/vector/asset types, a default, description, and one or more object-field bindings. Validation rejects duplicate or unknown types, wrong defaults, empty bindings, and bindings to objects absent from the scene. Object instances carry a `parameters` map for the eventual resolver integration.

## Alternatives considered

Adding editor-only metadata would leave game tools and MCP without a shared contract. Reusing raw `ObjectOverride` maps would preserve the internal-ID coupling. Both were rejected in favor of typed scene-format declarations.

## Consequences

Scene files can now round-trip a validated declaration without changing existing version 1–4 documents that omit the field. Resolution, inspector promotion, and MCP set/read operations remain follow-up work and must consume these declarations before parameter values affect instantiated objects.
