### Added

- `game/procgen`: versioned `*.placement.yaml` placement files (`ParsePlacement`, `EncodePlacement`) with a seeded `grid` sampler whose `Points` gives the same prefab points on every machine; each step draws from its own stream forked by its ID (#1729).
