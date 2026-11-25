### Added

- `game/ai`: add deterministic behavior tree state snapshots (`BehaviorTree`, `BehaviorTreeSnapshot`, `BTNodeSnapshot`, `SnapshotBehaviorTree`, `RestoreBehaviorTree`, `SnapshotNode`, `ChildNodes`, `ErrInvalidBTSnapshot`) with YAML/JSON round-trip support, structural shape validation, and transactional restore across all built-in and custom nodes (#837).
- `game/flow`: add `Machine.Snapshot`, `Machine.Restore`, `Machine.Started`, `MachineSnapshot[S]`, and `ErrInvalidSnapshot` to save and restore finite state machine state without running `Exit` or `Enter` hooks (#837).
