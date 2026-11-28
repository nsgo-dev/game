### Added

- `tools/agenteval`: `-trials N` runs each task N times, each in a freshly staged workspace, and the report gains per-task `trials`, `pass_rate`, `mean_score` and `stddev`; a task passes only when every trial passes (docs/en/agent-eval.md, docs/zh/agent-eval.md) (#1750).
