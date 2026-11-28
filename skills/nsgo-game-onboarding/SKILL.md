# NSGo Game onboarding

1. Reuse an existing plugin or MCP setup and preserve unrelated servers. Install only what is missing.
2. Prove the connection with a read-only call to `selection_get` on the editor server. If the editor is closed, report that state instead of claiming success.
3. Verify `go tool game` resolves in the project, then ask the person for the game brief before editing.
