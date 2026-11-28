# Agent Note: Generated asset kinds and provenance

Status: implemented

## Problem

Generated Assets accepted only images and sound, and a source sidecar could not identify a media family or cover related files such as a model and its material draft.

## Decision

The generator kind enum accepts images, sound, textures, models, animation and tiles. Asset source sidecars retain backward-compatible version 1 parsing while adding optional `kind` and `files` metadata. Picking a generated candidate writes the requested kind into the sidecar.

## Alternatives considered

Bumping the sidecar version immediately was rejected because existing generated assets must remain releasable without migration. Encoding related files as an opaque prompt field was rejected because release and review tooling needs deterministic paths.

## Consequences

Editor settings and provenance can represent new generator families and multi-file outputs. Decoding and preview adapters for model, animation and tiles payloads, plus MCP request tools, remain follow-up work.
