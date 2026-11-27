### Fixed

- `game/ai`: `Selector` now falls back to normal per-tick re-evaluation for higher-priority branches that mix `BlackboardCondition` with plain `Condition`, `Guard`, `WithAttackToken`, or other non-blackboard gating nodes, so non-blackboard conditions still preempt running lower-priority siblings while pure blackboard-only branches keep their key-driven observer behavior and `AbortSelf`/`AbortBoth` self-aborts continue to work (#1585).
