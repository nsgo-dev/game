### Added

- `pkg/editor/editormcp`: clients that declare the MCP Apps extension (`io.modelcontextprotocol/ui` with `text/html;profile=mcp-app`) see self-contained `ui://nsgo-game/playtest-report` and `ui://nsgo-game/viewport` widget resources, and `game_playtest`, `playtest_report` and `viewport_image` results point at them in `_meta.ui.resourceUri`; other clients see no change, and `go tool game mcp` forwards the extension to the editor (#1961).
