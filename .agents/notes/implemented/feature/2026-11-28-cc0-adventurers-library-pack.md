# Agent Note: CC0 Adventurers library pack

Status: implemented

## Problem

The shared library had only a 2D tile pack, so the 3D templates had no license-clean rigged character that an agent or developer could install and inspect. Animation names and durations were also undocumented.

## Decision

The library ships two small text glTF fixtures, `mage.gltf` and `knight.gltf`, with CC0-1.0 import sidecars, a shared animation-state YAML file, and a generated Markdown inventory. `tools/clipinventory` derives mesh, skin, bone, and animation duration data from the glTF JSON and emits deterministic output. The pack remains optional and does not replace action3d's procedural default.

## Alternatives considered

Bundling binary vendor models would better match production art, but it would make review and deterministic tests depend on opaque media and an external download. Replacing the template default would make every generated project carry a larger asset pack, so the pack is installed only when a project opts in.

## Consequences

`game library install cc0-adventurers` and `game credits` can validate and list the models from their sidecars. The generated inventory must be refreshed when model JSON changes. The fixture geometry is intentionally minimal until the upstream skinning and animation-graph dependencies land, and the README records the shared clip contract for that upgrade.
