### Added

- `game/ai`: add expansion budgets and partial-path fallback (`PathOptions`, `GridPathResult`, `NavPathResult`, `AStar.MaxExpansions`, `AStar.FindWithOptions`, `AStar.FindWithOptionsInto`, `NavMesh.FindPathWithOptions`), deterministic incremental path searches (`AStarSearch`, `NavMeshSearch`, `IncrementalSearch`, `SearchStatus`), and a budgeted deterministic `PathQueue` with FIFO/round-robin scheduling, priorities, cancellation, polling, and callbacks (#832).
- `game/world3d`: add bounded and partial 3D `NavMesh` searches (`NavPathResult`, `NavMesh.FindPathWithOptions`) and incremental `NavMeshSearch` (`NavMesh.StartSearch`, `NewNavMeshSearch`) implementing `ai.IncrementalSearch` for direct scheduling on `ai.PathQueue` (#832).
