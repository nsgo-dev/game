### Added

- `tools/agenteval`: added a scheduled agent evaluation suite with versioned YAML task definitions (`platformer-level`, `add-enemy-type`, `add-menu`), deterministic rubric scoring (`0..100` across docs, YAML round-trip, `assetcheck`, domain geometry/entity/menu checks, `nsgo check`, and headless `game/mcp` verification), pluggable `-mode=command` support for external coding agents, and the daily `.github/workflows/agent-eval.yml` CI workflow (#506).
