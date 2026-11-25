### Added

- `game/editor/editormcp`: the editor's MCP server, through which an agent reads and edits the open project with the same editor commands as the inspector, scene tree and viewport. The editor serves its project on a local per-project endpoint, never a network port, with `scene_read`, `scene_edit` (one undo step per call, or nothing and an error naming the file, object, field and fix when the editor refuses an edit), `selection_get`, `viewport_image` and `game_check`, a registry other parts of the editor add tools to, and `editor://scene` and `editor://selection` resources whose notifications say whether the agent or the person made a change (#1008).
- `game/cmd/game`: `go tool game mcp` serves the editor's tools on standard input and output for an agent, forwarding to the editor that has the project open, and says how to open it while none has (#1008).
- `game/editor`: `History.Group` records the commands a function executes as one undo step, or none when it fails (#1008).
- `game/mcp`: `RenderPNG` rasterizes a recorded drawing into a PNG with `game_screenshot`'s software rasterizer (#1008).
