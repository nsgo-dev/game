### Fixed

- `pkg/editor`: Start with AI now connects to the chosen CLI or built-in agent for real (reporting the editor MCP server's live tool count and validating API keys against the built-in model endpoint), asks the agent for a plan via `plan_propose`, builds the first scene through `scene_edit`, runs `go tool game playtest` (or skips honestly when `playtest.yaml` is absent), and derives the first review from the actual written scene instead of fixed design-artboard content (#1505).
