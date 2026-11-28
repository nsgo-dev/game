# Agent Note: Stable selection context for agent turns

Status: implemented

## Problem

The editor allowed a person to change selection while an Agent panel, background task, or automation turn was running. The turn could then lose the context that ⌘L captured, and an id-less MCP edit could be associated with the wrong object.

## Decision

Every turn created by the panel and task runners snapshots `AgentTurnContext` at request start, including scene path, selected IDs and names, active layer, and view mode. The existing `OpenAgentTurnPin` lookup uses the task ID for background and automation turns and the latest foreground turn otherwise. MCP target resolution continues to use this snapshot and never the live selection.

## Alternatives considered

Reading the live selection at every tool call would keep the UI current but allows a click during an agent turn to redirect work. Requiring every tool call to repeat object IDs would avoid ambiguity but would discard the context contract already provided by ⌘L.

## Consequences

Selection changes remain available for the person while an agent works. A removed or renamed pinned object is reported as an explicit target error. The turn context gains a small deterministic snapshot for background and automation turns.
