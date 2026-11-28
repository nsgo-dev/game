### Added

- `pkg/editor`: agent usage in `editor.yaml` records this week's input, cached-input and output tokens and estimated cost in US dollars (`Editor.RecordAgentSpend`), and agent settings accept `weeklySpendLimitUSD`, `requestSpendLimitUSD` and `toolCallLimit` caps, with `Editor.AgentSpendCapReached` reporting the weekly spend cap (#1746).
