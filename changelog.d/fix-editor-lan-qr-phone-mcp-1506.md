### Fixed

- `pkg/mcp`, `pkg/editor`, `pkg/editor/editormcp`: Play-mode web builds served from the editor's LAN QR server now auto-discover `/__nsgo_mcp/info` and load `/__nsgo_mcp/bridge.js`, register `window.__nsgoMCP.callTool` backed by `game/mcp` (including the new `game_state_dump` tool) and `game.Loop`, and expose `phone_sessions_list` and `phone_call_tool` on the editor MCP server under the permission and privacy rules (#1506).
