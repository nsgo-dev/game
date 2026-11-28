# Agent Note: MCP 3D and UI contracts

Status: implemented

## Problem

The runtime MCP bridge had no dependency-light contract for inspecting 3D nodes or activating game UI widgets, so games would have to couple the core bridge to renderer and gameui packages.

## Decision

`pkg/mcp3d` and `pkg/mcpui` expose optional serializable snapshots and provider callbacks. `mcpui.Activate` resolves a widget by path or label and delegates the action to the game's own handler. `mcp3d` carries node, camera and raycast records while leaving rendering and physics ownership with the game.

## Alternatives considered

Importing scene3d, physics3d and gameui into pkg/mcp was rejected because it would enlarge every runtime build and create coupling cycles. Pixel-coordinate-only tools were rejected because they cannot reliably target widgets or explain 3D ray hits.

## Consequences

Games can attach these packages without changing the existing 2D MCP bridge. Concrete adapters and MCP tool registration remain follow-up work, while tests pin the provider and lookup contracts.
