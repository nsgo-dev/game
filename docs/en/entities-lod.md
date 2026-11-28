# Entity simulation LOD

`entities.LODBand`, `ResolveLOD` and `NearestViewerDistance` provide deterministic distance bands with hysteresis. Systems can store the resulting level in an entity component and skip or time-slice far work without changing the scheduler's fixed-step ordering.
