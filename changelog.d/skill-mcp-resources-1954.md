### Added

- `game/pkg/editor/editormcp`: the editor's MCP server and the `go tool game mcp` proxy serve the `nsgo-game` agent skill built into their binary as `text/markdown` resources at `skill://nsgo-game/SKILL.md` and `skill://nsgo-game/references/<file>` (the proxy from its own copy, so before an editor has the project open), and the read-only `skill_get` tool returns one section of a skill file by its exact heading for clients without resource support (#1954).
- `game/skills`: a new package that embeds the `nsgo-game` skill (`skills.Files`, `skills.ReadFile`) (#1954).
