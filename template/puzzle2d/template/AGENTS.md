# Project agent guide

- Keep authored files as plain text. Project and game data use versioned YAML with unknown fields refused.
- Run checks through the toolchain: `go tool nsgo check`, `go tool nsgo test`, and `go tool game check` from this project. Do not substitute `go vet` or `go test`.
- `game.yaml` owns the project name, ID and version. After editing it, run `go tool game project sync` to update `nsgo.yaml`.
- Run `go tool nsgo dev` for the game, `go tool game generate` for generated assets, and `go tool game mcp install` to configure editor and game MCP connections.
- Read the matching guide under `docs/` and the schemas under `docs/schemas/` before changing a format. Keep `docs/en` and `docs/zh` equivalent.
- Review the complete source in this template before editing a snippet. Add a changelog fragment for user-visible changes.
