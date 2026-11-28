# Agent Note: MCP 3D and UI contract acceptance

Status: implemented

## Problem

The optional MCP 3D and UI packages already defined serializable provider contracts, but acceptance coverage did not exercise a provider raycast or explicitly protect path-based UI activation as a runtime boundary.

## Decision

Keep `pkg/mcp` independent of `scene3d`, `physics3d`, `render3d` and `gameui`. Add focused contract tests around a fake 3D provider and retain the existing UI tests that resolve labels to stable widget paths and refuse disabled widgets. Document that these packages are opt-in contracts until a later tool-registration bridge.

## Alternatives considered

Importing renderer and UI packages into `pkg/mcp` would expose richer inspection immediately but would increase release dependencies and violate the optional-provider architecture. Implementing tool registration without a running-game bridge would create APIs that cannot execute.

## Consequences

Games can validate their provider implementations and preserve path-safe UI actions without pulling in renderer dependencies. Full MCP tool registration and screenshots remain a separate integration task.

## Verification

`mcp3d` provider raycast and existing `mcpui` activation tests pass, and the package checks confirm the contracts remain standalone.
