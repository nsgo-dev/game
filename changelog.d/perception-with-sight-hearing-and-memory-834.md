### Added

- `game/ai`: perception for agents on `game.Loop`'s fixed steps: sight cones with a peripheral range and an eye offset, line of sight through a callback with a `physics` raycast adapter, noises heard within a range scaled by loudness, per-stimulus memories with last known position and velocity, decaying strength and a forget time, team attitude filters, gained/updated/lost/forgotten events, and a per-step line-of-sight budget served round-robin in a fixed order (#834).
- `game/ai/perception3d`: the same perception for 3D agents on `math3d.Vec3`, with a `physics3d` raycast adapter (#834).
- `game/lights2d`: `LineOfSight` adapts occluders to `ai.LineOfSight`, so the walls that cast shadows also block an agent's sight (#834).
