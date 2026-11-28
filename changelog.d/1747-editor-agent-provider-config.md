### Added

- `pkg/editor`: `editor.yaml` can set `agent.builtInEndpoint`, `agent.builtInModel` and `agent.builtInFastModel` to point the built-in agent at another Anthropic Messages server (https, or loopback http for local servers) and choose its model IDs; `agent.builtInFormat` accepts `anthropic` or `openai`, and the built-in runner refuses `openai` with `ErrBuiltInFormatUnsupported` until the OpenAI-compatible adapter lands (#1747).
