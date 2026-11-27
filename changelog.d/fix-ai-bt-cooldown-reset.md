### Fixed

- `game/ai`: track behavior tree `Cooldown` timers against a monotonic root tree clock so cooldowns continue elapsing when a branch is not ticked, survive `Reset()` from interruptions (`Guard`, `Selector` preemption, `TimeLimit`), preserve pending remaining time across `SnapshotBehaviorTree`/`RestoreBehaviorTree`, and add `ResetCooldowns` / `BehaviorTree.ResetCooldowns` for explicit hard resets (#1586).
