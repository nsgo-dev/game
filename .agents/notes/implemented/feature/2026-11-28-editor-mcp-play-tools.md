# Agent Note: Editor MCP live Play contracts

Status: implemented

## Problem

The editor MCP server lets an agent drive Play only after the person has frozen a frame (`play_frozen_context`, `play_replay_step`). Issue #1743 asks for live Play tools (start, stop, step, input, state, frame and Output panel reads). Their handlers need the editor's Play session, the in-process `mcp.Bridge` and viewport capture, which is a larger change; adapters written in parallel need a fixed wire shape first.

## Decision

`editormcp` reserves the seven tool names as constants and exports the argument and result types with snake_case JSON tags (`PlayStartArgs`, `PlayStepArgs`, `PlayInputArgs`, `PlayState`, `PlayEntity`, `OutputEntry`). `ValidatePlayStep` bounds `count` to 1..3600, the same limit as `play_replay_step` (`maxPlayReplaySteps`), and `ValidatePlayInput` requires an action and one of the event kinds `press`, `release`, `tap`, `axis` or `pointer`, with an axis value in [-1, 1] as `game_input_action` takes. The server does not register any of the tools yet, and the docs say so.

## Alternatives considered

Making agents guess pixel coordinates and shell commands was rejected because the Balanced permission profile denies shell access and coordinates do not identify game state. Importing runtime game and renderer packages into the MCP protocol layer was rejected because each editor project owns those implementations. Leaving the struct fields untagged was rejected: Go would encode them as `Action` and `Playing`, unlike every other editor MCP tool's snake_case keys.

## Consequences

Handlers can be added without changing the wire shapes or accepting unbounded stepping. The remaining work for #1743 is registering the tools with JSON schemas under the `playtests` run permission, wiring them to `Editor.Play`, the Play bridge, viewport capture and the Output panel, the Agent panel's "agent playing" indicator, and the end-to-end tests the issue lists.
