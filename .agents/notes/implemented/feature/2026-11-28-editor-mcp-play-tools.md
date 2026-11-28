# Agent Note: Editor MCP Play contracts

Status: implemented

## Problem

The editor MCP server exposed frozen replay context but no stable argument and result contract for starting Play, stepping input, reading live state, capturing frames or reading Output entries.

## Decision

`editormcp` defines named Play and output tools plus strict argument validation for bounded fixed-step counts and explicit input event kinds. Results use serializable state, entity and output records that adapters can connect to the editor's existing Play session.

## Alternatives considered

Making agents guess pixel coordinates and shell commands was rejected because balanced permission profiles deny shell access and coordinates do not identify game state. Importing runtime game and renderer packages into the MCP protocol layer was rejected because each editor project owns those implementations.

## Consequences

Tool adapters can be added without changing the wire shapes or accepting unbounded stepping. The current change establishes the contract, while wiring handlers to Editor.Play and viewport capture remains follow-up integration work.
