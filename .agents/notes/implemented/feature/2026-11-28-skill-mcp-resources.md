# Agent Note: The nsgo-game skill served as MCP resources and through skill_get

Status: implemented

## Problem

The `nsgo-game` skill reached an agent only as files copied by `bunx skills add`, so an agent read whatever version was current when it installed the skill, not the version of the `go tool game` the project pins. The editor's MCP server and the `go tool game mcp` proxy served only resources about the open project (`editor://scene` and the rest), so neither could hand the agent the skill that matches the tools it answers.

## Decision

- `skills/embed.go` is the package `github.com/nsgo-dev/game/skills`, which embeds `skills/nsgo-game` and exposes `Files` (paths relative to the skill, lexical order) and `ReadFile`. It sits beside the skill, outside `pkg/`, because `go:embed` cannot reach a parent directory; the repository layout note records the exception, and the package has rows in `docs/{en,zh}/stability.md` and the skill's `packages.md`.
- `pkg/editor/editormcp` serves each embedded file as a `text/markdown` resource at `skill://nsgo-game/<path>` (`SkillURIPrefix`), from `skillResources` and `readSkillResource` in `skill.go`. The editor's server and the proxy register the same list. The proxy reads the skill from its own embedded copy rather than forwarding to the editor, so the resources exist before an editor has the project open and do not depend on the editor's content policy, which governs project content only.
- The read-only `skill_get` tool takes `{file, section}`, both required, unknown fields refused by the server's strict argument decoding. `file` is a path inside the skill (a `nsgo-game/` or `skill://nsgo-game/` prefix is accepted), checked with `fs.ValidPath`. `section` must equal an ATX heading's text exactly; the section runs to the next heading of the same or a higher level, and lines inside fenced code blocks never count as headings, since `references/mcp-tools.md` has `# ...` shell comments inside a fence under `## Connecting an MCP client`.

## Alternatives considered

- **Embedding the skill in every game binary** so the running game's `nsgo mcp` server serves it too: rejected, it grows every shipped binary for a development aid, and NSGo v0.3.4's `automation.Register` has no resource registration (upstream nsgo-dev/nsgo#2722).
- **The proxy forwarding skill reads to the editor** like the `editor://` resources: rejected, the skill would vanish whenever no editor has the project open, which is exactly when an agent needs to learn how to open one.
- **Returning a whole file from `skill_get`**: rejected, the references total about 56 KB and most questions need one table, so the tool returns one section and the resources carry the whole files.
- **Fuzzy or case-insensitive heading matching**: rejected, an exact match keeps the tool deterministic, and the error tells the agent to pass the heading as written.

## Consequences

- Every client of the editor's server or the proxy can read the skill in the version of the binary that answers its tool calls, and the docs say that copy wins over an installed one.
- `skill_get` through the proxy is forwarded to the editor like every built-in tool, so a client without resource support needs the editor open; the resources do not.
- Not done here, and still open in #1954: the router table in `SKILL.md`, and the `internal/repocheck` checks that every tool, argument, command and flag the skill names exists.
