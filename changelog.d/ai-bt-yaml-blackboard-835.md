### Added

- `game/ai`: add typed per-agent/shared `Blackboard` with change notifications and monotonic revisions, `BlackboardCondition` decorator with observer aborts (`none`, `self`, `lower`, `both`), versioned `*.bt.yaml` parser/encoder/builder (`ParseBehaviorTree`, `EncodeBehaviorTree`, `LoadBehaviorTreeFS`, `BuildBehaviorTree`) supporting subtrees with parameter/key bindings and cycle detection via `loaderr`, and published JSON Schema `bt.v1.json` (#835).
