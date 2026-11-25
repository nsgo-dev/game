### Added

- `game/ai`: add generic reusable-buffer graph search (`Graph`, `HeuristicGraph`, `GraphFunc`, `GraphSearch`, `GraphRange`) supporting A*, Dijkstra, and budgeted range queries with path reconstruction, a `HexGrid` adapter over `tilemap.Tilemap` covering all hex stagger axes and indices, and a 2D/3D `WaypointGraph` (#831).
