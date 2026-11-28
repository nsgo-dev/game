# Agent Note: MCP Apps widgets

Status: implemented

## Problem

Editor MCP clients received playtest and viewport results as text, structured JSON, and PNG content only. Clients that support MCP Apps had no negotiated resource to render a richer report (#1961).

## Decision

`pkg/editor/editormcp/widgets.go` holds the whole feature. A session supports MCP Apps when the capabilities it declared (`ServerSession.InitializeParams`, which the SDK fills under every protocol version) list `text/html;profile=mcp-app` under `extensions["io.modelcontextprotocol/ui"].mimeTypes`. The two `ui://nsgo-game/` resources are registered on every server, and a receiving middleware (`gateMCPApps`), installed on both the editor's server and the `go tool game mcp` proxy, removes them from `resources/list` and refuses `resources/read` for a session without that capability, before the read handler runs. Successful `game_playtest`, `playtest_report` and `viewport_image` results get `_meta.ui.resourceUri` for capable sessions only; error results, including privacy refusals, never do. The proxy declares to the editor only the MCP Apps extension its client declared, not the client's whole capabilities, since the proxy handles no sampling, elicitation or roots requests itself.

The widget HTML is a static shell whose CSP is `default-src 'none'; img-src data: blob:; style-src 'unsafe-inline'`: no script, no network, no external URLs.

## Alternatives considered

- Always returning widget metadata would make terminal clients attempt to fetch unsupported resources and would violate capability negotiation.
- Registering the resources per session would need a server per session; the SDK registers resources per server, so filtering at the method layer is the smaller change.
- Forwarding the proxy client's full capabilities would advertise sampling and elicitation the proxy cannot answer.
- Embedding remote scripts or styles would make the widget depend on network access and weaken the editor's privacy boundary.

## Consequences

- Clients without the extension get byte-identical results and resource lists, so terminal agents and the built-in agent see no change.
- Not yet delivered from #1961: the widgets do not render the result's data (that needs a script reading the host's tool-result messages, which the CSP forbids today), and `_meta.ui.resourceUri` carries no per-result query or widget session ID.
- Viewport privacy is enforced by the existing tool policy: with viewport images off, `viewport_image` is refused and its result has no widget URI.
