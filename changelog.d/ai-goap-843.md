### Added

- `game/ai`: goal-oriented action planning (GOAP) with bitset-packed `WorldState` and small integer facts, regressive A* `GOAPPlanner` with node budget and zero-allocation buffer reuse, auto-replanning `PlanRunner` implementing behavior tree `Node`, and versioned YAML action sets `ParseGOAP`/`EncodeGOAP` (#843).
