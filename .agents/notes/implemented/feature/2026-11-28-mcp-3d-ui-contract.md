# Agent Note: MCP 3D and UI contracts

Status: implemented

## Problem

The runtime MCP bridge (`pkg/mcp`) had no contract for inspecting 3D nodes or activating game UI widgets (#1745), and adding one inside `pkg/mcp` would make every game that attaches the bridge link `gameui`, `scene3d`, `physics3d` and the 3D renderer.

## Decision

`pkg/mcp/mcpui` and `pkg/mcp/mcp3d` hold serializable records and a `Provider` interface each, and import nothing from the repository. They nest under `pkg/mcp` because they only refine the bridge (the nesting rule in `2026-11-27-repository-layout-2.md`), so they share its label, catalog row and guide. `mcpui.Find` matches a widget path first and only then a label, case-insensitively, and refuses a label that more than one widget carries with `ErrAmbiguous`, because a menu with two "OK" buttons must not let an agent press whichever comes first in traversal order. `mcpui.Activate` refuses a disabled widget with `ErrDisabled` before the provider runs, and passes the resolved path, never the label, to `Provider.Activate`, so the game's handler sees the same identifier for every lookup.

## Alternatives considered

Importing `scene3d`, `physics3d` and `gameui` into `pkg/mcp` was rejected because it enlarges every build that attaches the bridge and risks import cycles. Pixel-coordinate-only tools were rejected because they cannot reliably target widgets or explain 3D ray hits. Flat packages at `pkg/mcpui` and `pkg/mcp3d` were rejected by the layout rule above.

## Consequences

This change delivers the contracts only: no MCP tool is registered on them, no adapter over `gameui` or `scene3d` exists, and no 3D screenshot is rendered. Those, with the `go list -deps` guard and an example attaching `mcp3d`, are the remaining work of #1745. The unit tests pin the lookup order, ambiguity, disabled and not-found behavior.
