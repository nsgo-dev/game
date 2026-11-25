### Added

- `game/ai`: attack token pools (`TokenPool`, `TokenManager`, `WithAttackToken`, `HasAttackToken`) with priority preemption, cooldowns, and hold timeouts; non-crossing concentric engagement slot assignment (`SlotAssigner`) with hysteresis around moving targets; and `Squad` coordination with shared `SquadState`, scored role matching, and behavior-tree-readable `SquadOrder`s (#841).
