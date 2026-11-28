# Agent Note: Asset generator kinds

Status: implemented

## Problem

The editor's MCP asset generation flow only classified image and sound outputs, so requests for textures, 3D models, animation clips, and tilesets could not select an appropriate generator or preserve structured asset bytes.

## Decision

`AssetGeneratorKind` now includes texture, model, animation, and tileset. Target suffixes infer those kinds, generator discovery prefers kind-specific tool names, and structured candidate payloads remain bounded raw bytes until Pick writes them. Existing image and sound decoding remains unchanged.

## Alternatives considered

Adding a separate generator API for every media type would duplicate connection, candidate, licensing, and undo behavior. Treating every new type as an image would reject valid glTF and YAML payloads and choose the wrong MCP tool.

## Consequences

Projects can configure one generator server per structured asset kind and use the same review/pick flow. Structured payload validation is currently size-bounded rather than format-decoded, so downstream asset checks remain responsible for detailed model, animation, and tileset validation.
