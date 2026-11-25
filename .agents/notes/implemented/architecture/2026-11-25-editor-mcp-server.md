# Agent Note: The editor's MCP server on a per-project local endpoint

Status: implemented

## Problem

The NSGo Game Editor is AI first (`docs/design/editor/Spec.dc.html`): the agent does the bulk editing through the editor's MCP server and people watch, review and fine-tune (#1008). Before this change the editor had no MCP surface, so an agent could only write `.scene.yaml` files behind the open editor's back, where the person saw nothing until a reload and could undo nothing. `game/mcp` reaches a running game, not the editor. The connect (#1009), permissions (#1010), turns (#1011), live overlays (#1013) and review (#1014) issues all build on whatever server lands first, so its transport, its tool registry, its threading and its change stream are contracts.

## Decision

Package `game/editor/editormcp` is the editor's MCP server, built on the official MCP Go SDK (`github.com/modelcontextprotocol/go-sdk`, promoted from an indirect to a direct requirement of `packages/game`; NSGo's own `nsgo mcp` is built on the same SDK).

- **Transport**: `Server.Open` listens on a Unix domain socket named after a hash of the project's absolute, symlink-free path (`Endpoint`), in `nsgo-game/editor` under the user's cache directory or `$NSGO_GAME_EDITOR_ENDPOINTS`. The directory is created `0700` and the socket chmod `0600`; there is never a network port. The server follows the editor to the endpoint of another project when `ProjectPath` changes. `Listen` replaces a socket file nothing answers on and returns `ErrInUse` when another editor answers.
- **Proxy**: `go tool game mcp` runs `editormcp.Proxy` on stdio. It mirrors the editor's tool list (built-in tools until it first connects, then whatever the editor lists, kept current through `tools/list_changed`), forwards calls, resource reads and subscriptions, connects lazily and again after the editor restarts, and answers each call with `ErrNoEditor`'s actionable message while no editor has the project open. It speaks MCP 2025-11-25 to the editor, because under 2026-07-28 the SDK's `ClientSession.Subscribe` holds a `subscriptions/listen` request open, which the proxy's synchronous resubscribe-on-connect must not wait on; the proxy's own client may speak either version.
- **Threading**: tool handlers run on the server's goroutines. They reach the editor only through `Call.Editor`, which hands a function to `Options.Run` (`cx.Post` in the window) and waits; a call whose context ends before it starts never runs. A nil `Options.Run` runs functions inline under a mutex for an editor no UI drives. This is what lets `game_check` wait for the asynchronous project check without holding the editor, and `viewport_image` return MCP image content.
- **Edits are editor commands**: `scene_edit` calls the same `Editor` methods the inspector, tree and viewport use, inside the new `History.Group`, so one call is one undo step and a refused edit undoes the call's earlier edits and records nothing; nothing is saved to disk. `add` reads the object through `sceneformat.ParseWith` with the editor's component registry, so it is refused exactly as opening the file would refuse it, and `editormcp` itself decodes no YAML. Refusals are `*EditError` (file, edit index, op, object, component, field, problem, fix), returned as text and as structured content.
- **Registry**: `Server.AddTool` / `RemoveTool` take a `Tool` with an `Action` (`read`, `edit`, `run`) that the permission rules of #1010 decide on; the SDK announces list changes. The five built-in tools are `scene_read`, `scene_edit`, `selection_get`, `viewport_image` and `game_check`, named as Settings › Tools names them.
- **Change stream**: resources `editor://scene` and `editor://selection` with subscriptions. After each editor change the server compares the subscribed resources' content with what it last saw and queues a coalesced `notifications/resources/updated` whose `_meta` carries `nsgo.dev/by` (`agent` while a tool's function runs on the editor's goroutine, else `person`) and `nsgo.dev/revision`. #1011 and #1013 read `by` to tell the agent's edits from the person's.
- **Viewport picture**: `viewport_image` records `Editor.DrawViewport` into a `core.Drawing` and rasterizes it with `game/mcp`'s software rasterizer, exported as `mcp.RenderPNG`, so it is the same on every platform and headless; text and images not in memory are counted, not drawn.
- **Automation too**: `Server.RegisterAutomation` registers the same tools with `nsgo/automation` for an editor run under `nsgo dev -automation`; each runs on the graph goroutine from start to end and returns its PNG as base64 (nsgo-dev/nsgo#2342, nsgo-dev/nsgo#2343).

## Alternatives considered

### Serve the tools through NSGo's automation and `nsgo mcp` only

It reuses NSGo's server, its client setup and the way `game/mcp` already reaches agents, with no new direct dependency. It is rejected as the primary path because `core.RegisterAutomationTool` is a no-op outside automation and test builds (`core/automationtool_off.go:14` in NSGo v0.3.0), `nsgo mcp` attaches only to the app `nsgo dev -automation` runs, handlers run synchronously on the graph goroutine (nsgo-dev/nsgo#2342) and results are JSON only, so a viewport picture cannot be an image (nsgo-dev/nsgo#2343). The tools are still registered there as a secondary path.

### Have `go tool game mcp` relay bytes between stdio and the socket

A pure relay is a few lines and forwards every MCP feature without knowing any. It is rejected because the relay can only fail at start when no editor is open, which agents show as a dead server rather than an error the model reads, and it ends the agent's session whenever the editor restarts; the mirroring proxy answers with how to open the project and reconnects.

### A TCP port on localhost

Any client could reach it without a proxy, and it works the same on every OS. It is rejected because any local user or a browser page can reach a localhost port, ports collide between projects, and the issue requires a local endpoint per project, never a network port.

### Put the server inside package `editor`

Handlers could use the editor's unexported state directly. It is rejected because twelve editor UI issues land in `packages/game/editor` in parallel, the editor package would gain the MCP SDK for every program that embeds it, and the tools work through the editor's exported methods anyway; the only change to `editor` is `History.Group`.

## Consequences

- **Benefits**: an agent edits the open project through undoable editor commands the person sees at once, with refusals it can act on and a file on disk that a refused edit never touches; later issues add tools to one registry and gate them on `Tool.Action`; the change stream tells agent edits from the person's, which turns and overlays need.
- **Costs**: `packages/game` requires the MCP SDK directly, and `cmd/game` links it. Socket paths are limited to 103 bytes, so a long `$NSGO_GAME_EDITOR_ENDPOINTS` is refused with a message. `scene_edit` edits only the scene open in the editor; edits across files wait for turns (#1011). `viewport_image` leaves out text and images not in memory, as `game_screenshot` does. The change stream encodes the open scene after each change while a client subscribes to it.

## Verification

`editor/editormcp` tests cover each tool, the refusal contract (byte-identical file, unchanged history), the endpoint (`ErrNoEditor`, `ErrInUse`, a left socket, following the project), the proxy (no editor, connect, added tools, reconnect, forwarded change stream) and automation registration, with `FuzzSceneEdit` for arbitrary arguments. `cmd/game`'s `TestGameMCPEditsTheOpenEditor` runs the issue's acceptance flow in nsgotest through `go tool game mcp`.
