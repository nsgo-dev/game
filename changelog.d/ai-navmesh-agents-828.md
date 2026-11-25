### Added

- `game/ai`, `game/world3d`: add deterministic 2D and 3D navmesh path-corridor agents (`NavAgent`, `NavAgentGroup`, `NavAgentState`, `NavLinkEvent`) with cheap local corridor repair, throttled/staggered replan budgeting (`MaxReplansPerStep`, `ReplanInterval`, `StaggerStride`), and off-mesh/flagged link traversal callbacks (`AddOffMeshLink`, `SetEdgeFlags`, `OnLink`) (#828).
