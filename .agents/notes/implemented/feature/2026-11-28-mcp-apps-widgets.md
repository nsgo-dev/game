# Agent Note: MCP Apps widgets

Status: implemented

## Problem

Editor MCP clients received playtest and viewport results as text, structured JSON, and PNG content only. Clients that support MCP Apps had no negotiated resource to render a richer report.

## Decision

The server detects `io.modelcontextprotocol/ui` and the `text/html;profile=mcp-app` MIME type from each session's initialize capabilities. It advertises two self-contained `ui://` resources only to capable clients and adds `_meta.ui.resourceUri` to playtest and viewport results. The proxy forwards the client's capabilities on its downstream editor connection. Non-capable clients keep the existing result payload, and widget HTML disallows network connections.

## Alternatives considered

Always returning widget metadata would make terminal clients attempt to fetch unsupported resources and would violate capability negotiation. Embedding remote scripts or styles would make the widget dependent on network access and weaken the editor's privacy boundary.

## Consequences

Chat clients can render a stable report or viewport shell without changing tool schemas. The current widget shells intentionally rely on host-supplied tool content and do not call tools back. Viewport privacy remains enforced by the existing tool and resource policies.
