### Added

- `pkg/pathfind`, `pkg/pathfind3d`: `NavAgent.DesiredVelocity` returns the corridor-following velocity for an avoidance solver such as `ai.ORCASolver`, and `NavAgent.StepWithVelocity` moves the agent with the chosen velocity under `MaxAcceleration` and `MaxSpeed` with the same corridor repair, surface movement, link traversal and arrival handling as `Step` (#1718).
